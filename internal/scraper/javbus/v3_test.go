package javbus

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

// TestScrapeURLV3_HTTPStatusErrors tests ScrapeURL with various HTTP error codes
func TestScrapeURLV3_HTTPStatusErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"404 not found", 404, "not found"},
		{"429 rate limited", 429, "rate limited"},
		{"403 forbidden", 403, "forbidden"},
		{"451 unavailable", 451, "unavailable for legal reasons"},
		{"500 internal error", 500, "internal server error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				fmt.Fprint(w, tt.body)
			}))
			defer ts.Close()

			client := resty.New().SetBaseURL(ts.URL)
			scraper := &scraper{
				client:      client,
				enabled:     true,
				baseURL:     "https://www.javbus.com",
				rateLimiter: ratelimit.NewLimiter(0),
				settings:    models.ScraperSettings{Enabled: true},
			}

			_, err := scraper.ScrapeURL(context.Background(), "https://www.javbus.com/IPX-999")
			require.Error(t, err)
		})
	}
}

// TestFetchPageCtxV3_ChallengePages tests fetchPageCtx with challenge pages
func TestFetchPageCtxV3_ChallengePages(t *testing.T) {
	t.Run("driver verify challenge", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(200)
			fmt.Fprint(w, `<html><body>/doc/driver-verify?referer=/test</body></html>`)
		}))
		defer ts.Close()

		client := resty.New().SetBaseURL(ts.URL)
		scraper := &scraper{
			client:      client,
			enabled:     true,
			baseURL:     ts.URL,
			rateLimiter: ratelimit.NewLimiter(0),
			settings:    models.ScraperSettings{Enabled: true},
		}

		_, _, err := scraper.fetchPageCtx(context.Background(), ts.URL+"/test")
		require.Error(t, err)
	})
}

// TestIsJavbusChallengePageV3 tests isJavbusChallengePage
func TestIsJavbusChallengePageV3(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool
	}{
		{"empty", "", false},
		{"driver verify", `<html>/doc/driver-verify?referer=/test</html>`, true},
		{"age verification", `<html>age verification javbus</html>`, true},
		{"normal page", `<html><body>Normal content</body></html>`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isJavbusChallengePage(tt.html))
		})
	}
}

// TestValidateScraperSettingsV3 tests validateScraperSettings
func TestValidateScraperSettingsV3(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		require.NoError(t, validateScraperSettings(&models.ScraperSettings{Enabled: true, RateLimit: 100, RetryCount: 3, Timeout: 30}))
	})
}

// TestExtractInfoValueV3 tests extractInfoValue with various label formats
func TestExtractInfoValueV3(t *testing.T) {
	html := `
<!DOCTYPE html>
<html><body>
<div id="info">
<p><span class="header">品番:</span> IPX-123</p>
<p><span class="header">発売日:</span> 2024-01-15</p>
<p>Director: Some Director</p>
</div>
</body></html>`

	doc := parseHTMLDoc(t, html)

	t.Run("find by Japanese label", func(t *testing.T) {
		val := extractInfoValue(doc, []string{"品番"})
		assert.Equal(t, "IPX-123", val)
	})

	t.Run("find by date label", func(t *testing.T) {
		val := extractInfoValue(doc, []string{"発売日"})
		assert.Equal(t, "2024-01-15", val)
	})
}

// TestExtractInfoLinkValueV3 tests extractInfoLinkValue
func TestExtractInfoLinkValueV3(t *testing.T) {
	html := `
<!DOCTYPE html>
<html><body>
<div id="info">
<p><span class="header">メーカー:</span> <a href="/studio/1">Test Maker</a></p>
</div>
</body></html>`

	doc := parseHTMLDoc(t, html)
	val := extractInfoLinkValue(doc, []string{"メーカー"})
	assert.Equal(t, "Test Maker", val)
}

// TestApplyLanguageToURLV3 tests applyLanguageToURL
func TestApplyLanguageToURLV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		language:    "en",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	t.Run("add en prefix", func(t *testing.T) {
		result := scraper.applyLanguageToURL("https://www.javbus.com/IPX-123")
		assert.Contains(t, result, "/en/")
	})

	t.Run("replace existing language prefix", func(t *testing.T) {
		result := scraper.applyLanguageToURL("https://www.javbus.com/ja/IPX-123")
		assert.Contains(t, result, "/en/")
		assert.NotContains(t, result, "/ja/")
	})
}

// TestIdsMatchV3 tests idsMatch
func TestIdsMatchV3(t *testing.T) {
	// idsMatch normalizes the candidate and compares against a pre-normalized target
	assert.True(t, idsMatch("IPX-123", "ipx123"))
	assert.True(t, idsMatch("IPX123", "ipx123"))
	assert.False(t, idsMatch("", "ipx123"))
	assert.False(t, idsMatch("IPX-123", ""))
}

