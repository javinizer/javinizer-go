package core

import (
	"context"
	"time"
)

// ---------------------------------------------------------------------------
// Server lifecycle methods on APIRuntime
//
// ServerCtx and Shutdown manage the server-lifetime context and background
// goroutine teardown. Extracted from runtime_manager.go so that file focuses
// on lazy init + factory construction.
// ---------------------------------------------------------------------------

// ServerCtx returns the server-lifetime context. Cancelled on Shutdown().
// Batch job launch goroutines should use this instead of context.Background()
// so they receive a cancellation signal on graceful server shutdown.
func (r *APIRuntime) ServerCtx() context.Context {
	r.serverCtxOnce.Do(func() {
		r.serverCtx, r.serverCancel = context.WithCancel(context.Background())
	})
	return r.serverCtx
}

// Shutdown stops background goroutines and releases resources.
// Should be called on API server shutdown for clean termination.
func (r *APIRuntime) Shutdown() {
	// Force the lazy init first: goroutines launched via ServerCtx() write
	// serverCancel inside the Once, and only a Do() return establishes the
	// happens-before edge that makes reading serverCancel here race-free
	// (previously a shutdown racing a lazy ServerCtx() init was a data race).
	r.ServerCtx()
	if r.serverCancel != nil {
		r.serverCancel()
	}
	// Join the background update checker (when it was started) so its goroutine
	// is confirmed exited — and done writing to the on-disk state cache —
	// before teardown completes. Nil-safe: startUpdateChecker leaves the field
	// nil when version checks are disabled, and receiving from a nil channel
	// would block forever. No timeout on the join (tasks.md 3.3): after
	// serverCancel the in-flight BackgroundCheck aborts promptly via ctx, and
	// the check self-bounds (30s ctx timeout; 10s GitHub HTTP client timeout),
	// so the join cannot stall indefinitely.
	if r.updateCheckerDone != nil {
		<-r.updateCheckerDone
	}
}

// TrackBackgroundTask delegates to the runtime state — implemented there to
// keep runtime_manager.go within the API file-size budget. See
// (*RuntimeState).TrackBackgroundTask.
func (r *APIRuntime) TrackBackgroundTask() (done func()) {
	return r.GetRuntime().TrackBackgroundTask()
}

// WaitBackgroundTasks delegates to the runtime state. See
// (*RuntimeState).WaitBackgroundTasks.
func (r *APIRuntime) WaitBackgroundTasks(timeout time.Duration) bool {
	return r.GetRuntime().WaitBackgroundTasks(timeout)
}
