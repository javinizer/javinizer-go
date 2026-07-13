package core

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPIRuntime_WithReloadLock_RunsFn covers the method body: fn is invoked
// and its return value (including errors) is propagated.
func TestAPIRuntime_WithReloadLock_RunsFn(t *testing.T) {
	rt := newHotReloadRaceRuntime(t, newHotReloadRaceConfig("host", 1, 10))

	called := false
	err := rt.WithReloadLock(func() error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)

	want := errors.New("boom")
	got := rt.WithReloadLock(func() error {
		return want
	})
	assert.Same(t, want, got)
}

// TestAPIRuntime_WithReloadLock_SerializesReload proves the lock is held for
// writing: a concurrent ReloadConfig (which acquires reloadMu) cannot proceed
// until fn returns, and completes once the lock is released.
func TestAPIRuntime_WithReloadLock_SerializesReload(t *testing.T) {
	cfg := newHotReloadRaceConfig("host", 1, 10)
	rt := newHotReloadRaceRuntime(t, cfg)

	hold := make(chan struct{})
	release := make(chan struct{})
	fnDone := make(chan struct{})

	go func() {
		_ = rt.WithReloadLock(func() error {
			close(hold)
			<-release
			return nil
		})
		close(fnDone)
	}()

	<-hold

	// Snapshot uses reloadMu.RLock; it must block while fn holds the write lock.
	snapDone := make(chan struct{})
	go func() {
		_ = rt.Snapshot()
		close(snapDone)
	}()
	select {
	case <-snapDone:
		t.Fatal("Snapshot should block while WithReloadLock holds reloadMu")
	case <-time.After(80 * time.Millisecond):
	}

	// ReloadConfig acquires reloadMu.Lock; it must also block.
	var reloadStarted atomic.Bool
	reloadDone := make(chan error, 1)
	go func() {
		reloadStarted.Store(true)
		reloadDone <- rt.ReloadConfig(cfg)
	}()
	// Let the goroutine reach reloadMu.Lock().
	time.Sleep(50 * time.Millisecond)
	select {
	case err := <-reloadDone:
		t.Fatalf("ReloadConfig should block under WithReloadLock, got %v", err)
	case <-time.After(100 * time.Millisecond):
		// expected: still blocked
	}

	close(release)
	<-fnDone

	select {
	case err := <-reloadDone:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("ReloadConfig did not complete after WithReloadLock released")
	}
	select {
	case <-snapDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Snapshot did not complete after WithReloadLock released")
	}
	assert.True(t, reloadStarted.Load())
}

func TestAPIRuntime_ReloadConfigLocked(t *testing.T) {
	cfg := newHotReloadRaceConfig("host", 1, 10)
	rt := newHotReloadRaceRuntime(t, cfg)
	unlock := rt.LockReload()

	require.NoError(t, rt.ReloadConfigLocked(cfg))
	assert.EqualError(t, rt.ReloadConfigLocked(nil), "ReloadConfig: config is nil")

	unlock()
}

func TestAPIRuntime_PrepareReloadRejectsNilResolver(t *testing.T) {
	rt := newHotReloadRaceRuntime(t, newHotReloadRaceConfig("host", 1, 10))

	err := rt.prepareReload(&config.Config{}, nil)

	assert.EqualError(t, err, "failed to finalize scraper config: scrapers: Finalize called with nil resolver")
}

func TestAPIRuntime_LockReload_SerializesSnapshot(t *testing.T) {
	rt := newHotReloadRaceRuntime(t, newHotReloadRaceConfig("host", 1, 10))
	unlock := rt.LockReload()

	snapDone := make(chan struct{})
	go func() {
		_ = rt.Snapshot()
		close(snapDone)
	}()

	select {
	case <-snapDone:
		t.Fatal("Snapshot should block while LockReload holds reloadMu")
	case <-time.After(80 * time.Millisecond):
	}

	unlock()
	select {
	case <-snapDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Snapshot did not complete after LockReload released")
	}
}
