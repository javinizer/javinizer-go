package dmm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

type remasterRoundTripper struct {
	serve   func(url string) (int, string)
	hits    []string
	detailN int
	searchN int
}

func (rt *remasterRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	u := req.URL.String()
	rt.hits = append(rt.hits, u)
	if strings.Contains(u, "/search/=") {
		rt.searchN++
	}
	if strings.Contains(u, "/detail/=") {
		rt.detailN++
	}
	status, body := rt.serve(u)
	h := make(http.Header)
	h.Set("Content-Type", "text/html")
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func newRemasterTestScraper(t *testing.T) (*scraper, *database.ContentIDMappingRepository) {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := database.NewContentIDMappingRepository(db)
	settings := createTestSettings(true, nil)
	s := newScraper(&settings, testGlobalProxy, testGlobalFlareSolverr, dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})
	return s, repo
}

func TestCanonicalRemasterDisplayID(t *testing.T) {
	assert.Equal(t, "DV-818AI", canonicalRemasterDisplayID("DV-818AI"))
	assert.Equal(t, "RCT-156H", canonicalRemasterDisplayID("RCT-156-HD"))
	assert.Equal(t, "RCT-156H", canonicalRemasterDisplayID("rct-156h"))
	assert.Equal(t, "PLAIN", canonicalRemasterDisplayID("plain"))
	assert.Equal(t, "IPX-535", canonicalRemasterDisplayID("IPX-535"))
	assert.Equal(t, "IPX-535ZH", canonicalRemasterDisplayID("IPX-535Z-HD"))
	assert.Equal(t, "IPX-535ZH", canonicalRemasterDisplayID("IPX-535ZH"))
}

func TestRemasterClassification(t *testing.T) {
	cases := []struct {
		id        string
		marker    string
		series    string
		contentID bool
	}{
		{"RCT-156H", "h", "rct", false},
		{"RCT-156-HD", "h", "rct", false},
		{"rct156h", "h", "rct", false},
		{"DV-818AI", "ai", "dv", false},
		{"1RCT00156H", "h", "rct", true},
		{"1rct00156h", "h", "rct", true},
		{"dv00899ai", "ai", "dv", true},
		{"53dv899", "", "", true},
		{"1rct00156", "", "", true},
		{"h_1472smkcx003", "", "", true},
		{"RCT-156", "", "", false},
		{"IPX-535", "", "", false},
		{"ABP1234", "", "", false},
		{"LULU00441", "", "", true},
		{"118IPX00535", "", "", true},
		{"oreco183", "", "", false},
		{"ABC-1234H", "h", "abc", false},
		{"AbC-1234-Ai", "ai", "abc", false},
		{"IPX-535Z-HD", "h", "ipx", false},
		{"IPX-535ZH", "h", "ipx", false},
		{"1ipx00535zh", "h", "ipx", true},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			m, series, _, isCID := classifyRemasterQuery(tc.id)
			assert.Equal(t, tc.marker, m, "marker")
			assert.Equal(t, tc.series, series, "series")
			assert.Equal(t, tc.contentID, isCID, "isContentID")
		})
	}
}

func TestRemasterMarkerRentalStrip(t *testing.T) {
	cases := map[string]string{
		"1rct00156hr":  "1rct00156h",
		"1rct00156hdr": "1rct00156hd",
		"dv00899air":   "dv00899ai",
		"abc123r":      "abc123",
		"1rct00156h":   "1rct00156h",
		"oreco183":     "oreco183",
	}
	for in, want := range cases {
		assert.Equal(t, want, stripRentalSuffixMarkerAware(in), in)
	}
}

func TestBindResolvedCID(t *testing.T) {
	cases := []struct {
		urlCID   string
		resolved string
		want     bool
	}{
		{"1rct00156h", "1rct00156h", true},
		{"rct00156h", "1rct00156h", true},
		{"1rct00156hr", "1rct00156h", true},
		{"1rct00156hd", "1rct00156h", false},
		{"1rct00156", "1rct00156h", false},
		{"2rct00156h", "1rct00156h", true},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, bindResolvedCID(tc.urlCID, tc.resolved, false), "%s vs %s", tc.urlCID, tc.resolved)
	}
}

