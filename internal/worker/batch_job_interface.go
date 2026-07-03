package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// ApplyFileContext provides per-file context to PreApply/PostApply hooks.
type ApplyFileContext struct {
	FilePath    string
	Movie       *models.Movie
	MovieResult *MovieResult
	Match       models.FileMatchInfo
	Destination string
}

// ApplyFileResult captures the outcome of applying to a single file.
type ApplyFileResult struct {
	Result *workflow.ApplyResult
	Err    error
}

// BatchJobConfig holds the narrow configuration fields BatchJob actually consumes
// from *config.Config. Instead of passing the full config monolith (200+ fields),
// callers provide only the 4 fields BatchJob reads during scrape and apply phases.
type BatchJobConfig struct {
	MaxWorkers      int           // cfg.Performance.MaxWorkers → pool sizing
	WorkerTimeout   time.Duration // cfg.Performance.WorkerTimeout → per-task timeout
	ScraperPriority []string      // cfg.Scrapers.Priority → selected scrapers
	NFOEnabled      bool          // cfg.Metadata.NFO.Feature.Enabled → NFO generation toggle
}

// batchJobBase holds the 19 shared snapshot fields common to both BatchJobStatus
// Embedded in both types to eliminate field duplication while
// preserving the flat JSON serialization shape (Go promotes embedded struct fields).
type batchJobBase struct {
	ID                    models.JobID                    `json:"id"`
	Status                models.JobStatus                `json:"status"`
	TotalFiles            int                             `json:"total_files"`
	Completed             int                             `json:"completed"`
	Failed                int                             `json:"failed"`
	Excluded              map[string]bool                 `json:"excluded"`
	Files                 []string                        `json:"files"`
	FileMatchInfo         map[string]models.FileMatchInfo `json:"file_match_info,omitempty"`
	Progress              float64                         `json:"progress"`
	Destination           string                          `json:"destination"`
	TempDir               string                          `json:"temp_dir"`
	StartedAt             time.Time                       `json:"started_at"`
	CompletedAt           *time.Time                      `json:"completed_at,omitempty"`
	OrganizedAt           *time.Time                      `json:"organized_at,omitempty"`
	RevertedAt            *time.Time                      `json:"reverted_at,omitempty"`
	OperationModeOverride operationmode.OperationMode     `json:"operation_mode_override,omitempty"`
	Update                bool                            `json:"update"`
	PersistError          string                          `json:"persist_error,omitempty"`
	IsDeleted             bool                            `json:"is_deleted"`
}

// BatchJobStatus is a read-only snapshot of BatchJob state for API consumers.
// Unlike *BatchJob, this type has no mutation methods and no internal pointers
// back to the live job. API handlers should read from this snapshot, not from
// the live *BatchJob struct.
type BatchJobStatus struct {
	batchJobBase
	Results     map[string]*MovieResult    `json:"results"`
	ResultIndex map[string]string          `json:"result_index,omitempty"` // ResultID → FilePath lookup
	Provenance  map[string]*ProvenanceData `json:"provenance,omitempty"`
}

// RescrapeCmd carries everything the rescrape seam needs.
// String-accepting: the seam resolves domain types internally.
// No imports from models, scrape, organizer, nfo, or types.
//
// Infrastructure fields (Fs, TempDir, WF, PosterGen, BatchCfg) are sourced
// from the BatchJob itself, not from the command — the job already holds
// these from its construction path (JobStore.createJob or reconstructBatchJob).
// Per DEEP-6: WF/BatchCfg/PosterGen overrides have been removed from phase
// configs. API handlers must set job.deps.WF (via SetWorkflow) before calling
// phase methods on reconstructed jobs where deps.WF is nil.
type RescrapeCmd struct {
	MovieID           string   // JAV ID to rescrape
	ManualSearchInput string   // Optional manual query or URL
	SelectedScrapers  []string // Optional scraper filter
	Force             bool     // Force refresh

	// FilePath is the pre-resolved file path for the movie being rescraped.
	// when set by the caller, BatchJob.Rescrape uses it directly
	// instead of calling FindFileForMovieID internally. When empty, falls back
	// to FindFileForMovieID for backward compatibility.
	FilePath string

	// Merge controls how the freshly scraped metadata is merged into the
	// existing MovieResult before commit. Preset/ScalarStrategy/
	// ArrayStrategy are resolved at the factory boundary (via
	// workflow.ResolveSeamStrings) before being placed here.
	//
	// MergeEnabled gates whether merging is applied at all. When false (the
	// default for callers that don't supply merge options), CompleteRescrape
	// preserves the historical wholesale-replace behavior so existing
	// rescrape callers are unchanged. When true, the new scraped Movie is
	// merged into the existing one via nfo.MergeMovieMetadataWithOptions
	// before CommitResult — honoring the caller's requested merge policy
	// instead of silently dropping it.
	Merge        workflow.MergeOptions
	MergeEnabled bool
}

