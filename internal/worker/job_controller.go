package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
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
		c.job.results.SetFileMatchInfoMap(cfg.FileMatchInfo)
	}

	// codex r46 P2: atomic launch claim — markStarted CAS-claims the
	// lifecycle (Pending→Running + marker) BEFORE any phase admission, so a
	// duplicate/cancelled queued launch never registers pendingPhase behind
	// the winner's lease.
	if c.job.admission.IsGone() {
		return ErrJobGone // gone check first (admit-before-state contract)
	}
	ctx, cancel := context.WithCancel(ctx)
	pd, err := c.markStarted(models.JobStatusPending, JobPhaseScrape, cancel)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			cancel()
			return nil // cancelled before the claim — nothing runs
		}
		cancel()
		return err // CAS loser: duplicate launch rejected up-front
	}
	entry, err := c.job.admission.BeginPhase(ctx)
	if err != nil {
		cancel()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// claimed-but-never-launched: legacy-observable Cancelled, marker
			// cleared BEFORE the durable persist (codex r36/r37/r45).
			c.job.lifecycle.Cancel()
			c.job.lifecycle.SetCurrentPhase("")
			close(pd)
			if persistFn != nil {
				if perr := persistFn(); perr != nil {
					logging.Warnf("job %s: aborted-scrape cancel persist failed: %v", c.job.ID.String(), perr)
				}
			}
			return nil
		}
		c.job.lifecycle.MarkFailed()
		close(pd)
		return err // ErrJobGone when deleted mid-wait
	}
	release := entry.Downgrade()
	if persistFn != nil {
		// D16 fail-closed: the current_phase marker must be durable before
		// any scrape work begins; a failed marker persist aborts the start.
		if err := persistFn(); err != nil {
			release()
			cancel()
			c.job.lifecycle.MarkFailed()
			close(pd) // no phase goroutine will run; Wait() joins on phaseDone
			// codex P8: the terminal failure must ALSO be durable — a single
			// best-effort retry so the DB row doesn't stay pending until
			// restart reconverges it.
			if err2 := persistFn(); err2 != nil {
				logging.Warnf("[Scrape] post-abort persist failed for job %s: %v", c.job.ID.String(), err2)
			}
			return fmt.Errorf("job %s: persist phase-entry marker: %w", c.job.ID.String(), err)
		}
	}

	go func() {
		// Admission lease spans goroutine start through the FINAL envelope
		// persist (Run's defer) so DeleteJob's exclusive drain can never
		// reclaim the job mid-flush (D1/D16).
		// codex r44 P2: close phaseDone LAST — Wait() must join the ENTIRE
		// quiesced defer stack (marker-clear persist + lease release), not fire
		// while the final persist still runs. Defers execute LIFO, so register
		// the close FIRST.
		defer close(pd)
		defer release()
		// Clear the phase marker only after the worker's final write — a
		// cancelled Running phase stays fenced until its last flush (codex r38).
		defer func() {
			c.job.lifecycle.SetCurrentPhase("")
			// codex P2-I: persist the cleared marker too — the in-Run defer
			// carries the marker SET into the final database row; without this
			// second persist, a restart restores the stale marker and every edit
			// on the terminated job is rejected by the admission guard.
			if persistFn != nil {
				if err := persistFn(); err != nil {
					logging.Warnf("[BatchJob] %s marker-clear persist on phase end failed: %v", c.job.ID.String(), err)
				}
			}
		}()
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

	// codex r46 P2: atomic launch claim. markStarted CAS-claims the lifecycle
	// BEFORE any phase admission — a duplicate or cancelled queued launch
	// never gets the chance to register a pendingPhase behind the running
	// winner (which would stall every shared admission for its whole run).
	if c.job.admission.IsGone() {
		return ErrJobGone // gone check first (documented admit-before-state order)
	}
	ctx, cancel := context.WithCancel(ctx)
	// codex P1-G: the admission winner installs the CancelFunc below — a
	// queued start never supplants the running phase's cancel handle.
	pd, err := c.markStarted(models.JobStatusCompleted, JobPhaseApply, cancel)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			cancel()
			return nil
		}
		cancel()
		return err // CAS loser: duplicate launch rejected up-front
	}
	entry, err := c.job.admission.BeginPhase(ctx)
	if err != nil {
		cancel()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// claimed-but-never-launched: the legacy-observable Cancelled
			// terminal with NO Running work — marker cleared FIRST so the
			// persisted row is edit-admissible again (codex r36/r37/r45).
			c.job.lifecycle.Cancel()
			c.job.lifecycle.SetCurrentPhase("")
			close(pd)
			if persistFn != nil {
				if perr := persistFn(); perr != nil {
					logging.Warnf("job %s: aborted-apply cancel persist failed: %v", c.job.ID.String(), perr)
				}
			}
			return nil
		}
		// ErrJobGone mid-wait: the delete path owns the row — close the join
		// channel so Wait() doesn't hang; no envelope write (row going away).
		c.job.lifecycle.MarkFailed()
		close(pd)
		return err
	}
	release := entry.Downgrade()

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
		if err := persistFn(); err != nil {
			release()
			cancel()
			c.job.lifecycle.MarkFailed()
			close(pd) // no phase goroutine will run; Wait() joins on phaseDone
			if err2 := persistFn(); err2 != nil {
				logging.Warnf("[Apply] post-abort persist failed for job %s: %v", c.job.ID.String(), err2)
			}
			return fmt.Errorf("job %s: persist phase-entry marker: %w", c.job.ID.String(), err)
		}
	}

	go func() {
		// Admission lease spans through Run's deferred final persist (D1/D16).
		// codex r44 P2: close phaseDone LAST (register first — LIFO) so Wait()
		// joins the whole quiesced stack incl. the marker-clear persist.
		defer close(pd)
		defer release()
		defer func() {
			c.job.lifecycle.SetCurrentPhase("")
			// codex P2-I: persist the cleared marker too — the in-Run defer
			// carries the marker SET into the final database row; without this
			// second persist, a restart restores the stale marker and every edit
			// on the terminated job is rejected by the admission guard.
			if persistFn != nil {
				if err := persistFn(); err != nil {
					logging.Warnf("[BatchJob] %s marker-clear persist on phase end failed: %v", c.job.ID.String(), err)
				}
			}
		}()
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

	// Network work runs WITHOUT the family key (codex r20): scraping is a
	// long network section — holding the key there would stall every edit on
	// the family until timeout. Serialization lives at the COMMIT leg (see
	// familyKeyedResultMap in buildRescrapeInputs) and at provenance below.
	var outcome *RescrapeResult
	var err error
	// Rescrape (network + scrape) runs WITHOUT the family key — that section
	// is too long to hold locks (codex r20). The COMMIT leg serializes via
	// familyKeyedResultMap on the store side, and provenance publishes inside
	// the SAME keyed CommitResult section (codex r36 P1): no post-hoc locked
	// tail, so a concurrent field override can never slip into a
	// commit/provenance gap and have its attribution clobbered.
	outcome, err = c.job.rescrapePhase.Rescrape(ctx, inputs, cmd)
	if err != nil {
		return outcome, err
	}
	return outcome, nil
}

