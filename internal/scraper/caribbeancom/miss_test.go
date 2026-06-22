package caribbeancom

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

// newCaribHTTPTScraper creates a scraper wired to a httptest.Server via round-tripper.
func newCaribHTTPTScraper(server *httptest.Server, enabled bool) *scraper {
	client := resty.New()
	client.SetTransport(&missRoundTripper{server: server})
	client.SetTimeout(5 * time.Second)

	return &scraper{
		client:      client,
		enabled:     enabled,
		baseURL:     "https://www.caribbeancom.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: enabled, Language: "ja", BaseURL: "https://www.caribbeancom.com"},
	}
}

// --- Helper: build mock detail page ---
func buildCaribDetailHTML(movieID, title, description, releaseDate, runtime string, genres, actresses []string) string {
	genreHTML := ""
	for _, g := range genres {
		genreHTML += fmt.Sprintf(`<a>%s</a>`, g)
	}

	actressHTML := ""
	for _, a := range actresses {
		actressHTML += fmt.Sprintf(`<a itemprop="actor"><span itemprop="name">%s</span></a>`, a)
	}

	specHTML := ""
	if releaseDate != "" {
		specHTML += fmt.Sprintf(`<li class="movie-spec"><span class="spec-title">配信日</span><span class="spec-content">%s</span></li>`, releaseDate)
	}
	if runtime != "" {
		specHTML += fmt.Sprintf(`<li class="movie-spec"><span class="spec-title">再生時間</span><span class="spec-content">%s</span></li>`, runtime)
	}
	if len(genres) > 0 {
		specHTML += fmt.Sprintf(`<li class="movie-spec"><span class="spec-title">タグ</span><span class="spec-content">%s</span></li>`, genreHTML)
	}
	if len(actresses) > 0 {
		specHTML += fmt.Sprintf(`<li class="movie-spec"><span class="spec-title">出演</span><span class="spec-content">%s</span></li>`, actressHTML)
	}

	return fmt.Sprintf(`<html>
<head>
<meta property="og:title" content="%s | Caribbeancom"/>
<meta property="og:image" content="https://www.caribbeancom.com/moviepages/%s/images/l_l.jpg"/>
<script>var Movie = {"movie_id": "%s", "sample_flash_url": "https://www.caribbeancom.com/sample/%s/sample.mp4"};</script>
</head>
<body>
<div id="moviepages">
<div class="movie-info section">
<h1 itemprop="name">%s</h1>
<p itemprop="description">%s</p>
<ul>%s</ul>
</div>
</div>
<a class="fancy-gallery" href="https://www.caribbeancom.com/moviepages/%s/images/01.jpg" data-is_sample="1">Screenshot</a>
</body>
</html>`, title, movieID, movieID, movieID, title, description, specHTML, movieID)
}

// ============================================================
// Search tests
// ============================================================

func TestMiss_Search_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.caribbeancom.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}
	_, err := s.Search(context.Background(), "012345-001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestMiss_Search_Success(t *testing.T) {
	detailHTML := buildCaribDetailHTML("012345-001", "Test Caribbean Movie", "A tropical adventure", "2024/01/15", "PT1H30M", []string{"Amateur", "Creampie"}, []string{"田中ゆき"})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := newCaribHTTPTScraper(ts, true)
	result, err := s.Search(context.Background(), "012345-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "caribbeancom", result.Source)
	assert.Contains(t, result.Title, "Test Caribbean Movie")
}

func TestMiss_Search_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	s := newCaribHTTPTScraper(ts, true)
	_, err := s.Search(context.Background(), "999999-999")
	require.Error(t, err)
}

func TestMiss_Search_InvalidIDFormat(t *testing.T) {
	s := newCaribHTTPTScraper(httptest.NewServer(nil), true)
	_, err := s.Search(context.Background(), "INVALID-ID")
	require.Error(t, err)
}

func TestMiss_Search_SoftNotFound(t *testing.T) {
	// Returns 200 but with var Movie = null
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `<html><body><script>var Movie = null;</script></body></html>`)
	}))
	defer ts.Close()

	s := newCaribHTTPTScraper(ts, true)
	_, err := s.Search(context.Background(), "012345-001")
	require.Error(t, err)
}

func TestMiss_Search_404Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	s := newCaribHTTPTScraper(ts, true)
	_, err := s.Search(context.Background(), "012345-001")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// ============================================================
// ScrapeURL tests
// ============================================================

