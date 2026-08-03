package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// ApplyFileContext provides per-file context to PreApply/PostApply hooks.
type ApplyFileContext struct {
	FilePath    string
	Movie       *models.Movie
	MovieResult *resultstore.MovieResult
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
	RequestTimeout  time.Duration // cfg.Scrapers.RequestTimeoutSeconds → overall scrape operation timeout
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
	Results     map[string]*resultstore.MovieResult    `json:"results"`
	ResultIndex map[string]string                      `json:"result_index,omitempty"` // ResultID → FilePath lookup
	Provenance  map[string]*resultstore.ProvenanceData `json:"provenance,omitempty"`
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
	// PersistErr is non-nil when the in-critical-section envelope persist
	// failed (rescrapePhaseInputs.PersistEnvelope fired inside the phase's
	// poster-source locks). The phase already rolled everything back — the
	// in-memory MovieResult + provenance (P2-4/F1) and the replaced poster
	// caches, including a rekeyed origin's cleaned-up assets (F2) — before
	// releasing the locks, so memory, cache, and the unpersisted envelope
	// all converged back to the pre-rescrape state before any other
	// poster-state writer could interleave. Callers must surface this
	// instead of acking success a restart would contradict. A degraded
	// rollback (a pre-generation asset snapshot failed) is noted on the
	// error text.
	PersistErr error
}

// ErrEnvelopePersist marks failures of the in-critical-section job-envelope
// persist (field-override editor, rescrape phase) so API handlers can map
// them to 5xx instead of treating them as request-shaped 4xx errors.
var ErrEnvelopePersist = errors.New("job envelope persist failed")

// ---------------------------------------------------------------------------
// Atomic sub-interfaces
// ---------------------------------------------------------------------------

// JobReader provides read-only access to job state.
// Consumers that only need to observe job status and results should depend
// on this narrow interface rather than the full composite.
// Per the result-store extraction: GetMovieResult/GetResults return
// resultstore.* types (MovieLookup moved to the resultstore package).
type JobReader interface {
	GetID() string
	GetJobStatus() models.JobStatus
	GetStatus() *BatchJobStatus
	GetMovieResult(filePath string) (*resultstore.MovieResult, error)
	GetResults() []resultstore.MovieResult
	Subscribe() JobEventSubscriber
}

// JobEditor provides mutation operations on a job's results.
// Consumers that only need to modify job results should depend on this
// narrow interface rather than the full composite.
type JobEditor interface {
	UpdateMovie(ctx context.Context, filePath string, movie *models.Movie) error
	// RestoreMovieResult reverts one file's result to a pre-edit snapshot
	// taken via GetMovieResult. Unlike UpdateMovie it can restore a result
	// whose stored result legitimately had a nil Movie (UpdateMovie
	// dereferences movie and upserts by movie.ID, so it cannot express
	// "no movie"). Compensation must distinguish a FAILED snapshot lookup
	// (nil prior: nothing to restore to — surface it) from a PRESENT
	// snapshot holding Movie=nil (restore verbatim here); conflating them
	// strands rejected multipart edits.
	RestoreMovieResult(ctx context.Context, filePath string, prior *resultstore.MovieResult) error
	ExcludeFile(filePath string)
	UpdatePosterCrop(movieID string, croppedURL string, bounds *models.CropBounds) error
	UpdatePosterFromURL(ctx context.Context, movieID string, posterURL string, croppedURL string) error

	// ApplyFieldOverride cherry-picks a single field's value from the named
	// scraper source's raw results and applies it to the movie, updating
	// provenance attribution. Mirrors the original Javinizer "Replace" button.
	// Returns the updated MovieResult and ProvenanceData (both clones).
	// The whole sequence — re-keyed fan-out persists, provenance fan-out,
	// any poster-asset migration/refresh, and the job-envelope persist —
	// runs atomically under the poster-source lock the editor already holds:
	// a failed envelope persist is compensated (parts reverted, provenance
	// restored, cache rolled back, assets moved back) BEFORE the lock
	// releases, and the error wraps ErrEnvelopePersist so the caller can map
	// it to 5xx. A restart can therefore never resurrect pre-override state
	// against a cache holding refreshed assets.
	ApplyFieldOverride(ctx context.Context, resultID, fieldKey, source string) (*resultstore.MovieResult, *resultstore.ProvenanceData, error)
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
	resultstore.MovieLookup
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
	resultstore.MovieLookup
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
	resultstore.MovieLookup
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
	results     resultstore.Store
	snapshotFn  func() *BatchJobStatus           // closure from BatchJob
	subscribeFn func() JobEventSubscriber        // closure from BatchJob
	resultsFn   func() []resultstore.MovieResult // closure from ResultTracker.GetResults
}

