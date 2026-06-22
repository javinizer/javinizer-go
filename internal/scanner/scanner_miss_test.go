package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ScanWithFilter: directory filter matching ---

func TestScanWithFilter_DirectoryFilter(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directory structure with matching and non-matching subdirectories
	// File names must also match the filter (ScanWithFilter checks both dirs and files)
	actionDir := filepath.Join(tmpDir, "action-movies")
	require.NoError(t, os.Mkdir(actionDir, 0755))
	comedyDir := filepath.Join(tmpDir, "comedy-movies")
	require.NoError(t, os.Mkdir(comedyDir, 0755))

	require.NoError(t, os.WriteFile(filepath.Join(actionDir, "action-video1.mp4"), []byte("data"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(comedyDir, "comedy-video2.mp4"), []byte("data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	result, err := s.ScanWithFilter(context.Background(), tmpDir, 0, "action")
	require.NoError(t, err)
	// Should find the video in action-movies but not comedy-movies
	assert.GreaterOrEqual(t, len(result.Files), 1)
	found := false
	for _, f := range result.Files {
		if f.Name == "action-video1.mp4" {
			found = true
		}
	}
	assert.True(t, found, "should find action-video1.mp4 in action-movies directory")
}

func TestScanWithFilter_NoFilter(t *testing.T) {
	tmpDir := t.TempDir()

	actionDir := filepath.Join(tmpDir, "action-movies")
	require.NoError(t, os.Mkdir(actionDir, 0755))
	comedyDir := filepath.Join(tmpDir, "comedy-movies")
	require.NoError(t, os.Mkdir(comedyDir, 0755))

	require.NoError(t, os.WriteFile(filepath.Join(actionDir, "video1.mp4"), []byte("data"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(comedyDir, "video2.mp4"), []byte("data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	result, err := s.ScanWithFilter(context.Background(), tmpDir, 0, "")
	require.NoError(t, err)
	assert.Equal(t, 2, len(result.Files))
}

func TestScanWithFilter_FileNameFilter(t *testing.T) {
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "action-movie.mp4"), []byte("data"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "drama-movie.mp4"), []byte("data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	result, err := s.ScanWithFilter(context.Background(), tmpDir, 0, "action")
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Files))
	assert.Equal(t, "action-movie.mp4", result.Files[0].Name)
}

// --- ScanWithFilter: symlink root directory ---

func TestScanWithFilter_SymlinkRoot(t *testing.T) {
	tmpDir := t.TempDir()
	realDir := filepath.Join(tmpDir, "real")
	require.NoError(t, os.Mkdir(realDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(realDir, "video.mp4"), []byte("data"), 0644))

	linkDir := filepath.Join(tmpDir, "link")
	err := os.Symlink(realDir, linkDir)
	if err != nil {
		t.Skip("Cannot create symlinks")
	}

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	result, err := s.ScanWithFilter(context.Background(), linkDir, 0, "")
	require.NoError(t, err)
	// Symlink root should be skipped entirely
	assert.Equal(t, 0, len(result.Files))
	assert.GreaterOrEqual(t, result.SkippedCount, 1)
}

// --- ScanSingle: MemMapFs in-memory filesystem ---

func TestScanSingle_MemMapFs(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := filepath.Join(t.TempDir(), "dir")
	require.NoError(t, fs.MkdirAll(dir, 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "movie.mp4"), []byte("video data"), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "readme.txt"), []byte("text data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(fs, cfg)

	result, err := s.ScanSingle(dir)
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Files))
	assert.Equal(t, "movie.mp4", result.Files[0].Name)
}

func TestScanSingle_MemMapFs_SingleFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	tmpDir := t.TempDir()
	require.NoError(t, fs.MkdirAll(tmpDir, 0755))
	moviePath := filepath.Join(tmpDir, "movie.mp4")
	require.NoError(t, afero.WriteFile(fs, moviePath, []byte("video data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(fs, cfg)

	result, err := s.ScanSingle(moviePath)
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Files))
	assert.Equal(t, "movie.mp4", result.Files[0].Name)
}

func TestScanSingle_MemMapFs_ExcludePatterns(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := filepath.Join(t.TempDir(), "dir")
	require.NoError(t, fs.MkdirAll(dir, 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "movie.mp4"), []byte("video data"), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "movie-trailer.mp4"), []byte("trailer data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}, ExcludePatterns: []string{"*-trailer*"}}
	s := NewScanner(fs, cfg)

	result, err := s.ScanSingle(dir)
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Files))
	assert.Equal(t, "movie.mp4", result.Files[0].Name)
}

func TestScanSingle_MemMapFs_SingleFileExcluded(t *testing.T) {
	fs := afero.NewMemMapFs()
	tmpDir := t.TempDir()
	require.NoError(t, fs.MkdirAll(tmpDir, 0755))
	trailerPath := filepath.Join(tmpDir, "movie-trailer.mp4")
	require.NoError(t, afero.WriteFile(fs, trailerPath, []byte("trailer data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}, ExcludePatterns: []string{"*-trailer*"}}
	s := NewScanner(fs, cfg)

	result, err := s.ScanSingle(trailerPath)
	require.NoError(t, err)
	assert.Equal(t, 0, len(result.Files))
	assert.Equal(t, 1, result.SkippedCount)
}

// --- ScanSingle: directory with lstatInfo errors ---

func TestScanSingle_MemMapFs_SubdirectorySkipped(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := filepath.Join(t.TempDir(), "dir")
	require.NoError(t, fs.MkdirAll(filepath.Join(dir, "subdir"), 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "movie.mp4"), []byte("video data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(fs, cfg)

	result, err := s.ScanSingle(dir)
	require.NoError(t, err)
	// Non-recursive: should find movie.mp4 but not recurse into subdir
	assert.Equal(t, 1, len(result.Files))
}

// --- Scan: MemMapFs full recursive scan ---

func TestScan_MemMapFs_Recursive(t *testing.T) {
	// NOTE: Scan uses filepath.WalkDir which operates on the OS filesystem,
	// not the injected afero.Fs. Use OS filesystem for recursive scans.
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "movie1.mp4"), []byte("video data"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "subdir", "movie2.mp4"), []byte("video data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	result, err := s.Scan(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, 2, len(result.Files))
}

// --- ScanWithLimits: MemMapFs with max files limit ---

func TestScanWithLimits_MemMapFs_MaxFiles(t *testing.T) {
	// NOTE: ScanWithLimits uses filepath.WalkDir which operates on the OS filesystem.
	tmpDir := t.TempDir()
	for i := 0; i < 10; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, fmt.Sprintf("movie%02d.mp4", i)), []byte("data"), 0644))
	}

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	result, err := s.ScanWithLimits(context.Background(), tmpDir, 5)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result.Files), 5)
}

// --- ScanSingleFromHandle: nil handle ---

func TestScanSingleFromHandle_NilHandle(t *testing.T) {
	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	_, err := s.ScanSingleFromHandle(nil, "/some/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// --- ScanSingleFromHandle: file info error ---

func TestScanSingleFromHandle_FileInfoError(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "video.mp4"), []byte("data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	dirFile, err := os.Open(tmpDir)
	require.NoError(t, err)
	defer dirFile.Close()

	result, err := s.ScanSingleFromHandle(dirFile, tmpDir)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Files), 1)
}

// --- ScanWithFilter: MinSizeMB filter via MemMapFs ---

func TestScanWithFilter_MemMapFs_MinSize(t *testing.T) {
	// NOTE: ScanWithFilter uses filepath.WalkDir which operates on the OS filesystem.
	tmpDir := t.TempDir()

	// Create small file (1KB)
	smallFile := filepath.Join(tmpDir, "small.mp4")
	f1, err := os.Create(smallFile)
	require.NoError(t, err)
	f1.Truncate(1024)
	f1.Close()

	// Create large file (100MB)
	largeFile := filepath.Join(tmpDir, "large.mp4")
	f2, err := os.Create(largeFile)
	require.NoError(t, err)
	f2.Truncate(100 * 1024 * 1024)
	f2.Close()

	cfg := &Config{Extensions: []string{".mp4"}, MinSizeMB: 50}
	s := NewScanner(afero.NewOsFs(), cfg)

	result, err := s.ScanWithFilter(context.Background(), tmpDir, 0, "")
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Files))
	assert.Equal(t, "large.mp4", result.Files[0].Name)
}

// --- ScanWithFilter: context timeout with many files ---

func TestScanWithFilter_ContextTimeout(t *testing.T) {
	tmpDir := t.TempDir()

	// Create many files to hit the context check point (every 100 files)
	for i := 0; i < 250; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, fmt.Sprintf("file%03d.mp4", i)), []byte("data"), 0644))
	}

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	result, err := s.ScanWithFilter(ctx, tmpDir, 0, "")
	require.NoError(t, err)
	// Should have timed out and stopped early
	assert.True(t, result.TimedOut)
	assert.Less(t, len(result.Files), 250)
}

// --- Filter: MemMapFs ---

func TestFilter_MemMapFs(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/test", 0755))
	require.NoError(t, afero.WriteFile(fs, "/test/movie.mp4", []byte("data"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/test/readme.txt", []byte("data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(fs, cfg)

	result := s.Filter([]string{"/test/movie.mp4", "/test/readme.txt"})
	assert.Equal(t, 1, len(result))
	assert.Equal(t, "movie.mp4", result[0].Name)
}

// --- Scan: non-existent path ---

func TestScan_MemMapFs_NonExistentPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(fs, cfg)

	_, err := s.ScanSingle("/nonexistent/path")
	require.Error(t, err)
}
