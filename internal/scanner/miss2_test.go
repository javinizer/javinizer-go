package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- lstatInfo: non-Lstater filesystem falls back to Stat ---
// Line 45: return s.fs.Stat(path) when fs is not a Lstater

func TestMiss2_LstatInfo_NonLstaterFS(t *testing.T) {
	// MemMapFs actually IS a Lstater, so we need a wrapper that hides it
	fs := afero.NewMemMapFs()
	nonLstaterFs := &statOnlyFs{delegate: fs}
	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(nonLstaterFs, cfg)

	_ = fs.MkdirAll("/test/dir", 0755)
	_ = afero.WriteFile(fs, "/test/dir/movie.mp4", []byte("data"), 0644)

	info, err := s.lstatInfo("/test/dir/movie.mp4")
	require.NoError(t, err)
	assert.Equal(t, "movie.mp4", info.Name())
}

// statOnlyFs wraps an afero.Fs but doesn't implement Lstater,
// forcing lstatInfo to fall back to Stat.
type statOnlyFs struct {
	delegate afero.Fs
}

func (s *statOnlyFs) Create(name string) (afero.File, error)    { return s.delegate.Create(name) }
func (s *statOnlyFs) Mkdir(name string, perm os.FileMode) error { return s.delegate.Mkdir(name, perm) }
func (s *statOnlyFs) MkdirAll(name string, perm os.FileMode) error {
	return s.delegate.MkdirAll(name, perm)
}
func (s *statOnlyFs) Open(name string) (afero.File, error) { return s.delegate.Open(name) }
func (s *statOnlyFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	return s.delegate.OpenFile(name, flag, perm)
}
func (s *statOnlyFs) Remove(name string) error    { return s.delegate.Remove(name) }
func (s *statOnlyFs) RemoveAll(name string) error { return s.delegate.RemoveAll(name) }
func (s *statOnlyFs) Rename(oldname, newname string) error {
	return s.delegate.Rename(oldname, newname)
}
func (s *statOnlyFs) Stat(name string) (os.FileInfo, error)     { return s.delegate.Stat(name) }
func (s *statOnlyFs) Name() string                              { return "statOnlyFs" }
func (s *statOnlyFs) Chmod(name string, mode os.FileMode) error { return s.delegate.Chmod(name, mode) }
func (s *statOnlyFs) Chown(name string, uid, gid int) error     { return s.delegate.Chown(name, uid, gid) }
func (s *statOnlyFs) Chtimes(name string, atime, mtime time.Time) error {
	return s.delegate.Chtimes(name, atime, mtime)
}

// --- ScanSingle: non-Lstater fs with directory ---
// Line 217-219: fs.Stat error when scanning a non-existent single path

func TestMiss2_ScanSingle_NonLstaterFS(t *testing.T) {
	fs := afero.NewMemMapFs()
	nonLstaterFs := &statOnlyFs{delegate: fs}
	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(nonLstaterFs, cfg)

	dir := filepath.Join(t.TempDir(), "dir")
	_ = fs.MkdirAll(dir, 0755)
	_ = afero.WriteFile(fs, filepath.Join(dir, "movie.mp4"), []byte("data"), 0644)

	result, err := s.ScanSingle(dir)
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Files))
	assert.Equal(t, "movie.mp4", result.Files[0].Name)
}

// --- ScanSingle: non-Lstater fs with single file ---

func TestMiss2_ScanSingle_NonLstaterFS_SingleFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	nonLstaterFs := &statOnlyFs{delegate: fs}
	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(nonLstaterFs, cfg)

	tmpDir := t.TempDir()
	_ = fs.MkdirAll(tmpDir, 0755)
	moviePath := filepath.Join(tmpDir, "movie.mp4")
	_ = afero.WriteFile(fs, moviePath, []byte("data"), 0644)

	result, err := s.ScanSingle(moviePath)
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Files))
	assert.Equal(t, "movie.mp4", result.Files[0].Name)
}

// --- ScanSingle: non-Lstater fs with excluded single file ---

func TestMiss2_ScanSingle_NonLstaterFS_ExcludedFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	nonLstaterFs := &statOnlyFs{delegate: fs}
	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(nonLstaterFs, cfg)

	tmpDir := t.TempDir()
	_ = fs.MkdirAll(tmpDir, 0755)
	txtPath := filepath.Join(tmpDir, "readme.txt")
	_ = afero.WriteFile(fs, txtPath, []byte("data"), 0644)

	result, err := s.ScanSingle(txtPath)
	require.NoError(t, err)
	assert.Equal(t, 0, len(result.Files))
	assert.Equal(t, 1, result.SkippedCount)
}

// --- ScanWithFilter: filter matching with subdirectories ---
// Tests the filter logic more thoroughly

func TestMiss2_ScanWithFilter_SubdirFilter(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested directories with different names
	actionDir := tmpDir + "/action"
	require.NoError(t, os.Mkdir(actionDir, 0755))
	dramaDir := tmpDir + "/drama"
	require.NoError(t, os.Mkdir(dramaDir, 0755))

	require.NoError(t, os.WriteFile(actionDir+"/action-video1.mp4", []byte("data"), 0644))
	require.NoError(t, os.WriteFile(dramaDir+"/drama-video2.mp4", []byte("data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	result, err := s.ScanWithFilter(context.Background(), tmpDir, 0, "action")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Files), 1)
	for _, f := range result.Files {
		assert.Contains(t, f.Path, "action")
	}
}

// --- Filter: non-existent files are skipped ---

func TestMiss2_Filter_NonExistentPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(fs, cfg)

	result := s.Filter([]string{"/nonexistent/file.mp4"})
	assert.Empty(t, result)
}

// --- ScanSingle: MemMapFs directory with subdirs and non-video files ---

func TestMiss2_ScanSingle_MemMapFs_MixedContent(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := filepath.Join(t.TempDir(), "dir")
	require.NoError(t, fs.MkdirAll(filepath.Join(dir, "subdir"), 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "movie.mp4"), []byte("video"), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "readme.txt"), []byte("text"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(fs, cfg)

	result, err := s.ScanSingle(dir)
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Files))
	assert.Equal(t, 1, result.SkippedCount) // readme.txt skipped
}

// --- ScanWithFilter: non-existent root path error ---

func TestMiss2_ScanWithFilter_NonExistentRoot(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(fs, cfg)

	_, err := s.ScanWithFilter(context.Background(), "/nonexistent/path", 0, "")
	require.Error(t, err)
}
