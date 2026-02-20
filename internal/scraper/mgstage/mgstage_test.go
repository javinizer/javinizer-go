package mgstage

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testConfig creates a test configuration with MGStage enabled
func testConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Scrapers.MGStage.Enabled = true
	cfg.Scrapers.MGStage.RequestDelay = 0 // No delay for tests
	cfg.Scrapers.Proxy.Enabled = false
	return cfg
}

// TestScraperInterfaceCompliance verifies that Scraper implements models.Scraper
func TestScraperInterfaceCompliance(t *testing.T) {
	cfg := testConfig()
	scraper := New(cfg)

	// This will fail to compile if Scraper doesn't implement models.Scraper
	var _ models.Scraper = scraper
	var _ models.ScraperQueryResolver = scraper
}

// TestName verifies the scraper name
func TestName(t *testing.T) {
	cfg := testConfig()
	scraper := New(cfg)

	assert.Equal(t, "mgstage", scraper.Name())
}

// TestIsEnabled tests the enabled state
func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    bool
	}{
		{
			name:    "enabled",
			enabled: true,
			want:    true,
		},
		{
			name:    "disabled",
			enabled: false,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Scrapers.MGStage.Enabled = tt.enabled
			scraper := New(cfg)

			assert.Equal(t, tt.want, scraper.IsEnabled())
		})
	}
}

func TestResolveSearchQuery(t *testing.T) {
	cfg := testConfig()
	scraper := New(cfg)

	tests := []struct {
		name      string
		input     string
		wantQuery string
		wantMatch bool
	}{
		{
			name:      "prefixed ID with hyphen",
			input:     "259LUXU-1806",
			wantQuery: "259LUXU-1806",
			wantMatch: true,
		},
		{
			name:      "prefixed compact ID",
			input:     "259luxu1806",
			wantQuery: "259LUXU-1806",
			wantMatch: true,
		},
		{
			name:      "prefixed ID embedded in filename",
			input:     "[SubsPlease] 259LUXU-1806 [1080p]",
			wantQuery: "259LUXU-1806",
			wantMatch: true,
		},
		{
			name:      "mgstage URL",
			input:     "https://www.mgstage.com/product/product_detail/259LUXU-1806/",
			wantQuery: "259LUXU-1806",
			wantMatch: true,
		},
		{
			name:      "non-prefixed standard ID should not match resolver",
			input:     "ABP-123",
			wantQuery: "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotQuery, gotMatch := scraper.ResolveSearchQuery(tt.input)
			assert.Equal(t, tt.wantMatch, gotMatch)
			assert.Equal(t, tt.wantQuery, gotQuery)
		})
	}
}

// TestConstructor tests the constructor with various configs
func TestConstructor(t *testing.T) {
	tests := []struct {
		name         string
		enabled      bool
		requestDelay int
	}{
		{
			name:         "default config",
			enabled:      true,
			requestDelay: 500,
		},
		{
			name:         "disabled scraper",
			enabled:      false,
			requestDelay: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Scrapers.MGStage.Enabled = tt.enabled
			cfg.Scrapers.MGStage.RequestDelay = tt.requestDelay

			scraper := New(cfg)

			assert.Equal(t, tt.enabled, scraper.IsEnabled())
			assert.Equal(t, time.Duration(tt.requestDelay)*time.Millisecond, scraper.requestDelay)
		})
	}
}

// TestRateLimiting tests that rate limiting is applied
func TestRateLimiting(t *testing.T) {
	cfg := testConfig()
	cfg.Scrapers.MGStage.RequestDelay = 100 // 100ms delay

	scraper := New(cfg)

	// Make two requests and measure time
	start := time.Now()

	// Initialize last request time to simulate a recent request
	scraper.lastRequestTime.Store(time.Now().Add(-50 * time.Millisecond))

	scraper.waitForRateLimit()

	elapsed := time.Since(start)

	// Should have waited ~50ms (100ms delay - 50ms already elapsed)
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(45)) // Allow some tolerance
}

// TestNoRateLimiting tests that no delay when disabled
func TestNoRateLimiting(t *testing.T) {
	cfg := testConfig()
	cfg.Scrapers.MGStage.RequestDelay = 0 // No delay

	scraper := New(cfg)

	start := time.Now()
	scraper.waitForRateLimit()
	elapsed := time.Since(start)

	// Should be nearly instant (< 5ms)
	assert.Less(t, elapsed.Milliseconds(), int64(5))
}

