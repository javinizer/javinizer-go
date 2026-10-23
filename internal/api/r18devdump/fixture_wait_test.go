package r18devdump

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// Real SQLite import/validation/close work is fixture setup, not a product
	// latency contract. Each wait gets one finite ceiling, never renewed by
	// progress. Cancellation and shutdown assertions still use awaitDone.
	fixtureWaitCeiling = 60 * time.Second
	// Leave room for the existing five-second shutdown assertions before the
	// test command's hard deadline. Cleanup still cancels and joins the worker;
	// this reserve is not a new timeout on (or guarantee about) that join.
	fixtureCleanupReserve = 5 * time.Second
)

var (
	errDumpWaitTimeout         = errors.New("dump wait budget exhausted")
	errDumpDoneMissing         = errors.New("accepted operation has no done channel")
	errDumpFinishedBeforeReady = errors.New("operation finished before fixture readiness")
)

func fixtureWaitBudget(now, deadline time.Time, hasDeadline bool) time.Duration {
	if !hasDeadline {
		return fixtureWaitCeiling
	}
	remaining := deadline.Sub(now)
	// Compare before subtracting so expired or saturated negative durations
	// cannot underflow into a positive wait.
	if remaining <= fixtureCleanupReserve {
		return 0
	}
	return min(fixtureWaitCeiling, remaining-fixtureCleanupReserve)
}

// waitDumpDone is also the primitive used by awaitDone's fixed-duration
// cancellation contract. An already-closed channel succeeds even if no budget
// remains; an unfinished operation never receives an extension or a retry.
func waitDumpDone(done <-chan struct{}, budget time.Duration) error {
	if done == nil {
		return errDumpDoneMissing
	}
	select {
	case <-done:
		return nil
	default:
	}
	if budget <= 0 {
		return errDumpWaitTimeout
	}
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return errDumpWaitTimeout
	}
}

// Only a pre-install gate needs a predicate: waiting for done there would
// deadlock on the intentionally held reload lock. ready must be non-blocking.
// Observe the actual operation channel too, so premature failure is not
// mistaken for readiness. Polling never resets the single budget timer.
func waitDumpReady(done <-chan struct{}, budget time.Duration, ready func() bool) error {
	if done == nil {
		return errDumpDoneMissing
	}
	select {
	case <-done:
		return errDumpFinishedBeforeReady
	default:
	}
	if ready() {
		select {
		case <-done:
			return errDumpFinishedBeforeReady
		default:
			return nil
		}
	}
	if budget <= 0 {
		return errDumpWaitTimeout
	}
	timer := time.NewTimer(budget)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return errDumpFinishedBeforeReady
		case <-timer.C:
			return errDumpWaitTimeout
		case <-tick.C:
			if ready() {
				select {
				case <-done:
					return errDumpFinishedBeforeReady
				default:
					return nil
				}
			}
		}
	}
}

func awaitFixtureDone(t *testing.T, h *dumpHandler) {
	t.Helper()
	h.mu.Lock()
	done := h.done
	h.mu.Unlock()
	deadline, ok := t.Deadline()
	budget := fixtureWaitBudget(time.Now(), deadline, ok)
	if err := waitDumpDone(done, budget); err != nil {
		t.Fatalf("fixture operation did not finish within %s: %v\n%s", budget, err, dumpTimeoutDiagnostics(h))
	}
}

func awaitFixtureReady(t *testing.T, h *dumpHandler, phase string, ready func() bool) {
	t.Helper()
	h.mu.Lock()
	done := h.done
	h.mu.Unlock()
	deadline, ok := t.Deadline()
	budget := fixtureWaitBudget(time.Now(), deadline, ok)
	if err := waitDumpReady(done, budget, ready); err != nil {
		t.Fatalf("fixture did not reach %s within %s: %v\n%s", phase, budget, err, dumpTimeoutDiagnostics(h))
	}
}

func TestFixtureWaitBudget(t *testing.T) {
	now := time.Date(2026, time.October, 23, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name        string
		deadline    time.Time
		hasDeadline bool
		want        time.Duration
	}{
		{"no deadline has finite fallback", time.Time{}, false, 60 * time.Second},
		{"far deadline is capped", now.Add(2 * time.Minute), true, 60 * time.Second},
		{"exact ceiling plus reserve", now.Add(65 * time.Second), true, 60 * time.Second},
		{"near deadline leaves reserve", now.Add(15 * time.Second), true, 10 * time.Second},
		{"one nanosecond remains", now.Add(5*time.Second + time.Nanosecond), true, time.Nanosecond},
		{"exact reserve", now.Add(5 * time.Second), true, 0},
		{"inside reserve", now.Add(time.Second), true, 0},
		{"expired", now.Add(-time.Second), true, 0},
		{"saturated future duration", time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), true, 60 * time.Second},
		{"saturated past duration", time.Time{}, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, fixtureWaitBudget(now, tc.deadline, tc.hasDeadline))
		})
	}
}

