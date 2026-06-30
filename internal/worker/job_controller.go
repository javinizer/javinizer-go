package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// jobController owns the phase-launch orchestration for a BatchJob.
// Per DEEP-1: BatchJob is a pure state container (lifecycle, results, cfg, deps,
// events); jobController owns StartScrape/StartApply/Rescrape + resolveDeps +
// markStarted + setDepsFromConfig. The phaseControllerImpl closures are built
// by jobController rather than capturing *BatchJob directly.
//
// Construction: created in newBatchJob alongside the BatchJob. The controller
// holds a back-reference to *BatchJob for state access (lifecycle, results, cfg,
// deps). Callers that need phase execution should use the PhaseController
// interface obtained via BatchJob.Controller() or buildAdapters.
type jobController struct {
	job *BatchJob
}

// newJobController creates a jobController for the given BatchJob.
// Called once during BatchJob construction — the controller is immutable
// after creation (only the job's state changes).
func newJobController(job *BatchJob) *jobController {
	return &jobController{job: job}
}

// StartScrape begins the scrape phase for the given files.
// Returns an error if the job cannot start (e.g., missing workflow dependency).
// Per DEEP-6: reads WF and BatchCfg from job.deps directly — no per-call overrides.
func (c *jobController) StartScrape(ctx context.Context, files []string, cfg ScrapePhaseConfig) error {
	wf := c.resolveWF()
	batchCfg := c.resolveBatchCfg()

	if wf == nil {
		return fmt.Errorf("job %s: cannot start scrape — workflow not configured (provide JobConfig.WF at creation or call SetWorkflow)", c.job.ID.String())
	}

	c.job.mu.RLock()
	persistFn := c.job.deps.PersistFn
	c.job.mu.RUnlock()

	if cfg.FileMatchInfo != nil {
		c.job.results.setFileMatchInfo(cfg.FileMatchInfo)
	}

	ctx, cancel := context.WithCancel(ctx)
	// Store the cancel func BEFORE markStarted so that a concurrent Cancel()
	// call sets the status to Cancelled, causing markStarted to fail (status
	// != expectedFrom) and preventing the goroutine from starting with an
	// uncancelled context. If markStarted fails, the explicit cancel() call
	// is a safe no-op on the already-cancelled context.
	c.job.lifecycle.setCancelFunc(cancel)
	if err := c.markStarted(models.JobStatusPending); err != nil {
		cancel()
		return err
	}
	if persistFn != nil {
		persistFn()
	}

	go func() {
		defer cancel()
		inputs := c.buildScrapeInputs(wf, batchCfg, persistFn)
		c.job.scrapePhase.Run(ctx, inputs, files, cfg)
	}()

	return nil
}

// StartApply begins the apply (organize) phase.
// Returns an error if the job cannot start (e.g., missing workflow dependency).
// Per DEEP-6: reads WF and BatchCfg from job.deps directly — no per-call overrides.
func (c *jobController) StartApply(ctx context.Context, cfg ApplyPhaseConfig) error {
	wf := c.resolveWF()
	batchCfg := c.resolveBatchCfg()

	if wf == nil {
		return fmt.Errorf("job %s: cannot start apply — workflow not configured (provide JobConfig.WF at creation or call SetWorkflow)", c.job.ID.String())
	}

	c.job.mu.RLock()
	persistFn := c.job.deps.PersistFn
	c.job.mu.RUnlock()

	ctx, cancel := context.WithCancel(ctx)
	// Same ordering as StartScrape: setCancelFunc before markStarted.
	c.job.lifecycle.setCancelFunc(cancel)
	if err := c.markStarted(models.JobStatusCompleted); err != nil {
		cancel()
		return err
	}

	// Commit apply-phase config values ONLY after markStarted succeeds, so a
	// losing concurrent StartApply cannot clobber the winner's values. Both
	// calls previously wrote cfg before racing on markStarted; the loser then
	// returned an error but left its destination/operationMode/update/tempDir
	// in c.job.cfg for the winner to read. Now only the winner writes, under
	// c.job.mu, so GetStatus()/buildApplyInputs read committed apply config.
	// Fields are unexported so external callers cannot mutate them.
	c.job.mu.Lock()
	if cfg.Destination != "" {
		c.job.cfg.destination = cfg.Destination
	}
	if cfg.OperationModeOverride != "" {
		c.job.cfg.operationMode = cfg.OperationModeOverride
	}
	if cfg.Update != nil {
		c.job.cfg.update = *cfg.Update
	}
	if cfg.TempDir != "" {
		c.job.cfg.tempDir = cfg.TempDir
	}
	c.job.mu.Unlock()

	if persistFn != nil {
		persistFn()
	}

	go func() {
		defer cancel()
		inputs := c.buildApplyInputs(wf, batchCfg, cfg, persistFn)
		c.job.applyPhase.Run(ctx, inputs, cfg)
	}()

	return nil
}