// TestCleanString tests the cleanString helper
func TestCleanString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "removes extra whitespace",
			input: "  hello   world  ",
			want:  "hello world",
		},
		{
			name:  "removes newlines",
			input: "hello\nworld",
			want:  "hello world",
		},
		{
			name:  "removes tabs",
			input: "hello\tworld",
			want:  "hello world",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "removes carriage return",
			input: "hello\r\nworld",
			want:  "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cleanString(tt.input))
		})
	}
}

// TestCleanTitle tests the cleanTitle helper
func TestCleanTitle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "removes site suffix with pipe",
			input: "Movie Title | MGS Video Search",
			want:  "Movie Title",
		},
		{
			name:  "removes Japanese pipe",
			input: "映画タイトル｜MGStage",
			want:  "映画タイトル",
		},
		{
			name:  "clean title unchanged",
			input: "Clean Title",
			want:  "Clean Title",
		},
		{
			name:  "removes - MGStage suffix",
			input: "Movie Title - MGStage",
			want:  "Movie Title",
		},
		{
			name:  "extracts from Japanese brackets",
			input: "「溢れ出るエロオーラを持つ美人お姉さん」：エロ動画・アダルトビデオ -MGS動画",
			want:  "溢れ出るエロオーラを持つ美人お姉さん",
		},
		{
			name:  "extracts from Japanese brackets with subtitle",
			input: "「メインタイトル 【シリーズ名 1234】」：サイト名",
			want:  "メインタイトル 【シリーズ名 1234】",
		},
		{
			name:  "splits on Japanese colon when no brackets",
			input: "Title Text：サイト名",
			want:  "Title Text",
		},
		{
			name:  "drops generic mgstage landing title",
			input: "エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cleanTitle(tt.input))
		})
	}
}

// TestNormalizeIDForSearch tests ID normalization
func TestNormalizeIDForSearch(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "uppercase with hyphen",
			input: "MIDE-123",
			want:  "mide123",
		},
		{
			name:  "lowercase no hyphen",
			input: "mide123",
			want:  "mide123",
		},
		{
			name:  "mixed case with hyphen",
			input: "MiDe-456",
			want:  "mide456",
		},
		{
			name:  "already normalized",
			input: "abc123",
			want:  "abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeIDForSearch(tt.input))
		})
	}
}

// TestExtractIDFromURL tests URL ID extraction
func TestExtractIDFromURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "valid product URL lowercase",
			input: "https://www.mgstage.com/product/product_detail/mide-123/",
			want:  "MIDE-123",
		},
		{
			name:  "valid product URL uppercase",
			input: "https://www.mgstage.com/product/product_detail/MIDE-456/",
			want:  "MIDE-456",
		},
		{
			name:  "invalid URL no match",
			input: "https://www.mgstage.com/search/...",
			want:  "",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "product URL without trailing slash",
			input: "https://www.mgstage.com/product/product_detail/abc-789",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractIDFromURL(tt.input))
		})
	}
}

// TestExtractTableValue tests table value extraction
func TestExtractTableValue(t *testing.T) {
	html := `<html><body>
<table>
<tr><th>品番：</th><td>MIDE-123</td></tr>
<tr><th>配信開始日：</th><td>2023/05/01</td></tr>
<tr><th>収録時間：</th><td>120分</td></tr>
</table>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	tests := []struct {
		name       string
		headerText string
		want       string
	}{
		{
			name:       "extract ID",
			headerText: "品番：",
			want:       "MIDE-123",
		},
		{
			name:       "extract date",
			headerText: "配信開始日：",
			want:       "2023/05/01",
		},
		{
			name:       "extract runtime",
			headerText: "収録時間：",
			want:       "120分",
		},
		{
			name:       "not found",
			headerText: "存在しない：",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractTableValue(doc, tt.headerText))
		})
	}
}

// TestExtractTableLinkValue tests table link value extraction
func TestExtractTableLinkValue(t *testing.T) {
	html := `<html><body>
<table>
<tr><th>メーカー：</th><td><a href="/maker/1">MOODYZ</a></td></tr>
<tr><th>レーベル：</th><td><a href="/label/1">Premium</a></td></tr>
<tr><th>シリーズ：</th><td><a href="/series/1">Best Selection</a></td></tr>
</table>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	tests := []struct {
		name       string
		headerText string
		want       string
	}{
		{
			name:       "extract maker",
			headerText: "メーカー：",
			want:       "MOODYZ",
		},
		{
			name:       "extract label",
			headerText: "レーベル：",
			want:       "Premium",
		},
		{
			name:       "extract series",
			headerText: "シリーズ：",
			want:       "Best Selection",
		},
		{
			name:       "not found",
			headerText: "存在しない：",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractTableLinkValue(doc, tt.headerText))
		})
	}
}

