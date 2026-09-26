package r18devdump

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Separator-pinned display spellings pin their own series/number boundary: a
// T-28123-HD query must generate the T-series prefixes (55/874/1038/n_1137
// prefixed CIDs), not the t28-series set the compacted shape decodes to.
// Dropping the boundary would break dump and HTTP lookups for real T releases.
func TestContentIDCandidatesWithMarker_PinnedTSeriesBoundary(t *testing.T) {
	got := ContentIDCandidatesWithMarker("T-28123-HD")
	assert.Contains(t, got, "55t28123h")
	assert.Contains(t, got, "874t28123h")
	assert.NotContains(t, got, "9t28123h", "t28-series prefixes must not appear for a T-series display spelling")
}

// A separated E/Z suffix (T-28123-Z-HD) is the one display spelling whose
// series boundary the pinned grammar used to miss: the query compacted to
// t28123zh and only the t28-series candidates were generated, so a release
// stored solely under a catalog-prefixed T-series content id (55t28123zh)
// could not resolve. The pinned T-series candidates must now lead the list,
// with the compact t28 reading's candidates kept alongside them.
func TestContentIDCandidatesWithMarker_SeparatedEZSuffixKeepsTSeriesCandidates(t *testing.T) {
	got := ContentIDCandidatesWithMarker("T-28123-Z-HD")
	assert.Contains(t, got, "55t28123zh")
	assert.Contains(t, got, "874t28123zh")
	// 28123 is already five digits wide, so the generator emits no separate
	// zero-padded T-series form for it.
	assert.NotContains(t, got, "55t028123zh")
	// The compact t28 reading of the same compacted shape stays alongside
	// the pinned set, so dump rows keyed by it keep resolving.
	assert.Contains(t, got, "9t2800123zh")
	assert.Contains(t, got, "9t28123zh")
	// The display-pinned reading leads: every T-series catalog candidate
	// outranks the t28-series candidates.
	assert.Less(t, indexOfString(got, "55t28123zh"), indexOfString(got, "9t2800123zh"))

	// The E suffix and the dot/space separator families behave identically.
	for _, tc := range []struct{ query, tail string }{
		{"T-28123-E-HD", "eh"},
		{"T.28123.Z.HD", "zh"},
		{"T 28123 Z HD", "zh"},
	} {
		got := ContentIDCandidatesWithMarker(tc.query)
		assert.Contains(t, got, "55t28123"+tc.tail, tc.query)
		assert.Contains(t, got, "9t2800123"+tc.tail, tc.query)
		assert.Less(t, indexOfString(got, "55t28123"+tc.tail), indexOfString(got, "9t2800123"+tc.tail), tc.query)
	}
}

// Without a separated E/Z suffix the display pinning stays exclusive: no
// compact t28 candidates join the T-series set.
func TestContentIDCandidatesWithMarker_SeparatedEZSuffixAbsentStaysTSeriesOnly(t *testing.T) {
	got := ContentIDCandidatesWithMarker("T-28123-HD")
	assert.Contains(t, got, "55t28123h")
	assert.NotContains(t, got, "9t28123h", "t28-series prefixes must not appear for a T-series display spelling")
	assert.NotContains(t, got, "9t2800123h", "t28-series prefixes must not appear for a T-series display spelling")

	// A suffix glued to the number keeps the same exclusivity: only the
	// separated spelling carries the compact-reading ambiguity.
	got = ContentIDCandidatesWithMarker("T-28123Z-HD")
	assert.Contains(t, got, "55t28123zh")
	assert.NotContains(t, got, "9t28123zh", "a glued suffix keeps the pinned T-series set exclusive")
	assert.NotContains(t, got, "9t2800123zh", "a glued suffix keeps the pinned T-series set exclusive")
}

// The t28 boundary survives the separated-suffix fix intact: a T28-123-Z-HD
// display spelling still generates only the t28-series candidates.
func TestContentIDCandidatesWithMarker_SeparatedEZSuffixT28BoundaryIntact(t *testing.T) {
	got := ContentIDCandidatesWithMarker("T28-123-Z-HD")
	assert.Contains(t, got, "9t2800123zh")
	assert.Contains(t, got, "9t28123zh")
	assert.NotContains(t, got, "55t28123zh", "T-series prefixes must not appear for a t28-series display spelling")
}
