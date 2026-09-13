package dmm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The compacted forms of T-28123-HD and T28-123-HD are identical, so display
// verification must compare separator-pinned identities: a candidate page
// whose 品番 collides with the query under compaction is rejected instead of
// accepting and caching metadata for the wrong release.
func TestVerifyCandidateDisplayIDT28SeparatorCollision(t *testing.T) {
	pageDisplay := func(display string) func(u string) (int, string) {
		return func(u string) (int, string) {
			return 200, `<html><body><table><tr><th>品番</th><td>` + display + `</td></tr></table></body></html>`
		}
	}
	const productURL = "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=t28123h/"

	for _, tc := range []struct {
		name, query, pageDisplay string
		want                     displayStatus
	}{
		{"t28 query vs T-series page", "T28-123-HD", "T-28123-HD", displayRejected},
		{"t query vs T28 page", "T-28123-HD", "T28-123-HD", displayRejected},
		{"t28 query matching page", "T28-123-HD", "T28-123-HD", displayVerified},
		{"t query matching page", "T-28123-HD", "T-28123-HD", displayVerified},
		{"separator variants of one identity", "T28.123.HD", "t28-123-HD", displayVerified},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newRemasterTestScraper(t)
			s.client.SetTransport(&remasterRoundTripper{serve: pageDisplay(tc.pageDisplay)})
			st, err := s.verifyCandidateDisplayID(context.Background(), tc.query, []string{productURL})
			require.NoError(t, err)
			assert.Equal(t, tc.want, st)
		})
	}
}

// displayIdentityTuple keeps the pinned split for separated forms and applies
// the prefix-free disambiguation to compact ones.
func TestDisplayIdentityTupleSplit(t *testing.T) {
	for _, tc := range []struct {
		display, series, value, marker string
	}{
		{"T28-123-HD", "t28", "123", "h"},
		{"T-28123-HD", "t", "28123", "h"},
		{"T28.123.HD", "t28", "123", "h"},
		{"t28123h", "t", "28123", "h"},
		{"RCT-0156-HD", "rct", "156", "h"},
		{"DV-818AI", "dv", "818", "ai"},
	} {
		t.Run(tc.display, func(t *testing.T) {
			s, v, _, m, ok := displayIdentityTuple(tc.display)
			require.True(t, ok)
			assert.Equal(t, tc.series, s)
			assert.Equal(t, tc.value, v)
			assert.Equal(t, tc.marker, m)
			assert.NotEqual(t, "0", v[:1])
		})
	}
	_, _, _, _, ok := displayIdentityTuple("nonsense-display")
	assert.False(t, ok)
}
