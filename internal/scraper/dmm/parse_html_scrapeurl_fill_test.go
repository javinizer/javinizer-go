package dmm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ScrapeURL follows the marker-aware parser's AI rule end to end: AI-remaster
// cid numbers are unrelated to display numbers (dv00899ai maps to DV-818AI),
// so a page without an authoritative 品番 row publishes an empty display ID
// instead of a cid-derived spelling that would misfile the release.
func TestScrapeURLAICIDWithoutPinzanLeavesIDEmpty(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		return 200, `<html><body><h1 id="title" class="item">AI Remaster</h1></body></html>`
	}})

	res, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=dv00899ai/")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "dv00899ai", res.ContentID)
	assert.Empty(t, res.ID, "no page 品番 and an AI cid must leave the display id unset")
}

// HD cids already derive their display id from the cid; the fill must not
// interfere. A page with a 品番 row keeps outranking any derived spelling
// (pinned by TestScrapeURLMarkerModeFromURLCid).
func TestScrapeURLHDCIDKeepsDerivedID(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		return 200, `<html><body><h1 id="title" class="item">HD Remaster</h1></body></html>`
	}})

	res, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "1rct00156h", res.ContentID)
	assert.Equal(t, "RCT-156H", res.ID)
}

// The ScrapeURL-side analog of the round-16a search identity guard: H/HD
// remaster cids keep the display number, so a 品番 row numbering another
// release — DMM followed a redirect or served a mismatched page for
// cid=1rct00156h under RCT-157-HD — must not replace the cid-derived
// identity. The row is ignored like pageRemasterDisplayID's other mismatch
// cases, keeping ContentID and ID a coherent pair for the queried release.
func TestScrapeURLHDCIDMismatchedPageKeepsDerivedID(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		return 200, `<html><body><table><tr><td>品番：</td><td>RCT-157-HD</td></tr></table></body></html>`
	}})

	res, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "1rct00156h", res.ContentID)
	assert.NotEqual(t, "RCT-157H", res.ID, "a number-mismatched page 品番 must not re-key the release")
	assert.Equal(t, "RCT-156H", res.ID, "the cid-derived identity is kept")
}

// A matching 品番 still outranks the derived spelling, padding differences
// included: RCT-156-HD, RCT-156H and the zero-padded RCT-00156-HD all number
// the cid's release.
func TestScrapeURLHDCIDMatchingPageStillOverrides(t *testing.T) {
	for _, tc := range []struct{ name, pinzan string }{
		{"hyphenated HD spelling", "RCT-156-HD"},
		{"compact H spelling", "RCT-156H"},
		{"zero-padded page spelling", "RCT-00156-HD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newRemasterTestScraper(t)
			s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
				return 200, `<html><body><table><tr><td>品番：</td><td>` + tc.pinzan + `</td></tr></table></body></html>`
			}})
			res, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/")
			require.NoError(t, err)
			require.NotNil(t, res)
			assert.Equal(t, "1rct00156h", res.ContentID)
			assert.Equal(t, "RCT-156H", res.ID)
		})
	}
}

// AI cid numbers are unrelated to display numbers, so the number guard must
// not apply: the page 品番 stays authoritative and keeps outranking the cid.
func TestScrapeURLAICIDPageStillOutranksCID(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		return 200, `<html><body><table><tr><td>品番：</td><td>DV-818-AI</td></tr></table></body></html>`
	}})

	res, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=dv00899ai/")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "dv00899ai", res.ContentID)
	assert.Equal(t, "DV-818AI", res.ID, "the divergent page 品番 keeps outranking the cid spelling")
}
