package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// ---------------------------------------------------------------------------
// APIRuntime — lifecycle manager for the API layer
//
// Owns hot-reload, cache invalidation, and factory lifecycle. Handlers receive
// APIDeps through narrow group interfaces; only server.go, routes.go, and the
// config-reload handler see APIRuntime. This split gives each struct a single
// responsibility:
//
//   - APIDeps: pure DI container (what handlers need to do their job)
//   - APIRuntime: lifecycle manager (how the server starts, reloads, and shuts down)
//
// APIRuntime wraps APIDeps rather than embedding it, so callers cannot
// accidentally reach lifecycle methods through the DI container.
// ---------------------------------------------------------------------------

// lazyValue encapsulates double-checked locking for a lazily-initialized value.
// Per S-7: extracted so that the pattern isn't duplicated per factory type.
// The isEmpty function determines whether the cached value is considered unset.
type lazyValue struct {
	value   any
	mu      sync.RWMutex
	build   func() any
	isEmpty func(any) bool
}

// newLazyValue creates a lazyValue with the given build and isEmpty functions.
func newLazyValue(build func() any, isEmpty func(any) bool) *lazyValue {
	return &lazyValue{build: build, isEmpty: isEmpty}
}

// Get returns the cached value, constructing it on first access using double-checked locking.
func (lv *lazyValue) Get() any {
	lv.mu.RLock()
	v := lv.value
	lv.mu.RUnlock()
	if !lv.isEmpty(v) {
		return v
	}

	newVal := lv.build()

	lv.mu.Lock()
	// Double-check: another goroutine may have built the value while we waited.
	if !lv.isEmpty(lv.value) {
		lv.mu.Unlock()
		return lv.value
	}
	lv.value = newVal
	lv.mu.Unlock()

	return newVal
}

// Invalidate clears the cached value so the next Get() rebuilds it.
func (lv *lazyValue) Invalidate() {
	lv.mu.Lock()
	lv.value = nil
	lv.mu.Unlock()
}

// APIRuntime manages the lifecycle of API-layer components that change at runtime.
// It holds a reference to APIDeps and is the only type authorized to mutate
// config-coupled state (APIConfig snapshots, workflow factories, runtime state).
//
// Per ADR-0045: the mutable state that was previously on APIDeps has been moved
// here so that APIDeps is truly read-only after construction. All mutations go
// through APIRuntime methods.
//
// Lock ordering: apiMu → CoreDeps.mu.
// Never hold CoreDeps.mu while acquiring apiMu, or deadlock may result.
type APIRuntime struct {
	deps *APIDeps

	// apiMu protects the mutable state below.
	apiMu sync.RWMutex

	// apiCfg holds the narrow API-layer config snapshot.
	// Rebuilt on every config hot-reload via ConfigFromAppConfig.
	apiCfg APIConfig

	// workflowFactory caches the shared dependency sub-graph (scraper, matcher,
	// organizer, downloader, NFO generator, template engine, scanner, poster
	// generator). Nil until first access; niled on config reload so the next
	// call rebuilds from fresh config/registry. Per ADR-0023.
	//
	// Per DEEP-8: a single cached factory replaces the former triple cache.
	// The factory supports all workflow modes (NewWorkflow, NewScrapeOnlyWorkflow,
	// NewScanOnlyWorkflow) so separate cached instances are unnecessary.
	workflowFactory *lazyValue

	// batchJobFactory caches the BatchJobFactory used for constructing batch
	// jobs and phase configurations. Nil until first access; niled on config
	// reload so the next call rebuilds from fresh config/matcher/posterGen.
	batchJobFactory *lazyValue

	// Runtime holds the mutable server runtime components (WebSocket hub, etc.).
	Runtime *RuntimeState

	// Server lifecycle — serverCtx is the server-lifetime context,
	// cancelled on Shutdown(). Used by batch job launch goroutines so they
	// receive a cancellation signal when the server shuts down.
	serverCtxOnce sync.Once
	serverCtx     context.Context
	serverCancel  context.CancelFunc
}

