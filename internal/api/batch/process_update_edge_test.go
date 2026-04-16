package batch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
