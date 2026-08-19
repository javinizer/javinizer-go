package history

// POSTER-WRITE-HARDENING codex PR#215 w12 (P2): the explicit revert flow
// classified the pre-restore destination with afero.Exists — a Stat that
// FOLLOWS a dangling symlink and reports it absent. With a dangling symlink
// at dest, destMissingBeforeRestore came out true; if the restore then
// succeeded but backup removal AND RestorePending persistence both failed,
// the compensation branch deleted the restored destination even though a
// directory entry predated the restore — violating presence-aware retention.
// The classifier is now Lstat-first, exactly the wave-11 sweep discipline
// (restoreAndConsume / sweepOne): any Lstat-success object is present, a
// genuine Lstat ENOENT is missing, any other error is conservatively present.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// The presence-aware retention demand, stated precisely: when retention
// bookkeeping (backup removal + RestorePending persistence) BOTH fail after a
// successful restore, compensation may undo the restore ONLY when the
// destination was proven ABSENT before the copy — deleting it then reproduces
// exactly that proven-absent state. If ANY directory entry pre-existed — here
// a dangling symlink, which the old Stat-only classification misread as
// absent — the compensation must not vacuum the destination: the restored
// bytes are the state the armed journal can retry from, and deleting them
// would leave a hole neither the (already superseded) entry nor the restore
// can reconstruct from scratch.
//
// What happens to the pre-existing symlink itself: the EXPLICIT revert owns
// this destination through the journal, so its restore replaces the directory
// entry in place — POSIX rename (and Windows MoveFileEx-REPLACE_EXISTING)
// supersede the link object wholesale and NEVER write through it. Contrast
// the wave-11 sweep leg, an unattended heuristic that RETAINS rather than
// touch a link-classified destination. The "pre-existing entry must not be
// destroyed" demand therefore binds the COMPENSATION, not the restore the
// operator asked for: the entry's successor — the restored regular file now
// answering at that path — is the protected post-state, and this test asserts
// exactly that. (A bare "the symlink itself still exists" assertion would
// misdescribe the revert contract: the restore legitimately superseded it.)
func TestReverterRestorePresenceW12_DanglingSymlinkDestCompensationRetainsRestore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privilege on Windows")
	}
	root := t.TempDir()
	osfs := afero.NewOsFs()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA
	linkTarget := filepath.Join(root, "purged-original.bin") // never created — the link dangles
	require.NoError(t, os.Symlink(linkTarget, dest))
	require.NoError(t, os.WriteFile(backup, []byte("pre-replace-bytes"), 0o640))

	// Pin the exact misclassification this fix removes: Stat follows the
	// dangling link and reports the destination absent.
	presentViaStat, existsErr := afero.Exists(osfs, dest)
	require.NoError(t, existsErr)
	require.False(t, presentViaStat, "pre-fix classifier read: Stat follows the dangling link to ENOENT")

	repo := newP3OpRepo()
	op := w27CreateArmedReplacementRow(t, repo, dest, backup)
	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Installed = true
	row.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), row))
	op.GeneratedFiles = row.GeneratedFiles

	removeErr := errors.New("backup remove wedged")
	// The wrapper MUST forward the Lstater seam (production r.fs is afero.OsFs):
	// an fs that cannot Lstat falls back to Stat in lstatRestoreSource and
	// re-introduces exactly the link-following misclassification under test.
	fs := &w12PresenceFs{Fs: osfs, ls: osfs.(afero.Lstater), backup: backup, backupErr: removeErr, failBackup: true}
	markerErr := errors.New("marker update transient")
	failingRepo := &failingUpdateRepo{p3OpRepo: repo, updateErr: markerErr}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	t.Cleanup(restoreLog)

	ctx := context.Background()
	restored, err := NewReverter(fs, failingRepo).restoreReplacementJournal(ctx, op)
	require.ErrorIs(t, err, removeErr)
	require.True(t, restored[dest])
	require.Contains(t, logs.String(), "restored destination retained for retry",
		"the retention leg, not the undo leg, must handle a destination whose entry predated the restore")

	destInfo, lerr := os.Lstat(dest)
	require.NoError(t, lerr, "compensation must not vacuum a destination whose entry predated the restore")
	require.True(t, destInfo.Mode().IsRegular(), "the restore landed the backup bytes as a regular file")
	require.Equal(t, "pre-replace-bytes", string(mustRead2(t, osfs, dest)),
		"restored content survives failed retention bookkeeping — presence-aware retention")
	_, rlErr := os.Readlink(dest)
	require.Error(t, rlErr, "the operator-driven restore superseded the link object wholesale")
	_, terr := os.Stat(linkTarget)
	require.ErrorIs(t, terr, os.ErrNotExist, "the restore replaced the link — it never wrote THROUGH it")
	require.Equal(t, "pre-replace-bytes", string(mustRead2(t, osfs, backup)),
		"the failed backup removal keeps the backup bytes in place for retry")

	row, err = repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "the failed cleanup leaves the entry armed")
	require.True(t, gf.Replacements[0].Installed)
	require.False(t, gf.Replacements[0].RestorePending, "the failed marker update cannot set RestorePending")
}

