package core

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSnapshotTestRuntime builds an APIRuntime wired with enough repos for the
// workflow factory to construct (matcher/scanner/organizer paths), so the
// snapshot factory accessors exercise a real factory rather than the nil path.
func newSnapshotTestRuntime(t *testing.T, cfg *config.Config) *APIRuntime {
	t.Helper()
	deps := &APIDeps{
		CoreDeps: &commandutil.CoreDeps{ScraperRegistry: scraperutil.NewScraperRegistry()},
		Repos: database.Repositories{
			ContentRepos: database.ContentRepos{
				ContentIDMappingRepo: &database.ContentIDMappingRepository{},
			},
		},
	}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	rt.InitAPIConfig()
	return rt
}

func snapshotTestConfig(port, maxFiles int, regex string) *config.Config {
	return &config.Config{
		Server:  config.ServerConfig{Host: "snaphost", Port: port},
		Logging: config.LoggingConfig{Level: "info"},
		API: config.APIConfig{
			Security: config.SecurityConfig{MaxFilesPerScan: maxFiles},
		},
		Matching: config.MatchingConfig{RegexEnabled: true, RegexPattern: regex},
	}
}

// TestSnapshot_Accessors_ReturnCapturedState verifies the trivial snapshot
// accessors return the values captured at Snapshot() time, not a fresher
// CoreDeps after a reload.
func TestSnapshot_Accessors_ReturnCapturedState(t *testing.T) {
	cfgA := snapshotTestConfig(1111, 100, `([A-Z]+-\d+)`)
	cfgB := snapshotTestConfig(2222, 200, `([A-Z]+-\d+)`)
	rt := newSnapshotTestRuntime(t, cfgA)

	snap := rt.Snapshot()
	assert.Equal(t, cfgA, snap.Config())
	assert.Equal(t, 1111, snap.APIConfig().Port)
	assert.Equal(t, 100, snap.APIConfig().MaxFilesPerScan)
	assert.Equal(t, rt.deps.CoreDeps.GetRegistry(), snap.Registry())
	assert.Same(t, rt, snap.RT())

	// Reload after snapshot; snap must still reflect cfgA.
	rt.ReplaceReloadable(cfgB, scraperutil.NewScraperRegistry())
	assert.Equal(t, cfgA, snap.Config(), "snapshot must pin the captured config")
	assert.Equal(t, 1111, snap.APIConfig().Port, "snapshot must pin the captured apiCfg")
}

// TestSnapshot_WorkflowFactory_SameEpoch_ReusesCache confirms the same-epoch
// path returns the shared cached factory (pointer identity) and that repeated
// calls return the same instance.
func TestSnapshot_WorkflowFactory_SameEpoch_ReusesCache(t *testing.T) {
	cfg := snapshotTestConfig(1111, 100, `([A-Z]+-\d+)`)
	rt := newSnapshotTestRuntime(t, cfg)

	snap := rt.Snapshot()
	f1, err := snap.WorkflowFactory()
	require.NoError(t, err)
	require.NotNil(t, f1)

	// Same snapshot again → same pointer (cache hit on the same epoch).
	f2, err := snap.WorkflowFactory()
	require.NoError(t, err)
	assert.Same(t, f1, f2)

	// The shared lazy cache (reached via the legacy accessor) is the same
	// instance the snapshot served on the same-epoch path.
	f3, err := rt.getWorkflowFactory()
	require.NoError(t, err)
	assert.Same(t, f1, f3)
}

// TestSnapshot_WorkflowFactory_StaleEpoch_BuildsFreshUncached confirms that
// after a reload the snapshot builds a fresh factory from its captured
// cfg/registry (not the shared cache, which now holds a newer factory). The
// stale-epoch build is intentionally uncached, so each call yields a distinct
// instance — that is the documented behavior that prevents a stale-epoch build
// from poisoning the newer shared cache.
func TestSnapshot_WorkflowFactory_StaleEpoch_BuildsFreshUncached(t *testing.T) {
	cfgA := snapshotTestConfig(1111, 100, `([A-Z]+-\d+)`)
	cfgB := snapshotTestConfig(2222, 200, `([A-Z]+-\d+)`)
	rt := newSnapshotTestRuntime(t, cfgA)

	snap := rt.Snapshot()

	// Reload to a new epoch; the shared cache is invalidated and rebuilt.
	rt.ReplaceReloadable(cfgB, scraperutil.NewScraperRegistry())
	newer, err := rt.getWorkflowFactory()
	require.NoError(t, err)

	// The stale snapshot's factory differs from the post-reload shared cache.
	stale, err := snap.WorkflowFactory()
	require.NoError(t, err)
	assert.NotSame(t, newer, stale, "stale snapshot must not serve the newer cached factory")

	// The stale-epoch build is uncached: a second call yields a fresh instance,
	// not the previously-built one (otherwise it would poison the shared cache).
	stale2, err := snap.WorkflowFactory()
	require.NoError(t, err)
	assert.NotSame(t, stale, stale2, "stale-epoch build must not be cached")
}

