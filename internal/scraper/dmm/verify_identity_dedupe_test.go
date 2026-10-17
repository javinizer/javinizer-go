package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The same cleaned cid found under multiple product URLs whose pages use
// equivalent 品番 spellings (RCT-156-HD, RCT156HD) parses to one identity, so
// the candidate verifies instead of being rejected as unverifiable.
func TestVerifyCandidateDisplayIDEquivalentSpellings(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "cid=1rct00156h&p=1") {
			return 200, "<html><body><table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table></body></html>"
		}
		return 200, "<html><body><table><tr><td>品番：</td><td>RCT156HD</td></tr></table></body></html>"
	}})

	st, err := s.verifyCandidateDisplayID(context.Background(), "RCT-156H",
		[]string{
			"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/",
			"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/p=1/",
		})
	require.NoError(t, err)
	assert.Equal(t, displayVerified, st)

	// The T/T28 distinction survives the tuple keying.
	s2, _ := newRemasterTestScraper(t)
	s2.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		return 200, "<html><body><table><tr><td>品番：</td><td>T-28123-HD</td></tr></table></body></html>"
	}})
	st, err = s2.verifyCandidateDisplayID(context.Background(), "T28-123-HD",
		[]string{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=t28123h/"})
	require.NoError(t, err)
	assert.Equal(t, displayRejected, st)
}
