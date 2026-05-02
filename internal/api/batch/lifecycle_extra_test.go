package batch

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBatchJob_IncludesCompletedAndEndedAt(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobQueue.CreateJob([]string{"/path/to/IPX-700.mp4"})
	started := time.Now().UTC().Add(-2 * time.Minute)
	ended := time.Now().UTC()
	job.UpdateFileResult("/path/to/IPX-700.mp4", &worker.FileResult{
		FilePath:  "/path/to/IPX-700.mp4",
		MovieID:   "IPX-700",
		Status:    worker.JobStatusCompleted,
		Data:      &models.Movie{ID: "IPX-700", Title: "Done"},
		StartedAt: started,
		EndedAt:   &ended,
	})
	job.MarkCompleted()

	router := gin.New()
	router.GET("/batch/:id", getBatchJob(deps))

	req := httptest.NewRequest("GET", "/batch/"+job.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)

	var response BatchJobResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.NotNil(t, response.CompletedAt)
	require.Contains(t, response.Results, "/path/to/IPX-700.mp4")

	result := response.Results["/path/to/IPX-700.mp4"]
	assert.NotNil(t, result.EndedAt)
	assert.Equal(t, "completed", result.Status)
	assert.Equal(t, "IPX-700", result.MovieID)
}

func TestBatchScrape_InvalidPresetReturnsBadRequest(t *testing.T) {
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
	router.POST("/batch/scrape", batchScrape(deps))

	body := `{"files":["/path/to/IPX-535.mp4"],"preset":"not-a-preset"}`
	req := httptest.NewRequest("POST", "/batch/scrape", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code)
}

