package r18devdump

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// dumpStallTimeout is the maximum time the dump download may go without
// receiving bytes while waiting on the network before the watchdog aborts it.
// Local SQLite work is excluded from this idle budget.
const dumpStallTimeout = 5 * time.Minute

// stallWatchdog aborts the dump download when no bytes arrive within timeout.
// The transfer duration is unbounded (dump size x network speed), so an
// absolute deadline kills slow-but-healthy downloads; only stalled progress
// proves failure. Ping is fed by the download progress callback. Pause and
// Resume exclude local importer work while preserving accumulated network idle
// time; only actual byte progress resets that budget.
//
// last, paused, stopped, and the fire claim all serialize on mu: Stop, Ping, and
// tryFire are atomic with respect to each other, so activity arriving at the
// stall boundary is always accounted for, and a watchdog disarmed at stream
// EOF can never cancel the local import tail. last holds monotonic time.Time
// values (never UnixNano) so host clock jumps cannot fabricate or extend
// stalls. stop exists only to wake run; it is closed exactly once, under mu.
type stallWatchdog struct {
	timeout time.Duration
	mu      sync.Mutex
	last    time.Time
	paused  bool
	idle    time.Duration // accumulated network idle time at Pause
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
	w.idle = 0
	w.mu.Unlock()
}

// Pause excludes local work from the stall budget. It is idempotent and
// serialized with timeout claims, just like Stop.
func (w *stallWatchdog) Pause() {
	w.mu.Lock()
	if !w.stopped && !w.paused {
		w.idle = time.Since(w.last)
		w.paused = true
	}
	w.mu.Unlock()
}

// Resume continues the existing idle budget; it does not grant a fresh
// timeout to readers that repeatedly return no bytes.
func (w *stallWatchdog) Resume() {
	w.mu.Lock()
	if !w.stopped && w.paused {
		w.last = time.Now().Add(-w.idle)
		w.paused = false
	}
	w.mu.Unlock()
}

// reader pauses before importer setup, then watches only Read calls. SQLite
// setup, batch flushes, and other work between reads cannot stall the network.
func (w *stallWatchdog) reader(r io.Reader) io.Reader {
	w.Pause()
	return &stallWatchdogReader{r: r, watchdog: w}
}

type stallWatchdogReader struct {
	r        io.Reader
	watchdog *stallWatchdog
}

func (r *stallWatchdogReader) Read(p []byte) (int, error) {
	r.watchdog.Resume()
	defer r.watchdog.Pause()
	return r.r.Read(p)
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
// invokes onTimeout. While paused, only idle time already spent waiting on
// reads counts; an exhausted budget stays expired so repeated empty reads
// cannot evade the ticker by pausing between checks. The elapsed check runs
// under mu, so a Ping that won the lock first is always observed. onTimeout
// runs outside mu; only the claim is serialized.
func (w *stallWatchdog) tryFire(now time.Time, onTimeout func()) bool {
	w.mu.Lock()
	idle := now.Sub(w.last)
	if w.paused {
		idle = w.idle
	}
	if w.stopped || idle < w.timeout {
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
