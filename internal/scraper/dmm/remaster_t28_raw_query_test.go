package dmm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A prefix-free compact t28 tail with a three-digit number reads as the
// T-series release T-28123H, matching the anchoredSeriesMatches convention
// and normalizeID's word-boundary split. Separator-bearing display forms pin
// the series boundary (T28-123-HD stays T28-123H), and catalog-prefixed cids
// (9t28123h, h_003t28123h) stay series t28.
func TestClassifyRemasterQueryPrefixFreeT28Tail(t *testing.T) {
	marker, series, _, isCID := classifyRemasterQuery("t28123h")
	assert.Equal(t, "h", marker)
	assert.Equal(t, "t", series)
	assert.True(t, isCID)
	assert.Equal(t, "T-28123H", canonicalRemasterDisplayID("t28123h"))
	assert.Equal(t, "T-28123H", t28CidDisplayID("t28123h"))

	marker, series, _, _ = classifyRemasterQuery("9t28123h")
	assert.Equal(t, "h", marker)
	assert.Equal(t, "t28", series)
	assert.Equal(t, "T28-123H", t28CidDisplayID("9t28123h"))
	assert.Equal(t, "T28-123H", t28CidDisplayID("h_003t28123h"))

	_, series, _, _ = classifyRemasterQuery("T28-123-HD")
	assert.Equal(t, "t28", series)
	_, series, _, _ = classifyRemasterQuery("T-28123-HD")
	assert.Equal(t, "t", series)
	assert.Equal(t, "T28-123H", canonicalRemasterDisplayID("T28-123-HD"))
	assert.Equal(t, "T-28123H", canonicalRemasterDisplayID("T-28123-HD"))
}
