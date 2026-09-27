package dmm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// normalizeCIDPadding canonicalizes marker-bearing content ids by stripping
// leading zeros from the number. These tests pin the normalization contract
// directly, including the all-zero number edge: an all-zero number keeps a
// single zero so the identity never collapses to a markerless form.
func TestNormalizeCIDPadding(t *testing.T) {
	assert.Equal(t, "1rct156h", normalizeCIDPadding("1rct00156h"), "padded marker cid strips to the unpadded identity")
	assert.Equal(t, "1rct156h", normalizeCIDPadding(" 1RCT00156H "), "input is trimmed and lowercased before normalizing")
	assert.Equal(t, "h_003abc123hd", normalizeCIDPadding("h_003abc00123hd"), "the [hn]_ channel prefix stays verbatim while the number normalizes")
	assert.Equal(t, "1abc0h", normalizeCIDPadding("1abc00000h"), "an all-zero number keeps a single zero instead of vanishing")
	assert.Equal(t, "1rct00156", normalizeCIDPadding("1rct00156"), "markerless base cids pass through unchanged")
	assert.Equal(t, "plainword", normalizeCIDPadding("plainword"), "non-cid ids pass through unchanged")
}
