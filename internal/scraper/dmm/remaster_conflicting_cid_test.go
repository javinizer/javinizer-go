package dmm

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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

func TestVerifyCandidateDisplayIDRejectsCIDContradiction(t *testing.T) {
	for _, tc := range []struct {
		name, query, cid, display string
		want                      displayStatus
	}{
		{"H number conflict", "RCT-156H", "1rct00157h", "RCT-156-HD", displayRejected},
		{"HD number conflict", "RCT-156H", "1rct00157hd", "RCT-156-HD", displayRejected},
		{"valid H padding", "RCT-156H", "1rct00156h", "RCT-00156-HD", displayVerified},
		{"AI server slot differs", "DV-818AI", "dv00899ai", "DV-818AI", displayVerified},
		{"old number-free H fixture rejects", "ABC-999H", "abc01234h", "ABC-999-HD", displayRejected},
		{"prefix-free T CID conflicts", "T28-123-HD", "t28123h", "T28-123-HD", displayRejected},
		{"base page conflicts", "RCT-156H", "1rct00156h", "RCT-156", displayRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newRemasterTestScraper(t)
			s.client.SetTransport(&remasterRoundTripper{serve: func(string) (int, string) {
				return 200, fmt.Sprintf(`<table><tr><td>品番：</td><td>%s</td></tr></table>`, tc.display)
			}})
			status, err := s.verifyCandidateDisplayID(context.Background(), tc.query, []string{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=" + tc.cid + "/"})
			require.NoError(t, err)
			require.Equal(t, tc.want, status)
		})
	}
}
