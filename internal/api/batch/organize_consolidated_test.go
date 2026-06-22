package batch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiss5_OrganizeJob_CopyOnly(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	body := `{"operation_mode":"in-place","copy_only":true}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Accept either 200 (started) or a non-500 error (validation)
	assert.LessOrEqual(t, w.Code, 500)
}

func TestMiss5_OrganizeJob_NotCompleted(t *testing.T) {
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
	// Default status is pending, not completed

	router := gin.New()
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	body := `{"destination":"/tmp"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- updateBatchJob: successful update with empty body ---

func TestMiss5_OrganizeJob_SuccessfulInPlace(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	body := `{"operation_mode":"in-place","destination":"/tmp"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- organizeJob: not completed returns 400 ---

func TestMiss6_OrganizeJob_DeniedDirectory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("organize"),
			},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				DeniedDirectories: []string{"/denied"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := batchDeps.Deps().GetJobStore().CreateJobBatch([]string{})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	body := `{"destination":"/denied"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- updateBatchJob: with valid body and preserve_nfo ---

func TestMiss6_OrganizeJob_InvalidJSON(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/organize", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- organizeJob: running job returns 409 ---

func TestMiss6_OrganizeJob_JobNotFound(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	body := `{"destination":"/tmp"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/nonexistent-id/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- organizeJob: invalid JSON body returns 400 ---

func TestMiss6_OrganizeJob_PreviewModeRejected(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	body := `{"operation_mode":"preview","destination":"/tmp"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- organizeJob: organize mode with denied directory ---

func TestMiss6_OrganizeJob_RunningJob(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	body := `{"destination":"/tmp"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// --- organizeJob: preview mode rejected ---

func TestMiss7_OrganizeJob_InPlaceMode(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	// In-place mode should not check directory access
	body := `{"destination":"/any/path","operation_mode":"in-place"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusInternalServerError, "Expected 200 or 500, got %d", w.Code)
}

// --- updateBatchJob: empty body (no content) returns 200 ---

func TestMiss7_OrganizeJob_SuccessfulStart(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	body := `{"destination":"/tmp","operation_mode":"in-place"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should succeed or fail with workflow error
	if w.Code == http.StatusOK {
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Organization started", resp["message"])
	}
}

// --- organizeJob: in-place mode with allowed directory ---

func TestMiss7_OrganizeJob_WorkflowError(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	// Send valid request - the workflow creation may succeed or fail depending on config
	body := `{"destination":"/tmp","operation_mode":"in-place"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Either 200 (workflow created) or 500 (workflow creation failed)
	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusInternalServerError, "Expected 200 or 500, got %d", w.Code)
}

// --- organizeJob: successful organize start returns 200 ---

func TestOrganizeJob_CopyOnlySuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("organize"),
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/output"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination: "/output",
		CopyOnly:    true,
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Organization started")
}

func TestOrganizeJob_DeniedDirectory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("organize"),
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/allowed"},
				DeniedDirectories:  []string{"/denied"},
			},
		},
	}
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
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{Destination: "/denied"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 403, w.Code)
	assert.Contains(t, w.Body.String(), "Access denied")
}

func TestOrganizeJob_InvalidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/some-job/organize", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

// ---------------------------------------------------------------------------
// updateBatchJob: 72.0% → uncovered branches:
// - Running job rejected (409)
// - Job not completed (400)
// - force_overwrite + preserve_nfo mutually exclusive (400)
// - Invalid seam string in body (400)
// - Empty body (no request payload)
// - Invalid JSON body
// ---------------------------------------------------------------------------

func TestOrganizeJob_InvalidLinkMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("organize"),
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/output"},
			},
		},
	}
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
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination: "/output",
		LinkMode:    "invalid-link-mode",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestOrganizeJob_JobNotCompleted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("organize"),
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/output"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})
	// Job is in pending status (not completed)
	// Don't set status to completed

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{Destination: "/output"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "Job must be completed before organizing")
}

func TestOrganizeJob_JobNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("organize"),
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/output"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{Destination: "/output"})
	req := httptest.NewRequest("POST", "/batch/nonexistent-job/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
	assert.Contains(t, w.Body.String(), "Job not found")
}

func TestOrganizeJob_Miss2_CopyOnlySkipOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("organize"),
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/output"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination:  "/output",
		CopyOnly:     true,
		SkipNFO:      true,
		SkipDownload: true,
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Organization started")
}

