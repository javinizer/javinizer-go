package history

import (
	"context"
	"time"
)

// SwapReverterSweepForTest replaces the reverter's targeted-sweep invocation
// AND its budget and returns a restore function that MUST be deferred by the
// caller. Tests use it to shrink the deadline and/or hang the sweep (a
// stalled network filesystem stand-in) while driving a command end-to-end.
// A nil sweep keeps the production sweep and swaps only the budget.
func SwapReverterSweepForTest(sweep func(ctx context.Context, sweeper *ReplacementSweeper, dests []string) (int, error), timeout time.Duration) (restore func()) {
	prevSweep, prevTimeout := reverterSweepDestinations, reverterSweepTimeout
	if sweep != nil {
		reverterSweepDestinations = sweep
	}
	if timeout > 0 {
		reverterSweepTimeout = timeout
	}
	return func() {
		reverterSweepDestinations = prevSweep
		reverterSweepTimeout = prevTimeout
	}
}