// TestSnapshot_PosterGen_DelegatesToWorkflowFactory confirms PosterGen returns
// the factory's PosterGenerator on the same-epoch path and nil when the
// factory is unavailable.
func TestSnapshot_PosterGen_DelegatesToWorkflowFactory(t *testing.T) {
	cfg := snapshotTestConfig(1111, 100, `([A-Z]+-\d+)`)
	rt := newSnapshotTestRuntime(t, cfg)

	snap := rt.Snapshot()
	f, err := snap.WorkflowFactory()
	require.NoError(t, err)

	pg := snap.PosterGen()
	if f.PosterGen() != nil {
		assert.NotNil(t, pg)
	} else {
		assert.Nil(t, pg)
	}
}

// TestSnapshot_Matcher_BuiltFromAPIConfig confirms the matcher is built from
// the snapshot's apiCfg and is non-nil for a valid regex.
func TestSnapshot_Matcher_BuiltFromAPIConfig(t *testing.T) {
	cfg := snapshotTestConfig(1111, 100, `([A-Z]+-\d+)`)
	rt := newSnapshotTestRuntime(t, cfg)

	snap := rt.Snapshot()
	m := snap.Matcher()
	require.NotNil(t, m)
	assert.Equal(t, "ABC-123", m.MatchString("ABC-123.mp4"))
}

// TestSnapshot_Matcher_InvalidRegexReturnsNil confirms a bad regex yields nil
// rather than panicking.
func TestSnapshot_Matcher_InvalidRegexReturnsNil(t *testing.T) {
	cfg := snapshotTestConfig(1111, 100, `(unclosed[`)
	rt := newSnapshotTestRuntime(t, cfg)

	snap := rt.Snapshot()
	assert.Nil(t, snap.Matcher())
}

// TestSnapshot_BatchJobFactory_StaleEpoch_BuildsFreshWithoutCaching confirms a
// stale-epoch snapshot builds a fresh factory from its captured apiCfg/PosterGen
// without calling SetReconstructionDeps (which would mutate shared state). The
// stale-epoch fresh build is uncached: each call yields a distinct instance.
// This requires a primed workflow factory (for PosterGen); the shared
// batchJobFactory cache is left untouched (still empty for the new epoch).
func TestSnapshot_BatchJobFactory_StaleEpoch_BuildsFreshWithoutCaching(t *testing.T) {
	cfgA := snapshotTestConfig(1111, 100, `([A-Z]+-\d+)`)
	cfgB := snapshotTestConfig(2222, 200, `([A-Z]+-\d+)`)
	rt := newSnapshotTestRuntime(t, cfgA)

	// Prime the workflow factory so PosterGen resolves for a stale-epoch build.
	_, err := rt.getWorkflowFactory()
	require.NoError(t, err)
	snap := rt.Snapshot()

	// Reload to cfgB (new epoch). The shared batch cache is invalidated.
	rt.ReplaceReloadable(cfgB, scraperutil.NewScraperRegistry())

	// Stale snapshot builds fresh without panicking.
	f1 := snap.BatchJobFactory()
	// PosterGen may be nil for the stale build if the workflow factory isn't
	// primed for cfgA; if so the build returns nil gracefully (covered).
	if f1 != nil {
		f2 := snap.BatchJobFactory()
		assert.NotSame(t, f1, f2, "stale-epoch build must not be cached")
	}
	// The shared cache for the new epoch is untouched (nil/empty), proving the
	// stale build did not poison it.
	assert.Nil(t, rt.batchJobFactory.value, "stale-epoch build must not write the shared cache")
}

