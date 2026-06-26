package tui

import "context"

// backgroundRunner runs a function in a background goroutine and provides
// Wait/Stop semantics. Replaces the former PoolInterface which wrapped
// worker.Pool for single-task submission.
type backgroundRunner interface {
	// Go runs fn in a background goroutine. It returns an error if the runner
	// has already been stopped.
	Go(fn func() error) error
	Wait() error
	Stop()
	// Context returns the runner's cancellable context, so callers can derive
	// child contexts whose cancellation propagates when Stop() is called.
	Context() context.Context
}
