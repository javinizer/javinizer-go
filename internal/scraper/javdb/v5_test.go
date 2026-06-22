package javdb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildJavDBDetailHTML creates a complete JavDB detail page
func buildJavDBDetailHTML(id, title, date, runtime, maker, label, director, series, rating, genres, actresses, coverURL string) string {
	genreLinks := ""
	for _, g := range strings.Split(genres, ",") {
		g = strings.TrimSpace(g)
		if g != "" {
			genreLinks += fmt.Sprintf(`<a href="/tags/%s">%s</a>`, g, g)
		}
	}

	actressLinks := ""
	for _, a := range strings.Split(actresses, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			actressLinks += fmt.Sprintf(`<a href="/actors/%s">%s</a>`, a, a)
		}
	}

	coverHTML := ""
	if coverURL != "" {
		coverHTML = fmt.Sprintf(`<div class="column-video-cover"><img class="video-cover" src="%s"/></div>`, coverURL)
	}

	return fmt.Sprintf(`<html>
<head><meta property="og:title" content="%s"/></head>
<body>
<h2 class="title is-4"><strong>%s</strong> %s</h2>
%s
<div class="movie-panel-info">
	<div class="panel-block"><strong>番號：</strong><span class="value">%s</span></div>
	<div class="panel-block"><strong>發行日期：</strong><span class="value">%s</span></div>
	<div class="panel-block"><strong>時長：</strong><span class="value">%s 分鐘</span></div>
	<div class="panel-block"><strong>導演：</strong><span class="value"><a href="/director/1">%s</a></span></div>
	<div class="panel-block"><strong>片商：</strong><span class="value"><a href="/maker/1">%s</a></span></div>
	<div class="panel-block"><strong>發行：</strong><span class="value"><a href="/label/1">%s</a></span></div>
	<div class="panel-block"><strong>系列：</strong><span class="value"><a href="/series/1">%s</a></span></div>
	<div class="panel-block"><strong>評分：</strong><span class="value">%s</span></div>
	<div class="panel-block"><strong>類別：</strong><span class="value">%s</span></div>
	<div class="panel-block"><strong>女優：</strong><span class="value">%s</span></div>
</div>
<span itemprop="description">A great movie about testing</span>
</body>
</html>`, title, id, title, coverHTML, id, date, runtime, director, maker, label, series, rating, genreLinks, actressLinks)
}

// buildJavDBSearchHTML creates a JavDB search results page
func buildJavDBSearchHTML(id, detailPath string) string {
	return fmt.Sprintf(`<html><body>
<div class="movie-list">
	<div class="item">
		<a href="%s">
			<div class="video-title"><strong>%s</strong></div>
			<div class="uid">%s</div>
		</a>
	</div>
</div>
</body></html>`, detailPath, id, id)
}

// TestSearchV5_FullE2E tests Search from search to parse
func TestSearchV5_FullE2E(t *testing.T) {
	searchHTML := buildJavDBSearchHTML("IPX-999", "/v/abcde")
	detailHTML := buildJavDBDetailHTML("IPX-999", "Test Movie", "2024-01-15", "120", "TestMaker", "TestLabel", "TestDirector", "TestSeries", "4.5 (100)", "Action, Drama", "Actress A", "https://pics.dmm.co.jp/cover.jpg")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if strings.Contains(r.URL.Path, "/search") {
			fmt.Fprint(w, searchHTML)
			return
		}
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "IPX-999")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-999", result.ID)
	assert.Equal(t, "TestMaker", result.Maker)
}

// TestSearchV5_Disabled tests Search when disabled
func TestSearchV5_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "IPX-999")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV5_NotFound tests Search when movie is not found
func TestSearchV5_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `<html><body><div class="movie-list"></div></body></html>`)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "NOTFOUND-999")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestScrapeURLV5_Disabled tests ScrapeURL when disabled
func TestScrapeURLV5_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.ScrapeURL(context.Background(), "https://javdb.com/v/abcde")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestGetURLV5 tests GetURL
func TestGetURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://javdb.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	t.Run("empty ID", func(t *testing.T) {
		_, err := s.GetURL(context.Background(), "")
		assert.Error(t, err)
	})

	t.Run("valid ID", func(t *testing.T) {
		url, err := s.GetURL(context.Background(), "IPX-999")
		require.NoError(t, err)
		assert.Contains(t, url, "search")
		assert.Contains(t, url, "IPX-999")
	})
}

