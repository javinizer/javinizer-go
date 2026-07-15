package fanout

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// BoundedFanOut dispatches work items across a bounded errgroup, collecting
// outcomes via a buffered channel. The caller owns pre-loop setup (building
// commands, filtering) and post-drain aggregation (trackResults, lifecycle marks).
//
// The per-item work function runs under panic recovery at the caller's site
// (applyFile/scrapeFile already wrap with withFileRecovery) — BoundedFanOut
// itself does NOT add recovery, to keep the seam thin.
//
// Cancellation: when ctx is cancelled, eg.Wait() drains remaining goroutines
// and closes the outcome channel. Callers should check ctx.Err() after
// BoundedFanOut returns and call lifecycle.MarkCancelled() if needed.
func BoundedFanOut[T any, I any](
	ctx context.Context,
	maxWorkers int,
	items []I,
	work func(egCtx context.Context, item I) T,
) []T {
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(maxWorkers)

	outcomeCh := make(chan T, len(items))

	for _, item := range items {
		eg.Go(func() error {
			outcome := work(egCtx, item)
			outcomeCh <- outcome
			return nil
		})
	}

	// Close outcome channel when all goroutines complete.
	go func() {
		_ = eg.Wait()
		close(outcomeCh)
	}()

	outcomes := make([]T, 0, len(items))
	for o := range outcomeCh {
		outcomes = append(outcomes, o)
	}

	return outcomes
}
