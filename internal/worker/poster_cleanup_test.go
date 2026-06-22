package worker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupPosterPaths_RemovesExistingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	file1 := filepath.Join(tmpDir, "poster1.jpg")
	file2 := filepath.Join(tmpDir, "poster2-full.jpg")
	require.NoError(t, os.WriteFile(file1, []byte("img1"), 0644))
	require.NoError(t, os.WriteFile(file2, []byte("img2"), 0644))

	CleanupPosterPaths(afero.NewOsFs(), []string{file1, file2})

	_, err1 := os.Stat(file1)
	_, err2 := os.Stat(file2)
	assert.True(t, os.IsNotExist(err1), "file1 should be removed")
	assert.True(t, os.IsNotExist(err2), "file2 should be removed")
}

func TestCleanupPosterPaths_SkipsNonExistentFiles(t *testing.T) {
	tmpDir := t.TempDir()

	file1 := filepath.Join(tmpDir, "poster1.jpg")
	nonexistent := filepath.Join(tmpDir, "poster_nonexistent.jpg")
	require.NoError(t, os.WriteFile(file1, []byte("img1"), 0644))

	CleanupPosterPaths(afero.NewOsFs(), []string{file1, nonexistent})

	_, err1 := os.Stat(file1)
	assert.True(t, os.IsNotExist(err1), "existing file should be removed")
}

func TestCleanupMoviePosters_RemovesPosterFiles(t *testing.T) {
	tmpDir := t.TempDir()
	jobID := "test-job-123"
	movie := &models.Movie{ID: "ABC-123"}

	posterDir := filepath.Join(tmpDir, "posters", jobID)
	require.NoError(t, os.MkdirAll(posterDir, 0755))

	posterFile := filepath.Join(posterDir, "ABC-123.jpg")
	fullFile := filepath.Join(posterDir, "ABC-123-full.jpg")
	require.NoError(t, os.WriteFile(posterFile, []byte("poster"), 0644))
	require.NoError(t, os.WriteFile(fullFile, []byte("full"), 0644))

	CleanupMoviePosters(afero.NewOsFs(), tmpDir, models.JobID(jobID), movie)

	_, err1 := os.Stat(posterFile)
	_, err2 := os.Stat(fullFile)
	assert.True(t, os.IsNotExist(err1), "poster file should be removed")
	assert.True(t, os.IsNotExist(err2), "full poster file should be removed")
}

func TestCleanupMoviePosters_NilMovie(t *testing.T) {
	CleanupMoviePosters(afero.NewOsFs(), "/tmp", models.JobID("job-1"), nil)
}

func TestCleanupMoviePosters_EmptyMovieID(t *testing.T) {
	movie := &models.Movie{ID: ""}
	CleanupMoviePosters(afero.NewOsFs(), "/tmp", models.JobID("job-1"), movie)
}

func TestCleanupPosterPaths_NilFsUsesOS(t *testing.T) {
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "poster1.jpg")
	require.NoError(t, os.WriteFile(file1, []byte("img1"), 0644))

	CleanupPosterPaths(nil, []string{file1})

	_, err1 := os.Stat(file1)
	assert.True(t, os.IsNotExist(err1), "file should be removed even with nil fs")
}

func TestCleanupPosterPaths_MemMapFs(t *testing.T) {
	memFs := afero.NewMemMapFs()
	posterPath := "/tmp/posters/ABC-123.jpg"
	require.NoError(t, memFs.MkdirAll("/tmp/posters", 0755))
	require.NoError(t, afero.WriteFile(memFs, posterPath, []byte("img"), 0644))

	CleanupPosterPaths(memFs, []string{posterPath})

	_, err := memFs.Stat(posterPath)
	assert.True(t, os.IsNotExist(err), "poster should be removed from MemMapFs")
}