func TestMiss_ScrapeURL_WrongHost(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make request for non-Caribbeancom URL")
	}))
	defer ts.Close()

	s := newCaribHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://example.com/something")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

func TestMiss_ScrapeURL_Success(t *testing.T) {
	detailHTML := buildCaribDetailHTML("012345-001", "ScrapeURL Test Movie", "Great description", "2024/02/20", "PT1H30M", []string{"Action"}, []string{"Sato Hanako"})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := newCaribHTTPTScraper(ts, true)
	result, err := s.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/012345-001/index.html")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "caribbeancom", result.Source)
	assert.Contains(t, result.Title, "ScrapeURL Test Movie")
}

func TestMiss_ScrapeURL_404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	s := newCaribHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/999999-999/index.html")
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

	s := newCaribHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/012345-001/index.html")
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

	s := newCaribHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/012345-001/index.html")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindBlocked, scraperErr.Kind)
}

func TestMiss_ScrapeURL_SoftNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `<html><body><div class="error404-wrap">Not Found</div></body></html>`)
	}))
	defer ts.Close()

	s := newCaribHTTPTScraper(ts, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/012345-001/index.html")
	require.Error(t, err)
}

func TestMiss_ScrapeURL_CannotExtractID(t *testing.T) {
	s := newCaribHTTPTScraper(httptest.NewServer(nil), true)
	_, err := s.ScrapeURL(context.Background(), "https://www.caribbeancom.com/other/page")
	require.Error(t, err)
}

// ============================================================
// GetURL tests
// ============================================================

func TestMiss_GetURL_EmptyID(t *testing.T) {
	s := newCaribHTTPTScraper(httptest.NewServer(nil), true)
	_, err := s.GetURL(context.Background(), "")
	require.Error(t, err)
}

func TestMiss_GetURL_HTTPURL(t *testing.T) {
	s := newCaribHTTPTScraper(httptest.NewServer(nil), true)
	url, err := s.GetURL(context.Background(), "https://www.caribbeancom.com/moviepages/012345-001/index.html")
	require.NoError(t, err)
	assert.Contains(t, url, "caribbeancom.com")
}

func TestMiss_GetURL_ValidID(t *testing.T) {
	s := newCaribHTTPTScraper(httptest.NewServer(nil), true)
	url, err := s.GetURL(context.Background(), "012345-001")
	require.NoError(t, err)
	assert.Contains(t, url, "moviepages")
	assert.Contains(t, url, "012345-001")
}

func TestMiss_GetURL_InvalidID(t *testing.T) {
	s := newCaribHTTPTScraper(httptest.NewServer(nil), true)
	_, err := s.GetURL(context.Background(), "INVALID")
	require.Error(t, err)
}

// ============================================================
// CanHandleURL tests
// ============================================================

func TestMiss_CanHandleURL(t *testing.T) {
	s := newCaribHTTPTScraper(httptest.NewServer(nil), true)

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.caribbeancom.com/moviepages/012345-001/index.html", true},
		{"https://caribbeancom.com/anything", true},
		{"https://en.caribbeancom.com/eng/moviepages/012345-001/index.html", true},
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
	s := newCaribHTTPTScraper(httptest.NewServer(nil), true)

	tests := []struct {
		name     string
		url      string
		expected string
		wantErr  bool
	}{
		{"valid movie URL", "https://www.caribbeancom.com/moviepages/012345-001/index.html", "012345-001", false},
		{"underscore ID", "https://www.caribbeancom.com/moviepages/012345_001/index.html", "012345-001", false},
		{"no moviepages path", "https://www.caribbeancom.com/other", "", true},
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
	s := newCaribHTTPTScraper(httptest.NewServer(nil), true)

	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"012345-001", "012345-001", true},
		{"012345_001", "012345-001", true},
		{"carib-012345-001", "012345-001", true},
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
		{"caribbeancom.com", true},
		{"www.caribbeancom.com", true},
		{"en.caribbeancom.com", true},
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
	assert.Equal(t, "caribbeancom", s.Name())
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
	html := buildCaribDetailHTML("012345-001", "Full Movie Title", "A tropical adventure", "2024/03/15", "PT1H30M", []string{"Amateur", "Creampie"}, []string{"田中ゆき"})

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := parseDetailPage(doc, html, "https://www.caribbeancom.com/moviepages/012345-001/index.html", "012345-001", "ja")
	require.NotNil(t, result)
	assert.Equal(t, "012345-001", result.ID)
	assert.Contains(t, result.Title, "Full Movie Title")
	assert.Equal(t, "A tropical adventure", result.Description)
	assert.True(t, result.ShouldCropPoster)
	assert.Equal(t, "ja", result.Language)
}

