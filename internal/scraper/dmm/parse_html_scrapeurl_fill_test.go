package dmm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ScrapeURL ports Search's canonical-spelling fill: an AI-remaster page whose
// 品番 row is absent would otherwise publish an empty display ID, so the ID
// is derived from the URL cid instead.
func TestScrapeURLFillsIDFromCIDWithoutPinzan(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		return 200, `<html><body><h1 id="title" class="item">AI Remaster</h1></body></html>`
	}})

	res, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=dv00899ai/")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "dv00899ai", res.ContentID)
	assert.Equal(t, "DV-00899AI", res.ID, "canonical spelling derived from the URL cid fills the empty page id")
}

// HD cids already derive their display id from the cid; the fill must not
// interfere. A page with a 品番 row keeps outranking any derived spelling
// (pinned by TestScrapeURLMarkerModeFromURLCid).
func TestScrapeURLHDCIDKeepsDerivedID(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		return 200, `<html><body><h1 id="title" class="item">HD Remaster</h1></body></html>`
	}})

	res, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "1rct00156h", res.ContentID)
	assert.Equal(t, "RCT-156H", res.ID)
}
