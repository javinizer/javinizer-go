package dmm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ScrapeURL: non-DMM URL returns not-found error ---

func TestScrapeURL_Miss2_WrongHost(t *testing.T) {
	s := newTestDMMScraper()
	_, err := s.ScrapeURL(context.Background(), "https://example.com/some/page")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// --- ScrapeURL: 404 response returns not-found error ---

func TestScrapeURL_Miss2_Status404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// --- ScrapeURL: 429 rate limit returns status error ---

func TestScrapeURL_Miss2_Status429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusTooManyRequests, scraperErr.StatusCode)
}

// --- ScrapeURL: 403 geo-block returns status error ---

func TestScrapeURL_Miss2_Status403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusForbidden, scraperErr.StatusCode)
}

// --- ScrapeURL: 451 geo-block returns status error ---

func TestScrapeURL_Miss2_Status451(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnavailableForLegalReasons)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnavailableForLegalReasons, scraperErr.StatusCode)
}

// --- ScrapeURL: 500 server error returns status error ---

func TestScrapeURL_Miss2_Status500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, scraperErr.StatusCode)
}

// --- ScrapeURL: generic non-200 status ---

func TestScrapeURL_Miss2_StatusGeneric(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/")
	require.Error(t, err)
}

// --- ScrapeURL: HTTP request error ---

func TestScrapeURL_Miss2_RequestError(t *testing.T) {
	client := resty.New()
	// Use a transport that always errors
	client.SetTransport(&errorRoundedTripper{})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=test001/")
	require.Error(t, err)
}

// --- Search: non-200 status returns error ---

func TestSearch_Miss2_Non200Status(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "SRCH-002",
		ContentID: "srch00002",
		Source:    "dmm",
	}))

	searchHTML := `<html><body><a href="/digital/videoa/-/detail/=/cid=srch00002/">SRCH-002</a></body></html>`

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("srch00002"):  {status: http.StatusOK, body: searchHTML},
			searchURLFor("srch-002"):   {status: http.StatusOK, body: searchHTML},
			searchURLFor("srch002"):    {status: http.StatusOK, body: searchHTML},
			digitalURLFor("srch00002"): {status: http.StatusBadGateway, body: "error"},
		},
	}

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})
	scraper.client.SetTransport(transport)

	_, err := scraper.Search(context.Background(), "SRCH-002")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadGateway, scraperErr.StatusCode)
}

// --- Search: HTTP fetch error ---

func TestSearch_Miss2_FetchError(t *testing.T) {
	repo := newDMMTestRepo(t)
	require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
		SearchID:  "FETCH-001",
		ContentID: "fetch00001",
		Source:    "dmm",
	}))

	searchHTML := `<html><body><a href="/digital/videoa/-/detail/=/cid=fetch00001/">FETCH-001</a></body></html>`

	transport := &dmmSearchSuccessRoundTripper{
		responses: map[string]struct {
			status int
			body   string
		}{
			searchURLFor("fetch00001"): {status: http.StatusOK, body: searchHTML},
			searchURLFor("fetch-001"):  {status: http.StatusOK, body: searchHTML},
			searchURLFor("fetch001"):   {status: http.StatusOK, body: searchHTML},
		},
	}

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})
	scraper.client.SetTransport(transport)

	// The detail URL isn't in the transport map, so it'll get a 404 default
	_, err := scraper.Search(context.Background(), "FETCH-001")
	require.Error(t, err)
}

// --- getURLCtx: content ID resolution failure ---

func TestGetURLCtx_Miss2_ContentIDResolutionFailure(t *testing.T) {
	repo := newDMMTestRepo(t)
	// Don't create any mapping — resolution will fail

	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, &models.ProxyConfig{}, models.FlareSolverrConfig{},
		dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})

	_, err := scraper.getURLCtx(context.Background(), "NO-MAP-001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "movie not found")
}

// --- tryDirectURLs: all direct URLs return non-200/non-302 ---

func TestTryDirectURLs_Miss2_AllNonSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 404 for everything
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
	}

	candidates := s.tryDirectURLs(context.Background(), "miss2001")
	assert.Empty(t, candidates)
}

// --- tryDirectURLs: verify rental URLs are generated ---

func TestTryDirectURLs_Miss2_RentalURLsPresent(t *testing.T) {
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
	}

	_ = s.tryDirectURLs(context.Background(), "rent001")

	// Verify rental URL patterns were requested
	hasRental := false
	for _, p := range requestedPaths {
		if strings.Contains(p, "/rental/") {
			hasRental = true
			break
		}
	}
	assert.True(t, hasRental, "expected rental URLs to be probed, got paths: %v", requestedPaths)
}

// --- urlPriority tests ---

func TestUrlPriority_Miss2_AllPaths(t *testing.T) {
	tests := []struct {
		url      string
		expected int
	}{
		{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=x/", 350},
		{"https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=x/", 300},
		{"https://www.dmm.co.jp/digital/videoc/-/detail/=/cid=x/", 300},
		{"https://video.dmm.co.jp/amateur/content/?id=x", 250},
		{"https://video.dmm.co.jp/av/content/?id=x", 200},
		{"https://www.dmm.co.jp/monthly/premium/-/detail/=/cid=x/", 150},
		{"https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=x/", 100},
		{"https://www.dmm.co.jp/rental/ppr/-/detail/=/cid=x/", 0},
		{"https://www.dmm.co.jp/unknown/path/", 0},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, urlPriority(tt.url))
		})
	}
}

// --- ScrapeURL: parse HTML error path (via httptest server returning malformed data) ---

func TestScrapeURL_Miss2_ParseHTMLSuccess(t *testing.T) {
	detailHTML := `
<!DOCTYPE html>
<html>
<body>
	<h1 id="title" class="item">SCRAPE2-001 Parse Test</h1>
	<table>
		<tr><td>Release: 2024/01/01</td></tr>
	</table>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(detailHTML))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=scrape2001/")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "dmm", result.Source)
}

// --- errorRoundedTripper: returns error on every request ---

type errorRoundedTripper struct{}

func (rt *errorRoundedTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, &dmmTestError{msg: "connection refused"}
}

type dmmTestError struct {
	msg string
}

func (e *dmmTestError) Error() string {
	return e.msg
}
