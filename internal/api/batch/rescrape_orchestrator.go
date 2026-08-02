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
// The rescrape results are already committed at this point, so a persist
// failure rolls the replaced poster caches back (via the rescrape result's
// PosterCacheRollback) and is surfaced on the orchestrator results
// (PersistErr) instead of being acked as success — a restart would
// otherwise resurrect pre-rescrape state against the rescraped images.
type JobPersistencer interface {
	PersistJobByID(id string) error
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
	// PersistErr is non-nil when the post-commit job-envelope persist failed.
	// The per-file rescrapes already committed (Results are valid); PersistErr
	// tells the caller the envelope no longer matches them, so it can surface
	// a failure signal instead of acking state a restart would resurrect
	// alongside the rescraped poster images.
	PersistErr error
}

// SingleRescrapeResult contains the outcome of a single-movie rescrape.
type SingleRescrapeResult struct {
	RescrapeResult *worker.RescrapeResult
	JobID          string
	// PersistErr is non-nil when the post-commit job-envelope persist failed
	// (see RescrapeResult.PersistErr). The rescrape's poster cache was
	// already rolled back to the pre-rescrape assets when possible.
	PersistErr error
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

	if perr := o.persist.PersistJobByID(jobID); perr != nil {
		// Restart (reconstructBatchJob reads only the envelope) would
		// resurrect pre-rescrape job state while the temp poster cache still
		// holds the rescraped image: roll the cache back so both sides of the
		// restart agree, and surface the failure to the caller instead of
		// warn-only acking it.
		persistErr := fmt.Errorf("rescrape committed but job state persist failed: %w", perr)
		if result != nil && result.PosterCacheRollback != nil {
			if rbErr := result.PosterCacheRollback(); rbErr != nil {
				persistErr = fmt.Errorf("%w (poster rollback failed: %v)", persistErr, rbErr)
			}
		}
		logging.Warnf("rescrape for job %s committed but job envelope persist failed: %v", jobID, perr)
		return &SingleRescrapeResult{
			RescrapeResult: result,
			JobID:          jobID,
			PersistErr:     persistErr,
		}, nil
	}

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
			msg := stampJobCounts(&ws.ProgressMessage{
				JobID:    jobID,
				FilePath: movieID,
				Status:   ws.ProgressStatus(result.Status.String()),
				Message:  fmt.Sprintf("Rescrape %s: %s", movieID, result.Status),
				Error:    result.Error,
				Progress: progress,
			}, job)
			if code, args := rescrapeProgressCode(result, movieID); code != "" {
				msg.MessageCode = code
				msg.MessageArgs = args
			}
			o.broadcast.BroadcastProgress(msg)
		}
	}

	results, posterRollbacks := bulkRescrapePool(workCtx, job, movieIDs, req, o.factory, progressFn)

	var persistErr error
	if perr := o.persist.PersistJobByID(jobID); perr != nil {
		// Same restore-then-surface discipline as the single rescrape: every
		// successful per-movie rescrape replaced its shared poster assets,
		// so roll each back before reporting.
		persistErr = fmt.Errorf("bulk rescrape committed but job state persist failed: %w", perr)
		for _, rollback := range posterRollbacks {
			if rbErr := rollback(); rbErr != nil {
				persistErr = fmt.Errorf("%w (poster rollback failed: %v)", persistErr, rbErr)
			}
		}
		logging.Warnf("bulk rescrape for job %s committed but job envelope persist failed: %v", jobID, perr)
	}

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
		Succeeded:  succeeded,
		Failed:     failed,
		Results:    results,
		JobStatus:  updatedStatus,
		PersistErr: persistErr,
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

// rescrapeProgressCode maps a bulk-rescrape per-movie result to a stable
// progress MessageCode (with structured args) so clients can localize the
// outcome. Returns ("", nil) for non-terminal states so no code is stamped
// and the English Message stays authoritative.
func rescrapeProgressCode(result *contracts.BulkRescrapeMovieResult, movieID string) (string, map[string]any) {
	if result == nil {
		return "", nil
	}
	args := map[string]any{}
	if movieID != "" {
		args["movie_id"] = movieID
	}
	switch result.Status {
	case models.RescrapeStatusSuccess:
		return "SCRAPE_SUCCEEDED", args
	case models.RescrapeStatusFailed:
		if result.Error != "" {
			args["error"] = result.Error
		}
		return "SCRAPE_FAILED", args
	default:
		return "", nil
	}
}
