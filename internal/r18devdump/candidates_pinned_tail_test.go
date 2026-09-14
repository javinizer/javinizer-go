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
