package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// The dmm-side cache analog of the r18dev number-binding fixes: an H/HD
// remaster cid keeps the display number (1rct00156h is RCT-156H), so a stale
// same-series cached mapping for a different release must not short-circuit
// the verified resolver. AI mappings stay number-free (dv00899ai is
// DV-818AI).
func TestCachedRemasterIdentityNumberBinding(t *testing.T) {
	assert.False(t, cachedRemasterIdentityMatches("RCT-156H", "1rct00157h", "h", "rct", "", false),
		"a stale same-series H mapping for release 157 must not satisfy RCT-156H")
	assert.True(t, cachedRemasterIdentityMatches("RCT-156H", "1rct00156h", "h", "rct", "", false),
		"the correct mapping is accepted from cache")
	assert.True(t, cachedRemasterIdentityMatches("RCT-00156-HD", "1rct00156h", "h", "rct", "", false),
		"a zero-padded query spelling binds the padding-normalized cached number")
	assert.False(t, cachedRemasterIdentityMatches("RCT-00157-HD", "1rct00156h", "h", "rct", "", false),
		"padded stale spellings stay distinct from the cached release")

	// AI mappings keep the number-free acceptance: the cid number is
	// unrelated to the display number, so both the correct mapping and a
	// number-divergent sibling are accepted per current behavior.
	assert.True(t, cachedRemasterIdentityMatches("DV-818AI", "dv00899ai", "ai", "dv", "", false))
	assert.True(t, cachedRemasterIdentityMatches("DV-818AI", "dv00999ai", "ai", "dv", "", false),
		"AI cids cannot number-bind; the marker gate is the strongest check")

	// The t28/t display readings bind on both sides: t28123h is T-28123H,
	// while 9t28123h and h_003t28123h are T28-123H.
	assert.True(t, cachedRemasterIdentityMatches("T-28123-HD", "t28123h", "h", "t", "", false))
	assert.True(t, cachedRemasterIdentityMatches("T28-123-HD", "9t28123h", "h", "t28", "", false))
	assert.True(t, cachedRemasterIdentityMatches("T28-123-HD", "h_003t28123h", "h", "t28", "", false))
	assert.False(t, cachedRemasterIdentityMatches("T28-123-HD", "9t2800124h", "h", "t28", "", false),
		"a stale t28-series mapping for a different release is rejected")

	// A query without a parseable number keeps the marker-based acceptance.
	assert.True(t, cachedRemasterIdentityMatches("x", "1rct00156h", "h", "rct", "", false))
}

// A stale same-series cached H mapping must not short-circuit resolution: the
// query re-resolves through the verified resolver and the cache is repaired
// with the release the page display proves.
func TestResolveContentID_RejectsStaleCachedNumber(t *testing.T) {
	s, repo := newRemasterTestScraper(t)
	require.NoError(t, repo.Create(context.Background(), &models.ContentIDMapping{
		SearchID:  "RCT-156H",
		ContentID: "1rct00157h",
		Source:    "dmm",
	}))
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "/search/="):
			return 200, `<html><body><a href="/digital/videoa/-/detail/=/cid=1rct00156h/">remaster</a></body></html>`
		case strings.Contains(u, "cid=1rct00156h"):
			return 200, `<html><body><table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table></body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)

	cid, err := s.ResolveContentIDCtx(context.Background(), "RCT-156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cid, "the stale cached mapping must not win; release 156 resolves")
	assert.Positive(t, rt.searchN, "the stale mapping must not skip resolution")

	cached, err := repo.FindBySearchID(context.Background(), "RCT-156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cached.ContentID, "the verified release replaces the stale mapping")
}

// The correct same-series mapping still wins from cache: no resolution
// traffic runs for RCT-156H -> 1rct00156h.
func TestResolveContentID_AcceptsNumberBoundCache(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.cacheContentID(context.Background(), "RCT-156H", "1rct00156h")
	rt := &remasterRoundTripper{serve: func(u string) (int, string) { return 404, "" }}
	s.client.SetTransport(rt)

	cid, err := s.ResolveContentIDCtx(context.Background(), "RCT-156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cid)
	assert.Empty(t, rt.hits, "the number-bound mapping must skip resolution")
}

// AI mappings stay number-free in the cache gate: the unrelated cid digits
// (dv00899ai for DV-818AI) still satisfy the query from cache.
func TestResolveContentID_AICacheStaysNumberFree(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.cacheContentID(context.Background(), "DV-818AI", "dv00899ai")
	rt := &remasterRoundTripper{serve: func(u string) (int, string) { return 404, "" }}
	s.client.SetTransport(rt)

	cid, err := s.ResolveContentIDCtx(context.Background(), "DV-818AI")
	require.NoError(t, err)
	assert.Equal(t, "dv00899ai", cid)
	assert.Empty(t, rt.hits, "AI mappings keep the number-free cache acceptance")
}
