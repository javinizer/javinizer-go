package downloader

// POSTER-WRITE-HARDENING wave-31 (codex local round 1, PR#215 findings
// L1/L2) — the confirm-failure rollback used to delete the backup and
// retract the journal entry after copyBackupToDest WITHOUT revalidating (L1)
// that the destination still names the object the rollback published, and
// (L2) that the backup still names the object the rollback streamed. A
// foreign writer using the publish→remove window had the blessed state
// diverge from reality in both directions.
//
// L1: copyBackupToDestBound hands back the post-publish-VERIFIED destination
// identity (fsutil.PublishStagedBoundInfo) and the confirm leg rechecks the
// destination against it (rollbackRestoredDestStillOurs — the wave-25
// installedDestIdentity discipline) before the backup removal (wave-32: the
// successor chain quarantineRollbackBackupForRemoval + hold.removeVerified)
// and ReleaseReplacement ever run. Mismatch/absence refuses: destination
// untouched, backup retained, entry left armed.
//
// L2: the rollback backup removal binds the unlink to the object the rollback
// COPIED (history's wave-25 copiedFrom shape — the validated no-follow
// Lstat object: dev/inode when exposed, then size + mtime). A foreign plant
// swapped onto the backup name is kept byte-intact and the removal refuses.
//
// The e2e refusal legs replay the publish→recheck/remove instant through the
// package seam (unreachable for a Filesystem double on the real OsFs — the
// wave-30 identity gate requires the native descriptor); the real-OsFs
// detection itself is pinned by direct unit tests.

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

// w31StubRestoredDestRecheck replaces the confirm-rollback's dest recheck
// for the duration of a test; hook replays the foreign writer's action
// inside the publish→recheck window.
func w31StubRestoredDestRecheck(t *testing.T, hook func(fsys afero.Fs, dest string, id installedDestIdentity) bool) {
	t.Helper()
	prev := rollbackRestoredDestStillOurs
	rollbackRestoredDestStillOurs = hook
	t.Cleanup(func() { rollbackRestoredDestStillOurs = prev })
}

// L1: the destination stops naming the just-restored object inside the
// publish→remove window — the rollback blesses nothing: destination kept
// byte-intact, backup retained in place, journal entry left armed, entry
// never released.
func TestInstallOverwritingW31_ConfirmRollbackDestSwapRefused(t *testing.T) {
	fs := afero.NewMemMapFs()
	old := []byte("old bytes on disk")
	foreign := []byte("a foreign writer claimed the destination right after the rollback publish")
	staged, dest := w25InstallFixture(t, fs, old)

	w31StubRestoredDestRecheck(t, func(fsys afero.Fs, target string, _ installedDestIdentity) bool {
		// Replay the foreign writer landing inside the publish→recheck
		// window; the detection answer replays the real gate's verdict.
		_ = fsys.Remove(target)
		_ = afero.WriteFile(fsys, target, foreign, 0o644)
		return false
	})

	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	recorder := &w25ConfirmRollbackLedger{
		armedTestLedger: &armedTestLedger{}, fs: fs, confirmErr: errors.New("w31 confirmation failed"),
	}

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w31-dest-swap", recorder: recorder,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "install-confirm failed")
	require.Contains(t, err.Error(), "no longer names the restored object")
	require.Contains(t, err.Error(), "journal entry stays armed")
	require.False(t, skipped)
	require.True(t, replaced)

	require.Equal(t, foreign, readW31(t, fs, dest), "the foreign destination is never overwritten or removed")

	records := recorder.get()
	require.Len(t, records, 1, "the journal entry is still armed")
	require.Equal(t, old, readW31(t, fs, records[0].backupPath), "the backup is retained in place")
	require.Zero(t, recorder.releaseCalls, "the armed entry is never released after the refused cleanup")
	require.Empty(t, recorder.getPendings(), "no restore-pending mark — the armed entry arbitates recovery")
}

// L2: the backup name is re-occupied by a foreign object between the
// rollback copy and the removal — the removal refuses, the plant keeps its
// bytes, the destination keeps the restored pre-existing bytes, and the
// journal entry stays armed.
func TestInstallOverwritingW31_ConfirmRollbackBackupSwapRefusesRemoval(t *testing.T) {
	fs := afero.NewMemMapFs()
	old := []byte("old bytes on disk")
	foreign := []byte("a foreign writer re-occupied the backup name — much longer payload")
	staged, dest := w25InstallFixture(t, fs, old)

	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	recorder := &w25ConfirmRollbackLedger{
		armedTestLedger: &armedTestLedger{}, fs: fs, confirmErr: errors.New("w31 confirmation failed"),
	}

	w31StubRestoredDestRecheck(t, func(fsys afero.Fs, target string, id installedDestIdentity) bool {
		// The destination recheck itself is faithful (dest untouched): replay
		// ONLY the backup-name swap landing inside the copy→remove window.
		records := recorder.get()
		require.Len(t, records, 1)
		_ = fsys.Remove(records[0].backupPath)
		_ = afero.WriteFile(fsys, records[0].backupPath, foreign, 0o644)
		return true
	})

	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w31-backup-swap", recorder: recorder,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "backup cleanup failed")
	require.Contains(t, err.Error(), "refused")

	require.Equal(t, old, readW31(t, fs, dest), "the rollback restore of the pre-existing bytes stands")

	records := recorder.get()
	require.Len(t, records, 1, "the journal entry was NOT released — it still names the backup")
	require.Equal(t, foreign, readW31(t, fs, records[0].backupPath),
		"the foreign occupant at the backup name was NEVER deleted")
	require.Zero(t, recorder.releaseCalls)
	require.Empty(t, recorder.getPendings(), "a refused backup removal keeps the plain armed posture")
}