func (jr *jobReaderImpl) GetID() string                  { return jr.id }
func (jr *jobReaderImpl) GetJobStatus() models.JobStatus { return jr.lifecycle.GetJobStatus() }
func (jr *jobReaderImpl) GetStatus() *BatchJobStatus     { return jr.snapshotFn() }
func (jr *jobReaderImpl) GetMovieResult(filePath string) (*resultstore.MovieResult, error) {
	return jr.results.GetMovieResult(filePath)
}
func (jr *jobReaderImpl) GetResults() []resultstore.MovieResult { return jr.resultsFn() }
func (jr *jobReaderImpl) Subscribe() JobEventSubscriber         { return jr.subscribeFn() }

// jobEditorImpl satisfies JobEditor by composing a resultstore.Store,
// JobLifecycle, and PosterEditor. ExcludeFile crosses the results/lifecycle
// boundary — it cannot delegate to any single embedded type.
// Per the result-store extraction: the former updater/accessor/tracker fields
// are consolidated into a single resultstore.Store (Store covers ResultUpdater,
// ResultMapAccessor, and MovieLookup).
// poster DB persistence is handled by PosterEditor, not by this adapter.
type jobEditorImpl struct {
	store        resultstore.Store
	lifecycle    *JobLifecycle
	posterEditor *PosterEditor
	// posterGen + jobID regenerate the job's temp full-size poster
	// ({tempDir}/posters/{jobID}/{movie.ID}-full.jpg) when a poster_url or
	// cover_url override changes the effective poster source image — see
	// refreshOverriddenPosterSource.
	// posterGen may be nil for editors without poster infrastructure.
	posterGen poster.PosterGenerator
	jobID     string
	// planOverrideFn, when non-nil, replaces planMultipartOverride as the
	// multipart fan-out planner. Test-only seam (nil in production): no
	// stored-state combination makes an "id"-rekey merge fail naturally —
	// every part's merge re-applies the override against the SAME provenance
	// clone that already satisfied the selected part — so a deterministic
	// plan-failure-after-asset-move test injects it here.
	planOverrideFn func(filePaths []string, movie *models.Movie, prov *resultstore.ProvenanceData, fieldKey, source string) ([]overridePartWrite, error)
	movieRepo      database.MovieRepositoryInterface
	actressRepo    database.ActressRepositoryInterface
	overrideMu     sync.Map // resultID -> *sync.Mutex
	// persistEnvelope persists the whole job envelope with an error return
	// (wired from BatchJobDeps.PersistErrFn). ApplyFieldOverride invokes it
	// INSIDE the poster-source-lock critical section, after the fan-out
	// persists and the provenance fan-out, so a failure is compensated —
	// parts reverted, provenance restored, poster cache rolled back,
	// re-keyed assets moved back — before the lock releases: there is NO
	// release→re-acquire gap a crop or source edit could land in and be
	// silently erased by the compensation (the old handler-side F3 window).
	// The persist and its compensation run under the per-job envelope lock
	// (AcquireJobEnvelopeLock, acquired after the poster-source locks —
	// poster → envelope ordering) so concurrent envelope writers on other
	// movies of this job cannot durably capture the fan-out before it is
	// durable itself.
	// Nil for editors without envelope persistence (standalone jobs, tests):
	// the in-section persist is skipped, matching the pre-hoist non-API flow.
	//
	// Lock ordering: the SQLite write runs while the poster-source lock is
	// held; its internal locks (result-store snapshots, job mutex, repo
	// locks) are leaf-level — no path acquires a poster-source lock while
	// holding them — so no cycle.
	persistEnvelope func() error
}

