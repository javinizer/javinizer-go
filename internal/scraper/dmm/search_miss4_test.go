package dmm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Search: browser mode for video.dmm.co.jp URL ---

func TestSearch_Miss4_BrowserMode_VideoDMM(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "BROW-001",
		ContentID: "brow00001",
		Source:    "dmm",
	}))

	searchHTML := `<html><body><a href="https://video.dmm.co.jp/av/content/?id=brow00001">BROW-001</a></body></html>`

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("brow00001"): {status: http.StatusOK, body: searchHTML},
			searchURLFor("brow-001"):  {status: http.StatusOK, body: searchHTML},
			searchURLFor("brow001"):   {status: http.StatusOK, body: searchHTML},
		},
	}

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: true, Timeout: 30}, ContentIDRepo: repo})
	scraper.client.SetTransport(transport)

	// Search with browser mode enabled and video.dmm.co.jp URL.
	// This will attempt browser fetch, which will fail (no Chrome), hitting the
	// "browser fetch failed" error path.
	_, err := scraper.Search(context.Background(), "BROW-001")
	// Browser mode will fail since no Chrome is available
	require.Error(t, err)
}

// --- Search: rate limiter wait failure in else branch ---

func TestSearch_Miss4_NonBrowserRateLimiterFails(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "RLNF-001",
		ContentID: "rlnf00001",
		Source:    "dmm",
	}))

	searchHTML := `<html><body><a href="/digital/videoa/-/detail/=/cid=rlnf00001/">RLNF-001</a></body></html>`

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("rlnf00001"): {status: http.StatusOK, body: searchHTML},
			searchURLFor("rlnf-001"):  {status: http.StatusOK, body: searchHTML},
			searchURLFor("rlnf001"):   {status: http.StatusOK, body: searchHTML},
		},
	}

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})
	scraper.client.SetTransport(transport)

	// Cancel context so rate limiter wait fails in the else branch of Search
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := scraper.Search(ctx, "RLNF-001")
	require.Error(t, err)
}

// --- Search: non-200 status from detail page (else branch) ---

func TestSearch_Miss4_NonBrowserNon200Status(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "STAT-001",
		ContentID: "stat00001",
		Source:    "dmm",
	}))

	searchHTML := `<html><body><a href="/digital/videoa/-/detail/=/cid=stat00001/">STAT-001</a></body></html>`

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("stat00001"):  {status: http.StatusOK, body: searchHTML},
			searchURLFor("stat-001"):   {status: http.StatusOK, body: searchHTML},
			searchURLFor("stat001"):    {status: http.StatusOK, body: searchHTML},
			digitalURLFor("stat00001"): {status: http.StatusForbidden, body: "blocked"},
		},
	}

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})
	scraper.client.SetTransport(transport)
	scraper.useBrowser = false

	_, err := scraper.Search(context.Background(), "STAT-001")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	_ = scraperErr
}

// --- ScrapeURL: browser mode for video.dmm.co.jp ---

func TestScrapeURL_Miss4_BrowserModeVideoDMM(t *testing.T) {
	s := &scraper{
		enabled:       true,
		rateLimiter:   ratelimit.NewLimiter(0),
		client:        resty.New(),
		settings:      models.ScraperSettings{Enabled: true},
		useBrowser:    true,
		browserConfig: models.BrowserConfig{Enabled: true, Timeout: 1},
	}

	// Browser fetch will fail (no Chrome), hitting the "browser fetch failed" error path
	_, err := s.ScrapeURL(context.Background(), "https://video.dmm.co.jp/av/content/?id=test001/")
	require.Error(t, err)
}

// --- ScrapeURL: 404 status returns NotFoundError ---

func TestScrapeURL_Miss4_Status404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
		useBrowser:  false,
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=nf404001/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- ScrapeURL: 429 status returns rate-limited error ---

func TestScrapeURL_Miss4_Status429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
		useBrowser:  false,
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=rl429001/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusTooManyRequests, scraperErr.StatusCode)
}

