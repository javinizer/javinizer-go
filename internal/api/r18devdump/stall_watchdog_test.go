package r18devdump

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
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
		wg.Add(3)
		go func() { defer wg.Done(); w.Stop() }()
		go func() {
			defer wg.Done()
			w.tryFire(time.Now(), func() { fires.Add(1) })
		}()
		go func() { defer wg.Done(); w.Ping() }()
		wg.Wait()

		assert.LessOrEqual(t, fires.Load(), int32(1), "onTimeout must run at most once")
		assert.Equal(t, fires.Load() == 1, w.Fired(), "Fired must reflect whether the fire claim won")
		assert.False(t, w.tryFire(time.Now(), func() { fires.Add(1) }),
			"after either transition claimed the watchdog, no further fire is possible")
		assert.LessOrEqual(t, fires.Load(), int32(1))
	}
}

func TestStallWatchdog_CompletedPingSuppressesFire(t *testing.T) {
	// A Ping that completes before the claim must always be observed, even
	// after the stall window elapsed before the Ping.
	w := newStallWatchdog(time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	w.Ping()
	assert.False(t, w.tryFire(time.Now(), func() {
		t.Error("fire must see a Ping that completed before the claim")
	}))
}

func TestStallWatchdog_StopDisarmsImmediatelyBeforeDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const timeout = 50 * time.Millisecond
		w := newStallWatchdog(timeout)
		fired := make(chan struct{})
		done := make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			w.run(ctx, func() { close(fired) })
			close(done)
		}()
		synctest.Wait() // Start the ticker before advancing logical time.

		// A real Sleep may resume after the deadline on a busy runner. Fake
		// time makes this genuinely the last instant before expiry.
		time.Sleep(timeout - time.Nanosecond)
		synctest.Wait()
		require.False(t, w.Fired(), "the watchdog must not fire before the deadline")
		w.Stop()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("Stop must wake the watchdog without waiting for another tick")
		}

		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		select {
		case <-fired:
			t.Fatal("stop just before the deadline must still disarm")
		default:
		}
		require.False(t, w.tryFire(time.Now(), func() {
			t.Error("Stop must also suppress an elapsed tick's timeout claim")
		}))
		require.False(t, w.Fired())
	})
}

// These tests advance logical network-idle time rather than sleeping. They
// distinguish long local setup/batch work from actual waiting on the reader.
func TestStallWatchdog_PausePreservesIdleBudget(t *testing.T) {
	const timeout = time.Hour
	w := newStallWatchdog(timeout)
	w.last = time.Now().Add(-timeout / 2)
	w.Pause()
	idle := w.idle
	w.Pause()
	require.Equal(t, idle, w.idle, "repeated pauses must not count local work")
	require.False(t, w.tryFire(time.Now().Add(10*timeout), func() {
		t.Error("local setup or batch work must not expire the idle budget")
	}))

	w.Resume()
	last := w.last
	w.Resume()
	require.Equal(t, last, w.last, "repeated resumes must not reset idle time")
	require.GreaterOrEqual(t, time.Since(last), timeout/2, "resume must preserve prior idle time")
	require.False(t, w.tryFire(last.Add(timeout-time.Nanosecond), func() {
		t.Error("the remaining budget has not expired")
	}))
	require.True(t, w.tryFire(last.Add(timeout), func() {}))
}

func TestStallWatchdog_PingDuringPauseResetsBudget(t *testing.T) {
	const timeout = time.Hour
	w := newStallWatchdog(timeout)
	w.last = time.Now().Add(-2 * timeout)
	w.Pause()
	w.Ping()
	require.Zero(t, w.idle)
	require.False(t, w.tryFire(time.Now().Add(10*timeout), func() {
		t.Error("byte progress must reset the budget even while paused")
	}))
	w.Resume()
	require.Less(t, time.Since(w.last), timeout)
}

type stallReaderFunc func([]byte) (int, error)

func (f stallReaderFunc) Read(p []byte) (int, error) { return f(p) }

func TestStallWatchdogReader_LocalWorkAndProgress(t *testing.T) {
	const timeout = time.Hour
	w := newStallWatchdog(timeout)
	reads := 0
	r := w.reader(stallReaderFunc(func(p []byte) (int, error) {
		require.False(t, w.paused, "network waits must be armed")
		w.last = w.last.Add(-timeout / 2)
		require.False(t, w.tryFire(time.Now(), func() {
			t.Error("healthy read still has an idle budget")
		}))
		w.Ping() // compressed-byte progress comes from Download's countingReader
		reads++
		p[0] = byte(reads)
		return 1, nil
	}))
	buf := make([]byte, 1)
	for i := 0; i < 4; i++ {
		require.True(t, w.paused, "local setup and each batch flush must be paused")
		require.False(t, w.tryFire(time.Now().Add(10*timeout), func() {
			t.Error("local work does not count toward the stall timeout")
		}))
		n, err := r.Read(buf)
		require.NoError(t, err)
		require.Equal(t, 1, n)
		require.Equal(t, byte(i+1), buf[0])
	}
	require.False(t, w.Fired())
}

