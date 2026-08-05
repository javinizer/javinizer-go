package r18devdump

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandGalleryRejectsMaxIntRange(t *testing.T) {
	// end == math.MaxInt with start == 0 overflows end-start+1 to a negative
	// count: the cap check must still reject, and the fill loop must never run.
	first := "digital/video/118abw00013/118abw00013jp-0"
	last := "digital/video/118abw00013/118abw00013jp-" + strconv.Itoa(math.MaxInt)
	assert.Nil(t, ExpandGallery(first, last))
}

func TestExpandGalleryCapBoundaryStillExpands(t *testing.T) {
	// A span of exactly 1000 remains legal; 1001 is rejected.
	urls := ExpandGallery("p/a-1", "p/a-1000")
	assert.Len(t, urls, 1000)
	assert.Equal(t, "p/a-1", urls[0])
	assert.Equal(t, "p/a-1000", urls[999])
	assert.Nil(t, ExpandGallery("p/a-1", "p/a-1001"))
}

func TestExpandGalleryMaxIntTermination(t *testing.T) {
	// Span endings within the cap distance of math.MaxInt must terminate:
	// the fill loop breaks AT end, so its counter never wraps to MinInt.
	max := strconv.Itoa(math.MaxInt)
	urls := ExpandGallery("digital/video/118abw00013/118abw00013jp-"+strconv.Itoa(math.MaxInt-1), "digital/video/118abw00013/118abw00013jp-"+max)
	require.Len(t, urls, 2)
	assert.Equal(t, "digital/video/118abw00013/118abw00013jp-"+strconv.Itoa(math.MaxInt-1), urls[0])
	assert.Equal(t, "digital/video/118abw00013/118abw00013jp-"+max, urls[1])
}