// RescrapeResult is everything the caller gets back from the rescrape seam.
// Contains only data the API layer needs for response translation.
type RescrapeResult struct {
	Movie            *models.Movie           // Data struct — acceptable per RESEARCH §Architecture Patterns — Pattern 1 note
	FieldSources     map[string]string       // Per-field scraper attribution
	ActressSources   map[string]string       // Per-actress scraper attribution
	ScraperResults   []*models.ScraperResult // Raw per-scraper results, retained in-memory for the review source viewer
	Status           models.RescrapeStatus   // success, failed, gone, conflict
	Error            string                  // Human-readable error for "failed" status
	OrphanedMovieIDs []string                // IDs that became orphaned during rescrape cleanup
	FilePath         string                  // File path that was rescraped (for provenance propagation)
}

// FileLookupResult holds the output of looking up a movie ID in job results.
type FileLookupResult struct {
	FilePath         string
	OldMovieID       string
	CapturedRevision uint64
}

// ---------------------------------------------------------------------------
// Atomic sub-interfaces
// ---------------------------------------------------------------------------

// JobReader provides read-only access to job state.
// Consumers that only need to observe job status and results should depend
// on this narrow interface rather than the full composite.
type JobReader interface {
	GetID() string
	GetJobStatus() models.JobStatus
	GetStatus() *BatchJobStatus
	GetMovieResult(filePath string) (*MovieResult, error)
	GetResults() []MovieResult
	Subscribe() JobEventSubscriber
}

// MovieLookup provides methods to find movies within a job.
// extracted from the former Rescraper interface — lookup is a
// separate concern from rescrape action.
type MovieLookup interface {
	FindFilePathsForMovieID(movieID string) []string
	FindMovieResultForMovieID(movieID string) (*MovieResult, error)
	GetMovieResultsForMovieID(movieID string) []*MovieResult
	GetFileMatchInfosForMovieID(movieID string) []models.FileMatchInfo

	// GetFileResultByResultID returns the MovieResult and its file path for
	// a given stable ResultID (thread-safe). Returns (result, filePath, found).
	GetFileResultByResultID(resultID string) (*MovieResult, string, bool)

	// GetProvenance returns the provenance (field/actress source attribution
	// plus the in-memory raw ScraperResults) for a file path, or nil. The
	// returned value is a clone and safe for the caller to mutate. Used by the
	// review-page source viewer to read per-scraper raw results.
	GetProvenance(filePath string) *ProvenanceData
}

// JobEditor provides mutation operations on a job's results.
// Consumers that only need to modify job results should depend on this
// narrow interface rather than the full composite.
type JobEditor interface {
	UpdateMovie(ctx context.Context, filePath string, movie *models.Movie) error
	ExcludeFile(filePath string)
	UpdatePosterCrop(movieID string, croppedURL string) error
	UpdatePosterFromURL(ctx context.Context, movieID string, posterURL string, croppedURL string) error

	// ApplyFieldOverride cherry-picks a single field's value from the named
	// scraper source's raw results and applies it to the movie, updating
	// provenance attribution. Mirrors the original Javinizer "Replace" button.
	// Returns the updated MovieResult and ProvenanceData (both clones).
	ApplyFieldOverride(ctx context.Context, resultID, fieldKey, source string) (*MovieResult, *ProvenanceData, error)
}

