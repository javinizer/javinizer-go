package r18dev

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemaster_WideNumericRange(t *testing.T) {
	for _, tc := range []struct {
		id     string
		marker string
		series string
	}{
		{"ABC-1H", "h", "abc"},
		{"ABC-1-HD", "h", "abc"},
		{"ABC-123456H", "h", "abc"},
		{"ABC-123456-HD", "h", "abc"},
		{"IPX-535Z-HD", "h", "ipx"},
		{"abc1ai", "ai", "abc"},
	} {
		marker, series := classifyRemaster(tc.id)
		require.Equal(t, tc.marker, marker, tc.id)
		assert.Equal(t, tc.series, series, tc.id)
	}
	assert.Equal(t, "abc1h", foldDisplay("ABC-1-HD"))
	assert.Equal(t, "abc123456h", foldDisplay("ABC-123456-HD"))
}