func TestResolveRemasterContentID_SingleCandidate(t *testing.T) {
	s, repo := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "/search/="):
			return 200, `<html><body>` +
				`<a href="/digital/videoa/-/detail/=/cid=1rct00156h/">remaster</a>` +
				`<a href="/digital/videoa/-/detail/=/cid=1rct00156/">base original</a>` +
				`</body></html>`
		case strings.Contains(u, "cid=1rct00156h"):
			return 200, `<html><body><table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table></body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)

	cid, err := s.ResolveContentIDCtx(context.Background(), "RCT-156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cid)
	assert.Greater(t, rt.detailN, 0, "singleton candidates are display-verified too")

	cached, err := repo.FindBySearchID(context.TODO(), "RCT-156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cached.ContentID)
}

func TestResolveRemasterContentID_RentalOnlyDiscovery(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "/search/="):
			return 200, `<html><body><a href="/rental/ppr/-/detail/=/cid=1rct00156hr/">rental</a></body></html>`
		case strings.Contains(u, "cid=1rct00156hr"):
			return 200, `<html><body><table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table></body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)

	cid, err := s.ResolveContentIDCtx(context.Background(), "RCT-156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cid)
}

func TestResolveRemasterContentID_AmbiguityVerifiedByDisplayID(t *testing.T) {
	for _, reversed := range []bool{false, true} {
		s, _ := newRemasterTestScraper(t)
		anchors := []string{
			`<a href="/mono/dvd/-/detail/=/cid=dv00899ai/">A</a>`,
			`<a href="/mono/dvd/-/detail/=/cid=dv00123ai/">B</a>`,
		}
		if reversed {
			anchors[0], anchors[1] = anchors[1], anchors[0]
		}
		rt := &remasterRoundTripper{serve: func(u string) (int, string) {
			switch {
			case strings.Contains(u, "/search/="):
				return 200, "<html><body>" + strings.Join(anchors, "") + "</body></html>"
			case strings.Contains(u, "cid=dv00899ai"):
				return 200, `<html><body><table><tr><td>品番：</td><td>DV-818AI</td></tr><tr><td>商品番号：</td><td>dv00899ai</td></tr></table></body></html>`
			case strings.Contains(u, "cid=dv00123ai"):
				return 200, `<html><body><table><tr><td>品番：</td><td>DV-123AI</td></tr></table></body></html>`
			}
			return 404, ""
		}}
		s.client.SetTransport(rt)

		cid, err := s.ResolveContentIDCtx(context.Background(), "DV-818AI")
		require.NoError(t, err, "reversed=%v", reversed)
		assert.Equal(t, "dv00899ai", cid, "reversed=%v", reversed)
		assert.Greater(t, rt.detailN, 0, "verification fetches expected")
	}
}

func TestResolveRemasterContentID_AmbiguityUnverifiable(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "/search/="):
			return 200, `<html><body><a href="/mono/dvd/-/detail/=/cid=dv00899ai/">A</a><a href="/mono/dvd/-/detail/=/cid=dv00123ai/">B</a></body></html>`
		case strings.Contains(u, "/detail/="):
			return 200, `<html><body><table><tr><td>商品番号：</td><td>dv00899ai</td></tr></table></body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)

	_, err := s.ResolveContentIDCtx(context.Background(), "DV-818AI")
	require.Error(t, err)
}

func TestResolveContentID_BypassCachesVerbatimAndSkipsSearch(t *testing.T) {
	s, repo := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) { return 404, "" }}
	s.client.SetTransport(rt)

	cid, err := s.ResolveContentIDCtx(context.Background(), "1RCT00156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cid)
	assert.Equal(t, 0, rt.searchN, "content-id bypass must not run resolution search")

	cached, err := repo.FindBySearchID(context.TODO(), "1RCT00156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cached.ContentID)

}

func TestResolveContentID_BypassRejectsCacheConflict(t *testing.T) {
	s, repo := newRemasterTestScraper(t)
	require.NoError(t, repo.Create(context.Background(), &models.ContentIDMapping{SearchID: "1RCT00156H", ContentID: "zzzz999", Source: "dmm"}))
	rt := &remasterRoundTripper{serve: func(u string) (int, string) { return 404, "" }}
	s.client.SetTransport(rt)

	cid, err := s.ResolveContentIDCtx(context.Background(), "1RCT00156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cid)
	assert.Empty(t, rt.hits)
}

func TestResolveContentID_BaseQueryShortestWins_Preserved(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "/search/=") {
			return 200, `<html><body>` +
				`<a href="/digital/videoa/-/detail/=/cid=1rct00156h/">remaster</a>` +
				`<a href="/digital/videoa/-/detail/=/cid=1rct00156/">base</a>` +
				`</body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)

	cid, err := s.ResolveContentIDCtx(context.Background(), "RCT-156")
	require.NoError(t, err)
	assert.Equal(t, "rct00156", cid, "base query keeps shortest-candidate selection")
}