// PhaseController provides phase execution and dependency-wiring operations
// on a job. Rescrape is grouped with StartScrape/StartApply/Wait
// because rescraping is re-scraping — the same execution-lifecycle concern.
// Per DEEP-1: mutation methods (SetWorkflow, SetBatchCfg, SetJobStatus,
// SetOperationModeOverride, SetPersistError) are on the controller because
// BatchJob is a pure state container — dependency mutation and lifecycle
// transitions are controller concerns, not state-container concerns.
type PhaseController interface {
	// StartScrape begins the scrape phase for the given files.
	// Returns an error if the job cannot start (e.g., missing workflow dependency).
	StartScrape(ctx context.Context, files []string, cfg ScrapePhaseConfig) error

	// StartApply begins the apply (organize) phase.
	// Returns an error if the job cannot start (e.g., missing workflow dependency).
	StartApply(ctx context.Context, cfg ApplyPhaseConfig) error

	// Wait blocks until the job reaches a terminal state and returns any error.
	Wait() error

	// Rescrape re-scrapes a single movie within the job.
	Rescrape(ctx context.Context, cmd RescrapeCmd) (*RescrapeResult, error)

	// SetWorkflow sets the workflow seam on the job's deps.
	// Per DEEP-1: moved from *BatchJob — dependency mutation is a controller concern.
	SetWorkflow(wf workflow.WorkflowInterface)

	// SetBatchCfg sets the batch configuration on the job's deps.
	// Per DEEP-1: moved from *BatchJob — dependency mutation is a controller concern.
	SetBatchCfg(cfg BatchJobConfig)

	// SetJobStatus sets the job status directly.
	// Per DEEP-1: moved from *BatchJob — lifecycle transitions are a controller concern.
	SetJobStatus(status models.JobStatus)

	// SetOperationModeOverride sets the operation mode for the job.
	// Per DEEP-1: moved from *BatchJob — config mutation is a controller concern.
	SetOperationModeOverride(mode operationmode.OperationMode) error

	// SetPersistError sets the persist error message on the job.
	// Per DEEP-1: moved from *BatchJob — mutation is a controller concern.
	SetPersistError(msg string)
}

// JobCanceller provides lifecycle termination operations on a job.
// Consumers that only need to revert or cancel a job should depend on this
// narrow interface rather than the full composite.
type JobCanceller interface {
	Cancel()
	MarkReverted()

	// Done returns a channel that is closed when the job reaches a terminal state
	// (completed, failed, cancelled, organized, or reverted). Callers can select on
	// this to wait for a job to finish after requesting cancellation.
	Done() <-chan struct{}
}

// ---------------------------------------------------------------------------
// Handler-oriented composites
// ---------------------------------------------------------------------------

// EditableJob is the composite interface for movie editing handlers.
// returned by JobStore.GetJobForEdit for movie_edit and exclude handlers.
// movie persistence is routed through UpdateMovie, which persists
// to DB before updating in-memory state — callers no longer call MovieRepo directly.
type EditableJob interface {
	JobReader
	MovieLookup
	JobEditor
}

// ControlledJob is the composite interface for phase execution handlers.
// returned by JobStore.GetJobForControl for rescrape, organize,
// scrape, cancel, and revert handlers.
// Per DEEP-1: PhaseController now includes SetWorkflow/SetBatchCfg/SetJobStatus/
// SetOperationModeOverride/SetPersistError (controller mutation methods that
// were previously on BatchJob).
type ControlledJob interface {
	JobReader
	MovieLookup
	PhaseController
	JobCanceller
}

// BatchJobInterface is the unified lifecycle interface for batch jobs.
// It composes all narrow sub-interfaces (JobReader, MovieLookup, PhaseController,
// JobCanceller, JobEditor) into a single seam that batch handlers can depend on.
//
// Per DEEP-1: API batch handlers depend on this interface instead of juggling
// separate ControlledJob and EditableJob composites or reaching through *BatchJob
// directly. The interface encapsulates the full batch job lifecycle: status queries,
// phase execution, movie editing, rescrape, and cancellation.
// PhaseController now includes the mutation methods that were previously on
// BatchJob (SetWorkflow, SetBatchCfg, SetJobStatus, etc.).
//
// JobStore.CreateJob returns this interface, and JobStore.GetBatchJob retrieves
// an existing job as this interface. Handlers that only need a narrow view (e.g.,
// read-only status, edit-only access) should use the appropriate sub-composite
// (ControlledJob, EditableJob) via GetJobForControl/GetJobForEdit instead.
type BatchJobInterface interface {
	JobReader
	MovieLookup
	PhaseController
	JobCanceller
	JobEditor
}

// newStandaloneJobFromBatchJob creates a StandaloneJob from a *BatchJob.
// This is a package-internal helper for tests and the factory that need to
// construct a StandaloneJob from a concrete *BatchJob.
func newStandaloneJobFromBatchJob(job *BatchJob) StandaloneJob {
	a := buildAdapters(job)
	batchCfg := job.controller.resolveBatchCfg()

	controlledJob := &controlledJobAdapter{
		JobReader:       a.reader,
		MovieLookup:     a.movieLookup,
		PhaseController: a.phaseController,
		JobCanceller:    a.canceller,
	}
	runner := NewJobRunner(controlledJob, batchCfg)

	return &standaloneJobAdapter{
		ControlledJob:      controlledJob,
		runner:             runner,
		keepOpenFn:         job.SetKeepOpen,
		closeBroadcasterFn: job.CloseEventBroadcaster,
	}
}