func TestMiss_ParseDetailPage_MinimalHTML(t *testing.T) {
	html := `<html>
<head><meta property="og:title" content="Minimal Title | Caribbeancom"/></head>
<body>
<script>var Movie = {"movie_id": "012345-001"};</script>
<div id="moviepages"><div class="movie-info"><h1 itemprop="name">Minimal Title</h1></div></div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := parseDetailPage(doc, html, "https://www.caribbeancom.com/moviepages/012345-001/index.html", "012345-001", "ja")
	require.NotNil(t, result)
	assert.Contains(t, result.Title, "Minimal Title")
}

func TestMiss_ParseDetailPage_NoTitle(t *testing.T) {
	html := `<html><body><script>var Movie = {"movie_id": "012345-001"};</script></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := parseDetailPage(doc, html, "https://www.caribbeancom.com/moviepages/012345-001/index.html", "012345-001", "ja")
	require.NotNil(t, result)
	// Falls back to ID as title
	assert.NotEmpty(t, result.Title)
}

// ============================================================
// Helper function unit tests
// ============================================================

func TestMiss_IsMovieDetailPage(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool
	}{
		{"movie_id JSON", `<script>var Movie = {"movie_id": "012345-001"};</script>`, true},
		{"null movie", `<script>var Movie = null;</script>`, false},
		{"404 wrap", `<div class="error404-wrap">Not Found</div>`, false},
		{"movie-info present", `<div class="movie-info">content</div>`, true},
		{"h1 itemprop", `<h1 itemprop="name">Title</h1>`, true},
		{"empty", ``, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			assert.Equal(t, tt.expected, isMovieDetailPage(doc, tt.html))
		})
	}
}

func TestMiss_ExtractMovieID(t *testing.T) {
	tests := []struct {
		name       string
		html       string
		sourceURL  string
		fallbackID string
		expected   string
	}{
		{"from JSON", `var Movie = {"movie_id": "012345-001"};`, "", "", "012345-001"},
		{"from URL", "", "https://www.caribbeancom.com/moviepages/012345-001/", "", "012345-001"},
		{"from fallback", "", "", "012345-001", "012345-001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractMovieID(tt.html, tt.sourceURL, tt.fallbackID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMiss_ExtractSpecValue(t *testing.T) {
	html := `<html><body>
<li class="movie-spec"><span class="spec-title">配信日</span><span class="spec-content">2024/01/15</span></li>
<li class="movie-spec"><span class="spec-title">再生時間</span><span class="spec-content">PT1H30M</span></li>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	assert.Equal(t, "2024/01/15", extractSpecValue(doc, []string{"配信日"}))
	assert.Equal(t, "PT1H30M", extractSpecValue(doc, []string{"再生時間"}))
	assert.Equal(t, "", extractSpecValue(doc, []string{"nonexistent"}))
}

func TestMiss_ExtractActresses_Carib(t *testing.T) {
	html := `<html><body>
<li class="movie-spec"><span class="spec-title">出演</span><span class="spec-content">
<a itemprop="actor"><span itemprop="name">田中ゆき</span></a>
<a itemprop="actor"><span itemprop="name">佐藤花子</span></a>
</span></li>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	actresses := extractActresses(doc)
	require.Len(t, actresses, 2)
	assert.Equal(t, "田中ゆき", actresses[0].JapaneseName)
	assert.Equal(t, "佐藤花子", actresses[1].JapaneseName)
}

func TestMiss_ExtractGenres_Carib(t *testing.T) {
	html := `<html><body>
<li class="movie-spec"><span class="spec-title">タグ</span><span class="spec-content">
<a>Amateur</a>
<a>Creampie</a>
</span></li>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	genres := extractGenres(doc)
	assert.Equal(t, []string{"Amateur", "Creampie"}, genres)
}

func TestMiss_ExtractCoverURL_Carib(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		hasValue bool
	}{
		{"og:image", `<meta property="og:image" content="https://www.caribbeancom.com/moviepages/012345-001/images/l_l.jpg"/>`, true},
		{"regex match", `/moviepages/012345-001/images/l_l.jpg`, true},
		{"fallback by ID", `<html>no cover</html>`, true}, // Falls back to constructed URL
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractCoverURL(doc, tt.html, "https://www.caribbeancom.com", "012345-001")
			if tt.hasValue {
				assert.NotEmpty(t, result)
			} else {
				assert.Empty(t, result)
			}
		})
	}
}

func TestMiss_ExtractScreenshots_Carib(t *testing.T) {
	html := `<html><body>
<a class="fancy-gallery" href="https://www.caribbeancom.com/moviepages/012345-001/images/01.jpg" data-is_sample="1">1</a>
<a class="fancy-gallery" href="https://www.caribbeancom.com/moviepages/012345-001/images/02.jpg" data-is_sample="1">2</a>
<a class="fancy-gallery" href="https://www.caribbeancom.com/moviepages/012345-001/images/03.jpg" data-is_sample="0">Not sample</a>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	screenshots := extractScreenshots(doc, "https://www.caribbeancom.com")
	assert.Len(t, screenshots, 2) // Only is_sample=1 entries
}

func TestMiss_ExtractTrailerURL_Carib(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{"JSON sample_flash_url", `var Movie = {"sample_flash_url": "https://www.caribbeancom.com/sample/test.mp4"};`, "https://www.caribbeancom.com/sample/test.mp4"},
		{"assignment", `sample_flash_url = "https://www.caribbeancom.com/sample/test2.mp4";`, "https://www.caribbeancom.com/sample/test2.mp4"},
		{"no trailer", `<html>nothing</html>`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTrailerURL(tt.html, "https://www.caribbeancom.com")
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMiss_ParseRuntime(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"PT1H30M", 90},
		{"PT2H0M", 120},
		{"1:30:00", 90},
		{"1:30", 90},
		{"90 min", 90},
		{"90分", 90},
		{"", 0},
		{"invalid", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseRuntime(tt.input))
		})
	}
}

