package r18dev

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A prefix-free compact t28 tail with a three-digit number reads as the
// T-series release T-28123H, matching the anchoredSeriesMatches convention:
// the raw query t28123h classifies as series t so its own guard accepts the
// exact content id. Catalog-prefixed cids (9t28123h) stay series t28.
func TestClassifyRemasterPrefixFreeT28Tail(t *testing.T) {
	marker, series := classifyRemaster("t28123h")
	assert.Equal(t, "h", marker)
	assert.Equal(t, "t", series)
	assert.True(t, cidMatchesMarker("t28123h", "h", "t"), "the raw query must satisfy its own guard")
	assert.False(t, cidMatchesMarker("t28123h", "h", "t28"))
	assert.Equal(t, []string{"t-28123-hd"}, remasterDisplaySpellings("t28123h"))

	marker, series = classifyRemaster("9t28123h")
	assert.Equal(t, "h", marker)
	assert.Equal(t, "t28", series)
	assert.True(t, cidMatchesMarker("9t28123h", "h", "t28"))
	assert.False(t, cidMatchesMarker("9t28123h", "h", "t"))

	marker, series = classifyRemaster("h_003t28123h")
	assert.Equal(t, "h", marker)
	assert.Equal(t, "t28", series)

	_, series = classifyRemaster("T28-123-HD")
	assert.Equal(t, "t28", series)
	_, series = classifyRemaster("T-28123-HD")
	assert.Equal(t, "t", series)
}
