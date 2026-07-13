package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
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
// the mutable state that was previously on APIDeps has been moved
// here so that APIDeps is truly read-only after construction. All mutations go
// through APIRuntime methods.
//
// Lock ordering: reloadMu → CoreDeps.mu (and reloadMu → lazyValue.mu,
// reloadMu → RuntimeState.mu). apiMu protects only the Runtime pointer and
// is never acquired while holding reloadMu, so the two lock domains do not
// interact. Never hold CoreDeps.mu while acquiring reloadMu, or deadlock
// may result.
type APIRuntime struct {
	deps *APIDeps

	// apiMu protects the Runtime pointer below. It is intentionally separate
	// from reloadMu: Runtime is set once at init and read on the request path,
	// while reloadMu serializes hot-reload publication. Keeping them split means
	// a Runtime read never blocks on an in-progress reload.
	apiMu sync.RWMutex

	// reloadMu serializes hot-reload publication so the three config-coupled
	// holders — CoreDeps (cfg+registry), apiCfg, and the cached factory caches —
	// are published as one atomic unit. Readers that need a consistent view
	// across these holders take a snapshot via Snapshot() (reloadMu.RLock),
	// which prevents them from observing a mix of old/new state mid-reload.
	reloadMu sync.RWMutex

	// apiCfg holds the narrow API-layer config snapshot.
	// Rebuilt on every config hot-reload via ConfigFromAppConfig under reloadMu.
	apiCfg APIConfig

	// reloadGen is bumped under reloadMu on every config publication (reload /
	// SetConfig). RuntimeSnapshot captures it so its factory accessors can tell
	// whether a reload has landed since the snapshot was taken: a generation
	// match means the shared caches are still valid for this snapshot, while a
	// mismatch means the caches may hold a newer epoch and must not be served.
	// Comparing generations is more robust than cfg-pointer equality, which
	// misses a reload that reuses the same *config.Config with a new registry.
	reloadGen uint64

	// workflowFactory caches the shared dependency sub-graph (scraper, matcher,
	// organizer, downloader, NFO generator, template engine, scanner, poster
	// generator). Nil until first access; niled on config reload so the next
	// call rebuilds from fresh config/registry.
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

	// reloadPauseAfterRegistry and reloadPauseAfterAPICfg are test-only seams
	// that pause the reloader so race tests can probe publication consistency.
	// They fire AFTER the atomic reloadMu publication block, so a paused
	// reloader exposes a fully consistent post-publish state. Before the fix
	// they fired inside the non-atomic windows and observed split-brain state;
	// they remain as regression guards against reintroducing non-atomic
	// publication. Nil in production — checked and skipped on the reload path.
	reloadPauseAfterRegistry func()
	reloadPauseAfterAPICfg   func()

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
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	if r.deps.CoreDeps == nil || !r.deps.CoreDeps.HasConfig() {
		return
	}
	r.apiCfg = ConfigFromAppConfig(r.deps.CoreDeps.GetConfig())
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
// because the entire struct is swapped under reloadMu during reload.
//
// Note: GetAPIConfig is safe per-call, but a handler that reads GetAPIConfig
// and then separately reads the workflow factory can still observe a reload
// landing between the two calls. Such handlers must use Snapshot() to pin a
// consistent view across all config-coupled holders for the request lifetime.
func (r *APIRuntime) GetAPIConfig() APIConfig {
	r.reloadMu.RLock()
	cfg := r.apiCfg
	r.reloadMu.RUnlock()
	return cfg
}

// GetRuntime returns the runtime state (thread-safe).
func (r *APIRuntime) GetRuntime() *RuntimeState {
	r.apiMu.RLock()
	rt := r.Runtime
	r.apiMu.RUnlock()
	return rt
}

// RuntimeSnapshot is a consistent point-in-time view of the three config-coupled
// holders published atomically under reloadMu during hot-reload: the application
// config, the scraper registry, and the narrow APIConfig snapshot.
//
// A handler that needs to read more than one of these for a single request must
// take a Snapshot() once and read from it, rather than calling the individual
// accessors (GetAPIConfig, CoreDeps.GetConfig, getWorkflowFactory) separately —
// otherwise a reload landing between calls can yield a mix of old/new state.
//
// The captured cfg and registry pointers are the values installed by the last
// completed reload; they remain valid to use even after a subsequent reload
// publishes newer ones (Go GC keeps them alive). Phase 3 will add gen-gated
// factory accessors on this type so a snapshot's factory is always built from
// the snapshot's own cfg/registry rather than a fresher CoreDeps.
type RuntimeSnapshot struct {
	rt       *APIRuntime
	cfg      *config.Config
	registry *scraperutil.ScraperRegistry
	apiCfg   APIConfig
	gen      uint64
}

// Config returns the application config captured in this snapshot.
func (s *RuntimeSnapshot) Config() *config.Config { return s.cfg }

// APIConfig returns the narrow API-layer config captured in this snapshot.
func (s *RuntimeSnapshot) APIConfig() APIConfig { return s.apiCfg }

// Registry returns the scraper registry captured in this snapshot.
func (s *RuntimeSnapshot) Registry() *scraperutil.ScraperRegistry { return s.registry }

// RT returns the underlying APIRuntime. Use this only for immutable DI access
// (Deps(), ServerCtx(), GetRuntime()) that is not config-coupled; for
// config-coupled reads use the snapshot's own accessors so they stay consistent
// with the captured epoch.
func (s *RuntimeSnapshot) RT() *APIRuntime { return s.rt }

// Snapshot captures a consistent view of the config-coupled runtime state by
// reading all three holders under a single reloadMu.RLock. Use this for request
// paths that combine reads of config, registry, and APIConfig; it guarantees
// they all originate from the same reload epoch.
func (r *APIRuntime) Snapshot() *RuntimeSnapshot {
	r.reloadMu.RLock()
	defer r.reloadMu.RUnlock()
	return &RuntimeSnapshot{
		rt:       r,
		cfg:      r.deps.CoreDeps.GetConfig(),
		registry: r.deps.CoreDeps.GetRegistry(),
		apiCfg:   r.apiCfg,
		gen:      r.reloadGen,
	}
}

// WorkflowFactory returns a WorkflowFactory consistent with this snapshot.
//
// If no reload has landed since the snapshot was taken (reloadGen unchanged),
// the shared lazy cache is reused — the cache is only populated/invalidated
// under reloadMu, and holding reloadMu.RLock here prevents a reload from
// invalidating it mid-read, so the cached factory was necessarily built from
// the snapshot's config. This keeps the hot path (cache hit) cheap.
//
// If a reload has landed (generation differs), the shared cache may now hold a
// factory built from a newer config. Rather than serve a mismatched factory,
// this builds a fresh factory from the snapshot's captured cfg/registry WITHOUT
// caching it — caching a stale-epoch build would stomp the newer cache. This
// rebuild only happens on a request that straddles a reload (rare), so the perf
// cost is bounded.
func (s *RuntimeSnapshot) WorkflowFactory() (*workflow.WorkflowFactory, error) {
	r := s.rt
	r.reloadMu.RLock()
	sameEpoch := r.reloadGen == s.gen
	var cached any
	if sameEpoch {
		// Safe under RLock: a reload cannot invalidate the cache concurrently.
		cached = r.workflowFactory.Get()
	}
	r.reloadMu.RUnlock()
	if sameEpoch && cached != nil {
		return cached.(*workflow.WorkflowFactory), nil
	}
	if sameEpoch {
		return nil, fmt.Errorf("workflow factory not available — check config and registry")
	}
	// Reload landed between snapshot and here: build from the snapshot's own
	// cfg/registry so the factory matches snap.APIConfig(). Not cached.
	f := buildWorkflowFactoryFrom(s.cfg, s.registry, r.deps.Repos)
	if f == nil {
		return nil, fmt.Errorf("workflow factory not available — check config and registry")
	}
	return f.(*workflow.WorkflowFactory), nil
}

// PosterGen returns the cached PosterGenerator from a factory consistent with
// this snapshot. Returns nil if the factory is unavailable.
func (s *RuntimeSnapshot) PosterGen() poster.PosterGenerator {
	f, err := s.WorkflowFactory()
	if err != nil || f.PosterGen() == nil {
		return nil
	}
	return f.PosterGen()
}

// BatchWorkflow constructs a batch workflow from a factory consistent with this
// snapshot, so the workflow's cfg/registry match snap.APIConfig().
func (s *RuntimeSnapshot) BatchWorkflow(jobID string) (workflow.WorkflowInterface, error) {
	factory, err := s.WorkflowFactory()
	if err != nil {
		return nil, err
	}
	return factory.NewWorkflow(jobID)
}

// ScanOnlyWorkflow constructs a scan-only workflow from a factory consistent with
// this snapshot.
func (s *RuntimeSnapshot) ScanOnlyWorkflow() (workflow.WorkflowInterface, error) {
	factory, err := s.WorkflowFactory()
	if err != nil {
		return nil, err
	}
	return factory.NewScanOnlyWorkflow(), nil
}

// Matcher constructs a matcher from the snapshot's APIConfig. Cheap (no shared
// cache involved), and consistent with snap.APIConfig() by construction.
func (s *RuntimeSnapshot) Matcher() matcher.MatcherInterface {
	matchCfg := s.apiCfg.MatcherConfig()
	mat, err := matcher.NewMatcher(&matcher.Config{
		RegexEnabled: matchCfg.RegexEnabled,
		RegexPattern: matchCfg.RegexPattern,
	})
	if err != nil {
		logging.Warnf("Failed to create matcher from snapshot APIConfig: %v", err)
		return nil
	}
	return mat
}

// BatchJobFactory constructs a BatchJobFactory consistent with this snapshot.
//
// Hot path (no reload since the snapshot): reuses the shared lazy cache under
// reloadMu.RLock — matching WorkflowFactory()'s generation check — so
// SetReconstructionDeps (which iterates every job in the store) runs once per
// reload epoch, not once per request. Only when a reload has landed mid-request
// does this build fresh from snap.APIConfig()/PosterGen()/Matcher() without
// caching, so a stale-epoch request still gets a consistent factory without
// poisoning the newer cache.
//
// The stale-epoch fresh build deliberately does NOT call SetReconstructionDeps:
// that mutates the shared JobStore and every reconstructed job, so a stale
// request could otherwise roll current jobs back to old Matcher/PosterGen/
// BatchCfg after the new epoch was published. Reconstruction hydration stays on
// the current-epoch lazy build path (buildBatchJobFactory), which is always
// up-to-date by definition.
func (s *RuntimeSnapshot) BatchJobFactory() worker.BatchJobFactoryInterface {
	r := s.rt
	r.reloadMu.RLock()
	sameEpoch := r.reloadGen == s.gen
	var cached any
	if sameEpoch {
		// Safe under RLock: a reload cannot invalidate the cache concurrently,
		// and the cache was populated from this generation if sameEpoch holds.
		cached = r.batchJobFactory.Get()
	}
	r.reloadMu.RUnlock()
	if sameEpoch && cached != nil {
		return cached.(worker.BatchJobFactoryInterface)
	}
	if sameEpoch {
		return nil
	}
	// Reload landed between snapshot and here: build fresh from the snapshot's
	// own apiCfg/PosterGen/Matcher so the factory is internally consistent with
	// snap.APIConfig(). Not cached, and does NOT call SetReconstructionDeps —
	// see the doc comment above.
	batchCfg := s.apiCfg.BatchConfig()
	posterGen := s.PosterGen()
	if posterGen == nil {
		logging.Error("snapshot.BatchJobFactory: workflow factory PosterGen is nil — cannot construct batch job factory without it")
		return nil
	}
	matcherIface := s.Matcher()
	workerBatchCfg := worker.BatchJobConfig{
		MaxWorkers:      batchCfg.MaxWorkers,
		WorkerTimeout:   batchCfg.WorkerTimeout,
		ScraperPriority: batchCfg.ScraperPriority,
		NFOEnabled:      batchCfg.NFOEnabled,
	}
	return worker.NewBatchJobFactory(
		r.deps.JobStore,
		nil, // WF is per-job (BatchWorkflow), not shared across all jobs
		matcherIface,
		posterGen,
		workerBatchCfg,
		r.deps.EventEmitter,
	)
}

// PosterManager returns a PosterManager built from the snapshot's config (for
// tempDir) so it is consistent with snap.APIConfig(). Returns nil if the snapshot
// has no config (e.g. a test-only snapshot from NewSnapshotForTesting).
//
// Only routes through the shared RuntimeState cache when the snapshot's
// generation is current; a stale snapshot otherwise builds an uncached manager
// from snap.cfg so it can neither read nor repopulate the global cache with a
// mismatched TempDir from a different epoch.
func (s *RuntimeSnapshot) PosterManager() poster.PosterManagerInterface {
	if s.cfg == nil {
		return nil
	}
	r := s.rt
	buildFromSnap := func() poster.PosterManagerInterface {
		httpClient := ssrf.NewSSRFSafeClient(60 * time.Second)
		return poster.NewPosterManager(r.deps.GetFs(), s.cfg.System.TempDir, httpClient)
	}
	rs := r.GetRuntime()
	if rs == nil {
		return buildFromSnap()
	}
	// Stale snapshot: don't touch the shared cache — build an uncached manager
	// from this snapshot's captured config.
	r.reloadMu.RLock()
	current := r.reloadGen == s.gen
	r.reloadMu.RUnlock()
	if !current {
		return buildFromSnap()
	}
	return rs.GetPosterManager(buildFromSnap)
}

// NewSnapshotForTesting builds a RuntimeSnapshot without requiring a configured
// CoreDeps, for tests that exercise resolution paths (e.g. apply-config
// builders) with a nil/zero-config runtime. The cfg/registry fields are left
// nil; callers that need WorkflowFactory() should configure a real runtime and
// use Snapshot() instead.
func NewSnapshotForTesting(rt *APIRuntime, apiCfg APIConfig) *RuntimeSnapshot {
	return &RuntimeSnapshot{rt: rt, apiCfg: apiCfg}
}

// getWorkflowFactory returns the cached WorkflowFactory, constructing it on first access.
// buildWorkflowFactory constructs a new WorkflowFactory from the current config.
// Used as the build function for the lazy workflowFactory value.
func (r *APIRuntime) buildWorkflowFactory() any {
	return buildWorkflowFactoryFrom(r.deps.CoreDeps.GetConfig(), r.deps.CoreDeps.GetRegistry(), r.deps.Repos)
}

// buildWorkflowFactoryFrom constructs a WorkflowFactory from the given cfg/registry/repos.
// Shared by the lazy cache build path (buildWorkflowFactory) and the snapshot path
// (RuntimeSnapshot.WorkflowFactory), which builds from its captured cfg/registry when
// a reload has invalidated the shared cache mid-request.
func buildWorkflowFactoryFrom(cfg *config.Config, registry *scraperutil.ScraperRegistry, repos database.Repositories) any {
	fc, fcErr := workflow.NewFactoryConfigFromRepos(cfg, registry, repos)
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
// the factory caches the shared sub-graph so that per-request Workflow
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
	// Publish cfg, apiCfg, generation bump, and factory invalidation together
	// under reloadMu — the same atomic path reload uses — so a concurrent
	// GetAPIConfig/Snapshot cannot see new cfg with stale apiCfg or stale cached
	// factories.
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	r.deps.CoreDeps.SetConfig(cfg)
	r.invalidateFactoriesLocked(cfg)
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
