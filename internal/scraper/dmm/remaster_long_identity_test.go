package dmm

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLongRawIdentity(t *testing.T) {
	for _, id := range []string{"5750360vrg00123h", "5755360vrpg00123hd", "5750360vrg00123ai"} {
		marker, _, raw := classifyRemasterQuery(id)
		require.NotEmpty(t, marker)
		assert.True(t, raw)
		s, _ := newRemasterTestScraper(t)
		rt := &remasterRoundTripper{serve: func(u string) (int, string) { t.Errorf("unexpected request: %s", u); return 404, "" }}
		s.client.SetTransport(rt)
		for range 2 {
			cid, err := s.ResolveContentID(id)
			require.NoError(t, err)
			assert.Equal(t, id, cid)
		}
		assert.Zero(t, rt.searchN)
		assert.False(t, bindResolvedCID("1"+id, id, true))
	}
}
