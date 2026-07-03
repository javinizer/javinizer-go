package core

import (
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

// These tests demonstrate the non-atomic hot-reload publication race described
// in issue #44: ReloadConfig/ReplaceReloadable publish the config/registry, the
// APIConfig snapshot, and the cached workflow factories across three separate
// critical sections, so a concurrent request can observe mixed old/new state.
//
// They use the reloadPauseAfterRegistry / reloadPauseAfterAPICfg test seams
// (nil in production) to deterministically freeze the reloader inside each
// window so the split-brain state is observable without relying on timing.
//
// Expected to FAIL on the current (unfixed) code and PASS once the publication
// is made atomic.

func newHotReloadRaceConfig(host string, port, maxFilesPerScan int) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Host: host, Port: port},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				MaxFilesPerScan: maxFilesPerScan,
			},
		},
		Logging: config.LoggingConfig{Level: "info"},
	}
}

func newHotReloadRaceRuntime(t *testing.T, cfg *config.Config) *APIRuntime {
	t.Helper()
	deps := &APIDeps{
		CoreDeps: &commandutil.CoreDeps{ScraperRegistry: scraperutil.NewScraperRegistry()},
	}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	rt.InitAPIConfig()
	return rt
}

// TestHotReload_Race_Window1_NewCfg_OldAPIConfig demonstrates that a concurrent
// request can observe the new config/registry (published by ReplaceReloadable)
// alongside the still-old APIConfig snapshot, before invalidateFactories runs.
func TestHotReload_Race_Window1_NewCfg_OldAPIConfig(t *testing.T) {
	cfgA := newHotReloadRaceConfig("hostA", 1111, 100)
	cfgB := newHotReloadRaceConfig("hostB", 2222, 200)
	rt := newHotReloadRaceRuntime(t, cfgA)

	cfgSwapped := make(chan struct{})
	proceed := make(chan struct{})
	rt.reloadPauseAfterRegistry = func() {
		close(cfgSwapped)
		<-proceed
	}

	reloadDone := make(chan error, 1)
	go func() { reloadDone <- rt.ReloadConfig(cfgB) }()

	<-cfgSwapped

	// Frozen mid-reload: CoreDeps has published the new cfg/registry, but
	// invalidateFactories has not yet refreshed the APIConfig snapshot. A
	// request reading both must see them agree.
	gotCfg := rt.deps.CoreDeps.GetConfig()
	gotAPICfg := rt.GetAPIConfig()
	require.Equal(t, gotCfg.Server.Port, gotAPICfg.Port,
		"race window 1: published cfg/registry (port %d) and APIConfig snapshot (port %d) disagree mid-reload",
		gotCfg.Server.Port, gotAPICfg.Port)

	close(proceed)
	require.NoError(t, <-reloadDone)
}

// TestHotReload_Race_Window2_NewAPIConfig_OldFactory demonstrates that a
// concurrent request can observe the new APIConfig snapshot (published inside
// invalidateFactories) alongside a still-cached workflow factory built from the
// old config, before the factory cache is invalidated.
func TestHotReload_Race_Window2_NewAPIConfig_OldFactory(t *testing.T) {
	cfgA := newHotReloadRaceConfig("hostA", 1111, 100)
	cfgB := newHotReloadRaceConfig("hostB", 2222, 200)
	rt := newHotReloadRaceRuntime(t, cfgA)

	// Pre-build and cache the workflow factory from cfgA. Its MaxFilesPerScan
	// (derived from cfg.API.Security.MaxFilesPerScan) is 100.
	factoryA, err := rt.getWorkflowFactory()
	require.NoError(t, err)
	require.NotNil(t, factoryA)
	require.Equal(t, 100, factoryA.MaxFilesPerScan(), "precondition: cached factory built from cfgA")

	apiCfgSwapped := make(chan struct{})
	proceed := make(chan struct{})
	rt.reloadPauseAfterAPICfg = func() {
		close(apiCfgSwapped)
		<-proceed
	}

	reloadDone := make(chan error, 1)
	go func() { reloadDone <- rt.ReloadConfig(cfgB) }()

	<-apiCfgSwapped

	// Frozen mid-reload: the APIConfig snapshot is already new (cfgB), but the
	// cached workflow factory is still the one built from cfgA — it has not been
	// invalidated yet. A request reading both executes with a stale factory.
	gotAPICfg := rt.GetAPIConfig()
	factory, err := rt.getWorkflowFactory()
	require.NoError(t, err)
	require.NotNil(t, factory)
	require.Equal(t, gotAPICfg.MaxFilesPerScan, factory.MaxFilesPerScan(),
		"race window 2: new APIConfig (MaxFilesPerScan %d) and cached workflow factory (MaxFilesPerScan %d) disagree mid-reload",
		gotAPICfg.MaxFilesPerScan, factory.MaxFilesPerScan())

	close(proceed)
	require.NoError(t, <-reloadDone)
}