// TestSnapshot_PosterManager_CurrentEpoch_RoutesThroughCache confirms the
// current-epoch path routes through the shared RuntimeState cache (same
// instance across calls) and is non-nil when config is present.
func TestSnapshot_PosterManager_CurrentEpoch_RoutesThroughCache(t *testing.T) {
	cfg := snapshotTestConfig(1111, 100, `([A-Z]+-\d+)`)
	rt := newSnapshotTestRuntime(t, cfg)
	rt.EnsureRuntime()

	snap := rt.Snapshot()
	pm1 := snap.PosterManager()
	require.NotNil(t, pm1)
	// Same epoch → shared cache → same instance.
	pm2 := snap.PosterManager()
	assert.Same(t, pm1, pm2)
}

// TestSnapshot_PosterManager_StaleEpoch_BuildsUncached confirms a stale
// snapshot does not reuse the shared cache (which may hold a manager from a
// newer epoch with a different TempDir).
func TestSnapshot_PosterManager_StaleEpoch_BuildsUncached(t *testing.T) {
	cfgA := snapshotTestConfig(1111, 100, `([A-Z]+-\d+)`)
	cfgA.System.TempDir = "/tmp/snapA"
	cfgB := snapshotTestConfig(2222, 200, `([A-Z]+-\d+)`)
	cfgB.System.TempDir = "/tmp/snapB"
	rt := newSnapshotTestRuntime(t, cfgA)
	rt.EnsureRuntime()

	snap := rt.Snapshot()
	pmCurrent := snap.PosterManager()
	require.NotNil(t, pmCurrent)

	// Reload to cfgB (new TempDir). The shared cache is invalidated.
	rt.ReplaceReloadable(cfgB, scraperutil.NewScraperRegistry())

	// Stale snapshot must build an uncached manager (not the post-reload
	// shared-cache one). It should be non-nil and a distinct instance.
	pmStale := snap.PosterManager()
	require.NotNil(t, pmStale)
	assert.NotSame(t, pmCurrent, pmStale, "stale snapshot must not serve the shared-cache instance")
}

// TestSnapshot_PosterManager_NilConfigReturnsNil covers the NewSnapshotForTesting
// nil-cfg guard.
func TestSnapshot_PosterManager_NilConfigReturnsNil(t *testing.T) {
	rt := NewAPIRuntime(&APIDeps{})
	snap := NewSnapshotForTesting(rt, APIConfig{})
	assert.Nil(t, snap.PosterManager())
}

// TestNewSnapshotForTesting covers the test-only constructor.
func TestNewSnapshotForTesting(t *testing.T) {
	rt := NewAPIRuntime(&APIDeps{})
	apiCfg := APIConfig{Host: "t", Port: 9}
	snap := NewSnapshotForTesting(rt, apiCfg)
	assert.Same(t, rt, snap.RT())
	assert.Equal(t, "t", snap.APIConfig().Host)
	assert.Equal(t, 9, snap.APIConfig().Port)
	assert.Nil(t, snap.Config())
	assert.Nil(t, snap.Registry())
}

// TestSnapshot_BatchWorkflow_And_ScanOnlyWorkflow cover the two workflow
// construction accessors on the same-epoch path.
func TestSnapshot_BatchWorkflow_And_ScanOnlyWorkflow(t *testing.T) {
	cfg := snapshotTestConfig(1111, 100, `([A-Z]+-\d+)`)
	rt := newSnapshotTestRuntime(t, cfg)

	snap := rt.Snapshot()
	// BatchWorkflow needs a scraper (nil here without a MovieRepo) so it returns
	// an error; scan-only workflows don't, so they succeed. Assert both paths.
	_, err := snap.BatchWorkflow("job-1")
	assert.Error(t, err, "batch workflow needs a scraper; nil-repo setup must error")

	scan, err := snap.ScanOnlyWorkflow()
	require.NoError(t, err)
	require.NotNil(t, scan)
}

// badRegexSnapshotConfig returns a config whose matching regex is invalid, so
// the workflow factory cannot be built (buildMatcher fails with a non-Scraper
// construction error → buildWorkflowFactoryFrom returns nil). This drives the
// factory-unavailable error/nil branches in the snapshot accessors.
func badRegexSnapshotConfig() *config.Config {
	cfg := snapshotTestConfig(1111, 100, `(unclosed[`)
	return cfg
}