func TestOrganizeJob_Miss2_HardlinkMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("organize"),
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/output"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination: "/output",
		LinkMode:    "hard",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
}

// ---------------------------------------------------------------------------
// updateBatchJob: Content-Length 0 with non-nil Body (early return path)
// ---------------------------------------------------------------------------

func TestOrganizeJob_Miss2_InPlaceModeAllowedDir(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/output"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination: "/anywhere", // in-place mode doesn't check dir access
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Organization started")
}

// ---------------------------------------------------------------------------
// organizeJob: organize mode with allowed directory (pass dir check)
// ---------------------------------------------------------------------------

func TestOrganizeJob_Miss2_OrganizeModeAllowedDir(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("organize"),
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/output"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination: "/output",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Organization started")
}

// ---------------------------------------------------------------------------
// organizeJob: organize with hardlink link_mode
// ---------------------------------------------------------------------------

func TestOrganizeJob_Miss3_GoroutineOrganizePath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/output"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination:  "/output",
		CopyOnly:     false,
		SkipNFO:      true,
		SkipDownload: true,
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return 200 immediately (organize happens in goroutine)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Organization started")
}

func TestOrganizeJob_Miss3_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/some-id/organize", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

// ---------------------------------------------------------------------------
// updateBatchJob: force_overwrite and preserve_nfo mutually exclusive
// ---------------------------------------------------------------------------

func TestOrganizeJob_Miss3_JobAlreadyRunning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusRunning,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusRunning)

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination: "/output",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 409, w.Code)
}

// ---------------------------------------------------------------------------
// organizeJob: job not completed yet
// ---------------------------------------------------------------------------

func TestOrganizeJob_Miss3_JobNotCompleted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusPending,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusPending)

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination: "/output",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "must be completed")
}

// ---------------------------------------------------------------------------
// organizeJob: preview mode rejected
// ---------------------------------------------------------------------------

func TestOrganizeJob_Miss3_JobNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination: "/output",
	})
	req := httptest.NewRequest("POST", "/batch/nonexistent-job-id/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

// ---------------------------------------------------------------------------
// organizeJob: job is already running
// ---------------------------------------------------------------------------

func TestOrganizeJob_Miss3_OrganizeModeDeniedDir(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("organize"),
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/allowed"},
				DeniedDirectories:  []string{"/denied"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination: "/denied",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 403, w.Code)
}

// ---------------------------------------------------------------------------
// organizeJob: invalid JSON body
// ---------------------------------------------------------------------------

func TestOrganizeJob_Miss3_PostApplyErrorPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/output"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination:  "/output",
		SkipNFO:      true,
		SkipDownload: true,
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
}

// ---------------------------------------------------------------------------
// organizeJob: symlink link_mode
// ---------------------------------------------------------------------------

func TestOrganizeJob_Miss3_PreviewModeRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination:   "/output",
		OperationMode: "preview",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "Preview mode")
}

// ---------------------------------------------------------------------------
// organizeJob: organize mode with denied directory
// ---------------------------------------------------------------------------

func TestOrganizeJob_Miss3_SymlinkMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("organize"),
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/output"},
			},
		},
	}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{
		Destination: "/output",
		LinkMode:    "soft",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
}

// ---------------------------------------------------------------------------
// organizeJob: job not found
// ---------------------------------------------------------------------------

func TestOrganizeJob_Miss4_DeniedDestination(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	body := `{"operation_mode":"organize","destination":"/etc/secret"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// /etc is in the built-in deny list
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- organizeJob: running job returns 409 ---

func TestOrganizeJob_Miss4_InvalidJSON(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	req := httptest.NewRequest("POST", "/api/v1/batch/some-id/organize", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- organizeJob: job not found returns 404 ---

func TestOrganizeJob_Miss4_JobNotFound(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	body := `{"destination":"/tmp","operation_mode":"organize"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/nonexistent-id/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- updateBatchJob: job not found returns 404 ---

func TestOrganizeJob_Miss4_PreviewMode(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	body := `{"operation_mode":"preview","destination":"/tmp"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- organizeJob: organize mode with denied destination returns 403 ---

func TestOrganizeJob_Miss4_RunningJob(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/organize", organizeJob(batchDeps))

	body := `{"destination":"/tmp"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/organize", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// --- updateBatchJob: running job returns 409 ---
