package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/workflow"
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
			svc := rt.OfflineNameSync()
			require.NotNil(t, svc, "bootstrap must pin a service for the capable concrete repository even when disabled")
			select {
			case <-svc.Ready():
				t.Fatal("bootstrap must not start artifact initialization or the offline worker")
			default:
			}
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
	cfg.Matching.Extensions = []string{".mp4"}
	cfg.Matching.MinSizeMB = 0
	rt := NewAPIRuntime(&APIDeps{})
	rt.ReplaceReloadable(cfg, scraperutil.NewScraperRegistry())
	require.Nil(t, rt.GetWorkflow(), "missing movie repository must prevent scrape-only workflow construction")
	batch, err := rt.GetBatchWorkflow("fixture-job")
	require.Nil(t, batch)
	require.ErrorContains(t, err, "scraper must not be nil for full workflow")
	scan, err := rt.GetScanOnlyWorkflow()
	require.NoError(t, err)
	require.NotNil(t, scan, "scan-only remains available without the movie repository")

	dir := t.TempDir()
	path := filepath.Join(dir, "ABC-123.mp4")
	require.NoError(t, os.WriteFile(path, []byte("local scanner fixture"), 0o600))
	result, err := scan.ScanAndMatch(t.Context(), workflow.ScanAndMatchCmd{Directory: dir})
	require.NoError(t, err)
	require.Len(t, result.Files, 1)
	require.Equal(t, path, result.Files[0].Path)
	require.Equal(t, "ABC-123", result.Files[0].MovieID)
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
