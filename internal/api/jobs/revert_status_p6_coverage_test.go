package jobs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/require"
)

type statusPersistJobRepoP6 struct {
	database.JobRepositoryInterface
	job       *models.Job
	updateErr error
}

func (r *statusPersistJobRepoP6) FindByID(context.Context, string) (*models.Job, error) {
	job := *r.job
	return &job, nil
}

func (r *statusPersistJobRepoP6) Update(context.Context, *models.Job) error {
	return r.updateErr
}

type statusPersistBatchRepoP6 struct {
	database.BatchFileOperationRepositoryInterface
}

func (statusPersistBatchRepoP6) CountByBatchJobIDAndRevertStatus(context.Context, string, models.RevertStatusEnum) (int64, error) {
	return 0, nil
}

type successfulStatusReverterP6 struct{}

func (successfulStatusReverterP6) RevertBatch(context.Context, string) (*history.RevertBatchResult, error) {
	return &history.RevertBatchResult{Total: 1, Succeeded: 1}, nil
}

func (successfulStatusReverterP6) RevertScrape(context.Context, string, string) (*history.RevertBatchResult, error) {
	return &history.RevertBatchResult{Total: 1, Succeeded: 1}, nil
}

func TestRevertHandlersMarkLiveJobReverted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name string
		path func(string) string
	}{
		{name: "batch", path: func(id string) string { return "/api/v1/jobs/" + id + "/revert" }},
		{name: "operation", path: func(id string) string { return "/api/v1/jobs/" + id + "/operations/MOV-001/revert" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := worker.NewInMemoryJobStore()
			liveJob := store.CreateJobBatch(nil)
			jobID := liveJob.ID.String()
			repo := &statusPersistJobRepoP6{
				job: &models.Job{ID: jobID, Status: models.JobStatusOrganized},
			}
			deps := NewJobDeps(repo, statusPersistBatchRepoP6{}, store, successfulStatusReverterP6{}, nil, true)
			router := gin.New()
			if tc.name == "batch" {
				router.POST("/api/v1/jobs/:id/revert", revertBatch(deps))
			} else {
				router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(deps))
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, tc.path(jobID), nil))
			require.Equal(t, http.StatusOK, w.Code)
			_, ok := store.GetJobForControl(jobID)
			require.True(t, ok)
		})
	}
}

func TestRevertHandlersStatusPersistFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name  string
		route string
	}{
		{name: "batch", route: "/api/v1/jobs/p6-status-failure/revert"},
		{name: "operation", route: "/api/v1/jobs/p6-status-failure/operations/MOV-001/revert"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &statusPersistJobRepoP6{
				job:       &models.Job{ID: "p6-status-failure", Status: models.JobStatusOrganized},
				updateErr: errors.New("status update wedged"),
			}
			deps := NewJobDeps(repo, statusPersistBatchRepoP6{}, nil, successfulStatusReverterP6{}, nil, true)
			router := gin.New()
			if tc.name == "batch" {
				router.POST("/api/v1/jobs/:id/revert", revertBatch(deps))
			} else {
				router.POST("/api/v1/jobs/:id/operations/:movieId/revert", revertOperation(deps))
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, tc.route, nil))
			require.Equal(t, http.StatusInternalServerError, w.Code)
		})
	}
}
