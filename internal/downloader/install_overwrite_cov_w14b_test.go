package downloader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

var (
	covW14BReleaseErr   = errors.New("w14b release wedged")
	covW14BInstallErr   = errors.New("w14b staged install wedged")
	covW14BRearmErr     = errors.New("w14b re-arm staging wedged")
	covW14BStatErr      = errors.New("w14b backup stat wedged")
	covW14BNoClobberErr = errors.New("w14b existing backup was clobbered")
)

type covW14BReleaseFailingLedger struct {
	*armedTestLedger
	releaseErr error
	releases   int
}

func (l *covW14BReleaseFailingLedger) ReleaseReplacement(context.Context, string, string, string) error {
	l.releases++
	return l.releaseErr
}

func TestInstallOverwriting_RetractionFailureRearmsBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/W14B-REARM"
	dest := dir + "/poster.jpg"
	staged := dest + ".tmp"
	old := []byte("original bytes")
	mode := os.FileMode(0o601)
	mtime := time.Unix(123456789, 123456000)

	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, old, mode))
	require.NoError(t, base.Chmod(dest, mode))
	require.NoError(t, base.Chtimes(dest, mtime, mtime))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new bytes"), 0o644))

	fs := rejectStagedRenameFS{Fs: base}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	recorder := &covW14BReleaseFailingLedger{
		armedTestLedger: &armedTestLedger{},
		releaseErr:      covW14BReleaseErr,
	}

	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID:     "w14b-rearm",
		recorder: recorder,
	})
	require.ErrorContains(t, err, "staged install rejected")
	require.Equal(t, 1, recorder.releases)

	records := recorder.get()
	require.Len(t, records, 1, "release failure leaves the journal entry armed")
	backup := records[0].backupPath

	got, readErr := afero.ReadFile(base, dest)
	require.NoError(t, readErr)
	require.Equal(t, old, got, "rollback leaves the restored destination bytes in place")

	backupBytes, readErr := afero.ReadFile(base, backup)
	require.NoError(t, readErr, "release failure must re-arm the journaled backup")
	require.Equal(t, old, backupBytes)
	backupInfo, statErr := base.Stat(backup)
	require.NoError(t, statErr, "a later ReleaseReplacement/revert stat must remain usable")
	require.Equal(t, mode.Perm(), backupInfo.Mode().Perm())
	require.Equal(t, mtime.UnixNano(), backupInfo.ModTime().UnixNano())
}

func TestRearmReplacementBackup_ExistingBackupIsRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/out/W14B-IDEMPOTENT"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	backupBytes := []byte("already re-armed bytes")
	backupMode := os.FileMode(0o640)
	backupMTime := time.Unix(222222222, 222222000)

	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("restored destination"), 0o600))
	require.NoError(t, afero.WriteFile(fs, backup, backupBytes, backupMode))
	require.NoError(t, fs.Chmod(backup, backupMode))
	require.NoError(t, fs.Chtimes(backup, backupMTime, backupMTime))

	require.NoError(t, rearmReplacementBackup(fs, dest, backup))
	got, err := afero.ReadFile(fs, backup)
	require.NoError(t, err)
	require.Equal(t, backupBytes, got, "an existing re-arm must not be clobbered")
	info, err := fs.Stat(backup)
	require.NoError(t, err)
	require.Equal(t, backupMode.Perm(), info.Mode().Perm())
	require.Equal(t, backupMTime.UnixNano(), info.ModTime().UnixNano())
}

type covW14BPreRearmedFS struct {
	afero.Fs
	dest      string
	destOpens int
}

func (f *covW14BPreRearmedFS) Open(name string) (afero.File, error) {
	if filepath.Clean(name) == filepath.Clean(f.dest) {
		f.destOpens++
		return nil, covW14BNoClobberErr
	}
	return f.Fs.Open(name)
}

