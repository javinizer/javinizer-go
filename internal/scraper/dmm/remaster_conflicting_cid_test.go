package dmm

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestSearchDisplayQueryRejectsConflictingHCIDNumber(t *testing.T) {
	for _, marker := range []string{"h", "hd"} {
		t.Run(marker, func(t *testing.T) {
			s, _ := newRemasterTestScraper(t)
			cid := "1rct00157" + marker
			s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
				if strings.Contains(u, "/search/=") {
					return 200, fmt.Sprintf(`<a href="/mono/dvd/-/detail/=/cid=%s/">candidate</a>`, cid)
				}
				if strings.Contains(u, "cid="+cid) {
					return 200, `<h1 id="title" class="item">Wrong CID page</h1><table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table>`
				}
				return 404, ""
			}})
			result, err := s.Search(context.Background(), "RCT-156H")
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
}