// Rescrape re-scrapes a single movie within the job.
// Per DEEP-6: reads WF and BatchCfg from job.deps directly — no per-call overrides.
func (c *jobController) Rescrape(ctx context.Context, cmd RescrapeCmd) (*RescrapeResult, error) {
	wf := c.resolveWF()
	batchCfg := c.resolveBatchCfg()

	if wf == nil {
		return &RescrapeResult{Status: models.RescrapeStatusFailed, Error: "workflow not configured"}, nil
	}

	inputs := c.buildRescrapeInputs(wf, batchCfg)

	outcome, err := c.job.rescrapePhase.Rescrape(ctx, inputs, cmd)
	if err != nil {
		return outcome, err
	}

	// Provenance: set on ResultTracker after successful rescrape.
	// This stays accessible through job.results because provenance propagation
	// crosses the phase/results boundary.
	if outcome.Status == models.RescrapeStatusSuccess && outcome.FieldSources != nil && outcome.FilePath != "" {
		c.job.results.SetProvenance(outcome.FilePath, &ProvenanceData{
			FieldSources:   outcome.FieldSources,
			ActressSources: outcome.ActressSources,
		})
	}

	return outcome, nil
}

// Wait blocks until the job reaches a terminal state and returns any error.
func (c *jobController) Wait() error {
	c.job.lifecycle.mu.RLock()
	done := c.job.lifecycle.done
	c.job.lifecycle.mu.RUnlock()
	<-done
	c.job.lifecycle.mu.RLock()
	status := c.job.lifecycle.Status
	c.job.lifecycle.mu.RUnlock()

	switch status {
	case models.JobStatusFailed:
		return fmt.Errorf("job %s failed", c.job.ID.String())
	case models.JobStatusCancelled:
		return fmt.Errorf("job %s cancelled", c.job.ID.String())
	default:
		return nil
	}
}

// markStarted transitions the job from expectedFrom to running state and creates a
// fresh Done channel. It performs a compare-and-swap: if the lifecycle status is not
// expectedFrom when the lock is acquired, it returns an error without modifying state.
// This prevents the TOCTOU race where an API handler checks status == Completed but
// another concurrent request transitions the job before this call acquires the lock.
func (c *jobController) markStarted(expectedFrom models.JobStatus) error {
	c.job.lifecycle.mu.Lock()
	if c.job.lifecycle.Status != expectedFrom {
		actual := c.job.lifecycle.Status
		c.job.lifecycle.mu.Unlock()
		return fmt.Errorf("job %s: cannot start — expected status %s but got %s", c.job.ID.String(), expectedFrom, actual)
	}
	c.job.lifecycle.Status = models.JobStatusRunning
	c.job.lifecycle.CompletedAt = nil
	c.job.lifecycle.OrganizedAt = nil
	c.job.lifecycle.done = make(chan struct{})
	c.job.lifecycle.mu.Unlock()

	c.job.mu.Lock()
	c.job.StartedAt = time.Now()
	c.job.mu.Unlock()

	return nil
}

// setDepsFromConfig applies JobConfig fields to the job's deps.
// Shared by all 3 construction paths (newBatchJob, createJob, reconstructBatchJob).
// Per DEEP-1: moved from *BatchJob to jobController — BatchJob is a pure state
// container and does not own dependency wiring.
func (c *jobController) setDepsFromConfig(cfg *JobConfig) {
	if cfg == nil {
		return
	}
	c.job.mu.Lock()
	defer c.job.mu.Unlock()

	if cfg.WF != nil {
		c.job.deps.WF = cfg.WF
	}
	if cfg.Matcher != nil {
		c.job.deps.Matcher = cfg.Matcher
	}
	if cfg.BatchCfg.MaxWorkers > 0 || cfg.BatchCfg.WorkerTimeout > 0 || len(cfg.BatchCfg.ScraperPriority) > 0 {
		c.job.deps.BatchCfg = cfg.BatchCfg
	}
	if cfg.BatchFileOpRepo != nil {
		c.job.deps.BatchFileOpRepo = cfg.BatchFileOpRepo
	}
	if cfg.MovieRepo != nil {
		c.job.deps.MovieRepo = cfg.MovieRepo
		c.job.posterEditor = NewPosterEditor(c.job.resultIndex, c.job.results, cfg.MovieRepo)
	}
	if cfg.ActressRepo != nil {
		c.job.deps.ActressRepo = cfg.ActressRepo
	}
	if cfg.HistoryRepo != nil {
		c.job.deps.HistoryRepo = cfg.HistoryRepo
	}
	if cfg.Emitter != nil {
		c.job.deps.Emitter = cfg.Emitter
	}
	if cfg.PersistFn != nil {
		c.job.deps.PersistFn = cfg.PersistFn
	}
	if cfg.PosterGen != nil {
		c.job.deps.PosterGen = cfg.PosterGen
	}
	if cfg.Logger != nil {
		c.job.deps.Logger = cfg.Logger
	}
}

