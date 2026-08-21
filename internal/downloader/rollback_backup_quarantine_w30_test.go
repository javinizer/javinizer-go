package downloader

// POSTER-WRITE-HARDENING wave-30 follow-on (codex P2, PR#215 findings
// F1+F2) — the downloader mirror of history's wave-36 quarantine hardening
// (replacement_backup_quarantine_w36_test.go):
//
//   - F1: claimRollbackQuarantineName O_EXCL-claimed the .dlq. name, closed
//     the handle, and the replace-aware quarantining rename displaced
//     whatever occupied the name at THAT instant — a foreign writer
//     renaming the reservation away and planting its own bytes got them
//     silently destroyed by OUR move. The claim now returns the
//     reservation's captured identity and the move re-derives it first
//     (rollbackQuarantineReservationStillOurs): any divergence is the typed
//     collision class and behaves exactly like the claim-failure leg
//     (journal entry live, foreign bytes intact).
//   - F2: the wedge move-back only LOGGED its failure and cleared moved —
//     callers left the entry armed against a journaled name now proven
//     UNOWNED while the restored bytes sat at the .dlq. name. restore()
//     returns the classified failure (errRollbackQuarantineRestoreFailed),
//     internal wedge legs JOIN it into the removal error chain, and the
//     install-confirm rollback caller persists the rearm-refused
//     (journal-only) pending kind via MarkReplacementRestorePendingKind.
//
// Matrix: reservation swap (metadata leg, MemMap — and a deterministic
// dev/inode leg on OS files), reservation vanish, the claim's stat-failure
// leg, the hold restore classification + retry contract, and the two
// pipeline legs (divergence-with-failed-move-back marker persist +
// marker-failure both-cause log, and the joined unlink-failure routing).

import (
	"bytes"
	"context"
	"errors"
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

// w30SwapOnCloseFile replays the foreign writer landing inside the
// claim→move handoff: the very instant the reservation handle closes (the
// claim's last act before the caller's pre-move verify), the swap hook runs
// against the reserved .dlq. name.
type w30SwapOnCloseFile struct {
	afero.File
	name    string
	onClose func(name string)
}

func (f w30SwapOnCloseFile) Close() error {
	err := f.File.Close()
	if err == nil && f.onClose != nil {
		f.onClose(f.name)
	}
	return err
}

// w30ReservationSwapFs wraps the FIRST quarantine-reservation create with
// the swap-on-close file — the claim→verify handoff replay (finding F1).
type w30ReservationSwapFs struct {
	afero.Fs
	swap  func(name string)
	fired bool
}

func (f *w30ReservationSwapFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err == nil && f.swap != nil && !f.fired && flag&os.O_EXCL != 0 && strings.Contains(name, rollbackQuarantineSuffix) {
		f.fired = true
		return w30SwapOnCloseFile{File: file, name: name, onClose: f.swap}, nil
	}
	return file, err
}

// F1 headline, metadata leg (every platform): the reservation placeholder is
// swapped for a real foreign file between the claim and the quarantine move.
// The move is refused with the typed collision class — nothing relocates,
// the foreign occupant keeps its bytes, and the journaled backup is intact.
func TestRollbackBackupQuarantineW30_ForeignReservationSwapRefused(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w30s/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	fs := &w30ReservationSwapFs{Fs: base, swap: func(name string) {
		require.NoError(t, base.Remove(name))
		require.NoError(t, afero.WriteFile(base, name, []byte("foreign reservation occupant"), 0o600))
	}}

	err := quarantineAndRemoveVerifiedRollbackBackup(fs, backup, nil, "w30 unit")
	require.Error(t, err)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision,
		"a reservation proven foreign is the typed collision class")
	require.Contains(t, err.Error(), "no longer names the claimed empty placeholder")
	require.True(t, fs.fired)
	require.Equal(t, "old", string(readW31(t, base, backup)),
		"the journaled backup never moved — the refusal pre-empts the rename")
	names := w32RollbackQuarNames(t, base, "/w30s")
	require.Len(t, names, 1, "the foreign occupant keeps the .dlq. name byte-intact")
	require.Equal(t, "foreign reservation occupant", string(readW31(t, base, "/w30s/"+names[0])))
}

