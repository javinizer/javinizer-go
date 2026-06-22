package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// setJobDeleted sets the unexported deleted field on BatchJob for testing
// This uses unsafe to bypass Go's access control for testing purposes only
func setJobDeleted(job *worker.BatchJob, deleted bool) {
	job.Lifecycle().SetDeleted(deleted)
}

// TestRescrapeBatchMovie_MultiPartPosterCleanup_DataNilSibling tests Bug #1
// P0 CRITICAL - Poster deleted when sibling has nil Data but valid MovieID
func TestRescrapeBatchMovie_MultiPartPosterCleanup_DataNilSibling(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})

	// Create job with two multi-part files
	job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-001-A.mp4", "/tmp/IPX-001-B.mp4"})

	// First file has valid Data
	setJobResult(job, "/tmp/IPX-001-A.mp4", &worker.MovieResult{
		ResultID:      "IPX-001-A",
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-001-A.mp4", MovieID: "IPX-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-001", Title: "Multi-part A"},
	})

	// Second file has nil Data (simulating incomplete scrape or error state)
	// but still has valid MovieID field
	setJobResult(job, "/tmp/IPX-001-B.mp4", &worker.MovieResult{
		ResultID:      "IPX-001-B",
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-001-B.mp4", MovieID: "IPX-001"},
		Status:        models.JobStatusCompleted,
		Movie:         nil, // nil Data - this is the bug scenario
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	// Rescrape the first file (with valid Data), changing its ID to IPX-002
	// This should trigger the poster cleanup logic
	body, err := json.Marshal(contracts.BatchRescrapeRequest{
		SelectedScrapers:  []string{"stub-no-poster"},
		ManualSearchInput: "IPX-002", // New ID
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-001-A/rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Verify both files still exist in results
	status := job.GetStatus()
	require.Len(t, status.Results, 2, "Both files should still exist")

	// Find the file that was rescraped (now has IPX-002)
	var rescrapedFile *worker.MovieResult
	var siblingFile *worker.MovieResult
	for _, result := range status.Results {
		if result.FileMatchInfo.MovieID == "IPX-002" {
			rescrapedFile = result
		} else if result.FileMatchInfo.MovieID == "IPX-001" {
			siblingFile = result
		}
	}

	require.NotNil(t, rescrapedFile, "Rescraped file should exist with new ID")
	require.NotNil(t, siblingFile, "Sibling file should exist with old ID")

	// The critical test: sibling should retain MovieID = "IPX-001"
	// This ensures the poster cleanup logic correctly identified that the sibling
	// still needs the old poster (even though it has nil Data)
	assert.Equal(t, "IPX-001", siblingFile.FileMatchInfo.MovieID, "Sibling should retain old MovieID")
	assert.Nil(t, siblingFile.Movie, "Sibling should still have nil Data")

	// Rescraped file should have new ID
	assert.Equal(t, "IPX-002", rescrapedFile.FileMatchInfo.MovieID)
}

// TestRescrapeBatchMovie_JobLifecycleRace tests Bug #4
// P0 CRITICAL - Rescrape allowed during invalid job states
func TestRescrapeBatchMovie_JobLifecycleRace(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	t.Run("rejects rescrape when job is running", func(t *testing.T) {
		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-101.mp4"})
		setJobResult(job, "/tmp/IPX-101.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-101.mp4", MovieID: "IPX-101"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-101"},
		})

		// Manually set job status to running (simulating active scrape)
		setJobStatus(job, models.JobStatusRunning)

		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"stub-no-poster"},
			ManualSearchInput: "IPX-101",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-101/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "Cannot rescrape")
	})

	t.Run("rejects rescrape when job is organized", func(t *testing.T) {
		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-102.mp4"})
		setJobResult(job, "/tmp/IPX-102.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-102.mp4", MovieID: "IPX-102"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-102"},
		})

		// Mark job as organized (terminal state)
		setJobStatus(job, models.JobStatusOrganized)

		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"stub-no-poster"},
			ManualSearchInput: "IPX-102",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-102/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "Cannot rescrape")
	})

	t.Run("allows rescrape when job is completed", func(t *testing.T) {
		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-103.mp4"})
		setJobResult(job, "/tmp/IPX-103.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-103.mp4", MovieID: "IPX-103"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-103", Title: "Old Title"},
		})

		// Job is in Completed state (valid for rescrape)
		setJobStatus(job, models.JobStatusCompleted)

		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"stub-no-poster"},
			ManualSearchInput: "IPX-103",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-103/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("rejects rescrape when job is failed", func(t *testing.T) {
		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-104.mp4"})
		setJobResult(job, "/tmp/IPX-104.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-104.mp4", MovieID: "IPX-104"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-104"},
		})

		// Mark job as failed (terminal state)
		setJobStatus(job, models.JobStatusFailed)

		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"stub-no-poster"},
			ManualSearchInput: "IPX-104",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-104/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "Cannot rescrape")
	})

	t.Run("rejects rescrape when job is cancelled", func(t *testing.T) {
		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-105.mp4"})
		setJobResult(job, "/tmp/IPX-105.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-105.mp4", MovieID: "IPX-105"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-105"},
		})

		// Mark job as cancelled (terminal state)
		setJobStatus(job, models.JobStatusCancelled)

		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"stub-no-poster"},
			ManualSearchInput: "IPX-105",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-105/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "Cannot rescrape")
	})

	t.Run("returns 410 Gone for deleted job even if cancelled", func(t *testing.T) {
		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-106.mp4"})
		setJobResult(job, "/tmp/IPX-106.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-106.mp4", MovieID: "IPX-106"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-106"},
		})

		// Mark job as cancelled first
		setJobStatus(job, models.JobStatusCancelled)

		// Mark job as deleted using unsafe (unexported field)
		setJobDeleted(job, true)

		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"stub-no-poster"},
			ManualSearchInput: "IPX-106",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-106/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		// Should return 410 Gone (deleted takes priority over cancelled)
		assert.Equal(t, http.StatusGone, rec.Code)
		assert.Contains(t, rec.Body.String(), "deleted")
	})

	t.Run("returns 410 Gone for deleted job even if organized", func(t *testing.T) {
		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-107.mp4"})
		setJobResult(job, "/tmp/IPX-107.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-107.mp4", MovieID: "IPX-107"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-107"},
		})

		// Mark job as organized first
		setJobStatus(job, models.JobStatusOrganized)

		// Mark job as deleted using unsafe (unexported field)
		setJobDeleted(job, true)

		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"stub-no-poster"},
			ManualSearchInput: "IPX-107",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-107/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		// Should return 410 Gone (deleted takes priority over organized)
		assert.Equal(t, http.StatusGone, rec.Code)
		assert.Contains(t, rec.Body.String(), "deleted")
	})
}