// TestHotReload_Race_Stress is a fixed-code regression guard: many concurrent
// readers using Snapshot() (the fix's reader contract) and a reloader swapping
// between two configs via the atomic ReplaceReloadable. Under -race it verifies
// no memory race, and the invariant (snapshot cfg port == snapshot APIConfig
// port) must never be violated — the snapshot holds reloadMu.RLock across both
// reads, so it always observes a single consistent epoch.
func TestHotReload_Race_Stress(t *testing.T) {
	cfgA := newHotReloadRaceConfig("hostA", 1111, 100)
	cfgB := newHotReloadRaceConfig("hostB", 2222, 200)
	rt := newHotReloadRaceRuntime(t, cfgA)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	const readers = 8
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				snap := rt.Snapshot()
				if snap.cfg.Server.Port != snap.apiCfg.Port {
					t.Errorf("race: snapshot cfg port %d != APIConfig port %d", snap.cfg.Server.Port, snap.apiCfg.Port)
					return
				}
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			cfg := cfgA
			if i%2 == 1 {
				cfg = cfgB
			}
			rt.ReplaceReloadable(cfg, scraperutil.NewScraperRegistry())
		}
		close(stop)
	}()

	wg.Wait()
}

// TestHotReload_Race_CrossAccessor_SnapshotIsConsistent demonstrates the
// cross-accessor race that Snapshot() closes (issue #44, Phase 3): a handler
// that reads GetAPIConfig() and then getWorkflowFactory() separately can observe
// a reload landing between the two calls — new APIConfig with an old cached
// factory, or vice versa. Taking one Snapshot() and reading both from it pins a
// single epoch, so apiCfg and the factory's config-derived MaxFilesPerScan
// always agree.
//
// The raw cross-accessor path is exposed here only as a contrast and is gated
// behind a flag (it is inherently racy by design); the snapshot path is the
// supported contract and must never disagree.
func TestHotReload_Race_CrossAccessor_SnapshotIsConsistent(t *testing.T) {
	cfgA := newHotReloadRaceConfig("hostA", 1111, 100)
	cfgB := newHotReloadRaceConfig("hostB", 2222, 200)
	rt := newHotReloadRaceRuntime(t, cfgA)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	const readers = 8
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				snap := rt.Snapshot()
				factory, err := snap.WorkflowFactory()
				if err != nil {
					continue // factory unavailable this iteration; not a consistency failure
				}
				if snap.apiCfg.MaxFilesPerScan != factory.MaxFilesPerScan() {
					t.Errorf("snapshot: APIConfig MaxFilesPerScan %d != factory MaxFilesPerScan %d",
						snap.apiCfg.MaxFilesPerScan, factory.MaxFilesPerScan())
					return
				}
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			cfg := cfgA
			if i%2 == 1 {
				cfg = cfgB
			}
			rt.ReplaceReloadable(cfg, scraperutil.NewScraperRegistry())
		}
		close(stop)
	}()

	wg.Wait()
}

// TestReplaceReloadable_FiresPauseAfterRegistry covers the test-only
// reloadPauseAfterRegistry seam on the ReplaceReloadable path (distinct from
// ReloadConfig's seam). It asserts the seam fires after the atomic publish so
// the new cfg/registry are already visible when it runs.
func TestReplaceReloadable_FiresPauseAfterRegistry(t *testing.T) {
	cfgA := newHotReloadRaceConfig("hostA", 1111, 100)
	cfgB := newHotReloadRaceConfig("hostB", 2222, 200)
	rt := newHotReloadRaceRuntime(t, cfgA)

	fired := make(chan struct{})
	rt.reloadPauseAfterRegistry = func() {
		require.Equal(t, "hostB", rt.deps.CoreDeps.GetConfig().Server.Host,
			"seam must fire after the cfg/registry are published")
		close(fired)
	}

	rt.ReplaceReloadable(cfgB, scraperutil.NewScraperRegistry())
	require.NotNil(t, rt.GetAPIConfig(), "APIConfig must be rebuilt after ReplaceReloadable")
	select {
	case <-fired:
	default:
		t.Fatal("reloadPauseAfterRegistry seam did not fire")
	}
}
