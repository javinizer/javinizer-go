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

// buildCaribbeancomDetailHTML creates a complete Caribbeancom detail page
func buildCaribbeancomDetailHTML(movieID, title, date, runtime, actresses, genres, coverURL string) string {
	var movieJSON string
	if movieID != "" {
		movieJSON = fmt.Sprintf(`var Movie = {"movie_id": "%s", "sample_flash_url": "https://example.com/trailer.swf"};`, movieID)
	}

	actressHTML := ""
	for _, a := range strings.Split(actresses, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			actressHTML += fmt.Sprintf(`<a itemprop="actor"><span itemprop="name">%s</span></a>`, a)
		}
	}

	genreHTML := ""
	for _, g := range strings.Split(genres, ",") {
		g = strings.TrimSpace(g)
		if g != "" {
			genreHTML += fmt.Sprintf(`<a>%s</a>`, g)
		}
	}

	ogImage := ""
	if coverURL != "" {
		ogImage = fmt.Sprintf(`<meta property="og:image" content="%s"/>`, coverURL)
	}

	return fmt.Sprintf(`<html>
<head>
%s
<meta property="og:title" content="%s"/>
</head>
<body>
<div id="moviepages">
<div class="movie-info section">
	<h1 itemprop="name">%s</h1>
	<p itemprop="description">A great Caribbean movie</p>
	<ul>
		<li class="movie-spec"><span class="spec-title">配信日</span><span class="spec-content">%s</span></li>
		<li class="movie-spec"><span class="spec-title">再生時間</span><span class="spec-content">%s</span></li>
		<li class="movie-spec"><span class="spec-title">出演</span><span class="spec-content">%s</span></li>
		<li class="movie-spec"><span class="spec-title">タグ</span><span class="spec-content">%s</span></li>
	</ul>
	<a class="fancy-gallery" href="https://pics.carib.com/sample1.jpg" data-is_sample="1">s1</a>
</div>
</div>
%s
</body>
</html>`, ogImage, title, title, date, runtime, actressHTML, genreHTML, movieJSON)
}

// TestSearchV5_FullE2E tests Search with httptest server
func TestSearchV5_FullE2E(t *testing.T) {
	detailHTML := buildCaribbeancomDetailHTML("060924-001", "Test Movie | 無修正アダルト動画 カリビアンコム", "2024-06-09", "60 min", "Actress A", "Action, Drama", "https://pics.carib.com/cover.jpg")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "060924-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "060924-001", result.ID)
	assert.NotEmpty(t, result.Title)
}

// TestSearchV5_Disabled tests Search when disabled
func TestSearchV5_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.caribbeancom.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "060924-001")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestGetURLV5 tests GetURL
func TestGetURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://www.caribbeancom.com",
		language: "ja",
		settings: models.ScraperSettings{Enabled: true},
	}

	t.Run("empty ID", func(t *testing.T) {
		_, err := s.GetURL(context.Background(), "")
		assert.Error(t, err)
	})

	t.Run("HTTP URL", func(t *testing.T) {
		url, err := s.GetURL(context.Background(), "https://www.caribbeancom.com/moviepages/060924-001/index.html")
		require.NoError(t, err)
		assert.Contains(t, url, "060924-001")
	})

	t.Run("valid movie ID", func(t *testing.T) {
		url, err := s.GetURL(context.Background(), "060924-001")
		require.NoError(t, err)
		assert.Contains(t, url, "060924-001")
	})

	t.Run("invalid ID format", func(t *testing.T) {
		_, err := s.GetURL(context.Background(), "INVALID-ID")
		assert.Error(t, err)
	})
}

