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
		{"oreco183", "", "", false},
		{"ABC-1234H", "h", "abc", false},
		{"AbC-1234-Ai", "ai", "abc", false},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			m, series, isCID := classifyRemasterQuery(tc.id)
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
		assert.Equal(t, tc.want, bindResolvedCID(tc.urlCID, tc.resolved), "%s vs %s", tc.urlCID, tc.resolved)
	}
}

func TestResolveRemasterContentID_SingleCandidate(t *testing.T) {
	s, repo := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "/search/=") {
			return 200, `<html><body>` +
				`<a href="/digital/videoa/-/detail/=/cid=1rct00156h/">remaster</a>` +
				`<a href="/digital/videoa/-/detail/=/cid=1rct00156/">base original</a>` +
				`</body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)

	cid, err := s.ResolveContentIDCtx(context.Background(), "RCT-156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cid)
	assert.Equal(t, 0, rt.detailN, "single candidate must skip page verification")

	cached, err := repo.FindBySearchID(context.TODO(), "RCT-156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cached.ContentID)
}

func TestResolveRemasterContentID_RentalOnlyDiscovery(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "/search/=") {
			return 200, `<html><body><a href="/rental/ppr/-/detail/=/cid=1rct00156hr/">rental</a></body></html>`
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

func TestResolveContentID_BypassCacheConflictWins(t *testing.T) {
	s, repo := newRemasterTestScraper(t)
	require.NoError(t, repo.Create(context.Background(), &models.ContentIDMapping{SearchID: "1RCT00156H", ContentID: "zzzz999", Source: "dmm"}))
	rt := &remasterRoundTripper{serve: func(u string) (int, string) { return 404, "" }}
	s.client.SetTransport(rt)

	cid, err := s.ResolveContentIDCtx(context.Background(), "1RCT00156H")
	require.NoError(t, err)
	assert.Equal(t, "zzzz999", cid, "cached mapping (even conflicting) wins before bypass")
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

func TestExtractDisplayID(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(
		`<html><body><table>` +
			`<tr><td>商品番号：</td><td>1rct00156h</td></tr>` +
			`<tr><td>品番：</td><td>RCT-156-HD</td></tr>` +
			`</table></body></html>`))
	require.NoError(t, err)
	assert.Equal(t, "rct156h", extractDisplayID(doc))

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

	res, err = s.parseHTML(context.Background(), doc, "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/")
	require.NoError(t, err)
	assert.Equal(t, "rct00156h", res.ContentID, "marker-free parsing keeps prefix-cleaned identity")
}
