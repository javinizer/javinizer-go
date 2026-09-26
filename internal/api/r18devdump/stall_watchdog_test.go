package r18devdump

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStallWatchdog_Fires(t *testing.T) {
	fired := make(chan struct{})
	w := newStallWatchdog(60 * time.Millisecond)
	go w.run(context.Background(), func() { close(fired) })

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog should fire when no activity arrives")
	}
	assert.True(t, w.Fired())
}

func TestStallWatchdog_PingKeepsAlive(t *testing.T) {
	w := newStallWatchdog(80 * time.Millisecond)
	fired := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.run(ctx, func() { close(fired) })

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		w.Ping()
		time.Sleep(10 * time.Millisecond)
	}
	w.Stop()

	select {
	case <-fired:
		t.Fatal("watchdog fired despite regular pings")
	case <-time.After(200 * time.Millisecond):
	}
	assert.False(t, w.Fired())
}

func TestStallWatchdog_Stop(t *testing.T) {
	w := newStallWatchdog(40 * time.Millisecond)
	fired := make(chan struct{})
	go w.run(context.Background(), func() { close(fired) })
	w.Stop()
	w.Stop()

	select {
	case <-fired:
		t.Fatal("stopped watchdog must never fire")
	case <-time.After(200 * time.Millisecond):
	}
	assert.False(t, w.Fired())
}

func TestStallWatchdog_ContextCancel(t *testing.T) {
	w := newStallWatchdog(5 * time.Second)
	fired := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.run(ctx, func() { close(fired) })
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run should return when ctx is cancelled")
	}
	assert.False(t, w.Fired())
}

func TestStallWatchdog_SubTickTimeoutClamp(t *testing.T) {
	// A timeout of a few nanoseconds divides to a non-positive tick interval;
	// the clamp must kick in and the watchdog must still fire normally.
	w := newStallWatchdog(time.Nanosecond)
	fired := make(chan struct{})
	go w.run(context.Background(), func() { close(fired) })

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("clamped watchdog should still fire")
	}
	assert.True(t, w.Fired())
}

func TestStallWatchdog_StopWinsOverElapsedTick(t *testing.T) {
	w := newStallWatchdog(time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	w.Stop()

	fired := false
	if w.tryFire(time.Now(), func() { fired = true }) {
		t.Fatal("tryFire must be suppressed after Stop")
	}
	assert.False(t, fired)
	assert.False(t, w.Fired())
}

func TestStallWatchdog_FireSucceedsWhenElapsedAndArmed(t *testing.T) {
	w := newStallWatchdog(time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	fired := false
	assert.True(t, w.tryFire(time.Now(), func() { fired = true }))
	assert.True(t, fired)
	assert.True(t, w.Fired())
}

func TestStallWatchdog_StopAndFireAreAtomic(t *testing.T) {
	for i := 0; i < 2000; i++ {
		w := newStallWatchdog(time.Nanosecond)
		time.Sleep(time.Millisecond)
		var wg sync.WaitGroup
		var fires atomic.Int32
		wg.Add(2)
		go func() { defer wg.Done(); w.Stop() }()
		go func() {
			defer wg.Done()
			w.tryFire(time.Now(), func() { fires.Add(1) })
		}()
		wg.Wait()

		assert.LessOrEqual(t, fires.Load(), int32(1), "onTimeout must run at most once")
		assert.Equal(t, fires.Load() == 1, w.Fired(), "Fired must reflect whether the fire claim won")
		assert.False(t, w.tryFire(time.Now(), func() { fires.Add(1) }),
			"after either transition claimed the watchdog, no further fire is possible")
		assert.LessOrEqual(t, fires.Load(), int32(1))
	}
}

func TestStallWatchdog_StopDisarmsImmediatelyBeforeDeadline(t *testing.T) {
	w := newStallWatchdog(50 * time.Millisecond)
	fired := make(chan struct{})
	go w.run(context.Background(), func() { close(fired) })

	time.Sleep(40 * time.Millisecond)
	w.Stop()

	select {
	case <-fired:
		t.Fatal("stop just before the deadline must still disarm")
	case <-time.After(200 * time.Millisecond):
	}
	require.False(t, w.Fired())
}
