package history

// POSTER-WRITE-HARDENING wave-36 (codex local review round 6, PR#215) —
// findings F3+F4 around the quarantine reservation and the wedge
// move-back:
//
//   - F4: claimBackupQuarantineName O_EXCL-claimed the .dlq. name, closed
//     the handle, and the replace-aware quarantining rename displaced
//     whatever occupied the name at THAT instant — a foreign writer
//     renaming the reservation away and planting its own bytes got them
//     destroyed by OUR move. The claim now returns the reservation's
//     captured identity and the move re-derives it first
//     (backupQuarantineReservationStillOurs): any divergence is the typed
//     collision class and behaves exactly like the claim-failure leg
//     (journal entry live, foreign bytes intact).
//   - F3: the hold's restore() move-back only LOGGED its failure — callers
//     left the entry armed (or clean-pending) against a journaled name now
//     proven UNOWNED while the verified bytes sat at the .dlq. name.
//     restore() returns the classified failure
//     (errBackupQuarantineRestoreFailed), internal wedge legs JOIN it into
//     the removal error chain, and pendingKindForRemovalError routes it to
//     the rearm-refused (journal-only) pending kind.
//
// Test matrix: reservation swap (metadata leg, MemMap — and a deterministic
// dev/inode leg on OS files), reservation vanish, the claim's stat-failure
// leg, the hold restore classification + retry contract, and the joined
// unlink-time refusal classes.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// w36SwapOnCloseFile replays the foreign writer landing inside the
// claim→move handoff: the very instant the reservation handle closes (the
// claim's last act before the caller's pre-move verify), the swap hook runs
// against the reserved .dlq. name.
type w36SwapOnCloseFile struct {
	afero.File
	name    string
	onClose func(name string)
}

func (f w36SwapOnCloseFile) Close() error {
	err := f.File.Close()
	if err == nil && f.onClose != nil {
		f.onClose(f.name)
	}
	return err
}

// w36ReservationSwapFs wraps the FIRST quarantine-reservation create with
// the swap-on-close file — the claim→verify handoff replay (finding F4).
type w36ReservationSwapFs struct {
	afero.Fs
	swap  func(name string)
	fired bool
}

func (f *w36ReservationSwapFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err == nil && f.swap != nil && !f.fired && flag&os.O_EXCL != 0 && strings.Contains(name, backupQuarantineSuffix) {
		f.fired = true
		return w36SwapOnCloseFile{File: file, name: name, onClose: f.swap}, nil
	}
	return file, err
}

// F4 headline, metadata leg (every platform): the reservation placeholder is
// swapped for a real foreign file between the claim and the quarantine move.
// The move is refused with the typed collision class — nothing relocates,
// the foreign occupant keeps its bytes, and the journaled backup is intact.
func TestRemoveReplacementBackupW36_ForeignReservationSwapRefused(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w36s/poster.jpg.dlbak." + p3HexA
	w26WriteBackup(t, base, backup, "old")
	fs := &w36ReservationSwapFs{Fs: base, swap: func(name string) {
		require.NoError(t, base.Remove(name))
		require.NoError(t, afero.WriteFile(base, name, []byte("foreign reservation occupant"), 0o600))
	}}

	err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w36 unit", nil, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision,
		"a reservation proven foreign is the typed collision class")
	require.Contains(t, err.Error(), "no longer names the claimed empty placeholder")
	require.True(t, fs.fired)
	require.Equal(t, "old", string(mustRead2(t, base, backup)),
		"the journaled backup never moved — the refusal pre-empts the rename")
	names := w26DirQuarNames(t, base, "/w36s")
	require.Len(t, names, 1, "the foreign occupant keeps the .dlq. name byte-intact")
	require.Equal(t, "foreign reservation occupant", string(mustRead2(t, base, "/w36s/"+names[0])))
}

