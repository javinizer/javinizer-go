package dmm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyRemaster_WideNumericRange(t *testing.T) {
	for _, tc := range []struct {
		id     string
		marker string
		series string
		suffix string
	}{
		{"ABC-1H", "h", "abc", ""},
		{"ABC-1-HD", "h", "abc", ""},
		{"ABC-123456H", "h", "abc", ""},
		{"ABC-123456-HD", "h", "abc", ""},
		{"IPX-535ZH", "h", "ipx", "z"},
		{"abc1ai", "ai", "abc", ""},
	} {
		marker, series, suffix, _ := classifyRemasterQuery(tc.id)
		require.Equal(t, tc.marker, marker, tc.id)
		assert.Equal(t, tc.series, series, tc.id)
		assert.Equal(t, tc.suffix, suffix, tc.id)
	}
}
