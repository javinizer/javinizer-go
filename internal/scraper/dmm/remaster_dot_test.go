package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDottedRemasterDisplayResolution(t *testing.T) {
	for _, query := range []string{"RCT.00156.HD", "RCT.156.HD", "RCT.00156H"} {
		t.Run(query, func(t *testing.T) {
			marker, series, _, raw := classifyRemasterQuery(query)
			assert.Equal(t, "h", marker)
			assert.Equal(t, "rct", series)
			assert.False(t, raw)
			s, repo := newRemasterTestScraper(t)
			rt := &remasterRoundTripper{serve: func(u string) (int, string) {
				if strings.Contains(u, "/search/=") {
					return 200, `<a href="/digital/videoa/-/detail/=/cid=1rct00156h/">Remaster</a>`
				}
				if strings.Contains(u, "cid=1rct00156h") {
					return 200, `<table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table>`
				}
				return 404, ""
			}}
			s.client.SetTransport(rt)
			cid, err := s.ResolveContentID(query)
			require.NoError(t, err)
			assert.Equal(t, "1rct00156h", cid)
			assert.Positive(t, rt.searchN)
			cached, err := repo.FindBySearchID(context.Background(), strings.ToUpper(query))
			require.NoError(t, err)
			assert.Equal(t, cid, cached.ContentID)
			result, err := s.Search(context.Background(), query)
			require.NoError(t, err)
			assert.Equal(t, cid, result.ContentID)
		})
	}
	_, _, _, raw := classifyRemasterQuery("1rct00156hd")
	assert.True(t, raw)
}
