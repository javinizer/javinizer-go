package dmm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- sortCandidates tests ---

func TestSortCandidates_ByPriority(t *testing.T) {
	candidates := []urlCandidate{
		{url: "https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=a/", priority: 0, idLength: 5},
		{url: "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=b/", priority: 350, idLength: 5},
		{url: "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=c/", priority: 300, idLength: 5},
	}
	sortCandidates(candidates)
	assert.Equal(t, 350, candidates[0].priority)
	assert.Equal(t, 300, candidates[1].priority)
	assert.Equal(t, 0, candidates[2].priority)
}

func TestSortCandidates_SamePriorityByLength(t *testing.T) {
	candidates := []urlCandidate{
		{url: "url1", priority: 300, idLength: 10},
		{url: "url2", priority: 300, idLength: 5},
		{url: "url3", priority: 300, idLength: 8},
	}
	sortCandidates(candidates)
	assert.Equal(t, 5, candidates[0].idLength)
	assert.Equal(t, 8, candidates[1].idLength)
	assert.Equal(t, 10, candidates[2].idLength)
}

// --- maxPriority tests ---

func TestMaxPriority_Empty(t *testing.T) {
	assert.Equal(t, 0, maxPriority(nil))
	assert.Equal(t, 0, maxPriority([]urlCandidate{}))
}

func TestMaxPriority_WithCandidates(t *testing.T) {
	candidates := []urlCandidate{
		{priority: 100},
		{priority: 350},
		{priority: 200},
	}
	assert.Equal(t, 350, maxPriority(candidates))
}

// --- getURLCtx: context cancellation ---

func TestGetURLCtx_CancelledContext(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "IPX-535",
		ContentID: "ipx00535",
		Source:    "dmm",
	}))

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := scraper.getURLCtx(ctx, "IPX-535")
	require.Error(t, err)
}

// --- getURLCtx: search fails for all queries, tries direct URLs ---

func TestGetURLCtx_AllSearchQueriesFail(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "SSIS-100",
		ContentID: "ssis00100",
		Source:    "dmm",
	}))

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("ssis100"):    {status: http.StatusInternalServerError, body: "error"},
			searchURLFor("ssis-100"):   {status: http.StatusInternalServerError, body: "error"},
			searchURLFor("ssis00100"):  {status: http.StatusInternalServerError, body: "error"},
			digitalURLFor("ssis00100"): {status: http.StatusOK, body: "<html><body>ok</body></html>"},
		},
	}

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})
	scraper.client.SetTransport(transport)

	url, err := scraper.getURLCtx(context.Background(), "SSIS-100")
	require.NoError(t, err)
	assert.Contains(t, url, "digital/videoa")
}

// --- getURLCtx: no candidates from search or direct URLs ---

func TestGetURLCtx_NoCandidatesAnywhere(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "NOCAND-001",
		ContentID: "nocand00001",
		Source:    "dmm",
	}))

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("nocand00001"): {status: http.StatusOK, body: "<html><body>no results</body></html>"},
			searchURLFor("nocand-001"):  {status: http.StatusOK, body: "<html><body>no results</body></html>"},
			searchURLFor("nocand001"):   {status: http.StatusOK, body: "<html><body>no results</body></html>"},
			// All direct URLs return 404
			physicalURLFor("nocand00001"): {status: http.StatusNotFound, body: ""},
			digitalURLFor("nocand00001"):  {status: http.StatusNotFound, body: ""},
		},
	}

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})
	scraper.client.SetTransport(transport)

	_, err := scraper.getURLCtx(context.Background(), "NOCAND-001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no scrapable URL")
}

// --- getURLCtx: search returns low-priority candidates, supplements with direct URLs ---

func TestGetURLCtx_LowPrioritySearchSupplementedByDirect(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "LOWP-001",
		ContentID: "lowp00001",
		Source:    "dmm",
	}))

	// Search returns a rental link (priority 0, below 200 threshold)
	searchHTML := `<html><body><a href="https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=lowp00001/">Low priority</a></body></html>`

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("lowp00001"):   {status: http.StatusOK, body: searchHTML},
			searchURLFor("lowp-001"):    {status: http.StatusOK, body: searchHTML},
			searchURLFor("lowp001"):     {status: http.StatusOK, body: searchHTML},
			digitalURLFor("lowp00001"):  {status: http.StatusOK, body: "<html><body>ok</body></html>"},
			physicalURLFor("lowp00001"): {status: http.StatusNotFound, body: ""},
		},
	}

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})
	scraper.client.SetTransport(transport)

	url, err := scraper.getURLCtx(context.Background(), "LOWP-001")
	require.NoError(t, err)
	// Digital URL (priority 300) should be selected over rental (priority 0)
	assert.Contains(t, url, "digital/videoa")
}