// StandaloneJob is the composite interface for CLI/TUI usage where no
// JobStore persistence is needed. It extends ControlledJob with the
// CLI-specific methods SetRunOptions and Run.
// Per DEEP-2: callers of CreateStandaloneJob use this interface instead of
// *BatchJob directly, eliminating the need for passthrough methods on BatchJob.
// Per DEEP-1: Run/SetRunOptions are on JobRunner, not on BatchJob.
type StandaloneJob interface {
	ControlledJob

	// SetRunOptions configures the scrape and apply phase options for Run().
	SetRunOptions(scrapeCfg ScrapePhaseConfig, applyCfg ApplyPhaseConfig)

	// Run executes the configured scrape and apply phases.
	Run(ctx context.Context) error
}

// ---------------------------------------------------------------------------
// Adapter structs
// ---------------------------------------------------------------------------

// jobReaderImpl satisfies JobReader by composing closures and extracted types.
// No single extracted type satisfies JobReader — GetID reads from BatchJob,
// GetStatus requires a 3-lock snapshot, and Subscribe reads from eventBroadcaster.
// this struct does NOT embed *BatchJob.
type jobReaderImpl struct {
	id          string
	lifecycle   *JobLifecycle
	results     ResultMapAccessor
	snapshotFn  func() *BatchJobStatus    // closure from BatchJob
	subscribeFn func() JobEventSubscriber // closure from BatchJob
	resultsFn   func() []MovieResult      // closure from ResultTracker.GetResults
}

func (jr *jobReaderImpl) GetID() string                  { return jr.id }
func (jr *jobReaderImpl) GetJobStatus() models.JobStatus { return jr.lifecycle.GetJobStatus() }
func (jr *jobReaderImpl) GetStatus() *BatchJobStatus     { return jr.snapshotFn() }
func (jr *jobReaderImpl) GetMovieResult(filePath string) (*MovieResult, error) {
	return jr.results.GetMovieResult(filePath)
}
func (jr *jobReaderImpl) GetResults() []MovieResult     { return jr.resultsFn() }
func (jr *jobReaderImpl) Subscribe() JobEventSubscriber { return jr.subscribeFn() }

// jobEditorImpl satisfies JobEditor by composing ResultUpdater,
// ResultMapAccessor, JobLifecycle, and PosterEditor.
// ExcludeFile crosses the results/lifecycle boundary — it cannot delegate to any
// single embedded type.
// Per DEEP-5: resultExcluder merged into ResultUpdater (MarkExcluded exported),
// exclusionChecker merged into ResultMapAccessor (IsAllExcluded exported).
// poster DB persistence is handled by PosterEditor, not by this adapter.
type jobEditorImpl struct {
	updater      ResultUpdater
	accessor     ResultMapAccessor
	tracker      *ResultTracker
	lifecycle    *JobLifecycle
	posterEditor *PosterEditor
	movieRepo    database.MovieRepositoryInterface
	actressRepo  database.ActressRepositoryInterface
	overrideMu   sync.Map // resultID -> *sync.Mutex
}

func (je *jobEditorImpl) UpdateMovie(ctx context.Context, filePath string, movie *models.Movie) error {
	// Preserve the original cover snapshot from the existing in-memory movie
	// before persisting, so the cover/fanart reset survives server restarts
	// and the DB/in-memory states stay in sync. Read-only pass: does not mutate
	// the in-memory result, only populates movie.Poster.OriginalCoverURL.
	_ = je.updater.AtomicUpdateFileResult(filePath, func(current *MovieResult) (*MovieResult, error) {
		backupCoverOriginal(current.Movie, movie)
		return current, nil
	})

	// Apply explicit actress name edits before the movie upsert. The shared
	// MovieUpserter only fills missing actress fields, which would discard a
	// review-page name edit; renaming the record by ID here overwrites it, and
	// doing so before Upsert makes Upsert's name-based lookup find the renamed
	// record so the in-memory clone (and NFO generation) carries the edit.
	// Gated on movieRepo so the in-memory-only edit path (no DB persistence)
	// never mutates the database.
	if je.actressRepo != nil && je.movieRepo != nil {
		for i := range movie.Actresses {
			a := &movie.Actresses[i]
			if a.ID == 0 {
				continue
			}
			existing, err := je.actressRepo.FindByID(ctx, a.ID)
			if err != nil {
				if database.IsNotFound(err) {
					continue
				}
				return fmt.Errorf("load actress for rename: %w", err)
			}
			if existing.FirstName == a.FirstName && existing.LastName == a.LastName && existing.JapaneseName == a.JapaneseName {
				continue
			}
			if err := je.actressRepo.RenameNameFields(ctx, a.ID, a.FirstName, a.LastName, a.JapaneseName); err != nil {
				return fmt.Errorf("persist actress name edit: %w", err)
			}
		}
	}

	// persist to DB first, then update in-memory. If DB persist
	// fails, the in-memory state is not updated — no divergence. If DB persist
	// succeeds but in-memory update fails, the job's state is stale but the
	// DB is authoritative.
	if je.movieRepo != nil {
		if _, err := je.movieRepo.Upsert(ctx, movie); err != nil {
			return fmt.Errorf("persist movie update: %w", err)
		}
	}
	return je.updater.UpdateMovie(filePath, movie)
}

