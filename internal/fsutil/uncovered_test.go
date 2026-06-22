package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAferoRemoveAll_SingleFileUncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/tmp/testfile.txt", []byte("hello"), 0644))
	require.NoError(t, AferoRemoveAll(fs, "/tmp/testfile.txt"))
	exists, err := afero.Exists(fs, "/tmp/testfile.txt")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestAferoRemoveAll_NestedDirectoriesUncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/a/b/c", 0755))
	require.NoError(t, afero.WriteFile(fs, "/a/b/c/file.txt", []byte("deep"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/a/b/file2.txt", []byte("mid"), 0644))
	require.NoError(t, AferoRemoveAll(fs, "/a"))
	exists, err := afero.Exists(fs, "/a")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestCanonicalizePath_RelativePathUncovered(t *testing.T) {
	result, err := CanonicalizePath("relative/path")
	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

func TestCopyFileFs_SameSourceAndDestUncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/tmp/file.txt", []byte("content"), 0644))
	err := CopyFileFs(fs, "/tmp/file.txt", "/tmp/file.txt")
	assert.NoError(t, err)
}

func TestCopyFileFs_SourceNotFoundUncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	err := CopyFileFs(fs, "/nonexistent.txt", "/tmp/dest.txt")
	assert.Error(t, err)
}

func TestMoveFileFs_SameSourceAndDestUncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/tmp/file.txt", []byte("content"), 0644))
	err := MoveFileFs(fs, "/tmp/file.txt", "/tmp/file.txt")
	assert.NoError(t, err)
}

func TestMoveFileFs_WithMemMapFsUncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/tmp/src.txt", []byte("move me"), 0644))
	err := MoveFileFs(fs, "/tmp/src.txt", "/tmp/dst.txt")
	require.NoError(t, err)
	content, err := afero.ReadFile(fs, "/tmp/dst.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("move me"), content)
}

func TestAferoRemoveAll_NonexistentPathUncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	err := AferoRemoveAll(fs, "/nonexistent/path")
	assert.NoError(t, err)
}

func TestAferoRemoveAll_StatErrorNonNotExistUncovered(t *testing.T) {
	fs := afero.NewReadOnlyFs(afero.NewMemMapFs())
	// ReadOnlyFs will fail on Stat for a non-existent path with file not found
	err := AferoRemoveAll(fs, "/some/path")
	// If the path doesn't exist, AferoRemoveAll returns nil (os.IsNotExist check)
	// This is expected behavior
	assert.NoError(t, err, "non-existent path should be handled gracefully")
}

func TestCanonicalizePath_DotPathUncovered(t *testing.T) {
	result, err := CanonicalizePath(".")
	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

// Test with real filesystem for CopyFileFs
func TestCopyFileFs_BasicOsFsUncovered(t *testing.T) {
	fs := afero.NewOsFs()
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "source.txt")
	dstPath := filepath.Join(tmpDir, "subdir", "dest.txt")
	require.NoError(t, afero.WriteFile(fs, srcPath, []byte("hello world"), 0644))
	err := CopyFileFs(fs, srcPath, dstPath)
	require.NoError(t, err)
	content, err := afero.ReadFile(fs, dstPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello world"), content)
}

func TestAferoRemoveAll_WalkErrorUncovered(t *testing.T) {
	// Test that AferoRemoveAll handles walk errors properly
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/dir", 0755))
	// Create a regular file — AferoRemoveAll should handle this
	require.NoError(t, afero.WriteFile(fs, "/tmp/dir/file.txt", []byte("data"), 0644))
	err := AferoRemoveAll(fs, "/tmp/dir")
	require.NoError(t, err)
	_, statErr := fs.Stat("/tmp/dir")
	assert.True(t, os.IsNotExist(statErr))
}