func TestGetURL_BindingRejectsForeignCID(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "/search/="):
			return 200, `<html><body>` +
				`<a href="/mono/dvd/-/detail/=/cid=1rct00156hd/">foreign hd variant</a>` +
				`<a href="/digital/videoa/-/detail/=/cid=1rct00156h/">remaster</a>` +
				`</body></html>`
		case strings.Contains(u, "cid=1rct00156hd"):
			return 200, `<html><body><table><tr><td>品番：</td><td>RCT-157-HD</td></tr></table></body></html>`
		case strings.Contains(u, "cid=1rct00156h"):
			return 200, `<html><body><table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table></body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)

	url, err := s.GetURL(context.Background(), "RCT-156H")
	require.NoError(t, err)
	assert.Contains(t, url, "cid=1rct00156h")
	assert.NotContains(t, url, "1rct00156hd")
}

func TestGetURL_MarkerFreeBypassUnbound(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "/search/=") {
			return 200, `<html><body><a href="/mono/dvd/-/detail/=/cid=53dv899/">base</a></body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)

	url, err := s.GetURL(context.Background(), "53dv899")
	require.NoError(t, err)
	assert.Contains(t, url, "cid=53dv899")
}

func TestPageRemasterDisplayID_RequiresSuffixMatch(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(
		`<html><body><table><tr><td>品番：</td><td>IPX-535-H</td></tr></table></body></html>`))
	require.NoError(t, err)

	assert.Equal(t, "IPX-535H", pageRemasterDisplayID(doc, "1ipx00535h", "ipx", "h", ""))
	assert.Equal(t, "", pageRemasterDisplayID(doc, "1ipx00535h", "ipx", "h", "e"), "page display missing the query's E/Z suffix must not be trusted")
}

