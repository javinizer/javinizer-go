package downloader

// POSTER-WRITE-HARDENING codex PR#215 wave-26 (P2) — bind the confirm
// rollback to the PUBLISHED inode. Wave-25 captured the rollback baseline by
// Lstat-ing the destination right AFTER ReplaceFile's publish: a writer
// swapping the destination inside that publish→capture window had its file
// recorded as the baseline, and the confirm-failure recheck then ACCEPTED
// the foreign inode, letting copyBackupToDest overwrite it. The baseline is
// now captured from the STAGED file BEFORE the publish (rename makes the
// destination hold the staged inode, size and mtime intact), the destination
// is verified against it once right after the publish (a mismatch routes
// through the publish-failure rollback — foreign occupant preserved), and
// the confirm-failure recheck compares against THAT post-publish-verified
// baseline, never a fresh capture.

import (
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
)

// w26RenameSwapFs interposes the staged install publish (rename FROM the
// staged ".staged*" name onto the destination): after the staged object
// lands, swap() replays a foreign writer acting inside the publish→capture
// window.
type w26RenameSwapFs struct {
	afero.Fs
	dest  string
	swap  func()
	fired bool
}

func (f *w26RenameSwapFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && newname == f.dest && strings.HasPrefix(filepath.Base(oldname), ".staged") {
		f.fired = true
		if f.swap != nil {
			f.swap()
		}
	}
	return err
}

// A swap between the publish and (wave-25's) capture point: the foreign
// occupant must NOT be appointed the rollback baseline — the publish result
// is the post-publish identity break, routed through the rollback restore,
// which REFUSES over the occupied destination. Foreign bytes stay intact,
// the backup is retained, the journal entry stays armed.
func TestInstallOverwritingW26_PostPublishOccupantSwapRefusedRollback(t *testing.T) {
	base := afero.NewMemMapFs()
	old := []byte("old bytes on disk")
	staged, dest := w25InstallFixture(t, base, old)
	foreign := []byte("a foreign writer's bytes — created right after the publish, longer than the install payload")
	fs := &w26RenameSwapFs{Fs: base, dest: dest, swap: func() {
		_ = base.Remove(dest)
		_ = afero.WriteFile(base, dest, foreign, 0o644)
	}}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	ledger := &w25Ledger{}

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w26-postpublish-swap", recorder: ledger})
	require.Error(t, err)
	require.ErrorIs(t, err, fsutil.ErrPublishStagedIdentityBreak,
		"the post-publish identity break sentinel stays unwrap-reachable")
	require.Contains(t, err.Error(), "post-publish identity break")
	require.Contains(t, err.Error(), "failed to replace",
		"the break routes through the publish-failure rollback leg")
	require.Contains(t, err.Error(), "rollback restore refused",
		"the no-replace rollback restore never clobbers the foreign occupant")
	require.False(t, skipped)
	require.True(t, replaced)
	require.True(t, fs.fired, "the injected post-publish swap ran")

	got, rerr := afero.ReadFile(base, dest)
	require.NoError(t, rerr)
	require.Equal(t, foreign, got, "the post-publish foreign occupant is never overwritten or removed")

	rec := ledger.firstRecord()
	backup, berr := afero.ReadFile(base, rec.backup)
	require.NoError(t, berr, "the backup is retained in place for sweep/revert arbitration")
	require.Equal(t, old, backup)
	require.Empty(t, ledger.released, "the armed entry is never released after the refused install")
	require.Zero(t, ledger.confirmed, "the refusal wedges before the journal confirm")
	require.Zero(t, ledger.pendings, "no restore-pending mark — the armed entry plus retained backup already arbitrate")
}

// A staged-baseline capture that cannot read the staged file fails CLOSED to
// the wave-25 unknown-identity posture: the post-publish verification is
// skipped (nothing to verify against) and the published install stands — a
// good publish must never be repurposed into a rollback on an observability
// gap.
type w26StagedStatFailFs struct {
	afero.Fs
	victim string
	err    error
}

func (f *w26StagedStatFailFs) Stat(name string) (os.FileInfo, error) {
	if name == f.victim {
		return nil, f.err
	}
	return f.Fs.Stat(name)
}