// F4, dev/inode leg on real files (POSIX): a same-size (0-byte), same-mtime
// substitute at the reservation name — two simultaneously-existing files
// guarantee the inode comparison cannot collapse.
func TestRemoveReplacementBackupW36_ReservationDevInodeMismatchRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dev/inode identity is POSIX-shaped")
	}
	base := afero.NewOsFs()
	tmp := t.TempDir()
	backup := filepath.Join(tmp, "poster.jpg.dlbak."+p3HexA)
	require.NoError(t, os.WriteFile(backup, []byte("old"), 0o600))
	foreign := filepath.Join(tmp, "foreign-placeholder")
	require.NoError(t, os.WriteFile(foreign, nil, 0o600))

	fs := &w36ReservationSwapFs{Fs: base, swap: func(name string) {
		claimInfo, lerr := os.Lstat(name)
		require.NoError(t, lerr)
		require.NoError(t, os.Chtimes(foreign, claimInfo.ModTime(), claimInfo.ModTime()))
		require.NoError(t, os.Remove(name))
		require.NoError(t, os.Rename(foreign, name))
	}}

	err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w36 unit", nil, nil)
	require.True(t, fs.fired)
	require.Error(t, err)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision)
	require.Contains(t, err.Error(), "dev/inode mismatch")
	require.Equal(t, "old", string(mustRead2(t, base, backup)))
	entries, derr := os.ReadDir(tmp)
	require.NoError(t, derr)
	require.Len(t, entries, 2, "the refused occupant stays at its displaced name; nothing else moved")
}

// F4, vanish leg: the reservation name was deleted inside the handoff — an
// indeterminate plain error (never a silent move into a watchable gap).
func TestRemoveReplacementBackupW36_ReservationVanishRefuses(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w36v/poster.jpg.dlbak." + p3HexA
	w26WriteBackup(t, base, backup, "old")
	fs := &w36ReservationSwapFs{Fs: base, swap: func(name string) {
		require.NoError(t, base.Remove(name))
	}}

	err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w36 unit", nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "inspect quarantine reservation")
	require.NotErrorIs(t, err, fsutil.ErrPublishCollision, "a vanish is indeterminate, not proven-foreign")
	require.Equal(t, "old", string(mustRead2(t, base, backup)))
	require.Empty(t, w26DirQuarNames(t, base, "/w36v"), "nothing relocated, nothing planted")
}

// w36QuarStatFailFs fails the Stat of every freshly-claimed quarantine
// reservation (the claim's post-create identity capture, F4's new leg).
type w36QuarStatFailFs struct {
	afero.Fs
	err error
}

type w36QuarStatFailFile struct {
	afero.File
	err error
}

func (f w36QuarStatFailFile) Stat() (os.FileInfo, error) { return nil, f.err }

func (f *w36QuarStatFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err == nil && flag&os.O_EXCL != 0 && strings.Contains(name, backupQuarantineSuffix) {
		return w36QuarStatFailFile{File: file, err: f.err}, nil
	}
	return file, err
}

// The claim's reservation-Stat wedge: the unknown-state placeholder is
// dropped and the claim fails closed before any move consideration.
func TestRemoveReplacementBackupW36_ReservationStatFailureRetainsPlaceholder(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w36t/poster.jpg.dlbak." + p3HexA
	w26WriteBackup(t, base, backup, "old")
	sentinel := errors.New("w36 quarantine reservation stat wedged")
	fs := &w36QuarStatFailFs{Fs: base, err: sentinel}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	err := quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w36 unit", nil, nil)
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "stat quarantine reservation")
	require.Equal(t, "old", string(mustRead2(t, base, backup)))
	require.Len(t, w26DirQuarNames(t, base, "/w36t"), 1,
		"wave-r19 (F3): the unproven placeholder is retained for manual cleanup — never unlinked when identity is unprovable")
	require.Contains(t, logs.String(), "left in place",
		"the retain-on-doubt leg warned that the placeholder's identity could not be proven")
}