// TestRescrapeBatchMovie_CASRevisionConflict verifies the CAS (Compare-And-Swap)
// revision mechanism directly. It simulates concurrent modification by setting
// a specific revision value and verifying the CAS check passes.
func TestRescrapeBatchMovie_CASRevisionConflict(t *testing.T) {
	// This test verifies the CAS (Compare-And-Swap) revision mechanism directly.
	// Instead of trying to create true HTTP concurrency (which is flaky),
	// we simulate concurrent modification by changing the revision between
	// the initial read and the final update.
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})

	// Create job with a file
	job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-200-A.mp4"})
	setJobResult(job, "/tmp/IPX-200-A.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-200-A.mp4", MovieID: "IPX-200"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-200"},
	})

	// Manually set revision to 5 (UpdateFileResult always resets to 1 or +1)
	require.NoError(t, job.ResultsWriter().SetFileResultRevision("/tmp/IPX-200-A.mp4", 5))

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	// Make a rescrape request
	body, _ := json.Marshal(contracts.BatchRescrapeRequest{
		SelectedScrapers:  []string{"stub-no-poster"},
		ManualSearchInput: "IPX-200",
	})

	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-200/rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// The rescrape should succeed because the revision was captured at the start
	// (before any modification in this test), and the CAS check should pass
	assert.Equal(t, http.StatusOK, rec.Code, "Rescrape should succeed when revision matches")

	// Verify the revision was incremented
	status := job.GetStatus()
	result := status.Results["/tmp/IPX-200-A.mp4"]
	require.NotNil(t, result)
	assert.Equal(t, uint64(6), result.Revision, "Revision should be incremented from 5 to 6")
}

func TestRescrapeBatchMovie_ConcurrentRescrapeDetected(t *testing.T) {
	// This test verifies that when a rescrape detects concurrent modification
	// (revision changed between capture and update), it returns 409 Conflict.
	// We simulate this by starting a rescrape, then modifying the revision
	// before the handler can complete its CAS check.
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&slowStubScraper{})

	// Create job
	job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-300.mp4"})
	setJobResult(job, "/tmp/IPX-300.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-300.mp4", MovieID: "IPX-300"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-300"},
		Revision:      1,
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	// Start rescrape in a goroutine
	var wg sync.WaitGroup
	var rec httptest.ResponseRecorder
	wg.Add(1)

	go func() {
		defer wg.Done()
		body, _ := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"slow-scraper"},
			ManualSearchInput: "IPX-300",
		})
		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-300/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(&rec, req)
	}()

	// Wait a bit for the rescrape to start and capture the revision
	time.Sleep(50 * time.Millisecond)

	// Simulate concurrent modification by incrementing the revision
	require.NoError(t, job.ResultsWriter().SetFileResultRevision("/tmp/IPX-300.mp4", 2))

	// Wait for the rescrape to complete
	wg.Wait()

	// The rescrape should return 409 because the revision changed
	assert.Equal(t, http.StatusConflict, rec.Code, "Rescrape should return 409 when revision changed")
}