// TestIsLikelyImageURLV3 tests isLikelyImageURL
func TestIsLikelyImageURLV3(t *testing.T) {
	assert.True(t, isLikelyImageURL("https://example.com/image.jpg"))
	assert.True(t, isLikelyImageURL("https://example.com/image.png"))
	assert.True(t, isLikelyImageURL("https://example.com/image.webp"))
	assert.False(t, isLikelyImageURL("https://example.com/page.html"))
	assert.False(t, isLikelyImageURL(""))
	assert.False(t, isLikelyImageURL("not-a-url"))
}

// helper to parse HTML into a goquery document
func parseHTMLDoc(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	return doc
}

// TestParseDetailPageV3_Success tests parseDetailPage with realistic HTML
func TestParseDetailPageV3_Success(t *testing.T) {
	html := `
<!DOCTYPE html>
<html>
<head><title>IPX-789 Test Movie - JavBus</title></head>
<body>
<div class="container">
<h3>IPX-789 Test Movie Title</h3>
<div id="info">
<p><span class="header">品番:</span> IPX-789</p>
<p><span class="header">発売日:</span> 2024-03-15</p>
<p><span class="header">収録時間:</span> 120分鐘</p>
<p><span class="header">監督:</span> <a>Director Name</a></p>
<p><span class="header">メーカー:</span> <a>Studio Name</a></p>
<p><span class="header">レーベル:</span> <a>Label Name</a></p>
<p><span class="header">シリーズ:</span> <a>Series Name</a></p>
<p><span class="header">ジャンル：</span> <a href="/genre/1">Drama</a> <a href="/genre/2">Romance</a></p>
<p><span class="header">出演者：</span> <a href="/star/1/">Actress A</a> <a href="/star/2/">Actress B</a></p>
</div>
<a class="bigImage" href="https://pics.dmm.co.jp/digital/video/ipx00789/ipx00789pl.jpg"><img src="https://pics.dmm.co.jp/digital/video/ipx00789/ipx00789ps.jpg"></a>
</div>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	client := resty.New()
	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.parseDetailPage(doc, "https://www.javbus.com/IPX-789", "IPX-789")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-789", result.ID)
	assert.Equal(t, "javbus", result.Source)
	assert.Equal(t, "Director Name", result.Director)
	assert.Equal(t, "Studio Name", result.Maker)
	assert.Equal(t, "Label Name", result.Label)
	assert.Equal(t, "Series Name", result.Series)
	assert.Equal(t, 120, result.Runtime)
	assert.Contains(t, result.Genres, "Drama")
}

// TestExtractActressesV3 tests extractActresses
func TestExtractActressesV3(t *testing.T) {
	html := `
<html><body>
<div id="star-div"><a href="/star/1/"><img title="Actress A" src="thumb1.jpg"></a></div>
<div id="info"><a href="/star/2/">Actress B</a></div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	actresses := extractActresses(doc)
	assert.GreaterOrEqual(t, len(actresses), 1)
}

// TestIsInvalidActressNameV3 tests isInvalidActressName
func TestIsInvalidActressNameV3(t *testing.T) {
	assert.True(t, isInvalidActressName(""))
	assert.True(t, isInvalidActressName("出演者"))
	assert.True(t, isInvalidActressName("<script>"))
	assert.False(t, isInvalidActressName("Valid Name"))
}

// TestExtractGenresV3 tests extractGenres
func TestExtractGenresV3(t *testing.T) {
	html := `
<html><body>
<div id="genre-toggle"><a href="/genre/1">Drama</a><a href="/genre/2">Romance</a></div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	genres := extractGenres(doc)
	assert.Contains(t, genres, "Drama")
	assert.Contains(t, genres, "Romance")
}

// TestExtractCoverURLV3 tests extractCoverURL
func TestExtractCoverURLV3(t *testing.T) {
	html := `
<html><body>
<a class="bigImage" href="https://pics.dmm.co.jp/digital/video/ipx00123/ipx00123pl.jpg"><img src="https://pics.dmm.co.jp/digital/video/ipx00123/ipx00123ps.jpg"></a>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	cover := extractCoverURL(doc, "https://www.javbus.com/IPX-123")
	assert.NotEmpty(t, cover)
}

// TestExtractDescriptionV3 tests extractDescription
func TestExtractDescriptionV3(t *testing.T) {
	html := `<html><head><meta name="description" content="Test movie description"></head><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	desc := extractDescription(doc)
	assert.Equal(t, "Test movie description", desc)
}

// TestNormalizeLanguageV3 tests normalizeLanguage
func TestNormalizeLanguageV3(t *testing.T) {
	assert.Equal(t, "en", normalizeLanguage("en"))
	assert.Equal(t, "ja", normalizeLanguage("ja"))
	assert.Equal(t, "zh", normalizeLanguage("zh"))
	assert.Equal(t, "zh", normalizeLanguage("cn"))
	assert.Equal(t, "zh", normalizeLanguage("tw"))
	assert.Equal(t, "zh", normalizeLanguage("fr")) // default
	assert.Equal(t, "zh", normalizeLanguage(""))   // default
}
