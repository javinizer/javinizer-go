package downloader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// W25 closes the downloader-side gaps identified in the 06:07 review:
// destination type discipline, rollback source identity, and confirmation
// cleanup ownership.

type w25ModeInfo struct {
	os.FileInfo
	mode os.FileMode
}

func (i w25ModeInfo) Mode() os.FileMode { return i.mode }

type w25SpecialDestinationFs struct {
	afero.Fs
	dest    string
	renamed bool
}

func (f *w25SpecialDestinationFs) Stat(name string) (os.FileInfo, error) {
	info, err := f.Fs.Stat(name)
	if err == nil && filepath.Clean(name) == filepath.Clean(f.dest) {
		return w25ModeInfo{FileInfo: info, mode: os.ModeNamedPipe | info.Mode().Perm()}, nil
	}
	return info, err
}

func (f *w25SpecialDestinationFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(f.dest) {
		f.renamed = true
	}
	return f.Fs.Rename(oldname, newname)
}

func TestInstallOverwritingW25_SpecialDestinationRefusedBeforeRename(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/W25-SPECIAL/poster.jpg"
	staged := dest + ".tmp"
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	fs := &w25SpecialDestinationFs{Fs: base, dest: dest}
	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w25-special", recorder: recorder,
	})
	require.NoError(t, err)
	require.True(t, skipped)
	require.True(t, replaced)
	require.False(t, fs.renamed, "special destination must not be moved into a backup")
	require.Empty(t, recorder.get(), "special destination must not be journaled")
	require.Equal(t, "old", string(mustReadDownloaderW7(t, base, dest)))
}

type w25ConfirmRollbackLedger struct {
	*armedTestLedger
	fs                 afero.Fs
	confirmErr         error
	releaseErr         error
	releaseCalls       int
	releaseSawNoBackup bool
}

func (l *w25ConfirmRollbackLedger) ConfirmReplacement(context.Context, string, string, string) error {
	return l.confirmErr
}

func (l *w25ConfirmRollbackLedger) ReleaseReplacement(ctx context.Context, opID, dest, backup string) error {
	l.releaseCalls++
	_, statErr := l.fs.Stat(backup)
	l.releaseSawNoBackup = os.IsNotExist(statErr)
	if l.releaseErr != nil {
		return l.releaseErr
	}
	return l.armedTestLedger.ReleaseReplacement(ctx, opID, dest, backup)
}

func TestInstallOverwritingW25_ConfirmFailureRemovesBackupBeforeSuccessfulRelease(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/W25-CONFIRM-OK/poster.jpg"
	staged := dest + ".tmp"
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("new"), 0o644))

	confirmErr := errors.New("w25 confirmation failed")
	recorder := &w25ConfirmRollbackLedger{
		armedTestLedger: &armedTestLedger{}, fs: fs, confirmErr: confirmErr,
	}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w25-confirm-ok", recorder: recorder,
	})
	require.ErrorIs(t, err, confirmErr)
	require.Equal(t, 1, recorder.releaseCalls)
	require.True(t, recorder.releaseSawNoBackup, "release must observe ownership cleanup already complete")
	require.Empty(t, recorder.get(), "successful release retracts the journal entry")
	require.Equal(t, "old", string(mustReadDownloaderW7(t, fs, dest)))
	requireNoDownloaderBackupW25(t, fs, filepath.Dir(dest))
}

type w25RemoveBackupErrorFs struct {
	afero.Fs
	err      error
	match    int
	armed    bool
	postArms int
	calls    int
}

// Wave-43: the take-asides' claim-bound housekeeping (vacated-name
// release/cleanup, scratch+placeholder unlinks) runs on ".dlq." siblings
// and rides through. Wave-44: the bound unlink NEVER path-removes the
// scratch — its placeholder vacates onto a fresh claimed terminal name and
// only that re-bound remove runs, on a ".vac." sibling — so the backup-
// family remove count excludes ".vac." names entirely. Wave-r19: the
// verified unlink's OWN bound terminal is likewise a ".vac." sibling, but
// it holds the verified object (non-zero); the take-aside's 0-byte
// placeholder removes (warn-only) and the bound-unlink's 0-byte terminal-
// placeholder release ride through (size 0). Only the object-bearing
// remove is the journaled backup's quarantine unlink, which must fail.
// The scripted wedge keys on backup→quarantine-shaped publishes: the
// FIRST is the fallback handoff's internal take-aside publish (backup →
// its scratch), the SECOND is the rollback handoff's publish of the
// journaled backup onto its quarantine name; after it, the FIRST object-
// bearing backup-family remove is the rollback's quarantine unlink of the
// journaled backup, which must fail.
func (f *w25RemoveBackupErrorFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && strings.Contains(newname, rollbackQuarantineSuffix) &&
		strings.Contains(filepath.Base(oldname), backupSuffixForDest+".") && !strings.Contains(oldname, rollbackQuarantineSuffix) {
		f.match++
		if f.match == 2 {
			f.armed = true
		}
	}
	return err
}