// TestCanHandleURLV5 tests CanHandleURL
func TestCanHandleURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://javdb.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://javdb.com/v/abcde", true},
		{"https://example.com/v/abcde", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

// TestExtractIDFromURLV5 tests URL ID extraction
func TestExtractIDFromURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://javdb.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	t.Run("valid video URL", func(t *testing.T) {
		id, err := s.ExtractIDFromURL("https://javdb.com/v/abcde")
		assert.NoError(t, err)
		assert.Equal(t, "abcde", id)
	})

	t.Run("invalid URL", func(t *testing.T) {
		_, err := s.ExtractIDFromURL("https://javdb.com/search?q=test")
		assert.Error(t, err)
	})
}

// TestParseDetailPageV5 tests detail page parsing
func TestParseDetailPageV5(t *testing.T) {
	html := buildJavDBDetailHTML("IPX-001", "Test Title", "2024-01-15", "90", "MakerA", "LabelB", "DirC", "SeriesD", "4.0 (50)", "Action", "ActressA", "https://pics.dmm.co.jp/cover.jpg")

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		enabled:  true,
		baseURL:  "https://javdb.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://javdb.com/v/abcde", "IPX-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-001", result.ID)
	assert.Equal(t, "MakerA", result.Maker)
	assert.Equal(t, "LabelB", result.Label)
	assert.Equal(t, "DirC", result.Director)
	assert.Equal(t, "SeriesD", result.Series)
	assert.Equal(t, 90, result.Runtime)
	assert.NotNil(t, result.ReleaseDate)
	assert.NotEmpty(t, result.Description)
	assert.NotEmpty(t, result.CoverURL)
}

// TestParseDetailPageV5_Minimal tests parsing a minimal page
func TestParseDetailPageV5_Minimal(t *testing.T) {
	html := `<html>
<head><meta property="og:title" content="MIN-001 Title"/></head>
<body>
<h2 class="title is-4"><strong>MIN-001</strong> MIN-001 Title</h2>
<div class="movie-panel-info"></div>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		enabled:  true,
		baseURL:  "https://javdb.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://javdb.com/v/xyz", "MIN-001")
	require.NoError(t, err)
	assert.Equal(t, "MIN-001", result.ID)
}

// TestParseDetailPageV5_NoIDInHTML tests fallback to fallbackID
func TestParseDetailPageV5_NoIDInHTML(t *testing.T) {
	html := `<html><body>
<div class="movie-panel-info"></div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		enabled:  true,
		baseURL:  "https://javdb.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://javdb.com/v/xyz", "FALLBACK-001")
	require.NoError(t, err)
	assert.Equal(t, "FALLBACK-001", result.ID)
}

// TestNormalizeIDForCompareV5 tests ID normalization
func TestNormalizeIDForCompareV5(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"IPX-999", "IPX999"},
		{"abc-123", "ABC123"},
		{"  IPX 999  ", "IPX999"},
		{"IPX_999", "IPX999"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeIDForCompare(tt.input))
		})
	}
}

// TestIdMatchRankV5 tests ID matching
func TestIdMatchRankV5(t *testing.T) {
	tests := []struct {
		candidate string
		target    string
		expected  idMatchType
	}{
		{"IPX-999", "IPX-999", idMatchExact},
		{"IPX-999", "IPX999", idMatchExact},
		{"IPX-099", "IPX-99", idMatchNormalized},
		{"IPX-999A", "IPX-999", idMatchVariant},
		{"ABC-123", "XYZ-789", idMatchNone},
		{"", "IPX-999", idMatchNone},
	}

	for _, tt := range tests {
		t.Run(tt.candidate+"_vs_"+tt.target, func(t *testing.T) {
			result := idMatchRank(tt.candidate, tt.target)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeLabelV5 tests label normalization
func TestNormalizeLabelV5(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  番號：  ", "番號"},
		{"Release:", "release"},
		{"日期", "日期"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeLabel(tt.input))
		})
	}
}

// TestLabelContainsV5 tests label matching
func TestLabelContainsV5(t *testing.T) {
	assert.True(t, labelContains("番號", "番號", "番号"))
	assert.True(t, labelContains("release date", "release"))
	assert.False(t, labelContains("unknown", "番號"))
}

// TestClassifyCastLabelV5 tests cast label classification
func TestClassifyCastLabelV5(t *testing.T) {
	tests := []struct {
		label    string
		expected castLabelKind
	}{
		{"male actor", castLabelMale},
		{"男優", castLabelMale},
		{"男演员", castLabelMale},
		{"女優", castLabelFemale},
		{"actress", castLabelFemale},
		{"演員", castLabelGeneric},
		{"actor", castLabelGeneric},
		{"出演者", castLabelGeneric},
		{"unknown label", castLabelUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			result := classifyCastLabel(tt.label)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseRuntimeV5 tests runtime parsing
func TestParseRuntimeV5(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"120 分鐘", 120},
		{"90", 90},
		{"", 0},
		{"N/A", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseRuntime(tt.input))
		})
	}
}

