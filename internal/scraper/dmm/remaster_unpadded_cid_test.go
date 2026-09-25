package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An unpadded raw marker cid (1rct156h) is equivalent to the server's padded
// cid (1rct00156h): the href substring filter and bindResolvedCID must accept
// the padded product the search returns, while the verbatim server cid stays
// in the result.
func TestSearchUnpaddedRawMarkerCIDResolvesPaddedProduct(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "/search/="):
			return 200, `<html><body><a href="/digital/videoa/-/detail/=/cid=1rct00156h/">remaster</a></body></html>`
		case strings.Contains(u, "cid=1rct00156h"):
			return 200, `<html><body><h1 id="title" class="item">Remaster</h1>` +
				`<table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table></body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)

	res, err := s.Search(context.Background(), "1rct156h")
	require.NoError(t, err, "the unpadded raw query must resolve the padded server cid")
	require.NotNil(t, res)
	assert.Equal(t, "1rct00156h", res.ContentID, "the verbatim server cid must be preserved")
	assert.Equal(t, "RCT-156H", res.ID)
}

func TestBindResolvedCIDPaddingEquivalence(t *testing.T) {
	assert.True(t, bindResolvedCID("1rct00156h", "1rct156h", true), "unpadded raw query binds the padded server cid")
	assert.True(t, bindResolvedCID("1rct00156h", "1rct00156h", true))
	assert.False(t, bindResolvedCID("rct00156h", "1rct156h", true), "catalog prefixes stay distinct")
	assert.False(t, bindResolvedCID("1rct00999h", "1rct156h", true), "numbers stay distinct")
	assert.False(t, bindResolvedCID("1rct00156hd", "1rct156h", true), "marker spellings stay distinct")
	assert.True(t, bindResolvedCID("h_003abc00123hd", "h_003abc123hd", true), "underscore channel prefixes normalize the number only")
}

// Unpadded base cids (no marker) keep the verbatim raw-cid bypass.
func TestResolveContentIDUnpaddedBaseCIDVerbatim(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		return 404, ""
	}}
	s.client.SetTransport(rt)

	cid, err := s.ResolveContentIDCtx(context.Background(), "118abf30")
	require.NoError(t, err)
	assert.Equal(t, "118abf30", cid)
	assert.Zero(t, rt.searchN, "unpadded base cids bypass search resolution")
}