func readW31(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return data
}

// Rollback backup removal unit legs (the wave-32 successor chain): every
// ownership binding.
func TestRemoveRollbackBackupW31_OwnershipLegs(t *testing.T) {
	setup := func(t *testing.T) (afero.Fs, string, os.FileInfo) {
		fs := afero.NewMemMapFs()
		backup := "/w31rm/poster.jpg.dlbak.0123"
		require.NoError(t, afero.WriteFile(fs, backup, []byte("copied bytes"), 0o644))
		info, err := lstatBackupCandidate(fs, backup)
		require.NoError(t, err)
		return fs, backup, info
	}

	t.Run("matching binding removes", func(t *testing.T) {
		fs, backup, copied := setup(t)
		require.NoError(t, quarantineAndRemoveVerifiedRollbackBackup(fs, backup, copied, "w31"))
		exists, _ := afero.Exists(fs, backup)
		require.False(t, exists)
	})

	t.Run("foreign occupant refused and preserved", func(t *testing.T) {
		fs, backup, copied := setup(t)
		require.NoError(t, fs.Remove(backup))
		require.NoError(t, afero.WriteFile(fs, backup, []byte("foreign occupant — different size"), 0o644))
		err := quarantineAndRemoveVerifiedRollbackBackup(fs, backup, copied, "w31")
		require.Error(t, err)
		require.Contains(t, err.Error(), "refused")
		require.Equal(t, []byte("foreign occupant — different size"), readW31(t, fs, backup))
	})

	t.Run("non-regular occupant refused and preserved", func(t *testing.T) {
		fs, backup, copied := setup(t)
		require.NoError(t, fs.Remove(backup))
		require.NoError(t, fs.Mkdir(backup, 0o755))
		err := quarantineAndRemoveVerifiedRollbackBackup(fs, backup, copied, "w31")
		require.Error(t, err)
		require.Contains(t, err.Error(), "refused")
		info, serr := fs.Stat(backup)
		require.NoError(t, serr)
		require.True(t, info.IsDir(), "the foreign directory is untouched")
	})

	t.Run("nil binding keeps the pre-wave-31 pathname posture", func(t *testing.T) {
		fs, backup, _ := setup(t)
		require.NoError(t, quarantineAndRemoveVerifiedRollbackBackup(fs, backup, nil, "w31"))
		exists, _ := afero.Exists(fs, backup)
		require.False(t, exists)
	})

	t.Run("already gone is removed", func(t *testing.T) {
		fs, backup, copied := setup(t)
		require.NoError(t, fs.Remove(backup))
		require.NoError(t, quarantineAndRemoveVerifiedRollbackBackup(fs, backup, copied, "w31"))
	})

	t.Run("indeterminate inspect keeps ownership", func(t *testing.T) {
		fs, backup, copied := setup(t)
		sentinel := errors.New("w31 stat wedged")
		err := quarantineAndRemoveVerifiedRollbackBackup(&w25StatErrFs{Fs: fs, victim: backup, err: sentinel}, backup, copied, "w31")
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, []byte("copied bytes"), readW31(t, fs, backup), "nothing was removed")
	})
}

// The dev/inode binding leg on a real filesystem: a SAME-SIZE foreign inode
// with the mtime re-stamped to the copied object's — only dev/inode tells it
// apart (rename-over of a pre-created file: CI filesystems reuse freed
// inodes on remove+create at one path).
func TestRemoveRollbackBackupW31_DevInoAndSymlinkLegs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inode/symlink assertions are POSIX-shaped; Windows coverage runs through the size/mtime legs")
	}
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak.abcd")
	require.NoError(t, os.WriteFile(backup, []byte("copied bytes, exact length"), 0o644))
	copied, err := lstatBackupCandidate(fs, backup)
	require.NoError(t, err)

	backupForeign := filepath.Join(dir, "foreign-backup-plant")
	require.NoError(t, os.WriteFile(backupForeign, []byte("foreign plant, exact length"), 0o644))
	foreignInfo, ferr := os.Lstat(backupForeign)
	require.NoError(t, ferr)
	require.NoError(t, os.Rename(backupForeign, backup))
	require.NoError(t, os.Chtimes(backup, copied.ModTime(), copied.ModTime()))

	err = quarantineAndRemoveVerifiedRollbackBackup(fs, backup, copied, "w31")
	require.Error(t, err)
	require.Contains(t, err.Error(), "refused")
	current, lerr := os.Lstat(backup)
	require.NoError(t, lerr)
	require.True(t, os.SameFile(current, foreignInfo), "the foreign occupant was never unlinked")

	// A symlink occupant is never the copied object — refused by mode, and
	// the link object itself stays intact.
	require.NoError(t, os.Remove(backup))
	require.NoError(t, os.Symlink(filepath.Join(dir, "nowhere"), backup))
	err = quarantineAndRemoveVerifiedRollbackBackup(fs, backup, copied, "w31")
	require.Error(t, err)
	require.Contains(t, err.Error(), "refused")
	linkInfo, lerr2 := os.Lstat(backup)
	require.NoError(t, lerr2)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink)
}

