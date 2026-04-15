package dlgetchu

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"golang.org/x/net/html/charset"
)

const defaultBaseURL = "http://dl.getchu.com"

var (
	itemIDRegex      = regexp.MustCompile(`(?i)(?:作品ID[:：]\s*|id=|/item)(\d{4,})`)
	descriptionRegex = regexp.MustCompile(`(?is)作品内容</td>(.*?)</td>`)
	releaseDateRegex = regexp.MustCompile(`(\d{4}/\d{2}/\d{2})`)
	runtimeRegex     = regexp.MustCompile(`([０-９\s]{1,3})分`)
	makerRegex       = regexp.MustCompile(`(?is)dojin_circle_detail\.php\?id=\d+[^>]*>([^<]+)</a>`) //nolint:gocritic
	genreRegex       = regexp.MustCompile(`(?is)genre_id=\d+[^>]*>([^<]+)</a>`)                     //nolint:gocritic
	coverRegex       = regexp.MustCompile(`(?i)(/data/item_img/[^\"']+/\d+top\.jpg)`)
	screenshotRegex  = regexp.MustCompile(`(?i)"(/data/item_img/[^\"']+\.(?:jpg|jpeg|webp))"\s+class="highslide"`)
	detailLinkRegex  = regexp.MustCompile(`https?://dl\.getchu\.com/i/item\d+`)
	detailPathLinkRe = regexp.MustCompile(`(?i)/i/item\d+`)
)

// Scraper implements the DLgetchu scraper.
type Scraper struct {
	client        *resty.Client
	enabled       bool
	baseURL       string
	proxyOverride *config.ProxyConfig
	downloadProxy *config.ProxyConfig
	rateLimiter   *ratelimit.Limiter
	settings      config.ScraperSettings // stores the full settings for Config() method
}

// New creates a new DLgetchu scraper.
func New(settings config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) *Scraper {
	// Build ScraperConfig for HTTP client (HTTP-01 pattern)
	configForHTTP := &config.ScraperSettings{
		Enabled:       settings.Enabled,
		Timeout:       settings.Timeout,
		RateLimit:     settings.RateLimit,
		RetryCount:    settings.RetryCount,
		UserAgent:     settings.UserAgent,
		Proxy:         settings.Proxy,
		DownloadProxy: settings.DownloadProxy,
	}

	// Handle nil globalProxy to avoid dereference panic
	globalProxyVal := config.ProxyConfig{}
	if globalProxy != nil {
		globalProxyVal = *globalProxy
	}
	proxyEnabled := globalProxyVal.Enabled
	if settings.Proxy != nil && settings.Proxy.Enabled {
		proxyEnabled = true
	}
	proxyCfg := config.ResolveScraperProxy(globalProxyVal, settings.Proxy)

	client, err := NewHTTPClient(configForHTTP, globalProxy, globalFlareSolverr)
	usingProxy := err == nil && proxyEnabled && strings.TrimSpace(proxyCfg.URL) != ""
	if err != nil {
		logging.Errorf("DLgetchu: Failed to create HTTP client with proxy: %v, using explicit no-proxy fallback", err)
		client = httpclient.NewRestyClientNoProxy(time.Duration(settings.Timeout)*time.Second, settings.RetryCount)
	}

	base := strings.TrimSpace(settings.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")

	s := &Scraper{
		client:        client,
		enabled:       settings.Enabled,
		baseURL:       base,
		proxyOverride: settings.Proxy,
		downloadProxy: settings.DownloadProxy,
		rateLimiter:   ratelimit.NewLimiter(time.Duration(settings.RateLimit) * time.Millisecond),
		settings:      settings,
	}

	if usingProxy {
		logging.Infof("DLgetchu: Using proxy %s", httpclient.SanitizeProxyURL(proxyCfg.URL))
	}

	return s
}

// Name returns scraper identifier.
func (s *Scraper) Name() string { return "dlgetchu" }

// IsEnabled returns whether scraper is enabled.
func (s *Scraper) IsEnabled() bool { return s.enabled }

// Config returns the scraper's configuration
func (s *Scraper) Config() *config.ScraperSettings {
	return s.settings.DeepCopy()
}

// Close cleans up resources held by the scraper
func (s *Scraper) Close() error {
	return nil
}

// ValidateConfig validates the scraper configuration.
// Returns error if config is invalid, nil if valid.
func (s *Scraper) ValidateConfig(cfg *config.ScraperSettings) error {
	if cfg == nil {
		return fmt.Errorf("dlgetchu: config is nil")
	}
	if !cfg.Enabled {
		return nil // Disabled is valid
	}
	if cfg.RateLimit < 0 {
		return fmt.Errorf("dlgetchu: rate_limit must be non-negative, got %d", cfg.RateLimit)
	}
	if cfg.RetryCount < 0 {
		return fmt.Errorf("dlgetchu: retry_count must be non-negative, got %d", cfg.RetryCount)
	}
	if cfg.Timeout < 0 {
		return fmt.Errorf("dlgetchu: timeout must be non-negative, got %d", cfg.Timeout)
	}
	return nil
}

// ResolveDownloadProxyForHost declares DLgetchu-owned media hosts for downloader proxy routing.
func (s *Scraper) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil, nil, false
	}
	if host == "dl.getchu.com" || strings.HasSuffix(host, ".dl.getchu.com") ||
		host == "getchu.com" || strings.HasSuffix(host, ".getchu.com") {
		return s.downloadProxy, s.proxyOverride, true
	}
	return nil, nil, false
}

