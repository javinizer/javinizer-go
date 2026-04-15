package jobs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRevertCheck_RevertDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	cfg := deps.GetConfig()
	cfg.Output.AllowRevert = false
	deps.SetConfig(cfg)

	router := gin.New()
	router.GET("/api/v1/jobs/:id/revert-check", revertCheck(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/some-id/revert-check", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var resp contracts.ErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "Revert is disabled")
}

func TestRevertCheck_JobNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	router := gin.New()
	router.GET("/api/v1/jobs/:id/revert-check", revertCheck(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/nonexistent-id/revert-check", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp contracts.ErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "Job not found")
}

func TestRevertCheck_NoAppliedOperations(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	jobID := uuid.New().String()
	job := &models.Job{
		ID:         jobID,
		Status:     string(models.JobStatusOrganized),
		TotalFiles: 0,
		Completed:  0,
		Failed:     0,
		Progress:   1.0,
		StartedAt:  time.Now().Add(-2 * time.Hour),
	}
	require.NoError(t, deps.JobRepo.Create(job))

	router := gin.New()
	router.GET("/api/v1/jobs/:id/revert-check", revertCheck(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+jobID+"/revert-check", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertCheckResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, jobID, resp.JobID)
	assert.Empty(t, resp.OverlappingBatches)
}

func TestRevertCheck_WithAppliedOps_NoOverlaps(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	now := time.Now()
	twoHoursAgo := now.Add(-2 * time.Hour)

	jobID := uuid.New().String()
	job := &models.Job{
		ID:          jobID,
		Status:      string(models.JobStatusOrganized),
		TotalFiles:  1,
		Completed:   1,
		Failed:      0,
		Progress:    1.0,
		Destination: "/dest/organized",
		StartedAt:   twoHoursAgo,
	}
	require.NoError(t, deps.JobRepo.Create(job))

	op := &models.BatchFileOperation{
		BatchJobID:    jobID,
		MovieID:       "ABC-001",
		OriginalPath:  "/src/ABC-001.mp4",
		NewPath:       "/dest/ABC-001.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, deps.BatchFileOpRepo.Create(op))

	router := gin.New()
	router.GET("/api/v1/jobs/:id/revert-check", revertCheck(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+jobID+"/revert-check", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertCheckResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, jobID, resp.JobID)
	assert.Empty(t, resp.OverlappingBatches)
}

func TestRevertCheck_WithOverlappingBatches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	now := time.Now()
	twoHoursAgo := now.Add(-2 * time.Hour)
	oneHourAgo := now.Add(-1 * time.Hour)

	targetJobID := uuid.New().String()
	targetJob := &models.Job{
		ID:          targetJobID,
		Status:      string(models.JobStatusOrganized),
		TotalFiles:  1,
		Completed:   1,
		Failed:      0,
		Progress:    1.0,
		Destination: "/dest/target",
		StartedAt:   twoHoursAgo,
	}
	require.NoError(t, deps.JobRepo.Create(targetJob))

	targetOp := &models.BatchFileOperation{
		BatchJobID:    targetJobID,
		MovieID:       "ABC-001",
		OriginalPath:  "/src/ABC-001.mp4",
		NewPath:       "/dest/ABC-001.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, deps.BatchFileOpRepo.Create(targetOp))

	laterJobID := uuid.New().String()
	laterJob := &models.Job{
		ID:          laterJobID,
		Status:      string(models.JobStatusOrganized),
		TotalFiles:  1,
		Completed:   1,
		Failed:      0,
		Progress:    1.0,
		Destination: "/dest/later",
		StartedAt:   oneHourAgo,
	}
	require.NoError(t, deps.JobRepo.Create(laterJob))

	laterOp := &models.BatchFileOperation{
		BatchJobID:    laterJobID,
		MovieID:       "ABC-001",
		OriginalPath:  "/dest/ABC-001.mp4",
		NewPath:       "/dest2/ABC-001.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, deps.BatchFileOpRepo.Create(laterOp))

	router := gin.New()
	router.GET("/api/v1/jobs/:id/revert-check", revertCheck(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+targetJobID+"/revert-check", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertCheckResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, targetJobID, resp.JobID)
	require.Len(t, resp.OverlappingBatches, 1)
	assert.Equal(t, laterJobID, resp.OverlappingBatches[0].JobID)
	assert.Equal(t, 1, resp.OverlappingBatches[0].OperationCount)
}

func TestRevertCheck_SkipsRevertedJobs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	now := time.Now()
	twoHoursAgo := now.Add(-2 * time.Hour)
	oneHourAgo := now.Add(-1 * time.Hour)

	targetJobID := uuid.New().String()
	targetJob := &models.Job{
		ID:          targetJobID,
		Status:      string(models.JobStatusOrganized),
		TotalFiles:  1,
		Completed:   1,
		Failed:      0,
		Progress:    1.0,
		Destination: "/dest/target",
		StartedAt:   twoHoursAgo,
	}
	require.NoError(t, deps.JobRepo.Create(targetJob))

	targetOp := &models.BatchFileOperation{
		BatchJobID:    targetJobID,
		MovieID:       "ABC-001",
		OriginalPath:  "/src/ABC-001.mp4",
		NewPath:       "/dest/ABC-001.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, deps.BatchFileOpRepo.Create(targetOp))

	revertedJobID := uuid.New().String()
	revertedJob := &models.Job{
		ID:          revertedJobID,
		Status:      string(models.JobStatusReverted),
		TotalFiles:  1,
		Completed:   1,
		Failed:      0,
		Progress:    1.0,
		Destination: "/dest/reverted",
		StartedAt:   oneHourAgo,
		RevertedAt:  &now,
	}
	require.NoError(t, deps.JobRepo.Create(revertedJob))

	router := gin.New()
	router.GET("/api/v1/jobs/:id/revert-check", revertCheck(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+targetJobID+"/revert-check", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertCheckResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Empty(t, resp.OverlappingBatches)
}

func TestRevertCheck_SkipsEarlierJobs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	now := time.Now()
	twoHoursAgo := now.Add(-2 * time.Hour)
	threeHoursAgo := now.Add(-3 * time.Hour)

	targetJobID := uuid.New().String()
	targetJob := &models.Job{
		ID:          targetJobID,
		Status:      string(models.JobStatusOrganized),
		TotalFiles:  1,
		Completed:   1,
		Failed:      0,
		Progress:    1.0,
		Destination: "/dest/target",
		StartedAt:   twoHoursAgo,
	}
	require.NoError(t, deps.JobRepo.Create(targetJob))

	targetOp := &models.BatchFileOperation{
		BatchJobID:    targetJobID,
		MovieID:       "ABC-001",
		OriginalPath:  "/src/ABC-001.mp4",
		NewPath:       "/dest/ABC-001.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, deps.BatchFileOpRepo.Create(targetOp))

	earlierJobID := uuid.New().String()
	earlierJob := &models.Job{
		ID:          earlierJobID,
		Status:      string(models.JobStatusOrganized),
		TotalFiles:  1,
		Completed:   1,
		Failed:      0,
		Progress:    1.0,
		Destination: "/dest/earlier",
		StartedAt:   threeHoursAgo,
	}
	require.NoError(t, deps.JobRepo.Create(earlierJob))

	router := gin.New()
	router.GET("/api/v1/jobs/:id/revert-check", revertCheck(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+targetJobID+"/revert-check", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertCheckResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Empty(t, resp.OverlappingBatches, "Earlier jobs should not appear as overlapping")
}
