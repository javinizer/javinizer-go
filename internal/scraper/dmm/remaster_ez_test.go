package dmm

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRentalRemasterClassification(t *testing.T) {
	marker, series, _, isCID := classifyRemasterQuery("1rct00156hr")
	assert.Equal(t, "h", marker)
	assert.Equal(t, "rct", series)
	assert.True(t, isCID)

	marker, series, _, isCID = classifyRemasterQuery("1rct00156hdr")
	assert.Equal(t, "h", marker)
	assert.Equal(t, "rct", series)
	assert.True(t, isCID)

	marker, _, _, isCID = classifyRemasterQuery("dv00899air")
	assert.Equal(t, "ai", marker)
	assert.True(t, isCID)

	marker, _, _, _ = classifyRemasterQuery("1rct00156h")
	assert.Equal(t, "h", marker)

	marker, series, _, isCID = classifyRemasterQuery("h_003abc00123hdr")
	assert.Equal(t, "h", marker)
	assert.Equal(t, "abc", series)
	assert.True(t, isCID)

	marker, _, _, isCID = classifyRemasterQuery("n_600abc00123air")
	assert.Equal(t, "ai", marker)
	assert.True(t, isCID)
	assert.Equal(t, "h_003abc00123hd", stripRentalSuffixMarkerAware("h_003abc00123hdr"))
	assert.Equal(t, "n_600abc00123ai", stripRentalSuffixMarkerAware("n_600abc00123air"))
}

func TestDisplayIdentityTupleCatalogSuffix(t *testing.T) {
	series, num, suffix, marker, ok := displayIdentityTuple("ipx535zh")
	require.True(t, ok)
	assert.Equal(t, "ipx", series)
	assert.Equal(t, "535", num)
	assert.Equal(t, "z", suffix)
	assert.Equal(t, "h", marker)

	_, _, suffix, _, ok = displayIdentityTuple("ipx535h")
	require.True(t, ok)
	assert.Empty(t, suffix)

	_, _, _, _, ok = displayIdentityTuple("ipx535dh")
	assert.False(t, ok)
}