func TestFixtureDoneWait(t *testing.T) {
	t.Run("completion after old five second bound succeeds", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			done := make(chan struct{})
			go func() { time.Sleep(6 * time.Second); close(done) }()
			start := time.Now()
			budget := fixtureWaitBudget(start, time.Time{}, false)
			require.NoError(t, waitDumpDone(done, budget))
			require.Equal(t, 6*time.Second, time.Since(start))
		})
	})
	t.Run("stuck operation exhausts one ceiling", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			budget := fixtureWaitBudget(start, time.Time{}, false)
			require.ErrorIs(t, waitDumpDone(make(chan struct{}), budget), errDumpWaitTimeout)
			require.Equal(t, 60*time.Second, time.Since(start))
		})
	})
	t.Run("test deadline shortens wait and retains reserve", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			deadline := start.Add(15 * time.Second)
			budget := fixtureWaitBudget(start, deadline, true)
			require.ErrorIs(t, waitDumpDone(make(chan struct{}), budget), errDumpWaitTimeout)
			require.Equal(t, 10*time.Second, time.Since(start))
			require.Equal(t, 5*time.Second, deadline.Sub(time.Now()))
		})
	})
	t.Run("fixed cancellation contract still expires at five seconds", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			done := make(chan struct{})
			go func() { time.Sleep(6 * time.Second); close(done) }()
			// Assert the five-second result first, then join the synthetic late
			// producer even on assertion failure before the bubble exits.
			defer func() { <-done }()
			start := time.Now()
			// This is the same primitive and explicit duration used by awaitDone,
			// not the fixture budget calculation.
			require.ErrorIs(t, waitDumpDone(done, 5*time.Second), errDumpWaitTimeout)
			require.Equal(t, 5*time.Second, time.Since(start))
		})
	})
	t.Run("exhausted budgets never wait but accept completed operations", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			closed := make(chan struct{})
			close(closed)
			start := time.Now()
			for _, budget := range []time.Duration{0, -time.Second} {
				require.NoError(t, waitDumpDone(closed, budget))
				require.ErrorIs(t, waitDumpDone(make(chan struct{}), budget), errDumpWaitTimeout)
			}
			require.ErrorIs(t, waitDumpDone(nil, time.Minute), errDumpDoneMissing)
			require.Equal(t, time.Duration(0), time.Since(start))
		})
	})
}

func TestFixtureReadinessWait(t *testing.T) {
	t.Run("readiness after old five second bound succeeds", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			err := waitDumpReady(make(chan struct{}), fixtureWaitCeiling, func() bool {
				return time.Since(start) >= 6*time.Second
			})
			require.NoError(t, err)
			require.Equal(t, 6*time.Second, time.Since(start))
		})
	})
	t.Run("polling does not renew the ceiling", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			polls := 0
			err := waitDumpReady(make(chan struct{}), fixtureWaitCeiling, func() bool { polls++; return false })
			require.ErrorIs(t, err, errDumpWaitTimeout)
			require.Equal(t, 60*time.Second, time.Since(start))
			require.Greater(t, polls, 1)
		})
	})
	t.Run("premature completion rejects readiness", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			done := make(chan struct{})
			go func() { time.Sleep(2 * time.Second); close(done) }()
			start := time.Now()
			require.ErrorIs(t, waitDumpReady(done, fixtureWaitCeiling, func() bool { return false }), errDumpFinishedBeforeReady)
			require.Equal(t, 2*time.Second, time.Since(start))
		})
	})
	for _, poll := range []bool{false, true} {
		name := "completion during initial readiness check"
		if poll {
			name = "completion during polled readiness check"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				done := make(chan struct{})
				first := true
				err := waitDumpReady(done, fixtureWaitCeiling, func() bool {
					if poll && first {
						first = false
						return false
					}
					close(done)
					return true
				})
				require.ErrorIs(t, err, errDumpFinishedBeforeReady)
			})
		})
	}
	t.Run("exhausted or missing operation cannot wait", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			require.ErrorIs(t, waitDumpReady(make(chan struct{}), 0, func() bool { return false }), errDumpWaitTimeout)
			require.NoError(t, waitDumpReady(make(chan struct{}), 0, func() bool { return true }))
			require.ErrorIs(t, waitDumpReady(nil, fixtureWaitCeiling, func() bool { return true }), errDumpDoneMissing)
			closed := make(chan struct{})
			close(closed)
			require.ErrorIs(t, waitDumpReady(closed, fixtureWaitCeiling, func() bool { return true }), errDumpFinishedBeforeReady)
			require.Equal(t, time.Duration(0), time.Since(start))
		})
	})
}
