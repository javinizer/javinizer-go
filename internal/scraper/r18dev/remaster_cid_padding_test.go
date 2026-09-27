package r18dev

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// normalizeRawCIDPadding canonicalizes raw content ids by stripping leading
// zeros from the number. These tests pin the contract directly, including the
// all-zero number edge (a single zero is kept) and the non-cid-shape
// passthrough the doc comment promises.
func TestNormalizeRawCIDPadding(t *testing.T) {
	assert.Equal(t, "1rct156h", normalizeRawCIDPadding("1rct00156h"), "padded raw cid strips to the unpadded identity")
	assert.Equal(t, "1rct156h", normalizeRawCIDPadding(" 1RCT00156H "), "input is trimmed and lowercased before normalizing")
	assert.Equal(t, "h_003abc123hd", normalizeRawCIDPadding("h_003abc00123hd"), "the [hn]_ channel prefix stays verbatim while the number normalizes")
	assert.Equal(t, "1abc0h", normalizeRawCIDPadding("1abc00000h"), "an all-zero number keeps a single zero instead of vanishing")
	assert.Equal(t, "12345", normalizeRawCIDPadding("12345"), "a bare number is not cid-shaped and passes through compacted")
	assert.Equal(t, "null", normalizeRawCIDPadding("NULL"), "a non-cid echo passes through lowercased for comparison")
}
