package fc2

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// missRoundTripper routes all requests to the test server regardless of URL host.
type missRoundTripper struct {
	server *httptest.Server
}

func (rt *missRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	proxyReq := req.Clone(req.Context())
	proxyReq.URL.Scheme = "http"
	proxyReq.URL.Host = rt.server.Listener.Addr().String()
	proxyReq.Host = rt.server.Listener.Addr().String()
	return http.DefaultTransport.RoundTrip(proxyReq)
}

// newFC2HTTPTScraper creates a scraper wired to a httptest.Server via round-tripper.
func newFC2HTTPTScraper(server *httptest.Server, enabled bool) *scraper {
	client := resty.New()
	client.SetTransport(&missRoundTripper{server: server})
	client.SetTimeout(5 * time.Second)

	return &scraper{
		client:      client,
		enabled:     enabled,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: enabled, BaseURL: "https://adult.contents.fc2.com"},
	}
}

// --- Helper: build mock detail page ---
func buildFC2DetailHTMLFull(articleID, title, description, releaseDate, runtime, maker string, genres []string, coverURL string, ratingJSON string) string {
	genreHTML := ""
	for _, g := range genres {
		genreHTML += fmt.Sprintf(`<a class="tagTag" href="/genre/1">%s</a>`, g)
	}

	ldJSON := ""
	if ratingJSON != "" {
		ldJSON = fmt.Sprintf(`<script type="application/ld+json">%s</script>`, ratingJSON)
	}

	return fmt.Sprintf(`<html>
<head>
<meta property="og:title" content="FC2 PPV %s %s"/>
<meta property="og:image" content="%s"/>
<meta property="og:description" content="%s"/>
<meta property="og:video" content="https://example.com/trailer.mp4"/>
%s
</head>
<body>
<div class="items_article_MainitemThumb">
<img src="%s" alt="cover"/>
<div class="items_article_info">%s</div>
</div>
<div class="items_article_headerInfo">
<a href="/users/1">%s</a>
</div>
<div class="items_article_softDevice">
<p>販売日： %s</p>
</div>
<div class="items_article_TagArea">%s</div>
<div class="items_article_SampleImagesArea">
<a href="https://pics.fc2.com/sample1.jpg">Sample 1</a>
<a href="https://pics.fc2.com/sample2.jpg">Sample 2</a>
</div>
</body>
</html>`, articleID, title, coverURL, description, ldJSON, coverURL, runtime, maker, releaseDate, genreHTML)
}

// ============================================================
// Search tests
// ============================================================

func TestMiss_Search_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}
	_, err := s.Search(context.Background(), "FC2-PPV-1234567")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestMiss_Search_Success(t *testing.T) {
	detailHTML := buildFC2DetailHTMLFull("1234567", "Test FC2 Movie", "A great movie", "2024/01/15", "60", "TestMaker", []string{"Amateur", "Creampie"}, "https://pics.fc2.com/cover.jpg", "")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	result, err := s.Search(context.Background(), "FC2-PPV-1234567")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "fc2", result.Source)
	assert.Contains(t, result.ID, "1234567")
	assert.Contains(t, result.Title, "Test FC2 Movie")
}

func TestMiss_Search_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	_, err := s.Search(context.Background(), "FC2-PPV-9999999")
	require.Error(t, err)
}

func TestMiss_Search_InvalidIDFormat(t *testing.T) {
	s := newFC2HTTPTScraper(httptest.NewServer(nil), true)
	_, err := s.Search(context.Background(), "INVALID-ID")
	require.Error(t, err)
}

func TestMiss_Search_SoftNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `<html><body>お探しの商品が見つかりませんでした</body></html>`)
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	_, err := s.Search(context.Background(), "FC2-PPV-1234567")
	require.Error(t, err)
}

func TestMiss_Search_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	_, err := s.Search(context.Background(), "FC2-PPV-1234567")
	require.Error(t, err)
}

