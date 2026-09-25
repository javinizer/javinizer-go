package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rawHSearchServer serves the URL finder's search page with the given product
// cid and the product page with the given 品番 row (empty pinzan omits it).
func rawHSearchServer(cid, pinzan string) *remasterRoundTripper {
	page := `<html><body><h1 id="title" class="item">Remaster</h1></body></html>`
	if pinzan != "" {
		page = `<html><body><h1 id="title" class="item">Remaster</h1>` +
			`<table><tr><td>品番：</td><td>` + pinzan + `</td></tr></table></body></html>`
	}
	return &remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "/search/="):
			return 200, `<html><body><a href="/digital/videoa/-/detail/=/cid=` + cid + `/">remaster</a></body></html>`
		case strings.Contains(u, "cid="+cid):
			return 200, page
		}
		return 404, ""
	}}
}

// Round-29b: a raw H/HD content-id query (1rct00156h) echoes itself as the
// resolved cid, so a fetched page whose 品番 numbers another release
// (RCT-157-HD) means DMM followed a redirect or served a different product:
// Search must miss honestly instead of returning release 157's metadata under
// the cid-derived release 156 identity, matching the ScrapeURL-side
// pageDisplayIdentityForCID gates.
func TestSearchRawHCIDWrongPageNumberRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawHSearchServer("1rct00156h", "RCT-157-HD"))

	res, err := s.Search(context.Background(), "1rct00156h")
	require.Error(t, err, "the raw-H query's page numbers release 157 and must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// The same wrong-number conflict from an unpadded raw query whose resolved
// URL cid is padded: the padding-normalized number still binds the release.
func TestSearchRawHCIDUnpaddedQueryWrongPageNumberRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawHSearchServer("1rct00156h", "RCT-157-HD"))

	res, err := s.Search(context.Background(), "1rct156h")
	require.Error(t, err, "the unpadded raw query's page numbers release 157 and must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// The matching-page counterpart: the same raw query whose page publishes the
// cid's own release (RCT-156-HD) keeps returning normally.
func TestSearchRawHCIDMatchingPageReturns(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawHSearchServer("1rct00156h", "RCT-156-HD"))

	res, err := s.Search(context.Background(), "1rct00156h")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "1rct00156h", res.ContentID)
	assert.Equal(t, "RCT-156H", res.ID)
}

// A raw H/HD query whose page publishes no 品番 row keeps the cid-derived
// identity: nothing authoritative is published to conflict with.
func TestSearchRawHCIDNoPageIdentityReturns(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawHSearchServer("1rct00156h", ""))

	res, err := s.Search(context.Background(), "1rct00156h")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "1rct00156h", res.ContentID)
	assert.Equal(t, "RCT-156H", res.ID)
}

// The round-25b rule on the raw path: a markerless 品番 names the base
// release, not the remaster the marker-bearing cid asks for, so the page is
// the base product's and must miss honestly.
func TestSearchRawHCIDMarkerlessPageRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawHSearchServer("1rct00156h", "RCT-157"))

	res, err := s.Search(context.Background(), "1rct00156h")
	require.Error(t, err, "the markerless 品番 names the base release and must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// The round-20a rule on the raw path: a parseable 品番 belonging to a foreign
// series proves the page is another product's, so the whole page conflicts.
func TestSearchRawHCIDForeignSeriesPageRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawHSearchServer("1rct00156h", "ABC-156-HD"))

	res, err := s.Search(context.Background(), "1rct00156h")
	require.Error(t, err, "the foreign-series 品番 proves another product's page and must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// The underscore-prefixed raw cid shape takes the same gate: h_003rct00156h
// is RCT-156H, so its page must number 156.
func TestSearchRawHCIDUnderscorePrefixWrongPageNumberRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawHSearchServer("h_003rct00156h", "RCT-157-HD"))

	res, err := s.Search(context.Background(), "h_003rct00156h")
	require.Error(t, err, "the underscore-prefixed raw query's page numbers release 157 and must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// Raw AI queries keep their number-free behavior (round-27): AI cid numbers
// diverge from the display number by design, so the page 品番 still outranks
// the cid-derived spelling — matching pages keep their identity and even a
// differently-numbered same-series row is adopted, never gated.
func TestSearchRawAICIDPageOutranksKept(t *testing.T) {
	for _, tc := range []struct{ name, pinzan, wantID string }{
		{"matching page keeps identity", "DV-818-AI", "DV-818AI"},
		{"foreign-numbered page still adopts the page identity", "DV-819-AI", "DV-819AI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newRemasterTestScraper(t)
			s.client.SetTransport(rawHSearchServer("dv00899ai", tc.pinzan))

			res, err := s.Search(context.Background(), "dv00899ai")
			require.NoError(t, err, "raw AI queries keep the number-free behavior")
			require.NotNil(t, res)
			assert.Equal(t, "dv00899ai", res.ContentID)
			assert.Equal(t, tc.wantID, res.ID)
		})
	}
}

// The raw-AI markerless case is likewise untouched: only the H/HD raw gate
// was added, so an AI page without a 品番 row still returns with the empty
// page-derived identity the verbatim path publishes today.
func TestSearchRawAICIDNoPageIdentityReturns(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawHSearchServer("dv00899ai", ""))

	res, err := s.Search(context.Background(), "dv00899ai")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "dv00899ai", res.ContentID)
	assert.Empty(t, res.ID, "AI cids do not encode the display number; identity requires the page 品番")
}

// The raw-H gate itself: pageDisplayIdentityForCID's conflict verdicts for
// the cid the URL carries, skipped for non-H cids and URL-less pages.
func TestRawHCIDPageConflict(t *testing.T) {
	page := func(display string) *goquery.Document {
		t.Helper()
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(
			"<html><body><table><tr><td>品番：</td><td>" + display + "</td></tr></table></body></html>"))
		require.NoError(t, err)
		return doc
	}
	rctURL := "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=1rct00156h/"
	for _, tc := range []struct {
		name string
		doc  *goquery.Document
		url  string
		want bool
	}{
		{"wrong number conflicts", page("RCT-157-HD"), rctURL, true},
		{"matching number passes", page("RCT-156-HD"), rctURL, false},
		{"markerless row conflicts", page("RCT-157"), rctURL, true},
		{"foreign series conflicts", page("ABC-156-HD"), rctURL, true},
		{"no pinzan row passes", page("???"), rctURL, false},
		{"nil document passes", nil, rctURL, false},
		{"ai cid is never gated", page("DV-819-AI"), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=dv00899ai/", false},
		{"cid-less URL passes", page("RCT-157-HD"), "https://www.dmm.co.jp/digital/videoa/-/list/=/article=keyword/", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, rawHCIDPageConflict(tc.doc, tc.url))
		})
	}
}
