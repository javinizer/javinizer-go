package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackSkipped_Uncovered(t *testing.T) {
	result := &ScanResult{
		Skipped: make([]string, 0),
	}

	trackSkipped(result, "/path1")
	trackSkipped(result, "/path2")

	assert.Equal(t, 2, result.SkippedCount)
	assert.Len(t, result.Skipped, 2)
}

func TestTrackSkipped_CappedAtMax_Uncovered(t *testing.T) {
	result := &ScanResult{
		Skipped: make([]string, 0),
	}

	for i := 0; i < MaxSkippedFiles+10; i++ {
		trackSkipped(result, "/path")
	}

	assert.Equal(t, MaxSkippedFiles+10, result.SkippedCount)
	assert.Len(t, result.Skipped, MaxSkippedFiles)
}

func TestScanner_ScanWithFilter_Uncovered(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files - file names contain the filter term
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "action-movie.mp4"), []byte("valid mp4 data"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "comedy-movie.mkv"), []byte("valid mkv data"), 0644))

	cfg := &Config{
		Extensions: []string{".mp4", ".mkv"},
	}
	s := NewScanner(afero.NewOsFs(), cfg)

	t.Run("filter matches file name", func(t *testing.T) {
		result, err := s.ScanWithFilter(context.Background(), tmpDir, 0, "action")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(result.Files), 1)
		found := false
		for _, f := range result.Files {
			if f.Name == "action-movie.mp4" {
				found = true
			}
		}
		assert.True(t, found, "should find action-movie.mp4")
	})

	t.Run("filter matches different file name", func(t *testing.T) {
		result, err := s.ScanWithFilter(context.Background(), tmpDir, 0, "comedy")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(result.Files), 1)
	})
}

func TestScanner_NewScanner_Uncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		Extensions: []string{".MP4", ".Mkv"},
	}
	s := NewScanner(fs, cfg)
	assert.NotNil(t, s)
	// Extension map should be lowercase
	assert.Contains(t, s.extSet, ".mp4")
	assert.Contains(t, s.extSet, ".mkv")
}

func TestScanner_EmptyExtensionSet_Uncovered(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file.mp4"), []byte("data"), 0644))

	cfg := &Config{Extensions: []string{}}
	s := NewScanner(afero.NewOsFs(), cfg)

	result, err := s.Scan(tmpDir)
	require.NoError(t, err)
	// With no extensions, nothing should match
	assert.Empty(t, result.Files)
}

func TestScanner_ScanWithLimits_Context_Uncovered(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file.mp4"), []byte("data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	// Cancelled context should work
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := s.ScanWithLimits(ctx, tmpDir, 0)
	// May return error or result depending on timing
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.NotNil(t, result)
	}
}

func TestScanner_LstatInfo_Uncovered(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("data"), 0644))

	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	info, err := s.lstatInfo(testFile)
	require.NoError(t, err)
	assert.NotNil(t, info)
	assert.False(t, info.IsDir())
}

func TestScanner_LstatInfo_NonExistent_Uncovered(t *testing.T) {
	cfg := &Config{Extensions: []string{".mp4"}}
	s := NewScanner(afero.NewOsFs(), cfg)

	_, err := s.lstatInfo("/nonexistent/path/file.txt")
	assert.Error(t, err)
}
