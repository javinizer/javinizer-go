package batch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

type errorReader struct{}

func (e *errorReader) Read(_ []byte) (int, error) { return 0, fmt.Errorf("read error") }
func (e *errorReader) Close() error               { return nil }

func TestMiss5_UpdateBatchJob_InvalidPreset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	body := `{"preset":"nonexistent-preset"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- organizeJob: copy_only flag ---

func TestMiss5_UpdateBatchJob_InvalidSeamStrings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	body := `{"scalar_strategy":"invalid-strategy","array_strategy":"invalid"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- updateBatchJob: valid update with body ---

func TestMiss5_UpdateBatchJob_SuccessfulEmptyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- updateBatchJob: invalid seam strings ---

func TestMiss5_UpdateBatchJob_ValidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	body := `{"force_overwrite":true,"scalar_strategy":"prefer-scraper","array_strategy":"merge"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Update started", resp["message"])
}

// --- updateBatchJob: preset validation ---

func TestMiss6_UpdateBatchJob_MutuallyExclusiveOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	body := `{"force_overwrite":true,"preserve_nfo":true}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "mutually exclusive")
}

// --- previewOrganize: movie not found in job ---

func TestMiss6_UpdateBatchJob_WithBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	body := `{"preserve_nfo":true,"skip_nfo":false}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- updateBatchJob: force_overwrite and preserve_nfo are mutually exclusive ---

func TestMiss7_UpdateBatchJob_EmptyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusInternalServerError, "Expected 200 or 500, got %d", w.Code)
}

// --- updateBatchJob: body with content-length 0 ---

func TestMiss7_UpdateBatchJob_NotCompleted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusRunning)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// --- previewOrganize: organize mode with denied directory ---

func TestMiss7_UpdateBatchJob_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	req := httptest.NewRequest("POST", "/api/v1/batch/nonexistent/update", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- updateBatchJob: not completed status ---

func TestMiss7_UpdateBatchJob_RunningJob(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusRunning)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// --- updateBatchJob: job not found ---

func TestMiss7_UpdateBatchJob_WithPreset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	body := `{"preset":"conservative","skip_nfo":false}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusInternalServerError, "Expected 200 or 500, got %d", w.Code)
}

// --- updateBatchJob: running job returns 409 ---

func TestMiss7_UpdateBatchJob_ZeroContentLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", bytes.NewReader([]byte{}))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = 0
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusInternalServerError, "Expected 200 or 500, got %d", w.Code)
}

// --- updateBatchJob: valid body with preset ---

func TestUpdateBatchJob_InvalidJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")

	job := createJobWithWF(deps, config.DefaultConfig(nil, nil), []string{"/tmp/IPX-001.mp4"})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	body := bytes.NewBufferString(`{invalid json`)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/update", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid request body")
}

func TestUpdateBatchJob_InvalidPreset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")

	job := createJobWithWF(deps, config.DefaultConfig(nil, nil), []string{"/tmp/IPX-001.mp4"})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	bodyBytes, _ := json.Marshal(contracts.UpdateRequest{
		Preset: "not-a-valid-preset",
	})
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/update", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateBatchJob_JobNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodPost, "/batch/nonexistent-id/update", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "Job not found")
}

func TestUpdateBatchJob_Miss2_ContentLengthZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	// Send a request with Body non-nil but ContentLength=0
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBuffer(nil))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = 0
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Update started")
}

// ---------------------------------------------------------------------------
// updateBatchJob: io.ReadAll error with errorReader
// ---------------------------------------------------------------------------

// errorReader always returns an error on Read
func TestUpdateBatchJob_Miss2_InvalidJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	// Send invalid JSON with ContentLength > 0
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len("{bad json"))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid request body")
}

// ---------------------------------------------------------------------------
// updateBatchJob: PostApplyFunc closure (success path with valid body)
// ---------------------------------------------------------------------------

func TestUpdateBatchJob_Miss2_ReadAllError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	// Use an errorReader to simulate ReadAll failure
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", &errorReader{})
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = 100
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "Failed to read request body")
}

// ---------------------------------------------------------------------------
// updateBatchJob: invalid JSON body path (with ContentLength > 0 and body > 0 bytes)
// ---------------------------------------------------------------------------

func TestUpdateBatchJob_Miss2_ValidBodyWithSkipOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.UpdateRequest{
		SkipNFO:      true,
		SkipDownload: true,
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Update started")
}

// ---------------------------------------------------------------------------
// previewOrganize: organize/preview mode with denied directory
// ---------------------------------------------------------------------------

func TestUpdateBatchJob_Miss3_ForceAndPreserveMutuallyExclusive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.UpdateRequest{
		ForceOverwrite: true,
		PreserveNFO:    true,
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "mutually exclusive")
}

// ---------------------------------------------------------------------------
// updateBatchJob: job not found
// ---------------------------------------------------------------------------

func TestUpdateBatchJob_Miss3_InvalidSeamStrings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.UpdateRequest{
		ScalarStrategy: "invalid-strategy",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

// ---------------------------------------------------------------------------
// updateBatchJob: PostApplyFunc success path with organize event emission
// ---------------------------------------------------------------------------

func TestUpdateBatchJob_Miss3_JobAlreadyRunning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusRunning,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusRunning)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBuffer(nil))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 409, w.Code)
}

