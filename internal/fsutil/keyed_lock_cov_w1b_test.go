package fsutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeyedLockAcquireMany_CoverageW1B(t *testing.T) {
	r := NewKeyedLockRegistry()

	// A nil set takes the no-key early return and its release is intentionally
	// a no-op.
	release := r.AcquireMany(nil)
	release()

	// Exercise the normal acquisition path and the folded-key deduplication
	// path in one isolated registry.
	release = r.AcquireMany([]string{"/dst/poster.jpg", "/DST/POSTER.JPG", "/dst/other.jpg"})
	release()

	// Empty-looking inputs are normalized before AcquireMany sees them; they
	// remain safe to acquire and release like any other key.
	release = r.AcquireMany([]string{"", " "})
	release()

	r.mu.Lock()
	defer r.mu.Unlock()
	require.Empty(t, r.locks)
}