// NewAPIRuntime creates an APIRuntime that manages the given APIDeps.
// Per DEEP-2: the back-reference on APIDeps has been removed; callers that
// need both DI and lifecycle access hold *APIRuntime directly and call
// rt.Deps() for the immutable container.
func NewAPIRuntime(deps *APIDeps) *APIRuntime {
	r := &APIRuntime{deps: deps}
	r.workflowFactory = newLazyValue(r.buildWorkflowFactory, func(v any) bool { return v == nil })
	r.batchJobFactory = newLazyValue(r.buildBatchJobFactory, func(v any) bool { return v == nil })
	return r
}

// Deps returns the underlying APIDeps for DI wiring (route registration, etc.).
// This is the controlled escape hatch — callers that need the DI container
// but not lifecycle methods should use this.
func (r *APIRuntime) Deps() *APIDeps {
	return r.deps
}

// InitAPIConfig initializes the APIConfig from the current config.
// Must be called once after APIDeps is constructed, before any handler runs.
// This is called automatically by NewServer.
func (r *APIRuntime) InitAPIConfig() {
	if r.deps.CoreDeps == nil || !r.deps.CoreDeps.HasConfig() {
		return
	}
	cfg := r.deps.CoreDeps.GetConfig()
	r.apiMu.Lock()
	r.apiCfg = ConfigFromAppConfig(cfg)
	r.apiMu.Unlock()
}

// EnsureRuntime initializes and returns the runtime state.
func (r *APIRuntime) EnsureRuntime() *RuntimeState {
	r.apiMu.Lock()
	defer r.apiMu.Unlock()
	if r.Runtime == nil {
		r.Runtime = NewRuntimeState()
	}
	return r.Runtime
}

// ReplaceReloadable is defined in hot_reload.go.

// GetAPIConfig returns the current narrow API config snapshot (thread-safe).
// Concurrent handlers see a consistent snapshot, not partial writes,
// because the entire struct is swapped under write lock during reload.
func (r *APIRuntime) GetAPIConfig() APIConfig {
	r.apiMu.RLock()
	cfg := r.apiCfg
	r.apiMu.RUnlock()
	return cfg
}

// GetRuntime returns the runtime state (thread-safe).
func (r *APIRuntime) GetRuntime() *RuntimeState {
	r.apiMu.RLock()
	rt := r.Runtime
	r.apiMu.RUnlock()
	return rt
}

// getWorkflowFactory returns the cached WorkflowFactory, constructing it on first access.
// Per ADR-0023: the factory caches the shared sub-graph so that per-request Workflow
// buildWorkflowFactory constructs a new WorkflowFactory from the current config.
// Used as the build function for the lazy workflowFactory value.
func (r *APIRuntime) buildWorkflowFactory() any {
	fc, fcErr := workflow.NewFactoryConfigFromRepos(r.deps.CoreDeps.GetConfig(), r.deps.CoreDeps.GetRegistry(), r.deps.Repos)
	if fcErr != nil {
		// Per S-10: ScraperConstructionError is a known partial-construction failure.
		// We still create the factory so that scan-only workflows work (they don't
		// need a scraper). Full-workflow requests will fail with a clear error.
		var sce *workflow.ScraperConstructionError
		if !errors.As(fcErr, &sce) {
			logging.Errorf("Failed to create workflow factory config: %v", fcErr)
			return nil
		}
		logging.Warnf("Workflow factory created with nil scraper — full workflows unavailable: %v", fcErr)
	}
	newFactory, err := workflow.NewWorkflowFactory(fc)
	if err != nil {
		logging.Errorf("Failed to create workflow factory: %v", err)
		return nil
	}
	return newFactory
}

// getWorkflowFactory returns the cached WorkflowFactory, constructing it on first access.
// Per ADR-0023: the factory caches the shared sub-graph so that per-request Workflow
// construction only creates a fresh RevertLog (varying by JobID).
//
// Per DEEP-8: a single cached factory replaces the former triple cache. The factory
// supports all workflow modes — callers use NewWorkflow, NewScrapeOnlyWorkflow, or
// NewScanOnlyWorkflow on the same factory instance.
func (r *APIRuntime) getWorkflowFactory() (*workflow.WorkflowFactory, error) {
	f := r.workflowFactory.Get()
	if f == nil {
		return nil, fmt.Errorf("workflow factory not available — check config and registry")
	}
	return f.(*workflow.WorkflowFactory), nil
}

