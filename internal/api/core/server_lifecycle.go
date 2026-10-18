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
	r.serverTasksMu.Lock()
	r.ServerCtx()
	if r.serverCancel != nil {
		r.serverCancel()
	}
	r.serverTasksMu.Unlock()
	// Admission is closed by cancellation before waiting. Join server-owned
	// tasks before closing dependencies or returning to the DB-close caller.
	r.serverTasksWg.Wait()
	// The runtime owns its built-in dependency token store. RuntimeState.Shutdown
	// also runs during WebSocket hub resets, so token cleanup belongs here.
	if r.deps != nil {
		CloseTokenStore(r.deps.TokenStore)
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

// TrackServerTask registers work that Shutdown must cancel and join before
// dependencies close. Call before accessing dependencies or launching work;
// use ServerCtx for cancellation and defer the returned release in the worker.
// Unlike legacy background tracking, admission is serialized with Shutdown,
// so even a handler still running after HTTP drain timeout cannot add late work.
// A nil release means shutdown has started and the caller must not start work.
func (r *APIRuntime) TrackServerTask() (done func()) {
	r.serverTasksMu.Lock()
	defer r.serverTasksMu.Unlock()
	if r.ServerCtx().Err() != nil {
		return nil
	}
	r.serverTasksWg.Add(1)
	backgroundDone := r.EnsureRuntime().TrackBackgroundTask()
	return func() {
		backgroundDone()
		r.serverTasksWg.Done()
	}
}