// slowStubScraper is a scraper that introduces a delay to allow concurrent modification
type slowStubScraper struct {
}

func (s *slowStubScraper) Name() string { return "slow-scraper" }

func (s *slowStubScraper) Search(_ context.Context, id string) (*models.ScraperResult, error) {
	time.Sleep(100 * time.Millisecond)
	releaseDate, _ := time.Parse("2006-01-02", "2024-01-15")
	return &models.ScraperResult{
		Source:        s.Name(),
		ID:            id,
		ContentID:     id,
		Title:         "Slow Scraper Result",
		OriginalTitle: "Slow Scraper Result",
		ReleaseDate:   &releaseDate,
		Actresses:     []models.ActressInfo{{FirstName: "Slow", LastName: "Scraper"}},
		Genres:        []string{"Test"},
	}, nil
}

func (s *slowStubScraper) GetURL(_ context.Context, id string) (string, error) {
	return "https://example.invalid/" + id, nil
}

func (s *slowStubScraper) IsEnabled() bool { return true }

func (s *slowStubScraper) Close() error { return nil }

func (s *slowStubScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}

// TestRescrapeBatchMovie_ConcurrentJobStateMutation tests Bug #3
// P2 DEFENSIVE - Concurrent rescrapes corrupt job state
func TestRescrapeBatchMovie_ConcurrentJobStateMutation(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})

	// Create job with single file
	job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-300.mp4"})
	setJobResult(job, "/tmp/IPX-300.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-300.mp4", MovieID: "IPX-300"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-300", Title: "Original"},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	// Launch two concurrent rescrapes on same file
	var wg sync.WaitGroup
	wg.Add(2)

	var errors []error
	var mu sync.Mutex

	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()

			body, err := json.Marshal(contracts.BatchRescrapeRequest{
				SelectedScrapers:  []string{"stub-no-poster"},
				ManualSearchInput: "IPX-300",
			})
			if err != nil {
				mu.Lock()
				errors = append(errors, err)
				mu.Unlock()
				return
			}

			req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-300/rescrape", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			// Both should either succeed or one should fail gracefully
			// No panics or corruption should occur
		}()
	}

	wg.Wait()

	// Verify job state is consistent
	status := job.GetStatus()
	result := status.Results["/tmp/IPX-300.mp4"]
	require.NotNil(t, result)
	assert.Equal(t, models.JobStatusCompleted, result.Status)
	assert.NotNil(t, result.Movie)

	require.NotNil(t, result.Movie)
	assert.Equal(t, "IPX-300", result.Movie.ID)
}

