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
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func TestMiss6_PreviewOrganize_InvalidJSON(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/results/:resultId/preview", previewOrganize(batchDeps))

	req := httptest.NewRequest("POST", "/api/v1/batch/test-id/results/ABC-001/preview", bytes.NewReader([]byte("bad json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMiss6_PreviewOrganize_MovieNotFound(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/results/:resultId/preview", previewOrganize(batchDeps))

	body := `{"destination":"/tmp"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/"+job.GetID()+"/results/NONEXISTENT-001/preview", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- previewOrganize: invalid JSON body ---

func TestMiss7_PreviewOrganize_DeniedDirectory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
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

	router := gin.New()
	router.POST("/api/v1/batch/:id/results/:resultId/preview", previewOrganize(batchDeps))

	body := `{"destination":"/denied","operation_mode":"organize"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/test-id/results/ABC-001/preview", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- previewOrganize: preview mode with denied directory ---

func TestMiss7_PreviewOrganize_InvalidOperationMode(t *testing.T) {
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
	router.POST("/api/v1/batch/:id/results/:resultId/preview", previewOrganize(batchDeps))

	body := `{"destination":"/tmp","operation_mode":"invalid-mode"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/test-id/results/ABC-001/preview", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMiss7_PreviewOrganize_PreviewModeDeniedDir(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Operation: config.OutputOperationConfig{
				OperationMode: config.GetOperationMode("in-place"),
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

	router := gin.New()
	router.POST("/api/v1/batch/:id/results/:resultId/preview", previewOrganize(batchDeps))

	body := `{"destination":"/denied","operation_mode":"preview"}`
	req := httptest.NewRequest("POST", "/api/v1/batch/test-id/results/ABC-001/preview", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- previewOrganize: invalid operation mode ---

func TestPreviewOrganize_InPlaceModeAllowedDir(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
			},
			Operation: config.OutputOperationConfig{
				RenameFile:    true,
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

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizePreviewRequest{
		Destination:   "/output",
		OperationMode: "in-place",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/TEST-001/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// in-place mode doesn't require directory access check (only organize/preview do)
	assert.Equal(t, 200, w.Code)
}

// ---------------------------------------------------------------------------
// updateBatchJob: body read failure path
// ---------------------------------------------------------------------------

func TestPreviewOrganize_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/some-id/results/TEST-001/preview", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestPreviewOrganize_JobNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
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
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizePreviewRequest{
		Destination: "/output",
	})
	req := httptest.NewRequest("POST", "/batch/nonexistent/results/TEST-001/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
	assert.Contains(t, w.Body.String(), "Job not found")
}

func TestPreviewOrganize_Miss2_MovieFromResults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
			},
			Operation: config.OutputOperationConfig{
				RenameFile:    true,
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
		Movie:         &models.Movie{ID: "TEST-001", Title: "From Results"},
		StartedAt:     time.Now(),
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizePreviewRequest{
		Destination: "/output",
		// No Movie override — should use result data
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/TEST-001/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
}

// ---------------------------------------------------------------------------
// organizeJob: copy_only with skip_nfo and skip_download
// ---------------------------------------------------------------------------

func TestPreviewOrganize_Miss2_MultipartSort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
			},
			Operation: config.OutputOperationConfig{
				RenameFile:    true,
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

	job := createJobWithWF(deps, cfg, []string{"/path/to/TEST-001-1.mp4", "/path/to/TEST-001-2.mp4"})
	setJobResult(job, "/path/to/TEST-001-1.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001-1.mp4", MovieID: "TEST-001", PartNumber: 1},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test Part 1"},
		StartedAt:     time.Now(),
	})
	setJobResult(job, "/path/to/TEST-001-2.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001-2.mp4", MovieID: "TEST-001", PartNumber: 2},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Test Part 2"},
		StartedAt:     time.Now(),
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizePreviewRequest{
		Destination: "/output",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/TEST-001/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
}

// ---------------------------------------------------------------------------
// previewOrganize: movie data from results (no override)
// ---------------------------------------------------------------------------

func TestPreviewOrganize_Miss2_NilMovieFromResultsNoOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
			},
			Operation: config.OutputOperationConfig{
				RenameFile:    true,
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

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/TEST-001.mp4"})
	// Set a result with Movie nil — this should trigger the "movieData == nil" 404 path
	setJobResult(job, "/path/to/TEST-001.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         nil,
		StartedAt:     time.Now(),
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizePreviewRequest{
		Destination: "/output",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/TEST-001/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
	assert.Contains(t, w.Body.String(), "not found in job")
}

// ---------------------------------------------------------------------------
// previewOrganize: multipart files sorted by PartNumber
// ---------------------------------------------------------------------------

func TestPreviewOrganize_Miss2_OrganizeModeDeniedDir(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
			},
			Operation: config.OutputOperationConfig{
				RenameFile:    true,
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

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizePreviewRequest{
		Destination:   "/denied",
		OperationMode: "organize",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/TEST-001/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 403, w.Code)
}

func TestPreviewOrganize_Miss2_PreviewModeDeniedDir(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
			},
			Operation: config.OutputOperationConfig{
				RenameFile:    true,
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

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizePreviewRequest{
		Destination:   "/denied",
		OperationMode: "preview",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/TEST-001/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 403, w.Code)
}

// ---------------------------------------------------------------------------
// previewOrganize: nil movieData from results (no req.Movie override)
// ---------------------------------------------------------------------------

func TestPreviewOrganize_MovieNotFoundInJob(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
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

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizePreviewRequest{
		Destination: "/output",
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/NONEXISTENT/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
	assert.Contains(t, w.Body.String(), "not found in job")
}

func TestPreviewOrganize_MovieOverrideInRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
			},
			Operation: config.OutputOperationConfig{
				RenameFile:    true,
				OperationMode: config.GetOperationMode("preview"),
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
		Movie:         &models.Movie{ID: "TEST-001", Title: "Original Title"},
		StartedAt:     time.Now(),
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	// Provide a movie override in the request
	overrideMovie := &contracts.MovieView{ID: "TEST-001", Title: "Overridden Title", Maker: "TestStudio"}
	body, _ := json.Marshal(contracts.OrganizePreviewRequest{
		Destination: "/output",
		Movie:       overrideMovie,
	})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/TEST-001/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var resp contracts.OrganizePreviewResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	// The preview should use the overridden movie data — verify the response has data
	assert.NotEmpty(t, resp.FileName, "FileName should not be empty")
}