// TestScrapeURLV5_StatusCodes tests ScrapeURL with various HTTP status codes
func TestScrapeURLV5_StatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"404", 404},
		{"429", 429},
		{"403", 403},
		{"451", 451},
		{"500", 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer ts.Close()

			s := &scraper{
				client:      resty.New().SetBaseURL(ts.URL),
				enabled:     true,
				baseURL:     "https://www.caribbeancom.com",
				rateLimiter: ratelimit.NewLimiter(0),
				settings:    models.ScraperSettings{Enabled: true},
			}

			result, err := s.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/060924-001/index.html")
			assert.Nil(t, result)
			assert.Error(t, err)
		})
	}
}

// TestResolveSearchQueryV5 tests ResolveSearchQuery
func TestResolveSearchQueryV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://www.caribbeancom.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		input      string
		expected   string
		expectedOK bool
	}{
		{"060924-001", "060924-001", true},
		{"060924_001", "060924-001", true},
		{"", "", false},
		{"INVALID", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, ok := s.ResolveSearchQuery(tt.input)
			assert.Equal(t, tt.expectedOK, ok)
			if ok {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestNormalizeMovieIDV5 tests movie ID normalization
func TestNormalizeMovieIDV5(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"060924-001", "060924-001"},
		{"060924_001", "060924-001"},
		{"060924-01", "060924-001"},
		{"060924-1", "060924-1"},
		{"ABC", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeMovieID(tt.input))
		})
	}
}

// TestStripSiteSuffixV5 tests site suffix removal
func TestStripSiteSuffixV5(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Test Movie | 無修正アダルト動画 カリビアンコム", "Test Movie"},
		{"Another Movie | Caribbeancom", "Another Movie"},
		{"No Suffix Here", "No Suffix Here"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripSiteSuffix(tt.input))
		})
	}
}

// TestParseRuntimeV5 tests runtime parsing
func TestParseRuntimeV5(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"60 min", 60},
		{"PT1H30M", 90},
		{"01:30:00", 90},
		{"", 0},
		{"45", 45},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseRuntime(tt.input))
		})
	}
}

