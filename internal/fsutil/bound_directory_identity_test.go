package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoundObjectIdentityNativeDirectorySurvivesRename(t *testing.T) {
	fs := afero.NewOsFs()
	root := filepath.Join(t.TempDir(), "stage")
	require.NoError(t, fs.MkdirAll(root, 0o755))
	ownedInfo, err := fs.Stat(root)
	require.NoError(t, err)
	device, inode, strong := BoundObjectIdentity(fs, root, ownedInfo)
	require.True(t, strong, "native directory identity must not fall back to timestamps")
	aside := root + ".retained"
	require.NoError(t, fs.Rename(root, aside))
	asideInfo, err := fs.Stat(aside)
	require.NoError(t, err)
	asideDevice, asideInode, strong := BoundObjectIdentity(fs, aside, asideInfo)
	require.True(t, strong)
	assert.Equal(t, [2]uint64{device, inode}, [2]uint64{asideDevice, asideInode})
	require.NoError(t, fs.MkdirAll(root, os.FileMode(0o755)))
	require.NoError(t, fs.Chtimes(root, ownedInfo.ModTime(), ownedInfo.ModTime()))
	foreignInfo, err := fs.Stat(root)
	require.NoError(t, err)
	assert.Equal(t, ownedInfo.Size(), foreignInfo.Size())
	assert.Equal(t, ownedInfo.ModTime(), foreignInfo.ModTime())
	foreignDevice, foreignInode, strong := BoundObjectIdentity(fs, root, foreignInfo)
	require.True(t, strong)
	assert.NotEqual(t, [2]uint64{device, inode}, [2]uint64{foreignDevice, foreignInode})
}
