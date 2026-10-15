package javbus

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// detailPageHTML returns a minimal but valid JavBus detail page
const detailPageHTML = `<!DOCTYPE html>
<html><head><title>ABC-123</title></head>
<body>
<div class="container">
  <h3>ABC-123 标题</h3>
  <div id="info">
    <p><span class="header">品番:</span> <a href="/">ABC-123</a></p>
    <p><span class="header">発売日:</span> 2024-01-15</p>
    <p><span class="header">収録時間:</span> 120</p>
    <p><span class="header">監督:</span> Director Name</p>
    <p><span class="header">メーカー:</span> <a href="/">Maker Name</a></p>
    <p><span class="header">レーベル:</span> <a href="/">Label Name</a></p>
    <p><span class="header">ジャンル:</span> <a href="/">Genre1</a> <a href="/">Genre2</a></p>
    <p><span class="header">出演者:</span> <a href="/">Actress A</a></p>
  </div>
  <a class="bigImage" href="https://example.com/cover.jpg"><img src="https://example.com/cover.jpg" title="ABC-123"></a>
</div>
</body></html>`

const searchResultHTML = `<!DOCTYPE html>
<html><body>
<div id="waterfall">
  <div class="item">
    <a class="movie-box" href="/ABC-123" title="ABC-123"><date>ABC-123</date><img src="/pics/cover.jpg"></a>
  </div>
</div>
</body></html>`

type javbusFixtureRoundTripper func(*http.Request) (*http.Response, error)

func (f javbusFixtureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestScraperWithServer(t *testing.T, ts *httptest.Server) *scraper {
	t.Helper()
	target, err := url.Parse(ts.URL)
	require.NoError(t, err)
	s := newScraper(&models.ScraperSettings{
		Enabled:   true,
		RateLimit: 0,
		Language:  "ja",
		BaseURL:   ts.URL,
	}, nil, models.FlareSolverrConfig{})
	transport := ts.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	t.Cleanup(transport.CloseIdleConnections)
	s.client.SetRetryCount(0)
	// A server's ordinary transport does not redirect absolute JavBus URLs.
	// Route only the expected fixture requests to loopback and reject the rest.
	s.client.SetTransport(javbusFixtureRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet ||
			!((req.URL.Scheme == "https" && req.URL.Host == "www.javbus.com") ||
				(req.URL.Scheme == target.Scheme && req.URL.Host == target.Host)) {
			t.Errorf("unexpected fixture request: %s %s", req.Method, req.URL)
			return nil, fmt.Errorf("unexpected fixture request: %s %s", req.Method, req.URL)
		}
		switch req.URL.RequestURI() {
		case "/search/ABC-123&type=0&parent=uc", "/ja/ABC-123":
		default:
			t.Errorf("unexpected fixture path: %s", req.URL.RequestURI())
			return nil, fmt.Errorf("unexpected fixture path: %s", req.URL.RequestURI())
		}
		local := req.Clone(req.Context())
		local.URL.Scheme = target.Scheme
		local.URL.Host = target.Host
		local.Host = target.Host
		response, err := transport.RoundTrip(local)
		if response != nil {
			response.Request = req
		}
		return response, err
	}))
	return s
}

func TestScraper_SearchWithMockServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.Contains(path, "search") {
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			fmt.Fprint(w, searchResultHTML)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailPageHTML)
	}))
	defer ts.Close()

	s := newTestScraperWithServer(t, ts)
	result, err := s.Search(context.Background(), "ABC-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ABC-123", result.ID)
	assert.Equal(t, "javbus", result.Source)
	assert.Equal(t, ts.URL+"/ja/ABC-123", result.SourceURL)
}

func TestScraper_ScrapeURLWithMockServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailPageHTML)
	}))
	defer ts.Close()

	s := newTestScraperWithServer(t, ts)

	t.Run("valid JavBus URL", func(t *testing.T) {
		// CanHandleURL checks the hostname, so use javbus.com URL with test server transport
		s2 := newTestScraperWithServer(t, ts)
		result, err := s2.ScrapeURL(context.Background(), "https://www.javbus.com/ABC-123")
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "ABC-123", result.ID)
		assert.Equal(t, "https://www.javbus.com/ja/ABC-123", result.SourceURL)
	})

	t.Run("non-JavBus URL returns not found", func(t *testing.T) {
		_, err := s.ScrapeURL(context.Background(), "https://example.com/ABC-123")
		assert.Error(t, err)
	})
}

func TestScraper_GetURLWithMockServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.Contains(path, "search") {
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			fmt.Fprint(w, searchResultHTML)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailPageHTML)
	}))
	defer ts.Close()

	s := newTestScraperWithServer(t, ts)
	url, err := s.GetURL(context.Background(), "ABC-123")
	require.NoError(t, err)
	assert.Equal(t, ts.URL+"/ja/ABC-123", url)
}

func TestScraper_DisabledSearch(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: false}, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "ABC-123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}
