package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLongPrefixVRGRemasterSearch(t *testing.T) {
	for _, q := range []string{"VRG-123-HD", "VRG-123H", "5750360vrg00123h"} {
		t.Run(q, func(t *testing.T) {
			marker, series, _ := classifyRemasterQuery(q)
			assert.Equal(t, "h", marker)
			assert.Equal(t, "vrg", series)
			s, _ := newRemasterTestScraper(t)
			s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
				if strings.Contains(u, "/search/=") {
					return 200, `<a href="/mono/dvd/-/detail/=/cid=5750360vrg00123h/">Remaster</a>`
				}
				if strings.Contains(u, "cid=5750360vrg00123h") {
					return 200, `<html><h1 id="title" class="item">Remaster</h1><table><tr><td>品番：</td><td>VRG-123-HD</td></tr></table></html>`
				}
				return 404, ""
			}})
			result, err := s.Search(context.Background(), q)
			require.NoError(t, err)
			assert.Equal(t, "5750360vrg00123h", result.ContentID)
			assert.Equal(t, "VRG-123H", result.ID)
		})
	}
}
