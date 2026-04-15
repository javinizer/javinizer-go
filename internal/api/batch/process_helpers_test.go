package batch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyTempCroppedPoster(t *testing.T) {
	t.Run("missing temp poster returns empty string", func(t *testing.T) {
		cfg := config.DefaultConfig()
		destDir := t.TempDir()
		job := &worker.BatchJob{ID: "missing-temp-poster"}
		movie := &models.Movie{ID: "IPX-001"}

		result := copyTempCroppedPoster(job, movie, destDir, cfg, "Update", nil)
		assert.Equal(t, "", result)
	})

	t.Run("download poster disabled returns empty string even when temp poster exists", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.DownloadPoster = false

		job := worker.NewJobQueue(nil, "", nil).CreateJob(nil)
		t.Cleanup(func() {
			_ = os.RemoveAll(filepath.Join("data", "temp", "posters", job.ID))
		})

		movie := &models.Movie{ID: "IPX-778"}
		destDir := t.TempDir()

		tempPosterDir := filepath.Join("data", "temp", "posters", job.ID)
		require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
		tempPosterPath := filepath.Join(tempPosterDir, movie.ID+".jpg")
		require.NoError(t, os.WriteFile(tempPosterPath, []byte("poster-bytes"), 0o644))

		result := copyTempCroppedPoster(job, movie, destDir, cfg, "Update", nil)
		assert.Equal(t, "", result)

		files, err := os.ReadDir(destDir)
		require.NoError(t, err)
		assert.Len(t, files, 0)
	})

	t.Run("copies poster using sanitized template output", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.DownloadPoster = true
		cfg.Output.PosterFormat = "<INVALID-TEMPLATE"

		job := worker.NewJobQueue(nil, "", nil).CreateJob(nil)
		t.Cleanup(func() {
			_ = os.RemoveAll(filepath.Join("data", "temp", "posters", job.ID))
		})

		movie := &models.Movie{ID: "IPX-777"}
		destDir := t.TempDir()

		tempPosterDir := filepath.Join("data", "temp", "posters", job.ID)
		require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))

		tempPosterPath := filepath.Join(tempPosterDir, movie.ID+".jpg")
		require.NoError(t, os.WriteFile(tempPosterPath, []byte("poster-bytes"), 0o644))

		multipart := &downloader.MultipartInfo{IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"}
		result := copyTempCroppedPoster(job, movie, destDir, cfg, "Update", multipart)
		require.NotEmpty(t, result)

		files, err := os.ReadDir(destDir)
		require.NoError(t, err)
		require.Len(t, files, 1)

		destPosterPath := filepath.Join(destDir, files[0].Name())
		content, err := os.ReadFile(destPosterPath)
		require.NoError(t, err)
		assert.Equal(t, "poster-bytes", string(content))
	})
}

func TestDownloadMediaFilesWithHistory(t *testing.T) {
	t.Run("downloads media and logs history", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.DownloadCover = true
		cfg.Output.DownloadPoster = false
		cfg.Output.DownloadExtrafanart = false
		cfg.Output.DownloadTrailer = false
		cfg.Output.DownloadActress = false
		cfg.Output.FanartFormat = "<ID>-fanart.jpg"

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/cover.jpg" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte("cover-image"))
		}))
		defer server.Close()

		deps := createTestDeps(t, cfg, "")
		historyLogger := history.NewLogger(deps.DB)
		dl := downloader.NewDownloaderWithNFOConfig(server.Client(), afero.NewOsFs(), &cfg.Output, "test-agent", false, true, nil)

		movie := &models.Movie{
			ID:       "IPX-900",
			CoverURL: server.URL + "/cover.jpg",
		}
		destDir := t.TempDir()

		downloadMediaFilesWithHistory(context.Background(), dl, movie, destDir, cfg, historyLogger, nil)

		coverPath := filepath.Join(destDir, "IPX-900-fanart.jpg")
		_, err := os.Stat(coverPath)
		require.NoError(t, err)

		records, err := historyLogger.GetByMovieID(movie.ID)
		require.NoError(t, err)
		downloadCount := 0
		for _, record := range records {
			if record.Operation == "download" {
				downloadCount++
			}
		}
		assert.Equal(t, 1, downloadCount)
	})

	t.Run("no urls skips history logging", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.DownloadCover = false
		cfg.Output.DownloadPoster = false
		cfg.Output.DownloadExtrafanart = false
		cfg.Output.DownloadTrailer = false
		cfg.Output.DownloadActress = false

		deps := createTestDeps(t, cfg, "")
		historyLogger := history.NewLogger(deps.DB)
		dl := downloader.NewDownloaderWithNFOConfig(http.DefaultClient, afero.NewOsFs(), &cfg.Output, "test-agent", false, true, nil)

		movie := &models.Movie{ID: "IPX-901"}
		downloadMediaFilesWithHistory(context.Background(), dl, movie, t.TempDir(), cfg, historyLogger, nil)

		records, err := historyLogger.GetByMovieID(movie.ID)
		require.NoError(t, err)
		assert.Len(t, records, 0)
	})
}

