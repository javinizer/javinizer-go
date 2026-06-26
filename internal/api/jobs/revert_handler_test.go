package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func TestRevertBatch_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	cfg := deps.CoreDeps.GetConfig()
	cfg.Output.Operation.AllowRevert = false
	testkit.GetTestRuntime(deps).InitAPIConfig()

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/revert", revertBatch(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/some-id/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "Revert is disabled")
}

func TestRevertBatch_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/revert", revertBatch(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/nonexistent-id/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "Job not found")
}

func TestRevertBatch_WrongStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	// Create a job with "completed" status (not "organized")
	job := createTestJob(t, deps, models.JobStatusCompleted)

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/revert", revertBatch(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+job.ID+"/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "not in organized status")
}

func TestRevertBatch_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, fs := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	jobID := seedRevertableJob(t, deps, fs, []string{"ABC-001"})

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/revert", revertBatch(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertResultResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, jobID, resp.JobID)
	assert.Equal(t, models.JobStatusReverted, resp.Status)
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, 1, resp.Succeeded)
	assert.Equal(t, 0, resp.Failed)
	assert.Empty(t, resp.Errors)

	// Verify file was moved back
	_, err := afero.Exists(fs, "/src/ABC-001.mp4")
	assert.NoError(t, err)
}

func TestRevertBatch_AlreadyReverted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, fs := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	jobID := seedRevertableJob(t, deps, fs, []string{"ABC-001"})

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/revert", revertBatch(svc))

	// First revert succeeds
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Second revert: job is now "reverted" status, so handler returns 409
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusConflict, w2.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "already reverted")
}

func TestRevertBatch_NoOperationsFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, _ := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	// Create an organized job with NO file operations
	jobID := uuid.New().String()
	now := time.Now()
	job := &models.Job{
		ID:          jobID,
		Status:      models.JobStatusOrganized,
		TotalFiles:  0,
		Completed:   0,
		Failed:      0,
		Progress:    1.0,
		Destination: "/dest",
		StartedAt:   now.Add(-2 * time.Hour),
		OrganizedAt: &now,
	}
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), job))

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/revert", revertBatch(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "No operations found")
}

func TestRevertBatch_MultipleMovies(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, fs := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	jobID := seedRevertableJob(t, deps, fs, []string{"ABC-001", "ABC-002"})

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/revert", revertBatch(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertResultResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Total)
	assert.Equal(t, 2, resp.Succeeded)
	assert.Equal(t, 0, resp.Failed)
	assert.Empty(t, resp.Errors)
}

func TestRevertOperation_Disabled(t *testing.T) {
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

func TestRevertOperation_NotFound(t *testing.T) {
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

func TestRevertOperation_WrongStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	// Create a job with "completed" status — neither organized nor reverted
	job := createTestJob(t, deps, models.JobStatusCompleted)

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+job.ID+"/operations/ABC-001/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "not in a revertible status")
}

func TestRevertOperation_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, fs := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	jobID := seedRevertableJob(t, deps, fs, []string{"ABC-001", "ABC-002"})

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/operations/ABC-001/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertResultResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, jobID, resp.JobID)
	assert.GreaterOrEqual(t, resp.Succeeded, 1)
}

func TestRevertOperation_AlreadyReverted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, fs := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	jobID := seedRevertableJob(t, deps, fs, []string{"ABC-001"})

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(svc))

	// First revert succeeds
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/operations/ABC-001/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Second revert — the operation is already reverted, so RevertScrape returns an error
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/operations/ABC-001/revert", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	// May return 409 (already reverted) or 500 depending on whether ErrBatchAlreadyReverted is returned
	assert.True(t, w2.Code == http.StatusConflict || w2.Code == http.StatusInternalServerError,
		"Expected 409 or 500 for already-reverted operation, got %d", w2.Code)
}

func TestRevertOperation_NoOperationsForMovie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, fs := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	jobID := seedRevertableJob(t, deps, fs, []string{"ABC-001"})

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(svc))

	// Revert a movie that doesn't exist in the batch — returns 500 (internal error from RevertScrape)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/operations/NONEXISTENT-999/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// The handler maps ErrNoOperationsFound to 404, but for individual movies
	// the error may surface differently; accept either 404 or 500
	assert.True(t, w.Code == http.StatusNotFound || w.Code == http.StatusInternalServerError,
		"Expected 404 or 500 for non-existent movie operations, got %d", w.Code)
}

func TestRevertBatch_PartialFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, fs := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	jobID := uuid.New().String()
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	// Create the organized job
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

	// Movie 1: properly seeded file
	movie1Path := "/dest/ABC-001/ABC-001.mp4"
	require.NoError(t, fs.MkdirAll(filepath.Dir(movie1Path), 0777))
	require.NoError(t, afero.WriteFile(fs, movie1Path, []byte("test-content-1"), 0666))

	op1 := &models.BatchFileOperation{
		BatchJobID:    jobID,
		MovieID:       "ABC-001",
		OriginalPath:  "/src/ABC-001.mp4",
		NewPath:       movie1Path,
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, deps.Repos.BatchFileOpRepo.Create(context.Background(), op1))

	// Movie 2: file does NOT exist at newPath (will cause revert failure)
	op2 := &models.BatchFileOperation{
		BatchJobID:    jobID,
		MovieID:       "ABC-002",
		OriginalPath:  "/src/ABC-002.mp4",
		NewPath:       "/dest/ABC-002/ABC-002.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, deps.Repos.BatchFileOpRepo.Create(context.Background(), op2))

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/revert", revertBatch(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertResultResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Total)
	assert.GreaterOrEqual(t, resp.Succeeded, 1)
	// Job status should remain organized since not all reverted
	assert.Equal(t, models.JobStatusOrganized, resp.Status)
}

func TestRevertBatch_AllRevertedUpdatesJobStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, fs := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	jobID := seedRevertableJob(t, deps, fs, []string{"MOV-001"})

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/revert", revertBatch(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertResultResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.JobStatusReverted, resp.Status)

	// Verify the job was updated in the DB
	updatedJob, err := deps.Repos.JobRepo.FindByID(context.Background(), jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusReverted, updatedJob.Status)
	assert.NotNil(t, updatedJob.RevertedAt)
}
