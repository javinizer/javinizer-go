package dmm

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestTwoDigitRemasterSearch(t *testing.T) {
	for _, marker := range []string{"h", "ai"} {
		for _, available := range []bool{false, true} {
			t.Run(marker+map[bool]string{false: "missing", true: "present"}[available], func(t *testing.T) {
				for _, q := range []string{"ABC-12" + strings.ToUpper(marker), "ABC.12." + map[string]string{"h": "HD", "ai": "AI"}[marker]} {
					s, _ := newRemasterTestScraper(t)
					s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
						cid := "1abc00012"
						if available {
							cid += marker
						}
						if strings.Contains(u, "/search/=") {
							return 200, `<a href="/mono/dvd/-/detail/=/cid=` + cid + `/">Product</a>`
						}
						return 200, `<html><h1 id="title" class="item">Product</h1><table><tr><td>品番：</td><td>ABC-12` + strings.ToUpper(marker) + `</td></tr></table></html>`
					}})
					result, err := s.Search(context.Background(), q)
					if !available {
						require.Error(t, err)
						assert.Nil(t, result)
						continue
					}
					require.NoError(t, err)
					assert.Equal(t, "1abc00012"+marker, result.ContentID)
					assert.Equal(t, "ABC-12"+strings.ToUpper(marker), result.ID)
				}
			})
		}
	}
}
