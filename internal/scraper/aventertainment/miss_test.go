package aventertainment

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

// newAVEHTTPTScraper creates a scraper wired to a httptest.Server via round-tripper.
func newAVEHTTPTScraper(server *httptest.Server, enabled bool) *scraper {
	client := resty.New()
	client.SetTransport(&missRoundTripper{server: server})
	client.SetTimeout(5 * time.Second)

	return &scraper{
		client:      client,
		enabled:     enabled,
		baseURL:     "https://www.aventertainments.com",
		language:    "en",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: enabled, Language: "en", BaseURL: "https://www.aventertainments.com"},
	}
}

// --- Helper: build mock search result page ---
func buildAVESearchHTML(detailURLs ...string) string {
	links := ""
	for _, u := range detailURLs {
		links += fmt.Sprintf(`<a href="%s">Movie</a>`, u)
	}
	return fmt.Sprintf(`<html><body>%s</body></html>`, links)
}

// --- Helper: build mock detail page ---
func buildAVEDetailHTML(productID, title, releaseDate, runtime, studio, description string, genres, actresses []string) string {
	genreHTML := ""
	for _, g := range genres {
		genreHTML += fmt.Sprintf(`<a href="/ppv/cat?cat_id=1">%s</a>`, g)
	}

	actressHTML := ""
	for _, a := range actresses {
		actressHTML += fmt.Sprintf(`<a href="/ppv/idoldetail?id=1">%s</a>`, a)
	}

	categoriesBlock := ""
	if genreHTML != "" {
		categoriesBlock = fmt.Sprintf(`<div class="value-category">%s</div>`, genreHTML)
	}

	return fmt.Sprintf(`<html>
<head>
<title>%s - AV Entertainment</title>
<meta property="og:title" content="%s"/>
</head>
<body>
<div class="section-title"><h1>%s</h1></div>
<div class="product-info-block-rev">
  <div class="single-info">
    <span class="title">Product ID</span>
    <span class="value">%s</span>
    <span class="tag-title">%s</span>
  </div>
  <div class="single-info">
    <span class="title">Studio</span>
    <span class="value"><a href="/ppv/studio?studio=1">%s</a></span>
  </div>
  <div class="single-info">
    <span class="title">Release Date</span>
    <span class="value">%s</span>
  </div>
  <div class="single-info">
    <span class="title">Runtime</span>
    <span class="value">%s</span>
  </div>
  <div class="single-info">
    <span class="title">Actress</span>
    <span class="value">%s</span>
  </div>
  %s
</div>
<div class="product-description">%s</div>
<div id="PlayerCover"><img src="/vodimages/xlarge/test.jpg"/></div>
<a class="lightbox" href="/vodimages/screenshot/large/test-01.jpg">Screenshot</a>
</body>
</html>`, title, title, title, productID, productID, studio, releaseDate, runtime, actressHTML, categoriesBlock, description)
}

// ============================================================
// Search tests
// ============================================================

func TestMiss_Search_Disabled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make request when disabled")
	}))
	defer ts.Close()

	s := newAVEHTTPTScraper(ts, false)
	_, err := s.Search(context.Background(), "1PON-012345-001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestMiss_Search_Success(t *testing.T) {
	detailHTML := buildAVEDetailHTML("1PON-012345-001", "Test Movie Title", "01/15/2024", "60 min", "TestStudio", "A great movie", []string{"Action", "Drama"}, []string{"Yuki Tanaka"})

	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		// First call: search results, subsequent calls: detail page
		if strings.Contains(r.URL.Path, "/ppv/search") {
			fmt.Fprint(w, buildAVESearchHTML("https://www.aventertainments.com/ppv/detail/12345/1/1/new_detail"))
			return
		}
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := newAVEHTTPTScraper(ts, true)
	result, err := s.Search(context.Background(), "1PON-012345-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "aventertainment", result.Source)
	assert.Contains(t, result.Title, "Test Movie Title")
}

func TestMiss_Search_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		// Return search page with no matching links
		fmt.Fprint(w, buildAVESearchHTML())
	}))
	defer ts.Close()

	s := newAVEHTTPTScraper(ts, true)
	_, err := s.Search(context.Background(), "NOMATCH-999")
	require.Error(t, err)
}

