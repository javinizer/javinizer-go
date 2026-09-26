package r18devdump

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// dumpStallTimeout is the maximum time the dump download may go without
// receiving bytes before the watchdog aborts it. Zero bytes for 5 minutes is
// a reliable dead-connection signal regardless of total transfer duration.
const dumpStallTimeout = 5 * time.Minute

// stallWatchdog aborts the dump download when no bytes arrive within timeout.
// The transfer duration is unbounded (dump size x network speed), so an
// absolute deadline kills slow-but-healthy downloads; only stalled progress
// proves failure. Ping is fed by the download progress callback.
//
// last, stopped, and the fire claim all serialize on mu: Stop, Ping, and
// tryFire are atomic with respect to each other, so activity arriving at the
// stall boundary is always accounted for, and a watchdog disarmed at stream
// EOF can never cancel the local import tail. last holds monotonic time.Time
// values (never UnixNano) so host clock jumps cannot fabricate or extend
// stalls. stop exists only to wake run; it is closed exactly once, under mu.
type stallWatchdog struct {
	timeout time.Duration
	mu      sync.Mutex
	last    time.Time
	stopped bool
	stop    chan struct{}
	fired   atomic.Bool
}

func newStallWatchdog(timeout time.Duration) *stallWatchdog {
	return &stallWatchdog{timeout: timeout, last: time.Now(), stop: make(chan struct{})}
}

func (w *stallWatchdog) Ping() {
	w.mu.Lock()
	w.last = time.Now()
	w.mu.Unlock()
}

func (w *stallWatchdog) Stop() {
	w.mu.Lock()
	if !w.stopped {
		w.stopped = true
		close(w.stop)
	}
	w.mu.Unlock()
}

func (w *stallWatchdog) Fired() bool { return w.fired.Load() }

// tryFire claims a timeout at now: if the watchdog has seen no activity for
// at least timeout and is not already stopped, it transitions to stopped and
// invokes onTimeout. The elapsed check runs under mu, so a Ping that won the
// lock first is always observed. onTimeout runs outside mu; only the claim
// is serialized.
func (w *stallWatchdog) tryFire(now time.Time, onTimeout func()) bool {
	w.mu.Lock()
	if w.stopped || now.Sub(w.last) < w.timeout {
		w.mu.Unlock()
		return false
	}
	w.stopped = true
	close(w.stop)
	w.mu.Unlock()
	w.fired.Store(true)
	onTimeout()
	return true
}

func (w *stallWatchdog) run(ctx context.Context, onTimeout func()) {
	interval := w.timeout / 4
	if interval <= 0 {
		interval = time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-ctx.Done():
			return
		case now := <-t.C:
			if w.tryFire(now, onTimeout) {
				return
			}
		}
	}
}
