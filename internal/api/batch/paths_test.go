package batch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPathWithin(t *testing.T) {
	tests := []struct {
		name string
		path string
		base string
		want bool
	}{
		{"same path", "/media/videos", "/media/videos", true},
		{"direct child", "/media/videos/anime", "/media/videos", true},
		{"deep child", "/media/videos/anime/2024", "/media/videos", true},
		{"sibling is not within", "/media/photos", "/media/videos", false},
		{"parent is not within", "/media", "/media/videos", false},
		{"traversal attempt", "/media/../etc/passwd", "/media", false},
		{"dot-prefixed child is within", "/media/videos/.Others", "/media/videos", true},
		{"dot-dot child is within", "/media/videos/..hidden", "/media/videos", true},
		{"dot-dot-slash traversal is not within", "/media/../etc", "/media", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, core.IsPathWithin(tt.path, tt.base))
		})
	}
}

func TestIsDirAllowed_OsFs(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	require.NoError(t, fs.MkdirAll(filepath.Join(dir, "videos"), 0o755))

	t.Run("allowed directory", func(t *testing.T) {
		result := isDirAllowed(fs, dir, &core.SecurityNarrowConfig{AllowedDirectories: []string{dir}})
		assert.True(t, result)
	})

	t.Run("no allow list denies all", func(t *testing.T) {
		result := isDirAllowed(fs, dir, &core.SecurityNarrowConfig{})
		assert.False(t, result)
	})
}

func TestCleanupJobTempPosters_MemMapFs(t *testing.T) {
	fs := afero.NewMemMapFs()
	posterDir := filepath.Join("/tmp/javinizer", "posters", "job-123")
	require.NoError(t, fs.MkdirAll(posterDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(posterDir, "poster.jpg"), []byte("img"), 0o644))

	cleanupJobTempPosters(fs, "job-123", "/tmp/javinizer")

	exists, err := afero.Exists(fs, posterDir)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestCleanupJobTempPosters_NonExistentDir(t *testing.T) {
	fs := afero.NewMemMapFs()

	cleanupJobTempPosters(fs, "missing-job", "/tmp/javinizer")

	_, err := fs.Stat(filepath.Join("/tmp/javinizer", "posters", "missing-job"))
	assert.True(t, os.IsNotExist(err))
}
