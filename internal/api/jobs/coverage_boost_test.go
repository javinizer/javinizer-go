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
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func TestGetJob_WithOpAndRevertCounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)
	jobID := seedJobsData(t, deps)

	router := gin.New()
	router.GET("/api/v1/jobs/:id", getJob(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+jobID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp contracts.JobListItem
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, jobID, resp.ID)
	assert.Equal(t, int64(3), resp.OperationCount)
	assert.Equal(t, int64(1), resp.RevertedCount)
}

func TestGetJob_AllTimestampFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	twoHoursAgo := now.Add(-2 * time.Hour)

	jobID := uuid.New().String()
	job := &models.Job{
		ID: jobID, Status: models.JobStatusReverted,
		TotalFiles: 2, Completed: 2, Progress: 1.0,
		Destination: "/dest/test", StartedAt: twoHoursAgo,
		CompletedAt: &oneHourAgo, OrganizedAt: &oneHourAgo, RevertedAt: &now,
	}
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), job))

	router := gin.New()
	router.GET("/api/v1/jobs/:id", getJob(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+jobID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp contracts.JobListItem
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp.CompletedAt)
	assert.NotNil(t, resp.OrganizedAt)
	assert.NotNil(t, resp.RevertedAt)
}

func TestListJobs_VerifyOperationCounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)
	seedJobsData(t, deps)

	router := gin.New()
	router.GET("/api/v1/jobs", listJobs(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp contracts.JobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Jobs, 3)

	for _, j := range resp.Jobs {
		if j.Status == models.JobStatusOrganized {
			assert.Equal(t, int64(3), j.OperationCount)
			assert.Equal(t, int64(1), j.RevertedCount)
		}
	}
}

func TestListOperations_RevertedAtPopulated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)
	jobID := seedJobsData(t, deps)

	router := gin.New()
	router.GET("/api/v1/jobs/:id/operations", listOperations(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+jobID+"/operations", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp contracts.OperationListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Operations, 3)

	var foundReverted bool
	for _, op := range resp.Operations {
		if op.RevertStatus == models.RevertStatusReverted {
			assert.NotNil(t, op.RevertedAt)
			foundReverted = true
		}
	}
	assert.True(t, foundReverted)
}

func TestRevertBatch_PartiallyFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db, fs := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	jobID := uuid.New().String()
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	job := &models.Job{
		ID: jobID, Status: models.JobStatusOrganized,
		TotalFiles: 2, Completed: 2, Progress: 1.0,
		Destination: "/dest", StartedAt: oneHourAgo, OrganizedAt: &now,
	}
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), job))

	// Only seed one file in MemMapFs — the other will fail to revert
	dstPath := "/dest/PARTIAL-001/PARTIAL-001.mp4"
	require.NoError(t, fs.MkdirAll("/dest/PARTIAL-001", 0777))
	require.NoError(t, afero.WriteFile(fs, dstPath, []byte("content"), 0666))

	ops := []*models.BatchFileOperation{
		{
			BatchJobID: jobID, MovieID: "PARTIAL-001",
			OriginalPath: "/src/PARTIAL-001.mp4", NewPath: dstPath,
			OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
		},
		{
			BatchJobID: jobID, MovieID: "PARTIAL-002",
			OriginalPath: "/src/PARTIAL-002.mp4", NewPath: "/dest/PARTIAL-002/PARTIAL-002.mp4",
			OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
		},
	}
	for _, op := range ops {
		require.NoError(t, deps.Repos.BatchFileOpRepo.Create(context.Background(), op))
	}

	router := gin.New()
	router.POST("/api/v1/jobs/:id/revert", revertBatch(newTestJobDeps(deps)))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp contracts.RevertResultResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Total)
	// The missing file is skipped (anchor_missing), not failed
	assert.GreaterOrEqual(t, resp.Skipped+resp.Failed, 1)
}

func TestRevertOperation_FailedStatusRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)
	job := createTestJob(t, deps, models.JobStatusFailed)

	router := gin.New()
	router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+job.ID+"/operations/ABC-001/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRevertCheck_DisabledConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	cfg := deps.CoreDeps.GetConfig()
	cfg.Output.Operation.AllowRevert = false
	testkit.GetTestRuntime(deps).InitAPIConfig()
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)

	router := gin.New()
	router.GET("/api/v1/jobs/:id/revert-check", revertCheck(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/some-id/revert-check", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRevertCheck_NoOpsReturnsEmptyOverlaps(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)
	jobID := uuid.New().String()
	job := &models.Job{
		ID: jobID, Status: models.JobStatusOrganized,
		TotalFiles: 0, Completed: 0, Progress: 1.0,
		Destination: "/dest", StartedAt: time.Now(),
	}
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), job))

	router := gin.New()
	router.GET("/api/v1/jobs/:id/revert-check", revertCheck(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+jobID+"/revert-check", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		OverlappingBatches []interface{} `json:"overlapping_batches"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.OverlappingBatches)
}

func TestRevertCheck_JobMissingReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)

	router := gin.New()
	router.GET("/api/v1/jobs/:id/revert-check", revertCheck(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/nonexistent/revert-check", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRevertCheck_OverlapAcrossJobs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps, db, _ := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	twoHoursAgo := now.Add(-2 * time.Hour)

	// Target job (older)
	targetJobID := uuid.New().String()
	targetJob := &models.Job{
		ID: targetJobID, Status: models.JobStatusOrganized,
		TotalFiles: 1, Completed: 1, Progress: 1.0,
		Destination: "/dest", StartedAt: twoHoursAgo, OrganizedAt: &oneHourAgo,
	}
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), targetJob))
	require.NoError(t, deps.Repos.BatchFileOpRepo.Create(context.Background(), &models.BatchFileOperation{
		BatchJobID: targetJobID, MovieID: "SHARED-001",
		OriginalPath: "/src/SHARED-001.mp4", NewPath: "/dest/SHARED-001.mp4",
		OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
	}))

	// Later job with overlapping path
	laterJobID := uuid.New().String()
	laterJob := &models.Job{
		ID: laterJobID, Status: models.JobStatusOrganized,
		TotalFiles: 1, Completed: 1, Progress: 1.0,
		Destination: "/dest2", StartedAt: oneHourAgo, OrganizedAt: &now,
	}
	require.NoError(t, deps.Repos.JobRepo.Create(context.Background(), laterJob))
	require.NoError(t, deps.Repos.BatchFileOpRepo.Create(context.Background(), &models.BatchFileOperation{
		BatchJobID: laterJobID, MovieID: "SHARED-001",
		OriginalPath: "/src/SHARED-001.mp4", NewPath: "/dest2/SHARED-001.mp4",
		OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
	}))

	router := gin.New()
	router.GET("/api/v1/jobs/:id/revert-check", revertCheck(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+targetJobID+"/revert-check", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		OverlappingBatches []struct {
			JobID string `json:"job_id"`
		} `json:"overlapping_batches"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.OverlappingBatches, 1)
	assert.Equal(t, laterJobID, resp.OverlappingBatches[0].JobID)
}