// F1, dev/inode leg on real files (POSIX): a same-size (0-byte), same-mtime
// substitute at the reservation name — two simultaneously-existing files
// guarantee the inode comparison cannot collapse.
func TestRollbackBackupQuarantineW30_ReservationDevInodeMismatchRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dev/inode identity is POSIX-shaped")
	}
	base := afero.NewOsFs()
	tmp := t.TempDir()
	backup := filepath.Join(tmp, "poster.jpg.dlbak.abcd")
	require.NoError(t, os.WriteFile(backup, []byte("old"), 0o600))
	foreign := filepath.Join(tmp, "foreign-placeholder")
	require.NoError(t, os.WriteFile(foreign, nil, 0o600))

	fs := &w30ReservationSwapFs{Fs: base, swap: func(name string) {
		claimInfo, lerr := os.Lstat(name)
		require.NoError(t, lerr)
		require.NoError(t, os.Chtimes(foreign, claimInfo.ModTime(), claimInfo.ModTime()))
		require.NoError(t, os.Remove(name))
		require.NoError(t, os.Rename(foreign, name))
	}}

	err := quarantineAndRemoveVerifiedRollbackBackup(fs, backup, nil, "w30 unit")
	require.True(t, fs.fired)
	require.Error(t, err)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision)
	require.Contains(t, err.Error(), "dev/inode mismatch")
	require.Equal(t, "old", string(readW31(t, base, backup)))
	entries, derr := os.ReadDir(tmp)
	require.NoError(t, derr)
	require.Len(t, entries, 2, "the refused occupant stays at its displaced name; nothing else moved")
}

// F1, vanish leg: the reservation name was deleted inside the handoff — an
// indeterminate plain error (never a silent move into a watchable gap).
func TestRollbackBackupQuarantineW30_ReservationVanishRefuses(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w30v/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	fs := &w30ReservationSwapFs{Fs: base, swap: func(name string) {
		require.NoError(t, base.Remove(name))
	}}

	err := quarantineAndRemoveVerifiedRollbackBackup(fs, backup, nil, "w30 unit")
	require.Error(t, err)
	require.Contains(t, err.Error(), "inspect quarantine reservation")
	require.NotErrorIs(t, err, fsutil.ErrPublishCollision, "a vanish is indeterminate, not proven-foreign")
	require.Equal(t, "old", string(readW31(t, base, backup)))
	require.Empty(t, w32RollbackQuarNames(t, base, "/w30v"), "nothing relocated, nothing planted")
}

// w30QuarStatFailFs fails the Stat of every freshly-claimed quarantine
// reservation (the claim's post-create identity capture, F1's new leg).
type w30QuarStatFailFs struct {
	afero.Fs
	err error
}

type w30QuarStatFailFile struct {
	afero.File
	err error
}

func (f w30QuarStatFailFile) Stat() (os.FileInfo, error) { return nil, f.err }

func (f *w30QuarStatFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err == nil && flag&os.O_EXCL != 0 && strings.Contains(name, rollbackQuarantineSuffix) {
		return w30QuarStatFailFile{File: file, err: f.err}, nil
	}
	return file, err
}

// The claim's reservation-Stat wedge: the unknown-state placeholder is
// RETAINED (wave-65, finding F2 — mirroring claimOverwriteBackupPath's
// wave-62 rule): the name's identity is UNPROVEN, so unlinking it could
// delete foreign bytes another writer rotated onto it. The claim fails
// closed before any move consideration; the placeholder stays claimed and
// visible for manual cleanup.
func TestRollbackBackupQuarantineW30_ReservationStatFailureRetainsPlaceholder(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w30t/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	sentinel := errors.New("w30 quarantine reservation stat wedged")
	fs := &w30QuarStatFailFs{Fs: base, err: sentinel}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	err := quarantineAndRemoveVerifiedRollbackBackup(fs, backup, nil, "w30 unit")
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "stat quarantine reservation")
	require.Equal(t, "old", string(readW31(t, base, backup)))
	require.Len(t, w32RollbackQuarNames(t, base, "/w30t"), 1, "the unproven placeholder is retained for manual cleanup — never unlinked on doubt")
	require.Contains(t, logs.String(), "left in place", "the retained placeholder is warn-logged for manual cleanup")
}

