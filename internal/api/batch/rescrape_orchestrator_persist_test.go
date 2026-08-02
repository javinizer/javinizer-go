package batch

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// failingEnvelopePersist implements the orchestrator's JobPersistencer seam
// with an always-failing upsert, pinning that a persist failure is logged but
// does NOT fail an otherwise-successful rescrape (the results were already
// committed; the failure is visible via the job's PersistError).
type failingEnvelopePersist struct {
	err   error
	calls int
}

func (f *failingEnvelopePersist) PersistJobByID(string) error { f.calls++; return f.err }

// stubWfFactory resolves any job to a fixed (nil-ok) workflow.
type stubWfFactory struct{ err error }

func (s stubWfFactory) GetBatchWorkflow(string) (workflow.WorkflowInterface, error) {
	return nil, s.err
}

// stubRescrapeCmdFactory satisfies BatchJobFactoryInterface for the orch tests;
// only NewRescrapeCmd is exercised.
type stubRescrapeCmdFactory struct {
	worker.BatchJobFactoryInterface
}

func (stubRescrapeCmdFactory) NewRescrapeCmd(movieID, filePath, manualSearchInput string, selectedScrapers []string, force bool, mergeOpts workflow.MergeOptions) worker.RescrapeCmd {
	return worker.RescrapeCmd{
		MovieID:           movieID,
		FilePath:          filePath,
		ManualSearchInput: manualSearchInput,
		SelectedScrapers:  selectedScrapers,
		Force:             force,
		Merge:             mergeOpts,
	}
}

func TestRescrapeOrchestrator_Rescrape_PersistFailureDoesNotFailSuccess(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().SetWorkflow(mock.Anything)
	mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(
		&worker.RescrapeResult{Status: models.RescrapeStatusSuccess}, nil)

	persist := &failingEnvelopePersist{err: errors.New("job repository unavailable")}
	orch := NewRescrapeOrchestrator(RescrapeDeps{
		JobStore:  &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob},
		WfFactory: stubWfFactory{},
		Factory:   stubRescrapeCmdFactory{},
		Persist:   persist,
		ServerCtx: context.Background(),
	})

	result, err := orch.Rescrape(context.Background(), "job-1", "MOV-1", "/f.mp4", &contracts.BatchRescrapeRequest{})
	require.NoError(t, err, "a committed-but-unpersisted rescrape is still a successful rescrape")
	require.NotNil(t, result)
	assert.Equal(t, 1, persist.calls, "the orchestrator must attempt the persist")
	assert.Equal(t, "job-1", result.JobID)
}

func TestRescrapeOrchestrator_BulkRescrape_PersistFailureDoesNotFailSuccess(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().SetWorkflow(mock.Anything)
	mockJob.EXPECT().Rescrape(mock.Anything, mock.Anything).Return(
		&worker.RescrapeResult{Status: models.RescrapeStatusSuccess}, nil)
	mockJob.EXPECT().GetStatus().Return(&worker.BatchJobStatus{})

	persist := &failingEnvelopePersist{err: errors.New("job repository unavailable")}
	orch := NewRescrapeOrchestrator(RescrapeDeps{
		JobStore:  &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob},
		WfFactory: stubWfFactory{},
		Factory:   stubRescrapeCmdFactory{},
		Persist:   persist,
		ServerCtx: context.Background(),
	})

	result, err := orch.BulkRescrape(context.Background(), "job-1", []string{"MOV-1"}, &contracts.BatchRescrapeRequest{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, persist.calls)
	assert.Equal(t, 1, result.Succeeded)
}

// TestPrepareAndLaunchApply_StartApplyError_PersistFailureLogged pins
// execute.go's StartApply-failure path: the failed job start is persisted so
// the failure state survives restarts, and a persist failure there is logged,
// not swallowed.
func TestPrepareAndLaunchApply_StartApplyError_PersistFailureLogged(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)
	// Register a real job under a fixed ID so PersistJobByID resolves to it
	// (persist-by-ID is a store-map lookup; a failed upsert is then recorded
	// on the job's PersistError).
	job := deps.JobStore.CreateJobBatch([]string{"/f.mp4"}, &worker.JobConfig{ID: "job-apply-fail"})

	started := make(chan struct{})
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetID().Return("job-apply-fail").Maybe()
	mockJob.EXPECT().SetWorkflow(mock.Anything)
	mockJob.EXPECT().StartApply(mock.Anything, mock.Anything).RunAndReturn(
		func(context.Context, worker.ApplyPhaseConfig) error {
			close(started)
			return errors.New("apply launcher exploded")
		})

	rt := testkit.GetTestRuntime(deps)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/batch/job-apply-fail/organize", nil)
	prepareAndLaunchApply(c, rt, rt.Snapshot(), mockJob, worker.ApplyPhaseConfig{}, "Organization started")

	<-started
	require.Eventually(t, func() bool {
		return job.GetPersistError() != ""
	}, 5*time.Second, 5*time.Millisecond, "the failed StartApply must be persisted; the failed upsert lands on PersistError")
	assert.Contains(t, job.GetPersistError(), "upsert failed")
}