func TestProcessUpdateMode_NoCompletedResults(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig()
	deps := createTestDeps(t, cfg, "")
	job := deps.JobQueue.CreateJob([]string{"/tmp/fail.mp4"})
	job.UpdateFileResult("/tmp/fail.mp4", &worker.FileResult{
		FilePath: "/tmp/fail.mp4",
		Status:   worker.JobStatusFailed,
		Error:    "scrape failed",
	})

	processUpdateMode(job, cfg, deps.DB, deps.Registry, context.Background(), nil)

	status := job.GetStatus()
	assert.Equal(t, worker.JobStatusCompleted, status.Status)
	assert.Equal(t, 100.0, status.Progress)
}

func TestProcessOrganizeJob_InvalidLinkModeMarksFailed(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig()
	deps := createTestDeps(t, cfg, "")
	job := deps.JobQueue.CreateJob(nil)

	processOrganizeJob(context.Background(), job, deps.JobQueue, t.TempDir(), false, "not-a-valid-link-mode", deps.DB, cfg, deps.Registry, nil)

	status := job.GetStatus()
	assert.Equal(t, worker.JobStatusFailed, status.Status)
}

func TestProcessUpdateMode_SuccessfulResults(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig()
	deps := createTestDeps(t, cfg, "")
	job := deps.JobQueue.CreateJob([]string{"/tmp/test.mp4"})

	// Simulate a successful scrape with movie data
	movie := &models.Movie{
		ID:       "IPX-123",
		Title:    "Test Movie IPX-123",
		CoverURL: "https://example.com/cover.jpg",
	}
	job.UpdateFileResult("/tmp/test.mp4", &worker.FileResult{
		FilePath: "/tmp/test.mp4",
		Status:   worker.JobStatusCompleted,
		Data:     movie,
	})

	cfg.Output.DownloadCover = false
	cfg.Output.DownloadPoster = false

	processUpdateMode(job, cfg, deps.DB, deps.Registry, context.Background(), nil)

	status := job.GetStatus()
	assert.Equal(t, worker.JobStatusCompleted, status.Status)
	assert.Equal(t, 100.0, status.Progress)
}

func TestProcessUpdateMode_MixedResults(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig()
	deps := createTestDeps(t, cfg, "")
	job := deps.JobQueue.CreateJob([]string{"/tmp/success.mp4", "/tmp/fail.mp4"})

	// First file successful
	movie := &models.Movie{
		ID:       "IPX-123",
		Title:    "Test Movie IPX-123",
		CoverURL: "https://example.com/cover.jpg",
	}
	job.UpdateFileResult("/tmp/success.mp4", &worker.FileResult{
		FilePath: "/tmp/success.mp4",
		Status:   worker.JobStatusCompleted,
		Data:     movie,
	})

	// Second file failed
	job.UpdateFileResult("/tmp/fail.mp4", &worker.FileResult{
		FilePath: "/tmp/fail.mp4",
		Status:   worker.JobStatusFailed,
		Error:    "scrape failed",
	})

	cfg.Output.DownloadCover = false
	cfg.Output.DownloadPoster = false

	processUpdateMode(job, cfg, deps.DB, deps.Registry, context.Background(), nil)

	status := job.GetStatus()
	assert.Equal(t, worker.JobStatusCompleted, status.Status)
	// Should complete with partial success
	assert.Equal(t, 100.0, status.Progress)
}