func TestMiss_Search_429(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	_, err := s.Search(context.Background(), "FC2-PPV-1234567")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusTooManyRequests, scraperErr.StatusCode)
}

func TestMiss_Search_403(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	_, err := s.Search(context.Background(), "FC2-PPV-1234567")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusForbidden, scraperErr.StatusCode)
}

func TestMiss_Search_IDMismatch(t *testing.T) {
	// Build HTML that explicitly contains a different product ID
	detailHTML := fmt.Sprintf(`<html>
<head>
<meta property="og:title" content="FC2 PPV 7654321 Wrong Movie"/>
<meta property="og:image" content="https://pics.fc2.com/cover.jpg"/>
</head>
<body>
商品ID : FC2 PPV 7654321
</body>
</html>`)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	_, err := s.Search(context.Background(), "FC2-PPV-1234567")
	require.Error(t, err)
}

// ============================================================
// ScrapeURL tests
// ============================================================

func TestMiss_ScrapeURL_WrongHost(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make request for non-FC2 URL")
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://example.com/something")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

func TestMiss_ScrapeURL_Success(t *testing.T) {
	detailHTML := buildFC2DetailHTMLFull("1234567", "ScrapeURL Test Movie", "Description", "2024/02/20", "90", "MakerA", []string{"Action"}, "https://pics.fc2.com/cover.jpg", "")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	result, err := s.ScrapeURL(context.Background(), "https://adult.contents.fc2.com/article/1234567/")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "fc2", result.Source)
	assert.Contains(t, result.Title, "ScrapeURL Test Movie")
}

func TestMiss_ScrapeURL_404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://adult.contents.fc2.com/article/1234567/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

func TestMiss_ScrapeURL_429(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://adult.contents.fc2.com/article/1234567/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindRateLimited, scraperErr.Kind)
}

func TestMiss_ScrapeURL_403(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://adult.contents.fc2.com/article/1234567/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindBlocked, scraperErr.Kind)
}

func TestMiss_ScrapeURL_CannotExtractArticleID(t *testing.T) {
	s := newFC2HTTPTScraper(httptest.NewServer(nil), true)
	_, err := s.ScrapeURL(context.Background(), "https://adult.contents.fc2.com/other/page")
	require.Error(t, err)
}

func TestMiss_ScrapeURL_SoftNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `<html><body>this page may have been deleted</body></html>`)
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://adult.contents.fc2.com/article/1234567/")
	require.Error(t, err)
}

func TestMiss_ScrapeURL_InvalidHTML(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `<html><body>minimal content</body></html>`)
	}))
	defer ts.Close()

	s := newFC2HTTPTScraper(ts, true)
	result, err := s.ScrapeURL(context.Background(), "https://adult.contents.fc2.com/article/1234567/")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.ID, "1234567")
}

// ============================================================
// GetURL tests
// ============================================================

func TestMiss_GetURL_EmptyID(t *testing.T) {
	s := newFC2HTTPTScraper(httptest.NewServer(nil), true)
	_, err := s.GetURL(context.Background(), "")
	require.Error(t, err)
}

func TestMiss_GetURL_HTTPURL(t *testing.T) {
	s := newFC2HTTPTScraper(httptest.NewServer(nil), true)
	url, err := s.GetURL(context.Background(), "https://adult.contents.fc2.com/article/1234567/")
	require.NoError(t, err)
	assert.Contains(t, url, "1234567")
}

func TestMiss_GetURL_ValidID(t *testing.T) {
	s := newFC2HTTPTScraper(httptest.NewServer(nil), true)
	url, err := s.GetURL(context.Background(), "FC2-PPV-1234567")
	require.NoError(t, err)
	assert.Contains(t, url, "article/1234567")
}

func TestMiss_GetURL_InvalidID(t *testing.T) {
	s := newFC2HTTPTScraper(httptest.NewServer(nil), true)
	_, err := s.GetURL(context.Background(), "INVALID")
	require.Error(t, err)
}

