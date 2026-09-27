package dmm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Underscore cids carry a DMM maker/channel code between the h_/n_ prefix and
// the series letters (h_1472smkcx003 -> SMKCX-003). anchoredSeriesMatches
// strips only the two-character prefix, so those digits stay the catalog
// prefix: h_003t28123h is T28-123H, not T-28123H.
func TestAnchoredSeriesMatchesUnderscorePrefixKeepsT28Series(t *testing.T) {
	assert.True(t, anchoredSeriesMatches("h_003t28123h", "t28"))
	assert.False(t, anchoredSeriesMatches("h_003t28123h", "t"))
	assert.True(t, anchoredSeriesMatches("n_796t28045h", "t28"))
	assert.False(t, anchoredSeriesMatches("n_796t28045h", "t"))
}
