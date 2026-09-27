package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rawCIDSearchServer serves the URL finder's search page with the given
// product cid and the product page with the given 品番 row (empty pinzan
// omits it).
func rawCIDSearchServer(cid, pinzan string) *remasterRoundTripper {
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
	s.client.SetTransport(rawCIDSearchServer("1rct00156h", "RCT-157-HD"))

	res, err := s.Search(context.Background(), "1rct00156h")
	require.Error(t, err, "the raw-H query's page numbers release 157 and must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// The same wrong-number conflict from an unpadded raw query whose resolved
// URL cid is padded: the padding-normalized number still binds the release.
func TestSearchRawHCIDUnpaddedQueryWrongPageNumberRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawCIDSearchServer("1rct00156h", "RCT-157-HD"))

	res, err := s.Search(context.Background(), "1rct156h")
	require.Error(t, err, "the unpadded raw query's page numbers release 157 and must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// The matching-page counterpart: the same raw query whose page publishes the
// cid's own release (RCT-156-HD) keeps returning normally.
func TestSearchRawHCIDMatchingPageReturns(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawCIDSearchServer("1rct00156h", "RCT-156-HD"))

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
	s.client.SetTransport(rawCIDSearchServer("1rct00156h", ""))

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
	s.client.SetTransport(rawCIDSearchServer("1rct00156h", "RCT-157"))

	res, err := s.Search(context.Background(), "1rct00156h")
	require.Error(t, err, "the markerless 品番 names the base release and must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// The round-20a rule on the raw path: a parseable 品番 belonging to a foreign
// series proves the page is another product's, so the whole page conflicts.
func TestSearchRawHCIDForeignSeriesPageRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawCIDSearchServer("1rct00156h", "ABC-156-HD"))

	res, err := s.Search(context.Background(), "1rct00156h")
	require.Error(t, err, "the foreign-series 品番 proves another product's page and must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// The underscore-prefixed raw cid shape takes the same gate: h_003rct00156h
// is RCT-156H, so its page must number 156.
func TestSearchRawHCIDUnderscorePrefixWrongPageNumberRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawCIDSearchServer("h_003rct00156h", "RCT-157-HD"))

	res, err := s.Search(context.Background(), "h_003rct00156h")
	require.Error(t, err, "the underscore-prefixed raw query's page numbers release 157 and must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// Raw AI queries keep their number-free behavior (round-27, F1): AI cid
// numbers diverge from the display number by design, so the page 品番
// still outranks the cid-derived spelling — matching pages keep their
// identity and even a differently-numbered same-series row is adopted. Only
// rows that provably belong to another product — a foreign series, a
// markerless base release, a marker or catalog-suffix swap — are gated (the
// F1 tests below).
func TestSearchRawAICIDPageOutranksKept(t *testing.T) {
	for _, tc := range []struct{ name, pinzan, wantID string }{
		{"matching page keeps identity", "DV-818-AI", "DV-818AI"},
		{"foreign-numbered page still adopts the page identity", "DV-819-AI", "DV-819AI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newRemasterTestScraper(t)
			s.client.SetTransport(rawCIDSearchServer("dv00899ai", tc.pinzan))

			res, err := s.Search(context.Background(), "dv00899ai")
			require.NoError(t, err, "raw AI queries keep the number-free behavior")
			require.NotNil(t, res)
			assert.Equal(t, "dv00899ai", res.ContentID)
			assert.Equal(t, tc.wantID, res.ID)
		})
	}
}

// A raw-AI page without a 品番 row publishes nothing authoritative to
// conflict with, so it still returns with the empty page-derived identity
// the verbatim path publishes.
func TestSearchRawAICIDNoPageIdentityReturns(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawCIDSearchServer("dv00899ai", ""))

	res, err := s.Search(context.Background(), "dv00899ai")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "dv00899ai", res.ContentID)
	assert.Empty(t, res.ID, "AI cids do not encode the display number; identity requires the page 品番")
}

// F1: a raw AI content-id query echoes itself as the resolved cid, so a
// page whose 品番 provably belongs to another product — a foreign series
// (ABC-999-AI) — must miss honestly like the raw-H gate instead of
// returning the foreign product's metadata under the queried content id
// with an empty display id, matching the ScrapeURL-side hard miss
// (TestScrapeURLMarkerCIDForeignIdentityPageRejected).
func TestSearchRawAICIDForeignSeriesPageRejected(t *testing.T) {
	for _, tc := range []struct{ name, cid, pinzan string }{
		{"foreign series on a dv cid", "dv00899ai", "ABC-999-AI"},
		{"foreign series on an rct cid", "1rct00156ai", "ABC-999-AI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newRemasterTestScraper(t)
			s.client.SetTransport(rawCIDSearchServer(tc.cid, tc.pinzan))

			res, err := s.Search(context.Background(), tc.cid)
			require.Error(t, err, "the foreign-series 品番 proves another product's page and must miss honestly")
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), "different release")
		})
	}
}