// ExcludeFile marks a file as excluded from the job and, if all files are excluded,
// cancels the job. Cancel() is safe to call even if the job has already transitioned
// to a terminal state (Completed, Cancelled, Failed), because cancelAndMarkCancelled
// has a cancelled bool guard that makes it a no-op on repeated or post-terminal calls.
func (je *jobEditorImpl) ExcludeFile(filePath string) {
	je.updater.MarkExcluded(filePath)

	je.lifecycle.mu.RLock()
	status := je.lifecycle.Status
	je.lifecycle.mu.RUnlock()

	// Only cancel a job still in flight (Pending/Running). A job that already
	// reached a terminal success state (Completed/Organized) must not be
	// clobbered by Cancel when its last file is excluded. This mirrors the
	// explicit Pending/Running guard in BatchJob.ExcludeFile (batch_job.go) —
	// do NOT reuse isJobTransitioned here, whose gone-check semantics exclude
	// Organized and would let an Organized job be cancelled.
	if je.accessor.IsAllExcluded() &&
		(status == models.JobStatusPending || status == models.JobStatusRunning) {
		je.lifecycle.Cancel()
		return
	}
}

func (je *jobEditorImpl) UpdatePosterCrop(movieID string, croppedURL string) error {
	return je.posterEditor.UpdatePosterCrop(movieID, croppedURL)
}

func (je *jobEditorImpl) UpdatePosterFromURL(ctx context.Context, movieID string, posterURL string, croppedURL string) error {
	// Delegates entirely to PosterEditor, which handles both in-memory update
	// and DB persistence when movieRepo is configured.
	return je.posterEditor.UpdatePosterFromURL(ctx, movieID, posterURL, croppedURL)
}

