package jav321

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

// --- ScrapeURL requires CanHandleURL to pass which checks hostname.
// Since httptest server doesn't have jav321.com hostname,
// we use Search for HTTP-level integration tests. ---

func TestScrapeURL_MissTest_UnhandledURL(t *testing.T) {
	settings := testSettings("https://jp.jav321.com")
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.ScrapeURL(context.Background(), "https://example.com/video/ABC-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not handled")
}

// --- Search with httptest.NewServer: HTTP-level integration tests ---

func TestSearch_MissTest_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" && r.Method == "POST" {
			if err := r.ParseForm(); err == nil && r.FormValue("sn") == "NOTFOUND" {
				_, _ = fmt.Fprint(w, `<html><body><p>No results</p></body></html>`)
				return
			}
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
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" && r.Method == "POST" {
			if err := r.ParseForm(); err == nil && r.FormValue("sn") == "ABC-123" {
				w.Header().Set("Location", server.URL+"/video/abc123")
				w.WriteHeader(http.StatusFound)
				return
			}
		}
		if r.URL.Path == "/video/abc123" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "ABC-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status code 500")
}

// --- getURLCtx: search page with redirect ---

func TestGetURL_MissTest_SearchRedirectToVideo(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" && r.Method == "POST" {
			if err := r.ParseForm(); err == nil && r.FormValue("sn") == "ABC-123" {
				w.Header().Set("Location", server.URL+"/video/abc123")
				w.WriteHeader(http.StatusFound)
				return
			}
		}
		if r.URL.Path == "/video/abc123" {
			_, _ = fmt.Fprint(w, `<html><body>Video page</body></html>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	url, err := s.GetURL(context.Background(), "ABC-123")
	require.NoError(t, err)
	assert.Contains(t, url, "/video/")
}

// --- getURLCtx: search with no results ---

func TestGetURL_MissTest_SearchNoResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			_, _ = fmt.Fprint(w, `<html><body><p>No results found</p></body></html>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.GetURL(context.Background(), "NOTFOUND-9999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- getURLCtx: search page returns non-200 ---

func TestGetURL_MissTest_SearchNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.GetURL(context.Background(), "ABC-123")
	require.Error(t, err)
}

// --- fetchPageCtx: Cloudflare challenge ---

func TestSearch_MissTest_CloudflareChallenge(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" && r.Method == "POST" {
			w.Header().Set("Location", server.URL+"/video/abc123")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `<html><head><title>Just a moment...</title></head><body>
<script>cf-challenge</script></body></html>`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "ABC-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Cloudflare")
}

// --- Search: disabled ---

func TestSearch_MissTest_Disabled(t *testing.T) {
	settings := testSettings("https://jp.jav321.com")
	settings.Enabled = false
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "ABC-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// --- CanHandleURL edge cases ---

func TestCanHandleURL_MissTest_Malformed(t *testing.T) {
	settings := testSettings("https://jp.jav321.com")
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	assert.False(t, s.CanHandleURL("://bad-url"))
}

// --- ExtractIDFromURL: invalid URL ---

func TestExtractIDFromURL_MissTest_InvalidURL(t *testing.T) {
	settings := testSettings("https://jp.jav321.com")
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.ExtractIDFromURL("://bad-url")
	require.Error(t, err)
}

// --- Search: successful full parse ---

func TestSearch_MissTest_SuccessWithFullHTML(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" && r.Method == "POST" {
			if err := r.ParseForm(); err == nil && r.FormValue("sn") == "XYZ-789" {
				w.Header().Set("Location", server.URL+"/video/xyz789")
				w.WriteHeader(http.StatusFound)
				return
			}
		}
		if r.URL.Path == "/video/xyz789" {
			_, _ = fmt.Fprint(w, `<html><head>
<meta property="og:title" content="Full Test Movie - JAV321">
<meta property="og:image" content="/images/cover.jpg">
</head><body>
<div class="panel-heading"><h3>Full Test Movie XYZ-789</h3></div>
<b>品番</b> : XYZ-789<br>
<b>発売日</b> : 2024-06-15<br>
<b>収録時間</b> : 90 minutes<br>
<b>メーカー</b> : <a href="/maker/test">Test Maker</a><br>
<b>シリーズ</b> : <a href="/series/test">Test Series</a><br>
<b>出演者</b> : <a href="/actress/a">花子</a><br>
<a href="/genre/test">Drama</a>
<a href="/snapshot/1"><img src="/shots/1.jpg"></a>
</body></html>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	result, err := s.Search(context.Background(), "XYZ-789")
	require.NoError(t, err)
	assert.Equal(t, "jav321", result.Source)
	assert.Equal(t, "XYZ-789", result.ID)
	assert.Equal(t, "Full Test Movie", result.Title)
	assert.Equal(t, 90, result.Runtime)
	assert.Equal(t, "Test Maker", result.Maker)
	assert.Equal(t, "Test Series", result.Series)
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "花子", result.Actresses[0].JapaneseName)
}