// F2: the hold's move-back surfaces the classified failure bound to the
// UNOWNED journaled name, stays retryable, and completes once the name is
// free again.
func TestRollbackBackupQuarantineW30_RestoreFailureClassifiedAndRetryable(t *testing.T) {
	base := afero.NewMemMapFs()
	const backup = "/w30h/poster.jpg.dlbak.abcd"
	w32RollbackBackup(t, base, backup, "old")
	hold, err := quarantineRollbackBackupForRemoval(base, backup, nil, "w30 unit")
	require.NoError(t, err)

	// A foreign claimant takes the journaled name mid-hold.
	require.NoError(t, afero.WriteFile(base, backup, []byte("foreign claimant at the journaled name"), 0o644))
	err = hold.restore()
	require.Error(t, err)
	require.ErrorIs(t, err, errRollbackQuarantineRestoreFailed,
		"the failed move-back rides the typed class callers route on")
	require.True(t, hold.moved, "a failed move-back keeps the compensation retryable")
	require.Equal(t, "foreign claimant at the journaled name", string(readW31(t, base, backup)),
		"the claimant is never clobbered (no-replace compensation)")
	names := w32RollbackQuarNames(t, base, "/w30h")
	require.Len(t, names, 1, "the verified bytes stay recoverable at the quarantine name")
	require.Equal(t, "old", string(readW31(t, base, "/w30h/"+names[0])))

	// Claimant gone: the retried compensation lands exactly once.
	require.NoError(t, base.Remove(backup))
	require.NoError(t, hold.restore())
	require.False(t, hold.moved)
	require.Equal(t, "old", string(readW31(t, base, backup)))
	require.Empty(t, w32RollbackQuarNames(t, base, "/w30h"))
}

// F2 pipeline, divergence leg: the destination re-gate diverges AFTER
// quarantine AND the move-back is refused by a foreign claimant at the
// journaled name — the entry is durably marked restore-pending
// (rearm-refused), the failure surfaces upward, the claimant and the
// quarantined bytes both stay recoverable, and the entry is never released.
func TestInstallOverwritingW30_ConfirmRollbackDivergenceMoveBackFailureMarksRearmRefused(t *testing.T) {
	fs := afero.NewMemMapFs()
	old := []byte("old bytes on disk")
	staged, dest := w25InstallFixture(t, fs, old)

	recorder := &w25ConfirmRollbackLedger{
		armedTestLedger: &armedTestLedger{}, fs: fs, confirmErr: errors.New("w30 confirmation failed"),
	}
	claimant := []byte("foreign claimant at the journaled name")
	calls := 0
	w31StubRestoredDestRecheck(t, func(fsys afero.Fs, _ string, _ installedDestIdentity) bool {
		calls++
		if calls == 2 {
			// The claimant takes the journaled backup name inside the
			// quarantine handoff — the NO-REPLACE move-back is refused.
			require.NoError(t, afero.WriteFile(fsys, recorder.get()[0].backupPath, claimant, 0o644))
		}
		return calls == 1 // the pre-removal recheck passes; the post-quarantine re-gate fails
	})

	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w30-divergence-moveback", recorder: recorder,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "verified move-back failed")
	require.Contains(t, err.Error(), "rearm-refused")
	require.True(t, replaced)
	require.False(t, skipped)
	require.Equal(t, 2, calls)

	records := recorder.get()
	require.Len(t, records, 1, "the journal entry was NOT released")
	require.Zero(t, recorder.releaseCalls)
	pendings := recorder.getPendings()
	require.Len(t, pendings, 1, "the entry left the armed state as restore-pending")
	require.Equal(t, models.RestorePendingKindRearmRefused, pendings[0].kind,
		"the journaled name is unowned — the pending retry must run journal-only")
	require.Equal(t, records[0].backupPath, pendings[0].backupPath)
	require.Equal(t, claimant, readW31(t, fs, records[0].backupPath),
		"the claimant keeps the journaled name byte-intact")
	require.Equal(t, old, readW31(t, fs, dest), "the restored destination is untouched")
	quar := w32RollbackQuarNames(t, fs, filepath.Dir(dest))
	require.Len(t, quar, 1, "the restored bytes stay recoverable at the quarantine name")
	require.Equal(t, old, readW31(t, fs, filepath.Join(filepath.Dir(dest), quar[0])))
}

// w30MarkFailLedger fails the restore-pending mark on top of the confirm
// failure: the both-cause log leg of markRollbackQuarantineRestoreFailed.
type w30MarkFailLedger struct {
	*w25ConfirmRollbackLedger
	markErr error
}

func (l *w30MarkFailLedger) MarkReplacementRestorePendingKind(context.Context, string, string, string, string) error {
	return l.markErr
}

