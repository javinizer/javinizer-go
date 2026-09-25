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
// cid=1rct00156h under RCT-157-HD — means the page is for the wrong product.
// Declining the page-ID override is not enough: release 157's title and media
// must not be labeled RCT-156H via fillMarkerIDFromURL, so the whole page is
// a hard miss instead of a result carrying the URL-derived identity.
func TestScrapeURLHDCIDMismatchedPageRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		return 200, `<html><body><table><tr><td>品番：</td><td>RCT-157-HD</td></tr></table></body></html>`
	}})

	res, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/")
	require.Error(t, err, "a page publishing another release's 品番 must be a hard miss")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
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

// Round-20a: a direct H/HD url redirected to a page whose parseable 品番
// belongs to another series or marker line — cid=1rct00156h serving
// ABC-999-HD — must reject the whole page: merely dropping the page-ID
// override would still label the foreign product's metadata with the
// URL-derived RCT-156H via fillMarkerIDFromURL. A foreign series on an AI
// url conflicts the same way: AI numbers diverge from the cid by design, so
// the number guard never applies, but the series/marker identity never
// diverges between an AI cid and its display.
func TestScrapeURLMarkerCIDForeignIdentityPageRejected(t *testing.T) {
	for _, tc := range []struct{ name, url, pinzan string }{
		{"foreign series row", "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/", "ABC-999-HD"},
		{"foreign marker line row", "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/", "RCT-156-AI"},
		{"foreign catalog suffix row", "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1ipx00535zh/", "IPX-535-HD"},
		{"foreign series row on an ai url", "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=dv00899ai/", "RCT-156H"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newRemasterTestScraper(t)
			s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
				return 200, `<html><body><table><tr><td>品番：</td><td>` + tc.pinzan + `</td></tr></table></body></html>`
			}})

			res, err := s.ScrapeURL(context.Background(), tc.url)
			require.Error(t, err, "a page publishing another product's 品番 must be a hard miss")
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), "different release")
		})
	}
}

// Round-25b: a marker-bearing url redirected to a markerless product page —
// cid=1rct00156h serving 品番 RCT-157 — must reject the whole page. A
// nonempty markerless row names the base release, not the remaster, so
// keeping the cid-derived spelling would label release 157's metadata
// RCT-156H via fillMarkerIDFromURL. The row naming the cid's own base
// release conflicts identically (it is still not the remaster), compact
// spellings parse the same, and an AI url conflicts too — the request is
// marker-bearing. Empty or absent rows keep the pass-through pinned by
// TestScrapeURLHDCIDKeepsDerivedID and TestScrapeURLAICIDWithoutPinzan.
func TestScrapeURLMarkerCIDMarkerlessRowRejected(t *testing.T) {
	for _, tc := range []struct{ name, url, pinzan string }{
		{"h url serving the next base release", "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/", "RCT-157"},
		{"h url serving compact markerless spelling", "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/", "RCT157"},
		{"h url serving its own base release", "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/", "RCT-156"},
		{"ai url serving the base release", "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=dv00899ai/", "DV-818"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newRemasterTestScraper(t)
			s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
				return 200, `<html><body><table><tr><td>品番：</td><td>` + tc.pinzan + `</td></tr></table></body></html>`
			}})

			res, err := s.ScrapeURL(context.Background(), tc.url)
			require.Error(t, err, "a markerless 品番 names the base release, not the remaster: the page must be a hard miss")
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), "different release")
		})
	}
}