// buildScrapeInputs constructs scrapePhaseInputs directly from the job's
// sub-managers. Per DEEP-7: eliminates the intermediate batchJobInputs struct
// that mixed copied values and shared pointers. The controller owns the
// sub-managers for the duration of the phase, so there is no snapshot-vs-pointer
// ambiguity — the inputs are constructed inline from live state.
func (c *jobController) buildScrapeInputs(wf workflow.WorkflowInterface, batchCfg BatchJobConfig, persistFn func()) scrapePhaseInputs {
	c.job.mu.RLock()
	m := c.job.deps.Matcher
	pg := c.job.deps.PosterGen
	movieRepo := c.job.deps.MovieRepo
	c.job.mu.RUnlock()

	c.job.batchJobEventSource.mu.RLock()
	keepOpen := c.job.keepBroadcasterOpen
	broadcaster := c.job.eventBroadcaster
	c.job.batchJobEventSource.mu.RUnlock()

	fileMatchInfo := c.job.results.CloneFileMatchInfo()

	inputs := scrapePhaseInputs{
		JobID:               c.job.ID,
		Concurrency:         newConcurrencyConfig(batchCfg.MaxWorkers, batchCfg.WorkerTimeout, defaultMaxWorkers, defaultWorkerTimeout),
		WF:                  wf,
		PosterGen:           pg,
		KeepBroadcasterOpen: keepOpen,
		Broadcaster:         broadcaster,
		Updater:             c.job.results,
		Lifecycle:           c.job.lifecycle,
		persister:           persistFunc(persistFn),
		FileMatchInfo:       fileMatchInfo,
		MovieRepo:           movieRepo,
	}
	if m != nil {
		inputs.Matcher = m
	}
	return inputs
}

// buildApplyInputs constructs applyPhaseInputs directly from the job's
// sub-managers. Per DEEP-7: same rationale as buildScrapeInputs.
func (c *jobController) buildApplyInputs(wf workflow.WorkflowInterface, batchCfg BatchJobConfig, cfg ApplyPhaseConfig, persistFn func()) applyPhaseInputs {
	c.job.batchJobEventSource.mu.RLock()
	broadcaster := c.job.eventBroadcaster
	c.job.batchJobEventSource.mu.RUnlock()

	snap := c.job.results.SnapshotData()

	c.job.mu.RLock()
	upd := c.job.cfg.update
	c.job.mu.RUnlock()

	return applyPhaseInputs{
		JobID:       c.job.ID,
		Concurrency: newConcurrencyConfig(batchCfg.MaxWorkers, batchCfg.WorkerTimeout, 1, defaultWorkerTimeout),
		NFOEnabled:  batchCfg.NFOEnabled,
		WF:          wf,
		Results:     snap.Results,
		Excluded:    snap.Excluded,
		Destination: cfg.Destination,
		Update:      upd,
		Broadcaster: broadcaster,
		Updater:     c.job.results,
		Lifecycle:   c.job.lifecycle,
		persister:   persistFunc(persistFn),
	}
}

// buildRescrapeInputs constructs rescrapePhaseInputs directly from the job's
// sub-managers. Per DEEP-7: same rationale as buildScrapeInputs.
func (c *jobController) buildRescrapeInputs(wf workflow.WorkflowInterface, batchCfg BatchJobConfig) rescrapePhaseInputs {
	c.job.mu.RLock()
	pg := c.job.deps.PosterGen
	pfn := c.job.deps.PersistFn
	tempDir := c.job.cfg.tempDir
	c.job.mu.RUnlock()

	return rescrapePhaseInputs{
		JobID:       c.job.ID,
		Concurrency: newConcurrencyConfig(batchCfg.MaxWorkers, batchCfg.WorkerTimeout, defaultMaxWorkers, defaultWorkerTimeout),
		WF:          wf,
		PosterGen:   pg,
		ResultMap:   c.job.resultIndex,
		Lifecycle:   c.job.lifecycle,
		persister:   persistFunc(pfn),
		Lookup:      c.job.resultIndex,
		Finder:      c.job.resultIndex,
		Fs:          c.job.fs,
		TempDir:     tempDir,
		FsCaseCache: c.job.fsCaseCache,
	}
}

// resolveWF reads the workflow from job.deps under the mutex.
// Per DEEP-6: replaced the old resolveDeps (which accepted per-call WF overrides)
// with this simpler method that only reads from job.deps. API handlers set
// deps.WF via SetWorkflow before calling phase methods on reconstructed jobs.
func (c *jobController) resolveWF() workflow.WorkflowInterface {
	c.job.mu.RLock()
	wf := c.job.deps.WF
	c.job.mu.RUnlock()
	return wf
}

