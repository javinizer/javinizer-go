package jobs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type boundaryFailureReverter struct {
	err   error
	calls int
}

func (r *boundaryFailureReverter) RevertBatch(context.Context, string) (*history.RevertBatchResult, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return &history.RevertBatchResult{Total: 1, Succeeded: 1}, nil
}
func (r *boundaryFailureReverter) RevertScrape(ctx context.Context, jobID, _ string) (*history.RevertBatchResult, error) {
	return r.RevertBatch(ctx, jobID)
}

func TestRevertBackendErrorsDoNotMarkJobReverted(t *testing.T) {
	for _, single := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			err    error
			status int
		}{
			{"already reverted", history.ErrBatchAlreadyReverted, http.StatusConflict},
			{"no operations", history.ErrNoOperationsFound, http.StatusNotFound},
			{"backend failure", errors.New("private reverter failure"), http.StatusInternalServerError},
		} {
			// The concrete RevertScrape implementation does not emit this sentinel.
			// Do not manufacture that adapter-only combination as production coverage.
			if single && errors.Is(tc.err, history.ErrBatchAlreadyReverted) {
				continue
			}
			name := "batch " + tc.name
			if single {
				name = "operation " + tc.name
			}
			t.Run(name, func(t *testing.T) {
				repo := mocks.NewMockJobRepositoryInterface(t)
				job := &models.Job{ID: "job", Status: models.JobStatusOrganized}
				repo.On("FindByID", mock.Anything, "job").Return(job, nil).Once()
				reverter := &boundaryFailureReverter{err: tc.err}
				deps := JobDeps{JobRepo: repo, Reverter: reverter, AllowRevert: true}
				router := gin.New()
				path := "/jobs/job/revert"
				if single {
					path = "/jobs/job/operations/MOV-001/revert"
					router.POST("/jobs/:id/operations/:movieId/revert", revertOperation(deps))
				} else {
					router.POST("/jobs/:id/revert", revertBatch(deps))
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
				require.Equal(t, tc.status, rec.Code)
				require.Equal(t, 1, reverter.calls)
				require.Equal(t, models.JobStatusOrganized, job.Status)
				require.Nil(t, job.RevertedAt)
				require.NotContains(t, rec.Body.String(), "private reverter failure")
				repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
			})
		}
	}
}

func TestRevertCompletionCountFailureDoesNotClaimCompletion(t *testing.T) {
	for _, failedStatus := range []models.RevertStatusEnum{models.RevertStatusApplied, models.RevertStatusFailed} {
		t.Run(string(failedStatus), func(t *testing.T) {
			repo := mocks.NewMockJobRepositoryInterface(t)
			ops := mocks.NewMockBatchFileOperationRepositoryInterface(t)
			job := &models.Job{ID: "job", Status: models.JobStatusOrganized}
			repo.On("FindByID", mock.Anything, "job").Return(job, nil).Once()
			failure := errors.New("private count failure")
			var appliedErr error
			if failedStatus == models.RevertStatusApplied {
				appliedErr = failure
			}
			ops.On("CountByBatchJobIDAndRevertStatus", mock.Anything, "job", models.RevertStatusApplied).Return(int64(0), appliedErr).Once()
			if failedStatus == models.RevertStatusFailed {
				ops.On("CountByBatchJobIDAndRevertStatus", mock.Anything, "job", models.RevertStatusFailed).Return(int64(0), failure).Once()
			}
			reverter := &boundaryFailureReverter{}
			deps := JobDeps{JobRepo: repo, BatchFileOpRepo: ops, Reverter: reverter, AllowRevert: true}
			router := gin.New()
			router.POST("/jobs/:id/operations/:movieId/revert", revertOperation(deps))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/jobs/job/operations/MOV-001/revert", nil))
			require.Equal(t, http.StatusInternalServerError, rec.Code)
			require.Contains(t, rec.Body.String(), "Failed to verify revert completion")
			require.NotContains(t, rec.Body.String(), "private count failure")
			require.Equal(t, 1, reverter.calls, "the operation finished, but batch completion remains unverified")
			require.Equal(t, models.JobStatusOrganized, job.Status)
			require.Nil(t, job.RevertedAt)
			repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
		})
	}
}
