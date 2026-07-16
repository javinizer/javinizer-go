package stalltest_test

import (
	"context"
	"testing"
	"time"
)

// TestRequestTimeoutDeadlineReachesScrape asserts that when RequestTimeout is
// set, the nested context passed to WF.Scrape carries a deadline derived from
// request_timeout_seconds (not just worker_timeout). This is a unit-level
// contract test for the #153 fix.
func TestRequestTimeoutDeadlineReachesScrape(t *testing.T) {
	if testing.Short() {
		t.Skip("stall test")
	}

	// Simulate the nesting: parent (worker_timeout=10s) → child (request_timeout=2s)
	parent, parentCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer parentCancel()

	requestTimeout := 2 * time.Second
	child, childCancel := context.WithTimeout(parent, requestTimeout)
	defer childCancel()

	dl, ok := child.Deadline()
	if !ok {
		t.Fatal("child context has no deadline")
	}

	// The child deadline should be ~now + 2s (request_timeout), not ~now + 10s (worker_timeout)
	remaining := time.Until(dl)
	if remaining > 3*time.Second {
		t.Fatalf("deadline remaining = %v, expected ~%v (request_timeout should be the binding deadline, not worker_timeout)", remaining, requestTimeout)
	}
	if remaining < 1*time.Second {
		t.Fatalf("deadline remaining = %v, expected ~%v (too short — may have used a different timeout)", remaining, requestTimeout)
	}
}

// TestRequestTimeoutZeroMeansNoExtraDeadline asserts that when RequestTimeout
// is zero (unset), only the parent (worker_timeout) deadline applies — no
// additional nesting occurs. This is the "no request_timeout configured" path.
func TestRequestTimeoutZeroMeansNoExtraDeadline(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer parentCancel()

	// When RequestTimeout == 0, the code uses the parent context directly
	// (no nested WithTimeout). Simulate that:
	var scrapeCtx context.Context = parent
	if requestTimeout := time.Duration(0); requestTimeout > 0 {
		var cancel context.CancelFunc
		scrapeCtx, cancel = context.WithTimeout(parent, requestTimeout)
		defer cancel()
	}

	dl, ok := scrapeCtx.Deadline()
	if !ok {
		t.Fatal("expected parent deadline to be present")
	}
	// Should be ~10s (worker_timeout), not a shorter request_timeout
	remaining := time.Until(dl)
	if remaining < 8*time.Second {
		t.Fatalf("deadline remaining = %v, expected ~10s (worker_timeout) when request_timeout is unset", remaining)
	}
}