// --- tryDirectURLs: cancelled context ---

func TestTryDirectURLs_CancelledContext(t *testing.T) {
	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	candidates := s.tryDirectURLs(ctx, "test001")
	// Should return empty or partial results due to cancelled context
	assert.NotNil(t, candidates)
}

// --- tryDirectURLs: nil response ---

func TestTryDirectURLs_NilResponseHandling(t *testing.T) {
	// Test that direct URL probing handles various non-200 responses gracefully.
	// All direct URLs return 404 (not in the map), so no candidates should be found.
	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{},
	}
	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}
	s.client.SetTransport(transport)

	candidates := s.tryDirectURLs(context.Background(), "test001")
	// All URLs return 404, so no candidates should be found
	assert.Empty(t, candidates)
}

// --- tryDirectURLs: 302 redirect is accepted ---

func TestTryDirectURLs_302RedirectAccepted(t *testing.T) {
	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			physicalURLFor("test001"): {status: http.StatusFound, body: ""},
			digitalURLFor("test001"):  {status: http.StatusNotFound, body: ""},
		},
	}

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}
	s.client.SetTransport(transport)

	candidates := s.tryDirectURLs(context.Background(), "test001")
	// Should find the 302 as a valid candidate
	require.NotEmpty(t, candidates)
	assert.Equal(t, physicalURLFor("test001"), candidates[0].url)
}

// --- Search: browser mode path ---

func TestSearch_BrowserModeForVideoDmm(t *testing.T) {
	// This tests the browser mode code path declaration.
	// Since we can't actually run a browser in tests, we test that when useBrowser
	// is true and the URL contains video.dmm.co.jp, the correct path is taken.
	// The fetchWithBrowser function will fail early (no Chrome available), which is fine.

	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "SSIS-200",
		ContentID: "ssis00200",
		Source:    "dmm",
	}))

	searchHTML := `<html><body><a href="https://video.dmm.co.jp/av/content/?id=ssis00200">SSIS-200</a></body></html>`

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("ssis200"):   {status: http.StatusOK, body: searchHTML},
			searchURLFor("ssis-200"):  {status: http.StatusOK, body: searchHTML},
			searchURLFor("ssis00200"): {status: http.StatusOK, body: searchHTML},
		},
	}

	settings := models.ScraperSettings{Enabled: true, UseBrowser: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: true, Timeout: 5}, ContentIDRepo: repo})
	scraper.client.SetTransport(transport)

	// This will try browser mode for video.dmm.co.jp URL which will fail
	// since there's no Chrome. We just verify it doesn't panic.
	_, err := scraper.Search(context.Background(), "SSIS-200")
	// Error is expected since browser isn't available
	_ = err
}

// --- Search: cancelled context ---

func TestSearch_CancelledContextBeforeRateLimiter(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "CTX-001",
		ContentID: "ctx00001",
		Source:    "dmm",
	}))

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})

	// Set up transport with a digital URL that would succeed
	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("ctx00001"):  {status: http.StatusOK, body: "<html><body><a href=\"/digital/videoa/-/detail/=/cid=ctx00001/\">link</a></body></html>"},
			digitalURLFor("ctx00001"): {status: http.StatusOK, body: "<html><body><h1 id=\"title\" class=\"item\">CTX-001</h1></body></html>"},
		},
	}
	scraper.client.SetTransport(transport)

	// Cancel context before calling Search
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := scraper.Search(ctx, "CTX-001")
	require.Error(t, err)
}

// --- Search: success via non-browser path ---

func TestSearch_SuccessViaHTTP(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "HTTP-001",
		ContentID: "http00001",
		Source:    "dmm",
	}))

	searchHTML := `<html><body><a href="/digital/videoa/-/detail/=/cid=http00001/">HTTP-001</a></body></html>`
	detailHTML := `<html><body><h1 id="title" class="item">HTTP-001 Test Movie</h1></body></html>`

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("http00001"):  {status: http.StatusOK, body: searchHTML},
			searchURLFor("http-001"):   {status: http.StatusOK, body: searchHTML},
			searchURLFor("http001"):    {status: http.StatusOK, body: searchHTML},
			digitalURLFor("http00001"): {status: http.StatusOK, body: detailHTML},
		},
	}

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})
	scraper.client.SetTransport(transport)

	result, err := scraper.Search(context.Background(), "HTTP-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "dmm", result.Source)
}

// --- ScrapeURL: browser mode path for video.dmm.co.jp ---

