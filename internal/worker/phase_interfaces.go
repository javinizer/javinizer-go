package worker

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/workflow"
)

// defaultMaxWorkers is the default concurrency limit for the scrape phase.
// Apply phase uses a different default (1) because file operations are I/O-bound
// and benefit less from parallelism than network-bound scraping.
const defaultMaxWorkers = 5

// defaultWorkerTimeout is the default per-task timeout for scrape and apply phases.
const defaultWorkerTimeout = 5 * time.Minute

// concurrencyConfig holds resolved concurrency settings for a phase.
// Construct via newConcurrencyConfig to apply defaults — zero-value fields
// are replaced with the provided defaults.
type concurrencyConfig struct {
	MaxWorkers    int
	WorkerTimeout time.Duration
}

// newConcurrencyConfig constructs a concurrencyConfig, applying defaults for
// any zero or negative values. This is the single source of truth for default
// resolution — callers should not duplicate the if/else logic.
func newConcurrencyConfig(maxWorkers int, workerTimeout time.Duration, defaultMaxWorkers int, defaultWorkerTimeout time.Duration) concurrencyConfig {
	cc := concurrencyConfig{
		MaxWorkers:    maxWorkers,
		WorkerTimeout: workerTimeout,
	}
	if cc.MaxWorkers <= 0 {
		cc.MaxWorkers = defaultMaxWorkers
	}
	if cc.WorkerTimeout <= 0 {
		cc.WorkerTimeout = defaultWorkerTimeout
	}
	return cc
}

// progressBroadcaster sends job events to subscribers.
// *jobEventBroadcaster satisfies this interface.
type progressBroadcaster interface {
	Send(event JobEvent)
	Close()
}

// ResultUpdater updates file results, provenance, and exclusion state in the job.
// *ResultTracker satisfies this interface.
type ResultUpdater interface {
	UpdateFileResult(filePath string, result *MovieResult)
	SetProvenance(filePath string, prov *ProvenanceData)
	AtomicUpdateFileResult(filePath string, updateFn func(*MovieResult) (*MovieResult, error)) error

	// UpdateMovie atomically updates the movie for a file result.
	UpdateMovie(filePath string, movie *models.Movie) error

	// MarkExcluded marks a file as excluded and adjusts progress counters.
	// Per DEEP-5: merged from the former resultExcluder unexported interface.
	MarkExcluded(filePath string)
}

// PhaseLifecycle handles phase lifecycle transitions.
// *BatchJob satisfies this interface.
type PhaseLifecycle interface {
	MarkCompleted()
	MarkFailed()
	MarkCancelled()
	MarkOrganized()
}

// persister persists job state. Called by phases after state mutations.
// *BatchJob satisfies this interface via persistFunc adapter wrapping job.deps.PersistFn.
type persister interface {
	Persist()
}

// persistFunc wraps a func() to satisfy the persister interface.
// A nil persistFunc is safe to call — Persist() becomes a no-op.
type persistFunc func()

func (p persistFunc) Persist() {
	if p != nil {
		p()
	}
}

// ResultMapAccessor provides read-only and atomic-commit access to the result map.
// *ResultTracker satisfies this interface (per ADR-0042).
//
// Callers that only need to read results or atomically commit rescrape outcomes
// should accept ResultMapAccessor instead of the broader *ResultTracker.
// Per DEEP-8: only exported methods — external consumers (including Mockery)
// can satisfy this interface without implementing unexported methods.
type ResultMapAccessor interface {
	// IsGone returns true if the job is deleted or transitioned to a terminal state.
	IsGone() bool

	// GetFileMatchInfo returns match metadata for a file.
	GetFileMatchInfo(filePath string) (models.FileMatchInfo, bool)

	// GetCurrentMovieID returns the movie ID for a file's current result.
	// Returns empty string if no result or no movie.
	GetCurrentMovieID(filePath string) string

	// CommitResult atomically writes a result and recalculates progress.
	// Returns an error if the revision doesn't match expectedRevision (optimistic concurrency).
	CommitResult(filePath string, result *MovieResult, expectedRevision uint64) error

	// OtherResultUsesMovieID checks if any result OTHER than excludePath uses the given movieID.
	OtherResultUsesMovieID(excludePath string, movieID string) bool

	// GetMovieResult returns a clone of the MovieResult for a file path.
	GetMovieResult(filePath string) (*MovieResult, error)

	// CloneFileMatchInfo returns a shallow clone of the FileMatchInfo map.
	CloneFileMatchInfo() map[string]models.FileMatchInfo

	// SnapshotData returns a point-in-time clone of all result data.
	SnapshotData() ResultSnapshot

	// IsAllExcluded checks whether all files are excluded.
	// Per DEEP-5: merged from the former exclusionChecker unexported interface.
	IsAllExcluded() bool
}

