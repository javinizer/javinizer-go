package core

import (
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"

	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
)

// ---------------------------------------------------------------------------
// Narrow handler dependency interfaces
// ---------------------------------------------------------------------------

// ScraperLister is the narrow interface the API layer requires from the
// scraper registry. Alias for scraperutil.ScraperListerInterface, which is
// the canonical definition shared by all consumers (API, TUI, health checks).
//
// Deprecated: Use scraperutil.ScraperListerInterface directly in new code.
// This alias is kept for backward compatibility with existing call sites.
type ScraperLister = scraperutil.ScraperListerInterface

// NOTE: Per DEEP-3 / LANGUAGE.md principle ("One adapter = hypothetical seam.
// Two adapters = real one."), the former BatchDeps, FileDeps, and RealtimeDeps
// interfaces have been removed. APIDeps is the sole adapter, so handlers accept
// *APIDeps directly. If a second adapter emerges (e.g., a test-only lightweight
// deps struct), re-introduce the interface at that point.

// ---------------------------------------------------------------------------
// APIDeps — read-only DI container
//
// Holds all dependencies that API handlers need. All fields are set once at
// construction and never mutated afterwards. Mutable config-coupled state
// (apiCfg, workflow factories, server lifecycle) is owned by APIRuntime
// (see runtime_manager.go). Handlers that need runtime methods receive
// *APIRuntime directly; APIDeps is the pure immutable DI container.
//
// Per DEEP-2: the former runtime back-reference and its 9 delegation methods
// have been removed. Handlers now accept *APIRuntime directly instead of
// proxying through APIDeps. This eliminates the nil-check fallback pattern
// and makes the dependency surface explicit.
// ---------------------------------------------------------------------------

// APIDeps holds the dependency graph for the API layer.
// Handlers that only need immutable deps (Repos, EventEmitter) receive *APIDeps
// via rt.Deps(). Handlers that also need runtime methods (GetAPIConfig,
// GetBatchWorkflow, etc.) receive *APIRuntime directly.
//
// APIDeps contains only immutable deps that are set once at construction and
// never mutated. All mutable config-coupled state (APIConfig snapshots,
// cached workflow factories, server lifecycle) is owned by APIRuntime.
// Per ADR-0045: the former mutable state has been extracted to APIRuntime
// so that APIDeps is truly read-only after construction.
type APIDeps struct {
	CoreDeps *commandutil.CoreDeps

	ConfigFile   string
	Repos        database.Repositories
	EventEmitter eventlog.EventEmitter
	Reverter     history.BatchReverter
	JobStore     worker.JobStoreInterface
	Auth         commandutil.AuthProvider
	TokenStore   TokenStoreInterface
	Fs           afero.Fs
}

// GetFs returns the filesystem used by API dependencies.
// If no filesystem is injected, it defaults to the OS filesystem.
func (d *APIDeps) GetFs() afero.Fs {
	if d.Fs == nil {
		return afero.NewOsFs()
	}
	return d.Fs
}

// ---------------------------------------------------------------------------
// APIDeps — DI accessors (read-only config, registry)
// ---------------------------------------------------------------------------
//
// GetConfig and GetRegistry are no longer overridden on APIDeps.
// Callers should use deps.CoreDeps.GetConfig() / deps.CoreDeps.GetRegistry()
// directly. This makes the dependency surface explicit and eliminates the
// fragile override pattern where promoted methods had to be overridden
// with panic guards.

// GetScraperLister returns the narrow ScraperLister interface for the current
// scraper registry. API handlers should prefer this over GetRegistry() to
// limit their dependency surface per Go convention: consume interfaces,
// produce structs.
func (d *APIDeps) GetScraperLister() ScraperLister {
	return d.CoreDeps.GetRegistry()
}

// ---------------------------------------------------------------------------
// APIDeps — narrow interface method implementations
//
// These methods are called directly on *APIDeps by handlers that receive
// *APIDeps via rt.Deps() and only need immutable deps.
// ---------------------------------------------------------------------------

// GetBatchFileOpRepo returns the batch file operation repository for counting operations.
func (d *APIDeps) GetBatchFileOpRepo() database.BatchFileOperationRepositoryInterface {
	return d.Repos.BatchFileOpRepo
}

// GetJobRepo returns the job repository for persisted-job queries (list, lookup, upsert).
// Callers that need persisted job records use this directly rather than routing through
// the in-memory JobStore — matching the api/jobs package's established pattern.
func (d *APIDeps) GetJobRepo() database.JobRepositoryInterface {
	return d.Repos.JobRepo
}

// GetEventEmitter returns the event emitter used for batch progress events.
func (d *APIDeps) GetEventEmitter() eventlog.EventEmitter {
	return d.EventEmitter
}

// GetJobStore returns the in-memory batch job store.
func (d *APIDeps) GetJobStore() worker.JobStoreInterface {
	return d.JobStore
}

// ---------------------------------------------------------------------------
// APIDeps — Scraper options helper
// ---------------------------------------------------------------------------

// scraperOptionsResult holds the display title and option entries for a scraper.
type scraperOptionsResult struct {
	DisplayTitle string
	Options      []models.ScraperOption
}

// GetScraperOptions returns the display title and configurable options for the named scraper.
// Returns (result, true) if found, or (zero, false) if the scraper is not registered.
func (d *APIDeps) GetScraperOptions(name string) (scraperOptionsResult, bool) {
	provider, exists := d.GetScraperLister().GetOptions(name)
	if !exists {
		return scraperOptionsResult{}, false
	}
	return scraperOptionsResult{
		DisplayTitle: provider.DisplayTitle,
		Options:      provider.Options,
	}, true
}

// ---------------------------------------------------------------------------
// APIDeps — lifecycle access helper
//
// NewAPIRuntime returns an APIRuntime wrapping d. Use this in test code
// that previously called the removed convenience methods (ReplaceReloadable,
// ReloadConfig, InvalidateWorkflowCaches) on APIDeps directly.
// ---------------------------------------------------------------------------