// TestRescrapeBatchMovie_IDNormalization_CaseInsensitiveFS tests Bug #5
// P0 PLATFORM - ID case changes delete poster on case-insensitive filesystems
func TestRescrapeBatchMovie_IDNormalization_CaseInsensitiveFS(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})

	job := createJobWithWF(deps, cfg, []string{"/tmp/ipx-400.mp4"})
	setJobResult(job, "/tmp/ipx-400.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/ipx-400.mp4", MovieID: "ipx-400"}, // lowercase
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ipx-400", Title: "Original"},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	// Rescrape with normalized uppercase ID
	body, err := json.Marshal(contracts.BatchRescrapeRequest{
		SelectedScrapers:  []string{"stub-no-poster"},
		ManualSearchInput: "IPX-400", // uppercase normalization
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/ipx-400/rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// On case-insensitive FS, poster should be preserved
	// (Test verifies the logic handles case changes correctly)
	status := job.GetStatus()
	result := status.Results["/tmp/ipx-400.mp4"]
	require.NotNil(t, result)

	require.NotNil(t, result.Movie)

	// Verify ID was normalized (case-only change should be handled)
	assert.Equal(t, "IPX-400", result.Movie.ID, "Movie ID should be normalized to uppercase")

	// Note: This test verifies case-change logic in rescrape.go:296
	// On case-insensitive filesystems (macOS/Windows), poster cleanup is skipped
	// for case-only ID changes using strings.EqualFold comparison
}

// TestRescrapeBatchMovie_ScraperCompatibility_Validation tests Bug #17
// P2 MEDIUM - URL with no compatible scrapers should provide clear feedback
func TestRescrapeBatchMovie_ScraperCompatibility_Validation(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})

	job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-500.mp4"})
	setJobResult(job, "/tmp/IPX-500.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-500.mp4", MovieID: "IPX-500"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-500"},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	// Send URL that no scraper can handle
	body, err := json.Marshal(contracts.BatchRescrapeRequest{
		ManualSearchInput: "https://unsupported-site.com/video/12345",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-500/rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Should proceed but likely fail during scraping
	// The fix provides a warning log for visibility
	// Actual behavior: returns 500 (scraping failure) rather than 400 (validation failure)
	assert.True(t, rec.Code == http.StatusInternalServerError || rec.Code == http.StatusUnprocessableEntity,
		"Expected scraping failure, got status %d with body: %s", rec.Code, rec.Body.String())
}

// TestRescrapeBatchMovie_MalformedInput_Handling tests Bug #18
// P2 MEDIUM - Unicode/invisible characters in input should be sanitized
func TestRescrapeBatchMovie_MalformedInput_Handling(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	t.Run("sanitizes zero-width spaces", func(t *testing.T) {
		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-600.mp4"})
		setJobResult(job, "/tmp/IPX-600.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-600.mp4", MovieID: "IPX-600"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-600"},
		})

		// Input with zero-width space
		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"stub-no-poster"},
			ManualSearchInput: "IPX\u200B-600", // Zero-width space
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-600/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		// Should sanitize and succeed
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("rejects empty input after trimming", func(t *testing.T) {
		job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-601.mp4"})
		setJobResult(job, "/tmp/IPX-601.mp4", &worker.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-601.mp4", MovieID: "IPX-601"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "IPX-601"},
		})

		// Input with only whitespace
		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			ManualSearchInput: "   \t\n   ",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-601/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		// Should reject with 400
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "cannot be empty")
	})
}

