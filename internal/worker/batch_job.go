package worker

import (
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
)

// BatchJobDeps groups the 9 infrastructure dependencies that BatchJob uses
// for phase orchestration (scrape, apply, rescrape) and movie persistence.
// All fields are optional — a job with nil deps can still be used for status
// queries and result editing, but StartScrape/StartApply will return errors
// if the required dependencies (WF, BatchCfg) are missing.
//
// Construction: callers provide BatchJobDeps via JobConfig. The 3 construction
// paths (newBatchJob, createJob, reconstructBatchJob) each build a BatchJobDeps
// and pass it to the shared initializer setDepsFromConfig.
type BatchJobDeps struct {
	WF              workflow.WorkflowInterface                     // Workflow seam for Scrape/Apply calls
	Matcher         matcher.MatcherInterface                       // JAV ID extraction from filenames
	PosterGen       poster.PosterGenerator                         // Poster generation for scraped movies
	BatchCfg        BatchJobConfig                                 // Narrow config fields (replaces *config.Config)
	BatchFileOpRepo database.BatchFileOperationRepositoryInterface // Batch file operations repository
	MovieRepo       database.MovieRepositoryInterface              // Movie persistence for batch editing
	HistoryRepo     database.HistoryRepositoryInterface            // History repository
	Emitter         eventlog.EventEmitter                          // Event emission for audit trail
	PersistFn       func()                                         // Callback to persist job state to database
	Logger          logging.Logger                                 // Structured logger seam; defaults to GlobalLogger() when nil
}

// NewBatchJobDeps constructs a BatchJobDeps with the three core dependencies
// (workflow, matcher, poster generator) and the narrow batch configuration.
// This is the single source of truth for BatchJobDeps construction — all callers
// (CLI, API, TUI) should use this constructor instead of manual struct literals,
// ensuring that new BatchJobDeps fields are added in exactly one place.
//
// Optional fields (BatchFileOpRepo, MovieRepo, HistoryRepo, Emitter, PersistFn,
// Logger) can be set on the returned struct directly after construction.
func NewBatchJobDeps(wf workflow.WorkflowInterface, m matcher.MatcherInterface, posterGen poster.PosterGenerator, batchCfg BatchJobConfig) BatchJobDeps {
	return BatchJobDeps{
		WF:        wf,
		Matcher:   m,
		PosterGen: posterGen,
		BatchCfg:  batchCfg,
	}
}

// JobConfig holds workflow and infrastructure dependencies for a BatchJob.
// These are set at creation time and enable StartScrape/StartApply orchestration.
// All fields are optional — a job created without JobConfig can still be used
// for status queries and result editing, but StartScrape/StartApply will return
// errors if the required dependencies (WF, BatchCfg) are missing.
//
// Destination, OperationModeOverride, and Update are creation-time defaults
// persisted on the job for UI retrieval. If StartApply's ApplyPhaseConfig also
// provides these fields, the apply-time values take precedence.
//
// BatchCfg consumption by BatchJob:
//   - MaxWorkers      → pool sizing for scrape and apply
//   - WorkerTimeout   → timeout for scrape and apply
//   - ScraperPriority → selected scrapers for batch scrape
//   - NFOEnabled      → NFO generation toggle during apply
type JobConfig struct {
	ID                    string                      // Pre-generated job ID (if empty, UUID is auto-generated)
	Destination           string                      // Target directory for organized files (persisted on job for UI retrieval)
	OperationModeOverride operationmode.OperationMode // Resolved operation mode (set at API boundary per ADR-0030)
	Update                *bool                       // Update mode: nil = don't change, true/false = set explicitly
	BatchJobDeps                                      // Embedded deps — all 9 infrastructure fields promoted
}

// setDepsFromConfig is owned by jobController. Per DEEP-1: dependency wiring
// is a controller concern, not a state-container concern.
// See jobController.setDepsFromConfig.

// jobConfig holds configuration fields that are set at creation time or
// from ApplyPhaseConfig at StartApply and persisted for UI retrieval.
// Separated from BatchJob to distinguish config from runtime state.
type jobConfig struct {
	destination   string                      // Target directory for organized files
	tempDir       string                      // Temp directory for poster paths
	operationMode operationmode.OperationMode // Resolved operation mode (set at API boundary per ADR-0030)
	update        bool                        // Update mode (in-place, no file organization)
}