// TestParseRatingV5 tests rating parsing
func TestParseRatingV5(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  float64
		nilResult bool
	}{
		{"4.5 rating", "4.5 (100 votes)", 9.0, false},
		{"3.0 rating", "3.0 (50)", 6.0, false},
		{"empty", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRating(tt.input)
			if tt.nilResult {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expected, result.Score)
			}
		})
	}
}

// TestIsNotAvailableValueV5 tests N/A value detection
func TestIsNotAvailableValueV5(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"N/A", true},
		{"n/a", true},
		{"none", true},
		{"-", true},
		{"Unknown", false},
		{"valid value", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, isNotAvailableValue(tt.input))
		})
	}
}

// TestIsJavDBVideoCodeV5 tests video code detection
func TestIsJavDBVideoCodeV5(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"abcde", true},
		{"ABC123", true},
		{"ab", false},         // too short
		{"abcdefghijk", true}, // 11 chars, still under 12
		{"IPX-999", false},    // has hyphen
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, isJavDBVideoCode(tt.input))
		})
	}
}

// TestTrimNumericPaddingV5 tests numeric padding trimming
func TestTrimNumericPaddingV5(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"IPX099", "IPX99"},
		{"IPX009", "IPX9"},
		{"IPX000", "IPX0"},
		{"IPX123", "IPX123"},
		{"NOPAD", "NOPAD"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, trimNumericPadding(tt.input))
		})
	}
}

// TestTrimVariantSuffixV5 tests variant suffix trimming
func TestTrimVariantSuffixV5(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"IPX999A", "IPX999"},
		{"IPX999", "IPX999"},
		{"AB", "AB"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, trimVariantSuffix(tt.input))
		})
	}
}

// TestHasDetailMetadataV5 tests metadata presence detection
func TestHasDetailMetadataV5(t *testing.T) {
	tests := []struct {
		name     string
		result   *models.ScraperResult
		expected bool
	}{
		{"nil result", nil, false},
		{"with cover", &models.ScraperResult{CoverURL: "https://example.com/cover.jpg"}, true},
		{"with runtime", &models.ScraperResult{Runtime: 90}, true},
		{"with maker", &models.ScraperResult{Maker: "TestMaker"}, true},
		{"with title different from ID", &models.ScraperResult{Title: "Real Title", ID: "IPX-999"}, true},
		{"title same as ID", &models.ScraperResult{Title: "IPX-999", ID: "IPX-999"}, false},
		{"empty result", &models.ScraperResult{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fallbackID := "IPX-999"
			if tt.result != nil && tt.result.ID != "" {
				fallbackID = tt.result.ID
			}
			assert.Equal(t, tt.expected, hasDetailMetadata(tt.result, fallbackID))
		})
	}
}

// TestFetchPageCtxV5_HTTPError tests fetchPageCtx with non-200 status
func TestFetchPageCtxV5_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.fetchPageCtx(context.Background(), ts.URL+"/test")
	assert.Error(t, err)
}

// TestFindDetailURLCtxV5 tests findDetailURLCtx
func TestFindDetailURLCtxV5(t *testing.T) {
	searchHTML := buildJavDBSearchHTML("IPX-100", "/v/xyz123")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, searchHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	url, err := s.findDetailURLCtx(context.Background(), "IPX-100")
	require.NoError(t, err)
	assert.Contains(t, url, "/v/xyz123")
}

// TestFindDetailURLCtxV5_SingleFallback tests single result fallback
func TestFindDetailURLCtxV5_SingleFallback(t *testing.T) {
	searchHTML := `<html><body>
<div class="movie-list">
	<div class="item">
		<a href="/v/single1">
			<div class="video-title"><strong>OTHER-001</strong></div>
			<div class="uid">OTHER-001</div>
		</a>
	</div>
</div>
</body></html>`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, searchHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// When there's only one result but it doesn't match, it should still return the single candidate
	url, err := s.findDetailURLCtx(context.Background(), "TARGET-999")
	require.NoError(t, err)
	assert.Contains(t, url, "/v/single1")
}

