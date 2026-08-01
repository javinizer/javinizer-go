package poster

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errFS wraps an afero.Fs to inject failures into specific operations so the
// snapshot/restore error paths are observable under test.
type errFS struct {
	afero.Fs
	failOpen     bool
	failOpenFile bool
	failMkdirAll bool
}

func (e *errFS) Open(name string) (afero.File, error) {
	if e.failOpen {
		return nil, errors.New("injected open failure")
	}
	return e.Fs.Open(name)
}

func (e *errFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if e.failOpenFile {
		return nil, errors.New("injected openfile failure")
	}
	return e.Fs.OpenFile(name, flag, perm)
}

func (e *errFS) MkdirAll(path string, perm os.FileMode) error {
	if e.failMkdirAll {
		return errors.New("injected mkdirall failure")
	}
	return e.Fs.MkdirAll(path, perm)
}

const (
	snapJobID = "job1"
	snapID    = "ABC-001"
)

func snapshotPaths(fs afero.Fs, tempDir string, full, preview []byte) (string, string) {
	dir := filepath.Join(tempDir, "posters", snapJobID)
	_ = fs.MkdirAll(dir, 0o755)
	fullPath := filepath.Join(dir, snapID+"-full.jpg")
	previewPath := filepath.Join(dir, snapID+".jpg")
	if full != nil {
		_ = afero.WriteFile(fs, fullPath, full, 0o644)
	}
	if preview != nil {
		_ = afero.WriteFile(fs, previewPath, preview, 0o644)
	}
	return fullPath, previewPath
}

func TestSnapshotRestoreAssets_RoundTrip(t *testing.T) {
	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", http.DefaultClient)
	fullPath, previewPath := snapshotPaths(fs, "/tmp", []byte("old-full"), []byte("old-preview"))

	snap, err := pm.SnapshotAssets(snapJobID, snapID)
	require.NoError(t, err)
	require.NotNil(t, snap)

	// Simulate a refresh replacing both assets.
	require.NoError(t, afero.WriteFile(fs, fullPath, []byte("new-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, previewPath, []byte("new-preview"), 0o644))

	require.NoError(t, pm.RestoreAssets(snap))
	full, err := afero.ReadFile(fs, fullPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("old-full"), full)
	preview, err := afero.ReadFile(fs, previewPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("old-preview"), preview)
}

func TestSnapshotRestoreAssets_AbsentFilesRemovedOnRestore(t *testing.T) {
	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", http.DefaultClient)

	// Snapshot with nothing on disk: both assets recorded absent.
	snap, err := pm.SnapshotAssets(snapJobID, snapID)
	require.NoError(t, err)

	// A failed refresh may have created them; restore must remove them again.
	fullPath, previewPath := snapshotPaths(fs, "/tmp", []byte("new-full"), []byte("new-preview"))
	require.NoError(t, pm.RestoreAssets(snap))

	for _, p := range []string{fullPath, previewPath} {
		_, statErr := fs.Stat(p)
		assert.True(t, os.IsNotExist(statErr), "%s must be removed by restore", p)
	}
}

func TestSnapshotRestoreAssets_ValidationErrors(t *testing.T) {
	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", http.DefaultClient)

	_, err := pm.SnapshotAssets("..", snapID)
	assert.Error(t, err)
	_, err = pm.SnapshotAssets(snapJobID, "a/b")
	assert.Error(t, err)

	assert.Error(t, pm.RestoreAssets(&AssetsSnapshot{jobID: "..", posterID: snapID}))
	assert.Error(t, pm.RestoreAssets(&AssetsSnapshot{jobID: snapJobID, posterID: "a/b"}))
}

func TestRestoreAssets_NilSnapshotIsNoOp(t *testing.T) {
	pm := NewPosterManager(afero.NewMemMapFs(), "/tmp", http.DefaultClient)
	assert.NoError(t, pm.RestoreAssets(nil))
}

func TestSnapshotAssets_ReadErrorAborts(t *testing.T) {
	pm := NewPosterManager(&errFS{Fs: afero.NewMemMapFs(), failOpen: true}, "/tmp", http.DefaultClient)
	_, err := pm.SnapshotAssets(snapJobID, snapID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot poster asset")
}

func TestRestoreAssets_WriteFailures(t *testing.T) {
	t.Run("mkdir", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		pm := NewPosterManager(&errFS{Fs: fs, failMkdirAll: true}, "/tmp", http.DefaultClient)
		err := pm.RestoreAssets(&AssetsSnapshot{jobID: snapJobID, posterID: snapID, hasFull: true, full: []byte("x")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "restore poster asset directory")
	})
	t.Run("write", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		pm := NewPosterManager(&errFS{Fs: fs, failOpenFile: true}, "/tmp", http.DefaultClient)
		err := pm.RestoreAssets(&AssetsSnapshot{jobID: snapJobID, posterID: snapID, hasPreview: true, preview: []byte("x")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "restore poster asset")
	})
}

func TestScrapePosterGenerator_SnapshotRestore_NoManager(t *testing.T) {
	gen := NewScrapePosterGenerator(nil, "", "")
	snap, err := gen.SnapshotPosterAssets(snapJobID, snapID)
	require.NoError(t, err)
	assert.Nil(t, snap, "a manager-less generator has no assets to snapshot")
	assert.NoError(t, gen.RestorePosterAssets(snap))
}

func TestScrapePosterGenerator_SnapshotRestore_WithManager(t *testing.T) {
	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/tmp", http.DefaultClient)
	gen := NewScrapePosterGenerator(pm, "", "")
	fullPath, _ := snapshotPaths(fs, "/tmp", []byte("old-full"), []byte("old-preview"))

	snap, err := gen.SnapshotPosterAssets(snapJobID, snapID)
	require.NoError(t, err)
	require.NotNil(t, snap)

	require.NoError(t, afero.WriteFile(fs, fullPath, []byte("new-full"), 0o644))
	require.NoError(t, gen.RestorePosterAssets(snap))
	full, err := afero.ReadFile(fs, fullPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("old-full"), full)
}
