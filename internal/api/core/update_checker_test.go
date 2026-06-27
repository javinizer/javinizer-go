package core

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/update"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// countingChecker is a stub update.Checker for the API bootstrap wiring tests.
// It records how many times the latest version was queried and never touches
// the network, keeping the bootstrap tests hermetic.
type countingChecker struct {
	mu      sync.Mutex
	calls   int
	version *update.VersionInfo
}

func (c *countingChecker) CheckLatestVersion(_ context.Context) (*update.VersionInfo, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if c.version == nil {
		return &update.VersionInfo{Version: "v0.0.0", TagName: "v0.0.0"}, nil
	}
	return c.version, nil
}

func (c *countingChecker) callsCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// newCheckerConfig builds a config with the given version-check flag. The
// interval is set to 24h so the bootstrap's ticker (derived from config) never
// fires during a test — only the one-shot startup check is observable, which
// is exactly the wiring these tests assert.
func newCheckerConfig(enabled bool) *config.Config {
	cfg := &config.Config{}
	cfg.System.VersionCheckEnabled = enabled
	cfg.System.VersionCheckIntervalHours = 24
	return cfg
}

// newCheckerRuntime returns a minimal APIRuntime suitable for exercising
// startUpdateChecker. startUpdateChecker only touches rt.ServerCtx() (and
// rt.Shutdown() cancels it), so a bare APIDeps is sufficient and avoids
// standing up the full database/scraper stack.
func newCheckerRuntime(t *testing.T) *APIRuntime {
	t.Helper()
	return NewAPIRuntime(&APIDeps{})
}

// stubOpts returns ServiceOptions that inject the stub checker and isolate the
// on-disk cache to a temp dir, so the bootstrap never hits the network or the
// real data directory.
func stubOpts(t *testing.T, chk *countingChecker) update.ServiceOptions {
	t.Helper()
	return update.ServiceOptions{
		Checker:   chk,
		StatePath: filepath.Join(t.TempDir(), "update_cache.json"),
	}
}

// TestStartUpdateChecker_Disabled_DoesNothing covers AC (d) at the bootstrap
// seam: a disabled config starts no goroutine and never queries the checker.
func TestStartUpdateChecker_Disabled_DoesNothing(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	chk := &countingChecker{}
	rt := newCheckerRuntime(t)
	defer rt.Shutdown()

	startUpdateChecker(rt, newCheckerConfig(false), stubOpts(t, chk))

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 0, chk.callsCount(),
		"disabled config must not start the update checker")
}

// TestStartUpdateChecker_Enabled_FiresOneShotAndStopsOnShutdown covers ACs (a)
// and (c) at the bootstrap seam: with version checks enabled, the one-shot
// startup check fires immediately (warming the cache), and rt.Shutdown()
// cancels the server context so the background ticker stops cleanly.
func TestStartUpdateChecker_Enabled_FiresOneShotAndStopsOnShutdown(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	chk := &countingChecker{}
	rt := newCheckerRuntime(t)

	startUpdateChecker(rt, newCheckerConfig(true), stubOpts(t, chk))

	// The one-shot check is dispatched at startup and must query the stub
	// before the first GetStatus read would otherwise hit a nil cache.
	require.Eventually(t, func() bool { return chk.callsCount() >= 1 }, time.Second, 5*time.Millisecond,
		"one-shot startup check must fire when version checks are enabled")

	callsBeforeShutdown := chk.callsCount()
	rt.Shutdown()

	// After shutdown the server context is cancelled, so the ticker (24h
	// interval) must not produce any further checks.
	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, callsBeforeShutdown, chk.callsCount(),
		"rt.Shutdown must stop the background update checker")
}

// TestStartUpdateChecker_NilConfigIsSafe asserts the bootstrap gate is nil-safe
// so a misconfigured caller cannot panic startup.
func TestStartUpdateChecker_NilConfigIsSafe(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	chk := &countingChecker{}
	rt := newCheckerRuntime(t)
	defer rt.Shutdown()

	assert.NotPanics(t, func() {
		startUpdateChecker(rt, nil, stubOpts(t, chk))
	})
	assert.Equal(t, 0, chk.callsCount())
}