func TestDeleteBatchJob_Success(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobQueue.CreateJob([]string{"/path/to/IPX-700.mp4"})
	job.MarkCompleted()

	router := gin.New()
	router.DELETE("/batch/:id", deleteBatchJob(deps))

	req := httptest.NewRequest("DELETE", "/batch/"+job.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
}

func TestDeleteBatchJob_NotFound(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.DELETE("/batch/:id", deleteBatchJob(deps))

	req := httptest.NewRequest("DELETE", "/batch/nonexistent-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

func TestDeleteBatchJob_RunningJobRejected(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobQueue.CreateJob([]string{"/path/to/IPX-700.mp4"})
	job.Status = worker.JobStatusRunning

	router := gin.New()
	router.DELETE("/batch/:id", deleteBatchJob(deps))

	req := httptest.NewRequest("DELETE", "/batch/"+job.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestListBatchJobs_Success(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	deps.JobQueue.CreateJob([]string{"/path/to/IPX-700.mp4"})
	deps.JobQueue.CreateJob([]string{"/path/to/IPX-701.mp4"})

	router := gin.New()
	router.GET("/batch", listBatchJobs(deps))

	req := httptest.NewRequest("GET", "/batch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	jobs, ok := response["jobs"].([]interface{})
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(jobs), 2)
}

func TestListBatchJobs_WithCompletedJob(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobQueue.CreateJob([]string{"/path/to/IPX-700.mp4"})
	started := time.Now().UTC().Add(-2 * time.Minute)
	ended := time.Now().UTC()
	job.UpdateFileResult("/path/to/IPX-700.mp4", &worker.FileResult{
		FilePath:  "/path/to/IPX-700.mp4",
		MovieID:   "IPX-700",
		Status:    worker.JobStatusCompleted,
		Data:      &models.Movie{ID: "IPX-700", Title: "Done"},
		StartedAt: started,
		EndedAt:   &ended,
	})
	job.MarkCompleted()

	router := gin.New()
	router.GET("/batch", listBatchJobs(deps))

	req := httptest.NewRequest("GET", "/batch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response contracts.BatchJobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Jobs, 1)
	assert.Equal(t, job.ID, response.Jobs[0].ID)
}

func TestListBatchJobs_EmptyList(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.GET("/batch", listBatchJobs(deps))

	req := httptest.NewRequest("GET", "/batch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response contracts.BatchJobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Empty(t, response.Jobs)
}

func TestListBatchJobs_WithExcludedFiles(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	deps.JobQueue.CreateJob([]string{"/path/to/IPX-700.mp4", "/path/to/IPX-701.mp4"})

	router := gin.New()
	router.GET("/batch", listBatchJobs(deps))

	req := httptest.NewRequest("GET", "/batch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response contracts.BatchJobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Jobs, 1)
}

func TestGetBatchJobFull_Success(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobQueue.CreateJob([]string{"/path/to/IPX-800.mp4"})
	started := time.Now().UTC().Add(-1 * time.Minute)
	ended := time.Now().UTC()
	job.UpdateFileResult("/path/to/IPX-800.mp4", &worker.FileResult{
		FilePath:  "/path/to/IPX-800.mp4",
		MovieID:   "IPX-800",
		Status:    worker.JobStatusCompleted,
		Data:      &models.Movie{ID: "IPX-800", Title: "Full Test"},
		StartedAt: started,
		EndedAt:   &ended,
	})
	job.MarkCompleted()

	router := gin.New()
	router.GET("/batch/:id", func(c *gin.Context) {
		getBatchJobFull(deps, c, c.Param("id"))
	})

	req := httptest.NewRequest("GET", "/batch/"+job.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response contracts.BatchJobResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, job.ID, response.ID)
	assert.Equal(t, "completed", response.Status)
	assert.NotNil(t, response.CompletedAt)
	assert.Len(t, response.Results, 1)
}

func TestGetBatchJobFull_NotFound(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.GET("/batch/:id", func(c *gin.Context) {
		getBatchJobFull(deps, c, c.Param("id"))
	})

	req := httptest.NewRequest("GET", "/batch/nonexistent-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

func TestGetBatchJobFull_MultiPart(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobQueue.CreateJob([]string{"/path/to/IPX-900-1.mp4", "/path/to/IPX-900-2.mp4"})
	started := time.Now().UTC()
	job.UpdateFileResult("/path/to/IPX-900-1.mp4", &worker.FileResult{
		FilePath:    "/path/to/IPX-900-1.mp4",
		MovieID:     "IPX-900",
		Status:      worker.JobStatusCompleted,
		Data:        &models.Movie{ID: "IPX-900", Title: "Multi Part"},
		StartedAt:   started,
		IsMultiPart: true,
		PartNumber:  1,
		PartSuffix:  "A",
	})

	router := gin.New()
	router.GET("/batch/:id", func(c *gin.Context) {
		getBatchJobFull(deps, c, c.Param("id"))
	})

	req := httptest.NewRequest("GET", "/batch/"+job.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response contracts.BatchJobResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, job.ID, response.ID)
	result, ok := response.Results["/path/to/IPX-900-1.mp4"]
	require.True(t, ok)
	assert.True(t, result.IsMultiPart)
	assert.Equal(t, 1, result.PartNumber)
}

func TestListBatchJobs_WithResults(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobQueue.CreateJob([]string{"/path/to/IPX-950.mp4"})
	started := time.Now().UTC().Add(-1 * time.Minute)
	ended := time.Now().UTC()
	job.UpdateFileResult("/path/to/IPX-950.mp4", &worker.FileResult{
		FilePath:  "/path/to/IPX-950.mp4",
		MovieID:   "IPX-950",
		Status:    worker.JobStatusCompleted,
		Data:      &models.Movie{ID: "IPX-950", Title: "List Test"},
		StartedAt: started,
		EndedAt:   &ended,
	})
	job.MarkCompleted()

	router := gin.New()
	router.GET("/batch", listBatchJobs(deps))

	req := httptest.NewRequest("GET", "/batch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response contracts.BatchJobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Jobs, 1)
	assert.Equal(t, job.ID, response.Jobs[0].ID)
	assert.GreaterOrEqual(t, response.Jobs[0].OperationCount, int64(0))
}
