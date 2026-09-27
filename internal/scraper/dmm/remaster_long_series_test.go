package dmm

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestLongSeriesRemasterSearch(t *testing.T) {
	for _, marker := range []string{"h", "ai"} {
		for _, available := range []bool{false, true} {
			t.Run(marker+map[bool]string{false: "missing", true: "present"}[available], func(t *testing.T) {
				for _, q := range []string{"ABEAUTY-123" + strings.ToUpper(marker), "ABEAUTY.123." + map[string]string{"h": "HD", "ai": "AI"}[marker]} {
					s, _ := newRemasterTestScraper(t)
					s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
						cid := "5125abeauty00123"
						if available {
							cid += marker
						}
						if strings.Contains(u, "/search/=") {
							return 200, `<a href="/mono/dvd/-/detail/=/cid=` + cid + `/">Product</a>`
						}
						return 200, `<html><h1 id="title" class="item">Product</h1><table><tr><td>品番：</td><td>ABEAUTY-123` + strings.ToUpper(marker) + `</td></tr></table></html>`
					}})
					result, err := s.Search(context.Background(), q)
					if !available {
						require.Error(t, err)
						assert.Nil(t, result)
						continue
					}
					require.NoError(t, err)
					assert.Equal(t, "5125abeauty00123"+marker, result.ContentID)
					assert.Equal(t, "ABEAUTY-123"+strings.ToUpper(marker), result.ID)
				}
			})
		}
	}
}
