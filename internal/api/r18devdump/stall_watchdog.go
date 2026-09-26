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
type stallWatchdog struct {
	timeout time.Duration
	last    atomic.Int64
	mu      sync.Mutex
	stopped bool
	stop    chan struct{}
	fired   atomic.Bool
}

func newStallWatchdog(timeout time.Duration) *stallWatchdog {
	w := &stallWatchdog{timeout: timeout, stop: make(chan struct{})}
	w.last.Store(time.Now().UnixNano())
	return w
}

func (w *stallWatchdog) Ping() { w.last.Store(time.Now().UnixNano()) }

// Stop disarms the watchdog permanently. The stopped claim and the fire claim
// (tryFire) serialize on mu, so once either transition wins, the other can
// never observe the pre-claim state — Stop and onTimeout are atomic with
// respect to each other and each happens at most once.
func (w *stallWatchdog) Stop() {
	w.mu.Lock()
	if !w.stopped {
		w.stopped = true
		close(w.stop)
	}
	w.mu.Unlock()
}

func (w *stallWatchdog) Fired() bool { return w.fired.Load() }

// tryFire claims a timeout at now: if the watchdog has been stalled for at
// least timeout and was not already stopped or fired, it transitions to
// stopped and invokes onTimeout. The claim is atomic with Stop, so a watchdog
// disarmed at stream EOF can never abort the local import tail. onTimeout
// runs outside mu; only the claim is serialized.
func (w *stallWatchdog) tryFire(now time.Time, onTimeout func()) bool {
	if now.Sub(time.Unix(0, w.last.Load())) < w.timeout {
		return false
	}
	w.mu.Lock()
	if w.stopped {
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
