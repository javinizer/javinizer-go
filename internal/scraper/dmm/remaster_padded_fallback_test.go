package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Padded display queries keep the deliberate verification contract: an AI
// candidate whose page cannot publish a 品番 stays unresolvable (the AI cid
// digits do not encode the display number, so only the page may prove the
// release), while a zero-padded query against a normal marker page keeps the
// page's zero-trimmed display value.
func TestSearchPaddedDisplayFallback(t *testing.T) {
	t.Run("ai page without pinzan cannot prove the release", func(t *testing.T) {
		s, _ := newRemasterTestScraper(t)
		rt := &remasterRoundTripper{serve: func(u string) (int, string) {
			switch {
			case strings.Contains(u, "/search/="):
				return 200, `<html><body><a href="/digital/videoa/-/detail/=/cid=dv00899ai/">remaster</a></body></html>`
			case strings.Contains(u, "cid=dv00899ai"):
				return 200, `<html><body><h1 id="title" class="item">AI Remaster</h1></body></html>`
			}
			return 404, ""
		}}
		s.client.SetTransport(rt)
		_, err := s.Search(context.Background(), "DV-818AI")
		require.Error(t, err, "AI cids do not encode the display number; without a page identity the candidate must not resolve")
		assert.Contains(t, err.Error(), "could not verify")
	})

	t.Run("page pinzan outranks padded query spelling", func(t *testing.T) {
		s, _ := newRemasterTestScraper(t)
		rt := &remasterRoundTripper{serve: func(u string) (int, string) {
			switch {
			case strings.Contains(u, "/search/="):
				return 200, `<html><body><a href="/digital/videoa/-/detail/=/cid=rct00156h/">remaster</a></body></html>`
			case strings.Contains(u, "cid=rct00156h"):
				return 200, `<html><body><h1 id="title" class="item">AI Remaster</h1><table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table></body></html>`
			}
			return 404, ""
		}}
		s.client.SetTransport(rt)
		res, err := s.Search(context.Background(), "RCT-00156-HD")
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, "RCT-156H", res.ID, "page zero-trimmed value wins over the padded query spelling")
	})
}

// A cached content-id mapping short-circuits on-line verification; when the
// AI cid page carries no 品番 row the parse publishes an empty display id and
// Search fills it from the query's canonical spelling.
func TestSearchAIFallbackFillsEmptyPageID(t *testing.T) {
	s, repo := newRemasterTestScraper(t)
	require.NoError(t, repo.Create(context.Background(), &models.ContentIDMapping{
		SearchID:  "DV-818-AI",
		ContentID: "dv00899ai",
		Source:    "dmm",
	}))
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "cid=dv00899ai"):
			return 200, "<html><body><h1 id=\"title\" class=\"item\">AI Remaster</h1></body></html>"
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)

	res, err := s.Search(context.Background(), "DV-818-AI")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "DV-818AI", res.ID, "canonical query spelling fills the empty page id")
}