func TestInstallOverwritingW26_StagedBaselineUncapturableKeepsPublishedInstall(t *testing.T) {
	base := afero.NewMemMapFs()
	staged, dest := w25InstallFixture(t, base, []byte("old bytes on disk"))
	sentinel := errors.New("staged identity capture unavailable")
	fs := &w26StagedStatFailFs{Fs: base, victim: staged, err: sentinel}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	ledger := &w25Ledger{}

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w26-uncapturable-baseline", recorder: ledger})
	require.NoError(t, err,
		"an uncapturable staged baseline never turns a good publish into a rollback")
	require.False(t, skipped)
	require.True(t, replaced)
	got, rerr := afero.ReadFile(base, dest)
	require.NoError(t, rerr)
	require.Equal(t, []byte("new bytes from cdn"), got, "the published install stands")
	require.Equal(t, 1, ledger.confirmed, "the journal confirm runs normally")
	rec := ledger.firstRecord()
	require.NotEmpty(t, rec.backup, "the armed backup keeps its usual retention")
}

// The confirm-failure rollback with the destination UNTOUCHED since the
// publish: the staged-captured baseline verifies (post-publish check AND the
// rollback recheck) and the pre-existing bytes restore exactly like the
// wave-25 normal path.
func TestInstallOverwritingW26_ConfirmRollbackRestoresVerifiedBaseline(t *testing.T) {
	base := afero.NewMemMapFs()
	old := []byte("old bytes on disk")
	staged, dest := w25InstallFixture(t, base, old)
	d := NewDownloader(nil, base, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	ledger := &w25Ledger{confirmErr: errors.New("journal store wedged")}

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w26-verified-rollback", recorder: ledger})
	require.Error(t, err)
	require.Contains(t, err.Error(), "install-confirm failed, rolled back to pre-existing bytes")

	got, rerr := afero.ReadFile(base, dest)
	require.NoError(t, rerr)
	require.Equal(t, old, got, "the verified baseline licenses the same rollback restore as wave-25")
	rec := ledger.firstRecord()
	exists, _ := afero.Exists(base, rec.backup)
	require.False(t, exists, "the consumed backup went with the retract")
	require.Len(t, ledger.released, 1, "the journal entry retracted after the clean rollback")
}

// The dev/inode leg on a real filesystem: the post-publish swap plants a
// SAME-BYTES foreign inode (matching size, matching mtime re-stamped) — only
// the staged inode identity can tell it apart. POSIX-shaped; Windows coverage
// runs through the size/mtime legs.
func TestInstallOverwritingW26_PostPublishSwapSameSizeCaughtByDevIno(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inode identity assertions are POSIX-shaped; Windows coverage runs through the size/mtime legs")
	}
	base := afero.NewOsFs()
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "poster.jpg")
	staged := filepath.Join(tmp, ".staged-download")
	payload := []byte("new bytes, same length!")
	require.NoError(t, os.WriteFile(staged, payload, 0o644))
	require.NoError(t, os.WriteFile(dest, []byte("old poster bytes here."), 0o644))
	// A pre-built foreign object with identical bytes: rename-over guarantees
	// a distinct foreign inode (CI filesystems reuse freed inodes on
	// remove+create at the same path).
	foreign := filepath.Join(tmp, "foreign-plant.jpg")
	require.NoError(t, os.WriteFile(foreign, payload, 0o644))
	foreignInfo, ferr := os.Lstat(foreign)
	require.NoError(t, ferr)

	fs := &w26RenameSwapFs{Fs: base, dest: dest, swap: func() {
		info, lerr := os.Lstat(dest)
		require.NoError(t, lerr)
		require.NoError(t, os.Rename(foreign, dest),
			"the foreign writer replaces the destination right after the publish")
		require.NoError(t, os.Chtimes(dest, info.ModTime(), info.ModTime()),
			"even a matching mtime must not rescue the swap")
	}}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	ledger := &w25Ledger{}

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w26-postpublish-devino", recorder: ledger})
	require.Error(t, err)
	require.Contains(t, err.Error(), "post-publish identity break",
		"a same-bytes same-mtime foreign inode mismatches the staged baseline via dev/inode")
	require.True(t, fs.fired)

	curInfo, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	require.True(t, os.SameFile(curInfo, foreignInfo),
		"the foreign inode still occupies the destination — never displaced or overwritten")

	rec := ledger.firstRecord()
	backup, berr := os.ReadFile(rec.backup)
	require.NoError(t, berr, "the backup is retained for sweep/revert arbitration")
	require.Equal(t, []byte("old poster bytes here."), backup)
	require.Empty(t, ledger.released)
}
