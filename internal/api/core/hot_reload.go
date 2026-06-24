package core

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
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
// Config is stored via atomic.Pointer INSIDE the write lock so that
// GetConfig (which reads atomic-first) cannot see new config while
// GetRegistry (mutex-protected) still returns
// old values. This prevents a split-brain window during hot-reload.
func (r *APIRuntime) ReplaceReloadable(cfg *config.Config, registry *scraperutil.ScraperRegistry) {
	if cfg == nil {
		panic("core: APIRuntime.ReplaceReloadable() called with nil config — this is a programming error")
	}
	if r.deps.CoreDeps == nil {
		r.deps.CoreDeps = &commandutil.CoreDeps{}
	}
	r.deps.CoreDeps.ReplaceReloadable(cfg, registry)

	// Rebuild APIConfig from new config and invalidate the workflow factories
	// so the next request rebuilds them from the fresh config/registry.
	r.invalidateFactories(cfg)
}

// ReloadConfig rebuilds the scraper registry from the given config and atomically
// swaps the config and registry. Callers (e.g., system/config.go) no longer need
// to construct aggregator or matcher directly — the workflow factory creates them
// from config on each request.
func (r *APIRuntime) ReloadConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("ReloadConfig: config is nil")
	}
	reg := scraperutil.NewScraperRegistry()
	scraper.RegisterAll(reg)

	// Must finalize before reading Overrides — populates defaults for
	// unconfigured scrapers and builds the validateFns dispatch.
	if err := cfg.Scrapers.Finalize(reg); err != nil {
		return fmt.Errorf("failed to finalize scraper config: %w", err)
	}

	newRegistry, err := scraper.NewDefaultScraperRegistryFrom(reg, scraper.ScraperRegistryConfigFromApp(cfg), r.deps.Repos.ContentIDMappingRepo)
	if err != nil {
		return fmt.Errorf("failed to initialize scraper registry: %w", err)
	}
	if r.deps.CoreDeps == nil {
		return fmt.Errorf("ReloadConfig: CoreDeps is not initialized")
	}
	r.deps.CoreDeps.ReplaceReloadable(cfg, newRegistry)

	// Rebuild APIConfig and invalidate the workflow factories.
	r.invalidateFactories(cfg)

	return nil
}

// InvalidateWorkflowCaches refreshes the aggregator's replacement caches in-place
// so the next operation sees fresh genre/word/alias data. Use this when in-memory
// caches are mutated (e.g., genre/word replacement CRUD) and the next operation
// must see the updated mappings.
//
// Per ADR-0023: the factory's shared sub-graph (scraper, matcher, organizer,
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

// invalidateFactories rebuilds the APIConfig snapshot and nils all cached
// workflow factories so they are reconstructed from the new config on next access.
// Also invalidates the cached poster manager on RuntimeState.
func (r *APIRuntime) invalidateFactories(cfg *config.Config) {
	r.apiMu.Lock()
	r.apiCfg = ConfigFromAppConfig(cfg)
	r.apiMu.Unlock()

	r.workflowFactory.Invalidate()
	r.batchJobFactory.Invalidate()

	// Invalidate poster manager on RuntimeState so it is reconstructed with
	// fresh config (e.g., tempDir may have changed) on next access.
	if r.Runtime != nil {
		r.Runtime.InvalidatePosterManager()
	}
}
