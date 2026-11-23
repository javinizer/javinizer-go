package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestTryRemoveEmptyDirLeavesParentsAndNonemptyDirs(t *testing.T) {
	fs := afero.NewOsFs()
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	file := filepath.Join(child, "file")
	require.NoError(t, fs.Mkdir(child, 0o700))
	require.NoError(t, afero.WriteFile(fs, file, []byte("retained"), 0o600))

	require.False(t, TryRemoveEmptyDir(fs, parent))
	require.False(t, TryRemoveEmptyDir(fs, child))
	require.False(t, TryRemoveEmptyDir(fs, file))
	contents, err := afero.ReadFile(fs, file)
	require.NoError(t, err)
	require.Equal(t, "retained", string(contents))

	require.NoError(t, fs.Remove(file))
	require.True(t, TryRemoveEmptyDir(fs, child))
	_, err = fs.Stat(child)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.False(t, TryRemoveEmptyDir(fs, child))
	info, err := fs.Stat(parent)
	require.NoError(t, err)
	require.True(t, info.IsDir(), "only the requested empty directory should be removed")
}
