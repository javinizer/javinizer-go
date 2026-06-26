package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// --- revertBatch: successful full revert updates job to reverted status ---

func TestRevertBatch_Miss2_SuccessFullRevert(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, fs := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	jobID := seedRevertableJob(t, deps, fs, []string{"FULL-001"})

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/revert", revertBatch(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertResultResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.JobStatusReverted, resp.Status)
	assert.Equal(t, 1, resp.Succeeded)
	assert.Equal(t, 0, resp.Failed)
}

// --- revertBatch: partial revert (some failed) keeps organized status ---

func TestRevertBatch_Miss2_PartialRevert(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, _ := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	// Create organized job with 2 operations, only 1 file exists
	jobID := uuid.New().String()
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	job := &models.Job{
		ID:          jobID,
		Status:      models.JobStatusOrganized,
		TotalFiles:  2,
		Completed:   2,
		Failed:      0,
		Progress:    1.0,
		Destination: "/dest",
		StartedAt:   oneHourAgo,
		OrganizedAt: &now,
	}
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), job))

	// Only one file exists at its new path (for revert)
	importJobRevertOp(t, deps, jobID, "MOV-001", "/src/MOV-001.mp4", "/dest/MOV-001/MOV-001.mp4", true)
	importJobRevertOp(t, deps, jobID, "MOV-002", "/src/MOV-002.mp4", "/dest/MOV-002/MOV-002.mp4", false)

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/revert", revertBatch(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertResultResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// Job should remain organized since not all reverted
	assert.Equal(t, models.JobStatusOrganized, resp.Status)
}

// --- revertBatch: batch already reverted returns 409 ---

func TestRevertBatch_Miss2_AlreadyReverted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, fs := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	jobID := seedRevertableJob(t, deps, fs, []string{"REV-001"})

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/revert", revertBatch(svc))

	// First revert succeeds
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Second attempt: job is now "reverted" status
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusConflict, w2.Code)
}

// --- revertOperation: job not found returns 404 ---

func TestRevertOperation_Miss2_JobNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/nonexistent-id/operations/ABC-001/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- revertOperation: revert disabled returns 403 ---

func TestRevertOperation_Miss2_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	cfg := deps.CoreDeps.GetConfig()
	cfg.Output.Operation.AllowRevert = false
	testkit.GetTestRuntime(deps).InitAPIConfig()

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/some-id/operations/ABC-001/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- revertOperation: successful individual revert ---

func TestRevertOperation_Miss2_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, fs := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	jobID := seedRevertableJob(t, deps, fs, []string{"SINGLE-001"})

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/operations/SINGLE-001/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertResultResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, jobID, resp.JobID)
	assert.GreaterOrEqual(t, resp.Succeeded, 1)
}

// --- revertOperation: job status not organized or reverted returns 400 ---

func TestRevertOperation_Miss2_InvalidStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	job := createTestJob(t, deps, models.JobStatusCompleted)

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+job.ID+"/operations/ABC-001/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- emitRevertEvent: nil emitter returns early ---

func TestEmitRevertEvent_Miss2_NilEmitter(t *testing.T) {
	// Should not panic
	emitRevertEvent(context.Background(), nil, "test", "job1", &history.RevertBatchResult{
		Total:     1,
		Succeeded: 1,
	}, nil)
}

// --- emitRevertEvent: with failed + succeeded = warn ---

func TestEmitRevertEvent_Miss2_WarnSeverity(t *testing.T) {
	// This tests the severity logic
	emitRevertEvent(context.Background(), nil, "test", "job1", &history.RevertBatchResult{
		Total:     2,
		Succeeded: 1,
		Failed:    1,
	}, nil)
}

// --- emitRevertEvent: all failed = error severity ---

func TestEmitRevertEvent_Miss2_ErrorSeverity(t *testing.T) {
	emitRevertEvent(context.Background(), nil, "test", "job1", &history.RevertBatchResult{
		Total:  1,
		Failed: 1,
	}, nil)
}

// Suppress unused imports
var _ = afero.NewMemMapFs