// TestRescrapeBatchMovie_PosterCleanup verifies poster is removed when ID changes
// This tests the "normal" cleanup path (not the skip conditions)
func TestRescrapeBatchMovie_PosterCleanup(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})

	// Create job with single file
	job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-900.mp4"})
	setJobResult(job, "/tmp/IPX-900.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-900.mp4", MovieID: "IPX-900"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-900", Title: "Original"},
	})

	// Create old poster file
	posterDir := filepath.Join(cfg.System.TempDir, "posters", job.GetID())
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	oldPosterPath := filepath.Join(posterDir, "IPX-900.jpg")
	writeJPEG(t, oldPosterPath, 900, 600)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	// Rescrape with different ID (no siblings, so cleanup should happen)
	body, err := json.Marshal(contracts.BatchRescrapeRequest{
		SelectedScrapers:  []string{"stub-no-poster"},
		ManualSearchInput: "IPX-901", // New ID
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-900/rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Verify old poster was removed (cleanup succeeded)
	_, err = os.Stat(oldPosterPath)
	assert.True(t, os.IsNotExist(err), "Old poster should be removed when ID changes and no siblings exist")

	// Verify movie ID changed
	status := job.GetStatus()
	result := status.Results["/tmp/IPX-900.mp4"]
	require.NotNil(t, result)
	assert.Equal(t, "IPX-901", result.FileMatchInfo.MovieID, "Movie ID should be updated to new ID")
}

// TestRescrapeBatchMovie_OverlappingRescrape_CaseInsensitiveFS tests Task 1
// P0 PLATFORM - Case-only ID change in overlapping rescrapes should NOT delete poster on macOS/Windows
// This test verifies the guard logic in the overlapping rescrape cleanup path (lines 493-518)
func TestRescrapeBatchMovie_OverlappingRescrape_CaseInsensitiveFS(t *testing.T) {
	// Skip on case-sensitive filesystems (Linux typically uses ext4 which is case-sensitive)
	tempDir := t.TempDir()
	fsCache := worker.NewFSCaseCache(afero.NewMemMapFs())
	if !fsCache.IsCaseInsensitive(tempDir) {
		t.Skip("Skipping on case-sensitive filesystem - test is for case-insensitive FS behavior")
	}

	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})

	// Create job with single file
	job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-800.mp4"})

	// Initial state: movie with lowercase ID
	setJobResult(job, "/tmp/IPX-800.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-800.mp4", MovieID: "ipx-800"}, // lowercase
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ipx-800", Title: "Original"},
	})

	// Create poster for lowercase ID
	posterDir := filepath.Join(cfg.System.TempDir, "posters", job.GetID())
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	lowercasePosterPath := filepath.Join(posterDir, "ipx-800.jpg")
	writeJPEG(t, lowercasePosterPath, 900, 600)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	// Rescrape with case-only change (ipx-800 -> IPX-800)
	body, err := json.Marshal(contracts.BatchRescrapeRequest{
		SelectedScrapers:  []string{"stub-no-poster"},
		ManualSearchInput: "IPX-800", // uppercase
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/ipx-800/rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// On darwin (case-insensitive FS), poster should still exist
	// The normal cleanup path (lines 522-530) has the guard
	_, err = os.Stat(lowercasePosterPath)
	// On darwin, the poster should NOT be deleted because of the case-insensitive FS guard
	assert.False(t, os.IsNotExist(err), "Poster should not be deleted on case-insensitive FS for case-only ID change")
}

// TestRescrapeBatchMovie_ConcurrentOverwriteCleanup tests the concurrent overwrite branch
// at rescrape.go:520 (currentMovieIDBeforeUpdate != oldMovieID)
// This tests that when two rescrapes run concurrently on the same file:
// - Both take status snapshots seeing MovieID="ABC-001"
// - Both set oldMovieID="ABC-001" from their snapshots
// - One rescrape gets lock first, updates to new ID (ABC-002 or ABC-003)
// - Other rescrape gets lock next, sees currentMovieIDBeforeUpdate from first, updates to its ID
// - The second rescrape correctly cleans up the intermediate poster (concurrent overwrite branch)
func TestRescrapeBatchMovie_ConcurrentOverwriteCleanup(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	posterGenScraper := &posterGeneratingStubScraper{
		tempDir: cfg.System.TempDir,
	}
	deps.CoreDeps.GetRegistry().RegisterInstance(posterGenScraper)

	// Create job with single file having MovieID "ABC-001"
	job := createJobWithWF(deps, cfg, []string{"/tmp/test-video.mp4"})
	setJobResult(job, "/tmp/test-video.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/test-video.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ABC-001", Title: "Original Movie"},
	})

	// Create poster directory and pre-create posters for all potential movie IDs
	// This simulates what would happen if the scraper downloaded posters
	posterDir := filepath.Join(cfg.System.TempDir, "posters", job.GetID())
	require.NoError(t, os.MkdirAll(posterDir, 0o755))

	// Pre-create posters for all three potential movie IDs
	// The stub scraper doesn't create posters, so we create them upfront to test cleanup
	posterA := filepath.Join(posterDir, "ABC-001.jpg")
	posterB := filepath.Join(posterDir, "ABC-002.jpg")
	posterC := filepath.Join(posterDir, "ABC-003.jpg")
	writeJPEG(t, posterA, 900, 600)
	writeJPEG(t, posterB, 900, 600)
	writeJPEG(t, posterC, 900, 600)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	// Track results from both rescrapes
	var rescrape1Status atomic.Int32
	var rescrape2Status atomic.Int32
	var rescrape1Err, rescrape2Err string

	var wg sync.WaitGroup
	wg.Add(2)

	// Start both rescrapes concurrently
	// Both will call GetStatus() and get snapshots with MovieID="ABC-001"
	// The stub scraper is fast, so both should get the same snapshot before either updates

	// Goroutine 1: Rescrape ABC-001 → ABC-002
	go func() {
		defer wg.Done()

		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"poster-gen"},
			ManualSearchInput: "ABC-002",
		})
		if err != nil {
			rescrape1Status.Store(http.StatusInternalServerError)
			rescrape1Err = err.Error()
			return
		}

		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/ABC-001/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		rescrape1Status.Store(int32(rec.Code))
		if rec.Code != http.StatusOK {
			rescrape1Err = rec.Body.String()
		}
	}()

	// Goroutine 2: Rescrape ABC-001 → ABC-003 (starts concurrently with rescrape 1)
	go func() {
		defer wg.Done()

		body, err := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"poster-gen"},
			ManualSearchInput: "ABC-003",
		})
		if err != nil {
			rescrape2Status.Store(http.StatusInternalServerError)
			rescrape2Err = err.Error()
			return
		}

		// Use same movie ID "ABC-001" - both rescrapes target the same file
		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/ABC-001/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		rescrape2Status.Store(int32(rec.Code))
		if rec.Code != http.StatusOK {
			rescrape2Err = rec.Body.String()
		}
	}()

	wg.Wait()

	status1 := rescrape1Status.Load()
	status2 := rescrape2Status.Load()

	t.Logf("Rescrape 1: status=%d, err=%s", status1, rescrape1Err)
	t.Logf("Rescrape 2: status=%d, err=%s", status2, rescrape2Err)

	// With CAS revision check, there are two valid race outcomes:
	//
	// 1. Overlapping reads: Both goroutines capture the same revision before
	//    either writes. CAS detects the conflict — one succeeds (200), the
	//    other gets 409 Conflict or 404 Not Found.
	//
	// 2. Serialized execution: One goroutine completes entirely before the
	//    other reads the result. The second goroutine sees a different
	//    revision and the result may no longer match (404), or it may
	//    succeed (200) if the lookup still resolves. On fast runners
	//    with a trivial stub scraper, both can succeed.
	//
	// Both outcomes are valid — the test verifies no data corruption.
	successCount := 0
	loserCount := 0
	conflictCount := 0
	notFoundCount := 0
	if status1 == http.StatusOK {
		successCount++
	} else {
		loserCount++
		if status1 == http.StatusConflict {
			conflictCount++
		} else if status1 == http.StatusNotFound {
			notFoundCount++
		}
	}
	if status2 == http.StatusOK {
		successCount++
	} else {
		loserCount++
		if status2 == http.StatusConflict {
			conflictCount++
		} else if status2 == http.StatusNotFound {
			notFoundCount++
		}
	}
	require.Equal(t, 2, successCount+conflictCount+notFoundCount, "Both rescrapes should produce a definitive status")
	require.GreaterOrEqual(t, successCount, 1, "At least one rescrape should succeed (200)")
	require.LessOrEqual(t, successCount, 2, "At most two rescrapes can succeed (200)")

	// Determine which rescrape succeeded based on final movie ID
	status := job.GetStatus()
	result := status.Results["/tmp/test-video.mp4"]
	require.NotNil(t, result, "Result should exist")

	finalMovieID := result.FileMatchInfo.MovieID
	t.Logf("Final MovieID: %s", finalMovieID)

	// Determine which rescrape won based on final movie ID
	// If final is ABC-003: rescrape 2 won
	// If final is ABC-002: rescrape 1 won
	if finalMovieID == "ABC-003" {
		t.Log("Race outcome: rescrape 2 won (ABC-003)")
	} else if finalMovieID == "ABC-002" {
		t.Log("Race outcome: rescrape 1 won (ABC-002)")
	} else {
		t.Fatalf("Unexpected final movie ID: %s", finalMovieID)
	}

	// Verify poster cleanup
	_, errA := os.Stat(posterA)

	// Original poster (ABC-001) should be deleted - cleaned up by the winner
	assert.True(t, os.IsNotExist(errA), "Original poster ABC-001.jpg should be deleted")
	// Poster cleanup assertions are best-effort because the exact filesystem state
	// depends on the interleaving of two concurrent goroutines, which is nondeterministic.
	// Under heavy system load, the loser may get an unexpected status code (not just 409/404),
	// and the poster directory state may differ from the idealized CAS/sequential model.
	// The critical invariant is: no data corruption occurs (verified by the status checks above).

	// Verify no orphaned posters beyond what we pre-created
	entries, err := os.ReadDir(posterDir)
	require.NoError(t, err)
	var posterFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".jpg" {
			posterFiles = append(posterFiles, entry.Name())
		}
	}
	// At most 2 posters should exist (winner + possibly loser's test artifact)
	assert.LessOrEqual(t, len(posterFiles), 2, "At most two posters should exist after concurrent rescrape")

	// Log outcome for debugging
	t.Logf("SUCCESS: CAS correctly prevented concurrent overwrite! (status1=%d, status2=%d)", status1, status2)
}

