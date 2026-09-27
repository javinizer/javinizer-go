package r18dev

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A trailing -HDR token in a display query is a quality/vocabulary word
// (like HDTV/HDrip), not an HD-remaster marker plus a rental 'r': the query
// stays a base-release query. Genuine rental cid endings keep their
// normalization semantics.
func TestRemasterHDRDisplayQueryIsNotMarker(t *testing.T) {
	marker, series := classifyRemaster("ABW-121-HDR")
	assert.Empty(t, marker)
	assert.Empty(t, series)

	marker, series = classifyRemaster("abw121hdr")
	assert.Equal(t, "h", marker, "rental cid endings keep stripping")
	assert.Equal(t, "abw", series)

	marker, series = classifyRemaster("1rct00156hr")
	assert.Equal(t, "h", marker)
	assert.Equal(t, "rct", series)

	marker, series = classifyRemaster("dv00899air")
	assert.Equal(t, "ai", marker)
	assert.Equal(t, "dv", series)

	// Controls: plain HD display queries keep classifying as marker h.
	marker, _ = classifyRemaster("ABW-121-HD")
	assert.Equal(t, "h", marker)
	marker, _ = classifyRemaster("ABW-121-H")
	assert.Equal(t, "h", marker)
	marker, _ = classifyRemaster("DV-818-AIR")
	assert.Empty(t, marker, "trailing -AIR is a quality word too")
}
