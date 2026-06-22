package javbus

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ScrapeURL: 404 status returns not-found error ---

func TestScrapeURL_Miss_Status404(t *testing.T) {
	rt := &javbusRoundTripper{
		statuses: map[string]int{
			"https://www.javbus.com/NF-001": http.StatusNotFound,
		},
		responses: map[string]string{
			"https://www.javbus.com/NF-001": "not found",
		},
	}

	client := resty.New()
	client.SetTransport(rt)

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.javbus.com/NF-001")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// --- ScrapeURL: 429 status returns rate-limited error ---

func TestScrapeURL_Miss_Status429(t *testing.T) {
	rt := &javbusRoundTripper{
		statuses: map[string]int{
			"https://www.javbus.com/RL-001": http.StatusTooManyRequests,
		},
		responses: map[string]string{
			"https://www.javbus.com/RL-001": "rate limited",
		},
	}

	client := resty.New()
	client.SetTransport(rt)

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.javbus.com/RL-001")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusTooManyRequests, scraperErr.StatusCode)
}

// --- ScrapeURL: 403 status returns access-blocked error ---

func TestScrapeURL_Miss_Status403(t *testing.T) {
	rt := &javbusRoundTripper{
		statuses: map[string]int{
			"https://www.javbus.com/BLK-001": http.StatusForbidden,
		},
		responses: map[string]string{
			"https://www.javbus.com/BLK-001": "forbidden",
		},
	}

	client := resty.New()
	client.SetTransport(rt)

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.javbus.com/BLK-001")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusForbidden, scraperErr.StatusCode)
}

// --- ScrapeURL: 451 status returns access-blocked error ---

func TestScrapeURL_Miss_Status451(t *testing.T) {
	rt := &javbusRoundTripper{
		statuses: map[string]int{
			"https://www.javbus.com/LEG-001": http.StatusUnavailableForLegalReasons,
		},
		responses: map[string]string{
			"https://www.javbus.com/LEG-001": "unavailable for legal reasons",
		},
	}

	client := resty.New()
	client.SetTransport(rt)

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.javbus.com/LEG-001")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnavailableForLegalReasons, scraperErr.StatusCode)
}

// --- ScrapeURL: generic non-200 status ---

func TestScrapeURL_Miss_StatusGeneric(t *testing.T) {
	rt := &javbusRoundTripper{
		statuses: map[string]int{
			"https://www.javbus.com/GEN-001": http.StatusBadGateway,
		},
		responses: map[string]string{
			"https://www.javbus.com/GEN-001": "bad gateway",
		},
	}

	client := resty.New()
	client.SetTransport(rt)

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.javbus.com/GEN-001")
	require.Error(t, err)
}

// --- ScrapeURL: fetchPageCtx error propagates ---

func TestScrapeURL_Miss_FetchError(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := scraper.ScrapeURL(ctx, "https://www.javbus.com/CNC-001")
	require.Error(t, err)
}

// --- Search: disabled scraper ---

func TestSearch_Miss_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// --- fetchPageCtx: challenge page detection ---

func TestFetchPageCtx_Miss_DriverVerifyChallenge(t *testing.T) {
	challengeHTML := `<html><body>/doc/driver-verify?referer=/IPX-001</body></html>`

	rt := &statusRoundTripper{
		status:    http.StatusFound,
		body:      challengeHTML,
		finalPath: "/doc/driver-verify",
	}

	client := resty.New()
	client.SetTransport(rt)

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, _, err := scraper.fetchPageCtx(context.Background(), "https://www.javbus.com/CHL-001")
	require.Error(t, err)
}

// --- fetchPageCtx: Cloudflare challenge page ---

func TestFetchPageCtx_Miss_CloudflareChallenge(t *testing.T) {
	cfHTML := `<html><body><title>Just a moment...</title><p>Cloudflare attention required. Checking your browser before accessing. DDoS protection by Cloudflare. Ray ID: abc123. cf-ray: xyz. /cdn-cgi/ path.</p></body></html>`

	rt := &javbusRoundTripper{
		responses: map[string]string{
			"https://www.javbus.com/CF-001": cfHTML,
		},
	}

	client := resty.New()
	client.SetTransport(rt)

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, _, err := scraper.fetchPageCtx(context.Background(), "https://www.javbus.com/CF-001")
	require.Error(t, err)
}

// --- fetchPageCtx: JavBus driver-verify in body ---

func TestFetchPageCtx_Miss_JavBusChallengeInBody(t *testing.T) {
	challengeHTML := `<html><body><p>age verification javbus</p></body></html>`

	rt := &javbusRoundTripper{
		responses: map[string]string{
			"https://www.javbus.com/JVC-001": challengeHTML,
		},
	}

	client := resty.New()
	client.SetTransport(rt)

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, _, err := scraper.fetchPageCtx(context.Background(), "https://www.javbus.com/JVC-001")
	require.Error(t, err)
}

// --- CanHandleURL: edge cases ---

func TestCanHandleURL_Miss_EdgeCases(t *testing.T) {
	scraper := &scraper{baseURL: "https://www.javbus.com"}

	assert.True(t, scraper.CanHandleURL("https://www.javbus.com/IPX-535"))
	assert.True(t, scraper.CanHandleURL("https://www.javbus.org/IPX-535"))
	assert.True(t, scraper.CanHandleURL("https://sub.javbus.com/IPX-535"))
	assert.False(t, scraper.CanHandleURL("https://example.com/IPX-535"))
	assert.False(t, scraper.CanHandleURL("://invalid"))
}

// --- ExtractIDFromURL: various URL formats ---

func TestExtractIDFromURL_Miss_VariousFormats(t *testing.T) {
	scraper := &scraper{baseURL: "https://www.javbus.com"}

	// Valid detail page URL
	id, err := scraper.ExtractIDFromURL("https://www.javbus.com/IPX-535")
	require.NoError(t, err)
	assert.Equal(t, "IPX-535", id)

	// URL with language prefix
	id, err = scraper.ExtractIDFromURL("https://www.javbus.com/en/IPX-535")
	require.NoError(t, err)
	assert.Equal(t, "IPX-535", id)

	// URL with ja prefix
	id, err = scraper.ExtractIDFromURL("https://www.javbus.com/ja/IPX-535")
	require.NoError(t, err)
	assert.Equal(t, "IPX-535", id)

	// URL with zh prefix
	id, err = scraper.ExtractIDFromURL("https://www.javbus.com/zh/ABC-123")
	require.NoError(t, err)
	assert.Equal(t, "ABC-123", id)

	// Invalid URL - no path
	_, err = scraper.ExtractIDFromURL("https://www.javbus.com/")
	require.Error(t, err)

	// Invalid URL - multiple segments
	_, err = scraper.ExtractIDFromURL("https://www.javbus.com/a/b/c")
	require.Error(t, err)

	// Invalid URL - unparseable
	_, err = scraper.ExtractIDFromURL("://invalid")
	require.Error(t, err)
}

// --- ResolveDownloadProxyForHost: various hosts ---

func TestResolveDownloadProxyForHost_Miss(t *testing.T) {
	scraper := &scraper{
		settings: models.ScraperSettings{
			DownloadProxy: &models.ProxyConfig{Enabled: true, Profile: "dl-proxy"},
			Proxy:         &models.ProxyConfig{Enabled: true, Profile: "scraper-proxy"},
		},
	}

	// JavBus hosts
	dl, scr, ok := scraper.ResolveDownloadProxyForHost("javbus.com")
	assert.True(t, ok)
	assert.NotNil(t, dl)
	assert.NotNil(t, scr)

	dl, scr, ok = scraper.ResolveDownloadProxyForHost("www.javbus.com")
	assert.True(t, ok)

	dl, scr, ok = scraper.ResolveDownloadProxyForHost("javbus.org")
	assert.True(t, ok)

	// Unknown host
	_, _, ok = scraper.ResolveDownloadProxyForHost("example.com")
	assert.False(t, ok)

	// Empty host
	_, _, ok = scraper.ResolveDownloadProxyForHost("")
	assert.False(t, ok)
}

// --- applyLanguageToURL: various URL formats ---

func TestApplyLanguageToURL_Miss(t *testing.T) {
	scraper := &scraper{baseURL: "https://www.javbus.com", language: "en"}
	assert.Contains(t, scraper.applyLanguageToURL("https://www.javbus.com/IPX-535"), "/en/")

	scraper.language = "ja"
	assert.Contains(t, scraper.applyLanguageToURL("https://www.javbus.com/IPX-535"), "/ja/")

	// URL already has language prefix
	scraper.language = "en"
	result := scraper.applyLanguageToURL("https://www.javbus.com/ja/IPX-535")
	assert.Contains(t, result, "/en/")

	// Invalid URL - should return as-is
	result = scraper.applyLanguageToURL("://invalid")
	assert.Equal(t, "://invalid", result)
}

// --- isLikelyImageURL: edge cases ---

func TestIsLikelyImageURL_Miss_EdgeCases(t *testing.T) {
	assert.False(t, isLikelyImageURL(""))
	assert.False(t, isLikelyImageURL("://invalid"))
	assert.True(t, isLikelyImageURL("https://example.com/image.jpg"))
	assert.True(t, isLikelyImageURL("https://example.com/image.JPEG"))
	assert.True(t, isLikelyImageURL("https://example.com/image.png"))
	assert.True(t, isLikelyImageURL("https://example.com/image.webp"))
	assert.True(t, isLikelyImageURL("https://example.com/image.gif"))
	assert.True(t, isLikelyImageURL("https://example.com/image.bmp"))
	assert.True(t, isLikelyImageURL("https://example.com/image.avif"))
	assert.False(t, isLikelyImageURL("https://example.com/image.mp4"))
	assert.False(t, isLikelyImageURL("https://example.com/image.html"))
}

// --- isJavbusChallengePage: edge cases ---

func TestIsJavbusChallengePage_Miss(t *testing.T) {
	assert.False(t, isJavbusChallengePage(""))
	assert.True(t, isJavbusChallengePage(`<html><body>/doc/driver-verify?referer=/test</body></html>`))
	assert.True(t, isJavbusChallengePage(`<html><body>age verification javbus</body></html>`))
	assert.True(t, isJavbusChallengePage(`<html><body>driver verification</body></html>`))
	assert.True(t, isJavbusChallengePage(`<html><body>driver-verify?referer=/test</body></html>`))
	assert.False(t, isJavbusChallengePage(`<html><body>normal page content</body></html>`))
}

// --- normalizeLanguage: edge cases ---

func TestNormalizeLanguage_Miss(t *testing.T) {
	assert.Equal(t, "en", normalizeLanguage("en"))
	assert.Equal(t, "en", normalizeLanguage("EN"))
	assert.Equal(t, "ja", normalizeLanguage("ja"))
	assert.Equal(t, "zh", normalizeLanguage("zh"))
	assert.Equal(t, "zh", normalizeLanguage("cn"))
	assert.Equal(t, "zh", normalizeLanguage("tw"))
	assert.Equal(t, "zh", normalizeLanguage(""))      // default
	assert.Equal(t, "zh", normalizeLanguage("other")) // default
}

// --- getURLCtx: rate limit wait cancelled ---

func TestGetURLCtx_Miss_RateLimitCancelled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := scraper.getURLCtx(ctx, "CNC-001")
	require.Error(t, err)
}

