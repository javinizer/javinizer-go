package core

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
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
	reg := scraperutil.NewScraperRegistry()
	scraper.RegisterAll(reg)
	if err := r.prepareReload(cfg, reg); err != nil {
		return err
	}
	r.reloadMu.Lock()
	err := r.reloadConfigLocked(cfg, reg)
	r.reloadMu.Unlock()
	if err == nil {
		if r.reloadPauseAfterRegistry != nil {
			r.reloadPauseAfterRegistry()
		}
		if r.reloadPauseAfterAPICfg != nil {
			r.reloadPauseAfterAPICfg()
		}
	}
	return err
}

// ReloadConfigLocked reloads config while the caller holds reloadMu.
func (r *APIRuntime) ReloadConfigLocked(cfg *config.Config) error {
	reg := scraperutil.NewScraperRegistry()
	scraper.RegisterAll(reg)
	if err := r.prepareReload(cfg, reg); err != nil {
		return err
	}
	return r.reloadConfigLocked(cfg, reg)
}

func (r *APIRuntime) prepareReload(cfg *config.Config, resolver models.ScraperConfigResolverInterface) error {
	if cfg == nil {
		return fmt.Errorf("ReloadConfig: config is nil")
	}
	if r.deps.CoreDeps == nil {
		return fmt.Errorf("ReloadConfig: CoreDeps is not initialized")
	}

	// Must finalize before reading Overrides — populates defaults for
	// unconfigured scrapers and builds the validateFns dispatch.
	if err := cfg.Scrapers.Finalize(resolver); err != nil {
		return fmt.Errorf("failed to finalize scraper config: %w", err)
	}
	cfg.RecomputeWarnings()
	if err := config.ValidateScraperOverrides(cfg); err != nil {
		return fmt.Errorf("invalid scraper configuration: %w", err)
	}
	return nil
}

// actressOnlyPriorityWarnings reports per-field config warnings when a
// field's override is satisfied only by actress-only resolvers. For the
// actress field that means no cast can ever be aggregated for them to
// enrich; for any other field it can never gain data from them. YAML-authored
// configs skip API save-time validation, so boot/reload reports them loudly.
func actressOnlyPriorityWarnings(reg *scraperutil.ScraperRegistry, cfg *config.Config) []config.ConfigWarning {
	if reg == nil || cfg == nil {
		return nil
	}
	fields := make([]string, 0, len(cfg.Metadata.Priority.Fields))
	for field := range cfg.Metadata.Priority.Fields {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	var warnings []config.ConfigWarning
	for _, field := range fields {
		override := cfg.Metadata.Priority.Fields[field]
		if len(override) == 0 {
			continue
		}
		if len(override) == 1 && strings.EqualFold(strings.TrimSpace(override[0]), "__skip__") {
			continue
		}
		// The actress field is driven by the sync engine's direct
		// ActressMetadataResolver calls, not movie search — an actress-only
		// provider list is a supported configuration there (codex r8).
		if strings.EqualFold(field, "actress") {
			continue
		}
		recognized, capable := false, false
		for _, name := range override {
			inst, found := reg.GetInstance(strings.ToLower(strings.TrimSpace(name)))
			if !found || inst == nil {
				continue
			}
			recognized = true
			if c, ok := inst.(models.MovieSearchCapable); !ok || c.SupportsMovieSearch() {
				capable = true
				break
			}
		}
		if !recognized || capable {
			continue
		}
		msg := fmt.Sprintf("metadata.priority.%s = [%s]: every listed scraper resolves actress metadata but never produces movie results; the field can never gain data from them — add a movie-capable scraper", field, strings.Join(override, ", "))
		warnings = append(warnings, config.ConfigWarning{Field: field, Scrapers: override, Message: msg})
	}
	return warnings
}

func (r *APIRuntime) reloadConfigLocked(cfg *config.Config, reg *scraperutil.ScraperRegistry) error {
	r18DumpLookup, r18DumpCloser, dumpErr := commandutil.OpenR18DevDumpLookup(cfg)
	if dumpErr != nil {
		// Surface a broken dump setup (permission denied, corrupt file) instead
		// of silently downgrading to "absent". The reload keeps working via HTTP
		// fallback, but the failure is logged so it is diagnosable.
		logging.Warnf("%v", dumpErr)
	}
	newRegistry, err := scraper.NewDefaultScraperRegistryFrom(reg, scraper.ScraperRegistryConfigFromApp(cfg, reg.Names(), reg.GetAllDefaults()), r.deps.Repos.ContentIDMappingRepo, r18DumpLookup)
	if err != nil {
		if r18DumpCloser != nil {
			_ = r18DumpCloser.Close()
		}
		return fmt.Errorf("failed to initialize scraper registry: %w", err)
	}
	warnings := actressOnlyPriorityWarnings(newRegistry, cfg)
	for _, warning := range warnings {
		logging.Warnf("%s", warning.Message)
	}
	cfg.Warnings = append(cfg.Warnings, warnings...)
	// Atomic publication (issue #44): swap the dump closer, publish cfg+registry,
	// rebuild the APIConfig snapshot, and invalidate the cached factories all
	// under one reloadMu.Lock so concurrent readers cannot observe a mix of
	// old/new state across the three holders. Lock order is reloadMu → CoreDeps.mu.
	// Swap the reloadables BEFORE retiring the old dump handle. Closing the
	// old closer first would leave a window where the still-active scraper
	// registry references a closed SQLite connection and dump-backed searches
	// fail. Replacing first ensures new lookups route to the new dump store
	// before the old one is released.
	old := r.deps.CoreDeps.ReplaceR18DevDumpCloser(r18DumpCloser)
	r.deps.CoreDeps.ReplaceReloadable(cfg, newRegistry)
	r.invalidateFactoriesLocked(cfg)
	if old != nil {
		_ = old.Close()
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