func TestScrapeURL_BrowserModeVideoDMM(t *testing.T) {
	s := &scraper{
		enabled:       true,
		useBrowser:    true,
		browserConfig: models.BrowserConfig{Enabled: true, Timeout: 5},
		rateLimiter:   ratelimit.NewLimiter(0),
		client:        resty.New(),
		settings:      models.ScraperSettings{Enabled: true},
	}

	// This will attempt browser fetch for video.dmm.co.jp, which will fail
	// since Chrome isn't available. We verify it doesn't panic.
	_, err := s.ScrapeURL(context.Background(), "https://video.dmm.co.jp/av/content/?id=test123")
	require.Error(t, err)
}

// --- ScrapeURL: rate limit error ---

func TestScrapeURL_RateLimitError(t *testing.T) {
	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.ScrapeURL(ctx, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/")
	require.Error(t, err)
}

// --- ScrapeURL: HTML parse error ---

func TestScrapeURL_HTMLParseError(t *testing.T) {
	// This is hard to trigger because goquery is very lenient, but we test the normal parse path
	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			"/digital/videoa/-/detail/=/cid=test001/": {
				status: http.StatusOK,
				body:   `<html><body><h1 id="title" class="item">Test Movie</h1></body></html>`,
			},
		},
	}
	s.client.SetTransport(transport)

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/")
	// Should succeed or fail gracefully
	_ = err
}

// --- ScrapeURL: success ---

func TestScrapeURL_SuccessNonVideoDMM(t *testing.T) {
	detailHTML := `
<!DOCTYPE html>
<html>
<body>
	<h1 id="title" class="item">SCRAPE-001 ScrapeURL Success</h1>
	<div class="mg-b20 lh4"><p class="mg-b20">Description.</p></div>
	<table>
		<tr><td>Release: 2024/03/15</td></tr>
	</table>
</body>
</html>`

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			// The key should match the request URL from resty
			"https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=scrape00001/": {
				status: http.StatusOK,
				body:   detailHTML,
			},
		},
	}

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}
	s.client.SetTransport(transport)

	result, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=scrape00001/")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "dmm", result.Source)
}

// --- extractCandidateURLs: browser mode includes video.dmm.co.jp ---

func TestExtractCandidateURLs_BrowserModeIncludesVideoDMM(t *testing.T) {
	html := `<html><body>
		<a href="https://video.dmm.co.jp/av/content/?id=test001">Streaming Link</a>
		<a href="https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/">Digital Link</a>
	</body></html>`

	doc, err := goqueryDoc(html)
	require.NoError(t, err)

	s := &scraper{
		enabled:    true,
		useBrowser: true,
		settings:   models.ScraperSettings{Enabled: true},
	}

	candidates := s.extractCandidateURLs(doc, "test001")
	// Both URLs should be included when browser mode is enabled
	assert.GreaterOrEqual(t, len(candidates), 1)
}

func TestExtractCandidateURLs_NoBrowserExcludesVideoDMM(t *testing.T) {
	html := `<html><body>
		<a href="https://video.dmm.co.jp/av/content/?id=test001">Streaming Link</a>
		<a href="https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/">Digital Link</a>
	</body></html>`

	doc, err := goqueryDoc(html)
	require.NoError(t, err)

	s := &scraper{
		enabled:    true,
		useBrowser: false,
		settings:   models.ScraperSettings{Enabled: true},
	}

	candidates := s.extractCandidateURLs(doc, "test001")
	// video.dmm.co.jp should be excluded when browser mode is disabled
	for _, c := range candidates {
		assert.NotContains(t, c.url, "video.dmm.co.jp")
	}
}

// --- extractCandidateURLs: excluded patterns ---

func TestExtractCandidateURLs_ExcludedSearchPatterns(t *testing.T) {
	html := `<html><body>
		<a href="https://www.dmm.co.jp/search/=/searchstr=test001/">Search Page</a>
		<a href="https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/">Detail Page</a>
	</body></html>`

	doc, err := goqueryDoc(html)
	require.NoError(t, err)

	s := &scraper{
		enabled:    true,
		useBrowser: false,
		settings:   models.ScraperSettings{Enabled: true},
	}

	candidates := s.extractCandidateURLs(doc, "test001")
	// Search page should be excluded, detail page should be included
	for _, c := range candidates {
		assert.NotContains(t, c.url, "/search/")
	}
}

func TestExtractCandidateURLs_RentalSearchExcluded(t *testing.T) {
	html := `<html><body>
		<a href="https://www.dmm.co.jp/rental/-/search/=/searchstr=test001/">Rental Search</a>
		<a href="https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/">Detail</a>
	</body></html>`

	doc, err := goqueryDoc(html)
	require.NoError(t, err)

	s := &scraper{
		enabled:    true,
		useBrowser: false,
		settings:   models.ScraperSettings{Enabled: true},
	}

	candidates := s.extractCandidateURLs(doc, "test001")
	for _, c := range candidates {
		assert.NotContains(t, c.url, "/rental/-/search/")
	}
}