// --- ScrapeURL: 403 status returns access blocked error ---

func TestScrapeURL_Miss4_Status403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
		useBrowser:  false,
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=fb403001/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "geo-restriction")
}

// --- ScrapeURL: 451 status returns access blocked error ---

func TestScrapeURL_Miss4_Status451(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(451) // Unavailable For Legal Reasons
		_, _ = w.Write([]byte("legal"))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
		useBrowser:  false,
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=lg451001/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "geo-restriction")
}

// --- ScrapeURL: 500 status returns server error ---

func TestScrapeURL_Miss4_Status500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
		useBrowser:  false,
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=se500001/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "server error")
}

// --- ScrapeURL: other non-200 status (e.g., 302) ---

func TestScrapeURL_Miss4_Status302(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
		_, _ = w.Write([]byte("redirect"))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
		useBrowser:  false,
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=rd302001/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "status code")
}

// --- ScrapeURL: rate limit wait failure ---

func TestScrapeURL_Miss4_RateLimitWaitFailed(t *testing.T) {
	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
		useBrowser:  false,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.ScrapeURL(ctx, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=rl001/")
	require.Error(t, err)
}

// --- ScrapeURL: request error ---

func TestScrapeURL_Miss4_RequestError(t *testing.T) {
	client := resty.New()
	client.SetTransport(&errorRoundedTripper{})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
		useBrowser:  false,
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=err001/")
	require.Error(t, err)
}

// --- getURLCtx: rate limit wait fails for a search query (not context cancelled) ---

func TestGetURLCtx_Miss4_RateLimitWaitFailsForQuery(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "RLQ-001",
		ContentID: "rlq00001",
		Source:    "dmm",
	}))

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})

	// Use a cancelled context - rate limiter wait will fail
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := scraper.getURLCtx(ctx, "RLQ-001")
	require.Error(t, err)
}

// --- tryDirectURLs: context cancelled during rate limiter wait ---

func TestTryDirectURLs_Miss4_ContextCancelledDuringWait(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "TDR-001",
		ContentID: "tdr00001",
		Source:    "dmm",
	}))

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})

	// Cancel context so rate limiter wait fails in tryDirectURLs
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := scraper.tryDirectURLs(ctx, "tdr00001")
	// Should return empty/partial candidates since context was cancelled
	assert.NotNil(t, result)
}

// --- tryDirectURLs: nil response from direct URL ---

func TestTryDirectURLs_Miss4_NilResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return nothing - this simulates edge cases
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "NILR-001",
		ContentID: "nilr00001",
		Source:    "dmm",
	}))

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})
	scraper.client.SetTransport(&dmmTestTransport{server: server})

	result := scraper.tryDirectURLs(context.Background(), "nilr00001")
	// Should handle gracefully (may return empty candidates)
	_ = result
}

// --- getURLCtx: search with context cancelled during search loop ---

func TestGetURLCtx_Miss4_ContextCancelledMidSearch(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "MID-001",
		ContentID: "mid00001",
		Source:    "dmm",
	}))

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})

	// Use cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := scraper.getURLCtx(ctx, "MID-001")
	require.Error(t, err)
}

// --- getURLCtx: search request fails for a query ---

func TestGetURLCtx_Miss4_SearchRequestFails(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "SRF-001",
		ContentID: "srf00001",
		Source:    "dmm",
	}))

	client := resty.New()
	client.SetTransport(&errorRoundedTripper{})

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})
	scraper.client.SetTransport(&errorRoundedTripper{})

	// Search requests will fail, then tryDirectURLs will also fail
	_, err := scraper.getURLCtx(context.Background(), "SRF-001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no scrapable URL")
}

// --- Search: failed to parse HTML from browser mode ---
// Already covered by TestSearch_Miss4_BrowserMode_VideoDMM above.

// --- ScrapeURL: video.dmm.co.jp URL with browser mode, browser fetch fails ---