func (f *covW14BPreRearmedFS) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) && strings.HasSuffix(oldname, ".tmp") {
		return covW14BInstallErr
	}
	if filepath.Clean(newname) == filepath.Clean(f.dest) && strings.Contains(filepath.Base(oldname), ".dlbak.") {
		data, err := afero.ReadFile(f.Fs, oldname)
		if err != nil {
			return err
		}
		info, err := f.Fs.Stat(oldname)
		if err != nil {
			return err
		}
		if err := afero.WriteFile(f.Fs, newname, data, info.Mode().Perm()); err != nil {
			return err
		}
		if err := f.Fs.Chmod(newname, info.Mode().Perm()); err != nil {
			return err
		}
		return f.Fs.Chtimes(newname, info.ModTime(), info.ModTime())
	}
	return f.Fs.Rename(oldname, newname)
}

func TestInstallOverwriting_RetractionFailureWithPreexistingBackupDoesNotClobber(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/W14B-IDEMPOTENT-ROLLBACK"
	dest := dir + "/poster.jpg"
	staged := dest + ".tmp"
	old := []byte("original bytes")

	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, old, 0o601))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new bytes"), 0o644))

	fs := &covW14BPreRearmedFS{Fs: base, dest: dest}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	recorder := &covW14BReleaseFailingLedger{
		armedTestLedger: &armedTestLedger{},
		releaseErr:      covW14BReleaseErr,
	}

	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID:     "w14b-pre-rearmed",
		recorder: recorder,
	})
	require.ErrorIs(t, err, covW14BInstallErr)
	require.Equal(t, 1, recorder.releases)
	require.Zero(t, fs.destOpens, "existing backup makes re-arm return before opening the restored destination")
	records := recorder.get()
	require.Len(t, records, 1)
	backupBytes, readErr := afero.ReadFile(base, records[0].backupPath)
	require.NoError(t, readErr)
	require.Equal(t, old, backupBytes, "pre-existing re-arm remains untouched")
}

type covW14BBackupStatErrorFS struct {
	afero.Fs
	path string
	err  error
}

func (f covW14BBackupStatErrorFS) Stat(name string) (os.FileInfo, error) {
	if filepath.Clean(name) == filepath.Clean(f.path) {
		return nil, f.err
	}
	return f.Fs.Stat(name)
}

func TestRearmReplacementBackup_BackupStatFailure(t *testing.T) {
	base := afero.NewMemMapFs()
	backup := "/out/W14B-STAT/poster.jpg.dlbak.0123456789abcdef"
	fs := covW14BBackupStatErrorFS{Fs: base, path: backup, err: covW14BStatErr}

	err := rearmReplacementBackup(fs, "/out/W14B-STAT/poster.jpg", backup)
	require.ErrorIs(t, err, covW14BStatErr)
}

type covW14BRearmFailureFS struct {
	afero.Fs
	dest string
}

func (f covW14BRearmFailureFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.Contains(name, ".dlrstr.") {
		return nil, covW14BRearmErr
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f covW14BRearmFailureFS) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) && strings.HasSuffix(oldname, ".tmp") {
		return covW14BInstallErr
	}
	return f.Fs.Rename(oldname, newname)
}

func TestInstallOverwriting_RetractionFailureRearmFailureIsLoggedAndOriginalErrorReturned(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/W14B-REARM-ERR"
	dest := dir + "/poster.jpg"
	staged := dest + ".tmp"
	old := []byte("original bytes")

	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, old, 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new bytes"), 0o644))

	fs := covW14BRearmFailureFS{Fs: base, dest: dest}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	recorder := &covW14BReleaseFailingLedger{
		armedTestLedger: &armedTestLedger{},
		releaseErr:      covW14BReleaseErr,
	}

	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID:     "w14b-rearm-error",
		recorder: recorder,
	})
	require.ErrorIs(t, err, covW14BInstallErr, "re-arm failure must not replace the rollback error")
	require.Equal(t, 1, recorder.releases)
	records := recorder.get()
	require.Len(t, records, 1)
	_, statErr := base.Stat(records[0].backupPath)
	require.Error(t, statErr, "a failed re-arm must not claim a usable backup exists")
	got, readErr := afero.ReadFile(base, dest)
	require.NoError(t, readErr)
	require.Equal(t, old, got)
}
