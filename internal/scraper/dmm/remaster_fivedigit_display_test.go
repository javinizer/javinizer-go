package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A compact five-digit display query (ABC12345H) carries no zero-padding
// evidence, so the raw-content-id shape must not claim it: the server's cid
// is catalog-prefixed (1abc12345h) and only display resolution can find it.
// The raw bypass would cache abc12345h and the raw binding would then reject
// the server's prefixed cid, missing the release entirely.
func TestSearchCompactFiveDigitDisplayResolvesPrefixedCID(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "/search/="):
			return 200, `<html><body><a href="/digital/videoa/-/detail/=/cid=1abc12345h/">remaster</a></body></html>`
		case strings.Contains(u, "cid=1abc12345h"):
			return 200, `<html><body><h1 id="title" class="item">Remaster</h1>` +
				`<table><tr><td>品番：</td><td>ABC-12345-HD</td></tr></table></body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)

	res, err := s.Search(context.Background(), "ABC12345H")
	require.NoError(t, err, "the compact five-digit display query must resolve through the display path")
	require.NotNil(t, res)
	assert.Equal(t, "1abc12345h", res.ContentID, "the catalog-prefixed server cid must be found via display resolution")
	assert.Equal(t, "ABC-12345H", res.ID)
	assert.Positive(t, rt.searchN, "a display query must not take the raw-cid bypass")
}

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