// TestExtractActresses tests actress extraction
func TestExtractActresses(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		wantLen  int
		wantName string
	}{
		{
			name: "single Japanese actress",
			html: `<html><body>
<table>
<tr><th>出演：</th><td><a href="/actress/1">山田花子</a></td></tr>
</table>
</body></html>`,
			wantLen:  1,
			wantName: "山田花子",
		},
		{
			name: "multiple actresses",
			html: `<html><body>
<table>
<tr><th>出演：</th><td><a href="/actress/1">山田花子</a><a href="/actress/2">田中太郎</a></td></tr>
</table>
</body></html>`,
			wantLen:  2,
			wantName: "山田花子",
		},
		{
			name: "no actresses",
			html: `<html><body>
<table>
<tr><th>品番：</th><td>MIDE-123</td></tr>
</table>
</body></html>`,
			wantLen:  0,
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)

			actresses := extractActresses(doc)

			assert.Len(t, actresses, tt.wantLen)
			if tt.wantLen > 0 && tt.wantName != "" {
				assert.Equal(t, tt.wantName, actresses[0].JapaneseName)
			}
		})
	}
}

// TestExtractGenres tests genre extraction
func TestExtractGenres(t *testing.T) {
	tests := []struct {
		name    string
		html    string
		wantLen int
		want    []string
	}{
		{
			name: "multiple genres",
			html: `<html><body>
<table>
<tr><th>ジャンル：</th><td><a href="/genre/1">ギャル</a><a href="/genre/2">巨乳</a></td></tr>
</table>
</body></html>`,
			wantLen: 2,
			want:    []string{"ギャル", "巨乳"},
		},
		{
			name: "single genre",
			html: `<html><body>
<table>
<tr><th>ジャンル：</th><td><a href="/genre/1">単体作品</a></td></tr>
</table>
</body></html>`,
			wantLen: 1,
			want:    []string{"単体作品"},
		},
		{
			name: "no genres",
			html: `<html><body>
<table>
<tr><th>品番：</th><td>MIDE-123</td></tr>
</table>
</body></html>`,
			wantLen: 0,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)

			genres := extractGenres(doc)

			assert.Len(t, genres, tt.wantLen)
			if tt.wantLen > 0 {
				assert.Equal(t, tt.want, genres)
			}
		})
	}
}

// TestExtractRating tests rating extraction
func TestExtractRating(t *testing.T) {
	tests := []struct {
		name      string
		html      string
		wantScore float64
		wantVotes int
		wantNil   bool
	}{
		{
			name: "rating from star class",
			html: `<html><body>
<div class="star_40">Rating</div>
</body></html>`,
			wantScore: 8.0, // 40/5 = 8.0 on 0-10 scale
			wantVotes: 0,
			wantNil:   false,
		},
		{
			name: "no rating",
			html: `<html><body>
<p>No rating here</p>
</body></html>`,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)

			rating := extractRating(doc)

			if tt.wantNil {
				assert.Nil(t, rating)
			} else {
				require.NotNil(t, rating)
				assert.Equal(t, tt.wantScore, rating.Score)
				assert.Equal(t, tt.wantVotes, rating.Votes)
			}
		})
	}
}

// TestExtractCoverURL tests cover URL extraction
func TestExtractCoverURL(t *testing.T) {
	tests := []struct {
		name    string
		html    string
		want    string
		wantErr bool
	}{
		{
			name: "cover from link_magnify class",
			html: `<html><body>
<a class="link_magnify" href="https://example.com/cover.jpg">Enlarge</a>
</body></html>`,
			want: "https://example.com/cover.jpg",
		},
		{
			name: "cover from relative URL",
			html: `<html><body>
<a class="link_magnify" href="/images/cover.jpg">Enlarge</a>
</body></html>`,
			want: "https://www.mgstage.com/images/cover.jpg",
		},
		{
			name: "cover from jacket img",
			html: `<html><body>
<img src="/images/jacket_ps.jpg" />
</body></html>`,
			want: "https://www.mgstage.com/images/jacket_pl.jpg", // ps -> pl upgrade
		},
		{
			name: "no cover found",
			html: `<html><body>
<p>No cover here</p>
</body></html>`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)

			coverURL := extractCoverURL(doc)
			assert.Equal(t, tt.want, coverURL)
		})
	}
}