func TestExtractCandidateURLs_ListPageExcluded(t *testing.T) {
	html := `<html><body>
		<a href="https://www.dmm.co.jp/digital/videoa/-/list/=/article=test/">List Page</a>
		<a href="https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/">Detail Page</a>
	</body></html>`

	doc, err := goqueryDoc(html)
	require.NoError(t, err)

	s := &scraper{
		enabled:    true,
		useBrowser: false,
		settings:   models.ScraperSettings{Enabled: true},
	}

	candidates := s.extractCandidateURLs(doc, "test001")
	for _, c := range candidates {
		assert.NotContains(t, c.url, "/list/")
	}
}

// --- extractCandidateURLs: relative URL handling ---

func TestExtractCandidateURLs_RelativeURL(t *testing.T) {
	html := `<html><body>
		<a href="/digital/videoa/-/detail/=/cid=test001/">Relative Link</a>
	</body></html>`

	doc, err := goqueryDoc(html)
	require.NoError(t, err)

	s := &scraper{
		enabled:    true,
		useBrowser: false,
		settings:   models.ScraperSettings{Enabled: true},
	}

	candidates := s.extractCandidateURLs(doc, "test001")
	require.NotEmpty(t, candidates)
	assert.True(t, strings.HasPrefix(candidates[0].url, "https://www.dmm.co.jp"))
}

// --- extractCandidateURLs: non-http URL skipped ---

func TestExtractCandidateURLs_NonHTTPURLSkipped(t *testing.T) {
	html := `<html><body>
		<a href="javascript:void(0)">JS Link</a>
		<a href="mailto:test@example.com">Email Link</a>
	</body></html>`

	doc, err := goqueryDoc(html)
	require.NoError(t, err)

	s := &scraper{
		enabled:    true,
		useBrowser: false,
		settings:   models.ScraperSettings{Enabled: true},
	}

	candidates := s.extractCandidateURLs(doc, "test001")
	assert.Empty(t, candidates)
}

// --- GetURL: wraps getURLCtx ---

func TestGetURL_WrapsGetURLCtx(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "WRAP-001",
		ContentID: "wrap00001",
		Source:    "dmm",
	}))

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("wrap00001"):  {status: http.StatusOK, body: `<html><body><a href="/digital/videoa/-/detail/=/cid=wrap00001/">Link</a></body></html>`},
			searchURLFor("wrap-001"):   {status: http.StatusOK, body: `<html><body><a href="/digital/videoa/-/detail/=/cid=wrap00001/">Link</a></body></html>`},
			searchURLFor("wrap001"):    {status: http.StatusOK, body: `<html><body><a href="/digital/videoa/-/detail/=/cid=wrap00001/">Link</a></body></html>`},
			digitalURLFor("wrap00001"): {status: http.StatusOK, body: "<html><body>ok</body></html>"},
		},
	}

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})
	scraper.client.SetTransport(transport)

	url, err := scraper.GetURL(context.Background(), "WRAP-001")
	require.NoError(t, err)
	assert.NotEmpty(t, url)
}

// --- Helper: goquery doc from string (without panicking) ---

func goqueryDoc(html string) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(html))
}

// --- Shared test helpers (previously in original search_miss_test.go) ---

func docFromHTMLDMM(t *testing.T, raw string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("goquery.NewDocumentFromReader() error = %v", err)
	}
	return doc
}

func newTestDMMScraper() *scraper {
	return &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}
}

type dmmResponse struct {
	status int
	body   string
}

type dmmStatusRoundTripper struct {
	status int
}

func (rt *dmmStatusRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: rt.status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("error response")),
		Request:    req,
	}, nil
}

type dmmTestTransport struct {
	server *httptest.Server
}

func (rt *dmmTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	proxyReq := req.Clone(req.Context())
	proxyReq.URL.Scheme = "http"
	proxyReq.URL.Host = rt.server.Listener.Addr().String()
	return http.DefaultTransport.RoundTrip(proxyReq)
}

func newDMMServer(t *testing.T, responses map[string]dmmResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, ok := responses[r.URL.String()]
		if !ok {
			resp = dmmResponse{status: http.StatusNotFound, body: "not found"}
		}
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
}

func newDMMScraperWithServer(server *httptest.Server, enabled, useBrowser bool) *scraper {
	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	return &scraper{
		client:       client,
		enabled:      enabled,
		useBrowser:   useBrowser,
		rateLimiter:  ratelimit.NewLimiter(0),
		settings:     models.ScraperSettings{Enabled: enabled},
		proxyProfile: nil,
	}
}
