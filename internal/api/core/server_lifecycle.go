package core

import (
	"context"
)

// ---------------------------------------------------------------------------
// Server lifecycle methods on APIRuntime
//
// ServerCtx, Shutdown, and SetTempCleanupStop manage the server-lifetime
// context and background goroutine teardown. Extracted from runtime_manager.go
// so that file focuses on lazy init + factory construction.
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

// SetTempCleanupStop sets the channel to stop the periodic stale temp dir cleanup.
func (r *APIRuntime) SetTempCleanupStop(ch chan struct{}) {
	r.tempCleanupStop = ch
}

// Shutdown stops background goroutines and releases resources.
// Should be called on API server shutdown for clean termination.
func (r *APIRuntime) Shutdown() {
	if r.tempCleanupStop != nil {
		close(r.tempCleanupStop)
		r.tempCleanupStop = nil
	}
	if r.serverCancel != nil {
		r.serverCancel()
	}
}