func (f *w25RemoveBackupErrorFs) Remove(name string) error {
	if f.armed && strings.Contains(filepath.Base(name), backupSuffixForDest+".") {
		if info, err := f.Fs.Stat(name); err == nil && info.Size() > 0 {
			f.postArms++
			if f.postArms == 1 {
				f.calls++
				return f.err
			}
		}
	}
	return f.Fs.Remove(name)
}

func TestInstallOverwritingW25_ConfirmRollbackBackupRemovalFailureKeepsOwnership(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/W25-CONFIRM-RM-ERR/poster.jpg"
	staged := dest + ".tmp"
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	removeErr := errors.New("w25 backup remove failed")
	fs := &w25RemoveBackupErrorFs{Fs: base, err: removeErr}
	recorder := &w25ConfirmRollbackLedger{
		armedTestLedger: &armedTestLedger{}, fs: fs, confirmErr: errors.New("w25 confirmation failed"),
	}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w25-confirm-rm-err", recorder: recorder,
	})
	require.ErrorIs(t, err, removeErr)
	require.Contains(t, err.Error(), "backup cleanup")
	require.Equal(t, 1, fs.calls,
		"wave-43/44: the take-asides' claim-bound + terminal housekeeping rides through; only the journaled backup's quarantine unlink is wedged")
	require.Zero(t, recorder.releaseCalls, "failed cleanup must not retract durable ownership")
	records := recorder.get()
	require.Len(t, records, 1)
	require.Equal(t, "old", string(mustReadDownloaderW7(t, fs, dest)))
	require.Equal(t, "old", string(mustReadDownloaderW7(t, fs, records[0].backupPath)))
}

func TestInstallOverwritingW25_ConfirmRollbackReleaseFailureRearmsBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/W25-CONFIRM-REL-ERR/poster.jpg"
	staged := dest + ".tmp"
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	releaseErr := errors.New("w25 release failed")
	fs := base
	recorder := &w25ConfirmRollbackLedger{
		armedTestLedger: &armedTestLedger{}, fs: fs,
		confirmErr: errors.New("w25 confirmation failed"), releaseErr: releaseErr,
	}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w25-confirm-rel-err", recorder: recorder,
	})
	require.ErrorIs(t, err, releaseErr)
	require.Equal(t, 1, recorder.releaseCalls)
	records := recorder.get()
	require.Len(t, records, 1, "a failed release leaves the journal armed")
	require.Equal(t, "old", string(mustReadDownloaderW7(t, fs, dest)))
	require.Equal(t, "old", string(mustReadDownloaderW7(t, fs, records[0].backupPath)), "release failure re-arms the removed backup")
}

func requireNoDownloaderBackupW25(t *testing.T, fs afero.Fs, dir string) {
	t.Helper()
	entries, err := afero.ReadDir(fs, dir)
	require.NoError(t, err)
	for _, entry := range entries {
		require.NotContains(t, entry.Name(), backupSuffixForDest+".")
	}
}

func w25RestoreFixture(t *testing.T) (afero.Fs, string, string) {
	t.Helper()
	fs := afero.NewMemMapFs()
	dest := "/out/W25-SOURCE/dest.bin"
	backup := "/out/W25-SOURCE/backup.bin"
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), 0o644))
	return fs, backup, dest
}

type w25NilLstatFs struct{ afero.Fs }

func (f *w25NilLstatFs) LstatIfPossible(string) (os.FileInfo, bool, error) {
	return nil, true, nil
}

type w25OpenErrorFs struct {
	afero.Fs
	backup string
	err    error
}

