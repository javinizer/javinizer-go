package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestReproofStreamedSourceRejectsModeDrift(t *testing.T) {
	fs := afero.NewOsFs()
	path := filepath.Join(t.TempDir(), "source")
	require.NoError(t, afero.WriteFile(fs, path, []byte("unchanged bytes"), 0600))
	require.NoError(t, fs.Chmod(path, 0644))
	t.Cleanup(func() { _ = fs.Chmod(path, 0600) })
	file, err := fs.Open(path)
	require.NoError(t, err)
	defer file.Close()
	info, err := file.Stat()
	require.NoError(t, err)
	reproof := reproofStreamedSource(file, info, path, "test", "handle")
	require.NoError(t, reproof())
	require.NoError(t, os.Chmod(path, 0444))
	changed, err := file.Stat()
	require.NoError(t, err)
	require.Equal(t, info.Size(), changed.Size())
	require.Equal(t, info.ModTime(), changed.ModTime())
	require.NotEqual(t, info.Mode().Perm(), changed.Mode().Perm())
	require.ErrorIs(t, reproof(), ErrTakeAsideForeign)
}
