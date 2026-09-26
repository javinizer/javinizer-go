package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Direct-URL scrapes take the same marker-aware path as search results: a
// marker-bearing URL cid (dv00899ai) defers to the page's authoritative 品番
// instead of the cid-derived spelling, while non-marker URLs keep the
// prefix-cleaned identity.
func TestScrapeURLMarkerModeFromURLCid(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		return 200, "<html><body><table><tr><td>品番：</td><td>DV-818-AI</td></tr></table></body></html>"
	}})

	res, err := s.ScrapeURL(context.Background(),
		"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=dv00899ai/")
	require.NoError(t, err)
	assert.Equal(t, "DV-818AI", res.ID)
	assert.Equal(t, "dv00899ai", res.ContentID)

	res2, err := s.ScrapeURL(context.Background(),
		"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=ipx00535/")
	require.NoError(t, err)
	assert.NotEmpty(t, res2.ID)
	assert.True(t, strings.HasPrefix(strings.ToUpper(res2.ID), "IPX"))
}
