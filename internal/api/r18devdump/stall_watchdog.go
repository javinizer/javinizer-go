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
	stop    chan struct{}
	once    sync.Once
	fired   atomic.Bool
}

func newStallWatchdog(timeout time.Duration) *stallWatchdog {
	w := &stallWatchdog{timeout: timeout, stop: make(chan struct{})}
	w.last.Store(time.Now().UnixNano())
	return w
}

func (w *stallWatchdog) Ping() { w.last.Store(time.Now().UnixNano()) }

func (w *stallWatchdog) Stop() { w.once.Do(func() { close(w.stop) }) }

func (w *stallWatchdog) Fired() bool { return w.fired.Load() }

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
			if now.Sub(time.Unix(0, w.last.Load())) >= w.timeout {
				w.fired.Store(true)
				w.Stop()
				onTimeout()
				return
			}
		}
	}
}