// TestExtractScreenshots tests screenshot extraction
func TestExtractScreenshots(t *testing.T) {
	tests := []struct {
		name string
		html string
		want []string
	}{
		{
			name: "multiple screenshots",
			html: `<html><body>
<a class="sample_image" href="https://example.com/sample1.jpg">Sample 1</a>
<a class="sample_image" href="https://example.com/sample2.jpg">Sample 2</a>
</body></html>`,
			want: []string{
				"https://example.com/sample1.jpg",
				"https://example.com/sample2.jpg",
			},
		},
		{
			name: "relative URLs converted to absolute",
			html: `<html><body>
<a class="sample_image" href="/images/sample1.jpg">Sample 1</a>
</body></html>`,
			want: []string{"https://www.mgstage.com/images/sample1.jpg"},
		},
		{
			name: "no screenshots",
			html: `<html><body>
<p>No screenshots</p>
</body></html>`,
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)

			screenshots := extractScreenshots(doc)
			assert.Equal(t, tt.want, screenshots)
		})
	}
}

// TestParseHTML tests the full HTML parsing
func TestParseHTML(t *testing.T) {
	productHTML := `<html>
<head><title>MIDE-123 Sample Movie | MGStage</title></head>
<body>
<table>
<tr><th>品番：</th><td>MIDE-123</td></tr>
<tr><th>配信開始日：</th><td>2023/05/01</td></tr>
<tr><th>収録時間：</th><td>120分</td></tr>
<tr><th>メーカー：</th><td><a href="/maker/1">MOODYZ</a></td></tr>
<tr><th>レーベル：</th><td><a href="/label/1">MOODYZ</a></td></tr>
<tr><th>シリーズ：</th><td><a href="/series/1">Sample Series</a></td></tr>
<tr><th>ジャンル：</th><td><a href="/genre/1">Genre1</a><a href="/genre/2">Genre2</a></td></tr>
<tr><th>出演：</th><td><a href="/actress/1">女優名</a></td></tr>
</table>
<p class="txt introduction">This is the description text.</p>
<a class="link_magnify" href="https://example.com/cover.jpg">Enlarge</a>
<a class="sample_image" href="https://example.com/sample1.jpg">Sample 1</a>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(productHTML))
	require.NoError(t, err)

	cfg := testConfig()
	scraper := New(cfg)

	result, err := scraper.parseHTML(doc, "https://www.mgstage.com/product/product_detail/mide-123/")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify all fields
	assert.Equal(t, "mgstage", result.Source)
	assert.Equal(t, "ja", result.Language)
	assert.True(t, result.ShouldCropPoster)
	assert.Equal(t, "MIDE-123", result.ID)
	assert.Equal(t, "MIDE-123 Sample Movie", result.Title)
	assert.Equal(t, "MIDE-123 Sample Movie", result.OriginalTitle)
	assert.Equal(t, "This is the description text.", result.Description)
	assert.Equal(t, 120, result.Runtime)
	assert.Equal(t, "MOODYZ", result.Maker)
	assert.Equal(t, "MOODYZ", result.Label)
	assert.Equal(t, "Sample Series", result.Series)
	assert.Equal(t, []string{"Genre1", "Genre2"}, result.Genres)
	assert.Equal(t, "https://example.com/cover.jpg", result.CoverURL)
	assert.Equal(t, "https://example.com/cover.jpg", result.PosterURL)
	assert.Equal(t, []string{"https://example.com/sample1.jpg"}, result.ScreenshotURL)

	require.Len(t, result.Actresses, 1)
	assert.Equal(t, "女優名", result.Actresses[0].JapaneseName)

	require.NotNil(t, result.ReleaseDate)
	assert.Equal(t, 2023, result.ReleaseDate.Year())
	assert.Equal(t, time.May, result.ReleaseDate.Month())
}

func TestParseHTML_DescriptionFromIntroductionDD(t *testing.T) {
	productHTML := `<html>
<head>
<title>SIRO-5615 Sample Movie | MGStage</title>
<meta property="og:description" content="Meta fallback description">
</head>
<body>
<table>
<tr><th>品番：</th><td>SIRO-5615</td></tr>
</table>
<dl id="introduction">
	<dd>
		<p class="txt introduction"><p>今回応募していただいたのは【りりか 20歳 学生】</p></p>
	</dd>
</dl>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(productHTML))
	require.NoError(t, err)

	cfg := testConfig()
	scraper := New(cfg)

	result, err := scraper.parseHTML(doc, "https://www.mgstage.com/product/product_detail/SIRO-5615/")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "SIRO-5615", result.ID)
	assert.Contains(t, result.Description, "りりか 20歳 学生")
}