// F3: the hold's move-back surfaces the classified failure bound to the
// UNOWNED journaled name, stays retryable, and completes once the name is
// free again.
func TestBackupQuarantineHoldW36_RestoreFailureClassifiedAndRetryable(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w36h/poster.jpg.dlbak." + p3HexA
	w26WriteBackup(t, base, backup, "old")
	hold, err := quarantineReplacementBackupForRemoval(base, backup, "w36 unit", nil, nil)
	require.NoError(t, err)

	// A foreign claimant takes the journaled name mid-hold.
	require.NoError(t, afero.WriteFile(base, backup, []byte("foreign claimant at the journaled name"), 0o644))
	err = hold.restore()
	require.Error(t, err)
	require.ErrorIs(t, err, errBackupQuarantineRestoreFailed,
		"the failed move-back rides the typed class callers route on")
	require.True(t, hold.moved, "a failed move-back keeps the compensation retryable")
	require.Equal(t, "foreign claimant at the journaled name", string(mustRead2(t, base, backup)),
		"the claimant is never clobbered (no-replace compensation)")
	names := w26DirQuarNames(t, base, "/w36h")
	require.Len(t, names, 1, "the verified bytes stay recoverable at the quarantine name")
	require.Equal(t, "old", string(mustRead2(t, base, "/w36h/"+names[0])))

	// Claimant gone: the retried compensation lands exactly once.
	require.NoError(t, base.Remove(backup))
	require.NoError(t, hold.restore())
	require.False(t, hold.moved)
	require.Equal(t, "old", string(mustRead2(t, base, backup)))
	require.Empty(t, w26DirQuarNames(t, base, "/w36h"))
}

// F3 routing: wrapped and joined restore-failure classes both resolve to the
// rearm-refused (journal-only) pending kind; plain and proven-foreign
// classes keep the clean marker.
func TestPendingKindForRemovalErrorW36(t *testing.T) {
	require.Equal(t, models.RestorePendingKindRearmRefused,
		pendingKindForRemovalError(fmt.Errorf("wedge: %w", errBackupQuarantineRestoreFailed)))
	require.Equal(t, models.RestorePendingKindRearmRefused,
		pendingKindForRemovalError(errors.Join(
			refuseReplacementBackupRemoval("b", "p", "proven foreign"),
			fmt.Errorf("%w", errBackupQuarantineRestoreFailed))))
	require.Equal(t, models.RestorePendingKindClean,
		pendingKindForRemovalError(errors.New("plain removal failure")))
}

// F3 internal leg: the unlink-time divergence whose move-back FAILS joins
// both classes into the removal error — the proven-foreign refusal rides
// unchanged, the unowned-name classification is visible, and the routed
// pending kind is journal-only.
func TestRemoveReplacementBackupW36_UnlinkRefusalJoinsFailedMoveBack(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w36j/poster.jpg.dlbak." + p3HexA
	w26WriteBackup(t, base, backup, "old")
	require.NoError(t, afero.WriteFile(base, "/w36j/foreign", []byte("a much longer foreign payload"), 0o644))
	foreignInfo, err := base.Stat("/w36j/foreign")
	require.NoError(t, err)

	fs := &w32QuarFs{Fs: base}
	fs.lstat = func(call int, name string) (os.FileInfo, error) {
		if call == 2 {
			// Unlink-time divergence AND a claimant at the journaled name:
			// the move-back compensation fails.
			require.NoError(t, afero.WriteFile(base, backup, []byte("foreign claimant"), 0o644))
			return foreignInfo, nil
		}
		return w32RestoreReadsReal(fs)(call, name)
	}

	err = quarantineAndRemoveVerifiedReplacementBackup(fs, backup, "w36 unit", nil, nil)
	require.Error(t, err)
	var refused *BackupRemovalRefusedError
	require.ErrorAs(t, err, &refused, "the proven-foreign refusal rides unchanged through the join")
	require.ErrorIs(t, err, errBackupQuarantineRestoreFailed, "the failed move-back joins the classified chain")
	require.Equal(t, models.RestorePendingKindRearmRefused, pendingKindForRemovalError(err),
		"the journaled name is unowned — the pending retry must run journal-only")
	require.Equal(t, "foreign claimant", string(mustRead2(t, base, backup)),
		"the claimant keeps the journaled name byte-intact")
	names := w26DirQuarNames(t, base, "/w36j")
	require.Len(t, names, 1, "the verified bytes stay recoverable at the quarantine name")
	require.Equal(t, "old", string(mustRead2(t, base, "/w36j/"+names[0])))
}
