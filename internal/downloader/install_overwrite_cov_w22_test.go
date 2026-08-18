package downloader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func resetBackupOrdinalW22(t *testing.T) {
	t.Helper()
	previous := backupOrdinal.Load()
	backupOrdinal.Store(0)
	t.Cleanup(func() { backupOrdinal.Store(previous) })
}

func backupCandidateW22(dest, opID string, ordinal uint64) string {
	return dest + backupSuffixForDest + "." + sha1hex8(opID) + "." + strconv.FormatUint(ordinal, 16)
}

func TestInstallOverwritingW22_OccupiedBackupNamesAdvanceWithoutClobber(t *testing.T) {
	resetBackupOrdinalW22(t)
	fs := afero.NewMemMapFs()
	dir := "/out/W22-OCCUPIED"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	opID := "w22-occupied"
	first := backupCandidateW22(dest, opID, 1)
	second := backupCandidateW22(dest, opID, 2)
	third := backupCandidateW22(dest, opID, 3)
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("new"), 0o644))
	require.NoError(t, afero.WriteFile(fs, first, []byte("user-file"), 0o600))
	require.NoError(t, afero.WriteFile(fs, second, []byte("older-backup"), 0o600))

	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: opID, recorder: recorder,
	})
	require.NoError(t, err)
	require.False(t, skipped)
	require.True(t, replaced)
	require.Equal(t, "new", string(mustReadDownloaderW7(t, fs, dest)))
	require.Equal(t, "user-file", string(mustReadDownloaderW7(t, fs, first)))
	require.Equal(t, "older-backup", string(mustReadDownloaderW7(t, fs, second)))
	require.Equal(t, "current", string(mustReadDownloaderW7(t, fs, third)))
	records := recorder.get()
	require.Len(t, records, 1)
	require.Equal(t, third, records[0].backupPath)
}

func TestInstallOverwritingW22_BackupNameExhaustionFailsClosed(t *testing.T) {
	resetBackupOrdinalW22(t)
	fs := afero.NewMemMapFs()
	dir := "/out/W22-EXHAUST"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	opID := "w22-exhaust"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("new"), 0o644))
	for ordinal := uint64(1); ordinal <= backupNameClaimTries; ordinal++ {
		require.NoError(t, afero.WriteFile(fs, backupCandidateW22(dest, opID, ordinal), []byte("occupied"), 0o600))
	}

	recorder := &armedTestLedger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: opID, recorder: recorder,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "backup names exhausted")
	require.False(t, skipped)
	require.True(t, replaced)
	require.Equal(t, "current", string(mustReadDownloaderW7(t, fs, dest)))
	require.Empty(t, recorder.get(), "failed claim must not arm the ledger")
	_, markerErr := fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, markerErr, os.ErrNotExist)
}

type backupCandidateErrorW22Fs struct {
	afero.Fs
	err error
}

func (f *backupCandidateErrorW22Fs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if strings.Contains(filepath.ToSlash(name), backupSuffixForDest+".") {
		return nil, true, f.err
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func TestInstallOverwritingW22_BackupCandidateInspectionErrorFailsClosed(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/out/W22-CANDIDATE-ERROR"
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("new"), 0o644))
	sentinel := errors.New("candidate lstat wedged")
	wrapped := &backupCandidateErrorW22Fs{Fs: fs, err: sentinel}

	d := NewDownloader(nil, wrapped, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{
		opID: "w22-candidate-error", recorder: &armedTestLedger{},
	})
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, "current", string(mustReadDownloaderW7(t, fs, dest)))
}