func TestMiss_Search_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	s := newAVEHTTPTScraper(ts, true)
	_, err := s.Search(context.Background(), "1PON-012345-001")
	require.Error(t, err)
}

// ============================================================
// ScrapeURL tests
// ============================================================

func TestMiss_ScrapeURL_WrongHost(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make request for non-AVE URL")
	}))
	defer ts.Close()

	s := newAVEHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://example.com/something")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

func TestMiss_ScrapeURL_Success(t *testing.T) {
	detailHTML := buildAVEDetailHTML("1PON-012345-001", "ScrapeURL Test Movie", "02/20/2024", "90 min", "StudioA", "Description here", []string{"Romance"}, []string{"Sakura"})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := newAVEHTTPTScraper(ts, true)
	result, err := s.ScrapeURL(context.Background(), "https://www.aventertainments.com/ppv/detail/12345/1/1/new_detail?item_no=1pon-012345-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "aventertainment", result.Source)
	assert.Contains(t, result.Title, "ScrapeURL Test Movie")
}

func TestMiss_ScrapeURL_404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	s := newAVEHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.aventertainments.com/ppv/detail/99999/1/1/new_detail")
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

	s := newAVEHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.aventertainments.com/ppv/detail/12345/1/1/new_detail")
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

	s := newAVEHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.aventertainments.com/ppv/detail/12345/1/1/new_detail")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindBlocked, scraperErr.Kind)
}

func TestMiss_ScrapeURL_InvalidHTML(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `<html><body>some content</body></html>`)
	}))
	defer ts.Close()

	s := newAVEHTTPTScraper(ts, true)
	result, err := s.ScrapeURL(context.Background(), "https://www.aventertainments.com/ppv/detail/12345/1/1/new_detail?item_no=1pon-012345-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	// Should still return a result even with minimal HTML
	assert.Equal(t, "aventertainment", result.Source)
}

// ============================================================
// GetURL tests
// ============================================================

func TestMiss_GetURL_EmptyID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	s := newAVEHTTPTScraper(ts, true)
	_, err := s.GetURL(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestMiss_GetURL_HTTPURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	s := newAVEHTTPTScraper(ts, true)
	url, err := s.GetURL(context.Background(), "https://www.aventertainments.com/ppv/detail/12345/1/1/new_detail")
	require.NoError(t, err)
	assert.Contains(t, url, "aventertainments.com")
}

func TestMiss_GetURL_SearchFound(t *testing.T) {
	detailHTML := buildAVEDetailHTML("1PON-012345-001", "Test", "01/01/2024", "60 min", "Studio", "Desc", nil, nil)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if strings.Contains(r.URL.Path, "/ppv/search") {
			fmt.Fprint(w, buildAVESearchHTML("https://www.aventertainments.com/ppv/detail/12345/1/1/new_detail"))
			return
		}
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := newAVEHTTPTScraper(ts, true)
	url, err := s.GetURL(context.Background(), "1PON-012345-001")
	require.NoError(t, err)
	assert.Contains(t, url, "aventertainments.com")
}

func TestMiss_GetURL_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `<html><body>no results</body></html>`)
	}))
	defer ts.Close()

	s := newAVEHTTPTScraper(ts, true)
	_, err := s.GetURL(context.Background(), "NOMATCH-999")
	require.Error(t, err)
}

// ============================================================
// CanHandleURL tests
// ============================================================

func TestMiss_CanHandleURL(t *testing.T) {
	s := newAVEHTTPTScraper(httptest.NewServer(nil), true)

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.aventertainments.com/ppv/detail/12345", true},
		{"https://aventertainments.com/ppv/detail/12345", true},
		{"https://sub.aventertainments.com/anything", true},
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
	s := newAVEHTTPTScraper(httptest.NewServer(nil), true)

	tests := []struct {
		name     string
		url      string
		wantErr  bool
		expected string
	}{
		{"item_no query param", "https://www.aventertainments.com/ppv/detail?item_no=1pon-012345-001", false, "1PON-012345-001"},
		{"path-based ID", "https://www.aventertainments.com/ppv/detail/12345/1/1/new_detail", false, ""},
		{"invalid URL", "://invalid", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := s.ExtractIDFromURL(tt.url)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.expected != "" {
					assert.Contains(t, id, tt.expected)
				}
			}
		})
	}
}