// BatchJob represents a batch processing job.
// Per DEEP-1: BatchJob is a pure state container — lifecycle, results, cfg, deps,
// and events. All mutation and orchestration methods are owned by jobController
// (SetWorkflow, SetBatchCfg, SetJobStatus, SetOperationModeOverride, StartScrape,
// StartApply, Rescrape, Wait, markStarted) or JobRunner (Run, SetRunOptions).
// BatchJob exposes only read accessors and attachLifecycleCallback().
type BatchJob struct {
	ID          models.JobID     `json:"id"`
	mu          sync.RWMutex     `json:"-"`
	lifecycle   *JobLifecycle    `json:"-"` // Lifecycle state: status, timestamps, Done channel
	results     *ResultTracker   `json:"-"` // Result state: Results map, progress counters, files
	resultIndex ResultReadFacade `json:"-"` // Same pointer as results — provides lookup-oriented surface (MovieLookup, FileFinder, ResultMapAccessor)

	// Retained on BatchJob (not mutex-protected state groups)
	StartedAt time.Time `json:"started_at"`

	// Configuration — set from ApplyPhaseConfig at StartApply or from DB during reconstruction.
	// External callers cannot mutate these; configuration flows through StartApply(ctx, ApplyPhaseConfig).
	cfg                 jobConfig                // Job config: destination, tempDir, operationMode, update
	persistError        string                   // Output field written by persistToDatabase, read via GetPersistError()
	templateEngine      template.EngineInterface `json:"-"` // Shared template engine (safe for concurrent use)
	batchJobEventSource `json:"-"`               // Event streaming (embedded: Subscribe, SendJobEvent, CloseEventBroadcaster)
	fs                  afero.Fs                 `json:"-"` // Filesystem for poster cleanup (nil = OS fs)

	// Infrastructure dependencies (set via JobConfig, not persisted)
	deps BatchJobDeps `json:"-"`

	// Sub-orchestrators (delegated from jobController methods)
	rescrapePhase RescrapePhase  `json:"-"` // Rescrape phase sub-orchestrator
	scrapePhase   ScrapePhase    `json:"-"` // Scrape phase sub-orchestrator
	applyPhase    ApplyPhase     `json:"-"` // Apply phase sub-orchestrator
	posterEditor  *PosterEditor  `json:"-"` // Poster mutation delegation (extracted from BatchJob)
	controller    *jobController `json:"-"` // Phase-launch orchestration (DEEP-1: owns StartScrape/StartApply/Rescrape)

	fsCaseCache *FSCaseCache `json:"-"` // Per-job filesystem case-sensitivity cache

	adapters     *jobAdapters `json:"-"` // Cached adapters — built once, used by JobStore methods (D-6)
	adaptersOnce sync.Once    `json:"-"` // Guards adapters lazy-init against concurrent JobStore RLock callers
}

// newBatchJob creates a BatchJob without a JobStore.
// Per NEW-2: this is unexported because all external construction should
// route through JobStore.CreateJob or JobStore.CreateJobBatch, which
// provides the single construction path with JobStore registration and
// PersistFn wiring. This function is only used internally by
// JobStore.createJob as the base constructor.
//
// The caller must provide JobConfig with at least WF and BatchCfg set
// before calling Run(), StartScrape(), or StartApply().
func newBatchJob(files []string, jobCfg ...*JobConfig) *BatchJob {
	job := &BatchJob{
		ID: models.NewJobID(),
		lifecycle: &JobLifecycle{
			Status: models.JobStatusPending,
			done:   make(chan struct{}),
		},
		results:             NewResultTracker(len(files), files),
		StartedAt:           time.Now(),
		batchJobEventSource: newBatchJobEventSource(),
		rescrapePhase:       NewRescrapePhase(),
		scrapePhase:         NewScrapePhase(),
		applyPhase:          NewApplyPhase(),
		fsCaseCache:         NewFSCaseCache(nil),
	}

	// Per P-2: wireJobDeps centralizes attachLifecycleCallback, posterEditor,
	// controller, and PersistFn wiring shared with reconstructBatchJob.
	wireJobDeps(job, nil, nil)

	if len(jobCfg) > 0 && jobCfg[0] != nil {
		cfg := jobCfg[0]
		if cfg.ID != "" {
			job.ID = models.MustJobID(cfg.ID)
		}
		if cfg.Destination != "" {
			job.cfg.destination = cfg.Destination
		}
		if cfg.OperationModeOverride != "" {
			job.cfg.operationMode = cfg.OperationModeOverride
		}
		if cfg.Update != nil {
			job.cfg.update = *cfg.Update
		}
		job.controller.setDepsFromConfig(cfg)
	}

	return job
}

