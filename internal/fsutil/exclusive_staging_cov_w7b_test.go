//go:build !windows

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// chmodFailFs fails only Chmod so the strict mode-restoration cleanup path in
// CreateExclusiveStagingFile is reachable while the exclusive open succeeds.
type chmodFailFs struct {
	afero.Fs
}

func (f chmodFailFs) Chmod(name string, mode os.FileMode) error {
	return errors.New("injected chmod failure")
}

func TestCreateExclusiveStagingFileW7B_ExactModeUnderRestrictiveUmask(t *testing.T) {
	// NOT parallel: syscall.Umask is process-wide for the test window.
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })

	fs := afero.NewOsFs()
	dir := t.TempDir()

	staged, file, err := CreateExclusiveStagingFile(fs, filepath.Join(dir, "poster.jpg"), ".rstr", 1, 0o666)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	// Without the mode restoration the kernel would have narrowed this to
	// 0644 under umask 0077, silently losing group/other write bits.
	info, err := os.Stat(staged)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o666), info.Mode().Perm())

	staged600, file600, err := CreateExclusiveStagingFile(fs, filepath.Join(dir, "trailer.mp4"), ".rstr", 1, 0o600)
	require.NoError(t, err)
	require.NoError(t, file600.Close())
	info600, err := os.Stat(staged600)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info600.Mode().Perm())
}

func TestCreateExclusiveStagingFileW7B_ModeRestoreFailureCleansUp(t *testing.T) {
	base := afero.NewOsFs()
	dir := t.TempDir()
	fs := chmodFailFs{Fs: base}

	dest := filepath.Join(dir, "poster.jpg")
	staged, file, err := CreateExclusiveStagingFile(fs, dest, ".rstr", 1, 0o666)
	require.Error(t, err)
	require.ErrorContains(t, err, "apply exclusive staging mode")
	require.Empty(t, staged)
	require.Nil(t, file)

	// The failed staging attempt must not leak the staged inode: a retry at
	// the same ordinal has to be able to claim the same name again.
	entries, rerr := os.ReadDir(dir)
	require.NoError(t, rerr)
	require.Empty(t, entries)

	retry, retryFile, rerr2 := CreateExclusiveStagingFile(base, dest, ".rstr", 1, 0o666)
	require.NoError(t, rerr2)
	require.NoError(t, retryFile.Close())
	require.FileExists(t, retry)
}