// H/HD remaster cids keep the display number, so a same-series 品番 numbering
// another release (a redirect or a served mismatched page for cid=1rct00156h
// under RCT-157-HD) publishes no display id — and flags the page as a
// conflict so callers reject it instead of re-keying the release. A
// parseable marker-bearing row of a foreign identity (another series, catalog
// suffix or marker line, cid=1rct00156h under ABC-999-HD) conflicts the same
// way: the page provably belongs to another product. AI numbers diverge from
// the cid by design, so same-series AI rows keep outranking it, but a foreign
// row on an AI url is still another product's page and conflicts. A
// nonempty markerless row conflicts identically (round 25b), plain or
// E/Z-suffixed (round 26): it names the base release, never the remaster
// the marker-bearing cid asks for.
func TestPageRemasterDisplayID_RequiresNumberMatch(t *testing.T) {
	assert.Equal(t, "", pageRemasterDisplayID(doc2(t, "RCT-157-HD"), "1rct00156h", "rct", "h", ""),
		"page display numbering another release must not be trusted")
	assert.Equal(t, "", pageRemasterDisplayID(doc2(t, "ABC-999-HD"), "1rct00156h", "rct", "h", ""),
		"a foreign-series page display must not be trusted")
	assert.Equal(t, "RCT-156H", pageRemasterDisplayID(doc2(t, "RCT-156-HD"), "1rct00156h", "rct", "h", ""))
	// Padding differences normalize on both sides (cid 00156, page 0156).
	assert.Equal(t, "RCT-156H", pageRemasterDisplayID(doc2(t, "RCT-00156-HD"), "1rct00156h", "rct", "h", ""))
	assert.Equal(t, "DV-818AI", pageRemasterDisplayID(doc2(t, "DV-818-AI"), "dv00899ai", "dv", "ai", ""),
		"AI numbers diverge from the cid by design: the page stays authoritative")
	// A cid without a parseable number binds nothing beyond the marker-based
	// acceptance (mirroring cachedRemasterIdentityMatches).
	assert.Equal(t, "IPX-535H", pageRemasterDisplayID(doc2(t, "IPX-535-H"), "not-a-cid", "ipx", "h", ""))

	// The same rows are whole-page conflicts, not just unusable rows: DMM
	// followed a redirect or served another product for the cid, so the
	// page must be rejected instead of keeping the cid-derived identity.
	_, conflict := pageDisplayIdentityForCID(doc2(t, "RCT-157-HD"), "1rct00156h", "rct", "h", "")
	assert.True(t, conflict, "a same-line row numbering another release conflicts with the cid")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "ABC-999-HD"), "1rct00156h", "rct", "h", "")
	assert.True(t, conflict, "a foreign-series row conflicts with the cid: the page is another product's")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "RCT-156-AI"), "1rct00156h", "rct", "h", "")
	assert.True(t, conflict, "a foreign marker line conflicts with the cid: the AI remaster is another product")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "IPX-535-HD"), "1ipx00535zh", "ipx", "h", "z")
	assert.True(t, conflict, "a row missing the cid's catalog suffix conflicts: the page is another product's")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "RCT-156-HD"), "1rct00156h", "rct", "h", "")
	assert.False(t, conflict, "a row numbering the cid's release does not conflict")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "DV-819-AI"), "dv00899ai", "dv", "ai", "")
	assert.False(t, conflict, "AI numbers diverge by design: a number difference is never a conflict")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "RCT-156H"), "dv00899ai", "dv", "ai", "")
	assert.True(t, conflict, "a foreign-series row on an AI cid conflicts: the page is another product's")
	// A nonempty markerless 品番 names the base release, not the remaster the
	// marker-bearing cid asks for, so it conflicts like a foreign row — with
	// or without a number match (RCT-157 and RCT-156 both name the base
	// product, never the remaster) — while a markerless request keeps the
	// pass-through and unparseable rows stay ignored.
	_, conflict = pageDisplayIdentityForCID(doc2(t, "RCT-157"), "1rct00156h", "rct", "h", "")
	assert.True(t, conflict, "a markerless row naming the base release conflicts with the marker-bearing cid")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "RCT-156"), "1rct00156h", "rct", "h", "")
	assert.True(t, conflict, "a markerless row naming the cid's own base release still conflicts: it is not the remaster")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "rct157"), "1rct00156h", "rct", "h", "")
	assert.True(t, conflict, "a compact markerless row conflicts the same way")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "DV-818"), "dv00899ai", "dv", "ai", "")
	assert.True(t, conflict, "a markerless row on an AI cid conflicts: the request is marker-bearing")
	// E/Z-suffixed base-release spellings conflict identically (round 26):
	// RCT-157E and RCT-157Z name the base release's catalog variants, never
	// the remaster, while marker-bearing E/Z rows keep the tuple comparison.
	_, conflict = pageDisplayIdentityForCID(doc2(t, "RCT-157E"), "1rct00156h", "rct", "h", "")
	assert.True(t, conflict, "an E-suffixed markerless row naming the base release conflicts with the marker-bearing cid")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "RCT-157Z"), "1rct00156h", "rct", "h", "")
	assert.True(t, conflict, "a Z-suffixed markerless row naming the base release conflicts with the marker-bearing cid")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "rct157e"), "1rct00156h", "rct", "h", "")
	assert.True(t, conflict, "a compact E-suffixed markerless row conflicts the same way")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "RCT-157-E-HD"), "1rct00156h", "rct", "h", "")
	assert.True(t, conflict, "an E-suffixed marker-bearing row is not markerless: it conflicts through the tuple comparison")
	assert.Equal(t, "RCT-156EH", pageRemasterDisplayID(doc2(t, "RCT-156-E-HD"), "1rct00156eh", "rct", "h", "e"),
		"a matching E-suffixed row proves the cid's release")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "RCT-157"), "1rct00156h", "rct", "", "")
	assert.False(t, conflict, "a markerless request keeps the markerless pass-through")
	_, conflict = pageDisplayIdentityForCID(doc2(t, "12345"), "1rct00156h", "rct", "h", "")
	assert.False(t, conflict, "an unparseable row is not a conflict")
	_, conflict = pageDisplayIdentityForCID(nil, "1rct00156h", "rct", "h", "")
	assert.False(t, conflict, "a nil document publishes nothing to compare")
}