// FileFinder provides the FindFileForMovieID method for resolving a movie ID
// to its primary file path and revision. Per ADR-0041: this is separate from
// MovieLookup because FindFileForMovieID has a different return type.
// *ResultTracker satisfies this interface.
type FileFinder interface {
	FindFileForMovieID(movieID string) (*FileLookupResult, error)
	// GetRevision returns the current revision for a file's result.
	// Per ADR-0041: moved from ResultMapAccessor to ResultTracker/FileFinder.
	GetRevision(filePath string) uint64
}

// scrapePhaseInputs carries only what the scrape phase needs.
// Not the full *BatchJob — each field is a narrow dependency.
type scrapePhaseInputs struct {
	JobID               models.JobID
	Concurrency         concurrencyConfig
	WF                  workflow.WorkflowInterface
	Matcher             matcher.MatcherInterface
	PosterGen           poster.PosterGenerator
	KeepBroadcasterOpen bool
	FileMatchInfo       map[string]models.FileMatchInfo

	// MovieRepo persists scraped movies OFF the per-goroutine critical path.
	// When set, the scrape phase opts out of the workflow's inline persist
	// (cmd.SkipPersist=true) and runs persistence in a dedicated goroutine pool
	// independent of eg.SetLimit(MaxWorkers), so SQLite's single-writer lock does
	// not serialize the per-file scrape workers. When nil (tests, scan-only),
	// the workflow's Scrape persists inline as before.
	MovieRepo database.MovieRepositoryInterface

	Broadcaster progressBroadcaster
	Updater     ResultUpdater
	Lifecycle   PhaseLifecycle
	persister   persister
}

// applyPhaseInputs carries only what the apply phase needs.
// Not the full *BatchJob — each field is a narrow dependency.
type applyPhaseInputs struct {
	JobID       models.JobID
	Concurrency concurrencyConfig
	NFOEnabled  bool
	WF          workflow.WorkflowInterface

	// Current state snapshot (frozen at construction, not live)
	Results     map[string]*MovieResult
	Excluded    map[string]bool
	Destination string
	Update      bool // Update mode (in-place, no file organization)

	Broadcaster progressBroadcaster
	Updater     ResultUpdater
	Lifecycle   PhaseLifecycle
	persister   persister
}

// rescrapePhaseInputs carries only what the rescrape phase needs.
// Not the full *BatchJob — each field is a narrow dependency.
//
// rescrapePhaseInputs is unique among phase inputs because CompleteRescrape
// performs atomic read-modify-write on the results map under a lock.
// The ResultMap interface abstracts this so RescrapePhase doesn't need *BatchJob.
type rescrapePhaseInputs struct {
	JobID       models.JobID
	Concurrency concurrencyConfig
	WF          workflow.WorkflowInterface
	PosterGen   poster.PosterGenerator

	// For ScrapeSingle — no job state access needed
	// For CompleteRescrape — needs result map access + metadata
	ResultMap ResultMapAccessor
	Lifecycle PhaseLifecycle
	persister persister

	// ADR-0041 Decision 3: additional dependencies for full rescrape sequence
	Lookup      MovieLookup  // for FindFilePathsForMovieID etc.
	Finder      FileFinder   // for FindFileForMovieID (not on MovieLookup interface)
	Fs          afero.Fs     // for poster cleanup
	TempDir     string       // for poster cleanup paths
	FsCaseCache *FSCaseCache // for orphaned poster detection
}

// ResultReadFacade composes the three read-oriented narrow interfaces that
// external consumers (adapters, phase inputs) typically need from ResultTracker.
// Callers that only need lookup/access should accept ResultReadFacade instead
// of the full *ResultTracker, reducing the exposed method surface from ~32 to ~15.
//
// *ResultTracker satisfies this interface.
type ResultReadFacade interface {
	ResultMapAccessor
	MovieLookup
	FileFinder
}

// Compile-time assertions that concrete types satisfy the interfaces.
var (
	_ progressBroadcaster = (*jobEventBroadcaster)(nil)
	_ ResultUpdater       = (*ResultTracker)(nil)
	_ PhaseLifecycle      = (*JobLifecycle)(nil)
	_ ResultMapAccessor   = (*ResultTracker)(nil)
	_ MovieLookup         = (*ResultTracker)(nil)
	_ FileFinder          = (*ResultTracker)(nil)
	_ ResultReadFacade    = (*ResultTracker)(nil)
	_ persister           = persistFunc(nil)
)

// IsGone returns true if the job is deleted or in a terminal-running state.