// --- findDetailURL: multiple candidates (no match) ---

func TestFindDetailURL_Miss_NoMatchMultiple(t *testing.T) {
	html := `
<html><body>
	<a class="movie-box" href="/ABC-001" title="ABC-001"><date>ABC-001</date></a>
	<a class="movie-box" href="/DEF-002" title="DEF-002"><date>DEF-002</date></a>
</body></html>`

	scraper := &scraper{baseURL: "https://www.javbus.com"}
	result := scraper.findDetailURL(html, "https://www.javbus.com", "XYZ-999")
	assert.Empty(t, result, "should not find a match when ID doesn't match any candidate")
}

// --- findDetailURL: single candidate fallback ---

func TestFindDetailURL_Miss_SingleFallback(t *testing.T) {
	html := `
<html><body>
	<a class="movie-box" href="/ONLY-001" title="ONLY-001"><date>ONLY-001</date></a>
</body></html>`

	scraper := &scraper{baseURL: "https://www.javbus.com"}
	result := scraper.findDetailURL(html, "https://www.javbus.com", "ONLY-001")
	assert.Contains(t, result, "/ONLY-001")
}

// --- findDetailURL: empty href ---

func TestFindDetailURL_Miss_EmptyHref(t *testing.T) {
	html := `
<html><body>
	<a class="movie-box" href="" title="EMPTY-001"><date>EMPTY-001</date></a>
</body></html>`

	scraper := &scraper{baseURL: "https://www.javbus.com"}
	result := scraper.findDetailURL(html, "https://www.javbus.com", "EMPTY-001")
	assert.Empty(t, result)
}

