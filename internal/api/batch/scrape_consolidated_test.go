package batch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func TestBatchScrape_DirNotAllowed(t *testing.T) {
	initTestWebSocket(t)

	cfg := &config.Config{
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/allowed"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/scrape", batchScrape(testkit.GetTestRuntime(deps)))

	// File is not in an allowed directory
	body := `{"files":["/forbidden/DIR-001.mp4"]}`
	req := httptest.NewRequest("POST", "/batch/scrape", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)
	assert.Equal(t, 403, w.Code)
}

// TestBatchScrape_InvalidJSON tests the JSON binding error path.

func TestBatchScrape_InvalidJSON(t *testing.T) {
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/scrape", batchScrape(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/scrape", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code)
}

func TestBatchScrape_SeamError(t *testing.T) {
	initTestWebSocket(t)

	cfg := &config.Config{
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/path"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/scrape", batchScrape(testkit.GetTestRuntime(deps)))

	body := `{"files":["/path/to/SEAM-001.mp4"],"scalar_strategy":"invalid-strategy"}`
	req := httptest.NewRequest("POST", "/batch/scrape", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code)
}

// TestCancelBatchJob_Success tests the successful cancel path for a running job.

func TestGetBatchJob_FullPathWithIncludeData(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/FUL-001.mp4"})
	setJobResult(job, "/path/to/FUL-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/FUL-001.mp4", MovieID: "FUL-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "FUL-001", Title: "Full Test"},
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.GET("/batch/:id", getBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch/"+job.GetID()+"?include_data=true", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)

	var resp contracts.BatchJobResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, job.GetID(), resp.ID)
}

// TestGetBatchJob_NotFound tests the 404 path for both slim and full responses.

func TestGetBatchJob_NotFound(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.GET("/batch/:id", getBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch/nonexistent-job-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)

	// Also test with include_data=true
	req2 := httptest.NewRequest("GET", "/batch/nonexistent-job-id?include_data=true", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	assert.Equal(t, 404, w2.Code)
}

// TestBatchScrape_SeamError tests the batchScrape handler with an invalid
// seam configuration that should return 400.

func TestGetBatchJob_SlimPath(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/SLM-001.mp4"})
	setJobResult(job, "/path/to/SLM-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/SLM-001.mp4", MovieID: "SLM-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SLM-001", Title: "Slim Test"},
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.GET("/batch/:id", getBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch/"+job.GetID(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
}

// TestGetBatchJob_FullPathWithIncludeData tests the full response path (with include_data=true).

func TestListBatchJobs_MultipleJobs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job1 := deps.JobStore.CreateJobBatch([]string{"/path/A-001.mp4"})
	setJobStatus(job1, models.JobStatusCompleted)

	job2 := deps.JobStore.CreateJobBatch([]string{"/path/B-002.mp4"})
	setJobStatus(job2, models.JobStatusCompleted)

	router := gin.New()
	router.GET("/batch", listBatchJobs(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodGet, "/batch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.BatchJobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.GreaterOrEqual(t, len(resp.Jobs), 2)
}

func TestListBatchJobs_NoJobs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")

	router := gin.New()
	router.GET("/batch", listBatchJobs(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodGet, "/batch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.BatchJobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Jobs)
}

func TestListBatchJobs_WithExcludedData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/IPX-010.mp4"})
	setJobResult(job, "/path/IPX-010.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/IPX-010.mp4", MovieID: "IPX-010"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-010"},
	})
	setJobStatus(job, models.JobStatusCompleted)
	excludeFile(job, "/path/IPX-010.mp4")

	router := gin.New()
	router.GET("/batch", listBatchJobs(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodGet, "/batch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.BatchJobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Jobs)
}

// --- processBulkRescrapeMovie coverage (batch_rescrape.go:176) ---
// This is an internal function called by batchRescrapeMovies. We test it
// through the HTTP handler to cover the validation and error paths.

func TestListBatchJobs_WithOpAndRevertCounts(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/OPC-001.mp4"})
	setJobResult(job, "/path/to/OPC-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/OPC-001.mp4", MovieID: "OPC-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "OPC-001", Title: "Count Test"},
	})
	setJobStatus(job, models.JobStatusCompleted)
	deps.JobStore.PersistJob(job)

	router := gin.New()
	router.GET("/batch", listBatchJobs(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	var resp contracts.BatchJobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Jobs, 1)
	assert.Equal(t, job.GetID(), resp.Jobs[0].ID)
	// OperationCount and RevertedCount should be populated (may be 0)
	assert.NotNil(t, resp.Jobs[0].OperationCount)
	assert.NotNil(t, resp.Jobs[0].RevertedCount)
}

// TestGetBatchJob_SlimPath tests the slim response path (without include_data=true).
