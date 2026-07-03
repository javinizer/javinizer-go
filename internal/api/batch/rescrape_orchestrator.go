package batch

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// RescrapeOrchestrator owns the resolve→construct→execute pipeline for rescrape
// operations. Both the single-movie and bulk rescrape handlers delegate to it;
// handlers only do HTTP request/response mapping.
type RescrapeOrchestrator struct {
	jobStore  worker.JobStoreInterface
	wfFactory WorkflowFactory
	factory   worker.BatchJobFactoryInterface
	persist   JobPersistencer
	broadcast ProgressBroadcaster
	serverCtx context.Context
}

// RescrapeDeps holds the narrow dependencies the orchestrator needs.
type RescrapeDeps struct {
	JobStore  worker.JobStoreInterface
	WfFactory WorkflowFactory
	Factory   worker.BatchJobFactoryInterface
	Persist   JobPersistencer
	Broadcast ProgressBroadcaster
	ServerCtx context.Context
}

// NewRescrapeOrchestrator creates a new orchestrator with the given deps.
func NewRescrapeOrchestrator(deps RescrapeDeps) *RescrapeOrchestrator {
	return &RescrapeOrchestrator{
		jobStore:  deps.JobStore,
		wfFactory: deps.WfFactory,
		factory:   deps.Factory,
		persist:   deps.Persist,
		broadcast: deps.Broadcast,
		serverCtx: deps.ServerCtx,
	}
}

// JobPersistencer persists a job by ID after rescrape.
type JobPersistencer interface {
	PersistJobByID(id string)
}

// ProgressBroadcaster broadcasts rescrape progress via WebSocket.
type ProgressBroadcaster interface {
	BroadcastProgress(msg *ws.ProgressMessage)
}

// WorkflowFactory resolves a workflow for a given job ID.
type WorkflowFactory interface {
	GetBatchWorkflow(jobID string) (workflow.WorkflowInterface, error)
}

// RescrapeResult contains the outcome of a bulk rescrape operation.
type RescrapeResult struct {
	Succeeded int
	Failed    int
	Results   []contracts.BulkRescrapeMovieResult
	JobStatus *worker.BatchJobStatus
}

// SingleRescrapeResult contains the outcome of a single-movie rescrape.
type SingleRescrapeResult struct {
	RescrapeResult *worker.RescrapeResult
	JobID          string
}

// Rescrape performs a single-movie rescrape: resolve job → set workflow → execute.
func (o *RescrapeOrchestrator) Rescrape(ctx context.Context, jobID, movieID, filePath string, req *contracts.BatchRescrapeRequest) (*SingleRescrapeResult, error) {
	job, ok := o.jobStore.GetBatchJob(jobID)
	if !ok {
		return nil, fmt.Errorf("job %s not found", jobID)
	}

	wf, wfErr := o.wfFactory.GetBatchWorkflow(jobID)
	if wfErr != nil {
		return nil, fmt.Errorf("workflow init failed: %v", wfErr)
	}

	// Per DEEP-6: set WF on the job's deps before calling Rescrape.
	job.SetWorkflow(wf)

	// propagate the client's merge strategy
	// (preset/scalar_strategy/array_strategy) into the rescrape command instead
	// of dropping it. resolveRescrapeMergeOptions resolves the seam strings at
	// this boundary; MergeEnabled gates whether CompleteRescrape applies the
	// merge (false preserves the historical wholesale-replace default).
	mergeOpts, mergeEnabled, mergeErr := resolveRescrapeMergeOptions(req)
	if mergeErr != nil {
		return nil, fmt.Errorf("invalid merge options: %w", mergeErr)
	}
	cmd := o.factory.NewRescrapeCmd(
		movieID,
		filePath,
		req.ManualSearchInput,
		req.SelectedScrapers,
		req.Force,
		mergeOpts,
	)
	cmd.MergeEnabled = mergeEnabled
	result, err := job.Rescrape(ctx, cmd)
	if err != nil {
		return nil, err
	}

	o.persist.PersistJobByID(jobID)

	return &SingleRescrapeResult{
		RescrapeResult: result,
		JobID:          jobID,
	}, nil
}

// BulkRescrape performs a bulk rescrape for multiple movies in a job.
func (o *RescrapeOrchestrator) BulkRescrape(ctx context.Context, jobID string, movieIDs []string, req *contracts.BatchRescrapeRequest) (*RescrapeResult, error) {
	job, ok := o.jobStore.GetBatchJob(jobID)
	if !ok {
		return nil, fmt.Errorf("job %s not found", jobID)
	}

	wf, wfErr := o.wfFactory.GetBatchWorkflow(jobID)
	if wfErr != nil {
		return nil, fmt.Errorf("workflow init failed: %v", wfErr)
	}

	job.SetWorkflow(wf)

	logging.Infof("Bulk rescrape request for job %s: %d movies, scrapers=%v, force=%v",
		jobID, len(movieIDs), req.SelectedScrapers, req.Force)

	// Derive workCtx from both o.serverCtx (so server shutdown cancels bulk
	// work) and the caller's ctx (so a canceled HTTP request stops expensive
	// rescrapes instead of running until shutdown).
	baseCtx := o.serverCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	workCtx, cancelWork := context.WithCancel(baseCtx)
	defer cancelWork()
	if ctx != nil {
		go func() {
			select {
			case <-ctx.Done():
				cancelWork()
			case <-workCtx.Done():
			}
		}()
	}

	progressFn := func(movieID string, result *contracts.BulkRescrapeMovieResult, progress float64) {
		if o.broadcast != nil {
			o.broadcast.BroadcastProgress(stampJobCounts(&ws.ProgressMessage{
				JobID:    jobID,
				FilePath: movieID,
				Status:   ws.ProgressStatus(result.Status.String()),
				Message:  fmt.Sprintf("Rescrape %s: %s", movieID, result.Status),
				Error:    result.Error,
				Progress: progress,
			}, job))
		}
	}

	results := bulkRescrapePool(workCtx, job, movieIDs, req, o.factory, progressFn)

	o.persist.PersistJobByID(jobID)

	succeeded := 0
	failed := 0
	for _, r := range results {
		if r.Status == models.RescrapeStatusSuccess {
			succeeded++
		} else {
			failed++
		}
	}

	updatedStatus := job.GetStatus()

	logging.Infof("Bulk rescrape complete for job %s: %d succeeded, %d failed", jobID, succeeded, failed)

	return &RescrapeResult{
		Succeeded: succeeded,
		Failed:    failed,
		Results:   results,
		JobStatus: updatedStatus,
	}, nil
}

// apiWorkflowFactory adapts a RuntimeSnapshot to the WorkflowFactory interface,
// so the orchestrator builds workflows from the snapshot's pinned epoch rather
// than re-reading CoreDeps (issue #44).
type apiWorkflowFactory struct {
	snap *core.RuntimeSnapshot
}

func (f *apiWorkflowFactory) GetBatchWorkflow(jobID string) (workflow.WorkflowInterface, error) {
	return f.snap.BatchWorkflow(jobID)
}

// runtimeStateBroadcaster adapts *core.RuntimeState to ProgressBroadcaster.
type runtimeStateBroadcaster struct {
	rs *core.RuntimeState
}

func (b *runtimeStateBroadcaster) BroadcastProgress(msg *ws.ProgressMessage) {
	broadcastProgress(b.rs, msg)
}
