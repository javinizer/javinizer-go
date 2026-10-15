package tokyohot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ScrapeURL coverage: since CanHandleURL checks hostname,
// we test ScrapeURL with a libredmm-like URL to verify the "not handled" path,
// and use Search for the HTTP-level integration tests. ---

func TestScrapeURL_UnhandledURL(t *testing.T) {
	settings := testSettings("https://www.tokyo-hot.com")
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.ScrapeURL(context.Background(), "https://example.com/product/N1234/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not handled")
}

func TestScrapeURL_CanHandleURL_Malformed(t *testing.T) {
	settings := testSettings("https://www.tokyo-hot.com")
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	assert.False(t, s.CanHandleURL("://bad-url"))
}

// --- Search with httptest.NewServer: HTTP-level integration tests ---

func TestSearch_NotFound(t *testing.T) {
	// Search goes through getURLCtx first, so we need to serve both
	// the search page and the detail page
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "" {
			// Search endpoint - return a link
			_, _ = fmt.Fprint(w, `<html><body><a href="/product/N9999/">N9999</a></body></html>`)
			return
		}
		if r.URL.Path == "/product/N9999/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "N9999")
	require.Error(t, err)
	// Search returns generic "status code" error for non-200
	assert.Contains(t, err.Error(), "status code 404")
}

func TestSearch_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "" {
			_, _ = fmt.Fprint(w, `<html><body><a href="/product/N1234/">N1234</a></body></html>`)
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "N1234")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status code 429")
}

func TestSearch_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "" {
			_, _ = fmt.Fprint(w, `<html><body><a href="/product/N1234/">N1234</a></body></html>`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "N1234")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status code 403")
}

func TestSearch_InternalServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "" {
			_, _ = fmt.Fprint(w, `<html><body><a href="/product/N1234/">N1234</a></body></html>`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "N1234")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status code 500")
}

// --- getURLCtx: search page with single candidate ---

func TestGetURL_SingleCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "N1234" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, `<html><body><a href="/product/other999/">Other Movie</a></body></html>`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	url, err := s.GetURL(context.Background(), "N1234")
	require.NoError(t, err)
	assert.Contains(t, url, "/product/other999/")
}

// --- getURLCtx: search page with no results ---

func TestGetURL_SearchNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `<html><body><p>No results</p></body></html>`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.GetURL(context.Background(), "NOTFOUND-9999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- getURLCtx: search page returns non-200 ---

func TestGetURL_SearchNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.GetURL(context.Background(), "N1234")
	require.Error(t, err)
}

// --- fetchPageCtx: Cloudflare challenge ---

func TestSearch_CloudflareChallenge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "" {
			_, _ = fmt.Fprint(w, `<html><body><a href="/product/N1234/">N1234</a></body></html>`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `<html><head><title>Just a moment...</title></head><body>
<script>cf-challenge</script></body></html>`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "N1234")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Cloudflare")
}

// --- ExtractIDFromURL edge cases ---

func TestExtractIDFromURL_InvalidURL(t *testing.T) {
	settings := testSettings("https://www.tokyo-hot.com")
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.ExtractIDFromURL("://bad-url")
	require.Error(t, err)
}

// --- Search: disabled ---

func TestSearch_Disabled_MissTest(t *testing.T) {
	settings := testSettings("https://www.tokyo-hot.com")
	settings.Enabled = false
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "N1234")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// ScrapeURL handles an explicit page independently of Search's enabled gate.
// Its response must come from a fixture even when settings.Enabled is false.
func TestScrapeURL_MissTest_DisabledNotCheckedInScrapeURL(t *testing.T) {
	const target = "https://www.tokyo-hot.com/product/N1234/"
	settings := testSettings("https://www.tokyo-hot.com")
	settings.Enabled = false
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	transport := &thMockTransport{statusCode: http.StatusNotFound}
	s.client.SetTransport(transport)

	result, err := s.ScrapeURL(context.Background(), target)
	assert.Nil(t, result)
	var scraperErr *models.ScraperError
	require.ErrorAs(t, err, &scraperErr)
	assert.Equal(t, "TokyoHot", scraperErr.Scraper)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
	assert.Equal(t, "page not found", scraperErr.Message)
	assert.False(t, scraperErr.Retryable)
	require.Len(t, transport.requests, 1)
	assert.Equal(t, http.MethodGet, transport.requests[0].Method)
	assert.Equal(t, target+"?lang=en", transport.requests[0].URL.String())
}
