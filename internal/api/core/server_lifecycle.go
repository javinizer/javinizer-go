package core

import (
	"context"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Server lifecycle methods on APIRuntime
//
// ServerCtx and Shutdown manage the server-lifetime context and background
// goroutine teardown. Extracted from runtime_manager.go so that file focuses
// on lazy init + factory construction.
// ---------------------------------------------------------------------------

// serverLifecycleState groups the server-owned context and task join handles.
// Embedded by value in APIRuntime so its zero value remains ready for lazy init.
type serverLifecycleState struct {
	// Server lifecycle — serverCtx is the server-lifetime context,
	// cancelled on Shutdown(). Used by batch job launch goroutines so they
	// receive a cancellation signal when the server shuts down.
	// serverTasksMu serializes task admission with shutdown cancellation.
	serverTasksMu sync.Mutex
	serverTasksWg sync.WaitGroup
	serverCtxOnce sync.Once
	serverCtx     context.Context
	serverCancel  context.CancelFunc

	// updateCheckerDone is the join handle for the background update-check
	// goroutine started by startUpdateChecker: closed when that goroutine
	// has fully exited (ticker stopped, in-flight check returned). Written
	// once during bootstrap, before the server serves, and joined by
	// Shutdown after cancelling serverCtx, so teardown cannot race the
	// update checker's final state-store write. Nil when version checks
	// are disabled (startUpdateChecker returned early).
	updateCheckerDone <-chan struct{}
}

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