// ============================================================
// ResolveSearchQuery tests
// ============================================================

func TestMiss_ResolveSearchQuery(t *testing.T) {
	s := newAVEHTTPTScraper(httptest.NewServer(nil), true)

	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"1pon_012345_001", "1pon_012345_001", true},
		{"carib_012345_001", "carib_012345_001", true},
		{"050419_844-1pon-1080p", "1pon_050419_844", true},
		{"021226_001-carib-720p", "carib_021226_001", true},
		{"050419_844.mp4", "1pon_050419_844", true},
		{"", "", false},
		{"NORMAL-123", "", false},
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
		host         string
		wantHandled  bool
		wantDownload bool
	}{
		{"aventertainments.com", true, true},
		{"www.aventertainments.com", true, true},
		{"example.com", false, false},
		{"", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			dl, _, handled := s.ResolveDownloadProxyForHost(tt.host)
			assert.Equal(t, tt.wantHandled, handled)
			if tt.wantDownload {
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
	assert.Equal(t, "aventertainment", s.Name())
}

func TestMiss_Config(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, Timeout: 30}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	cfg := s.Config()
	require.NotNil(t, cfg)
	assert.True(t, cfg.Enabled)
	// Verify it's a clone
	cfg.Enabled = false
	assert.True(t, s.Config().Enabled)
}

func TestMiss_Close(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.NoError(t, s.Close())
}

// ============================================================
// parseDetailPage unit tests (no HTTP)
// ============================================================

func TestMiss_ParseDetailPage_FullDetail(t *testing.T) {
	html := buildAVEDetailHTML("1PON-012345-001", "Full Movie Title", "03/15/2024", "90 min", "TestStudio", "An exciting film", []string{"Action", "Comedy"}, []string{"Yuki Tanaka"})

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := parseDetailPage(doc, html, "https://www.aventertainments.com/ppv/detail/12345", "1PON-012345-001", "en", false)
	require.NotNil(t, result)
	assert.Equal(t, "1PON-012345-001", result.ID)
	assert.Contains(t, result.Title, "Full Movie Title")
	assert.Equal(t, "TestStudio", result.Maker)
	assert.Equal(t, "An exciting film", result.Description)
	assert.True(t, result.ShouldCropPoster)
	assert.Equal(t, "en", result.Language)
}

func TestMiss_ParseDetailPage_MinimalHTML(t *testing.T) {
	html := `<html><head><title>Minimal Title - AV Entertainment</title></head><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := parseDetailPage(doc, html, "https://www.aventertainments.com/ppv/detail/12345", "1PON-012345-001", "en", false)
	require.NotNil(t, result)
	assert.Contains(t, result.Title, "Minimal Title")
	// ID falls back to the fallbackID when no product ID found in HTML
	assert.NotEmpty(t, result.ID)
}

func TestMiss_ParseDetailPage_WithBonusScreens(t *testing.T) {
	html := `<html>
<head><title>Test</title></head>
<body>
<div id="PlayerCover"><img src="/vodimages/xlarge/test.jpg"/></div>
<a class="lightbox" href="/vodimages/screenshot/large/test-01.jpg">Screenshot 1</a>
<a href="/vodimages/gallery/large/testid/01.webp"><img src="/vodimages/gallery/large/testid/01.webp"/></a>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := parseDetailPage(doc, html, "https://www.aventertainments.com/ppv/detail/12345", "TEST-001", "en", true)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.ScreenshotURL)
}

// ============================================================
// Helper function unit tests
// ============================================================

func TestMiss_ExtractDetailLinks(t *testing.T) {
	html := `<html><body>
<a href="/ppv/detail/12345/1/1/new_detail">Movie 1</a>
<a href="/ppv/detail/67890/1/1/new_detail">Movie 2</a>
<a href="/other/page">Not a detail</a>
</body></html>`

	links := extractDetailLinks(html, "https://www.aventertainments.com")
	assert.Len(t, links, 2)
	assert.Contains(t, links[0], "aventertainments.com")
}