// TestSnapshot_WorkflowFactory_SameEpoch_FactoryUnavailable covers the
// same-epoch path where the shared cache build returns nil (bad regex) →
// WorkflowFactory returns an error rather than a factory.
func TestSnapshot_WorkflowFactory_SameEpoch_FactoryUnavailable(t *testing.T) {
	rt := newSnapshotTestRuntime(t, badRegexSnapshotConfig())
	snap := rt.Snapshot()

	f, err := snap.WorkflowFactory()
	assert.Nil(t, f)
	assert.Error(t, err)

	// PosterGen delegates to WorkflowFactory → nil on the error path.
	assert.Nil(t, snap.PosterGen())

	// BatchWorkflow / ScanOnlyWorkflow propagate the WorkflowFactory error.
	_, err = snap.BatchWorkflow("job-1")
	assert.Error(t, err)
	_, err = snap.ScanOnlyWorkflow()
	assert.Error(t, err)
}

// TestSnapshot_WorkflowFactory_StaleEpoch_FactoryUnavailable covers the
// stale-epoch path where buildWorkflowFactoryFrom(s.cfg, ...) returns nil →
// WorkflowFactory returns an error.
func TestSnapshot_WorkflowFactory_StaleEpoch_FactoryUnavailable(t *testing.T) {
	cfgBad := badRegexSnapshotConfig()
	cfgGood := snapshotTestConfig(2222, 200, `([A-Z]+-\d+)`)
	rt := newSnapshotTestRuntime(t, cfgBad)

	snap := rt.Snapshot()
	// Reload to a good-regex epoch so the snapshot is stale.
	rt.ReplaceReloadable(cfgGood, scraperutil.NewScraperRegistry())

	f, err := snap.WorkflowFactory()
	assert.Nil(t, f, "stale snapshot with bad regex must not build a factory")
	assert.Error(t, err)
}

// TestSnapshot_BatchJobFactory_StaleEpoch_PosterGenNil covers the stale-epoch
// BatchJobFactory path where PosterGen is nil (workflow factory unavailable for
// the snapshot's captured cfg) → returns nil without panicking and without
// touching the shared cache.
func TestSnapshot_BatchJobFactory_StaleEpoch_PosterGenNil(t *testing.T) {
	cfgBad := badRegexSnapshotConfig()
	cfgGood := snapshotTestConfig(2222, 200, `([A-Z]+-\d+)`)
	rt := newSnapshotTestRuntime(t, cfgBad)

	snap := rt.Snapshot()
	rt.ReplaceReloadable(cfgGood, scraperutil.NewScraperRegistry())

	// Stale snapshot's captured cfg has a bad regex → PosterGen nil →
	// BatchJobFactory returns nil on the stale-epoch fresh-build path.
	assert.Nil(t, snap.BatchJobFactory())
	// Shared cache for the new (good) epoch is untouched by the stale build.
	assert.Nil(t, rt.batchJobFactory.value, "stale-epoch build must not write the shared cache")
}

// TestSnapshot_BatchJobFactory_SameEpoch_FactoryUnavailable covers the
// same-epoch path where the shared cache build returns nil (bad regex →
// PosterGen nil → buildBatchJobFactory returns nil before touching the
// JobStore) → BatchJobFactory returns nil without panicking.
func TestSnapshot_BatchJobFactory_SameEpoch_FactoryUnavailable(t *testing.T) {
	rt := newSnapshotTestRuntime(t, badRegexSnapshotConfig())
	snap := rt.Snapshot()
	// sameEpoch=true; cache build returns nil (PosterGen nil) → returns nil.
	assert.Nil(t, snap.BatchJobFactory())
}

// TestSnapshot_PosterManager_NoRuntimeState covers the rs == nil branch: when
// the runtime has no RuntimeState, PosterManager builds directly from snap.cfg
// without routing through the shared cache.
func TestSnapshot_PosterManager_NoRuntimeState(t *testing.T) {
	cfg := snapshotTestConfig(1111, 100, `([A-Z]+-\d+)`)
	cfg.System.TempDir = "/tmp/snapNoRT"
	rt := newSnapshotTestRuntime(t, cfg)
	// Intentionally do NOT call EnsureRuntime → rs is nil.
	snap := rt.Snapshot()
	pm := snap.PosterManager()
	require.NotNil(t, pm)
}
