package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P2 (PR #241 F2): the completed-noop revert status is terminal and
// non-revertible, so every count consumer exposes it next to the reverted
// count — revertible = operation_count − reverted_count − noop_count — and
// the revertOperation completion check (applied+failed pendings) already
// excludes noop rows, letting a job whose remaining rows are all noop flip
// to reverted. These tests pin both surfaces against a real sqlite stack.

// seedOpsW241 persists one op per (movieID, status) pair for jobID.
func seedOpsW241(t *testing.T, deps *core.APIDeps, jobID string, statuses map[string]models.RevertStatusEnum) {
	t.Helper()
	for movieID, status := range statuses {
		op := &models.BatchFileOperation{
			BatchJobID:    jobID,
			MovieID:       movieID,
			OriginalPath:  "/src/" + movieID + ".mp4",
			NewPath:       "/dest/" + movieID + "/" + movieID + ".mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  status,
		}
		require.NoError(t, deps.Repos.BatchFileOpRepo.Create(context.Background(), op))
	}
}

func TestGetJobW241_NoopCountExposed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	job := createTestJob(t, deps, models.JobStatusOrganized)
	seedOpsW241(t, deps, job.ID, map[string]models.RevertStatusEnum{
		"APP-001": models.RevertStatusApplied,
		"REV-001": models.RevertStatusReverted,
		"DUP-001": models.RevertStatusNoOp,
	})

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.GET("/api/v1/jobs/:id", getJob(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+job.ID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.JobListItem
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(3), resp.OperationCount)
	assert.Equal(t, int64(1), resp.RevertedCount)
	assert.Equal(t, int64(1), resp.NoopCount, "terminal noop rows surface as their own count")
}

func TestListJobsW241_NoopCountExposed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	jobA := createTestJob(t, deps, models.JobStatusOrganized)
	seedOpsW241(t, deps, jobA.ID, map[string]models.RevertStatusEnum{
		"APP-001": models.RevertStatusApplied,
		"DUP-001": models.RevertStatusNoOp,
		"DUP-002": models.RevertStatusNoOp,
	})
	jobB := createTestJob(t, deps, models.JobStatusOrganized)
	seedOpsW241(t, deps, jobB.ID, map[string]models.RevertStatusEnum{
		"APP-002": models.RevertStatusApplied,
	})

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.GET("/api/v1/jobs", listJobs(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.JobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Jobs, 2)

	byID := map[string]contracts.JobListItem{}
	for _, item := range resp.Jobs {
		byID[item.ID] = item
	}
	require.Contains(t, byID, jobA.ID)
	require.Contains(t, byID, jobB.ID)
	assert.Equal(t, int64(3), byID[jobA.ID].OperationCount)
	assert.Equal(t, int64(2), byID[jobA.ID].NoopCount)
	assert.Equal(t, int64(1), byID[jobB.ID].OperationCount)
	assert.Equal(t, int64(0), byID[jobB.ID].NoopCount, "jobs without noop rows read zero")
}

func TestListJobsW241_EmptyJobList(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db := setupJobsTestDeps(t)
	defer func() { _ = db.Close() }()

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.GET("/api/v1/jobs", listJobs(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.JobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Jobs)
}

// w241ErrJobRepo satisfies JobRepositoryInterface via embedding, overriding
// only the reads the stats service performs.
type w241ErrJobRepo struct {
	database.JobRepositoryInterface
}

func (w241ErrJobRepo) FindByID(context.Context, string) (*models.Job, error) {
	return &models.Job{ID: "job-err", StartedAt: time.Now()}, nil
}

func (w241ErrJobRepo) List(context.Context) ([]models.Job, error) {
	return []models.Job{{ID: "job-err", StartedAt: time.Now()}}, nil
}

// w241ErrOpRepo fails exactly the noop count fetch — the F2 aggregation leg.
type w241ErrOpRepo struct {
	database.BatchFileOperationRepositoryInterface
}

func (w241ErrOpRepo) CountByBatchJobID(context.Context, string) (int64, error) {
	return 2, nil
}

func (w241ErrOpRepo) CountByBatchJobIDAndRevertStatus(context.Context, string, models.RevertStatusEnum) (int64, error) {
	return 1, nil
}

func (w241ErrOpRepo) CountByBatchJobIDs(context.Context, []string) (map[string]int64, error) {
	return map[string]int64{"job-err": 2}, nil
}

func (w241ErrOpRepo) CountRevertedByBatchJobIDs(context.Context, []string) (map[string]int64, error) {
	return map[string]int64{}, nil
}

func (w241ErrOpRepo) CountNoOpByBatchJobIDs(context.Context, []string) (map[string]int64, error) {
	return nil, errors.New("w241 outage")
}

func TestJobStatsW241_NoopCountErrorPropagates(t *testing.T) {
	deps := NewJobDeps(w241ErrJobRepo{}, w241ErrOpRepo{}, nil, nil, nil, false)

	got, err := deps.GetJobWithStats(context.Background(), "job-err")
	require.ErrorContains(t, err, "w241 outage")
	assert.Nil(t, got)

	listed, err := deps.ListJobsWithStats(context.Background())
	require.ErrorContains(t, err, "w241 outage")
	assert.Nil(t, listed)
}

// A completed-noop row must never count as pending: after the last applied
// row reverts, the batch closes as fully reverted (job status flips) even
// though a noop row remains. Before F2 the noop row leaked into the
// operation/reverted subtraction and misreported the revertible total.
func TestRevertOperationW241_NoopRowDoesNotBlockJobReverted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deps, db, fs := setupJobsTestDepsWithReverter(t)
	defer func() { _ = db.Close() }()

	jobID := seedRevertableJob(t, deps, fs, []string{"ABC-001"})

	// An authorized duplicate-skip row for the same batch: terminal noop,
	// no filesystem footprint, NewPath empty by construction.
	noopOp := &models.BatchFileOperation{
		BatchJobID:    jobID,
		MovieID:       "ABC-001-DUP",
		OriginalPath:  "/src/ABC-001-DUP.mp4",
		NewPath:       "",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusNoOp,
	}
	require.NoError(t, deps.Repos.BatchFileOpRepo.Create(context.Background(), noopOp))

	svc := newTestJobDeps(deps)
	router := gin.New()
	router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(svc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/operations/ABC-001/revert", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.RevertResultResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.JobStatusReverted, resp.Status,
		"applied+failed pendings are zero — the remaining noop row is terminal, not pending")
	assert.Equal(t, 1, resp.Succeeded)

	job, err := deps.Repos.JobRepo.FindByID(context.Background(), jobID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusReverted, job.Status, "job closed as reverted")

	row, err := deps.Repos.BatchFileOpRepo.FindByID(context.Background(), noopOp.ID)
	require.NoError(t, err)
	assert.Equal(t, models.RevertStatusNoOp, row.RevertStatus, "the noop row is untouched by the revert")
}
