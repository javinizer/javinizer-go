package core

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// ---------------------------------------------------------------------------
// Hot-reload methods on APIRuntime
//
// ReplaceReloadable, ReloadConfig, invalidateFactories, and
// InvalidateWorkflowCaches manage atomic config/registry swaps and
// cache invalidation during hot-reload. Extracted from runtime_manager.go
// so that file focuses on lazy init + factory construction.
// ---------------------------------------------------------------------------

// ReplaceReloadable swaps config-coupled runtime components atomically.
//
// The registry swap, the APIConfig snapshot rebuild, and the cached-factory
// invalidation all happen under a single reloadMu.Lock so a concurrent reader
// (GetAPIConfig / Snapshot) cannot observe a mix of old/new state across the
// three holders. Config is stored via atomic.Pointer inside the same critical
// section as the registry swap, so GetConfig and GetRegistry stay consistent.
//
// The test-only reloadPauseAfterRegistry seam fires AFTER the atomic publish,
// so a paused reloader exposes a fully consistent (post-publish) state — the
// race it widens is closed by the lock.
func (r *APIRuntime) ReplaceReloadable(cfg *config.Config, registry *scraperutil.ScraperRegistry) {
	if cfg == nil {
		panic("core: APIRuntime.ReplaceReloadable() called with nil config — this is a programming error")
	}
	if r.deps.CoreDeps == nil {
		r.deps.CoreDeps = &commandutil.CoreDeps{}
	}
	r.reloadMu.Lock()
	r.deps.CoreDeps.ReplaceReloadable(cfg, registry)
	r.invalidateFactoriesLocked(cfg)
	r.reloadMu.Unlock()

	if r.reloadPauseAfterRegistry != nil {
		r.reloadPauseAfterRegistry()
	}
}

// ReloadConfig rebuilds the scraper registry from the given config and atomically
// swaps the config and registry. Callers (e.g., system/config.go) no longer need
// to construct aggregator or matcher directly — the workflow factory creates them
// from config on each request.
func (r *APIRuntime) ReloadConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("ReloadConfig: config is nil")
	}
	if r.deps.CoreDeps == nil {
		return fmt.Errorf("ReloadConfig: CoreDeps is not initialized")
	}
	reg := scraperutil.NewScraperRegistry()
	scraper.RegisterAll(reg)

	// Must finalize before reading Overrides — populates defaults for
	// unconfigured scrapers and builds the validateFns dispatch.
	if err := cfg.Scrapers.Finalize(reg); err != nil {
		return fmt.Errorf("failed to finalize scraper config: %w", err)
	}
	cfg.RecomputeWarnings()

	r18DumpLookup, r18DumpCloser, dumpErr := commandutil.OpenR18DevDumpLookup(cfg)
	if dumpErr != nil {
		// Surface a broken dump setup (permission denied, corrupt file) instead
		// of silently downgrading to "absent". The reload keeps working via HTTP
		// fallback, but the failure is logged so it is diagnosable.
		logging.Warnf("%v", dumpErr)
	}
	newRegistry, err := scraper.NewDefaultScraperRegistryFrom(reg, scraper.ScraperRegistryConfigFromApp(cfg), r.deps.Repos.ContentIDMappingRepo, r18DumpLookup)
	if err != nil {
		return fmt.Errorf("failed to initialize scraper registry: %w", err)
	}
	// Atomic publication (issue #44): swap the dump closer, publish cfg+registry,
	// rebuild the APIConfig snapshot, and invalidate the cached factories all
	// under one reloadMu.Lock so concurrent readers cannot observe a mix of
	// old/new state across the three holders. The slow registry construction
	// above stays outside the lock; only pointer swaps and cheap invalidations
	// are inside. Lock order is reloadMu → CoreDeps.mu.
	r.reloadMu.Lock()
	// Swap the reloadables BEFORE retiring the old dump handle. Closing the
	// old closer first would leave a window where the still-active scraper
	// registry references a closed SQLite connection and dump-backed searches
	// fail. Replacing first ensures new lookups route to the new dump store
	// before the old one is released.
	old := r.deps.CoreDeps.ReplaceR18DevDumpCloser(r18DumpCloser)
	r.deps.CoreDeps.ReplaceReloadable(cfg, newRegistry)
	r.invalidateFactoriesLocked(cfg)
	r.reloadMu.Unlock()
	if old != nil {
		_ = old.Close()
	}

	// Test seams fire after the atomic publish so readers observe a consistent
	// snapshot; nil in production.
	if r.reloadPauseAfterRegistry != nil {
		r.reloadPauseAfterRegistry()
	}
	if r.reloadPauseAfterAPICfg != nil {
		r.reloadPauseAfterAPICfg()
	}

	return nil
}

// InvalidateWorkflowCaches refreshes the aggregator's replacement caches in-place
// so the next operation sees fresh genre/word/alias data. Use this when in-memory
// caches are mutated (e.g., genre/word replacement CRUD) and the next operation
// must see the updated mappings.
//
// the factory's shared sub-graph (scraper, matcher, organizer,
// downloader, NFO generator, poster, scanner) is read-only after construction.
// Only the aggregator's replacement caches need to be reloaded — not the entire
// dependency graph. This avoids the cold-start penalty that would result from
// nil-ing the factories and forcing a full rebuild on the next request.
//
// Per DEEP-8: single factory — only one cache to refresh instead of three.
func (r *APIRuntime) InvalidateWorkflowCaches() {
	r.workflowFactory.mu.RLock()
	cached := r.workflowFactory.value
	r.workflowFactory.mu.RUnlock()
	if cached == nil {
		return
	}
	factory := cached.(*workflow.WorkflowFactory)

	// Targeted reload: refresh replacement caches on the existing factory
	// without destroying the shared sub-graph.
	factory.ReloadReplacementCaches(context.Background())
}

// InvalidateWorkflowCachesOnRuntime returns a function that calls
// InvalidateWorkflowCaches on the given APIRuntime.
// Used by route registration where a func() callback is needed
// (e.g., genre handler cache invalidation).
func InvalidateWorkflowCachesOnRuntime(rt *APIRuntime) func() {
	return func() {
		rt.InvalidateWorkflowCaches()
	}
}

// invalidateFactoriesLocked rebuilds the APIConfig snapshot and nils all cached
// workflow factories so they are reconstructed from the new config on next
// access. Also invalidates the cached poster manager on RuntimeState.
//
// Caller must hold r.reloadMu so the apiCfg publish and factory invalidation
// are atomic relative to the registry swap in ReplaceReloadable/ReloadConfig.
func (r *APIRuntime) invalidateFactoriesLocked(cfg *config.Config) {
	r.apiCfg = ConfigFromAppConfig(cfg)
	r.reloadGen++

	r.workflowFactory.Invalidate()
	r.batchJobFactory.Invalidate()

	// Invalidate poster manager on RuntimeState so it is reconstructed with
	// fresh config (e.g., tempDir may have changed) on next access.
	if r.Runtime != nil {
		r.Runtime.InvalidatePosterManager()
	}
}
