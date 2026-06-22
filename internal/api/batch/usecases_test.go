package batch

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newTestAPIDeps builds an *core.APIDeps whose GetJobRepo() / GetBatchFileOpRepo()
// return the supplied mocks. Used to test use cases (e.g. ListJobsUseCase) that
// depend on the persisted-job query ports rather than the in-memory JobStore.
func newTestAPIDeps(t *testing.T, jobRepo *mocks.MockJobRepositoryInterface, opRepo *mocks.MockBatchFileOperationRepositoryInterface) *core.APIDeps {
	t.Helper()
	return &core.APIDeps{
		Repos: database.Repositories{
			HistoryRepos: database.HistoryRepos{
				JobRepo:         jobRepo,
				BatchFileOpRepo: opRepo,
			},
		},
	}
}

func sampleJob(id string, started time.Time) models.Job {
	return models.Job{
		ID:          id,
		Status:      models.JobStatusCompleted,
		TotalFiles:  3,
		Completed:   3,
		Failed:      0,
		Progress:    1.0,
		Destination: "/dest/" + id,
		StartedAt:   started,
	}
}

func TestListJobsUseCase_HappyPath(t *testing.T) {
	jobRepo := mocks.NewMockJobRepositoryInterface(t)
	opRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	deps := newTestAPIDeps(t, jobRepo, opRepo)

	now := time.Now().UTC()
	jobs := []models.Job{
		sampleJob("job-1", now),
		sampleJob("job-2", now),
		sampleJob("job-3", now),
	}
	jobRepo.On("List", mock.Anything).Return(jobs, nil)
	opRepo.On("CountByBatchJobIDs", mock.Anything, []string{"job-1", "job-2"}).
		Return(map[string]int64{"job-1": 4, "job-2": 2}, nil)
	opRepo.On("CountRevertedByBatchJobIDs", mock.Anything, []string{"job-1", "job-2"}).
		Return(map[string]int64{"job-1": 1, "job-2": 0}, nil)

	out, err := ListJobsUseCase(context.Background(), deps, ListJobsInput{Limit: 2, Offset: 0})
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, 3, out.Total, "total = full list length, not paged")
	require.Len(t, out.Jobs, 2)

	assert.Equal(t, "job-1", out.Jobs[0].ID)
	assert.Equal(t, int64(4), out.Jobs[0].OperationCount, "operation count from batch fetch")
	assert.Equal(t, int64(1), out.Jobs[0].RevertedCount, "reverted count from batch fetch")
	assert.Equal(t, contracts.FormatTime(now), out.Jobs[0].StartedAt)

	assert.Equal(t, "job-2", out.Jobs[1].ID)
	assert.Equal(t, int64(2), out.Jobs[1].OperationCount)
	assert.Equal(t, int64(0), out.Jobs[1].RevertedCount)
}

func TestListJobsUseCase_EmptyJobList(t *testing.T) {
	jobRepo := mocks.NewMockJobRepositoryInterface(t)
	opRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	deps := newTestAPIDeps(t, jobRepo, opRepo)

	// Empty list → no count calls expected (the len(jobIDs)==0 branch in usecases.go).
	jobRepo.On("List", mock.Anything).Return([]models.Job{}, nil)

	out, err := ListJobsUseCase(context.Background(), deps, ListJobsInput{Limit: 10, Offset: 0})
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, 0, out.Total)
	assert.Empty(t, out.Jobs)
}

func TestListJobsUseCase_OffsetBeyondTotal(t *testing.T) {
	jobRepo := mocks.NewMockJobRepositoryInterface(t)
	opRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	deps := newTestAPIDeps(t, jobRepo, opRepo)

	jobs := []models.Job{sampleJob("job-1", time.Now())}
	jobRepo.On("List", mock.Anything).Return(jobs, nil)

	out, err := ListJobsUseCase(context.Background(), deps, ListJobsInput{Limit: 10, Offset: 5})
	require.NoError(t, err)
	assert.Equal(t, 1, out.Total, "total reflects underlying list length")
	assert.Empty(t, out.Jobs, "offset clamped — no paged jobs returned")
}

func TestListJobsUseCase_ListError_Propagates(t *testing.T) {
	jobRepo := mocks.NewMockJobRepositoryInterface(t)
	opRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	deps := newTestAPIDeps(t, jobRepo, opRepo)

	listErr := assert.AnError
	jobRepo.On("List", mock.Anything).Return(nil, listErr)

	out, err := ListJobsUseCase(context.Background(), deps, ListJobsInput{Limit: 10})
	require.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, err.Error(), "failed to list jobs")
}

func TestListJobsUseCase_OperationCountError_Propagates(t *testing.T) {
	jobRepo := mocks.NewMockJobRepositoryInterface(t)
	opRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	deps := newTestAPIDeps(t, jobRepo, opRepo)

	jobs := []models.Job{sampleJob("job-1", time.Now())}
	jobRepo.On("List", mock.Anything).Return(jobs, nil)
	opRepo.On("CountByBatchJobIDs", mock.Anything, []string{"job-1"}).
		Return(nil, assert.AnError)

	out, err := ListJobsUseCase(context.Background(), deps, ListJobsInput{Limit: 10})
	require.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, err.Error(), "failed to retrieve operation counts")
}

func TestListJobsUseCase_RevertedCountError_Propagates(t *testing.T) {
	jobRepo := mocks.NewMockJobRepositoryInterface(t)
	opRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	deps := newTestAPIDeps(t, jobRepo, opRepo)

	jobs := []models.Job{sampleJob("job-1", time.Now())}
	jobRepo.On("List", mock.Anything).Return(jobs, nil)
	opRepo.On("CountByBatchJobIDs", mock.Anything, []string{"job-1"}).
		Return(map[string]int64{"job-1": 2}, nil)
	opRepo.On("CountRevertedByBatchJobIDs", mock.Anything, []string{"job-1"}).
		Return(nil, assert.AnError)

	out, err := ListJobsUseCase(context.Background(), deps, ListJobsInput{Limit: 10})
	require.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, err.Error(), "failed to retrieve revert counts")
}
