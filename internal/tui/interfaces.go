package tui

// backgroundRunner runs a function in a background goroutine and provides
// Wait/Stop semantics. Replaces the former PoolInterface which wrapped
// worker.Pool for single-task submission.
type backgroundRunner interface {
	Go(fn func() error) error
	Wait() error
	Stop()
}