// ============================================================
// CanHandleURL tests
// ============================================================

func TestMiss_CanHandleURL(t *testing.T) {
	s := newFC2HTTPTScraper(httptest.NewServer(nil), true)

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://adult.contents.fc2.com/article/1234567/", true},
		{"https://fc2.com/anything", true},
		{"https://sub.fc2.com/page", true},
		{"https://example.com/something", false},
		{"not-a-url", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

// ============================================================
// ExtractIDFromURL tests
// ============================================================

func TestMiss_ExtractIDFromURL(t *testing.T) {
	s := newFC2HTTPTScraper(httptest.NewServer(nil), true)

	tests := []struct {
		name     string
		url      string
		expected string
		wantErr  bool
	}{
		{"valid article URL", "https://adult.contents.fc2.com/article/1234567/", "FC2-PPV-1234567", false},
		{"no article path", "https://adult.contents.fc2.com/other", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := s.ExtractIDFromURL(tt.url)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, id)
			}
		})
	}
}

// ============================================================
// ResolveSearchQuery tests
// ============================================================

func TestMiss_ResolveSearchQuery(t *testing.T) {
	s := newFC2HTTPTScraper(httptest.NewServer(nil), true)

	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"FC2-PPV-1234567", "FC2-PPV-1234567", true},
		{"fc2 ppv 1234567", "FC2-PPV-1234567", true},
		{"1234567", "FC2-PPV-1234567", true},
		{"https://adult.contents.fc2.com/article/1234567/", "FC2-PPV-1234567", true},
		{"", "", false},
		{"INVALID", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, ok := s.ResolveSearchQuery(tt.input)
			assert.Equal(t, tt.ok, ok)
			if tt.expected != "" {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// ============================================================
// ResolveDownloadProxyForHost tests
// ============================================================

func TestMiss_ResolveDownloadProxyForHost(t *testing.T) {
	dlProxy := &models.ProxyConfig{Enabled: true, Profile: "dl"}
	scraperProxy := &models.ProxyConfig{Enabled: true, Profile: "sc"}
	s := &scraper{
		settings: models.ScraperSettings{
			DownloadProxy: dlProxy,
			Proxy:         scraperProxy,
		},
	}

	tests := []struct {
		host        string
		wantHandled bool
	}{
		{"fc2.com", true},
		{"adult.contents.fc2.com", true},
		{"example.com", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			dl, _, handled := s.ResolveDownloadProxyForHost(tt.host)
			assert.Equal(t, tt.wantHandled, handled)
			if tt.wantHandled {
				assert.Equal(t, dlProxy, dl)
			}
		})
	}
}

// ============================================================
// IsEnabled / Name / Config / Close tests
// ============================================================

func TestMiss_IsEnabled(t *testing.T) {
	s1 := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.True(t, s1.IsEnabled())

	s2 := newScraper(&models.ScraperSettings{Enabled: false}, nil, models.FlareSolverrConfig{})
	assert.False(t, s2.IsEnabled())
}

func TestMiss_Name(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.Equal(t, "fc2", s.Name())
}

func TestMiss_Config(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, Timeout: 30}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	cfg := s.Config()
	require.NotNil(t, cfg)
	assert.True(t, cfg.Enabled)
	cfg.Enabled = false
	assert.True(t, s.Config().Enabled)
}

func TestMiss_Close(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.NoError(t, s.Close())
}

// ============================================================
// parseDetailPage unit tests
// ============================================================