// GetWorkflow constructs a scrape-only Workflow from the cached factory.
// Returns nil if the workflow cannot be created (e.g., missing DB or registry).
func (r *APIRuntime) GetWorkflow() workflow.WorkflowInterface {
	factory, err := r.getWorkflowFactory()
	if err != nil {
		logging.Errorf("Failed to create workflow factory: %v", err)
		return nil
	}
	wf, err := factory.NewScrapeOnlyWorkflow()
	if err != nil {
		logging.Errorf("Failed to create scrape-only workflow: %v", err)
		return nil
	}
	return wf
}

// buildBatchJobFactory constructs a new BatchJobFactory from the current config.
// Used as the build function for the lazy batchJobFactory value.
//
// Per W-3: retrieves the PosterGenerator from the cached WorkflowFactory instead of
// constructing a new ScrapePosterGenerator, collapsing the construction site.
func (r *APIRuntime) buildBatchJobFactory() any {
	apiCfg := r.GetAPIConfig()
	batchCfg := apiCfg.BatchConfig()

	// Per W-3: the factory must always be available when the API layer runs
	// (API server starts after bootstrapping). If PosterGen is nil, return nil
	// to signal failure rather than constructing a new one outside the factory.
	posterGen := r.GetPosterGen()
	if posterGen == nil {
		logging.Error("buildBatchJobFactory: workflow factory PosterGen is nil — cannot construct batch job factory without it")
		return nil
	}

	matcher := r.NewMatcher()
	workerBatchCfg := worker.BatchJobConfig{
		MaxWorkers:      batchCfg.MaxWorkers,
		WorkerTimeout:   batchCfg.WorkerTimeout,
		ScraperPriority: batchCfg.ScraperPriority,
		NFOEnabled:      batchCfg.NFOEnabled,
	}

	// Re-hydrate reconstructed jobs with infrastructure deps (matcher, posterGen,
	// batchCfg) that were not available at JobStore construction time. This
	// ensures jobs loaded from DB on startup can run apply/rescrape with the
	// correct BatchCfg (e.g. NFOEnabled) and PosterGen after restart.
	r.deps.JobStore.SetReconstructionDeps(matcher, posterGen, workerBatchCfg)

	return worker.NewBatchJobFactory(
		r.deps.JobStore,
		nil, // WF is per-job (GetBatchWorkflow), not shared across all jobs
		matcher,
		posterGen,
		workerBatchCfg,
		r.deps.EventEmitter,
	)
}

// GetBatchJobFactory returns a cached BatchJobFactoryInterface for constructing batch jobs
// and phase configurations. The factory is lazily initialized on first access using the
// current APIConfig snapshot, matcher, and poster manager. Invalidated on config reload
// so the next call rebuilds from fresh config.
//
// Per DEEP-4: previously, GetBatchJobFactory on APIDeps constructed a fresh factory on
// every call, re-reading APIConfig and creating a new ScrapePosterGenerator each time.
// Caching in APIRuntime avoids repeated construction for the same config/matcher/posterGen.
func (r *APIRuntime) GetBatchJobFactory() worker.BatchJobFactoryInterface {
	f := r.batchJobFactory.Get()
	if f == nil {
		return nil
	}
	return f.(worker.BatchJobFactoryInterface)
}

// NewMatcher constructs a matcher.MatcherInterface from the current APIConfig snapshot.
// This keeps matcher construction knowledge in the deps layer so that API handlers
// don't need to import the matcher package directly.
// Returns nil if the matcher cannot be created (e.g., invalid regex pattern).
func (r *APIRuntime) NewMatcher() matcher.MatcherInterface {
	apiCfg := r.GetAPIConfig()
	matchCfg := apiCfg.MatcherConfig()
	mat, err := matcher.NewMatcher(&matcher.Config{
		RegexEnabled: matchCfg.RegexEnabled,
		RegexPattern: matchCfg.RegexPattern,
	})
	if err != nil {
		logging.Warnf("Failed to create matcher from APIConfig: %v", err)
		return nil
	}
	return mat
}

