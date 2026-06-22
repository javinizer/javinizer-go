package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

type noPosterStubScraper struct {
}

func (s *noPosterStubScraper) Name() string { return "stub-no-poster" }

func (s *noPosterStubScraper) Search(_ context.Context, id string) (*models.ScraperResult, error) {
	releaseDate, _ := time.Parse("2006-01-02", "2024-01-15")
	return &models.ScraperResult{
		Source:        s.Name(),
		ID:            id,
		ContentID:     id,
		Title:         "Rescrape Edge Test",
		OriginalTitle: "Rescrape Edge Test",
		ReleaseDate:   &releaseDate,
		Actresses:     []models.ActressInfo{{FirstName: "Edge", LastName: "Case"}},
		Genres:        []string{"Test"},
	}, nil
}

func (s *noPosterStubScraper) GetURL(_ context.Context, id string) (string, error) {
	return "https://example.invalid/" + id, nil
}

func (s *noPosterStubScraper) IsEnabled() bool { return true }

func (s *noPosterStubScraper) Close() error { return nil }

func (s *noPosterStubScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}

func writeJPEG(t *testing.T, path string, width, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 120, G: 90, B: 40, A: 255})
		}
	}
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, jpeg.Encode(f, img, &jpeg.Options{Quality: 90}))
	require.NoError(t, f.Close())
}

func TestRescrapeBatchMovie_EdgePaths(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	t.Run("invalid preset rejected at API boundary", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-901.mp4"})
		setJobResult(job, "/tmp/IPX-901.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-901.mp4", MovieID: "IPX-901"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-901"},
		})

		router := gin.New()
		router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers: []string{"stub-no-poster"},
			Preset:           "invalid",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-901/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		// API layer now validates presets at the boundary — invalid presets
		// are rejected with HTTP 400 before reaching the pipeline.
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("scraper failure returns internal error", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-902.mp4"})
		setJobResult(job, "/tmp/IPX-902.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-902.mp4", MovieID: "IPX-902"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-902"},
		})

		router := gin.New()
		router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"missing-scraper"},
			ManualSearchInput: "IPX-902",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-902/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Contains(t, rec.Body.String(), "Rescrape failed")
	})

	t.Run("successful rescrape updates job state", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})

		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-903.mp4"})
		setJobResult(job, "/tmp/IPX-903.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-903.mp4", MovieID: "IPX-903"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-903", Title: "Old Title"},
		})

		router := gin.New()
		router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"stub-no-poster"},
			ManualSearchInput: "IPX-903",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-903/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var resp contracts.BatchRescrapeResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.NotNil(t, resp.Movie)
		assert.Equal(t, "IPX-903", resp.Movie.ID)
		assert.Equal(t, "Rescrape Edge Test", resp.Movie.Title)

		status := job.GetStatus()
		updated := status.Results["/tmp/IPX-903.mp4"]
		require.NotNil(t, updated)
		assert.Equal(t, models.JobStatusCompleted, updated.Status)
		assert.Equal(t, "IPX-903", updated.FileMatchInfo.MovieID)
	})
}

func TestUpdateBatchMoviePosterCrop_EdgePaths(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	t.Run("rejects invalid movie id path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/batch/job-any/movies/../bad/poster-crop", bytes.NewBufferString(`{"x":1,"y":1,"width":10,"height":10}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("rejects invalid body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/batch/missing/results/IPX-100/poster-crop", bytes.NewBufferString("{bad-json"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("job not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/batch/does-not-exist/results/IPX-100/poster-crop", bytes.NewBufferString(`{"x":0,"y":0,"width":10,"height":10}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Contains(t, rec.Body.String(), "Job not found")
	})

	t.Run("invalid poster id derived from movie data", func(t *testing.T) {
		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-777.mp4"})
		setJobResult(job, "/tmp/IPX-777.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-777.mp4", MovieID: "IPX-777"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "../bad"},
		})

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-777/poster-crop", bytes.NewBufferString(`{"x":0,"y":0,"width":10,"height":10}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid movie ID for poster operation")
	})

	t.Run("falls back to existing cropped image when full image is missing", func(t *testing.T) {
		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-778.mp4"})
		setJobResult(job, "/tmp/IPX-778.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-778.mp4", MovieID: "IPX-778"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-778", Title: "Fallback Crop"},
		})

		posterDir := filepath.Join("data", "temp", "posters", job.GetID())
		require.NoError(t, os.MkdirAll(posterDir, 0o755))
		writeJPEG(t, filepath.Join(posterDir, "IPX-778.jpg"), 900, 600)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-778/poster-crop", bytes.NewBufferString(`{"x":100,"y":0,"width":472,"height":600}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "/api/v1/temp/posters/"+job.GetID()+"/IPX-778.jpg")
	})

	t.Run("movie lookup fallback by data movie id", func(t *testing.T) {
		job := createJobWithWF(deps, cfg, []string{"/tmp/ALT-001.mp4"})
		setJobResult(job, "/tmp/ALT-001.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/ALT-001.mp4", MovieID: "LEGACY-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ALT-001", Title: "Movie ID Fallback"},
		})

		posterDir := filepath.Join("data", "temp", "posters", job.GetID())
		require.NoError(t, os.MkdirAll(posterDir, 0o755))
		writeJPEG(t, filepath.Join(posterDir, "ALT-001-full.jpg"), 1000, 600)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/LEGACY-001/poster-crop", bytes.NewBufferString(`{"x":200,"y":0,"width":472,"height":600}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		status := job.GetStatus()
		result := status.Results["/tmp/ALT-001.mp4"]
		require.NotNil(t, result)
		require.NotNil(t, result.Movie)
		assert.Equal(t, "ALT-001", result.FileMatchInfo.MovieID)
		assert.Contains(t, result.Movie.Poster.CroppedPosterURL, "/api/v1/temp/posters/"+job.GetID()+"/ALT-001.jpg")
		assert.False(t, result.Movie.Poster.ShouldCropPoster)
	})
}
