package organizer

import (
	"context"
	"testing"
)

func TestFinalObserveClaimInvalidIdentityDoesNotClaimDestination(t *testing.T) {
	target := t.TempDir() + "/movie.mp4"
	var absent *DuplicateTracker
	if _, dup := absent.ObserveClaim(context.Background(), "source", target, true); dup {
		t.Fatal("nil tracker claimed destination")
	}
	tracker := NewDuplicateTracker(true)
	for _, source := range []string{"", "  "} {
		if owner, dup := tracker.ObserveClaim(context.Background(), source, target, true); owner != "" || dup {
			t.Fatalf("invalid source %q: owner=%q duplicate=%v", source, owner, dup)
		}
	}
	if owner, dup := tracker.ObserveClaim(context.Background(), "source", " ", true); owner != "" || dup {
		t.Fatalf("invalid target: owner=%q duplicate=%v", owner, dup)
	}
	if owner, dup := tracker.ObserveClaim(context.Background(), "first", target, true); owner != "" || dup {
		t.Fatalf("first valid claimant unexpectedly rejected: %q %v", owner, dup)
	}
}