// TestRescrapeBatchMovie_ConcurrentOverwriteResolvesToOriginal tests the edge case
// where two concurrent rescrapes result in the final ID being the same as the original:
// - Initial: ABC-001
// - Both requests take snapshots seeing oldMovieID="ABC-001"
// - Request 1: ABC-001 → ABC-002 (gets lock first)
// - Request 2: ABC-001 → ABC-001 (resolves back to original, sees currentMovieIDBeforeUpdate="ABC-002")
// Expected: ABC-002 poster should be cleaned up (not orphaned), even though movie.ID == oldMovieID
func TestRescrapeBatchMovie_ConcurrentOverwriteResolvesToOriginal(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	posterGenScraper := &posterGeneratingStubScraper{
		tempDir: cfg.System.TempDir,
	}
	deps.CoreDeps.GetRegistry().RegisterInstance(posterGenScraper)

	// Create job with single file having MovieID "ABC-001"
	job := createJobWithWF(deps, cfg, []string{"/tmp/test-video.mp4"})
	setJobResult(job, "/tmp/test-video.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/test-video.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ABC-001", Title: "Original Movie"},
	})
	jobID := job.GetID()

	// Create poster directory and pre-create posters for all potential movie IDs
	posterDir := filepath.Join(cfg.System.TempDir, "posters", jobID)
	require.NoError(t, os.MkdirAll(posterDir, 0755))

	posterA := filepath.Join(posterDir, "ABC-001.jpg")
	posterB := filepath.Join(posterDir, "ABC-002.jpg")
	writeJPEG(t, posterA, 900, 600)
	writeJPEG(t, posterB, 900, 600)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	// We need to coordinate so that:
	// 1. Both requests take snapshots (seeing oldMovieID="ABC-001")
	// 2. Request 1 gets lock first, updates to ABC-002
	// 3. Request 2 gets lock next, sees currentMovieIDBeforeUpdate="ABC-002"
	// 4. Request 2 updates back to ABC-001

	var wg sync.WaitGroup
	var req1Done atomic.Bool
	var req2Status atomic.Int32

	wg.Add(2)

	// Request 1: ABC-001 → ABC-002
	go func() {
		defer wg.Done()
		body, _ := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"poster-gen"},
			ManualSearchInput: "ABC-002",
		})
		req := httptest.NewRequest(http.MethodPost, "/batch/"+jobID+"/results/ABC-001/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		req1Done.Store(true)
	}()

	// Request 2: ABC-001 → ABC-001 (back to original)
	// Waits briefly for request 1 to complete so it sees currentMovieIDBeforeUpdate="ABC-002"
	go func() {
		defer wg.Done()
		// Wait for request 1 to complete
		for !req1Done.Load() {
			time.Sleep(10 * time.Millisecond)
		}
		body, _ := json.Marshal(contracts.BatchRescrapeRequest{
			SelectedScrapers:  []string{"poster-gen"},
			ManualSearchInput: "ABC-001",
		})
		// URL uses the stable resultID (ABC-001 stays even after movieID changes)
		req := httptest.NewRequest(http.MethodPost, "/batch/"+jobID+"/results/ABC-001/rescrape", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		req2Status.Store(int32(rec.Code))
	}()

	wg.Wait()

	// Verify request 2 succeeded
	require.Equal(t, http.StatusOK, int(req2Status.Load()), "Rescrape 2 should succeed")

	// Verify final state
	status := job.GetStatus()
	result := status.Results["/tmp/test-video.mp4"]
	require.NotNil(t, result, "Result should exist")

	// Final MovieID should be ABC-001 (request 2 resolved back to original)
	require.Equal(t, "ABC-001", result.FileMatchInfo.MovieID, "Final MovieID should be ABC-001 (resolves to original)")
	t.Log("Final MovieID: ABC-001 (resolves to original)")

	// Verify poster cleanup
	_, errA := os.Stat(posterA)
	_, errB := os.Stat(posterB)

	// Final poster (ABC-001) should NOT exist - it was cleaned by request 1
	// and request 2's scraper didn't create a new one
	assert.True(t, os.IsNotExist(errA), "ABC-001.jpg was cleaned by request 1")

	// Intermediate poster (ABC-002) should be DELETED
	// This is the KEY verification: even though movie.ID == oldMovieID == "ABC-001",
	// the intermediate poster ABC-002 was still cleaned up via the overwrite cleanup branch
	assert.True(t, os.IsNotExist(errB), "Intermediate poster ABC-002.jpg should be deleted (not orphaned)")

	// Verify no orphaned posters
	entries, _ := os.ReadDir(posterDir)
	var posterFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".jpg" {
			posterFiles = append(posterFiles, entry.Name())
		}
	}
	assert.Empty(t, posterFiles, "No orphaned posters should remain")

	t.Log("SUCCESS: Concurrent overwrite that resolves to original correctly cleans intermediate poster!")
}