// ApplyFieldOverride cherry-picks a single field's value from the named
// scraper source's raw results and applies it to the movie, updating
// provenance attribution to reflect the user's choice. Mirrors the original
// PowerShell Javinizer "Replace" button (javinizergui.ps1:2538):
//
//	$cache:findData[$cache:index].Data.($prop.Name) = $prop.Value
//	$cache:findData[$cache:index].Selected.($prop.Name) = $source
//
// The movie is persisted via UpdateMovie (DB upsert + in-memory), consistent
// with the poster-from-url / poster-crop edit endpoints. Provenance
// (FieldSources/ActressSources/ScraperResults) is persisted via the job
// envelope — the handler calls PersistJobByID after this method succeeds.
// Raw ScraperResults round-trip through the envelope (json:"scraper_results").
// A per-resultID mutex serializes concurrent overrides on the same result so
// the read-clone-mutate-write sequence cannot lose an earlier override.
func (je *jobEditorImpl) ApplyFieldOverride(ctx context.Context, resultID, fieldKey, source string) (*MovieResult, *ProvenanceData, error) {
	mu, _ := je.overrideMu.LoadOrStore(resultID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	result, filePath, found := je.tracker.GetFileResultByResultID(resultID)
	if !found || result == nil || result.Movie == nil {
		return nil, nil, fmt.Errorf("result %s not found or has no movie", resultID)
	}
	prov := je.tracker.GetProvenance(filePath)
	if prov == nil {
		prov = &ProvenanceData{}
	}
	movie := result.Movie.Clone()
	if err := applyFieldOverride(movie, prov, fieldKey, source); err != nil {
		return nil, nil, err
	}
	if err := je.UpdateMovie(ctx, filePath, movie); err != nil {
		return nil, nil, fmt.Errorf("persist field override: %w", err)
	}
	je.updater.SetProvenance(filePath, prov)
	updated, _, _ := je.tracker.GetFileResultByResultID(resultID)
	updatedProv := je.tracker.GetProvenance(filePath)
	return updated, updatedProv, nil
}

// editableJobAdapter satisfies EditableJob by composing jobReaderImpl,
// ResultTracker, and jobEditorImpl. genuinely decomposed —
// no *BatchJob embedding.
type editableJobAdapter struct {
	JobReader
	MovieLookup
	JobEditor
}

// phaseControllerImpl satisfies PhaseController using closures from BatchJob.
// replaces *BatchJob embedding in controlledJobAdapter,
// eliminating the direct dependency on *BatchJob for the control path.
type phaseControllerImpl struct {
	startScrape      func(ctx context.Context, files []string, cfg ScrapePhaseConfig) error
	startApply       func(ctx context.Context, cfg ApplyPhaseConfig) error
	wait             func() error
	rescrape         func(ctx context.Context, cmd RescrapeCmd) (*RescrapeResult, error)
	setWorkflow      func(wf workflow.WorkflowInterface)
	setBatchCfg      func(cfg BatchJobConfig)
	setJobStatus     func(status models.JobStatus)
	setOperationMode func(mode operationmode.OperationMode) error
	setPersistError  func(msg string)
}

func (pc *phaseControllerImpl) StartScrape(ctx context.Context, files []string, cfg ScrapePhaseConfig) error {
	return pc.startScrape(ctx, files, cfg)
}
func (pc *phaseControllerImpl) StartApply(ctx context.Context, cfg ApplyPhaseConfig) error {
	return pc.startApply(ctx, cfg)
}
func (pc *phaseControllerImpl) Wait() error { return pc.wait() }
func (pc *phaseControllerImpl) Rescrape(ctx context.Context, cmd RescrapeCmd) (*RescrapeResult, error) {
	return pc.rescrape(ctx, cmd)
}
func (pc *phaseControllerImpl) SetWorkflow(wf workflow.WorkflowInterface) { pc.setWorkflow(wf) }
func (pc *phaseControllerImpl) SetBatchCfg(cfg BatchJobConfig)            { pc.setBatchCfg(cfg) }
func (pc *phaseControllerImpl) SetJobStatus(status models.JobStatus)      { pc.setJobStatus(status) }
func (pc *phaseControllerImpl) SetOperationModeOverride(mode operationmode.OperationMode) error {
	return pc.setOperationMode(mode)
}
func (pc *phaseControllerImpl) SetPersistError(msg string) { pc.setPersistError(msg) }

// controlledJobAdapter satisfies ControlledJob by composing jobReaderImpl,
// ResultTracker, phaseControllerImpl, and JobLifecycle.
// fully decomposed — no *BatchJob embedding.
// Per DEEP-1: PhaseController now includes SetWorkflow/SetBatchCfg/SetJobStatus/etc.
type controlledJobAdapter struct {
	JobReader
	MovieLookup
	PhaseController
	JobCanceller
}

// batchJobAdapter satisfies BatchJobInterface by composing all five narrow
// sub-interfaces. Per DEEP-1: this is the unified adapter
// returned by JobStore.CreateJob and JobStore.GetBatchJob, giving API handlers a
// single seam for the full batch job lifecycle.
// Per DEEP-1: PhaseController now includes mutation methods (SetWorkflow,
// SetBatchCfg, SetJobStatus, etc.) that were previously on BatchJob.
type batchJobAdapter struct {
	JobReader
	MovieLookup
	PhaseController
	JobCanceller
	JobEditor
}

// standaloneJobAdapter satisfies StandaloneJob by composing ControlledJob
// with a *JobRunner for CLI-specific methods (SetRunOptions, Run).
// Per DEEP-1: holds a *JobRunner directly instead of closing over *BatchJob methods.
// Run/SetRunOptions orchestration belongs on JobRunner, not on the state container.
// The adapter also manages the event broadcaster lifecycle (SetKeepOpen before
// Run, CloseEventBroadcaster after).
// Per N-7: validateWFFn removed — jobController.StartScrape/StartApply already
// validate resolveWF(), making the pre-Run validation redundant on the happy path.
type standaloneJobAdapter struct {
	ControlledJob
	runner             *JobRunner
	keepOpenFn         func(bool) // SetKeepOpen on the underlying BatchJob's event source
	closeBroadcasterFn func()     // CloseEventBroadcaster on the underlying BatchJob's event source
}

func (s *standaloneJobAdapter) SetRunOptions(scrapeCfg ScrapePhaseConfig, applyCfg ApplyPhaseConfig) {
	s.runner.SetRunOptions(scrapeCfg, applyCfg)
}

func (s *standaloneJobAdapter) Run(ctx context.Context) error {
	// Per N-7: validateWFFn removed — jobController.StartScrape/StartApply already
	// validate resolveWF() and return an appropriate error. No need to duplicate
	// the check here on the happy path.
	if s.keepOpenFn != nil {
		s.keepOpenFn(true)
	}
	err := s.runner.Run(ctx)
	if s.closeBroadcasterFn != nil {
		s.closeBroadcasterFn()
	}
	return err
}

// Compile-time assertions for adapters and extracted types.
var (
	_ JobReader         = (*jobReaderImpl)(nil)
	_ JobEditor         = (*jobEditorImpl)(nil)
	_ EditableJob       = (*editableJobAdapter)(nil)
	_ ControlledJob     = (*controlledJobAdapter)(nil)
	_ BatchJobInterface = (*batchJobAdapter)(nil)
	_ StandaloneJob     = (*standaloneJobAdapter)(nil)
	_ MovieLookup       = (*ResultTracker)(nil)
	_ JobCanceller      = (*JobLifecycle)(nil)
)

// ---------------------------------------------------------------------------
// Phase configuration types
// ---------------------------------------------------------------------------

// ScrapePhaseConfig carries only what the scrape phase needs.
// No apply fields — callers constructing a scrape cannot accidentally set apply options.
// Per DEEP-6: WF and BatchCfg overrides removed. These are resolved at the
// factory/job level instead of per-call phase config overrides.
type ScrapePhaseConfig struct {
	// Per-scrape configuration
	SelectedScrapers []string          // Restrict scraping to these scrapers (empty = all)
	Strict           bool              // Strict mode: fail if no results from any scraper
	Force            bool              // Force refresh: bypass cache and re-scrape
	MovieIDOverride  map[string]string // Override movie ID per file path (rescrape use case)
	RawInputOverride map[string]string // Per-file manual input (ID or URL) keyed by file path; takes precedence over the matcher and MovieIDOverride — resolveScrapeInput parses it into MovieID + PriorityOverride
	PriorityOverride []string          // Reorder scraper priority instead of restricting

	// Job-level config applied before scrape starts
	FileMatchInfo map[string]models.FileMatchInfo // Match metadata per file

	// OnFileScraped is invoked after each file is successfully scraped,
	// carrying the source file path and a short status message. The API layer
	// wires this to broadcast a per-file WebSocket ProgressMessage with FilePath
	// set so the frontend's messagesByFile populates and ProgressModal shows
	// live per-file scrape status. Mirrors main's realtime.ProgressAdapter
	// which forwarded per-task scrape updates to the WS hub. Called concurrently
	// from worker goroutines. Nil = no per-file success reporting.
	OnFileScraped func(filePath, message string)

	// OnFileScrapeFailed is invoked after each file's scrape fails, carrying the
	// source file path and the error message. The API layer wires this to
	// broadcast a per-file WebSocket ProgressMessage with FilePath + Error set.
	// Mirrors main's realtime.ProgressAdapter failure forwarding. Called
	// concurrently from worker goroutines. Nil = no per-file failure reporting.
	OnFileScrapeFailed func(filePath, errMsg string)

	// OnScrapeStepProgress is invoked for each in-flight scrape step update
	// (e.g. "Querying scrapers...", "Aggregating metadata..."), carrying the
	// source file path, step name, progress percentage, and message. The API
	// layer wires this to broadcast an incremental WebSocket ProgressMessage
	// with FilePath set so the frontend's messagesByFile updates live per step
	// and ProgressModal active rows show step text during scraping. Mirrors
	// main's realtime.ProgressAdapter which forwarded every step update to the
	// WS hub. Called concurrently from worker goroutines. Nil = no incremental
	// step-progress reporting (only terminal per-file success/error).
	OnScrapeStepProgress func(filePath, step string, pct float64, msg string)
}

// ApplyPhaseConfig carries only what the apply phase needs.
// Directly maps to workflow.ApplyCmd fields — no drift risk.
// Per DEEP-6: WF and BatchCfg overrides removed. These are resolved at the
// factory/job level instead of per-call phase config overrides.
type ApplyPhaseConfig struct {
	// Per-apply configuration (maps directly to ApplyCmd fields)
	OrganizeOptions     workflow.OrganizeOptions // File organization settings
	MergeOptions        workflow.MergeOptions    // NFO merge strategy settings
	Destination         string                   // Target directory for organized files
	GenerateNFO         bool                     // Generate NFO file for each movie
	Download            bool                     // Download media (poster, fanart, etc.)
	DownloadExtrafanart *bool                    // Optional override for extrafanart downloads; nil = use config default
	DryRun              bool                     // Dry-run mode: preview without making changes

	// Job-level config applied before apply starts
	OperationModeOverride operationmode.OperationMode // resolved at factory boundary
	Update                *bool                       // Update mode (in-place, no file organization); nil = don't change, true/false = set explicitly
	TempDir               string                      // Temp directory for poster paths (from job infrastructure)

	// Hooks (apply-phase only)
	PreApplyFunc  func(ctx context.Context, afc *ApplyFileContext) error
	PostApplyFunc func(ctx context.Context, afc *ApplyFileContext, afr *ApplyFileResult)

	// OnPhaseComplete is invoked once at the end of the apply phase with the
	// total organized / failed file counts, before MarkOrganized / MarkCompleted.
	// The API layer wires this to broadcast the {status:"organization_completed"|
	// "update_completed", progress:100} WebSocket progress message so frontend
	// clients (e.g. organize-controller's handleWebSocketMessage) can finalize
	// the organize/update flow in real time, mirroring main's process_organize.go
	// which called broadcastProgress inline at the end of organize.
	OnPhaseComplete func(organized, failed int)

	// OnFileProgress is invoked after each file's apply completes (success or
	// failure) with the running count of processed files and the total file
	// count. The API layer wires this to broadcast an incremental WebSocket
	// ProgressMessage (0-100) so the frontend progress bar advances per file
	// instead of jumping straight from 0 to 100 on the terminal
	// organization_completed message. Without it, the only WS progress message
	// the frontend receives during organize is the final 100% broadcast, so the
	// bar sits at 0% for the entire run and snaps to 100% at the end. Called
	// concurrently from worker goroutines; the broadcaster must be goroutine-
	// safe (the WS hub's Broadcast is). Nil = no per-file progress reporting.
	OnFileProgress func(processed, total int)

	// OnFileOrganizeStart is invoked at the TOP of applyFile, BEFORE any work
	// begins on the file, carrying the source file path. The API layer wires this
	// to broadcast a per-file WebSocket ProgressMessage with Status "pending",
	// Progress 0, and an "Organizing <basename>" message, so the Home "Current
	// Activity" card and OrganizeStatusCard show which file is currently being
	// organized (verbose organize progress) instead of only the aggregate
	// "Organized N of M files" count.
	//
	// Double-count safety (the certified pattern scrape already uses): the
	// non-terminal pending message (Progress 0) enters the frontend's
	// messagesByFile and counts in computeJobProgress's activeProgress
	// (contributing 0), keeping the bar = finished/total (monotonic). When the
	// file completes, the terminal OnFileOrganized/OnFileFailed message
	// (Progress:100, status organized/updated/failed) OVERWRITES it in
	// messagesByFile (dedup-latest by file_path). Emitters MUST keep Progress <
	// 100 (never 100) so the in-flight row stays non-terminal. Called
	// concurrently from worker goroutines; the broadcaster must be goroutine-
	// safe. Nil = no per-file start reporting.
	OnFileOrganizeStart func(filePath string)

	// OnFileOrganized is invoked after each file is successfully organized/updated,
	// carrying the source file path. The API layer wires this to broadcast a
	// per-file WebSocket ProgressMessage with Status "organized"/"updated" and
	// FilePath set, so the frontend's fileStatuses map populates per file and
	// OrganizeStatusCard can render live per-file rows. Mirrors main's
	// process_organize.go which sent per-file success over WS. Called concurrently
	// from worker goroutines. Nil = no per-file success reporting.
	OnFileOrganized func(filePath string)

	// OnFileFailed is invoked after each file's apply fails, carrying the source
	// file path and the error message. The API layer wires this to broadcast a
	// per-file WebSocket ProgressMessage with Status "failed", FilePath set, and
	// Error populated, so the frontend's fileStatuses map records the failure and
	// OrganizeStatusCard can offer a "Retry Failed" path. Mirrors main's
	// process_organize.go which sent per-file failure over WS. Called concurrently
	// from worker goroutines. Nil = no per-file failure reporting.
	OnFileFailed func(filePath, errMsg string)
}