// F2 pipeline, both-cause log: the move-back failure AND the rearm-refused
// marker persistence both fail — the compound message and the warn log name
// both failures, the entry stays armed (last-resort), and the quarantined
// bytes stay recoverable.
func TestInstallOverwritingW30_MoveBackFailureMarkFailureLogsBothCauses(t *testing.T) {
	logs := w16CaptureLogging(t)
	fs := afero.NewMemMapFs()
	old := []byte("old bytes on disk")
	staged, dest := w25InstallFixture(t, fs, old)

	markErr := errors.New("w30 marker store wedged")
	recorder := &w30MarkFailLedger{
		w25ConfirmRollbackLedger: &w25ConfirmRollbackLedger{
			armedTestLedger: &armedTestLedger{}, fs: fs, confirmErr: errors.New("w30 confirmation failed"),
		},
		markErr: markErr,
	}
	calls := 0
	w31StubRestoredDestRecheck(t, func(fsys afero.Fs, _ string, _ installedDestIdentity) bool {
		calls++
		if calls == 2 {
			require.NoError(t, afero.WriteFile(fsys, recorder.get()[0].backupPath, []byte("foreign claimant"), 0o644))
		}
		return calls == 1
	})

	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w30-moveback-markfail", recorder: recorder,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "verified move-back failed")

	require.Len(t, recorder.get(), 1, "without a durable marker the entry keeps its armed posture")
	require.Empty(t, recorder.getPendings())
	require.Zero(t, recorder.releaseCalls)
	require.Contains(t, logs.String(), "rearm-refused restore-pending marking failed",
		"the both-cause warn names the marker outage")
	require.Contains(t, logs.String(), markErr.Error(), "the warn carries the marker failure")
	require.Len(t, w32RollbackQuarNames(t, fs, filepath.Dir(dest)), 1,
		"the restored bytes stay recoverable at the quarantine name")
}

// w30UnlinkFailClaimFs wedges the quarantine unlink itself AND replays a
// foreign claimant taking the vacated journaled backup name — the removeVerified
// Remove-failure leg whose move-back compensation is then refused. The flow
// issues THREE .dlq. Removes — the backup handoff's take-aside scratch first
// (pre-journal, wave-38), then the rollback handoff's take-aside placeholder
// (wave-42), then the rollback's quarantine unlink — and only the LAST may be
// wedged (wedging a placeholder's warn-only release would clobber the
// just-moved set-aside with the claimant mid-test). The wedge therefore keys
// on the unlink of a NON-EMPTY .dlq. object: the quarantined verified backup.
type w30UnlinkFailClaimFs struct {
	afero.Fs
	err      error
	claimant []byte
}

func (f *w30UnlinkFailClaimFs) Remove(name string) error {
	if strings.Contains(name, rollbackQuarantineSuffix) {
		if info, serr := f.Fs.Stat(name); serr == nil && info.Size() > 0 {
			backup := strings.SplitN(name, rollbackQuarantineSuffix, 2)[0]
			_ = afero.WriteFile(f.Fs, backup, f.claimant, 0o644)
			return f.err
		}
	}
	return f.Fs.Remove(name)
}

// F2 pipeline, joined routing leg: the quarantine unlink fails AND the
// move-back is refused — the joined error carries the restore-failed class,
// so the caller's rmErr leg persists the rearm-refused pending kind instead
// of its plain "entry stays armed" posture.
func TestInstallOverwritingW30_UnlinkFailureWithRefusedMoveBackMarksRearmRefused(t *testing.T) {
	base := afero.NewMemMapFs()
	claimant := []byte("foreign claimant at the journaled name")
	fs := &w30UnlinkFailClaimFs{Fs: base, err: errors.New("w30 quarantine unlink wedged"), claimant: claimant}
	old := []byte("old bytes on disk")
	staged, dest := w25InstallFixture(t, fs, old)

	recorder := &w25ConfirmRollbackLedger{
		armedTestLedger: &armedTestLedger{}, fs: fs, confirmErr: errors.New("w30 confirmation failed"),
	}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w30-unlink-moveback", recorder: recorder,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, errRollbackQuarantineRestoreFailed,
		"the joined move-back classification surfaces through the pipeline error")
	require.Contains(t, err.Error(), "backup cleanup failed")
	require.Contains(t, err.Error(), "rearm-refused")

	records := recorder.get()
	require.Len(t, records, 1, "the journal entry was NOT released")
	require.Zero(t, recorder.releaseCalls)
	pendings := recorder.getPendings()
	require.Len(t, pendings, 1)
	require.Equal(t, models.RestorePendingKindRearmRefused, pendings[0].kind)
	require.Equal(t, claimant, readW31(t, base, records[0].backupPath),
		"the claimant keeps the journaled name byte-intact")
	require.Equal(t, old, readW31(t, base, dest), "the restored destination is untouched")
	quar := w32RollbackQuarNames(t, base, filepath.Dir(dest))
	require.Len(t, quar, 1, "the restored bytes stay recoverable at the quarantine name")
	require.Equal(t, old, readW31(t, base, filepath.Join(filepath.Dir(dest), quar[0])))
}