// resolveBatchCfg reads the BatchJobConfig from job.deps under the mutex.
// Per DEEP-6: replaced the old resolveAndStoreBatchCfg (which accepted per-call
// overrides and had store-back logic) with this simpler method. BatchCfg is
// set at construction time via JobConfig.BatchJobDeps.BatchCfg.
func (c *jobController) resolveBatchCfg() BatchJobConfig {
	c.job.mu.RLock()
	cfg := c.job.deps.BatchCfg
	c.job.mu.RUnlock()
	return cfg
}

// SetWorkflow sets the workflow seam on the job's deps.
// Per DEEP-1: moved from *BatchJob to jobController — BatchJob is a pure state
// container and does not own dependency mutation.
// Per DEEP-6: API handlers use this to inject a fresh WF per request on
// reconstructed jobs (loaded from DB with nil deps.WF) before calling
// phase methods. Freshly-created jobs already have deps.WF set at
// construction time via JobConfig.BatchJobDeps.WF.
//
// Callers must ensure no phase is actively using the old workflow when calling
// this — the mutex protects concurrent SetWorkflow calls, but does not prevent
// a running phase from seeing an inconsistent workflow mid-execution.
func (c *jobController) SetWorkflow(wf workflow.WorkflowInterface) {
	c.job.mu.Lock()
	c.job.deps.WF = wf
	c.job.mu.Unlock()
}

// SetBatchCfg sets the batch configuration on the job's deps.
// Per DEEP-1: moved from *BatchJob to jobController — BatchJob is a pure state
// container and does not own dependency mutation.
// Per DEEP-6: replaces the per-call BatchCfg overrides that were previously
// on ScrapePhaseConfig and ApplyPhaseConfig. BatchCfg is set on job.deps
// at construction time or via this method before phase calls.
// Not goroutine-safe — callers must serialize with phase execution.
func (c *jobController) SetBatchCfg(cfg BatchJobConfig) {
	c.job.mu.Lock()
	c.job.deps.BatchCfg = cfg
	c.job.mu.Unlock()
}

// SetJobStatus sets the job status directly. Per DEEP-1: moved from *BatchJob
// to jobController — lifecycle transitions are a controller concern.
// This is a test helper that bypasses the normal lifecycle (Done channel,
// CancelFunc). Tests that need Wait() to return should use
// MarkCompleted/MarkFailed/MarkCancelled instead.
func (c *jobController) SetJobStatus(status models.JobStatus) {
	c.job.lifecycle.mu.Lock()
	c.job.lifecycle.Status = status
	switch status {
	case models.JobStatusRunning:
		c.job.lifecycle.OrganizedAt = nil
		c.job.lifecycle.RevertedAt = nil
	case models.JobStatusCompleted:
		c.job.lifecycle.CompletedAt = nowTimePtr()
	case models.JobStatusOrganized:
		c.job.lifecycle.OrganizedAt = nowTimePtr()
	case models.JobStatusReverted:
		c.job.lifecycle.RevertedAt = nowTimePtr()
	}
	c.job.lifecycle.mu.Unlock()

	if status == models.JobStatusRunning {
		c.job.mu.Lock()
		c.job.StartedAt = time.Now()
		c.job.mu.Unlock()
	}
}

// SetOperationModeOverride sets the operation mode for the job.
// Per DEEP-1: moved from *BatchJob to jobController — BatchJob is a pure state
// container and does not own config mutation.
// This is the public escape hatch for callers that need to restore
// operationMode after DB reconstruction, when StartApply has not yet
// been called. Returns error for invalid values instead of panicking.
func (c *jobController) SetOperationModeOverride(mode operationmode.OperationMode) error {
	if mode != "" && !mode.IsValid() {
		return fmt.Errorf("SetOperationModeOverride: invalid operation mode %q", mode)
	}
	if mode == "" {
		mode = operationmode.OperationModeOrganize
	}
	c.job.mu.Lock()
	c.job.cfg.operationMode = mode
	c.job.mu.Unlock()
	return nil
}

// SetPersistError sets the persist error message on the job.
// Per DEEP-1: moved from *BatchJob to jobController — BatchJob is a pure
// state container and does not own mutation. The persistError field is
// an output written by the persistence layer and read via GetPersistError().
func (c *jobController) SetPersistError(msg string) {
	c.job.mu.Lock()
	defer c.job.mu.Unlock()
	c.job.persistError = msg
}

// Controller returns the PhaseController for this job.
// Per DEEP-1: callers that need phase execution (StartScrape, StartApply,
// Rescrape, Wait) should use this controller rather than calling methods on
// *BatchJob directly. BatchJob is a pure state container — phase orchestration
// is owned by jobController.
func (job *BatchJob) Controller() PhaseController {
	return job.controller
}