// The round-25b markerless probe: it must accept every base-release
// spelling — separator-pinned or compact, plain or carrying the E/Z
// catalog suffix — and reject marker-bearing rows and unparseable values,
// mirroring the displayIdentityTuple splits.
func TestIsMarkerlessDisplayID(t *testing.T) {
	for _, tc := range []struct {
		name    string
		display string
		want    bool
	}{
		{"hyphenated base release", "RCT-157", true},
		{"compact base release", "rct157", true},
		{"zero-padded base release", "RCT-00157", true},
		{"t-series base release", "T-28123", true},
		{"compact t-series base release", "t28123", true},
		{"e-suffixed base release", "RCT-157E", true},
		{"z-suffixed base release", "RCT-157Z", true},
		{"compact e-suffixed base release", "rct157e", true},
		{"hyphenated z-suffixed base release", "RCT-157-Z", true},
		{"e-suffixed t-series base release", "T-28123-E", true},
		{"hd marker row is not markerless", "RCT-157-HD", false},
		{"h marker row is not markerless", "RCT-157H", false},
		{"e-suffixed marker-bearing row is not markerless", "RCT-157-E-HD", false},
		{"compact z-suffixed marker-bearing row is not markerless", "RCT157ZHD", false},
		{"non-ez catalog-suffix row is unparseable", "RCT-157-X", false},
		{"digits-only row is unparseable", "12345", false},
		{"word row is unparseable", "garbage", false},
		{"empty row is unparseable", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isMarkerlessDisplayID(tc.display))
		})
	}
}

func TestExtractDisplayID(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(
		`<html><body><table>` +
			`<tr><td>商品番号：</td><td>1rct00156h</td></tr>` +
			`<tr><td>品番：</td><td>RCT-156-HD</td></tr>` +
			`</table></body></html>`))
	require.NoError(t, err)
	assert.Equal(t, "rct-156-hd", extractDisplayID(doc))

	doc2, err := goquery.NewDocumentFromReader(strings.NewReader(
		`<html><body><table><tr><td>商品番号：</td><td>1rct00156h</td></tr></table></body></html>`))
	require.NoError(t, err)
	assert.Equal(t, "", extractDisplayID(doc2), "CID-like 商品番号 must never serve as display ID")
}

func TestParseHTMLVerbatim_PersistsFullCID(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{Enabled: true}}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body><h1 id="title" class="item">T</h1></body></html>`))
	require.NoError(t, err)

	res, err := s.parseHTMLWithOptions(context.Background(), doc, "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/", true)
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", res.ContentID)
	assert.Equal(t, "RCT-156H", res.ID)

	res, err = s.parseHTML(context.Background(), doc, "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1ipx00535/")
	require.NoError(t, err)
	assert.Equal(t, "ipx00535", res.ContentID, "marker-free parsing keeps prefix-cleaned identity")
}

func TestParseHTMLVerbatim_MarkerUsesPageDisplayID(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{Enabled: true}}

	page := func(rows string) *goquery.Document {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body><h1 id="title" class="item">T</h1><table>` + rows + `</table></body></html>`))
		require.NoError(t, err)
		return doc
	}
	url := "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=dv00899ai/"

	res, err := s.parseHTMLWithOptions(context.Background(), page(`<tr><td>品番：</td><td>DV-818AI</td></tr>`), url, true)
	require.NoError(t, err)
	assert.Equal(t, "dv00899ai", res.ContentID)
	assert.Equal(t, "DV-818AI", res.ID, "remaster numbers are server-owned: the page 品番 outranks the CID-derived spelling")

	unusable := map[string]string{
		"markerless row":     `<tr><td>品番：</td><td>DV-818</td></tr>`,
		"marker mismatch":    `<tr><td>品番：</td><td>DV-818H</td></tr>`,
		"foreign series row": `<tr><td>品番：</td><td>RCT-156H</td></tr>`,
		"cid-only row":       `<tr><td>商品番号：</td><td>dv00899ai</td></tr>`,
		"cid-only th row":    `<tr><th>商品番号</th><td>dv00899ai</td></tr>`,
		"empty th row":       `<tr><th></th><td>DV-818AI</td></tr>`,
		"th row no value":    `<tr><th>品番</th></tr>`,
	}
	for name, rows := range unusable {
		res, err := s.parseHTMLWithOptions(context.Background(), page(rows), url, true)
		require.NoError(t, err, name)
		assert.Equal(t, "", res.ID, name+": unusable 品番 leaves the identity unset, AI CIDs do not encode the display number")
	}

	thPage := page(`<tr><th>品番</th><td>DV-818AI</td></tr>`)
	res, err = s.parseHTMLWithOptions(context.Background(), thPage, url, true)
	require.NoError(t, err)
	assert.Equal(t, "DV-818AI", res.ID, "th-labeled rows carry the value in the first td")
}
