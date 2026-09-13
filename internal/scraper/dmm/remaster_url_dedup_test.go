package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemasterVerificationDeduplicatesURLs(t *testing.T) {
	for _, browser := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "browser"}[browser], func(t *testing.T) {
			s, _ := newRemasterTestScraper(t)
			s.useBrowser = browser
			hits := map[string]int{}
			urls := []string{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/", "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=1rct00156h/"}
			if browser {
				urls = []string{"https://video.dmm.co.jp/av/content/?id=1rct00156h"}
			}
			page := `<table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table>`
			s.browserFetch = func(ctx context.Context, u string) (string, error) { hits[u]++; return page, nil }
			s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
				if strings.Contains(u, "/search/=") {
					html := ""
					for _, url := range urls {
						html += `<a href="` + url + `">Product</a><a href="` + url + `">Duplicate</a>`
					}
					return 200, html
				}
				hits[u]++
				return 200, page
			}})
			cid, err := s.ResolveContentID("RCT-156H")
			require.NoError(t, err)
			assert.Equal(t, "1rct00156h", cid)
			for _, url := range urls {
				assert.Equal(t, 1, hits[url], url)
			}
		})
	}
}