// TestResolveDownloadProxyForHostV5 tests proxy resolution
func TestResolveDownloadProxyForHostV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	t.Run("javdb.com", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("javdb.com")
		assert.True(t, ok)
	})

	t.Run("jdbstatic.com", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("jdbstatic.com")
		assert.True(t, ok)
	})

	t.Run("subdomain", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("pics.jdbstatic.com")
		assert.True(t, ok)
	})

	t.Run("empty", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("")
		assert.False(t, ok)
	})

	t.Run("unrelated", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("example.com")
		assert.False(t, ok)
	})
}

// TestExtractStringListV5 tests string list extraction
func TestExtractStringListV5(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected int
	}{
		{
			"links",
			`<div><a href="/g1">Action</a><a href="/g2">Drama</a></div>`,
			2,
		},
		{
			"plain text split",
			`<div>Action, Drama, Romance</div>`,
			3,
		},
		{
			"empty",
			`<div></div>`,
			0,
		},
		{
			"N/A value",
			`<div>N/A</div>`,
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			sel := doc.Find("div").First()
			result := extractStringList(sel)
			assert.Equal(t, tt.expected, len(result))
		})
	}
}

// TestExtractFirstURLV5 tests first URL extraction from selectors
func TestExtractFirstURLV5(t *testing.T) {
	html := `<div class="column-video-cover"><img class="video-cover" src="https://pics.dmm.co.jp/cover.jpg"/></div>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := extractFirstURL(doc, []string{".column-video-cover img.video-cover"}, "https://javdb.com")
	assert.NotEmpty(t, result)
}

// TestExtractTrailerURLV5 tests trailer URL extraction
func TestExtractTrailerURLV5(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool
	}{
		{
			"video source",
			`<video><source src="https://example.com/trailer.mp4" type="video/mp4"></video>`,
			true,
		},
		{
			"no trailer",
			`<div>no trailer</div>`,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractTrailerURL(doc, "https://javdb.com")
			assert.Equal(t, tt.expected, result != "")
		})
	}
}

// TestExtractScreenshotURLsV5 tests screenshot URL extraction
func TestExtractScreenshotURLsV5(t *testing.T) {
	html := `<div class="tile-images preview-images">
	<a href="https://pics.dmm.co.jp/sample1.jpg">s1</a>
	<a href="https://pics.dmm.co.jp/sample2.jpg">s2</a>
</div>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := extractScreenshotURLs(doc, "https://javdb.com")
	assert.Equal(t, 2, len(result))
}

// TestNewScraperV5 tests scraper creation
func TestNewScraperV5(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
		BaseURL: "https://javdb.com",
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, s)
	assert.True(t, s.enabled)
	assert.Equal(t, "https://javdb.com", s.baseURL)
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

// TestNewScraperV5_WithProxy tests scraper with proxy
func TestNewScraperV5_WithProxy(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
		Proxy:   &models.ProxyConfig{Enabled: true, Profile: "test"},
	}

	s := newScraper(settings, &models.ProxyConfig{Enabled: true, Profile: "global"}, models.FlareSolverrConfig{})
	require.NotNil(t, s)
}

// TestCloseV5 tests Close
func TestCloseV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}
	assert.NoError(t, s.Close())
}

// TestHasWordTokenV5 tests word token matching
func TestHasWordTokenV5(t *testing.T) {
	assert.True(t, hasWordToken("male actor", "male"))
	assert.True(t, hasWordToken("female actress", "female"))
	assert.False(t, hasWordToken("unknown", "male"))
}

// TestExtractActressesV5 tests actress extraction
func TestExtractActressesV5(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected int
	}{
		{
			"link actresses",
			`<div><a href="/a1">Actress A</a><a href="/a2">Actress B</a></div>`,
			2,
		},
		{
			"empty",
			`<div></div>`,
			0,
		},
		{
			"duplicate names",
			`<div><a href="/a1">Actress A</a><a href="/a2">Actress A</a></div>`,
			1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			sel := doc.Find("div").First()
			result := extractActresses(sel)
			assert.Equal(t, tt.expected, len(result))
		})
	}
}
