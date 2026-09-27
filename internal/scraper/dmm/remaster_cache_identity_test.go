package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemasterCacheIdentity(t *testing.T) {
	for _, tc := range []struct {
		id, cached, want string
		search           bool
	}{
		{"118abc00123h", "abc00123h", "118abc00123h", false},
		{"118abc00123hd", "118abc00123h", "118abc00123hd", false},
		{"118abc00123h", "118abc00123h", "118abc00123h", false},
		{"RCT-156H", "1rct00156", "1rct00156h", true},
		{"RCT-156H", "1abc00156h", "1rct00156h", true},
		{"RCT-156H", "1rct00156ai", "1rct00156h", true},
		{"RCT-156H", "1rct00156h", "1rct00156h", false},
		{"IPX-535", "118ipx00535", "118ipx00535", false},
	} {
		t.Run(tc.id+tc.cached, func(t *testing.T) {
			s, repo := newRemasterTestScraper(t)
			ctx := context.Background()
			s.cacheContentID(ctx, strings.ToUpper(tc.id), tc.cached)
			rt := &remasterRoundTripper{serve: func(u string) (int, string) {
				if !tc.search {
					t.Errorf("unexpected HTTP: %s", u)
				}
				if strings.Contains(u, "/search/=") {
					return 200, `<a href="/mono/dvd/-/detail/=/cid=1rct00156h/">Remaster</a>`
				}
				return 200, `<table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table>`
			}}
			s.client.SetTransport(rt)
			cid, err := s.ResolveContentIDCtx(ctx, tc.id)
			require.NoError(t, err)
			assert.Equal(t, tc.want, cid)
			if tc.search {
				assert.Positive(t, rt.searchN)
			} else {
				assert.Empty(t, rt.hits)
			}
			cached, err := repo.FindBySearchID(ctx, strings.ToUpper(tc.id))
			require.NoError(t, err)
			assert.Equal(t, tc.want, cached.ContentID)
		})
	}
}