func TestMiss_ParseDetailPage_FullDetail(t *testing.T) {
	html := buildFC2DetailHTMLFull("1234567", "Full Movie Title", "Great movie description", "2024/03/15", "60", "TestMaker", []string{"Amateur", "Creampie"}, "https://pics.fc2.com/cover.jpg", "")

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := parseDetailPage(doc, html, "https://adult.contents.fc2.com/article/1234567/", "1234567")
	require.NotNil(t, result)
	assert.Equal(t, "FC2-PPV-1234567", result.ID)
	assert.Contains(t, result.Title, "Full Movie Title")
	assert.Contains(t, result.Description, "Great movie description")
	assert.Equal(t, "TestMaker", result.Maker)
	require.NotNil(t, result.ReleaseDate)
	assert.Equal(t, 2024, result.ReleaseDate.Year())
}

func TestMiss_ParseDetailPage_WithRating(t *testing.T) {
	ratingJSON := `{"@type":"Product","aggregateRating":{"@type":"AggregateRating","ratingValue":4.5,"reviewCount":42}}`
	html := buildFC2DetailHTMLFull("1234567", "Rated Movie", "Desc", "2024/01/01", "30", "Maker", nil, "https://pics.fc2.com/cover.jpg", ratingJSON)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := parseDetailPage(doc, html, "https://adult.contents.fc2.com/article/1234567/", "1234567")
	require.NotNil(t, result)
	require.NotNil(t, result.Rating)
	assert.Equal(t, 4.5, result.Rating.Score)
	assert.Equal(t, 42, result.Rating.Votes)
}

func TestMiss_ParseDetailPage_NoArticleID(t *testing.T) {
	html := `<html><body>no content</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := parseDetailPage(doc, html, "https://adult.contents.fc2.com/other/", "")
	assert.Nil(t, result)
}

// ============================================================
// Helper function unit tests
// ============================================================

func TestMiss_ExtractArticleID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"FC2-PPV-1234567", "1234567"},
		{"fc2 ppv 1234567", "1234567"},
		{"ppv-1234567", "1234567"},
		{"1234567", "1234567"},
		{"https://adult.contents.fc2.com/article/1234567/", "1234567"},
		{"INVALID", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractArticleID(tt.input))
		})
	}
}

func TestMiss_CanonicalFC2ID(t *testing.T) {
	assert.Equal(t, "FC2-PPV-1234567", canonicalFC2ID("1234567"))
	assert.Equal(t, "FC2-PPV-1234567", canonicalFC2ID(" 1234567 "))
}

func TestMiss_StripFC2IDPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"FC2 PPV 1234567 Test Movie", "Test Movie"},
		{"FC2-PPV-1234567: Another Title", "Another Title"},
		{"No Prefix Here", "No Prefix Here"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripFC2IDPrefix(tt.input))
		})
	}
}

func TestMiss_StripSiteSuffix_FC2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Movie Title | FC2", "Movie Title"},
		{"Movie Title ｜ FC2コンテンツ", "Movie Title"},
		{"No suffix", "No suffix"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripSiteSuffix(tt.input))
		})
	}
}

func TestMiss_IsFC2NotFoundPage(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool
	}{
		{"Japanese not found", `<html><body>お探しの商品が見つかりませんでした</body></html>`, true},
		{"English not found", `<html><body>this page may have been deleted</body></html>`, true},
		{"normal page", `<html><body>Normal content</body></html>`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isFC2NotFoundPage(tt.html))
		})
	}
}

func TestMiss_ExtractProductIDFromHTML(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{"product ID", `商品ID : FC2 PPV 1234567`, "1234567"},
		{"no match", `<html>nothing</html>`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractProductIDFromHTML(tt.html))
		})
	}
}

func TestMiss_ParseReleaseDate_FC2(t *testing.T) {
	tests := []struct {
		input   string
		wantNil bool
		year    int
	}{
		{"2024-01-15", false, 2024},
		{"2024/01/15", false, 2024},
		{"invalid", true, 0},
		{"", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseReleaseDate(tt.input)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.year, result.Year())
			}
		})
	}
}

func TestMiss_ParseRuntime_FC2(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"1:30:00", 90},
		{"1:30", 2}, // Interpreted as 1min 30sec by clock regex; rounds to 2min
		{"60 min", 60},
		{"60", 60},
		{"", 0},
		{"invalid", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseRuntime(tt.input))
		})
	}
}

func TestMiss_ExtractInfoValue(t *testing.T) {
	html := `<html><body>
<div class="items_article_softDevice">
<p>販売日： 2024/01/15</p>
<p>再生時間： 60min</p>
</div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	assert.Equal(t, "2024/01/15", extractInfoValue(doc, "販売日"))
	assert.Equal(t, "", extractInfoValue(doc, "nonexistent"))
}

