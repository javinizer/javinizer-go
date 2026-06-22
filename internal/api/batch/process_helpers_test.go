package batch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func copyTempCroppedPoster(job *worker.BatchJob, movie *models.Movie, destDir string, cfg *config.Config, mode string, multipart *downloader.MultipartInfo) string {
	if !cfg.Output.Download.DownloadPoster {
		logging.Debugf("%s mode: Poster download disabled, skipping temp poster copy for %s", mode, movie.ID)
		return ""
	}

	tempPosterPath := filepath.Join(cfg.System.TempDir, "posters", job.ID.String(), movie.ID+".jpg")
	if _, err := os.Stat(tempPosterPath); err != nil {
		return ""
	}

	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = cfg.Output.Operation.GroupActress
	if multipart != nil {
		ctx.IsMultiPart = multipart.IsMultiPart
		ctx.PartNumber = multipart.PartNumber
		ctx.PartSuffix = multipart.PartSuffix
	}
	engine := job.TemplateEngine()
	posterFilename, err := engine.Execute(cfg.Output.MediaFormat.PosterFormat, ctx)
	if err != nil {
		posterFilename = fmt.Sprintf("%s-poster.jpg", movie.ID)
		logging.Warnf("%s mode: Template execution failed, using fallback filename: %v", mode, err)
	}

	posterFilename = template.SanitizeFilename(posterFilename)
	if posterFilename == "" {
		posterFilename = fmt.Sprintf("%s-poster.jpg", template.SanitizeFilename(movie.ID))
	}

	destPosterPath := filepath.Join(destDir, posterFilename)

	if err := fsutil.CopyFileFs(afero.NewOsFs(), tempPosterPath, destPosterPath); err != nil {
		logging.Warnf("[post-move] mode=%s movie=%s stage=temp_poster_copy src=%s dst=%s err=%v", mode, movie.ID, tempPosterPath, destPosterPath, err)
		return ""
	}

	logging.Infof("%s mode: Copied cropped poster from temp to %s", mode, destPosterPath)
	return destPosterPath
}

func TestCopyTempCroppedPoster(t *testing.T) {
	t.Run("missing temp poster returns empty string", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		destDir := t.TempDir()
		job := &worker.BatchJob{ID: "missing-temp-poster"}
		movie := &models.Movie{ID: "IPX-001"}

		result := copyTempCroppedPoster(job, movie, destDir, cfg, "Update", nil)
		assert.Equal(t, "", result)
	})

	t.Run("download poster disabled returns empty string even when temp poster exists", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		cfg.Output.Download.DownloadPoster = false

		job := worker.NewJobStore(nil, nil, nil, "", nil, nil).CreateJobBatch(nil)
		t.Cleanup(func() {
			_ = os.RemoveAll(filepath.Join("data", "temp", "posters", job.ID.String()))
		})

		movie := &models.Movie{ID: "IPX-778"}
		destDir := t.TempDir()

		tempPosterDir := filepath.Join("data", "temp", "posters", job.ID.String())
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
		cfg := config.DefaultConfig(nil, nil)
		cfg.Output.Download.DownloadPoster = true
		cfg.Output.MediaFormat.PosterFormat = "<INVALID-TEMPLATE"

		job := worker.NewJobStore(nil, nil, nil, "", nil, nil).CreateJobBatch(nil)
		t.Cleanup(func() {
			_ = os.RemoveAll(filepath.Join("data", "temp", "posters", job.ID.String()))
		})

		movie := &models.Movie{ID: "IPX-777"}
		destDir := t.TempDir()

		tempPosterDir := filepath.Join("data", "temp", "posters", job.ID.String())
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

func TestProcessUpdateMode_NoCompletedResults(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/tmp/fail.mp4"})
	setJobResult(job, "/tmp/fail.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/fail.mp4"},
		Status:        models.JobStatusFailed,
		Error:         "scrape failed",
	})

	testStartUpdateApply(context.Background(), job, cfg, deps.CoreDeps.DB, deps.CoreDeps.ScraperRegistry, nil, &updateOptions{})

	status := job.GetStatus()
	assert.Equal(t, models.JobStatusCompleted, status.Status)
	assert.Equal(t, 100.0, status.Progress)
}

func TestProcessOrganizeJob_InvalidLinkModeMarksFailed(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch(nil)

	testStartOrganizeApply(context.Background(), job, deps.JobStore, t.TempDir(), false, "not-a-valid-link-mode", false, false, deps.CoreDeps.DB, cfg, deps.CoreDeps.ScraperRegistry, nil)

	status := job.GetStatus()
	assert.Equal(t, models.JobStatusFailed, status.Status)
}

func TestProcessUpdateMode_SuccessfulResults(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/tmp/test.mp4"})

	// Simulate a successful scrape with movie data
	movie := &models.Movie{
		ID:     "IPX-123",
		Title:  "Test Movie IPX-123",
		Poster: models.PosterState{CoverURL: "https://example.com/cover.jpg"},
	}
	setJobResult(job, "/tmp/test.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/test.mp4"},
		Status:        models.JobStatusCompleted,
		Movie:         movie,
	})

	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false

	testStartUpdateApply(context.Background(), job, cfg, deps.CoreDeps.DB, deps.CoreDeps.ScraperRegistry, nil, &updateOptions{})

	status := job.GetStatus()
	assert.Equal(t, models.JobStatusCompleted, status.Status)
	assert.Equal(t, 100.0, status.Progress)
}

func TestProcessUpdateMode_MixedResults(t *testing.T) {
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/tmp/success.mp4", "/tmp/fail.mp4"})

	// First file successful
	movie := &models.Movie{
		ID:     "IPX-123",
		Title:  "Test Movie IPX-123",
		Poster: models.PosterState{CoverURL: "https://example.com/cover.jpg"},
	}
	setJobResult(job, "/tmp/success.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/success.mp4"},
		Status:        models.JobStatusCompleted,
		Movie:         movie,
	})

	// Second file failed
	setJobResult(job, "/tmp/fail.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/fail.mp4"},
		Status:        models.JobStatusFailed,
		Error:         "scrape failed",
	})

	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false

	testStartUpdateApply(context.Background(), job, cfg, deps.CoreDeps.DB, deps.CoreDeps.ScraperRegistry, nil, &updateOptions{})

	status := job.GetStatus()
	assert.Equal(t, models.JobStatusCompleted, status.Status)
	assert.Equal(t, 100.0, status.Progress)
}
