package dmm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ScrapeURL follows the marker-aware parser's AI rule end to end: AI-remaster
// cid numbers are unrelated to display numbers (dv00899ai maps to DV-818AI),
// so a page without an authoritative 品番 row publishes an empty display ID
// instead of a cid-derived spelling that would misfile the release.
func TestScrapeURLAICIDWithoutPinzanLeavesIDEmpty(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		return 200, `<html><body><h1 id="title" class="item">AI Remaster</h1></body></html>`
	}})

	res, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=dv00899ai/")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "dv00899ai", res.ContentID)
	assert.Empty(t, res.ID, "no page 品番 and an AI cid must leave the display id unset")
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
