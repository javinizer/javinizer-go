package javdb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// remasterMarkerSuffix: an empty or separator-only input normalizes to an empty
// comparison id and reports no marker, and an id whose trailing letter run has
// no number before it (all letters, no digits) reports no marker either.
//
// Note on the non-digit-before-the-letter-run guard (javdb.go:873-875): it is
// unreachable through normalizeIDForCompare. The comparison id strips every
// non-[A-Za-z0-9] rune and uppercases the rest, so after the trailing ASCII
// letter run is consumed, the byte before it is always an ASCII digit (or the
// string start, which returns earlier). The guard is defensive only and cannot
// be exercised by an honest test; these tests pin the reachable boundary
// instead (empty input, letter-only input, digit-preceded run).
func TestRemasterMarkerSuffix_EmptyAndUnnumberedInputs(t *testing.T) {
	assert.Empty(t, remasterMarkerSuffix(""), "empty input carries no marker")
	assert.Empty(t, remasterMarkerSuffix("   "), "whitespace-only input normalizes to an empty comparison id")
	assert.Empty(t, remasterMarkerSuffix("　"), "ideographic-space input normalizes to an empty comparison id")
	assert.Empty(t, remasterMarkerSuffix("-_-"), "separator-only input normalizes to an empty comparison id")
	assert.Empty(t, remasterMarkerSuffix("ABCHD"), "letters with no preceding number are not a marker suffix")
	assert.Empty(t, remasterMarkerSuffix("HD"), "a bare marker word with no number is not a marker suffix")

	// Positive control: a digit-preceded trailing letter run folds.
	assert.Equal(t, "H", remasterMarkerSuffix("RCT156H"))
}