// GetURL resolves detail URL for an ID.
func (s *Scraper) CanHandleURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "dl.getchu.com" || strings.HasSuffix(host, ".dl.getchu.com") ||
		host == "getchu.com" || strings.HasSuffix(host, ".getchu.com")
}

func (s *Scraper) ExtractIDFromURL(urlStr string) (string, error) {
	if m := itemIDRegex.FindStringSubmatch(urlStr); len(m) > 1 {
		return strings.TrimSpace(m[1]), nil
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}
	if strings.Contains(u.Path, "/i/item") {
		path := strings.TrimPrefix(u.Path, "/i/item")
		path = strings.TrimSuffix(path, "/")
		if path != "" {
			return path, nil
		}
	}
	return "", fmt.Errorf("failed to extract ID from DLgetchu URL")
}

func (s *Scraper) ScrapeURL(ctx context.Context, rawURL string) (*models.ScraperResult, error) {
	if !s.CanHandleURL(rawURL) {
		return nil, models.NewScraperNotFoundError("DLgetchu", "URL not handled by DLgetchu scraper")
	}

	id, err := s.ExtractIDFromURL(rawURL)
	if err != nil {
		id = ""
	}

	html, status, err := s.fetchPageCtx(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch DLgetchu detail page: %w", err)
	}
	if status == 404 {
		return nil, models.NewScraperNotFoundError("DLgetchu", "page not found")
	}
	if status == 429 {
		return nil, models.NewScraperStatusError("DLgetchu", 429, "rate limited")
	}
	if status == 403 || status == 451 {
		return nil, models.NewScraperStatusError("DLgetchu", status, "access blocked")
	}
	if status != 200 {
		return nil, models.NewScraperStatusError("DLgetchu", status, fmt.Sprintf("DLgetchu returned status code %d", status))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse DLgetchu detail page: %w", err)
	}

	return parseDetailPage(doc, html, rawURL, id), nil
}

func (s *Scraper) GetURL(id string) (string, error) {
	return s.getURLWithContext(context.Background(), id)
}

func (s *Scraper) getURLWithContext(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("movie ID cannot be empty")
	}
	if isHTTPURL(id) {
		return id, nil
	}

	if numericID := extractNumericID(id); numericID != "" {
		candidate := fmt.Sprintf("%s/i/item%s", s.baseURL, numericID)
		_, status, err := s.fetchPageCtx(ctx, candidate)
		if err == nil && status == 200 {
			return candidate, nil
		}
	}

	for _, searchURL := range []string{
		fmt.Sprintf("%s/?search_keyword=%s", s.baseURL, url.QueryEscape(id)),
		fmt.Sprintf("%s/gcosin/?search_keyword=%s", s.baseURL, url.QueryEscape(id)),
		fmt.Sprintf("%s/gcosl/?search_keyword=%s", s.baseURL, url.QueryEscape(id)),
	} {
		html, status, err := s.fetchPageCtx(ctx, searchURL)
		if err != nil || status != 200 {
			continue
		}

		if link := findFirstDetailLink(html, s.baseURL); link != "" {
			return link, nil
		}
	}

	return "", models.NewScraperNotFoundError(
		"DLgetchu",
		fmt.Sprintf("movie %s not found on DLgetchu (this scraper may require a direct DLgetchu URL)", id),
	)
}

// Search scrapes metadata from DLgetchu.
func (s *Scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("DLgetchu scraper is disabled")
	}

	detailURL, err := s.getURLWithContext(ctx, id)
	if err != nil {
		return nil, err
	}

	html, status, err := s.fetchPageCtx(ctx, detailURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch DLgetchu detail page: %w", err)
	}
	if status != 200 {
		return nil, models.NewScraperStatusError("DLgetchu", status, fmt.Sprintf("DLgetchu returned status code %d", status))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse DLgetchu detail page: %w", err)
	}

	return parseDetailPage(doc, html, detailURL, id), nil
}

