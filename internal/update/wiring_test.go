package update

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// countingChecker is a stub update.Checker that records how many times the
// latest version was queried. It returns a fixed version immediately and never
// touches the network, so tests depending on it are hermetic.
type countingChecker struct {
	mu      sync.Mutex
	calls   int
	version *versionInfo
}

func (c *countingChecker) CheckLatestVersion(_ context.Context) (*versionInfo, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if c.version == nil {
		return &versionInfo{Version: "v0.0.0", TagName: "v0.0.0"}, nil
	}
	return c.version, nil
}

func (c *countingChecker) callsCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// newStubService builds a hermetic service backed by a countingChecker and a
// temp-dir cache path, so tests neither hit the network nor write to the real
// data directory.
func newStubService(t *testing.T, enabled bool) (*Service, *countingChecker) {
	t.Helper()
	chk := &countingChecker{}
	svc := NewServiceWithOptions(UpdateConfig{
		Enabled:                   enabled,
		VersionCheckIntervalHours: 24,
	}, ServiceOptions{
		Checker:   chk,
		StatePath: filepath.Join(t.TempDir(), "update_cache.json"),
	})
	return svc, chk
}

// TestNewServiceWithOptions_ZeroOptsMatchesNewService verifies the injection
// seam is backward-compatible: a zero-value ServiceOptions reproduces the
// production constructor exactly (same interval, cache path, enabled flag, and
// a non-nil real GitHub checker).
func TestNewServiceWithOptions_ZeroOptsMatchesNewService(t *testing.T) {
	cfg := UpdateConfig{Enabled: true, VersionCheckIntervalHours: 24}
	s1 := NewService(cfg)
	s2 := NewServiceWithOptions(cfg, ServiceOptions{})

	assert.Equal(t, s1.interval, s2.interval)
	assert.Equal(t, s1.statePath, s2.statePath)
	assert.Equal(t, s1.enabled, s2.enabled)
	assert.NotNil(t, s1.checker)
	assert.NotNil(t, s2.checker)
}

// TestNewServiceWithOptions_InjectsCheckerAndStatePath verifies the injected
// checker is actually used by ForceCheck and that state is written to the
// overridden temp-dir path (not the real data directory).
func TestNewServiceWithOptions_InjectsCheckerAndStatePath(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "update_cache.json")
	chk := &countingChecker{version: &versionInfo{Version: "v9.9.9", TagName: "v9.9.9"}}
	svc := NewServiceWithOptions(UpdateConfig{Enabled: true, VersionCheckIntervalHours: 24}, ServiceOptions{
		Checker:   chk,
		StatePath: statePath,
	})

	assert.Equal(t, statePath, svc.statePath)

	state, err := svc.ForceCheck(context.Background())
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "v9.9.9", state.Version)
	assert.Equal(t, 1, chk.callsCount(), "injected checker must be used")

	// State must be persisted to the overridden temp path, proving tests do
	// not touch the real on-disk cache.
	_, err = os.Stat(statePath)
	assert.NoError(t, err, "update cache written to temp StatePath")
}

// TestService_BackgroundCheck_OneShotFiresWithStub covers AC (a): the one-shot
// startup check fires and queries the injected stub, with no real network.
func TestService_BackgroundCheck_OneShotFiresWithStub(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	svc, chk := newStubService(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go svc.BackgroundCheck(ctx)

	require.Eventually(t, func() bool { return chk.callsCount() >= 1 }, time.Second, 5*time.Millisecond,
		"one-shot BackgroundCheck must query the stub")
}

// TestService_StartBackgroundCheck_StubTickerFiresOnInterval covers AC (b): the
// background ticker queries the stub on each tick. StartBackgroundCheck does
// not fire an immediate check, so every observed call corresponds to a tick.
func TestService_StartBackgroundCheck_StubTickerFiresOnInterval(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	svc, chk := newStubService(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc.StartBackgroundCheck(ctx, 30*time.Millisecond)

	require.Eventually(t, func() bool { return chk.callsCount() >= 3 }, 2*time.Second, 10*time.Millisecond,
		"background ticker must fire multiple times on interval")
}

// TestService_StartBackgroundCheck_StubStopsOnCancel covers AC (c): after the
// context is cancelled, no further checks fire and the ticker goroutine exits.
func TestService_StartBackgroundCheck_StubStopsOnCancel(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	svc, chk := newStubService(t, true)
	ctx, cancel := context.WithCancel(context.Background())

	svc.StartBackgroundCheck(ctx, 20*time.Millisecond)

	require.Eventually(t, func() bool { return chk.callsCount() >= 2 }, time.Second, 10*time.Millisecond,
		"ticker must fire before cancellation")

	callsBeforeCancel := chk.callsCount()
	cancel()

	// Wait several tick periods; no new calls must arrive after cancellation.
	time.Sleep(120 * time.Millisecond)
	assert.Equal(t, callsBeforeCancel, chk.callsCount(),
		"no further checks after context cancel")
}

// TestService_StartBackgroundCheck_StubDisabledNoCalls covers AC (d): when the
// service is disabled, StartBackgroundCheck starts no goroutine and never
// queries the checker.
func TestService_StartBackgroundCheck_StubDisabledNoCalls(t *testing.T) {
	svc, chk := newStubService(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc.StartBackgroundCheck(ctx, 20*time.Millisecond)

	time.Sleep(120 * time.Millisecond)
	assert.Equal(t, 0, chk.callsCount(),
		"disabled service must not start the background checker")
}