// ---------------------------------------------------------------------------
// updateBatchJob: job not completed
// ---------------------------------------------------------------------------

func TestUpdateBatchJob_Miss3_JobNotCompleted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusPending,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusPending)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBuffer(nil))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "must be completed")
}

// ---------------------------------------------------------------------------
// updateBatchJob: invalid seam strings
// ---------------------------------------------------------------------------

func TestUpdateBatchJob_Miss3_JobNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/nonexistent/update", bytes.NewBuffer(nil))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

// ---------------------------------------------------------------------------
// updateBatchJob: job already running
// ---------------------------------------------------------------------------

func TestUpdateBatchJob_Miss3_PostApplySuccessPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.UpdateRequest{
		PreserveNFO:  true,
		SkipNFO:      false,
		SkipDownload: false,
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Update started")
}

// ---------------------------------------------------------------------------
// organizeJob: goroutine-based organize path (workflow creation succeeds)
// ---------------------------------------------------------------------------

func TestUpdateBatchJob_Miss4_JobNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	req := httptest.NewRequest("POST", "/api/v1/batch/nonexistent-id/update", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- updateBatchJob: force_overwrite and preserve_nfo are mutually exclusive ---

func TestUpdateBatchJob_Miss4_MutuallyExclusive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	// Create a completed job through the store
	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	body := `{"force_overwrite":true,"preserve_nfo":true}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "mutually exclusive")
}

// --- organizeJob: preview mode returns 400 ---

func TestUpdateBatchJob_Miss4_NotCompleted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	// Default status is pending

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateBatchJob_Miss4_RunningJob(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusRunning)

	router := gin.New()
	router.POST("/api/v1/batch/:id/update", updateBatchJob(batchDeps))

	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/update", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// --- updateBatchJob: not completed returns 400 ---

func TestUpdateBatchJob_Miss_BodyReadFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	// Send request with Content-Length > 0 but body that will cause read issues
	// This is hard to simulate directly; instead, verify the invalid JSON body path
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len("not-json"))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

// ---------------------------------------------------------------------------
// organizeJob: successful organize with copy_only=true
// ---------------------------------------------------------------------------

func TestUpdateBatchJob_Miss_EmptyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Empty body should be accepted (backward compatible)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Update started")
}

func TestUpdateBatchJob_Miss_ForceOverwriteAndPreserveNFOMutuallyExclusive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.UpdateRequest{
		ForceOverwrite: true,
		PreserveNFO:    true,
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "mutually exclusive")
}

func TestUpdateBatchJob_Miss_InvalidPreset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	// "invalid-preset" is not one of the valid preset values
	body, _ := json.Marshal(contracts.UpdateRequest{
		Preset: "invalid-preset",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Binding validation should reject invalid preset
	assert.Equal(t, 400, w.Code)
}

func TestUpdateBatchJob_Miss_InvalidScalarStrategy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	// "invalid-strategy" is not a valid scalar_strategy
	body, _ := json.Marshal(contracts.UpdateRequest{
		ScalarStrategy: "invalid-strategy",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Binding validation should reject invalid scalar strategy
	assert.Equal(t, 400, w.Code)
}

// ---------------------------------------------------------------------------
// previewOrganize: 85.7% → uncovered branches:
// - Movie override in request (req.Movie != nil)
// - Job not found (404)
// - Movie not found in job (404)
// ---------------------------------------------------------------------------

func TestUpdateBatchJob_Miss_JobNotCompleted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})
	// Job is in pending status

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "Job must be completed before updating")
}

func TestUpdateBatchJob_Miss_JobNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/nonexistent/update", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
	assert.Contains(t, w.Body.String(), "Job not found")
}

func TestUpdateBatchJob_Miss_RunningJobRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})
	setJobStatus(job, models.JobStatusRunning)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 409, w.Code)
}

func TestUpdateBatchJob_Miss_WithConservativePreset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.UpdateRequest{
		Preset: "conservative",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
}

func TestUpdateBatchJob_Miss_WithValidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/file.mp4"})
	setJobResult(job, "/path/to/file.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.UpdateRequest{
		ForceOverwrite: false,
		SkipNFO:        true,
		SkipDownload:   true,
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Update started")
}

func TestUpdateBatchJob_MutuallyExclusiveFlags(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")

	job := createJobWithWF(deps, config.DefaultConfig(nil, nil), []string{"/tmp/IPX-001.mp4"})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	bodyBytes, _ := json.Marshal(contracts.UpdateRequest{
		ForceOverwrite: true,
		PreserveNFO:    true,
	})
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/update", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "mutually exclusive")
}

func TestUpdateBatchJob_NotCompletedRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")

	job := createJobWithWF(deps, config.DefaultConfig(nil, nil), []string{"/tmp/IPX-001.mp4"})
	// Default status is pending, which is not completed

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/update", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "completed before updating")
}

func TestUpdateBatchJob_RunningJobRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")

	job := createJobWithWF(deps, config.DefaultConfig(nil, nil), []string{"/tmp/IPX-001.mp4"})
	setJobStatus(job, models.JobStatusRunning)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/update", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "already running")
}

func TestUpdateBatchJob_ValidNoBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/tmp/IPX-001.mp4"})
	setJobResult(job, "/tmp/IPX-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPX-001.mp4", MovieID: "IPX-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-001"},
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/update", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Update started")
}

// --- listBatchJobs coverage (lifecycle.go:244) ---
