package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Padded display queries keep the deliberate verification contract: an AI
// candidate whose page cannot publish a 品番 stays unresolvable (the AI cid
// digits do not encode the display number, so only the page may prove the
// release), while a zero-padded query against a normal marker page keeps the
// page's zero-trimmed display value.
func TestSearchPaddedDisplayFallback(t *testing.T) {
	t.Run("ai page without pinzan cannot prove the release", func(t *testing.T) {
		s, _ := newRemasterTestScraper(t)
		rt := &remasterRoundTripper{serve: func(u string) (int, string) {
			switch {
			case strings.Contains(u, "/search/="):
				return 200, `<html><body><a href="/digital/videoa/-/detail/=/cid=dv00899ai/">remaster</a></body></html>`
			case strings.Contains(u, "cid=dv00899ai"):
				return 200, `<html><body><h1 id="title" class="item">AI Remaster</h1></body></html>`
			}
			return 404, ""
		}}
		s.client.SetTransport(rt)
		_, err := s.Search(context.Background(), "DV-818AI")
		require.Error(t, err, "AI cids do not encode the display number; without a page identity the candidate must not resolve")
		assert.Contains(t, err.Error(), "could not verify")
	})

	t.Run("page pinzan outranks padded query spelling", func(t *testing.T) {
		s, _ := newRemasterTestScraper(t)
		rt := &remasterRoundTripper{serve: func(u string) (int, string) {
			switch {
			case strings.Contains(u, "/search/="):
				return 200, `<html><body><a href="/digital/videoa/-/detail/=/cid=rct00156h/">remaster</a></body></html>`
			case strings.Contains(u, "cid=rct00156h"):
				return 200, `<html><body><h1 id="title" class="item">AI Remaster</h1><table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table></body></html>`
			}
			return 404, ""
		}}
		s.client.SetTransport(rt)
		res, err := s.Search(context.Background(), "RCT-00156-HD")
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, "RCT-156H", res.ID, "page zero-trimmed value wins over the padded query spelling")
	})
}

// A cached AI mapping whose page publishes no 品番 can no longer be verified
// in-flow (AI cid numbers diverge from display numbers), so the
// canonical-spelling fill is withheld: the mapping is invalidated and the
// query re-resolves through the verified resolver. When the resolver cannot
// verify the release either, the search misses honestly instead of
// publishing the unverified page's metadata under the query's identity.
func TestSearchCachedAIWithoutPageIdentityMissesHonestly(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.cacheContentID(context.Background(), "DV-818AI", "dv00899ai")
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "cid=dv00899ai"):
			return 200, "<html><body><h1 id=\"title\" class=\"item\">AI Remaster</h1></body></html>"
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)

	res, err := s.Search(context.Background(), "DV-818AI")
	require.Error(t, err, "an unverifiable cached mapping must not publish the query identity")
	assert.Nil(t, res)
	assert.Positive(t, rt.searchN, "the mapping was invalidated and resolution re-ran")
}

// A freshly resolved AI query whose fetched page publishes no 品番 keeps the
// canonical-spelling fill: the resolver verified the query's release
// against a product-page 品番 earlier in the same call, so the identity-less
// page's metadata may be labeled with the query's canonical spelling. The
// monthly page carries the 品番 the resolver verifies; the URL finder then
// selects the digital page, which publishes none.
func TestSearchFreshResolutionFillsEmptyPageID(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "searchstr=dv00818ai/"):
			// The resolver's spelling finds the monthly product page.
			return 200, `<html><body><a href="/monthly/premium/-/detail/=/cid=dv00899ai/">remaster</a></body></html>`
		case strings.Contains(u, "searchstr=dv00899ai/"):
			// The URL finder's content-id spelling finds the digital page.
			return 200, `<html><body><a href="/digital/videoa/-/detail/=/cid=dv00899ai/">remaster</a></body></html>`
		case strings.Contains(u, "/monthly/"):
			return 200, `<html><body><h1 id="title" class="item">AI Remaster</h1>` +
				`<table><tr><td>品番：</td><td>DV-818-AI</td></tr></table></body></html>`
		case strings.Contains(u, "/digital/"):
			return 200, `<html><body><h1 id="title" class="item">AI Remaster (digital)</h1></body></html>`
		case strings.Contains(u, "/search/="):
			return 200, `<html><body></body></html>`
		}
		return 404, ""
	}})

	res, err := s.Search(context.Background(), "DV-818AI")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "dv00899ai", res.ContentID)
	assert.Equal(t, "DV-818AI", res.ID, "this-call verification gates the canonical query fill")
	assert.Equal(t, "AI Remaster (digital)", res.Title, "the metadata comes from the fetched digital page")
}
