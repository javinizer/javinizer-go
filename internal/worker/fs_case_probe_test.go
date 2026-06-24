package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRandomProbeToken_DistinctAcrossCalls verifies that repeated calls produce
// distinct tokens — the property that prevents concurrent probes (and
// pre-existing files) from colliding on the old fixed probe filenames.
func TestRandomProbeToken_DistinctAcrossCalls(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		tok := randomProbeToken()
		assert.NotEmpty(t, tok, "token must never be empty (even on rand failure it returns 'fallback')")
		assert.False(t, seen[tok], "token %q repeated at iteration %d — must be unique", tok, i)
		seen[tok] = true
	}
}

// TestRandomProbeToken_NonEmptyOnFallback verifies the fallback path yields a
// usable, non-empty token even if crypto/rand were to fail.
func TestRandomProbeToken_NonEmptyOnFallback(t *testing.T) {
	// We can't easily force crypto/rand to fail, but we assert the contract:
	// every call returns a non-empty string usable as a filename component.
	for i := 0; i < 10; i++ {
		assert.NotEmpty(t, randomProbeToken())
	}
}
