package dlgetchu

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ScrapeURL requires CanHandleURL to pass which checks hostname.
// Since httptest server doesn't have dl.getchu.com hostname,
// we use Search for HTTP-level integration tests. ---

func TestScrapeURL_UnhandledURL(t *testing.T) {
	settings := testSettings("http://dl.getchu.com")
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.ScrapeURL(context.Background(), "https://example.com/i/item12345")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not handled")
}

// --- Search with httptest.NewServer: HTTP-level integration tests ---

func TestSearch_MissTest_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// All requests return 404
		if strings.Contains(r.URL.RawQuery, "search_keyword") {
			_, _ = fmt.Fprint(w, `<html><body>No results</body></html>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "NOTFOUND")
	require.Error(t, err)
}

func TestSearch_MissTest_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "search_keyword") {
			_, _ = fmt.Fprint(w, `<html><body><a href="/i/item12345">Result</a></body></html>`)
			return
		}
		if strings.Contains(r.URL.Path, "/i/item") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "12345")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status code 500")
}

// --- getURLCtx: numeric ID pattern (needs prefix like /item or id=) ---

func TestGetURL_MissTest_NumericIDWithPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/i/item12345") {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `<html><body>Detail page</body></html>`)
			return
		}
		// Search pages
		if strings.Contains(r.URL.RawQuery, "search_keyword") {
			_, _ = fmt.Fprint(w, `<html><body><a href="/i/item12345">Result</a></body></html>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	// Input as an HTTP URL containing /item prefix so extractNumericID matches
	url, err := s.GetURL(context.Background(), server.URL+"/i/item12345")
	require.NoError(t, err)
	assert.Contains(t, url, "/i/item12345")
}

// --- getURLCtx: search fallback when direct check fails ---

func TestGetURL_MissTest_SearchFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Direct item check fails
		if strings.Contains(r.URL.Path, "/i/item") && !strings.Contains(r.URL.RawQuery, "search_keyword") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Search returns a result
		if strings.Contains(r.URL.RawQuery, "search_keyword") {
			_, _ = fmt.Fprint(w, `<html><body><a href="/i/item54321">Result</a></body></html>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	url, err := s.GetURL(context.Background(), "ABC-123")
	require.NoError(t, err)
	assert.Contains(t, url, "/i/item54321")
}

// --- getURLCtx: all searches fail ---

func TestGetURL_MissTest_AllSearchesFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Direct check fails
		if strings.Contains(r.URL.Path, "/i/item") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Search pages return no results
		_, _ = fmt.Fprint(w, `<html><body>No results</body></html>`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.GetURL(context.Background(), "NOTFOUND-999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found on DLgetchu")
}

// --- fetchPageCtx: Cloudflare challenge ---

func TestFetchPageCtx_MissTest_CloudflareChallenge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `<html><head><title>Just a moment...</title></head><body>
<script>cf-challenge</script></body></html>`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, _, err := s.fetchPageCtx(context.Background(), server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Cloudflare")
}

// --- CanHandleURL edge cases ---

func TestCanHandleURL_MissTest_Malformed(t *testing.T) {
	settings := testSettings("http://dl.getchu.com")
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	assert.False(t, s.CanHandleURL("://bad-url"))
}

// --- Search: disabled ---

func TestSearch_MissTest_Disabled(t *testing.T) {
	settings := testSettings("http://dl.getchu.com")
	settings.Enabled = false
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "12345")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// --- ExtractIDFromURL edge cases ---

func TestExtractIDFromURL_MissTest_URLPath(t *testing.T) {
	settings := testSettings("http://dl.getchu.com")
	s := newScraper(settings, nil, models.FlareSolverrConfig{})

	id, err := s.ExtractIDFromURL("http://dl.getchu.com/i/item12345")
	require.NoError(t, err)
	assert.Equal(t, "12345", id)
}

// --- Search with full detail page parse ---

func TestSearch_MissTest_SuccessWithFullDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "search_keyword") {
			_, _ = fmt.Fprint(w, `<html><body><a href="/i/item12345">Result</a></body></html>`)
			return
		}
		if strings.Contains(r.URL.Path, "/i/item12345") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, `<html><head>
<meta property="og:title" content="DLgetchu Test Title">
<meta name="description" content="Meta description">
</head><body>
<div>作品ID：12345</div>
<div>発売日 2024/06/15</div>
<div>９０分</div>
<a href="dojin_circle_detail.php?id=1">Test Circle</a>
<a href="genre_id=1">Action</a>
<img src="/data/item_img/demo/12345top.jpg">
"/data/item_img/demo/s1.jpg" class="highslide"
</body></html>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	result, err := s.Search(context.Background(), "ABC-123")
	require.NoError(t, err)
	assert.Equal(t, "dlgetchu", result.Source)
	assert.Equal(t, "12345", result.ID)
	assert.Equal(t, "DLgetchu Test Title", result.Title)
	assert.Equal(t, 90, result.Runtime)
	assert.Equal(t, "Test Circle", result.Maker)
}
