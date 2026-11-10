package testkit

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGetTestRuntimeInitializesManuallyConfiguredDeps(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{t.TempDir()}
	cfg.API.Security.MaxFilesPerScan = 42
	cfg.API.Security.ScanTimeoutSeconds = 17
	deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{}}
	deps.CoreDeps.SetConfig(cfg)

	// Manual dependencies have no runtime association yet. The fallback must
	// copy their config into the API snapshot, not leave zero-value security
	// and scan settings behind.
	rt := GetTestRuntime(deps)
	t.Cleanup(func() { CleanupTestRuntime(t, deps) })
	require.NotNil(t, rt.GetRuntime())
	require.Same(t, deps, rt.Deps())
	require.Equal(t, cfg.API.Security.AllowedDirectories, rt.GetAPIConfig().SecurityConfig().AllowedDirectories)
	require.Equal(t, cfg.API.Security.MaxFilesPerScan, rt.GetAPIConfig().ScannerConfig().MaxFilesPerScan)
	require.Equal(t, cfg.API.Security.ScanTimeoutSeconds, rt.GetAPIConfig().ScannerConfig().ScanTimeoutSeconds)
	require.Same(t, rt, GetTestRuntime(deps), "subsequent lookups reuse the initialized runtime")
}