func (f *w25OpenErrorFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if filepath.Clean(name) == filepath.Clean(f.backup) {
		return nil, f.err
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type w25OpenedInfoFile struct {
	afero.File
	info    os.FileInfo
	statErr error
}

func (f *w25OpenedInfoFile) Stat() (os.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	return f.info, nil
}

type w25OpenedInfoFs struct {
	afero.Fs
	backup  string
	info    os.FileInfo
	statErr error
}

func (f *w25OpenedInfoFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil || filepath.Clean(name) != filepath.Clean(f.backup) {
		return file, err
	}
	return &w25OpenedInfoFile{File: file, info: f.info, statErr: f.statErr}, nil
}

func TestCopyBackupToDestW25_SourceValidationErrorLegs(t *testing.T) {
	t.Run("nil lstat info", func(t *testing.T) {
		base, backup, dest := w25RestoreFixture(t)
		err := copyBackupToDest(&w25NilLstatFs{Fs: base}, backup, dest)
		require.ErrorIs(t, err, ErrRestoreSourceRefused)
		require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)))
	})

	t.Run("open error", func(t *testing.T) {
		base, backup, dest := w25RestoreFixture(t)
		sentinel := errors.New("w25 source open failed")
		err := copyBackupToDest(&w25OpenErrorFs{Fs: base, backup: backup, err: sentinel}, backup, dest)
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)))
	})

	t.Run("opened stat error", func(t *testing.T) {
		base, backup, dest := w25RestoreFixture(t)
		sentinel := errors.New("w25 opened stat failed")
		fs := &w25OpenedInfoFs{Fs: base, backup: backup, statErr: sentinel}
		err := copyBackupToDest(fs, backup, dest)
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)))
	})

	t.Run("opened object non-regular", func(t *testing.T) {
		base, backup, dest := w25RestoreFixture(t)
		dir := filepath.Dir(backup) + "/opened-dir"
		require.NoError(t, base.Mkdir(dir, 0o755))
		info, err := base.Stat(dir)
		require.NoError(t, err)
		fs := &w25OpenedInfoFs{Fs: base, backup: backup, info: info}
		err = copyBackupToDest(fs, backup, dest)
		require.ErrorIs(t, err, ErrRestoreSourceRefused)
		require.Equal(t, "current", string(mustReadDownloaderW7(t, base, dest)))
	})
}

func TestCopyBackupToDestW25_NonRegularBackupRefused(t *testing.T) {
	fs, backup, dest := w25RestoreFixture(t)
	require.NoError(t, fs.Remove(backup))
	require.NoError(t, fs.Mkdir(backup, 0o755))

	err := copyBackupToDest(fs, backup, dest)
	require.ErrorIs(t, err, ErrRestoreSourceRefused)
	require.Contains(t, err.Error(), "restore source")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, fs, dest)))
	info, statErr := fs.Stat(backup)
	require.NoError(t, statErr)
	require.True(t, info.IsDir())
}

func TestCopyBackupToDestW25_SymlinkSwapRefusedAndDestinationUntouched(t *testing.T) {
	if runtime.GOOS == "windows" || restoreSourceNoFollow == 0 {
		t.Skip("symlink swap requires POSIX no-follow support")
	}

	root := t.TempDir()
	backup := filepath.Join(root, "backup.bin")
	oldBackup := backup + ".old"
	victim := filepath.Join(root, "victim.bin")
	dest := filepath.Join(root, "dest.bin")
	require.NoError(t, os.WriteFile(backup, []byte("old"), 0o640))
	require.NoError(t, os.WriteFile(victim, []byte("protected"), 0o640))
	require.NoError(t, os.WriteFile(dest, []byte("current"), 0o644))

	fs := &w25SymlinkSwapFs{Fs: afero.NewOsFs(), backup: backup, oldBackup: oldBackup, victim: victim}
	err := copyBackupToDest(fs, backup, dest)
	require.ErrorIs(t, err, ErrRestoreSourceRefused)
	require.Equal(t, "current", string(mustReadDownloaderW7(t, fs, dest)))
	require.Equal(t, "protected", string(mustReadDownloaderW7(t, fs, victim)))
	info, statErr := os.Lstat(backup)
	require.NoError(t, statErr)
	require.NotZero(t, info.Mode()&os.ModeSymlink)
}

type w25SymlinkSwapFs struct {
	afero.Fs
	backup    string
	oldBackup string
	victim    string
	swapped   bool
}