// --- extractCoverURL: fallback to img src ---

func TestExtractCoverURL_Miss_FallbackImgSrc(t *testing.T) {
	html := `
<html><body>
	<div id="cover"><img src="https://example.com/cover.jpg" /></div>
</body></html>`

	doc := docFromHTMLMissJavbus(t, html)
	url := extractCoverURL(doc, "https://www.javbus.com")
	assert.Equal(t, "https://example.com/cover.jpg", url)
}

// --- extractScreenshotURLs: fallback to data-src ---

func TestExtractScreenshotURLs_Miss_FallbackDataSrc(t *testing.T) {
	html := `
<html><body>
	<a class="sample-box" href="https://example.com/shot.jpg">
		<img data-src="https://example.com/shot-thumb.jpg" />
	</a>
</body></html>`

	doc := docFromHTMLMissJavbus(t, html)
	urls := extractScreenshotURLs(doc, "https://www.javbus.com")
	assert.NotEmpty(t, urls)
}

// --- Search: non-200 detail page status ---

func TestSearch_Miss_Non200DetailStatus(t *testing.T) {
	searchHTML := `
<html><body>
	<a class="movie-box" href="/STS-001" title="STS-001"><date>STS-001</date></a>
</body></html>`

	rt := &javbusRoundTripper{
		responses: map[string]string{
			"/search/": searchHTML,
			"/STS-001": "server error",
		},
		statuses: map[string]int{
			"/STS-001": http.StatusInternalServerError,
		},
	}

	client := resty.New()
	client.SetTransport(rt)

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.Search(context.Background(), "STS-001")
	require.Error(t, err)
}