func TestMiss_ParseReleaseDate(t *testing.T) {
	tests := []struct {
		input   string
		wantNil bool
		year    int
	}{
		{"2024-01-15", false, 2024},
		{"2024/01/15", false, 2024},
		{"01-15-2024", false, 2024},
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

func TestMiss_ParseReleaseDateFromID(t *testing.T) {
	tests := []struct {
		id      string
		wantNil bool
		month   int
	}{
		{"012345-001", false, 1}, // 01/23/45 -> month=01 (January)
		{"061524-001", false, 6}, // 06/15/24 -> June 15, 2024
		{"invalid", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			result := parseReleaseDateFromID(tt.id)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.month, int(result.Month()))
			}
		})
	}
}

func TestMiss_NormalizeMovieID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"012345-001", "012345-001"},
		{"012345_001", "012345-001"},
		{"012345-01", "012345-001"}, // 2-digit suffix padded
		{"UPPER", "upper"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeMovieID(tt.input))
		})
	}
}

func TestMiss_StripSiteSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Movie Title | 無修正アダルト動画 カリビアンコム", "Movie Title"},
		{"Movie Title | Caribbeancom", "Movie Title"},
		{"No suffix", "No suffix"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripSiteSuffix(tt.input))
		})
	}
}

func TestMiss_NormalizeLanguage(t *testing.T) {
	assert.Equal(t, "en", normalizeLanguage("en"))
	assert.Equal(t, "en", normalizeLanguage("EN"))
	assert.Equal(t, "ja", normalizeLanguage("ja"))
	assert.Equal(t, "ja", normalizeLanguage("fr")) // Default to ja
}

func TestMiss_AtoiSafe(t *testing.T) {
	assert.Equal(t, 42, atoiSafe("42"))
	assert.Equal(t, 0, atoiSafe(""))
	assert.Equal(t, 0, atoiSafe("abc"))
	assert.Equal(t, 0, atoiSafe("  "))
}

func TestMiss_SelectMovieInfoRoot(t *testing.T) {
	html := `<html><body>
<div id="moviepages"><div class="movie-info section"><p>content</p></div></div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	root := selectMovieInfoRoot(doc)
	assert.True(t, root.Length() > 0)
}

func TestMiss_SelectMovieInfoRoot_Fallback(t *testing.T) {
	html := `<html><body><p>minimal</p></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	root := selectMovieInfoRoot(doc)
	assert.True(t, root.Length() > 0) // Falls back to doc.Selection
}