func TestMiss_ExtractDetailLinks_Empty(t *testing.T) {
	links := extractDetailLinks(`<html><body>no links</body></html>`, "https://www.aventertainments.com")
	assert.Empty(t, links)
}

func TestMiss_ExtractCandidateID(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{"tag-title", `<span class="tag-title">1PON-012345-001</span>`, "1PON-012345-001"},
		{"item_no param", `item_no=ABCD-123`, "ABCD-123"},
		{"no match", `<html>nothing</html>`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCandidateID(tt.html)
			if tt.expected == "" {
				assert.Empty(t, result)
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestMiss_FindDate(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{"MM/DD/YYYY", `<span class="value">03/15/2024</span>`, "03/15/2024"},
		{"YYYY-MM-DD", `<span class="value">2024-03-15</span>`, "2024-03-15"},
		{"YYYY/MM/DD", `<span class="value">2024/03/15</span>`, "2024/03/15"},
		{"no date", `<html>no dates here</html>`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findDate(tt.html)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMiss_ParseDate(t *testing.T) {
	tests := []struct {
		input   string
		wantNil bool
		year    int
	}{
		{"03/15/2024", false, 2024},
		{"2024-03-15", false, 2024},
		{"invalid", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseDate(tt.input)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.year, result.Year())
			}
		})
	}
}

func TestMiss_FindRuntime(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		hasValue bool
	}{
		{"clock format", `<span class="value">1:30:00</span>`, true},
		{"minute format", `<span class="value">90 min</span>`, true},
		{"no runtime", `<html>nothing</html>`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findRuntime(tt.html)
			if tt.hasValue {
				assert.NotEmpty(t, result)
			} else {
				assert.Empty(t, result)
			}
		})
	}
}

func TestMiss_ParseRuntime(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"1:30:00", 90},
		{"90 min", 90},
		{"90 minutes", 90},
		{"invalid", 0},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseRuntime(tt.input))
		})
	}
}

func TestMiss_FindMaker(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{"studio link", `<span class="title">Studio</span><span class="value"><a href="/ppv/studio?id=1">TestStudio</a></span>`, "TestStudio"},
		{"studio_products link", `<a href="studio_products.aspx?StudioID=1">MyStudio</a>`, "MyStudio"},
		{"no studio", `<html>nothing</html>`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, findMaker(tt.html))
		})
	}
}

func TestMiss_ExtractDescription(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{"product-description div", `<html><body><div class="product-description">A great movie about adventure</div></body></html>`, "A great movie about adventure"},
		{"meta description", `<html><head><meta name="description" content="Meta description here"/></head><body></body></html>`, "Meta description here"},
		{"no description", `<html><body></body></html>`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractDescription(doc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMiss_ExtractGenres(t *testing.T) {
	html := `<html><body>
<div class="value-category">
<a href="/ppv/cat?cat_id=1">Action</a>
<a href="/ppv/cat?cat_id=2">Drama</a>
</div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	genres := extractGenres(doc.Selection)
	assert.Equal(t, []string{"Action", "Drama"}, genres)
}

func TestMiss_ExtractGenres_Nil(t *testing.T) {
	genres := extractGenres(nil)
	assert.Nil(t, genres)
}

func TestMiss_ExtractActresses(t *testing.T) {
	html := `<html><body>
<a href="/ppv/idoldetail?id=1">田中ゆき</a>
<a href="/ppv/idoldetail?id=2">Jane Smith</a>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	actresses := extractActresses(doc.Selection)
	require.Len(t, actresses, 2)
	assert.Equal(t, "田中ゆき", actresses[0].JapaneseName)
	assert.Equal(t, "Jane", actresses[1].FirstName)
	assert.Equal(t, "Smith", actresses[1].LastName)
}

func TestMiss_ExtractActresses_Nil(t *testing.T) {
	actresses := extractActresses(nil)
	assert.Nil(t, actresses)
}

func TestMiss_ExtractPosterURL(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		hasValue bool
	}{
		{"PlayerCover img", `<html><body><div id="PlayerCover"><img src="/vodimages/xlarge/test.jpg"/></div></body></html>`, true},
		{"og:image", `<html><head><meta property="og:image" content="https://example.com/cover.jpg"/></head><body></body></html>`, true},
		{"no poster", `<html><body></body></html>`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractPosterURL(doc, tt.html, "https://www.aventertainments.com")
			if tt.hasValue {
				assert.NotEmpty(t, result)
			} else {
				assert.Empty(t, result)
			}
		})
	}
}

func TestMiss_ExtractCoverURL(t *testing.T) {
	html := `<html><body>
<a class="lightbox" href="/vodimages/gallery/large/test.jpg">Cover</a>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := extractCoverURL(doc, html, "https://www.aventertainments.com")
	assert.NotEmpty(t, result)
}

func TestMiss_ExtractScreenshotURLs(t *testing.T) {
	html := `<html><body>
<a class="lightbox" href="/vodimages/screenshot/large/test-01.jpg">Screenshot 1</a>
<a class="lightbox" href="/vodimages/screenshot/large/test-02.jpg">Screenshot 2</a>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	screenshots := extractScreenshotURLs(doc, html, "https://www.aventertainments.com", false)
	assert.Len(t, screenshots, 2)
}

func TestMiss_NormalizeInfoLabel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Product ID", "productid"},
		{"商品番号：", "商品番号"},
		{"Studio", "studio"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeInfoLabel(tt.input))
		})
	}
}

