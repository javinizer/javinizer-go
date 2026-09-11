package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestT28RemasterSearch(t *testing.T) {
	for _, q := range []string{"T28-123-HD", "T28-123H", "9t28123h"} {
		t.Run(q, func(t *testing.T) {
			marker, series, _ := classifyRemasterQuery(q)
			assert.Equal(t, "h", marker)
			assert.Equal(t, "t28", series)
			s, _ := newRemasterTestScraper(t)
			s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
				if strings.Contains(u, "/search/=") {
					return 200, `<a href="/mono/dvd/-/detail/=/cid=9t28123h/">Remaster</a>`
				}
				if strings.Contains(u, "cid=9t28123h") {
					return 200, `<html><h1 id="title" class="item">Remaster</h1><table><tr><td>品番：</td><td>T28-123-HD</td></tr></table></html>`
				}
				return 404, ""
			}})
			result, err := s.Search(context.Background(), q)
			require.NoError(t, err)
			assert.Equal(t, "9t28123h", result.ContentID)
			assert.Equal(t, "T28-123H", result.ID)
		})
	}
	assert.Equal(t, "T-28123", normalizeID("t28123"))
}