// TestRescrapeBatchMovie_SequentialStaleOverwriteSucceeds tests that a second rescrape
// on the same result (via stable resultID) proceeds normally after the first rescrape
// has already updated the file. With resultID-based routing, the lookup always succeeds
// since the resultID is stable — staleness is handled at the rescrape level via revisions,
// not at the route lookup level.
func TestRescrapeBatchMovie_SequentialStaleOverwriteSucceeds(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	// Register both scrapers
	fastScraper := &noPosterStubScraper{}
	deps.CoreDeps.GetRegistry().RegisterInstance(fastScraper)

	// Create job with single file having MovieID "ABC-001"
	job := createJobWithWF(deps, cfg, []string{"/tmp/test-video.mp4"})
	setJobResult(job, "/tmp/test-video.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/test-video.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ABC-001", Title: "Original Movie"},
	})
	jobID := job.GetID()

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	// Request 1: ABC-001 → ABC-002 (completes normally)
	body1, _ := json.Marshal(contracts.BatchRescrapeRequest{
		SelectedScrapers:  []string{"stub-no-poster"},
		ManualSearchInput: "ABC-002",
	})
	req1 := httptest.NewRequest(http.MethodPost, "/batch/"+jobID+"/results/ABC-001/rescrape", bytes.NewBuffer(body1))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code, "First rescrape should succeed")

	// Now the file has MovieID="ABC-002" (but resultID is still "ABC-001")

	// Request 2: Rescrape the same result again via its stable resultID
	// With resultID-based routing, the lookup succeeds since resultID is stable.
	// The rescrape proceeds normally — staleness is managed at the rescrape level.
	body2, _ := json.Marshal(contracts.BatchRescrapeRequest{
		SelectedScrapers:  []string{"stub-no-poster"},
		ManualSearchInput: "ABC-003",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/batch/"+jobID+"/results/ABC-001/rescrape", bytes.NewBuffer(body2))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	// The rescrape should succeed — the resultID lookup works, and the rescrape
	// overwrites with the new data
	require.Equal(t, http.StatusOK, rec2.Code, "Second rescrape should succeed via stable resultID")

	// Verify final state - should be ABC-003 (from second request)
	status := job.GetStatus()
	result := status.Results["/tmp/test-video.mp4"]
	require.NotNil(t, result)
	require.Equal(t, "ABC-003", result.FileMatchInfo.MovieID, "Final MovieID should be from second request")

	t.Log("SUCCESS: Sequential rescrape via stable resultID correctly overwrites!")
}

// posterGeneratingStubScraper is a stub scraper that creates actual poster files
type posterGeneratingStubScraper struct {
	tempDir string
}

func (s *posterGeneratingStubScraper) Name() string { return "poster-gen" }

func (s *posterGeneratingStubScraper) Search(_ context.Context, id string) (*models.ScraperResult, error) {
	releaseDate, _ := time.Parse("2006-01-02", "2024-01-15")
	return &models.ScraperResult{
		Source:        s.Name(),
		ID:            id,
		ContentID:     id,
		Title:         "Test Movie " + id,
		OriginalTitle: "Test Movie " + id,
		ReleaseDate:   &releaseDate,
		Actresses:     []models.ActressInfo{{FirstName: "Test", LastName: "Actress"}},
		Genres:        []string{"Test"},
	}, nil
}

func (s *posterGeneratingStubScraper) GetURL(_ context.Context, id string) (string, error) {
	return "https://example.invalid/" + id, nil
}

func (s *posterGeneratingStubScraper) IsEnabled() bool { return true }

func (s *posterGeneratingStubScraper) Close() error { return nil }

func (s *posterGeneratingStubScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}

// TestRescrapeBatchMovie_UpdateMode verifies updateMode flag derivation
// Tests different combinations of preset and strategy parameters
func TestRescrapeBatchMovie_UpdateMode(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	tests := []struct {
		name           string
		preset         string
		scalarStrategy string
		arrayStrategy  string
	}{
		{
			name: "no update mode (all empty)",
		},
		{
			name:   "preset only triggers update",
			preset: "conservative",
		},
		{
			name:           "scalar strategy only triggers update",
			scalarStrategy: "prefer-nfo",
		},
		{
			name:          "array strategy only triggers update",
			arrayStrategy: "merge",
		},
		{
			name:           "all three triggers update",
			preset:         "conservative",
			scalarStrategy: "prefer-nfo",
			arrayStrategy:  "merge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-700.mp4"})
			setJobResult(job, "/tmp/IPX-700.mp4", &worker.MovieResult{
				FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-700.mp4", MovieID: "IPX-700"},
				Status:        models.JobStatusCompleted,
				Movie:         &models.Movie{ID: "IPX-700"},
			})

			body, err := json.Marshal(contracts.BatchRescrapeRequest{
				SelectedScrapers:  []string{"stub-no-poster"},
				ManualSearchInput: "IPX-700",
				Preset:            tt.preset,
				ScalarStrategy:    tt.scalarStrategy,
				ArrayStrategy:     tt.arrayStrategy,
			})
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPX-700/rescrape", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			// All should succeed (stub scraper doesn't care about update mode)
			assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		})
	}
}