func isJobTransitioned(status models.JobStatus) bool {
	// "Transitioned" for the gone-check means the job has left the rescrapeable
	// active set. This intentionally includes Running (a job that already moved
	// past Pending is not rescrapeable mid-flight and reads as gone to
	// CompleteRescrape) and the terminal failure/cancel/revert states, but
	// EXCLUDES Completed and Organized — a Completed/Organized job remains a
	// valid rescrape source (its results are still authoritative). The
	// excludeFile cancellation guard below uses a separate terminal-success
	// check so it does not rely on this predicate's semantics.
	return status == models.JobStatusRunning ||
		status == models.JobStatusFailed ||
		status == models.JobStatusCancelled ||
		status == models.JobStatusReverted
}

// ExcludeFile is owned by jobEditorImpl (via the JobEditor interface).
// Per DEEP-1: BatchJob is a pure state container — file exclusion crosses the
// results/lifecycle boundary and is available through the EditableJob/
// BatchJobInterface adapter. Callers with *BatchJob should use
// job.Results().MarkExcluded() + job.Lifecycle() directly, or use the
// JobEditor interface obtained from JobStore.GetJobForEdit.

// attachLifecycleCallback sets the markCompletedFn callback on JobLifecycle
// so that MarkCompleted can handle the cross-boundary progress recalculation.
// Also sets the goneChecker callback on ResultTracker so it can satisfy
// ResultMapAccessor.IsGone(). Per ADR-0042: consolidated from 3 construction
// paths into a single method.
// getAdapters returns the cached jobAdapters for this BatchJob, constructing them
// on first call. Per D-6: buildAdapters was called 4 times per job (CreateJob,
// GetJobForEdit, GetJobForControl, GetBatchJob); caching eliminates the redundant
// construction. Thread-safe: a sync.Once guards the lazy init, so concurrent
// JobStore callers (which hold the store's RLock, allowing multiple readers)
// can all enter getAdapters simultaneously without racing the adapters write.
// After the first call returns, subsequent calls read the stable pointer with
// no lock contention.
func (job *BatchJob) getAdapters() *jobAdapters {
	job.adaptersOnce.Do(func() {
		job.adapters = buildAdapters(job)
	})
	return job.adapters
}

func (job *BatchJob) attachLifecycleCallback() {
	job.lifecycle.markCompletedFn = func() {
		// Must hold results.mu while reading/writing results state.
		// Lock ordering: lifecycle.mu -> results.mu (same as old MarkCompleted).
		job.results.mu.Lock()
		job.results.recalculateProgress()
		if job.results.Progress < 100 {
			job.results.Progress = 100
		}
		job.results.mu.Unlock()
	}
	job.results.goneChecker = func() bool {
		job.lifecycle.mu.RLock()
		defer job.lifecycle.mu.RUnlock()
		return job.lifecycle.deleted || isJobTransitioned(job.lifecycle.Status)
	}
	job.resultIndex = job.results // *ResultTracker satisfies ResultReadFacade
}

// batchJobSnapshot is a consistent point-in-time view of all BatchJob state.
// Per ADR-0025: the shared base eliminates the field-fan-out across GetStatus,
// snapshotForPersist — adding a new field changes
// batchJobBase + batchJobSnapshot, not three separate methods.
type batchJobSnapshot struct {
	batchJobBase

	results     map[string]*MovieResult
	provenance  map[string]*ProvenanceData
	resultIndex map[string]string // ResultID → FilePath
}

