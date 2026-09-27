package r18dev

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Underscore cids carry a DMM maker/channel code between the h_/n_ prefix and
// the series letters (h_1472smkcx003 -> SMKCX-003). The T/T28 disambiguation
// must keep treating those digits as a catalog prefix: h_003t28123h is
// T28-123H (maker 003, series t28), not T-28123H, so a series-t query must
// reject it while the t28 query accepts it.
func TestCidMatchesMarkerUnderscorePrefixKeepsT28Series(t *testing.T) {
	assert.True(t, cidMatchesMarker("h_003t28123h", "h", "t28"), "underscore maker prefix keeps the t28 series")
	assert.False(t, cidMatchesMarker("h_003t28123h", "h", "t"), "underscore maker prefix must not read as series t")
	marker, series := classifyRemaster("h_003t28123h")
	assert.Equal(t, "h", marker)
	assert.Equal(t, "t28", series)
}
