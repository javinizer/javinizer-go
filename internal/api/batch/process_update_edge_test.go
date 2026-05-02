package batch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessUpdateMode_MalformedExistingNFOAndDownloadFailure(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig()
	cfg.Output.DownloadTimeout = 1
	cfg.Output.DownloadCover = true
	cfg.Output.DownloadPoster = false
	cfg.Output.DownloadExtrafanart = false
	cfg.Output.DownloadTrailer = false
	cfg.Output.DownloadActress = false
	cfg.Metadata.NFO.FilenameTemplate = "<ID>.nfo"

	deps := createTestDeps(t, cfg, "")
	sourceDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "IPX-998.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "IPX-998.nfo"), []byte("<movie>"), 0o644))

	job := deps.JobQueue.CreateJob([]string{filePath})
	job.UpdateFileResult(filePath, &worker.FileResult{
		FilePath: filePath,
		MovieID:  "IPX-998",
		Status:   worker.JobStatusCompleted,
		Data: &models.Movie{
			ID:       "IPX-998",
			Title:    "Malformed Existing NFO",
			CoverURL: "http://127.0.0.1:1/unreachable.jpg",
		},
	})

	processUpdateMode(job, cfg, deps.DB, deps.Registry, context.Background(), nil, &UpdateOptions{})

	status := job.GetStatus()
	require.Equal(t, worker.JobStatusCompleted, status.Status)

	parsed, err := nfo.ParseNFO(afero.NewOsFs(), filepath.Join(sourceDir, "IPX-998.nfo"))
	require.NoError(t, err)
	assert.Equal(t, "IPX-998", parsed.Movie.ID)
}

func TestProcessUpdateMode_NFOFilenameFallbacks(t *testing.T) {
	t.Run("invalid template falls back to movie id", func(t *testing.T) {
		initTestWebSocket(t)

		cfg := config.DefaultConfig()
		cfg.Output.DownloadCover = false
		cfg.Output.DownloadPoster = false
		cfg.Output.DownloadExtrafanart = false
		cfg.Output.DownloadTrailer = false
		cfg.Output.DownloadActress = false
		cfg.Metadata.NFO.FilenameTemplate = "<ID"

		deps := createTestDeps(t, cfg, "")
		sourceDir := t.TempDir()
		filePath := filepath.Join(sourceDir, "IPX-889.mp4")
		require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o644))

		job := deps.JobQueue.CreateJob([]string{filePath})
		job.UpdateFileResult(filePath, &worker.FileResult{
			FilePath: filePath,
			MovieID:  "IPX-889",
			Status:   worker.JobStatusCompleted,
			Data:     &models.Movie{ID: "IPX-889", Title: "Template Error"},
		})

		processUpdateMode(job, cfg, deps.DB, deps.Registry, context.Background(), nil, &UpdateOptions{})
		require.Equal(t, worker.JobStatusCompleted, job.GetStatus().Status)
		entries, err := os.ReadDir(sourceDir)
		require.NoError(t, err)
		hasNFO := false
		for _, entry := range entries {
			if strings.HasSuffix(strings.ToLower(entry.Name()), ".nfo") {
				hasNFO = true
				break
			}
		}
		assert.True(t, hasNFO)
	})

	t.Run("empty sanitized template output falls back to movie id", func(t *testing.T) {
		initTestWebSocket(t)

		cfg := config.DefaultConfig()
		cfg.Output.DownloadCover = false
		cfg.Output.DownloadPoster = false
		cfg.Output.DownloadExtrafanart = false
		cfg.Output.DownloadTrailer = false
		cfg.Output.DownloadActress = false
		cfg.Metadata.NFO.FilenameTemplate = "<Title>"

		deps := createTestDeps(t, cfg, "")
		sourceDir := t.TempDir()
		filePath := filepath.Join(sourceDir, "IPX-890.mp4")
		require.NoError(t, os.WriteFile(filePath, []byte("video"), 0o644))

		job := deps.JobQueue.CreateJob([]string{filePath})
		job.UpdateFileResult(filePath, &worker.FileResult{
			FilePath: filePath,
			MovieID:  "IPX-890",
			Status:   worker.JobStatusCompleted,
			Data:     &models.Movie{ID: "IPX-890", Title: "///"},
		})

		processUpdateMode(job, cfg, deps.DB, deps.Registry, context.Background(), nil, &UpdateOptions{})
		require.Equal(t, worker.JobStatusCompleted, job.GetStatus().Status)
		entries, err := os.ReadDir(sourceDir)
		require.NoError(t, err)
		hasNFO := false
		for _, entry := range entries {
			if strings.HasSuffix(strings.ToLower(entry.Name()), ".nfo") {
				hasNFO = true
				break
			}
		}
		assert.True(t, hasNFO)
	})
}