func TestParseHTML_GenericLandingPageReturnsNotFound(t *testing.T) {
	landingHTML := `<html>
<head>
<title>エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞</title>
<meta property="og:description" content="エロ動画・アダルトビデオのMGS動画＜プレステージ グループ＞">
</head>
<body>
<p>Landing page content without product metadata</p>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(landingHTML))
	require.NoError(t, err)

	cfg := testConfig()
	scraper := New(cfg)

	result, err := scraper.parseHTML(doc, "https://www.mgstage.com/product/product_detail/clt-069/")
	require.Error(t, err)
	assert.Nil(t, result)

	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

func TestSearch_MismatchedDetailIDReturnsNotFound(t *testing.T) {
	searchURI := "/search/cSearch.php?search_word=gptpj018&type=top&page=1&list_cnt=120"
	detailURI := "/product/product_detail/GPTPJ-018/"

	detailHTML := `<html>
<head><title>OTHER-999 Sample Movie | MGStage</title></head>
<body>
<table>
<tr><th>品番：</th><td>OTHER-999</td></tr>
</table>
</body>
</html>`

	client := resty.New()
	client.SetTransport(&routeRoundTripper{
		routes: map[string]mockHTTPResponse{
			searchURI: {
				statusCode: http.StatusOK,
				body:       `<html><body><p>no exact match</p></body></html>`,
			},
			detailURI: {
				statusCode: http.StatusOK,
				body:       detailHTML,
			},
		},
	})

	scraper := &Scraper{
		client:       client,
		enabled:      true,
		requestDelay: 0,
	}
	scraper.lastRequestTime.Store(time.Time{})

	result, err := scraper.Search("GPTPJ-018")
	require.Error(t, err)
	assert.Nil(t, result)

	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

func TestHasProductSignals_TitleOnlyReturnsFalse(t *testing.T) {
	result := &models.ScraperResult{
		ID:    "GPTPJ-018",
		Title: "Search Results Page",
	}
	assert.False(t, hasProductSignals(result, ""))
}

// TestParseHTMLMinimal tests parsing with minimal data
func TestParseHTMLMinimal(t *testing.T) {
	minimalHTML := `<html>
<head><title>ABC-123</title></head>
<body>
<table>
<tr><th>品番：</th><td>ABC-123</td></tr>
</table>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(minimalHTML))
	require.NoError(t, err)

	cfg := testConfig()
	scraper := New(cfg)

	result, err := scraper.parseHTML(doc, "https://www.mgstage.com/product/product_detail/abc-123/")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "ABC-123", result.ID)
	assert.Equal(t, "ABC-123", result.Title)
	assert.Empty(t, result.Maker)
	assert.Empty(t, result.Genres)
	assert.Empty(t, result.Actresses)
}

// ExampleNew demonstrates basic usage
func ExampleNew() {
	cfg := config.DefaultConfig()
	cfg.Scrapers.MGStage.Enabled = true

	scraper := New(cfg)

	if scraper.IsEnabled() {
		// Use the scraper
		fmt.Println("MGStage scraper is enabled")
	}
}

func TestGetURL_ForbiddenWithProxyHint(t *testing.T) {
	client := resty.New()
	client.SetTransport(&statusRoundTripper{statusCode: http.StatusForbidden})

	scraper := &Scraper{
		client:       client,
		enabled:      true,
		usingProxy:   true,
		requestDelay: 0,
	}
	scraper.lastRequestTime.Store(time.Time{})

	_, err := scraper.GetURL("SIRO-5615")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status code 403")
	assert.Contains(t, err.Error(), "proxy likely blocked")
}

type statusRoundTripper struct {
	statusCode int
}

type mockHTTPResponse struct {
	statusCode int
	body       string
}

type routeRoundTripper struct {
	routes map[string]mockHTTPResponse
}

func (s *statusRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

func (r *routeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	route, ok := r.routes[req.URL.RequestURI()]
	if !ok {
		route = mockHTTPResponse{
			statusCode: http.StatusNotFound,
			body:       "",
		}
	}

	return &http.Response{
		StatusCode: route.statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(route.body)),
		Request:    req,
	}, nil
}
