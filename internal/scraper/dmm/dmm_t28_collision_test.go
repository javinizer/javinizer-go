package dmm

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

// displayIdentityTuple keeps the pinned split for separated forms and applies
// the prefix-free disambiguation to compact ones.
func TestDisplayIdentityTupleSplit(t *testing.T) {
	for _, tc := range []struct {
		display, series, value, marker string
	}{
		{"T28-123-HD", "t28", "123", "h"},
		{"T-28123-HD", "t", "28123", "h"},
		{"T28.123.HD", "t28", "123", "h"},
		{"t28123h", "t", "28123", "h"},
		{"RCT-0156-HD", "rct", "156", "h"},
		{"DV-818AI", "dv", "818", "ai"},
	} {
		t.Run(tc.display, func(t *testing.T) {
			s, v, _, m, ok := displayIdentityTuple(tc.display)
			require.True(t, ok)
			assert.Equal(t, tc.series, s)
			assert.Equal(t, tc.value, v)
			assert.Equal(t, tc.marker, m)
			assert.NotEqual(t, "0", v[:1])
		})
	}
	_, _, _, _, ok := displayIdentityTuple("nonsense-display")
	assert.False(t, ok)
}

func TestTrimDisplayZeros(t *testing.T) {
	assert.Equal(t, "156", trimDisplayZeros("0156"))
	assert.Equal(t, "0", trimDisplayZeros("000"))
	assert.Equal(t, "0", trimDisplayZeros("0"))
	assert.Equal(t, "28123", trimDisplayZeros("28123"))
}