func (f *w25SymlinkSwapFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func (f *w25SymlinkSwapFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if filepath.Clean(name) == filepath.Clean(f.backup) && !f.swapped {
		f.swapped = true
		if err := f.Fs.Rename(f.backup, f.oldBackup); err != nil {
			return nil, err
		}
		if err := os.Symlink(f.victim, f.backup); err != nil {
			return nil, err
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type w25IdentitySwapFs struct {
	afero.Fs
	backup      string
	replacement string
	swapped     bool
}

func (f *w25IdentitySwapFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if filepath.Clean(name) == filepath.Clean(f.backup) && !f.swapped {
		f.swapped = true
		if err := f.Fs.Rename(f.backup, f.backup+".old"); err != nil {
			return nil, err
		}
		if err := f.Fs.Rename(f.replacement, f.backup); err != nil {
			return nil, err
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func TestCopyBackupToDestW25_IdentitySwapRefused(t *testing.T) {
	root := t.TempDir()
	backup := filepath.Join(root, "backup.bin")
	replacement := filepath.Join(root, "replacement.bin")
	dest := filepath.Join(root, "dest.bin")
	require.NoError(t, os.WriteFile(backup, []byte("old"), 0o640))
	require.NoError(t, os.WriteFile(replacement, []byte("other"), 0o640))
	require.NoError(t, os.WriteFile(dest, []byte("current"), 0o644))
	info, err := os.Stat(backup)
	require.NoError(t, err)
	if _, _, ok := restoreSourceIdentity(info); !ok {
		t.Skip("platform does not expose source identity")
	}

	fs := &w25IdentitySwapFs{Fs: afero.NewOsFs(), backup: backup, replacement: replacement}
	err = copyBackupToDest(fs, backup, dest)
	require.ErrorIs(t, err, ErrRestoreSourceRefused)
	require.Equal(t, "current", string(mustReadDownloaderW7(t, fs, dest)))
	require.Equal(t, "other", string(mustReadDownloaderW7(t, fs, backup)))
}

func TestCopyBackupToDestW25_NoFollowFlagIsPassed(t *testing.T) {
	if runtime.GOOS == "windows" || restoreSourceNoFollow == 0 {
		t.Skip("target has no portable O_NOFOLLOW flag")
	}
	root := t.TempDir()
	backup := filepath.Join(root, "backup.bin")
	dest := filepath.Join(root, "dest.bin")
	require.NoError(t, os.WriteFile(backup, []byte("old"), 0o640))
	require.NoError(t, os.WriteFile(dest, []byte("current"), 0o644))
	fs := &w25FlagFs{Fs: afero.NewOsFs(), backup: backup}

	require.NoError(t, copyBackupToDest(fs, backup, dest))
	require.NotZero(t, fs.flags&restoreSourceNoFollow)
	require.Equal(t, "old", string(mustReadDownloaderW7(t, fs, dest)))
}

type w25FlagFs struct {
	afero.Fs
	backup string
	flags  int
}

func (f *w25FlagFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if filepath.Clean(name) == filepath.Clean(f.backup) {
		f.flags = flag
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type w25RearmErrorFs struct {
	afero.Fs
	err          error
	stagingCalls int
}

func (f *w25RearmErrorFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.Contains(name, ".dlrstr.") {
		f.stagingCalls++
		if f.stagingCalls >= 2 {
			return nil, f.err
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func TestInstallOverwritingW25_ConfirmReleaseRearmFailureKeepsOriginalError(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/W25-CONFIRM-REARM-ERR/poster.jpg"
	staged := dest + ".tmp"
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))

	releaseErr := errors.New("w25 release failed")
	fs := &w25RearmErrorFs{Fs: base, err: errors.New("w25 re-arm failed")}
	recorder := &w25ConfirmRollbackLedger{
		armedTestLedger: &armedTestLedger{}, fs: fs,
		confirmErr: errors.New("w25 confirmation failed"), releaseErr: releaseErr,
	}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w25-confirm-rearm-err", recorder: recorder,
	})
	require.ErrorIs(t, err, releaseErr)
	require.Len(t, recorder.get(), 1, "failed release leaves ownership armed")
	records := recorder.get()
	_, statErr := base.Stat(records[0].backupPath)
	require.ErrorIs(t, statErr, os.ErrNotExist, "failed re-arm must not claim a backup exists")
	require.Equal(t, "old", string(mustReadDownloaderW7(t, fs, dest)))
}

func TestCopyBackupToDestW25_SymlinkBackupRefusedBeforeOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privilege on Windows")
	}
	root := t.TempDir()
	backup := filepath.Join(root, "backup.bin")
	victim := filepath.Join(root, "victim.bin")
	dest := filepath.Join(root, "dest.bin")
	require.NoError(t, os.WriteFile(victim, []byte("protected"), 0o640))
	require.NoError(t, os.WriteFile(dest, []byte("current"), 0o644))
	if err := os.Symlink(victim, backup); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	err := copyBackupToDest(afero.NewOsFs(), backup, dest)
	require.ErrorIs(t, err, ErrRestoreSourceRefused)
	require.Equal(t, "current", string(mustReadDownloaderW7(t, afero.NewOsFs(), dest)))
	require.Equal(t, "protected", string(mustReadDownloaderW7(t, afero.NewOsFs(), victim)))
}
