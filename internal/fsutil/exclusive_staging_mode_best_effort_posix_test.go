//go:build !windows

package fsutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestCreateExclusiveStagingFileModeBestEffortNativeRefusal(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	calls := denyStagingChmod(t)
	fs := afero.NewOsFs()

	parent := t.TempDir()
	path, file, err := CreateExclusiveStagingFileModeBestEffort(fs, filepath.Join(parent, "marker"), ".tmp", 0, 0o666)
	require.NoError(t, err, "a refused chmod keeps the inherited mode for best-effort staging")
	defer file.Close()
	require.Equal(t, 1, *calls)
	info, err := file.Stat()
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	require.NoError(t, VerifyStagedIdentity(fs, path, file))

	strictParent := t.TempDir()
	_, _, err = CreateExclusiveStagingFile(fs, filepath.Join(strictParent, "media"), ".tmp", 0, 0o666)
	require.ErrorIs(t, err, syscall.EPERM, "media staging keeps the strict mode contract")
	entries, err := os.ReadDir(strictParent)
	require.NoError(t, err)
	require.Empty(t, entries)
}
