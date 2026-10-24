package core

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestBootstrapAPIWithOptsValidationAndWiring(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		deps, rt, err := BootstrapAPIWithOpts(nil, "", nil, &commandutil.DependenciesOptions{})
		require.ErrorContains(t, err, "config cannot be nil")
		require.Nil(t, deps)
		require.Nil(t, rt)
	})
	t.Run("dependency construction error", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		cfg.Database.Type = "unsupported"
		cfg.Database.DSN = ":memory:"
		deps, rt, err := BootstrapAPIWithOpts(cfg, "", nil, &commandutil.DependenciesOptions{})
		require.ErrorContains(t, err, "failed to initialize database")
		require.Nil(t, deps)
		require.Nil(t, rt)
	})
	for _, injected := range []bool{false, true} {
		name := "default dependencies"
		if injected {
			name = "injected scraper registry"
		}
		t.Run(name, func(t *testing.T) {
			cfg := config.DefaultConfig(nil, nil)
			cfg.Database.DSN = ":memory:"
			cfg.System.TempDir = t.TempDir()
			cfg.System.VersionCheckEnabled = false
			var opts *commandutil.DependenciesOptions
			registry := scraperutil.NewScraperRegistry()
			if injected {
				opts = &commandutil.DependenciesOptions{ScraperRegistry: registry}
			}
			deps, rt, err := BootstrapAPIWithOpts(cfg, "", nil, opts)
			require.NoError(t, err)
			require.NotNil(t, deps)
			require.NotNil(t, rt)
			t.Cleanup(func() { rt.Shutdown(); require.NoError(t, deps.CoreDeps.Close()) })
			require.Same(t, cfg, deps.CoreDeps.GetConfig())
			require.NotNil(t, deps.JobStore)
			require.NotNil(t, deps.TokenStore)
			require.NotNil(t, deps.Repos.MovieRepo)
			if injected {
				require.Same(t, registry, deps.CoreDeps.GetRegistry())
			}
			require.Nil(t, rt.updateCheckerDone, "disabled version checking must not start outbound work")
		})
	}
}
func TestRuntimeWorkflowConstructionFailureIsReported(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = "["
	rt := NewAPIRuntime(&APIDeps{})
	rt.ReplaceReloadable(cfg, scraperutil.NewScraperRegistry())
	require.Nil(t, rt.GetWorkflow())
	require.Nil(t, rt.NewMatcher())
	require.Nil(t, rt.GetPosterGen())
	wf, err := rt.GetBatchWorkflow("job")
	require.Nil(t, wf)
	require.ErrorContains(t, err, "workflow factory not available")
	wf, err = rt.GetScanOnlyWorkflow()
	require.Nil(t, wf)
	require.ErrorContains(t, err, "workflow factory not available")
}
func TestRuntimePartialWiringKeepsScanAvailable(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	rt := NewAPIRuntime(&APIDeps{})
	rt.ReplaceReloadable(cfg, scraperutil.NewScraperRegistry())
	require.Nil(t, rt.GetWorkflow(), "missing movie repository must prevent scrape-only workflow construction")
	scan, err := rt.GetScanOnlyWorkflow()
	require.NoError(t, err)
	require.NotNil(t, scan, "scan-only remains available without the movie repository")
}
func TestRuntimePosterManagerConstructionFailureIsNotCached(t *testing.T) {
	state := NewRuntimeState()
	calls := 0
	for i := 0; i < 2; i++ {
		require.Nil(t, state.GetPosterManager(func() poster.PosterManagerInterface { calls++; return nil }))
	}
	require.Equal(t, 2, calls, "failed construction must be retried rather than cached")
}
func TestRuntimeShutdownReturnsWhenHubDoesNotExit(t *testing.T) {
	state := NewRuntimeState()
	cancelled := false
	state.wsHubCancel = func() { cancelled = true }
	state.wsHubShutdown = make(chan struct{})
	started := time.Now()
	state.Shutdown()
	require.True(t, cancelled)
	require.Nil(t, state.wsHubCancel)
	require.Nil(t, state.wsHubShutdown)
	elapsed := time.Since(started)
	require.GreaterOrEqual(t, elapsed, 450*time.Millisecond, "shutdown must wait for the hub before abandoning it")
	require.Less(t, elapsed, 3*time.Second, "a stuck hub must not block shutdown indefinitely")
	state.Shutdown()
}