// w12PresenceFs injects the two failures the compensation matrix needs while
// preserving the afero.Lstater capability of its base — the seam the
// pre-restore classifier depends on (production r.fs is afero.OsFs, itself an
// Lstater). Optional lstatErr/lstatDest force the classification of one path
// into a non-ENOENT error, proving the indeterminate arm: an undecidable
// destination is conservatively PRESENT, never a license to vacuum restored
// bytes as compensation.
type w12PresenceFs struct {
	afero.Fs
	ls         afero.Lstater
	backup     string
	backupErr  error
	failBackup bool
	lstatDest  string
	lstatErr   error
}

func (f *w12PresenceFs) Remove(name string) error {
	// Wave-26: the removal gate unlinks the quarantine sibling (backup +
	// ".dlq." + token), never the journaled pathname; wedge both spellings.
	// The failed-unlink compensation moves the quarantined object back, so
	// "backup retained" assertions still observe the journaled name.
	if f.failBackup && filepath.Clean(name) == filepath.Clean(f.backup) ||
		f.failBackup && strings.HasPrefix(filepath.Clean(name), filepath.Clean(f.backup)+backupQuarantineSuffix) {
		return f.backupErr
	}
	return f.Fs.Remove(name)
}

func (f *w12PresenceFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.lstatErr != nil && filepath.Clean(name) == filepath.Clean(f.lstatDest) {
		return nil, false, f.lstatErr
	}
	return f.ls.LstatIfPossible(name)
}

func TestReverterRestorePresenceW12_IndeterminateLstatStaysConservativelyPresent(t *testing.T) {
	base := afero.NewMemMapFs()
	dest, backup := newW27ArmedReplacement(t, base, "W12-INDET") // dest deliberately absent
	repo := newP3OpRepo()
	op := w27CreateArmedReplacementRow(t, repo, dest, backup)

	removeErr := errors.New("backup remove wedged")
	lstatErr := errors.New("destination lstat wedged (indeterminate)")
	fs := &w12PresenceFs{
		Fs: base, ls: base.(afero.Lstater),
		backup: backup, backupErr: removeErr, failBackup: true,
		lstatDest: dest, lstatErr: lstatErr,
	}
	failingRepo := &failingUpdateRepo{p3OpRepo: repo, updateErr: errors.New("marker update transient")}

	restored, err := NewReverter(fs, failingRepo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, err, removeErr)
	require.True(t, restored[dest])
	require.Equal(t, "old", string(mustRead2(t, base, dest)),
		"an indeterminate pre-restore destination is conservatively present: the restore is retained")
	require.Equal(t, "old", string(mustRead2(t, base, backup)), "backup retained by the failed removal")

	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "the failed cleanup leaves the entry armed and retryable")
	require.False(t, gf.Replacements[0].RestorePending)
}

// The operator-driven contract on a real filesystem: absent injected
// failures, reverting over a dangling-symlink destination RESTORES over the
// entry (the journal owns the destination) and consumes normally — the
// pre-existing link object is superseded in place, never written through, and
// nothing behind it materializes. This is the sibling pin for the retention
// test above: classification-as-present governs only the failure
// compensation's undo scope, never the restore itself.
func TestReverterRestorePresenceW12_HappyPathRevertSupersedesDanglingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privilege on Windows")
	}
	root := t.TempDir()
	osfs := afero.NewOsFs()
	dest := filepath.Join(root, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA
	linkTarget := filepath.Join(root, "purged-original.bin") // never created — the link dangles
	require.NoError(t, os.Symlink(linkTarget, dest))
	require.NoError(t, os.WriteFile(backup, []byte("pre-replace-bytes"), 0o640))

	repo := newP3OpRepo()
	op := w27CreateArmedReplacementRow(t, repo, dest, backup)
	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Installed = true
	row.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), row))
	op.GeneratedFiles = row.GeneratedFiles

	ctx := context.Background()
	restored, err := NewReverter(osfs, repo).restoreReplacementJournal(ctx, op)
	require.NoError(t, err)
	require.True(t, restored[dest])
	require.Equal(t, "pre-replace-bytes", string(mustRead2(t, osfs, dest)), "backup bytes restored onto the destination")
	destInfo, lerr := os.Lstat(dest)
	require.NoError(t, lerr)
	require.True(t, destInfo.Mode().IsRegular(), "the restored destination is a regular file")
	_, terr := os.Stat(linkTarget)
	require.ErrorIs(t, terr, os.ErrNotExist, "nothing materialized behind the superseded link")
	_, berr := os.Lstat(backup)
	require.ErrorIs(t, berr, os.ErrNotExist, "consumed backup removed after the restore")

	row, err = repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements, "journal entry consumed")
}