// --- extractActresses: invalid actress name with HTML ---

func TestExtractActresses_Miss_InvalidName(t *testing.T) {
	html := `
<html><body>
	<div id="info">
		<a href="/star/1">出演者</a>
		<a href="/star/2"><img title="画像を拡大" /></a>
		<a href="/star/3"><img title="Valid Actress" /></a>
	</div>
</body></html>`

	doc := docFromHTMLMissJavbus(t, html)
	actresses := extractActresses(doc)
	// "出演者" should be filtered out, "画像を拡大" should be filtered
	hasInvalid := false
	for _, a := range actresses {
		if a.JapaneseName == "出演者" || a.FirstName == "出演者" {
			hasInvalid = true
		}
	}
	assert.False(t, hasInvalid, "invalid actress names should be filtered")
}

// --- Helper: statusRoundTripper returns redirect with a specific final path ---

type statusRoundTripper struct {
	status    int
	body      string
	finalPath string
}

func (rt *statusRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp := &http.Response{
		StatusCode: rt.status,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
		Body:       io.NopCloser(strings.NewReader(rt.body)),
		Request:    req,
	}
	// Simulate a redirect by setting the final URL path
	if rt.finalPath != "" {
		finalURL := *req.URL
		finalURL.Path = rt.finalPath
		resp.Request = req.Clone(req.Context())
		resp.Request.URL = &finalURL
	}
	return resp, nil
}

func docFromHTMLMissJavbus(t *testing.T, raw string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("goquery.NewDocumentFromReader() error = %v", err)
	}
	return doc
}