// "Fully settles" means the phase goroutine has RETURNED, including its
// deferred persistence — not merely that a terminal status was set. Run
// marks the terminal status (closing lifecycle.done) before its deferred
// persister.Persist() executes, so joining on done alone would let waiters
// (teardown drains, CLI phase chains) race the final DB writes. When no
// phase was ever started, phaseDone is nil and done (terminal status) is
// the join point instead.
func (c *jobController) Wait() error {
	c.job.lifecycle.mu.RLock()
	pd := c.job.lifecycle.phaseDone
	done := c.job.lifecycle.done
	c.job.lifecycle.mu.RUnlock()
	if pd != nil {
		<-pd
	} else {
		<-done
	}
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

// markStarted transitions the job from expectedFrom to running state and creates
// fresh Done/phaseDone channels, returning the phaseDone channel the phase
// goroutine must close when it fully returns. It performs a compare-and-swap:
// if the lifecycle status is not expectedFrom when the lock is acquired, it
// returns an error without modifying state.
// This prevents the TOCTOU race where an API handler checks status == Completed but
// another concurrent request transitions the job before this call acquires the lock.
// codex r47-followup P1: the claim installs the phase's cancel handle in the
// SAME critical section — a cancellation racing claim→bind previously marked
// the job Cancelled while leaving this launch's ctx unbound, so BeginPhase
// would start work after cancellation.
func (c *jobController) markStarted(expectedFrom models.JobStatus, phase JobPhase, cancelFunc context.CancelFunc) (chan struct{}, error) {
	c.job.lifecycle.mu.Lock()
	if c.job.lifecycle.Status != expectedFrom {
		actual := c.job.lifecycle.Status
		c.job.lifecycle.mu.Unlock()
		return nil, fmt.Errorf("job %s: cannot start — expected status %s but got %s", c.job.ID.String(), expectedFrom, actual)
	}
	if c.job.lifecycle.cancelled {
		c.job.lifecycle.mu.Unlock()
		return nil, context.Canceled // a cancel landed before the claim
	}
	c.job.lifecycle.CancelFunc = cancelFunc
	c.job.lifecycle.Status = models.JobStatusRunning
	c.job.lifecycle.currentPhase = string(phase)
	c.job.lifecycle.CompletedAt = nil
	c.job.lifecycle.OrganizedAt = nil
	c.job.lifecycle.done = make(chan struct{})
	c.job.lifecycle.phaseDone = make(chan struct{})
	pd := c.job.lifecycle.phaseDone
	c.job.lifecycle.mu.Unlock()

	c.job.mu.Lock()
	c.job.StartedAt = time.Now()
	c.job.mu.Unlock()

	return pd, nil
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
	if cfg.BatchCfg.MaxWorkers > 0 || cfg.BatchCfg.WorkerTimeout > 0 || cfg.BatchCfg.RequestTimeout > 0 || len(cfg.BatchCfg.ScraperPriority) > 0 {
		c.job.deps.BatchCfg = cfg.BatchCfg
	}
	if cfg.BatchFileOpRepo != nil {
		c.job.deps.BatchFileOpRepo = cfg.BatchFileOpRepo
	}
	if cfg.MovieRepo != nil {
		c.job.deps.MovieRepo = cfg.MovieRepo
		c.job.posterEditor.setMovieRepo(cfg.MovieRepo)
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
func (c *jobController) buildScrapeInputs(wf workflow.WorkflowInterface, batchCfg BatchJobConfig, persistFn func() error) scrapePhaseInputs {
	c.job.mu.RLock()
	m := c.job.deps.Matcher
	pg := c.job.deps.PosterGen
	movieRepo := c.job.deps.MovieRepo
	histRepo := c.job.deps.HistoryRepo
	c.job.mu.RUnlock()

	c.job.batchJobEventSource.mu.RLock()
	keepOpen := c.job.keepBroadcasterOpen
	broadcaster := c.job.eventBroadcaster
	c.job.batchJobEventSource.mu.RUnlock()

	fileMatchInfo := c.job.results.CloneFileMatchInfo()

	inputs := scrapePhaseInputs{
		JobID:               c.job.ID,
		Concurrency:         newConcurrencyConfig(batchCfg.MaxWorkers, batchCfg.WorkerTimeout, batchCfg.RequestTimeout, defaultMaxWorkers, defaultWorkerTimeout),
		WF:                  wf,
		PosterGen:           pg,
		KeepBroadcasterOpen: keepOpen,
		Broadcaster:         broadcaster,
		Updater:             c.job.results,
		Lifecycle:           c.job.lifecycle,
		persister:           persistFunc(persistFn),
		FileMatchInfo:       fileMatchInfo,
		MovieRepo:           movieRepo,
		HistoryRepo:         histRepo,
	}
	if m != nil {
		inputs.Matcher = m
	}
	return inputs
}

// buildApplyInputs constructs applyPhaseInputs directly from the job's
// sub-managers. Per DEEP-7: same rationale as buildScrapeInputs.
func (c *jobController) buildApplyInputs(wf workflow.WorkflowInterface, batchCfg BatchJobConfig, cfg ApplyPhaseConfig, persistFn func() error) applyPhaseInputs {
	c.job.batchJobEventSource.mu.RLock()
	broadcaster := c.job.eventBroadcaster
	c.job.batchJobEventSource.mu.RUnlock()

	snap := c.job.results.SnapshotData()

	c.job.mu.RLock()
	upd := c.job.cfg.update
	opMode := string(c.job.cfg.operationMode)
	histRepo := c.job.deps.HistoryRepo
	c.job.mu.RUnlock()
	if opMode == "" {
		opMode = "organize"
	}

	return applyPhaseInputs{
		JobID:            c.job.ID,
		EditLockFn:       func(movieIDs ...string) func() { return c.job.posterEditor.lockRegistry().AcquireMany(movieIDs) },
		PromoteWitnessFn: c.job.posterEditor.hasUnresolvedPromoteWitness,
		Concurrency:      newConcurrencyConfig(batchCfg.MaxWorkers, batchCfg.WorkerTimeout, batchCfg.RequestTimeout, 1, defaultWorkerTimeout),
		NFOEnabled:       batchCfg.NFOEnabled,
		WF:               wf,
		Results:          snap.Results,
		Provenance:       snap.Provenance,
		Excluded:         snap.Excluded,
		Destination:      cfg.Destination,
		Update:           upd,
		HistoryRepo:      histRepo,
		OperationMode:    opMode,
		OrganizeSkipped:  cfg.OrganizeOptions.Skip,
		Broadcaster:      broadcaster,
		Updater:          c.job.results,
		Lifecycle:        c.job.lifecycle,
		persister:        persistFunc(persistFn),
	}
}

// buildRescrapeInputs constructs rescrapePhaseInputs directly from the job's
// sub-managers. Per DEEP-7: same rationale as buildScrapeInputs.
func (c *jobController) buildRescrapeInputs(wf workflow.WorkflowInterface, batchCfg BatchJobConfig) rescrapePhaseInputs {
	c.job.mu.RLock()
	pg := c.job.deps.PosterGen
	pfn := c.job.deps.PersistFn
	tempDir := c.job.cfg.tempDir
	histRepo := c.job.deps.HistoryRepo
	c.job.mu.RUnlock()

	return rescrapePhaseInputs{
		JobID:       c.job.ID,
		Concurrency: newConcurrencyConfig(batchCfg.MaxWorkers, batchCfg.WorkerTimeout, batchCfg.RequestTimeout, defaultMaxWorkers, defaultWorkerTimeout),
		WF:          wf,
		PosterGen:   pg,
		HistoryRepo: histRepo,
		// Commit leg wraps through the family lock (codex r20): the scrape's network section stays unlocked; CommitResult serializes with concurrent family edits on the process-wide registry.
		ResultMap: &familyKeyedResultMap{
			ResultMapAccessor: c.job.results,
			updater:           c.job.results,
			registry:          c.job.posterEditor.lockRegistry(),
			fs:                c.job.fs,
			tempDir:           c.job.cfg.tempDir,
			jobID:             c.job.ID.String(),
		},
		EditLockFn:  func(ids ...string) func() { return c.job.posterEditor.lockRegistry().AcquireMany(ids) },
		Lifecycle:   c.job.lifecycle,
		persister:   persistFunc(pfn),
		Finder:      c.job.results,
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