// snapshotFull acquires lifecycle.mu → results.mu → job.mu before reading any
// fields, then copies a consistent point-in-time view of all job state.
// Per ADR-0041/0042: uses StatusSnapshot() and SnapshotForStatus() instead of
// reaching into sub-manager internals.
func (job *BatchJob) snapshotFull() batchJobSnapshot {
	job.lifecycle.mu.RLock()
	job.results.mu.RLock()
	job.mu.RLock()

	defer job.mu.RUnlock()
	defer job.results.mu.RUnlock()
	defer job.lifecycle.mu.RUnlock()

	lcSnap := job.lifecycle.statusSnapshotLocked()
	resultSnap, progressSnap := job.results.snapshotForStatusLocked()

	return batchJobSnapshot{
		batchJobBase: batchJobBase{
			ID:                    job.ID,
			Status:                lcSnap.Status,
			TotalFiles:            progressSnap.TotalFiles,
			Completed:             progressSnap.Completed,
			Failed:                progressSnap.Failed,
			Excluded:              resultSnap.Excluded,
			Files:                 resultSnap.Files,
			FileMatchInfo:         resultSnap.FileMatchInfo,
			Progress:              progressSnap.Progress,
			Destination:           job.cfg.destination,
			TempDir:               job.cfg.tempDir,
			StartedAt:             job.StartedAt,
			CompletedAt:           lcSnap.CompletedAt,
			OrganizedAt:           lcSnap.OrganizedAt,
			RevertedAt:            lcSnap.RevertedAt,
			OperationModeOverride: job.cfg.operationMode,
			Update:                job.cfg.update,
			PersistError:          job.persistError,
			IsDeleted:             lcSnap.IsDeleted,
		},
		results:     resultSnap.Results,
		provenance:  resultSnap.Provenance,
		resultIndex: resultSnap.ResultIDIndex,
	}
}

func cloneTimePtr(src *time.Time) *time.Time {
	if src == nil {
		return nil
	}
	t := *src
	return &t
}

func (job *BatchJob) GetStatus() *BatchJobStatus {
	snapshot := job.snapshotFull()
	return &BatchJobStatus{
		batchJobBase: snapshot.batchJobBase,
		Results:      snapshot.results,
		ResultIndex:  snapshot.resultIndex,
		Provenance:   snapshot.provenance,
	}
}

func (job *BatchJob) GetTempDir() string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.cfg.tempDir
}

func (job *BatchJob) GetOperationModeOverride() operationmode.OperationMode {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.cfg.operationMode
}

// SetOperationModeOverride is owned by jobController. Per DEEP-1: BatchJob is a
// pure state container — config mutation is a controller concern.
// See jobController.SetOperationModeOverride.
// Callers with *BatchJob should use job.Controller().SetOperationModeOverride(mode).

func (job *BatchJob) GetDestination() string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.cfg.destination
}

// Per DEEP-2: passthrough methods removed. Callers access sub-managers
// through composite interfaces (EditableJob, ControlledJob, BatchJobInterface)
// or directly via job.results / job.lifecycle within the worker package.
// For external packages (tests, CLI) that need direct sub-manager access,
// use the Results() and Lifecycle() accessors.

// Results returns the job's ResultTracker for read-only access to result
// state via the ResultMapAccessor interface. Per N-6: callers that need
// write access should obtain a ResultUpdater through the adapter layer
// (EditableJob, BatchJobInterface) rather than reaching through Results().
// Tests that need direct write access can use ResultsWriter().
func (job *BatchJob) Results() ResultMapAccessor {
	return job.results
}

// ResultsWriter returns the job's ResultTracker for direct write access.
// Per N-6: this is package-internal — external callers should use narrow
// interfaces (ResultUpdater, ResultReadFacade) or the adapter layer
// (EditableJob, BatchJobInterface) instead of reaching through ResultsWriter().
// Exported for test access only; production code should prefer ResultUpdater.
func (job *BatchJob) ResultsWriter() *ResultTracker {
	return job.results
}

// Lifecycle returns the job's JobLifecycle for direct access to lifecycle
// mutation and query methods. Per DEEP-2: replaces the 3 lifecycle-related
// passthrough methods (GetJobStatus, Cancel, SetDeleted) with a single
// accessor that lets callers reach the sub-manager directly.
func (job *BatchJob) Lifecycle() *JobLifecycle {
	return job.lifecycle
}