// GetBatchWorkflow constructs the workflow used by a batch job.
// Delegates to the cached workflow factory. Returns an error if the factory
// cannot be created (e.g., missing DB or registry).
func (r *APIRuntime) GetBatchWorkflow(jobID string) (workflow.WorkflowInterface, error) {
	factory, err := r.getWorkflowFactory()
	if err != nil {
		return nil, err
	}
	return factory.NewWorkflow(jobID)
}

// GetScanOnlyWorkflow constructs a scan-only workflow from the cached factory.
// Per DEEP-8: uses the single cached factory instead of a separate scan-only factory.
func (r *APIRuntime) GetScanOnlyWorkflow() (workflow.WorkflowInterface, error) {
	factory, err := r.getWorkflowFactory()
	if err != nil {
		return nil, err
	}
	return factory.NewScanOnlyWorkflow(), nil
}

// GetPosterGen returns the cached PosterGenerator from the workflow factory's
// shared sub-graph. Per W-3: the API layer retrieves the cached instance from
// the factory instead of constructing its own, collapsing 3 construction sites to 1.
// Returns nil if the workflow factory is unavailable.
func (r *APIRuntime) GetPosterGen() poster.PosterGenerator {
	if wf, err := r.getWorkflowFactory(); err == nil && wf.PosterGen() != nil {
		return wf.PosterGen()
	}
	return nil
}

// GetPosterManager returns a cached PosterManager using an SSRF-safe HTTP client.
// The manager is lazily initialized on first access and invalidated on config reload.
// Returns nil if config is unavailable.
func (r *APIRuntime) GetPosterManager() poster.PosterManagerInterface {
	if !r.deps.CoreDeps.HasConfig() {
		return nil
	}
	cfg := r.deps.CoreDeps.GetConfig()

	rs := r.GetRuntime()
	if rs == nil {
		// Fallback: create a fresh instance if runtime state is not yet initialized.
		httpClient := ssrf.NewSSRFSafeClient(60 * time.Second)
		return poster.NewPosterManager(r.deps.GetFs(), cfg.System.TempDir, httpClient)
	}

	return rs.GetPosterManager(func() poster.PosterManagerInterface {
		httpClient := ssrf.NewSSRFSafeClient(60 * time.Second)
		return poster.NewPosterManager(r.deps.GetFs(), cfg.System.TempDir, httpClient)
	})
}

// Server lifecycle methods (ServerCtx, Shutdown) are defined in server_lifecycle.go.

// SetConfig sets the full application config and rebuilds the APIConfig snapshot.
// This is a convenience method for test setup. Production code should use
// APIRuntime.ReplaceReloadable() instead.
func (r *APIRuntime) SetConfig(cfg *config.Config) {
	if r.deps.CoreDeps == nil {
		r.deps.CoreDeps = &commandutil.CoreDeps{}
	}
	r.deps.CoreDeps.SetConfig(cfg)
	// Keep APIConfig in sync whenever the full config is set
	r.apiMu.Lock()
	r.apiCfg = ConfigFromAppConfig(cfg)
	r.apiMu.Unlock()
}

// ReloadConfig is defined in hot_reload.go.

// InvalidateWorkflowCaches and InvalidateWorkflowCachesOnRuntime are defined in hot_reload.go.

// shutdownDeps gracefully shuts down runtime resources in APIRuntime.
//
//nolint:unused // used by same-package tests
func shutdownDeps(rt *APIRuntime) {
	if rt == nil {
		return
	}
	rs := rt.GetRuntime()
	if rs == nil {
		return
	}
	rs.Shutdown()
}

// invalidateFactories is defined in hot_reload.go.

// ---------------------------------------------------------------------------
// Legacy compatibility — these package-level functions delegate to APIRuntime.
// They exist so that callers that only have *APIDeps can still perform
// lifecycle operations without constructing an APIRuntime explicitly.
// New code in production paths should use *APIRuntime directly.
// ---------------------------------------------------------------------------
