package history

import (
	"context"
	"time"
)

// SwapReverterSweepForTest replaces the reverter's pre-sweep invocations
// (the destination-targeted sweep AND the wave-34 roots sweep, which share
// one wedge/timeout discipline) plus its budget, and returns a restore
// function that MUST be deferred by the caller. Tests use it to shrink the
// deadline and/or hang the sweep (a stalled network filesystem stand-in)
// while driving a command end-to-end; the given sweep then answers BOTH
// invocation seams, so a substitute wedges the whole pre-sweep surface.
// A nil sweep keeps the production sweeps and swaps only the budget.
func SwapReverterSweepForTest(sweep func(ctx context.Context, sweeper *ReplacementSweeper, dests []string) (int, error), timeout time.Duration) (restore func()) {
	prevDests, prevRoots, prevTimeout := reverterSweepDestinations, reverterSweepRoots, reverterSweepTimeout
	if sweep != nil {
		reverterSweepDestinations = sweep
		reverterSweepRoots = sweep
	}
	if timeout > 0 {
		reverterSweepTimeout = timeout
	}
	return func() {
		reverterSweepDestinations = prevDests
		reverterSweepRoots = prevRoots
		reverterSweepTimeout = prevTimeout
	}
}
