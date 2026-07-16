package worker

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/timeout"
	"github.com/javinizer/javinizer-go/internal/worker/fscase"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
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
	MaxWorkers     int
	WorkerTimeout  time.Duration
	RequestTimeout time.Duration
}

// newConcurrencyConfig constructs a concurrencyConfig, applying defaults for
// any zero or negative values. This is the single source of truth for default
// resolution — callers should not duplicate the if/else logic.
func newConcurrencyConfig(maxWorkers int, workerTimeout, requestTimeout time.Duration, defaultMaxWorkers int, defaultWorkerTimeout time.Duration) concurrencyConfig {
	cc := concurrencyConfig{
		MaxWorkers:     maxWorkers,
		WorkerTimeout:  workerTimeout,
		RequestTimeout: requestTimeout,
	}
	if cc.MaxWorkers <= 0 {
		cc.MaxWorkers = defaultMaxWorkers
	}
	var resolved timeout.Timeout
	if cc.WorkerTimeout <= 0 {
		resolved = timeout.FromConfig("performance.worker_timeout", 0, defaultWorkerTimeout)
		cc.WorkerTimeout = resolved.Duration
	} else {
		resolved = timeout.FromDuration(cc.WorkerTimeout, "config:performance.worker_timeout")
	}
	logging.Debugf("Worker concurrency: maxWorkers=%d workerTimeout=%s requestTimeout=%s", cc.MaxWorkers, resolved, cc.RequestTimeout)
	return cc
}

// progressBroadcaster sends job events to subscribers.
// *jobEventBroadcaster satisfies this interface.
type progressBroadcaster interface {
	Send(event JobEvent)
	Close()
}

// ResultUpdater, ResultMapAccessor, FileFinder, MovieLookup, and
// ResultReadFacade are now defined in internal/worker/resultstore. They are
// redefined there with the same method groupings as before; the 7 new
// Store-only methods are NOT members of any narrow sub-interface.

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
	Updater     resultstore.ResultUpdater
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
	Results     map[string]*resultstore.MovieResult
	Excluded    map[string]bool
	Destination string
	Update      bool // Update mode (in-place, no file organization)

	Broadcaster progressBroadcaster
	Updater     resultstore.ResultUpdater
	Lifecycle   PhaseLifecycle
	persister   persister
}

// rescrapePhaseInputs carries only what the rescrape phase needs.
// Not the full *BatchJob — each field is a narrow dependency.
//
// rescrapePhaseInputs is unique among phase inputs because CompleteRescrape
// performs atomic read-modify-write on the results map under a lock.
// The ResultMap interface abstracts this so RescrapePhase doesn't need *BatchJob.
// Per the result-store extraction: ResultMap and Finder use the narrow
// resultstore sub-interfaces (the rescrape phase calls 6 ResultMapAccessor
// methods and 2 FileFinder methods — it does NOT need full Store). The Lookup
// field is removed (dead weight — no callers).
type rescrapePhaseInputs struct {
	JobID       models.JobID
	Concurrency concurrencyConfig
	WF          workflow.WorkflowInterface
	PosterGen   poster.PosterGenerator

	// For ScrapeSingle — no job state access needed
	// For CompleteRescrape — needs result map access + metadata
	ResultMap resultstore.ResultMapAccessor
	Lifecycle PhaseLifecycle
	persister persister

	// additional dependencies for full rescrape sequence
	Finder      resultstore.FileFinder // for FindFileForMovieID and GetRevision
	Fs          afero.Fs               // for poster cleanup
	TempDir     string                 // for poster cleanup paths
	FsCaseCache *fscase.FSCaseCache    // for orphaned poster detection
}

// Compile-time assertions that concrete types satisfy the interfaces.
var (
	_ progressBroadcaster = (*jobEventBroadcaster)(nil)
	_ PhaseLifecycle      = (*JobLifecycle)(nil)
	_ persister           = persistFunc(nil)
)

// IsGone returns true if the job is deleted or in a terminal-running state.