// markStarted is owned by jobController. Per DEEP-1: lifecycle transitions
// during phase launch are a controller concern.
// See jobController.markStarted.

func (job *BatchJob) GetID() string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.ID.String()
}

// SetPersistError is owned by jobController. Per DEEP-1: BatchJob is a pure
// state container — mutation is a controller concern.
// See jobController.SetPersistError.
// Callers with *BatchJob should use job.Controller().SetPersistError(msg).

// GetPersistError returns the current persist error message in a thread-safe manner.
func (job *BatchJob) GetPersistError() string {
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.persistError
}

// SetWorkflow is owned by jobController. Per DEEP-1: BatchJob is a pure state
// container — dependency mutation is a controller concern.
// See jobController.SetWorkflow.
// Callers with *BatchJob should use job.Controller().SetWorkflow(wf).
// Production code should use the PhaseController interface.

// SetBatchCfg is owned by jobController. Per DEEP-1: BatchJob is a pure state
// container — dependency mutation is a controller concern.
// See jobController.SetBatchCfg.
// Callers with *BatchJob should use job.Controller().SetBatchCfg(cfg).

// SetJobStatus is owned by jobController. Per DEEP-1: BatchJob is a pure state
// container — lifecycle transitions are a controller concern.
// See jobController.SetJobStatus.
// Callers with *BatchJob should use job.Controller().SetJobStatus(status).
// Production code should use the PhaseController interface.

func (job *BatchJob) TemplateEngine() template.EngineInterface {
	job.mu.RLock()
	eng := job.templateEngine
	job.mu.RUnlock()
	if eng != nil {
		return eng
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.templateEngine == nil {
		job.templateEngine = template.NewEngine()
	}
	return job.templateEngine
}

// Rescrape is owned by jobController. Per DEEP-1: phase-launch orchestration
// is a controller concern. See jobController.Rescrape.
// Callers should use job.Controller().Rescrape(ctx, cmd) or the PhaseController interface.

// resolveWF and resolveBatchCfg are owned by jobController. Per DEEP-1: dependency
// resolution is a controller concern. See jobController.resolveWF and resolveBatchCfg.

// StartScrape is owned by jobController. Per DEEP-1: phase-launch orchestration
// is a controller concern. See jobController.StartScrape.
// Callers should use job.Controller().StartScrape(ctx, files, cfg) or the PhaseController interface.

// StartApply is owned by jobController. Per DEEP-1: phase-launch orchestration
// is a controller concern. See jobController.StartApply.
// Callers should use job.Controller().StartApply(ctx, cfg) or the PhaseController interface.

// SetRunOptions and Run are owned by JobRunner. Per DEEP-1: BatchJob is a pure
// state container — orchestration is owned by JobRunner.
// See JobRunner.SetRunOptions and JobRunner.Run.
// The StandaloneJob interface routes through JobRunner directly; the
// standaloneJobAdapter holds a *JobRunner instead of closing over *BatchJob.

// Wait is owned by jobController. Per DEEP-1: phase lifecycle observation
// is a controller concern. See jobController.Wait.
// Callers should use job.Controller().Wait() or the PhaseController interface.

// excludeFile is an unexported helper for test code that needs the old
// ExcludeFile behavior (mark excluded + auto-cancel when all excluded).
// Per DEEP-1: the public ExcludeFile method was removed from BatchJob —
// it crosses the results/lifecycle boundary and belongs on the JobEditor
// adapter. Test code within the worker package can use this helper.
//
//nolint:unused // used by 12+ test cases in this package
func excludeFile(job *BatchJob, filePath string) {
	job.results.MarkExcluded(filePath)

	job.lifecycle.mu.RLock()
	status := job.lifecycle.Status
	job.lifecycle.mu.RUnlock()

	if job.results.IsAllExcluded() {
		// Only cancel a job that is still in flight (Pending/Running). A job that
		// already reached a terminal success state (Completed/Organized) must not
		// be clobbered by Cancel when its last file is excluded — that was the
		// real bug the isJobTransitioned predicate was previously (mis)used to
		// guard. Check terminal-success explicitly here so the gone-check
		// predicate above can keep its intended Running-is-transitioned shape.
		if status == models.JobStatusPending || status == models.JobStatusRunning {
			job.lifecycle.Cancel()
		}
	}
}
