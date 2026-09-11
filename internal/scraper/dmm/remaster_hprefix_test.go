package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHPrefixRemasterSearchPreservesLiteralCID(t *testing.T) {
	for _, tc := range []struct{ cid, marker string }{
		{"h_003abc00123h", "h"},
		{"h_003abc00123hd", "h"},
		{"h_003abc00123ai", "ai"},
		{"n_600abc00123h", "h"},
		{"n_600abc00123hd", "h"},
		{"n_600abc00123ai", "ai"},
	} {
		t.Run(tc.cid, func(t *testing.T) {
			marker, series, _, raw := classifyRemasterQuery(tc.cid)
			assert.Equal(t, tc.marker, marker)
			assert.Equal(t, "abc", series)
			assert.True(t, raw)
			s, _ := newRemasterTestScraper(t)
			rt := &remasterRoundTripper{serve: func(u string) (int, string) {
				if strings.Contains(u, "/search/=") {
					return 200, `<a href="/mono/dvd/-/detail/=/cid=1abc00999h/">Wrong product</a>` +
						`<a href="/digital/videoa/-/detail/=/cid=` + tc.cid + `/">Requested product</a>`
				}
				if strings.Contains(u, "cid="+tc.cid) {
					return 200, `<html><h1 id="title" class="item">Requested remaster</h1></html>`
				}
				return 404, ""
			}}
			s.client.SetTransport(rt)
			result, err := s.Search(context.Background(), tc.cid)
			require.NoError(t, err)
			assert.Equal(t, tc.cid, result.ContentID)
			assert.NotEmpty(t, result.ID)
			assert.Contains(t, result.SourceURL, "cid="+tc.cid)
			for _, hit := range rt.hits {
				assert.NotContains(t, hit, "cid=1abc00999h")
			}
		})
	}
	marker, _, _, raw := classifyRemasterQuery("h_003abc00123")
	assert.Empty(t, marker)
	assert.True(t, raw)
}
