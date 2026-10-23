package batch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

type boundaryJobStore struct {
	worker.JobStoreInterface
	job worker.BatchJobInterface
}

func (s boundaryJobStore) GetBatchJob(string) (worker.BatchJobInterface, bool) {
	return s.job, s.job != nil
}

type boundaryWorkflow struct{ workflow.WorkflowInterface }

type boundaryWorkflowFactory struct {
	err   error
	calls int
}

func (f *boundaryWorkflowFactory) GetBatchWorkflow(string) (workflow.WorkflowInterface, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &boundaryWorkflow{}, nil
}

type boundaryJob struct {
	worker.BatchJobInterface
	setWorkflow bool
	run         func(context.Context, worker.RescrapeCmd) (*worker.RescrapeResult, error)
}

func (j *boundaryJob) SetWorkflow(workflow.WorkflowInterface) { j.setWorkflow = true }
func (j *boundaryJob) Rescrape(ctx context.Context, cmd worker.RescrapeCmd) (*worker.RescrapeResult, error) {
	return j.run(ctx, cmd)
}
func (j *boundaryJob) GetStatus() *worker.BatchJobStatus {
	return &worker.BatchJobStatus{Status: models.JobStatusCompleted}
}

type boundaryRescrapeFactory struct {
	worker.BatchJobFactoryInterface
}

func (boundaryRescrapeFactory) NewRescrapeCmd(movieID, filePath, manual string, scrapers []string, force bool, merge workflow.MergeOptions) worker.RescrapeCmd {
	return worker.RescrapeCmd{MovieID: movieID, FilePath: filePath, ManualSearchInput: manual, SelectedScrapers: scrapers, Force: force, Merge: merge}
}

type boundaryPersister struct{ calls int }

func (p *boundaryPersister) PersistJobByID(string) error { p.calls++; return nil }

func TestRescrapeOrchestratorAdmissionFailures(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		for _, missing := range []bool{false, true} {
			name := "single"
			if bulk {
				name = "bulk"
			}
			if missing {
				name += " missing job"
			} else {
				name += " workflow failure"
			}
			t.Run(name, func(t *testing.T) {
				wf := &boundaryWorkflowFactory{err: errors.New("factory unavailable")}
				job := &boundaryJob{}
				var stored worker.BatchJobInterface = job
				if missing {
					stored = nil
				}
				orch := NewRescrapeOrchestrator(RescrapeDeps{JobStore: boundaryJobStore{job: stored}, WfFactory: wf})
				var err error
				if bulk {
					result, e := orch.BulkRescrape(context.Background(), "job", []string{"TEST-001"}, &contracts.BatchRescrapeRequest{})
					require.Nil(t, result)
					err = e
				} else {
					result, e := orch.Rescrape(context.Background(), "job", "TEST-001", "file", &contracts.BatchRescrapeRequest{})
					require.Nil(t, result)
					err = e
				}
				if missing {
					require.ErrorContains(t, err, "not found")
					require.Zero(t, wf.calls)
				} else {
					require.ErrorContains(t, err, "workflow init failed")
					require.Equal(t, 1, wf.calls)
				}
				require.False(t, job.setWorkflow, "failed admission must not change the job workflow")
			})
		}
	}
}
func TestSingleRescrapeRejectsInvalidMergeOptions(t *testing.T) {
	job := &boundaryJob{}
	orch := NewRescrapeOrchestrator(RescrapeDeps{JobStore: boundaryJobStore{job: job}, WfFactory: &boundaryWorkflowFactory{}})
	result, err := orch.Rescrape(context.Background(), "job", "TEST-001", "file", &contracts.BatchRescrapeRequest{ScalarStrategy: "invalid"})
	require.Nil(t, result)
	require.ErrorContains(t, err, "invalid merge options")
}
func TestBulkRescrapePropagatesCallerCancellation(t *testing.T) {
	caller, cancel := context.WithCancel(context.Background())
	cancel()
	observed := make(chan error, 1)
	job := &boundaryJob{run: func(ctx context.Context, _ worker.RescrapeCmd) (*worker.RescrapeResult, error) {
		select {
		case <-ctx.Done():
			observed <- ctx.Err()
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			err := errors.New("cancellation not propagated")
			observed <- err
			return nil, err
		}
	}}
	persist := &boundaryPersister{}
	orch := NewRescrapeOrchestrator(RescrapeDeps{JobStore: boundaryJobStore{job: job}, WfFactory: &boundaryWorkflowFactory{}, Factory: boundaryRescrapeFactory{}, Persist: persist})
	result, err := orch.BulkRescrape(caller, "job", []string{"TEST-001"}, &contracts.BatchRescrapeRequest{})
	require.NoError(t, err)
	select {
	case observedErr := <-observed:
		require.ErrorIs(t, observedErr, context.Canceled)
	default:
		t.Fatal("rescrape returned without invoking the worker")
	}
	require.Equal(t, 1, result.Failed)
	require.Zero(t, result.Succeeded)
	require.Equal(t, 1, persist.calls)
	require.Contains(t, result.Results[0].Error, "context canceled")
}
func TestBulkRescrapePoolRecoversWorkerPanic(t *testing.T) {
	job := &boundaryJob{run: func(context.Context, worker.RescrapeCmd) (*worker.RescrapeResult, error) {
		panic("scraper worker panic")
	}}
	var progress []float64
	results, recovery := bulkRescrapePool(context.Background(), job, []string{"TEST-001"}, &contracts.BatchRescrapeRequest{}, boundaryRescrapeFactory{}, func(_ string, _ *contracts.BulkRescrapeMovieResult, p float64) { progress = append(progress, p) })
	require.Len(t, results, 1)
	require.Equal(t, "TEST-001", results[0].MovieID)
	require.Equal(t, models.RescrapeStatusFailed, results[0].Status)
	require.Equal(t, "Rescrape failed unexpectedly", results[0].Error)
	require.NotContains(t, results[0].Error, "scraper worker panic")
	require.Empty(t, recovery)
	require.Equal(t, []float64{100}, progress)
}
