package dmm

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

// The five-digit shape boundary: zero-padded five-digit forms and the
// prefix-free t28 tail keep the raw bypass, while non-padded five-digit
// display spellings stay on the resolver path.
func TestClassifyRemasterQueryFiveDigitPaddingBoundary(t *testing.T) {
	for _, id := range []string{"abc01234h", "lulu00441", "lulu00441ai", "t28123h", "t2800123hd"} {
		_, _, _, isCID := classifyRemasterQuery(id)
		assert.True(t, isCID, id+" keeps the raw content-id shape")
	}
	for _, id := range []string{"abc12345h", "abc12345", "ABC12345H"} {
		_, _, _, isCID := classifyRemasterQuery(id)
		assert.False(t, isCID, id+" is a display spelling without zero-padding evidence")
	}
}