func TestCalcFileTimeout(t *testing.T) {
	t.Run("default when no worker timeout", func(t *testing.T) {
		assert.Equal(t, 120*time.Second, calcFileTimeout(0, 10))
	})

	t.Run("default when no results", func(t *testing.T) {
		assert.Equal(t, 120*time.Second, calcFileTimeout(300, 0))
	})

	t.Run("divides timeout by results", func(t *testing.T) {
		assert.Equal(t, 31*time.Second, calcFileTimeout(300, 10))
	})

	t.Run("minimum 30 seconds", func(t *testing.T) {
		assert.Equal(t, 30*time.Second, calcFileTimeout(30, 10))
	})

	t.Run("maximum 600 seconds", func(t *testing.T) {
		assert.Equal(t, 600*time.Second, calcFileTimeout(36000, 10))
	})
}

func TestApplyMultipartMetadata(t *testing.T) {
	t.Run("applies multipart info when present", func(t *testing.T) {
		job := &worker.BatchJob{
			FileMatchInfo: map[string]worker.FileMatchInfo{
				"/test/file.mp4": {IsMultiPart: true, PartNumber: 2, PartSuffix: "pt2"},
			},
		}
		result := &worker.FileResult{}
		applyMultipartMetadata(job, result, "/test/file.mp4")
		assert.True(t, result.IsMultiPart)
		assert.Equal(t, 2, result.PartNumber)
		assert.Equal(t, "pt2", result.PartSuffix)
	})

	t.Run("no change when file not in match info", func(t *testing.T) {
		job := &worker.BatchJob{FileMatchInfo: map[string]worker.FileMatchInfo{}}
		result := &worker.FileResult{}
		applyMultipartMetadata(job, result, "/test/file.mp4")
		assert.False(t, result.IsMultiPart)
		assert.Equal(t, 0, result.PartNumber)
		assert.Equal(t, "", result.PartSuffix)
	})
}

func TestExtractCurrentMovieID(t *testing.T) {
	t.Run("returns ID from result data movie", func(t *testing.T) {
		job := &worker.BatchJob{
			Results: map[string]*worker.FileResult{
				"/test/file.mp4": {
					Data: &models.Movie{ID: "IPX-123"},
				},
			},
		}
		assert.Equal(t, "IPX-123", extractCurrentMovieID(job, "/test/file.mp4"))
	})

	t.Run("returns MovieID when no data", func(t *testing.T) {
		job := &worker.BatchJob{
			Results: map[string]*worker.FileResult{
				"/test/file.mp4": {MovieID: "ABC-456"},
			},
		}
		assert.Equal(t, "ABC-456", extractCurrentMovieID(job, "/test/file.mp4"))
	})

	t.Run("returns empty when no result", func(t *testing.T) {
		job := &worker.BatchJob{Results: map[string]*worker.FileResult{}}
		assert.Equal(t, "", extractCurrentMovieID(job, "/test/file.mp4"))
	})
}

func TestOtherResultUsesMovieID(t *testing.T) {
	t.Run("returns true when other result matches by MovieID", func(t *testing.T) {
		job := &worker.BatchJob{
			Results: map[string]*worker.FileResult{
				"/test/a.mp4": {MovieID: "IPX-123"},
				"/test/b.mp4": {MovieID: "IPX-456"},
			},
		}
		assert.True(t, otherResultUsesMovieID(job, "/test/b.mp4", "IPX-123"))
	})

	t.Run("returns true when other result matches by Data movie ID", func(t *testing.T) {
		job := &worker.BatchJob{
			Results: map[string]*worker.FileResult{
				"/test/a.mp4": {Data: &models.Movie{ID: "IPX-789"}},
				"/test/b.mp4": {MovieID: "other"},
			},
		}
		assert.True(t, otherResultUsesMovieID(job, "/test/b.mp4", "ipx-789"))
	})

	t.Run("returns false when no other result matches", func(t *testing.T) {
		job := &worker.BatchJob{
			Results: map[string]*worker.FileResult{
				"/test/a.mp4": {MovieID: "IPX-123"},
			},
		}
		assert.False(t, otherResultUsesMovieID(job, "/test/a.mp4", "IPX-123"))
	})

	t.Run("returns false when empty results", func(t *testing.T) {
		job := &worker.BatchJob{Results: map[string]*worker.FileResult{}}
		assert.False(t, otherResultUsesMovieID(job, "/test/a.mp4", "IPX-123"))
	})
}