// copyBackupToDestBound on the real filesystem pins BOTH bindings to the
// objects the operation actually touched, and the default recheck verdicts
// follow the destination's real state.
func TestCopyBackupToDestBoundW31_OSFactsAndRecheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inode identity assertions are POSIX-shaped; Windows coverage runs through the size/mtime legs")
	}
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak.4321"
	require.NoError(t, os.WriteFile(dest, []byte("installed bytes"), 0o644))
	require.NoError(t, os.WriteFile(backup, []byte("pre-existing bytes"), 0o640))

	facts, err := copyBackupToDestBound(fs, backup, dest)
	require.NoError(t, err)
	require.NotNil(t, facts.copied)
	require.True(t, facts.restored.known)

	// copied pins the backup object itself.
	backupInfo, berr := os.Lstat(backup)
	require.NoError(t, berr)
	require.True(t, os.SameFile(backupInfo, facts.copied), "copied binds the object the rollback streamed")

	// restored pins the object the publish landed at the destination (the
	// staged copy's own inode — not the backup's).
	destInfo, derr := os.Lstat(dest)
	require.NoError(t, derr)
	cDev, cIno, cOK := restoreSourceIdentity(facts.copied)
	dDev, dIno, dOK := restoreSourceIdentity(destInfo)
	require.True(t, cOK && dOK)
	require.False(t, cDev == dDev && cIno == dIno, "the restored object is the staged copy, not the backup")

	// The default recheck (the production seam wiring) on real states.
	require.True(t, rollbackRestoredDestStillOurs(fs, dest, facts.restored), "untouched dest verifies")

	foreign := filepath.Join(dir, "foreign-plant.jpg")
	require.NoError(t, os.WriteFile(foreign, []byte("pre-existing bytes"), 0o644)) // same size; mtime restamped below
	require.NoError(t, os.Rename(foreign, dest))
	require.NoError(t, os.Chtimes(dest, destInfo.ModTime(), facts.restored.modTime))
	require.False(t, rollbackRestoredDestStillOurs(fs, dest, facts.restored),
		"a same-size re-mtimed foreign inode mismatches via dev/inode")

	facts2, err := copyBackupToDestBound(fs, backup, dest)
	require.NoError(t, err)
	require.NoError(t, os.Remove(dest))
	require.False(t, rollbackRestoredDestStillOurs(fs, dest, facts2.restored), "a vanished dest never verifies")

	// And the pre-wave-31 documented residual: an unknown identity (virtual
	// legs) skips the check rather than refusing on nothing.
	require.True(t, rollbackRestoredDestStillOurs(fs, dest, installedDestIdentity{}))
}

// The L1/L2 flows through installOverwriting on the REAL OsFs with no
// foreign interference: publish identity captured, recheck verified, backup
// binding matched — the normal confirm-failure rollback is byte-identical to
// the pre-wave-31 discipline.
func TestInstallOverwritingW31_ConfirmRollbackOsFsNormalLegUnchanged(t *testing.T) {
	base := afero.NewOsFs()
	tmp := t.TempDir()
	dest := filepath.Join(tmp, strings.ReplaceAll(t.Name(), "/", "_")+".jpg")
	staged := filepath.Join(tmp, ".staged-w31")
	require.NoError(t, os.WriteFile(staged, []byte("new bytes from cdn"), 0o644))
	require.NoError(t, os.WriteFile(dest, []byte("old poster bytes here."), 0o644))

	d := NewDownloader(nil, base, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	recorder := &w25ConfirmRollbackLedger{
		armedTestLedger: &armedTestLedger{}, fs: base, confirmErr: errors.New("w31 confirmation failed"),
	}

	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w31-os-normal", recorder: recorder,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "install-confirm failed, rolled back to pre-existing bytes")
	require.Equal(t, []byte("old poster bytes here."), readW31(t, base, dest), "the rollback restores exactly")
	require.True(t, recorder.releaseSawNoBackup, "the backup removal completed before the release")
	require.Empty(t, recorder.get(), "the journal entry was released")
}
