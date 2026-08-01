package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/applyplan"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrganizeAndUpdateEndpointPlanConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{"/output"}
	deps := createTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)

	organizeBatch := createJobWithWF(deps, cfg, []string{"/source/organize.mp4"}, applyplan.Default(applyplan.VideoOperationLeaveInPlace, ""))
	setJobStatus(organizeBatch, models.JobStatusCompleted)
	organizeRouter := gin.New()
	organizeRouter.POST("/batch/:id/organize", organizeJob(rt))
	body, err := json.Marshal(contracts.OrganizeRequest{Destination: "/output"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+organizeBatch.GetID()+"/organize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	organizeRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)

	updateJob := createJobWithWF(deps, cfg, []string{"/source/update.mp4"}, applyplan.Default(applyplan.VideoOperationRenameFile, ""))
	setJobStatus(updateJob, models.JobStatusCompleted)
	updateRouter := gin.New()
	updateRouter.POST("/batch/:id/update", updateBatchJob(rt))
	req = httptest.NewRequest(http.MethodPost, "/batch/"+updateJob.GetID()+"/update", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	updateRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestPreviewOrganizeErrorPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("destination required", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		cfg.Output.Operation.OperationMode = "preview"
		deps := createTestDeps(t, cfg, "")
		job := deps.JobStore.CreateJobBatch([]string{"/source/preview.mp4"})
		setJobStatus(job, models.JobStatusCompleted)
		router := gin.New()
		router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))
		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/preview/preview", bytes.NewBufferString(`{}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "destination is required")
	})

	t.Run("workflow creation failure", func(t *testing.T) {
		cfg := badRegexConfig()
		cfg.Output.Operation.OperationMode = "in-place"
		deps := createTestDeps(t, cfg, "")
		job := deps.JobStore.CreateJobBatch([]string{"/source/failure.mp4"})
		setJobResult(job, "/source/failure.mp4", &resultstore.MovieResult{
			ResultID:      "failure",
			FileMatchInfo: models.FileMatchInfo{Path: "/source/failure.mp4", MovieID: "FAIL-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "FAIL-001", Title: "Failure"},
			StartedAt:     time.Now(),
		})
		router := gin.New()
		router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))
		body := []byte(`{"operation_mode":"in-place"}`)
		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/failure/preview", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to create workflow for preview")
	})

	t.Run("preview execution failure", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		cfg.Output.Operation.OperationMode = "in-place"
		deps := createTestDeps(t, cfg, "")
		job := createJobWithWF(deps, cfg, []string{"/source/cancelled.mp4"})
		setJobResult(job, "/source/cancelled.mp4", &resultstore.MovieResult{
			ResultID:      "cancelled",
			FileMatchInfo: models.FileMatchInfo{Path: "/source/cancelled.mp4", MovieID: "CANCEL-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "CANCEL-001", Title: "Cancelled"},
			StartedAt:     time.Now(),
		})
		router := gin.New()
		router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/cancelled/preview", bytes.NewBufferString(`{"operation_mode":"in-place"}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Preview failed")
	})
}