func TestCleanupMoviePosters_MemMapFs(t *testing.T) {
	memFs := afero.NewMemMapFs()
	tmpDir := "/data/temp"
	jobID := "job-001"
	movie := &models.Movie{ID: "ABC-123"}

	posterDir := filepath.Join(tmpDir, "posters", jobID)
	require.NoError(t, memFs.MkdirAll(posterDir, 0755))
	posterFile := filepath.Join(posterDir, "ABC-123.jpg")
	fullFile := filepath.Join(posterDir, "ABC-123-full.jpg")
	require.NoError(t, afero.WriteFile(memFs, posterFile, []byte("poster"), 0644))
	require.NoError(t, afero.WriteFile(memFs, fullFile, []byte("full"), 0644))

	CleanupMoviePosters(memFs, tmpDir, models.JobID(jobID), movie)

	_, err1 := memFs.Stat(posterFile)
	_, err2 := memFs.Stat(fullFile)
	assert.True(t, os.IsNotExist(err1), "poster file should be removed from MemMapFs")
	assert.True(t, os.IsNotExist(err2), "full poster file should be removed from MemMapFs")
}

func TestCleanupPosterPaths_MemMapFs_SkipsNonExistent(t *testing.T) {
	memFs := afero.NewMemMapFs()

	CleanupPosterPaths(memFs, []string{"/nonexistent/path.jpg"})
}

func TestOrphanedPosterPaths_BuildsCorrectPaths(t *testing.T) {
	tmpDir := t.TempDir()
	jobID := "test-job-456"
	cache := NewFSCaseCache(afero.NewMemMapFs())

	paths := OrphanedPosterPaths([]string{"OLD-001"}, "NEW-002", tmpDir, models.JobID(jobID), cache)

	require.Len(t, paths, 2, "should produce 2 paths per orphaned ID")
	assert.Equal(t, filepath.Join(tmpDir, "posters", jobID, "OLD-001.jpg"), paths[0])
	assert.Equal(t, filepath.Join(tmpDir, "posters", jobID, "OLD-001-full.jpg"), paths[1])
}

func TestOrphanedPosterPaths_MultipleOrphanedIDs(t *testing.T) {
	tmpDir := t.TempDir()
	jobID := "test-job-789"
	cache := NewFSCaseCache(afero.NewMemMapFs())

	paths := OrphanedPosterPaths([]string{"OLD-001", "OLD-002"}, "NEW-003", tmpDir, models.JobID(jobID), cache)

	require.Len(t, paths, 4, "should produce 2 paths per orphaned ID (2 IDs = 4 paths)")
	assert.Equal(t, filepath.Join(tmpDir, "posters", jobID, "OLD-001.jpg"), paths[0])
	assert.Equal(t, filepath.Join(tmpDir, "posters", jobID, "OLD-001-full.jpg"), paths[1])
	assert.Equal(t, filepath.Join(tmpDir, "posters", jobID, "OLD-002.jpg"), paths[2])
	assert.Equal(t, filepath.Join(tmpDir, "posters", jobID, "OLD-002-full.jpg"), paths[3])
}

func TestOrphanedPosterPaths_CaseOnlyChangeSkipsOnCaseInsensitiveFS(t *testing.T) {
	cache := NewFSCaseCache(afero.NewMemMapFs())
	tmpDir := t.TempDir()
	jobID := "test-job-case"

	posterDir := filepath.Join(tmpDir, "posters", jobID)
	require.NoError(t, os.MkdirAll(posterDir, 0755))

	paths := OrphanedPosterPaths([]string{"abc-123"}, "ABC-123", tmpDir, models.JobID(jobID), cache)

	isCaseInsensitive := cache.IsCaseInsensitive(posterDir)
	if isCaseInsensitive {
		assert.Empty(t, paths, "case-only change on case-insensitive FS should skip paths")
	} else {
		assert.Len(t, paths, 2, "case-only change on case-sensitive FS should produce paths")
	}
}

func TestOrphanedPosterPaths_EmptyOrphanedIDs(t *testing.T) {
	cache := NewFSCaseCache(afero.NewMemMapFs())
	paths := OrphanedPosterPaths(nil, "NEW-001", "/tmp", models.JobID("job-1"), cache)
	assert.Empty(t, paths, "empty orphaned IDs should produce no paths")
}

func TestOrphanedPosterPaths_NilCacheDefaultsToCaseInsensitive(t *testing.T) {
	paths := OrphanedPosterPaths([]string{"abc-123"}, "ABC-123", "/tmp", models.JobID("job-nil"), nil)
	assert.Empty(t, paths, "nil cache should assume case-insensitive (safe default: no delete)")
}
