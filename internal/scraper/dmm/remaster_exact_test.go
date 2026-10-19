package dmm

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestRawRemasterPrefixBinding(t *testing.T) {
	for _, cid := range []string{"118abc00123h", "h_003abc00123hd", "n_600abc00123ai"} {
		t.Run(cid, func(t *testing.T) {
			assert.True(t, bindResolvedCID(strings.ToUpper(cid), cid, true))
			assert.True(t, bindResolvedCID(cid+"r", cid, true))
			assert.False(t, bindResolvedCID("436abc00123h", cid, true))
			assert.False(t, bindResolvedCID("abc00123h", cid, true))
		})
	}
	for _, searchExact := range []bool{false, true} {
		for _, available := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{true: "search", false: "direct"}[searchExact], map[bool]string{true: "available", false: "missing"}[available]}, "/"), func(t *testing.T) {
				s, _ := newRemasterTestScraper(t)
				s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
					if strings.Contains(u, "/search/=") {
						html := `<a href="/mono/dvd/-/detail/=/cid=436abc00123h/">Foreign</a><a href="/mono/dvd/-/detail/=/cid=abc00123h/">Unprefixed</a>`
						if searchExact && available {
							html += `<a href="/digital/videoa/-/detail/=/cid=118abc00123h/">Exact</a>`
						}
						return 200, html
					}
					if strings.Contains(u, "cid=118abc00123h") && !available {
						return 404, ""
					}
					return 200, `<html><h1 id="title" class="item">Remaster</h1></html>`
				}})
				result, err := s.Search(context.Background(), "118abc00123h")
				if !available {
					require.Error(t, err)
					assert.Nil(t, result)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, "118abc00123h", result.ContentID)
				assert.Contains(t, result.SourceURL, "cid=118abc00123h")
			})
		}
	}
}