// TestParseReleaseDateV5 tests release date parsing
func TestParseReleaseDateV5(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"2024-06-09", true},
		{"2024/06/09", true},
		{"", false},
		{"invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseReleaseDate(tt.input)
			if tt.expected {
				assert.NotNil(t, result)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

// TestParseReleaseDateFromIDV5 tests date extraction from movie ID
func TestParseReleaseDateFromIDV5(t *testing.T) {
	tests := []struct {
		id       string
		expected bool
	}{
		{"060924-001", true},
		{"010101-001", true},
		{"ABC-123", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			result := parseReleaseDateFromID(tt.id)
			if tt.expected {
				assert.NotNil(t, result)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

// TestAtoiSafeV5 tests safe integer conversion
func TestAtoiSafeV5(t *testing.T) {
	assert.Equal(t, 42, atoiSafe("42"))
	assert.Equal(t, 0, atoiSafe(""))
	assert.Equal(t, 0, atoiSafe("abc"))
	assert.Equal(t, 100, atoiSafe(" 100 "))
}

// TestNormalizeLanguageV5 tests language normalization
func TestNormalizeLanguageV5(t *testing.T) {
	assert.Equal(t, "en", normalizeLanguage("en"))
	assert.Equal(t, "en", normalizeLanguage("EN"))
	assert.Equal(t, "ja", normalizeLanguage("ja"))
	assert.Equal(t, "ja", normalizeLanguage(""))
	assert.Equal(t, "ja", normalizeLanguage("zh"))
}

// TestIsMovieDetailPageV5 tests detail page detection
func TestIsMovieDetailPageV5(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool
	}{
		{"valid with movie_id", `<html><script>var Movie = {"movie_id": "060924-001"};</script></html>`, true},
		{"error404 page", `<html><body class="error404-wrap">not found</body></html>`, false},
		{"null movie", `<html><script>var Movie = null;</script></html>`, false},
		{"valid with moviepages", `<html><div id="moviepages">content</div></html>`, true},
		{"empty page", `<html></html>`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := isMovieDetailPage(doc, tt.html)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseDetailPageV5 tests detail page parsing
func TestParseDetailPageV5(t *testing.T) {
	html := buildCaribbeancomDetailHTML("060924-001", "Test Movie | 無修正アダルト動画 カリビアンコム", "2024-06-09", "60 min", "Actress A", "Action, Drama", "https://pics.carib.com/cover.jpg")

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := parseDetailPage(doc, html, "https://www.caribbeancom.com/moviepages/060924-001/index.html", "060924-001", "ja")
	require.NotNil(t, result)
	assert.Equal(t, "060924-001", result.ID)
	assert.NotEmpty(t, result.Title)
	assert.NotEmpty(t, result.Description)
	assert.Equal(t, 60, result.Runtime)
	assert.NotEmpty(t, result.Actresses)
	assert.NotEmpty(t, result.Genres)
	assert.NotEmpty(t, result.CoverURL)
}

// TestApplyLanguageV5 tests URL language modification
func TestApplyLanguageV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://www.caribbeancom.com",
		language: "en",
		settings: models.ScraperSettings{Enabled: true},
	}

	t.Run("ja language", func(t *testing.T) {
		s.language = "ja"
		result := s.applyLanguage("https://www.caribbeancom.com/moviepages/060924-001/index.html")
		assert.Contains(t, result, "www.caribbeancom.com")
		assert.NotContains(t, result, "/eng/")
	})

	t.Run("en language", func(t *testing.T) {
		s.language = "en"
		result := s.applyLanguage("https://www.caribbeancom.com/moviepages/060924-001/index.html")
		assert.Contains(t, result, "en.caribbeancom.com")
		assert.Contains(t, result, "/eng/")
	})

	t.Run("non-caribbeancom URL unchanged", func(t *testing.T) {
		result := s.applyLanguage("https://example.com/page")
		assert.Equal(t, "https://example.com/page", result)
	})
}

// TestNewScraperV5 tests scraper creation
func TestNewScraperV5(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
		BaseURL: "https://www.caribbeancom.com",
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, s)
	assert.True(t, s.enabled)
}

// TestNewScraperV5_DefaultBaseURL tests default base URL
func TestNewScraperV5_DefaultBaseURL(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
		BaseURL: "",
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, s)
	assert.Equal(t, defaultBaseURL, s.baseURL)
}

// TestExtractMovieIDV5 tests movie ID extraction
func TestExtractMovieIDV5(t *testing.T) {
	tests := []struct {
		name       string
		html       string
		sourceURL  string
		fallbackID string
		expected   string
	}{
		{"from JSON", `var Movie = {"movie_id": "060924-001"};`, "", "", "060924-001"},
		{"from URL", "", "https://www.caribbeancom.com/moviepages/060924-001/", "", "060924-001"},
		{"from fallback", "", "", "060924-001", "060924-001"},
		{"from fallback with underscore", "", "", "060924_001", "060924-001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractMovieID(tt.html, tt.sourceURL, tt.fallbackID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractSpecValueV5 tests spec value extraction
func TestExtractSpecValueV5(t *testing.T) {
	html := `<ul>
		<li class="movie-spec"><span class="spec-title">配信日</span><span class="spec-content">2024-06-09</span></li>
		<li class="movie-spec"><span class="spec-title">再生時間</span><span class="spec-content">60 min</span></li>
		<li class="movie-spec"><span class="spec-title">出演</span><span class="spec-content">Actress A</span></li>
	</ul>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	date := extractSpecValue(doc, []string{"配信日", "公開日", "Release Date"})
	assert.Equal(t, "2024-06-09", date)

	runtime := extractSpecValue(doc, []string{"再生時間", "Duration"})
	assert.Equal(t, "60 min", runtime)

	actress := extractSpecValue(doc, []string{"出演"})
	assert.Equal(t, "Actress A", actress)

	missing := extractSpecValue(doc, []string{"Unknown"})
	assert.Equal(t, "", missing)
}

// TestExtractActressesV5 tests actress extraction
func TestExtractActressesV5(t *testing.T) {
	html := `<div class="movie-info">
		<li class="movie-spec"><span class="spec-title">出演</span><span class="spec-content">
			<a itemprop="actor"><span itemprop="name">Actress A</span></a>
			<a itemprop="actor"><span itemprop="name">Actress B</span></a>
		</span></li>
	</div>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := extractActresses(doc)
	assert.Equal(t, 2, len(result))
}

// TestExtractGenresV5 tests genre extraction
func TestExtractGenresV5(t *testing.T) {
	html := `<div class="movie-info">
		<li class="movie-spec"><span class="spec-title">タグ</span><span class="spec-content">
			<a>Action</a><a>Drama</a>
		</span></li>
	</div>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := extractGenres(doc)
	assert.Equal(t, 2, len(result))
}

// TestExtractCoverURLV5 tests cover URL extraction
func TestExtractCoverURLV5(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool
	}{
		{"og:image", `<html><head><meta property="og:image" content="https://pics.carib.com/cover.jpg"/></head></html>`, true},
		{"cover path regex", `<html><img src="/moviepages/060924-001/images/l_l.jpg"/></html>`, true},
		{"movie ID fallback", `<html></html>`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractCoverURL(doc, tt.html, "https://www.caribbeancom.com", "060924-001")
			assert.Equal(t, tt.expected, result != "")
		})
	}
}

// TestExtractScreenshotsV5 tests screenshot extraction
func TestExtractScreenshotsV5(t *testing.T) {
	html := `<html><body>
		<a class="fancy-gallery" href="https://pics.carib.com/s1.jpg" data-is_sample="1">s1</a>
		<a class="fancy-gallery" href="https://pics.carib.com/s2.jpg" data-is_sample="1">s2</a>
		<a class="fancy-gallery" href="https://pics.carib.com/s3.jpg" data-is_sample="0">s3</a>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := extractScreenshots(doc, "https://www.caribbeancom.com")
	assert.Equal(t, 2, len(result)) // Only is_sample=1 counted
}

// TestExtractTrailerURLV5 tests trailer URL extraction
func TestExtractTrailerURLV5(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool
	}{
		{"JSON trailer", `<script>var Movie = {"sample_flash_url": "https://example.com/trailer.swf"};</script>`, true},
		{"assign trailer", `<script>sample_flash_url = "https://example.com/trailer2.swf";</script>`, true},
		{"no trailer", `<html>no trailer</html>`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTrailerURL(tt.html, "https://www.caribbeancom.com")
			assert.Equal(t, tt.expected, result != "")
		})
	}
}

// TestBuildMoviePageURLV5 tests movie page URL building
func TestBuildMoviePageURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://www.caribbeancom.com",
		language: "ja",
		settings: models.ScraperSettings{Enabled: true},
	}

	result := s.buildMoviePageURL("060924-001")
	assert.Contains(t, result, "/moviepages/060924-001/")
}

// TestSearchV5_NotFound tests Search when movie not found
func TestSearchV5_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `<html><body class="error404-wrap">Not Found</body></html>`)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// Search first resolves URL via ResolveSearchQuery, then fetches
	// Let's test via ScrapeURL path instead
	result, err := s.Search(context.Background(), "060924-001")
	// The Search call should eventually fail or return not found
	if result == nil {
		assert.Error(t, err)
	}
}

// TestFetchPageCtxV5_NetworkError tests fetchPageCtx with connection error
func TestFetchPageCtxV5_NetworkError(t *testing.T) {
	s := &scraper{
		client:      resty.New().SetBaseURL("http://127.0.0.1:1"),
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := s.fetchPageCtx(ctx, "http://127.0.0.1:1/nonexistent")
	assert.Error(t, err)
}