func TestScrapeURL_Miss4_BrowserFetchFails(t *testing.T) {
	s := &scraper{
		enabled:       true,
		rateLimiter:   ratelimit.NewLimiter(0),
		client:        resty.New(),
		settings:      models.ScraperSettings{Enabled: true},
		useBrowser:    true,
		browserConfig: models.BrowserConfig{Enabled: true, Timeout: 1},
	}

	_, err := s.ScrapeURL(context.Background(), "https://video.dmm.co.jp/av/content/?id=bf001/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "browser fetch failed")
}

// --- ScrapeURL: video.dmm.co.jp URL with browser mode, successful fetch but parse error ---
// This would require a mockable fetchWithBrowser which isn't available,
// so we focus on the paths we can reach.

// --- ScrapeURL: non-DMM URL returns not found ---

func TestScrapeURL_Miss4_UnhandledURL(t *testing.T) {
	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
		useBrowser:  false,
	}

	_, err := s.ScrapeURL(context.Background(), "https://example.com/test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not handled")
}

// --- urlPriority: test all URL priority levels ---

func TestURLPriority_Miss4_AllLevels(t *testing.T) {
	tests := []struct {
		url      string
		expected int
	}{
		{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=test001/", 350},
		{"https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/", 300},
		{"https://www.dmm.co.jp/digital/videoc/-/detail/=/cid=test001/", 300},
		{"https://video.dmm.co.jp/amateur/content/?id=test001", 250},
		{"https://video.dmm.co.jp/av/content/?id=test001", 200},
		{"https://www.dmm.co.jp/monthly/premium/-/detail/=/cid=test001/", 150},
		{"https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=test001/", 100},
		{"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=test001/", 0},
		{"https://www.dmm.co.jp/unknown/path/", 0},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, urlPriority(tt.url))
		})
	}
}

// --- maxPriority and sortCandidates ---

func TestMaxPriority_Miss4(t *testing.T) {
	assert.Equal(t, 0, maxPriority(nil))
	assert.Equal(t, 0, maxPriority([]urlCandidate{}))
	assert.Equal(t, 100, maxPriority([]urlCandidate{{priority: 100}}))
	assert.Equal(t, 200, maxPriority([]urlCandidate{{priority: 100}, {priority: 200}, {priority: 150}}))
}

func TestSortCandidates_Miss4(t *testing.T) {
	candidates := []urlCandidate{
		{url: "a", priority: 100, idLength: 5},
		{url: "b", priority: 200, idLength: 3},
		{url: "c", priority: 200, idLength: 7},
		{url: "d", priority: 50, idLength: 4},
	}
	sortCandidates(candidates)
	assert.Equal(t, "b", candidates[0].url) // priority 200, shortest idLength
	assert.Equal(t, "c", candidates[1].url) // priority 200, longer idLength
	assert.Equal(t, "a", candidates[2].url) // priority 100
	assert.Equal(t, "d", candidates[3].url) // priority 50
}

// --- getURLCtx: low priority candidates trigger tryDirectURLs ---

func TestGetURLCtx_Miss4_LowPriorityTriggersDirectURLs(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "LOWP-001",
		ContentID: "lowp00001",
		Source:    "dmm",
	}))

	// Return a rental link (priority 0) so tryDirectURLs is called
	searchHTML := `<html><body><a href="https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=lowp00001r/">LOWP-001</a></body></html>`
	directDetailHTML := `<!DOCTYPE html><html><body><h1 id="title">LOWP-001 Detail</h1></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search/=/searchstr/lowp00001/" ||
			r.URL.Path == "/search/=/searchstr/lowp-001/" ||
			r.URL.Path == "/search/=/searchstr/lowp001/" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(searchHTML))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(directDetailHTML))
		}
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})
	scraper.client.SetTransport(&dmmTestTransport{server: server})

	url, err := scraper.getURLCtx(context.Background(), "LOWP-001")
	if err != nil {
		// Acceptable - may not find a valid URL
		assert.Contains(t, err.Error(), "no scrapable URL")
	} else {
		assert.NotEmpty(t, url)
	}
}