// F1 + round-25b on the raw-AI path: a markerless 品番 names the base
// release — the queried cid's own (DV-818) or a neighbor's (RCT-157) — not
// the AI remaster the marker-bearing cid asks for, so the page is the base
// product's and must miss honestly.
func TestSearchRawAICIDMarkerlessPageRejected(t *testing.T) {
	for _, tc := range []struct{ name, cid, pinzan string }{
		{"neighbor base release", "dv00899ai", "RCT-157"},
		{"own base release", "dv00899ai", "DV-818"},
		{"rct cid serving the base release", "1rct00156ai", "RCT-157"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newRemasterTestScraper(t)
			s.client.SetTransport(rawCIDSearchServer(tc.cid, tc.pinzan))

			res, err := s.Search(context.Background(), tc.cid)
			require.Error(t, err, "the markerless 品番 names the base release and must miss honestly")
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), "different release")
		})
	}
}

// F1 + rounds 25b/26 on the raw-AI path: a parseable 品番 naming the query's
// series under a different marker line or catalog suffix — the HD remaster
// (DV-819-HD) or an E-suffixed row — belongs to another product, so the
// page must miss honestly instead of publishing its metadata under the AI
// cid with an empty display id.
func TestSearchRawAICIDMarkerSwapPageRejected(t *testing.T) {
	for _, tc := range []struct{ name, cid, pinzan string }{
		{"hd marker under a dv ai cid", "dv00899ai", "DV-819-HD"},
		{"hd marker under an rct ai cid", "1rct00156ai", "RCT-156-HD"},
		{"catalog-suffixed ai row", "dv00899ai", "DV-819-E-AI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newRemasterTestScraper(t)
			s.client.SetTransport(rawCIDSearchServer(tc.cid, tc.pinzan))

			res, err := s.Search(context.Background(), tc.cid)
			require.Error(t, err, "the marker-swapped 品番 belongs to another product and must miss honestly")
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), "different release")
		})
	}
}

// The same conflict from an unpadded raw AI query whose resolved URL cid is
// padded: the padding-normalized echo binds the release, and the conflict
// verdict is number-free either way.
func TestSearchRawAICIDUnpaddedQueryConflictingPageRejected(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawCIDSearchServer("1rct00156ai", "RCT-157"))

	res, err := s.Search(context.Background(), "1rct156ai")
	require.Error(t, err, "the unpadded raw AI query's page names the base release and must miss honestly")
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "different release")
}

// F1's accept path: a page 品番 matching the raw AI query's series and AI
// marker returns normally with the page-proved display id.
func TestSearchRawAICIDMatchingPageReturns(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.client.SetTransport(rawCIDSearchServer("dv00899ai", "DV-818-AI"))

	res, err := s.Search(context.Background(), "dv00899ai")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "dv00899ai", res.ContentID)
	assert.Equal(t, "DV-818AI", res.ID)
}

// The raw-cid gate itself: pageDisplayIdentityForCID's conflict verdicts for
// the marker-bearing cid the URL carries, skipped for markerless and absent
// cids.
func TestRawRemasterCIDPageConflict(t *testing.T) {
	page := func(display string) *goquery.Document {
		t.Helper()
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(
			"<html><body><table><tr><td>品番：</td><td>" + display + "</td></tr></table></body></html>"))
		require.NoError(t, err)
		return doc
	}
	rctURL := "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=1rct00156h/"
	dvAIURL := "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=dv00899ai/"
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
		{"ai cid adopts a same-series number", page("DV-819-AI"), dvAIURL, false},
		{"ai cid foreign series conflicts", page("ABC-999-AI"), dvAIURL, true},
		{"ai cid markerless row conflicts", page("RCT-157"), dvAIURL, true},
		{"ai cid marker swap conflicts", page("RCT-157-HD"), dvAIURL, true},
		{"markerless cid is never gated", page("RCT-157-HD"), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=1rct00156/", false},
		{"cid-less URL passes", page("RCT-157-HD"), "https://www.dmm.co.jp/digital/videoa/-/list/=/article=keyword/", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, rawRemasterCIDPageConflict(tc.doc, tc.url))
		})
	}
}