func TestStallWatchdogReader_EmptyReadsExhaustBudget(t *testing.T) {
	const timeout = time.Hour
	w := newStallWatchdog(timeout)
	r := w.reader(stallReaderFunc(func([]byte) (int, error) {
		// Simulate network waiting without byte progress, not local work.
		w.last = w.last.Add(-timeout / 3)
		return 0, nil
	}))
	buf := make([]byte, 1)
	for i := 0; i < 3; i++ {
		n, err := r.Read(buf)
		require.NoError(t, err)
		require.Zero(t, n)
		require.True(t, w.paused)
		fired := w.tryFire(time.Now(), func() {})
		require.Equal(t, i == 2, fired, "empty reads must accumulate idle time, even if the ticker sees a pause")
	}
	require.True(t, w.Fired())
}

func TestStallWatchdogReader_BlockedReadCancels(t *testing.T) {
	const timeout = time.Hour
	w := newStallWatchdog(timeout)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reading := make(chan struct{})
	done := make(chan error, 1)
	r := w.reader(stallReaderFunc(func([]byte) (int, error) {
		close(reading)
		<-ctx.Done()
		return 0, ctx.Err()
	}))
	go func() {
		_, err := r.Read(make([]byte, 1))
		done <- err
	}()
	<-reading
	w.mu.Lock()
	deadline := w.last.Add(timeout)
	w.mu.Unlock()
	require.True(t, w.tryFire(deadline, cancel), "a blocked body read must still time out")
	require.ErrorIs(t, <-done, context.Canceled)
	require.True(t, w.Fired())
}

func TestStallWatchdogReader_StoppedCannotRearm(t *testing.T) {
	for _, fire := range []bool{false, true} {
		t.Run(fmt.Sprint("fired=", fire), func(t *testing.T) {
			w := newStallWatchdog(time.Hour)
			if fire {
				require.True(t, w.tryFire(w.last.Add(w.timeout), func() {}))
			} else {
				w.Stop() // includes permanent disarm when the stream reaches EOF
			}
			r := w.reader(stallReaderFunc(func([]byte) (int, error) { return 0, io.EOF }))
			_, err := r.Read(make([]byte, 1))
			require.ErrorIs(t, err, io.EOF)
			w.Ping()
			require.False(t, w.tryFire(time.Now().Add(10*time.Hour), func() {
				t.Error("paused/resumed reads cannot rearm a terminal watchdog")
			}))
			require.Equal(t, fire, w.Fired())
		})
	}
}

func TestStallWatchdog_PauseResumeAndFireAreAtomic(t *testing.T) {
	for i := 0; i < 1000; i++ {
		w := newStallWatchdog(time.Hour)
		w.last = time.Now().Add(-2 * time.Hour)
		var wg sync.WaitGroup
		var fires atomic.Int32
		wg.Add(3)
		go func() { defer wg.Done(); w.Pause(); w.Resume() }()
		go func() { defer wg.Done(); w.tryFire(time.Now(), func() { fires.Add(1) }) }()
		go func() { defer wg.Done(); w.Stop() }()
		wg.Wait()
		require.LessOrEqual(t, fires.Load(), int32(1))
		require.Equal(t, fires.Load() == 1, w.Fired())
		require.False(t, w.tryFire(time.Now().Add(10*time.Hour), func() { fires.Add(1) }))
	}
}

func TestStallWatchdogReader_EOFDisarmsLocalTail(t *testing.T) {
	w := newStallWatchdog(time.Hour)
	r := &eofDetectReader{
		r: w.reader(stallReaderFunc(func(p []byte) (int, error) {
			w.Ping()
			p[0] = 'x'
			return 1, io.EOF // data and EOF may arrive in the same Read
		})),
		onEOF: w.Stop,
	}
	buf := make([]byte, 1)
	n, err := r.Read(buf)
	require.Equal(t, 1, n)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, byte('x'), buf[0])
	w.Resume()
	require.False(t, w.tryFire(time.Now().Add(10*time.Hour), func() {
		t.Error("EOF must permanently protect the local commit/checkpoint tail")
	}))
	require.False(t, w.Fired())
}