func (je *jobEditorImpl) UpdateMovie(ctx context.Context, filePath string, movie *models.Movie) error {
	// Preserve the original cover snapshot from the existing in-memory movie
	// before persisting, so the cover/fanart reset survives server restarts
	// and the DB/in-memory states stay in sync. Read-only pass: does not mutate
	// the in-memory result, only populates movie.Poster.OriginalCoverURL.
	_ = je.store.AtomicUpdateFileResult(filePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
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
	return je.store.UpdateMovie(filePath, movie)
}

// ExcludeFile marks a file as excluded from the job and, if all files are excluded,
// cancels the job. Cancel() is safe to call even if the job has already transitioned
// to a terminal state (Completed, Cancelled, Failed), because cancelAndMarkCancelled
// has a cancelled bool guard that makes it a no-op on repeated or post-terminal calls.
func (je *jobEditorImpl) ExcludeFile(filePath string) {
	je.store.MarkExcluded(filePath)

	je.lifecycle.mu.RLock()
	status := je.lifecycle.Status
	je.lifecycle.mu.RUnlock()

	// Only cancel a job still in flight (Pending/Running). A job that already
	// reached a terminal success state (Completed/Organized) must not be
	// clobbered by Cancel when its last file is excluded. This mirrors the
	// explicit Pending/Running guard in BatchJob.ExcludeFile (batch_job.go) —
	// do NOT reuse isJobTransitioned here, whose gone-check semantics exclude
	// Organized and would let an Organized job be cancelled.
	if je.store.IsAllExcluded() &&
		(status == models.JobStatusPending || status == models.JobStatusRunning) {
		je.lifecycle.Cancel()
		return
	}
}

func (je *jobEditorImpl) UpdatePosterCrop(movieID string, croppedURL string, bounds *models.CropBounds) error {
	return je.posterEditor.UpdatePosterCrop(movieID, croppedURL, bounds)
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
// the read-clone-mutate-write sequence cannot lose an earlier override, and a
// shared per-(jobID, movieID) lock (AcquirePosterSourceLock) additionally
// serializes EVERY override's whole-movie persist against the manual-crop,
// poster-from-URL, and whole-movie PATCH paths: every field override ends in
// an UpdateMovie of a whole-movie clone, so even a title/maker override can
// otherwise interleave with a manual crop, cloning the movie before the crop
// persists its new bounds and then persisting the stale clone — silently
// erasing the successful crop. The lock key is re-resolved from the result
// under the lock — a rescrape may have re-keyed the result to a new movie ID
// while this call waited — and on a change the old key's lock is released
// BEFORE the destination's is acquired, so this path never holds two poster
// locks at once (see the re-resolution loop below).
//
// A poster_url override — or a cover_url override when the movie has no
// PosterURL (the downloader falls back to CoverURL as the poster source) —
// regenerates the temp full-size poster before persisting
// (refreshOverriddenPosterSource) so a subsequent manual crop measures the
// newly selected image, not the stale pre-override -full.jpg. Clearing the
// last source removes the cache instead of regenerating it. As with the
// poster-from-url endpoint, a failed regeneration rejects the override rather
// than persisting a source URL the crop endpoint cannot match to the on-disk
// image, and when persistence itself fails after a successful regeneration,
// the cached -full.jpg/preview assets are rolled back so filesystem and job
// state never diverge. An override that clears the LAST poster source instead
// succeeds as a cleanup: the cached assets are removed and the persisted
// preview URL cleared, with the same snapshot rollback covering a persistence
// failure afterwards.
//
// For a multipart movie the overridden movie is persisted to EVERY file part
// returned by FindFilePathsForMovieID, mirroring updateBatchMovie's multipart
// loop (internal/api/batch/movie_edit.go, compensation since c82b2677): the
// poster refresh above replaces the movie-wide {movie.ID}-full.jpg that ALL
// parts share, so persisting the new source only to the selected part would
// leave a sibling holding the old source while sharing the refreshed crop
// image — cropping that sibling would then measure the new image, fan its
// bounds out to every part, and Organize would apply them to the sibling's
// stale source. Each sibling instead receives its OWN stored movie with
// only the overridden field merged in (mergeOverrideOntoPart) plus the
// selected part's poster state (source URLs, cleared CropBounds, synced
// ShouldCropPoster intent, refreshed CroppedPosterURL) — per-part,
// FileMatchInfo-derived identity fields such as OriginalFileName survive
// the fan-out instead of being stamped with the selected part's values.
// Provenance fans out with the movie so every part's review tooltip
// attributes the overridden field to the chosen source. There is no store-level transaction across
// parts, so a later part's persist failure compensates the earlier parts
// (re-persisted with their pre-override movies) BEFORE the poster-cache
// rollback runs — restoring the cache while a part still holds the new
// source would desync job state from the -full.jpg all over again.
//
// An "id" override RE-KEYS the movie (Movie.ID is set from the selected
// source's raw result): the destination key's poster-source lock joins the
// critical section in lexical pair order (the second two-lock exception,
// alongside the rescrape A→B pair — see AcquirePosterSourceLock's doc), and
// the cached poster assets are migrated from the old key to the new one
// (P3-6) so the cache is not orphaned at the old key while every
// crop/preview lookup resolves the new one; the persisted preview URL is
// re-pointed as well.
//
// The job-envelope persist is part of THIS critical section (P1-2): it
// runs under the same held lock via jobEditorImpl.persistEnvelope, and a
// failure is compensated in place — parts revert to their pre-override
// movies, the pre-override provenance is restored, the poster cache rolls
// back, and re-keyed assets move back — a restart must never resurrect
// pre-override job state against a refreshed cache, and no other
// poster-state writer can interleave with the revert. The fan-out onward is
// additionally serialized per JOB under the envelope lock
// (AcquireJobEnvelopeLock, poster → envelope ordering), so a concurrent edit
// on a different movie of the same job cannot durably persist this
// override's committed-but-not-yet-durable state.
func (je *jobEditorImpl) ApplyFieldOverride(ctx context.Context, resultID, fieldKey, source string) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
	mu, _ := je.overrideMu.LoadOrStore(resultID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	result, _, found := je.store.GetFileResultByResultID(resultID)
	if !found || result == nil || result.Movie == nil {
		return nil, nil, fmt.Errorf("result %s not found or has no movie", resultID)
	}
	// the manual-crop, poster-from-URL, and whole-movie PATCH paths
	// (internal/api/batch/movie_edit.go) via the shared per-(jobID, movieID)
	// lock — for EVERY field key, not just poster sources. A non-source
	// override that skips this lock can clone the movie before a concurrent
	// crop persists its bounds and then UpdateMovie the stale whole-movie
	// clone, silently erasing the successful crop. Poster-source overrides
	// additionally refresh the cached poster assets before persisting
	// (refreshOverriddenPosterSource below), which the same lock covers.
	// Taken AFTER the per-resultID overrideMu — the PATCH/crop paths take
	// only this lock — so acquisition order is consistent (overrideMu →
	// poster-source lock → result-store locks) and cannot deadlock. The key
	// is the same movie ID the temp poster cache and crop endpoints use
	// (Movie.ID when set, FileMatchInfo.MovieID otherwise).
	movieID := posterLockKeyForMovieResult(result)
	releasePosterLock := AcquirePosterSourceLock(je.jobID, movieID)
	// Closure-form deferred release IMMEDIATELY after acquisition (L1): the
	// lock is refcounted, so a recovered panic anywhere below — especially
	// inside the convergence loop's explicit release→re-acquire handoffs,
	// which reassign releasePosterLock — must still drop the CURRENT
	// entry; a value-form defer registered later would either miss the
	// panic window or release a stale function value.
	defer func() { releasePosterLock() }()

	// Re-read the result under the lock: a crop or source-changing edit may
	// have persisted while this call waited on the lock, replacing the movie
	// — cloning below from the pre-lock snapshot would lose that edit on the
	// whole-movie write (GetFileResultByResultID returns a deep clone, so the
	// pre-lock read is stale-but-safe, never concurrent).
	//
	// That edit can also RE-KEY the result mid-wait: a rescrape that
	// corrected the match from movie A to movie B holds A's lock and commits
	// the result with Movie.ID/FileMatchInfo.MovieID = B. Planning from the
	// stale pre-lock ID would fan B's overridden movie out over the results
	// STILL indexed at A — copying B's poster state and metadata onto
	// unrelated movies — while B's own family is skipped, and every persist
	// would run under A's lock, unserialized against B's crop/edit paths. So
	// BOTH the movie ID and the lock key are re-resolved from the post-lock
	// read, mirroring the crop endpoint's convergence loop
	// (internal/api/batch/movie_edit_poster.go's updateBatchMoviePosterCrop):
	// when the ID changed, the OLD lock is released BEFORE the destination's
	// is acquired. This path holds two poster-source locks only for the
	// "id"-override rekey below (destination pair in lexical order, mirroring
	// rescrapePhase.Rescrape's A→B rule); every other override converges on
	// ONE lock, preserving the cycle-free order overrideMu → poster-source
	// lock(s) → result-store locks. The loop converges because each
	// re-acquisition waits behind a writer whose re-key is already committed.
	//
	// releaseDestPosterLock/destKey: the DESTINATION movie ID's poster-source
	// lock for an "id" override (A→B) — the cached poster assets move from
	// A's key to B's (P3-6), so B's crop/edit writers must serialize with
	// this operation too. Acquired in lexical key order: when B sorts after
	// the held origin key, B stacks directly on top; when B sorts BEFORE A,
	// A is released first, then B and A are acquired in order, and the plan
	// is re-derived (the outer for loop restarts) because an A-side edit
	// could have landed in the gap. Deferred in closure form because the
	// variable is reassigned by the handoff; a stale destination lock (origin
	// key changed again or the re-derived override no longer rekeys) is
	// released immediately instead.
	var releaseDestPosterLock func()
	destKey := ""
	dropDestLock := func() {
		if releaseDestPosterLock != nil {
			releaseDestPosterLock()
			releaseDestPosterLock = nil
			destKey = ""
		}
	}
	defer dropDestLock()

	var filePath string
	var movie *models.Movie
	var prov *resultstore.ProvenanceData
	var oldPosterURL, oldCoverURL string
	for {
		for {
			freshResult, fp, stillFound := je.store.GetFileResultByResultID(resultID)
			if !stillFound || freshResult == nil || freshResult.Movie == nil {
				return nil, nil, fmt.Errorf("result %s not found or has no movie", resultID)
			}
			result = freshResult
			filePath = fp
			resolvedID := posterLockKeyForMovieResult(result)
			if resolvedID == movieID {
				break
			}
			releasePosterLock()
			movieID = resolvedID
			releasePosterLock = AcquirePosterSourceLock(je.jobID, movieID)
			// The origin key changed: any destination lock being held is now a
			// stale (and possibly order-violating) pair member — drop it and
			// let this pass re-derive the destination.
			dropDestLock()
		}

		prov = je.store.GetProvenance(filePath)
		if prov == nil {
			prov = &resultstore.ProvenanceData{}
		}
		movie = result.Movie.Clone()
		oldPosterURL = movie.Poster.PosterURL
		oldCoverURL = movie.Poster.CoverURL
		if err := applyFieldOverride(movie, prov, fieldKey, source); err != nil {
			return nil, nil, err
		}

		// Only an "id" override re-keys the movie. When it does, the
		// DESTINATION key's lock joins the critical section (lexical pair);
		// applyFieldOverride is deterministic for a fixed (result, prov,
		// source), so re-running it after the lexical re-acquisition yields
		// the same newID unless the underlying state changed — which the
		// convergence loop above then re-checks.
		newID := movie.ID
		// Pair decisions compare the case-folded key segments
		// (PosterSourceLockMovieID): the lock map folds case, so a case-only
		// "rekey" names the SAME lock — pairing it would self-deadlock — and
		// raw-string lexical order can disagree with the folded order.
		if fieldKey != "id" || newID == "" || newID == movieID {
			dropDestLock() // stale destination from a prior pass; no rekey now
			break
		}
		if PosterSourceLockMovieID(newID) == PosterSourceLockMovieID(movieID) {
			// Case-only re-key: no destination lock is stacked (same folded
			// key), but the RAW-keyed cache assets and preview-URL segments
			// still re-key — poster/manager.go names files by the raw movie
			// ID, so on a case-sensitive filesystem abc-full.jpg and
			// ABC-full.jpg are DIFFERENT files. Record the destination so
			// the collision check and MigratePosterCacheAssets below still
			// run; the single held lock already serializes every writer
			// keyed on either casing.
			dropDestLock() // stale destination lock from a prior pass
			destKey = newID
			break
		}
		if releaseDestPosterLock != nil && PosterSourceLockMovieID(destKey) == PosterSourceLockMovieID(newID) {
			break // destination pair already held from an earlier pass
		}
		if PosterSourceLockMovieID(newID) > PosterSourceLockMovieID(movieID) {
			dropDestLock()
			releaseDestPosterLock = AcquirePosterSourceLock(je.jobID, newID)
			destKey = newID
			break
		}
		dropDestLock()
		releasePosterLock()
		releaseDestPosterLock = AcquirePosterSourceLock(je.jobID, newID)
		destKey = newID
		releasePosterLock = AcquirePosterSourceLock(je.jobID, movieID)
	}

	// Codex P2: an "id" override whose destination ID is ALREADY in use by
	// another result family must be REJECTED before the asset move below —
	// running HERE, under the held (origin, destination) lock pair, before
	// any asset or state mutation. Shared with the whole-movie PATCH rename
	// (updateBatchMovie) via CheckRenameDestinationCollision; see its doc
	// for the full rationale (normalizing move semantics, origin-only
	// fan-out, same-family sibling exclusion, lock-pair index freeze).
	if err := CheckRenameDestinationCollision(je.store.FindFilePathsForMovieID, movieID, filePath, destKey); err != nil {
		return nil, nil, err
	}

	// P3-6: an "id" override re-keyed the movie — migrate the cached poster
	// assets from the old key to the new one UNDER BOTH held locks, or they
	// are orphaned at the old key while every crop/preview lookup resolves
	// the new one. The preview URLs embedded in the persisted movie
	// (CroppedPosterURL AND OriginalCroppedPosterURL — the poster reset
	// flow reads the latter) carry the posterID path segment, so BOTH are
	// re-pointed. A failed move is reversed immediately by
	// MigratePosterCacheAssets via both keys' pre-move SNAPSHOTS (MoveAssets
	// joins per-asset-leg errors instead of short-circuiting, so one leg may
	// have completed; its normalizing semantics make a reversed re-key
	// destructive). Generators without a manager (test stubs) hold no assets:
	// the move degrades to the URL rewrite only.
	var moveAssetsBack func() error
	if destKey != "" {
		var moveErr error
		moveAssetsBack, moveErr = MigratePosterCacheAssets(je.posterGen, je.jobID, movieID, destKey)
		if moveErr != nil {
			return nil, nil, moveErr
		}
		movie.Poster.CroppedPosterURL = RewritePosterIDInPreviewURL(movie.Poster.CroppedPosterURL, movieID, destKey)
		movie.Poster.OriginalCroppedPosterURL = RewritePosterIDInPreviewURL(movie.Poster.OriginalCroppedPosterURL, movieID, destKey)
	}
	// compensateMove reverses the completed A→B asset migration (when one
	// was performed) and MUST be invoked on every error return past this
	// point — an aborted override must never leave the origin's assets
	// stranded at the destination key while the persisted state still
	// resolves the old one. A failed reversal rides along on the error
	// instead of being swallowed.
	compensateMove := func(err error) error {
		if moveAssetsBack == nil {
			return err
		}
		if backErr := moveAssetsBack(); backErr != nil {
			return fmt.Errorf("%w (poster asset move-back failed: %w)", err, backErr)
		}
		return err
	}

	var rollback func() error
	if fieldKey == "poster_url" || fieldKey == "cover_url" {
		var err error
		rollback, err = je.refreshOverriddenPosterSource(ctx, movie, oldPosterURL, oldCoverURL)
		if err != nil {
			return nil, nil, compensateMove(err)
		}
	}
	filePaths := je.store.FindFilePathsForMovieID(movieID)
	if len(filePaths) == 0 {
		filePaths = []string{filePath}
	}
	// Fan out PER PART (planMultipartOverride): each sibling's persisted
	// movie is its OWN stored snapshot with only the overridden field and
	// the synchronized poster state merged in (mergeOverrideOntoPart) —
	// never a wholesale clone of the selected part's movie. Sibling parts
	// carry distinct FileMatchInfo-derived identity fields (notably
	// OriginalFileName, which template contexts read for <FILENAME>/the NFO
	// original path); stamping CD1's values onto CD2 would render the
	// sibling's templates with the wrong source file.
	planFn := je.planOverrideFn
	if planFn == nil {
		planFn = je.planMultipartOverride
	}
	planned, err := planFn(filePaths, movie, prov, fieldKey, source)
	if err != nil {
		// The id-rekey asset migration already ran: reverse it BEFORE
		// returning so the rejected override never strands the origin's
		// assets at the destination key (compensateMove).
		return nil, nil, compensateMove(err)
	}
	// Whole-job envelope serialization (AcquireJobEnvelopeLock — parity with
	// the rescrape phase's commit window and the API edit handlers),
	// layered HERE because this method owns the override path's persist: the
	// fan-out → persistEnvelope → compensation tail below must not interleave
	// with another movie's whole-envelope persist. A peer edit on a different
	// movie of this job holds only its own poster-source lock, so its persist
	// could otherwise durably capture this override's committed-but-not-yet-
	// durable part writes — resurrecting them on restart after a persist
	// failure rolls them back in memory. Nests AFTER overrideMu and the
	// poster-source lock(s) (overrideMu → poster-source → job-envelope →
	// result-store locks); the deferred release runs before both (LIFO).
	// The asset migration/refresh above is cache-level, not envelope state,
	// so it stays outside this window.
	releaseEnvelopeLock := AcquireJobEnvelopeLock(je.jobID)
	defer releaseEnvelopeLock()
	updatedParts := make([]overridePartWrite, 0, len(planned))
	for _, part := range planned {
		if updateErr := je.UpdateMovie(ctx, part.filePath, part.movie); updateErr != nil {
			errMsg := fmt.Errorf("persist field override: %w", updateErr)
			// The FAILING part's UpdateMovie can already have committed DB
			// side effects — actress renames land BEFORE the movie upsert and
			// the in-memory write (UpdateMovie's order) — so reverting only
			// the SUCCESSFUL parts below would leave its partial write
			// permanent. Restore it first with the same RestoreMovieResult
			// semantics (the re-upsert reverts persisted actress edits, the
			// snapshot re-seat undoes the in-memory leg).
			if part.prior == nil {
				errMsg = fmt.Errorf("%w (no pre-override snapshot for failing part %s: its partial update could not be reverted)", errMsg, part.filePath)
			} else if revertErr := je.revertPartWrite(ctx, part); revertErr != nil {
				errMsg = fmt.Errorf("%w (revert of failing part %s failed: %v)", errMsg, part.filePath, revertErr)
			}
			for _, done := range updatedParts {
				// nil prior means the snapshot LOOKUP failed (distinct from a
				// snapshot holding a legitimately nil Movie — restored by
				// revertPartWrite). Nothing remains to restore TO, so this part
				// keeps the rejected write: surface that instead of silently
				// skipping.
				if done.prior == nil {
					errMsg = fmt.Errorf("%w (no pre-override snapshot for part %s: its update could not be reverted)", errMsg, done.filePath)
					continue
				}
				if revertErr := je.revertPartWrite(ctx, done); revertErr != nil {
					errMsg = fmt.Errorf("%w (revert of part %s failed: %v)", errMsg, done.filePath, revertErr)
				}
			}
			if rollback != nil {
				if rollbackErr := rollback(); rollbackErr != nil {
					errMsg = fmt.Errorf("%w (poster rollback failed: %v)", errMsg, rollbackErr)
				}
			}
			errMsg = compensateMove(errMsg)
			return nil, nil, errMsg
		}
		updatedParts = append(updatedParts, part)
	}
	// Snapshot the pre-override provenance of every part BEFORE the fan-out,
	// so the envelope-persist-failure compensation below can restore the
	// recorded attribution without having mutated its own working clone.
	origProvenance := make(map[string]*resultstore.ProvenanceData, len(filePaths))
	for _, partPath := range filePaths {
		origProvenance[partPath] = je.store.GetProvenance(partPath)
	}
	for _, partPath := range filePaths {
		je.store.SetProvenance(partPath, prov)
	}
	updated, _, _ := je.store.GetFileResultByResultID(resultID)
	updatedProv := je.store.GetProvenance(filePath)

	// The envelope persist runs HERE, inside the poster-source lock critical
	// section (P1-2): a failure is compensated BEFORE the lock releases —
	// revert every persisted part to its pre-override movie, restore the
	// pre-override provenance attribution, roll the poster cache back, then
	// move the re-keyed assets back. There is no release→re-acquire gap for
	// a crop/source edit to land in and be silently erased by the revert
	// (the old handler-side F3 window is closed by construction). Order
	// matters: asset restores run LAST so no part still holds the new state
	// while the cache/keys flip back. Compensation failures ride along with
	// the persist error (errors.Join) instead of being swallowed. A nil
	// persistEnvelope (standalone jobs, tests) skips the in-section persist
	// exactly like the pre-hoist non-API flow.
	if je.persistEnvelope != nil {
		if perr := je.persistEnvelope(); perr != nil {
			errMsg := fmt.Errorf("failed to persist job state after field override: %w: %w", ErrEnvelopePersist, perr)
			var errs []error
			for _, part := range updatedParts {
				if part.prior == nil {
					errs = append(errs, fmt.Errorf("no pre-override snapshot for part %s: its update could not be reverted", part.filePath))
					continue
				}
				if revertErr := je.revertPartWrite(ctx, part); revertErr != nil {
					errs = append(errs, fmt.Errorf("revert of part %s failed: %w", part.filePath, revertErr))
				}
			}
			for partPath, orig := range origProvenance {
				// SetProvenance(nil) stores a nil clone, which GetProvenance
				// normalizes back to nil — that IS the unset. Skipping a nil
				// original would leave this part's override attribution in place
				// after the revert (phantom provenance), and the next successful
				// envelope persist would durably capture it. Mirrors the rescrape
				// rollback's unconditional SetProvenance(preRescrapeProv).
				je.store.SetProvenance(partPath, orig)
			}
			if rollback != nil {
				if rollbackErr := rollback(); rollbackErr != nil {
					errs = append(errs, fmt.Errorf("poster rollback failed: %w", rollbackErr))
				}
			}
			if moveAssetsBack != nil {
				if moveBackErr := moveAssetsBack(); moveBackErr != nil {
					errs = append(errs, fmt.Errorf("poster asset move-back failed: %w", moveBackErr))
				}
			}
			if compErr := errors.Join(errs...); compErr != nil {
				errMsg = fmt.Errorf("%w (override revert failed: %v)", errMsg, compErr)
			}
			return nil, nil, errMsg
		}
	}
	return updated, updatedProv, nil
}

// overridePartWrite plans one part of the ApplyFieldOverride multipart
// fan-out: the per-part merged movie to persist and the part's pre-override
// snapshot held for compensation.
type overridePartWrite struct {
	filePath string
	// prior is the part's COMPLETE pre-override stored result clone
	// (GetMovieResult) — including a legitimately nil Movie. nil prior means
	// the snapshot lookup itself failed, NOT that the stored movie was nil:
	// conflating the two let compensation skip a part whose nil-Movie result
	// had just been overwritten, stranding the rejected edit in memory.
	prior *resultstore.MovieResult
	movie *models.Movie // per-part merged movie to persist
}

// planMultipartOverride builds every part's fan-out write BEFORE any
// persistence, so a merge failure aborts the override cleanly instead of
// stranding earlier parts on compensated state. A part whose stored
// snapshot could not be read (nil prior) keeps the wholesale clone of the
// selected part's movie — no per-part identity remains to preserve — and
// its compensation is reported as un-revertible instead of silently
// skipped; a part whose stored result legitimately has a nil Movie carries
// a PRESENT snapshot restored verbatim (revertPartWrite).
func (je *jobEditorImpl) planMultipartOverride(filePaths []string, movie *models.Movie, prov *resultstore.ProvenanceData, fieldKey, source string) ([]overridePartWrite, error) {
	planned := make([]overridePartWrite, 0, len(filePaths))
	for _, partPath := range filePaths {
		// Snapshot the complete prior result (a nil Movie inside a present
		// snapshot is a valid pre-state, restored verbatim by compensation).
		var prior *resultstore.MovieResult
		if previous, getErr := je.store.GetMovieResult(partPath); getErr == nil && previous != nil {
			prior = previous
		}
		var original *models.Movie
		if prior != nil {
			original = prior.Movie
		}
		partMovie := movie
		if original != nil {
			merged, mergeErr := mergeOverrideOntoPart(original, movie, prov, fieldKey, source)
			if mergeErr != nil {
				return nil, fmt.Errorf("merge field override onto part %s: %w", partPath, mergeErr)
			}
			partMovie = merged
		}
		planned = append(planned, overridePartWrite{filePath: partPath, prior: prior, movie: partMovie})
	}
	return planned, nil
}

// revertPartWrite restores one fanned-out part to its pre-write snapshot.
func (je *jobEditorImpl) revertPartWrite(ctx context.Context, part overridePartWrite) error {
	return je.RestoreMovieResult(ctx, part.filePath, part.prior)
}

// RestoreMovieResult implements JobEditor. A present snapshot is restored
// COMPLETELY — movie AND result identity: for a snapshot carrying a movie,
// UpdateMovie runs FIRST (it re-upserts the DB row, reverts any persisted
// actress-name edits, and preserves the cover-original backup), then the
// whole snapshot (FileMatchInfo included) is re-seated verbatim.
// UpdateMovie alone would NOT be an exact rollback: it re-stamps
// FileMatchInfo.MovieID from Movie.ID (resultUpdater.UpdateMovie), so a
// re-keyed result whose snapshot legitimately diverges
// (FileMatchInfo.MovieID=A, Movie.ID=B) would be left indexed at B despite
// the promised restore. A snapshot whose stored result had a nil Movie goes
// straight to the re-seat — UpdateMovie cannot express "no movie" (it
// dereferences movie and upserts by movie.ID). The DB movies-table row the
// rejected write upserted is NOT deleted on the nil-Movie leg: that table
// is a shared by-ID cache the row may pre-date this job in, and Delete
// could clobber a legitimate record; an unreferenced cache row is harmless
// while a wrong deletion is not.
func (je *jobEditorImpl) RestoreMovieResult(ctx context.Context, filePath string, prior *resultstore.MovieResult) error {
	if prior == nil {
		return fmt.Errorf("missing pre-edit snapshot for %s", filePath)
	}
	if prior.Movie != nil {
		if err := je.UpdateMovie(ctx, filePath, prior.Movie); err != nil {
			return err
		}
	}
	// Re-seat the snapshot VERBATIM: FileMatchInfo.MovieID (family/index
	// membership), status, and every other pre-edit result field restore
	// exactly — nothing about the result's identity is recomputed from the
	// movie. UpdateFileResult re-indexes and keeps the counters coherent, so
	// the no-op-on-aligned-IDs flow is unchanged when the snapshot already
	// agrees with Movie.ID.
	je.store.UpdateFileResult(filePath, prior.Clone())
	return nil
}

// posterLockKeyForMovieResult derives the shared per-(jobID, movieID)
// poster-source lock key for a stored result: Movie.ID when set,
// FileMatchInfo.MovieID otherwise — the same key the temp poster cache and
// the crop/PATCH/poster-from-URL endpoints use. Callers must guarantee
// result.Movie is non-nil.
func posterLockKeyForMovieResult(result *resultstore.MovieResult) string {
	movieID := result.Movie.ID
	if movieID == "" {
		movieID = result.FileMatchInfo.MovieID
	}
	return movieID
}

// editableJobAdapter satisfies EditableJob by composing jobReaderImpl,
// resultstore.MovieLookup, and jobEditorImpl. genuinely decomposed —
// no *BatchJob embedding.
type editableJobAdapter struct {
	JobReader
	resultstore.MovieLookup
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
// resultstore.MovieLookup, phaseControllerImpl, and JobLifecycle.
// fully decomposed — no *BatchJob embedding.
// Per DEEP-1: PhaseController now includes SetWorkflow/SetBatchCfg/SetJobStatus/etc.
type controlledJobAdapter struct {
	JobReader
	resultstore.MovieLookup
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
	resultstore.MovieLookup
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
	// carrying the source file path, the resolved movie ID, and a short status
	// message. The API layer wires this to broadcast a per-file WebSocket
	// ProgressMessage with FilePath set so the frontend's messagesByFile
	// populates and ProgressModal shows live per-file scrape status. Mirrors
	// main's realtime.ProgressAdapter which forwarded per-task scrape updates
	// to the WS hub. Called concurrently from worker goroutines. Nil = no
	// per-file success reporting.
	OnFileScraped func(filePath, movieID, message string)

	// OnFileScrapeFailed is invoked after each file's scrape fails, carrying the
	// source file path, the resolved movie ID (may be empty when no ID was
	// matched), and the error message. The API layer wires this to broadcast a
	// per-file WebSocket ProgressMessage with FilePath + Error set. Mirrors
	// main's realtime.ProgressAdapter failure forwarding. Called concurrently
	// from worker goroutines. Nil = no per-file failure reporting.
	OnFileScrapeFailed func(filePath, movieID, errMsg string)

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