func TestMiss_LabelMatchers(t *testing.T) {
	assert.True(t, isProductIDLabel("商品番号"))
	assert.True(t, isProductIDLabel("productid"))
	assert.True(t, isProductIDLabel("itemno"))
	assert.False(t, isProductIDLabel("studio"))

	assert.True(t, isActressLabel("主演女優"))
	assert.True(t, isActressLabel("actress"))
	assert.False(t, isActressLabel("studio"))

	assert.True(t, isStudioLabel("スタジオ"))
	assert.True(t, isStudioLabel("studio"))
	assert.False(t, isStudioLabel("actress"))

	assert.True(t, isCategoryLabel("カテゴリ"))
	assert.True(t, isCategoryLabel("category"))
	assert.False(t, isCategoryLabel("studio"))

	assert.True(t, isReleaseDateLabel("発売日"))
	assert.True(t, isReleaseDateLabel("releasedate"))
	assert.True(t, isReleaseDateLabel("release"))
	assert.False(t, isReleaseDateLabel("studio"))

	assert.True(t, isRuntimeLabel("収録時間"))
	assert.True(t, isRuntimeLabel("runtime"))
	assert.True(t, isRuntimeLabel("length"))
	assert.False(t, isRuntimeLabel("studio"))
}

func TestMiss_NormalizeResolverInput(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/path/to/1pon_012345_001.mp4", "1pon_012345_001"},
		{"C:\\Users\\test\\carib_012345_001.mkv", "carib_012345_001"},
		{"", ""},
		{"   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeResolverInput(tt.input))
		})
	}
}

func TestMiss_IsAVEBonusScreenshotURL(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"/vodimages/gallery/large/testid/01.webp", true},
		{"/vodimages/gallery/large/testid/001.jpg", true},
		{"/vodimages/screenshot/large/test.jpg", false},
		{"", false},
		{"/vodimages/xlarge/test.jpg", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, isAVEBonusScreenshotURL(tt.url))
		})
	}
}

func TestMiss_ApplyLanguage(t *testing.T) {
	s := &scraper{language: "en"}
	result := s.applyLanguage("https://www.aventertainments.com/ppv/detail/12345/1/1/new_detail")
	assert.Contains(t, result, "lang=1")
	assert.Contains(t, result, "culture=en-US")

	sJa := &scraper{language: "ja"}
	result = sJa.applyLanguage("https://www.aventertainments.com/ppv/detail/12345/1/1/new_detail")
	assert.Contains(t, result, "lang=2")
	assert.Contains(t, result, "culture=ja-JP")
}