func parseDetailPage(doc *goquery.Document, html, sourceURL, fallbackID string) *models.ScraperResult {
	result := &models.ScraperResult{
		Source:    "dlgetchu",
		SourceURL: sourceURL,
		Language:  "ja",
	}

	result.ID = extractNumericID(html)
	if result.ID == "" {
		result.ID = extractNumericID(sourceURL)
	}
	if result.ID == "" {
		result.ID = extractNumericID(fallbackID)
	}
	if result.ID == "" {
		result.ID = strings.TrimSpace(fallbackID)
	}
	result.ContentID = result.ID

	title := scraperutil.CleanString(doc.Find("meta[property='og:title']").AttrOr("content", ""))
	if title == "" {
		title = scraperutil.CleanString(doc.Find("title").First().Text())
	}
	result.Title = title
	result.OriginalTitle = title

	if m := releaseDateRegex.FindStringSubmatch(html); len(m) > 1 {
		raw := strings.ReplaceAll(m[1], "/", "-")
		if t, err := time.Parse("2006-01-02", raw); err == nil {
			result.ReleaseDate = &t
		}
	}

	if m := runtimeRegex.FindStringSubmatch(html); len(m) > 1 {
		raw := normalizeFullWidthDigits(m[1])
		if v, err := strconv.Atoi(strings.TrimSpace(strings.ReplaceAll(raw, " ", ""))); err == nil {
			result.Runtime = v
		}
	}

	if m := descriptionRegex.FindStringSubmatch(html); len(m) > 1 {
		result.Description = scraperutil.CleanString(stripTags(m[1]))
	}
	if result.Description == "" {
		result.Description = scraperutil.CleanString(doc.Find("meta[name='description']").AttrOr("content", ""))
	}

	if m := makerRegex.FindStringSubmatch(html); len(m) > 1 {
		result.Maker = scraperutil.CleanString(stripTags(m[1]))
	}

	result.Genres = extractGenres(html)

	if m := coverRegex.FindStringSubmatch(html); len(m) > 1 {
		result.CoverURL = scraperutil.ResolveURL(sourceURL, m[1])
	}
	result.PosterURL = result.CoverURL
	result.ScreenshotURL = extractScreenshots(html, sourceURL)
	result.ShouldCropPoster = true

	if result.Title == "" {
		result.Title = result.ID
		result.OriginalTitle = result.ID
	}

	return result
}

func extractGenres(html string) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, m := range genreRegex.FindAllStringSubmatch(html, -1) {
		if len(m) <= 1 {
			continue
		}
		g := scraperutil.CleanString(stripTags(m[1]))
		if g == "" || seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
	}
	return out
}

func extractScreenshots(html, base string) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, m := range screenshotRegex.FindAllStringSubmatch(html, -1) {
		if len(m) <= 1 {
			continue
		}
		u := scraperutil.ResolveURL(base, m[1])
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}

func findFirstDetailLink(html, base string) string {
	if m := detailLinkRegex.FindStringSubmatch(html); len(m) > 0 {
		return m[0]
	}
	if m := detailPathLinkRe.FindStringSubmatch(html); len(m) > 0 {
		return scraperutil.ResolveURL(base, m[0])
	}
	return ""
}

func extractNumericID(v string) string {
	if m := itemIDRegex.FindStringSubmatch(v); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func normalizeFullWidthDigits(v string) string {
	replacer := strings.NewReplacer(
		"０", "0",
		"１", "1",
		"２", "2",
		"３", "3",
		"４", "4",
		"５", "5",
		"６", "6",
		"７", "7",
		"８", "8",
		"９", "9",
	)
	return replacer.Replace(v)
}

func (s *Scraper) fetchPageCtx(ctx context.Context, targetURL string) (string, int, error) {
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return "", 0, err
	}

	resp, err := s.client.R().SetContext(ctx).Get(targetURL)
	if err != nil {
		return "", 0, err
	}

	decoded, err := decodeBody(resp)
	if err != nil {
		html := resp.String()
		if resp.StatusCode() == 200 && models.IsCloudflareChallengePage(html) {
			return "", resp.StatusCode(), models.NewScraperChallengeError(
				"DLgetchu",
				"DLgetchu returned a Cloudflare challenge page (request blocked; adjust proxy/IP)",
			)
		}
		return html, resp.StatusCode(), nil
	}
	if resp.StatusCode() == 200 && models.IsCloudflareChallengePage(decoded) {
		return "", resp.StatusCode(), models.NewScraperChallengeError(
			"DLgetchu",
			"DLgetchu returned a Cloudflare challenge page (request blocked; adjust proxy/IP)",
		)
	}
	return decoded, resp.StatusCode(), nil
}

func decodeBody(resp *resty.Response) (string, error) {
	body := resp.Body()
	if len(body) == 0 {
		return "", nil
	}
	contentType := resp.Header().Get("Content-Type")
	enc, _, _ := charset.DetermineEncoding(body, contentType)
	decoded, err := enc.NewDecoder().Bytes(body)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func stripTags(v string) string {
	re := regexp.MustCompile(`(?s)<[^>]*>`) //nolint:gocritic
	return re.ReplaceAllString(v, "")
}

func isHTTPURL(v string) bool {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