func TestMiss_ExtractTags(t *testing.T) {
	html := `<html><body>
<div class="items_article_TagArea">
<a class="tagTag" href="/genre/1">Amateur</a>
<a class="tagTag" href="/genre/2">Creampie</a>
</div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	tags := extractTags(doc)
	assert.Equal(t, []string{"Amateur", "Creampie"}, tags)
}

func TestMiss_ExtractTags_Empty(t *testing.T) {
	html := `<html><body>no tags</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	tags := extractTags(doc)
	assert.Empty(t, tags)
}

func TestMiss_ExtractScreenshotURLs_FC2(t *testing.T) {
	html := `<html><body>
<div class="items_article_SampleImagesArea">
<a href="https://pics.fc2.com/sample1.jpg">1</a>
<a href="https://pics.fc2.com/sample2.jpg">2</a>
</div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	urls := extractScreenshotURLs(doc, "https://adult.contents.fc2.com/article/1234567/")
	assert.Len(t, urls, 2)
}

func TestMiss_NormalizeURL(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		sourceURL string
		expected  string
	}{
		{"absolute URL", "https://example.com/image.jpg", "https://adult.contents.fc2.com/article/1234567/", "https://example.com/image.jpg"},
		{"protocol-relative", "//example.com/image.jpg", "https://adult.contents.fc2.com/article/1234567/", "https://example.com/image.jpg"},
		{"relative URL", "/image.jpg", "https://adult.contents.fc2.com/article/1234567/", "https://adult.contents.fc2.com/image.jpg"},
		{"empty", "", "https://adult.contents.fc2.com/article/1234567/", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeURL(tt.raw, tt.sourceURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMiss_ExtractRating(t *testing.T) {
	html := `<html><body>
<script type="application/ld+json">{"@type":"Product","aggregateRating":{"@type":"AggregateRating","ratingValue":4.5,"reviewCount":42}}</script>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	rating := extractRating(doc)
	require.NotNil(t, rating)
	assert.Equal(t, 4.5, rating.Score)
	assert.Equal(t, 42, rating.Votes)
}

func TestMiss_ExtractRating_NoLDJSON(t *testing.T) {
	html := `<html><body>no structured data</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	rating := extractRating(doc)
	assert.Nil(t, rating)
}

func TestMiss_ToFloat64(t *testing.T) {
	assert.Equal(t, 4.5, toFloat64(4.5))
	assert.Equal(t, 4.0, toFloat64(4))
	assert.Equal(t, float64(42), toFloat64(int64(42)))
	assert.Equal(t, 3.14, toFloat64("3.14"))
	assert.Equal(t, 0.0, toFloat64("invalid"))
	assert.Equal(t, 0.0, toFloat64(nil))
}

func TestMiss_ToInt(t *testing.T) {
	assert.Equal(t, 42, toInt(42))
	assert.Equal(t, 42, toInt(int64(42)))
	assert.Equal(t, 42, toInt(float64(42.7)))
	assert.Equal(t, 42, toInt("42"))
	assert.Equal(t, 0, toInt("invalid"))
	assert.Equal(t, 0, toInt(nil))
}

func TestMiss_BuildArticleURL(t *testing.T) {
	s := &scraper{baseURL: "https://adult.contents.fc2.com"}
	url := s.buildArticleURL("1234567")
	assert.Equal(t, "https://adult.contents.fc2.com/article/1234567/", url)
}
