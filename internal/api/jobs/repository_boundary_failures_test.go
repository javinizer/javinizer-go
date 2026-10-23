package jobs

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestJobStatisticsFailWithoutPartialCounts(t *testing.T) {
	for _, single := range []bool{true, false} {
		for _, stage := range []string{"operations", "reverted"} {
			name := "list " + stage
			if single {
				name = "single " + stage
			}
			t.Run(name, func(t *testing.T) {
				repo := mocks.NewMockJobRepositoryInterface(t)
				ops := mocks.NewMockBatchFileOperationRepositoryInterface(t)
				failure := errors.New("count repository unavailable")
				deps := JobDeps{JobRepo: repo, BatchFileOpRepo: ops}
				if single {
					repo.On("FindByID", mock.Anything, "job").Return(&models.Job{ID: "job"}, nil).Once()
					var countErr error
					if stage == "operations" {
						countErr = failure
					}
					ops.On("CountByBatchJobID", mock.Anything, "job").Return(int64(4), countErr).Once()
					if stage == "reverted" {
						ops.On("CountByBatchJobIDAndRevertStatus", mock.Anything, "job", models.RevertStatusReverted).Return(int64(0), failure).Once()
					}
					result, err := deps.GetJobWithStats(t.Context(), "job")
					require.ErrorIs(t, err, failure)
					require.Nil(t, result)
				} else {
					repo.On("List", mock.Anything).Return([]models.Job{{ID: "job"}}, nil).Once()
					var countErr error
					if stage == "operations" {
						countErr = failure
					}
					ops.On("CountByBatchJobIDs", mock.Anything, []string{"job"}).Return(map[string]int64{"job": 4}, countErr).Once()
					if stage == "reverted" {
						ops.On("CountRevertedByBatchJobIDs", mock.Anything, []string{"job"}).Return(nil, failure).Once()
					}
					result, err := deps.ListJobsWithStats(t.Context())
					require.ErrorIs(t, err, failure)
					require.Nil(t, result)
				}
				ops.AssertNotCalled(t, "CountNoOpByBatchJobIDs", mock.Anything, mock.Anything)
			})
		}
	}
}

func TestJobOperationsRepositoryFailureIsSanitized(t *testing.T) {
	repo := mocks.NewMockJobRepositoryInterface(t)
	ops := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	repo.On("FindByID", mock.Anything, "job").Return(&models.Job{ID: "job"}, nil).Once()
	ops.On("FindByBatchJobID", mock.Anything, "job").Return(nil, errors.New("private database detail")).Once()
	router := gin.New()
	router.GET("/jobs/:id/operations", listOperations(JobDeps{JobRepo: repo, BatchFileOpRepo: ops}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/jobs/job/operations", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "Failed to retrieve operations")
	require.NotContains(t, rec.Body.String(), "private database detail")
}

func TestRevertCheckRepositoryFailuresFailClosed(t *testing.T) {
	for _, stage := range []string{"target operations", "job list", "later operations"} {
		t.Run(stage, func(t *testing.T) {
			repo := mocks.NewMockJobRepositoryInterface(t)
			ops := mocks.NewMockBatchFileOperationRepositoryInterface(t)
			now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			job := &models.Job{ID: "target", Status: models.JobStatusOrganized, StartedAt: now}
			repo.On("FindByID", mock.Anything, "target").Return(job, nil).Once()
			failure := errors.New("private database detail")
			var targetErr error
			if stage == "target operations" {
				targetErr = failure
			}
			ops.On("FindByBatchJobID", mock.Anything, "target").Return([]models.BatchFileOperation{{OriginalPath: "/original", NewPath: "/shared", RevertStatus: models.RevertStatusApplied}}, targetErr).Once()
			if stage != "target operations" {
				var listErr error
				if stage == "job list" {
					listErr = failure
				}
				repo.On("List", mock.Anything).Return([]models.Job{{ID: "later", StartedAt: now.Add(time.Hour)}}, listErr).Once()
			}
			if stage == "later operations" {
				ops.On("FindByBatchJobID", mock.Anything, "later").Return(nil, failure).Once()
			}
			router := gin.New()
			router.GET("/jobs/:id/revert-check", revertCheck(JobDeps{JobRepo: repo, BatchFileOpRepo: ops, AllowRevert: true}))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/jobs/target/revert-check", nil))
			require.Equal(t, http.StatusInternalServerError, rec.Code)
			require.NotContains(t, rec.Body.String(), "private database detail")
			require.NotContains(t, rec.Body.String(), "overlapping_batches", "a failed lookup must not claim an empty safe overlap list")
			require.Equal(t, models.JobStatusOrganized, job.Status)
			repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
		})
	}
}

func TestRevertCheckFiltersEarlierAndNonAppliedOperations(t *testing.T) {
	repo := mocks.NewMockJobRepositoryInterface(t)
	ops := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	target := models.Job{ID: "b", StartedAt: now, Status: models.JobStatusOrganized}
	repo.On("FindByID", mock.Anything, "b").Return(&target, nil).Once()
	ops.On("FindByBatchJobID", mock.Anything, "b").Return([]models.BatchFileOperation{
		{OriginalPath: "/original", NewPath: "/shared", RevertStatus: models.RevertStatusApplied},
		{OriginalPath: "/ignored", NewPath: "/ignored-new", RevertStatus: models.RevertStatusReverted},
	}, nil).Once()
	repo.On("List", mock.Anything).Return([]models.Job{
		target,
		{ID: "older", StartedAt: now.Add(-time.Hour)},
		{ID: "a", StartedAt: now},
		{ID: "reverted", StartedAt: now.Add(time.Hour), Status: models.JobStatusReverted},
		{ID: "c", StartedAt: now},
	}, nil).Once()
	ops.On("FindByBatchJobID", mock.Anything, "c").Return([]models.BatchFileOperation{
		{OriginalPath: "/shared", NewPath: "/new", RevertStatus: models.RevertStatusApplied},
		{OriginalPath: "/shared", NewPath: "/old", RevertStatus: models.RevertStatusReverted},
		{OriginalPath: "/ignored", NewPath: "/other", RevertStatus: models.RevertStatusApplied},
	}, nil).Once()
	router := gin.New()
	router.GET("/jobs/:id/revert-check", revertCheck(JobDeps{JobRepo: repo, BatchFileOpRepo: ops, AllowRevert: true}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/jobs/b/revert-check", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var result contracts.RevertCheckResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Len(t, result.OverlappingBatches, 1)
	require.Equal(t, "c", result.OverlappingBatches[0].JobID)
	require.Equal(t, 1, result.OverlappingBatches[0].OperationCount)
	repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}
