package jav321

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

const (
	defaultBaseURL = "https://jp.jav321.com"
)

var (
	idRegex          = regexp.MustCompile(`([A-Za-z]+-?\d+[A-Za-z]?)`)
	runtimeRegex     = regexp.MustCompile(`(\d+)\s*(?:minutes|min|分)?`)
	releaseDateRegex = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})`)
	nonAlphaNum      = regexp.MustCompile(`[^a-z0-9]+`)
)

// Scraper implements the Jav321 scraper.
type Scraper struct {
	client        *resty.Client
	enabled       bool
	baseURL       string
	proxyOverride *config.ProxyConfig
	downloadProxy *config.ProxyConfig
	rateLimiter   *ratelimit.Limiter
	settings      config.ScraperSettings // stores the full settings for Config() method
}

// New creates a new Jav321 scraper.
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
		logging.Errorf("Jav321: Failed to create HTTP client with proxy: %v, using explicit no-proxy fallback", err)
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
		logging.Infof("Jav321: Using proxy %s", httpclient.SanitizeProxyURL(proxyCfg.URL))
	}

	return s
}

// Name returns the scraper identifier.
func (s *Scraper) Name() string { return "jav321" }

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
		return fmt.Errorf("jav321: config is nil")
	}
	if !cfg.Enabled {
		return nil // Disabled is valid
	}
	if cfg.RateLimit < 0 {
		return fmt.Errorf("jav321: rate_limit must be non-negative, got %d", cfg.RateLimit)
	}
	if cfg.RetryCount < 0 {
		return fmt.Errorf("jav321: retry_count must be non-negative, got %d", cfg.RetryCount)
	}
	if cfg.Timeout < 0 {
		return fmt.Errorf("jav321: timeout must be non-negative, got %d", cfg.Timeout)
	}
	return nil
}

// ResolveDownloadProxyForHost declares Jav321-owned media hosts for downloader proxy routing.
func (s *Scraper) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil, nil, false
	}
	if host == "jav321.com" || strings.HasSuffix(host, ".jav321.com") {
		return s.downloadProxy, s.proxyOverride, true
	}
	return nil, nil, false
}

// GetURL returns the detail page URL for an ID.
func (s *Scraper) GetURL(id string) (string, error) {
	return s.GetURLCtx(context.Background(), id)
}

// GetURLCtx returns the detail page URL for an ID with context support.
func (s *Scraper) GetURLCtx(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("movie ID cannot be empty")
	}
	if isHTTPURL(id) {
		return id, nil
	}

	searchURL := s.baseURL + "/search"
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return "", err
	}
	resp, err := s.client.R().SetContext(ctx).SetFormData(map[string]string{"sn": id}).Post(searchURL)
	if err != nil {
		return "", fmt.Errorf("failed to search Jav321: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 400 {
		return "", models.NewScraperStatusError(
			"Jav321",
			resp.StatusCode(),
			fmt.Sprintf("Jav321 search returned status code %d", resp.StatusCode()),
		)
	}

	if raw := resp.RawResponse; raw != nil && raw.Request != nil && raw.Request.URL != nil {
		finalURL := raw.Request.URL.String()
		if strings.Contains(finalURL, "/video/") {
			return finalURL, nil
		}
	}

	html := resp.String()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", fmt.Errorf("failed to parse Jav321 search page: %w", err)
	}

	target := normalizeID(id)
	var found string
	doc.Find("a[href*='/video/']").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href := strings.TrimSpace(a.AttrOr("href", ""))
		if href == "" {
			return true
		}
		text := scraperutil.CleanString(a.Parent().Text())
		cand := extractID(text)
		if cand == "" {
			cand = extractID(a.Text())
		}
		if cand != "" && normalizeID(cand) == target {
			found = scraperutil.ResolveURL(s.baseURL, href)
			return false
		}
		return true
	})

	if found != "" {
		return found, nil
	}

	candidates := make([]string, 0, 1)
	doc.Find("a[href*='/video/']").Each(func(_ int, a *goquery.Selection) {
		href := strings.TrimSpace(a.AttrOr("href", ""))
		if href != "" {
			candidates = append(candidates, scraperutil.ResolveURL(s.baseURL, href))
		}
	})
	if len(candidates) == 1 {
		return candidates[0], nil
	}

	return "", models.NewScraperNotFoundError("Jav321", fmt.Sprintf("movie %s not found on Jav321", id))
}

func (s *Scraper) CanHandleURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "jav321.com" || strings.HasSuffix(host, ".jav321.com")
}

func (s *Scraper) ExtractIDFromURL(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}
	path := strings.Trim(u.Path, "/")
	if strings.HasPrefix(path, "video/") {
		id := strings.TrimPrefix(path, "video/")
		id = strings.TrimSuffix(id, "/")
		if id != "" {
			return strings.ToUpper(id), nil
		}
	}
	return "", fmt.Errorf("failed to extract ID from URL")
}

func (s *Scraper) ScrapeURL(ctx context.Context, rawURL string) (*models.ScraperResult, error) {
	if !s.CanHandleURL(rawURL) {
		return nil, models.NewScraperNotFoundError("Jav321", "URL not handled by Jav321 scraper")
	}

	id, err := s.ExtractIDFromURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to extract ID from URL: %w", err)
	}

	html, status, err := s.fetchPageCtx(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Jav321 detail page: %w", err)
	}
	if status == 404 {
		return nil, models.NewScraperNotFoundError("Jav321", "page not found")
	}
	if status == 429 {
		return nil, models.NewScraperStatusError("Jav321", 429, "rate limited")
	}
	if status == 403 || status == 451 {
		return nil, models.NewScraperStatusError("Jav321", status, "access blocked")
	}
	if status != 200 {
		return nil, models.NewScraperStatusError("Jav321", status, fmt.Sprintf("Jav321 returned status code %d", status))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse Jav321 detail page: %w", err)
	}

	return parseDetailPage(doc, rawURL, id), nil
}

// Search searches and extracts metadata with context support.
func (s *Scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("Jav321 scraper is disabled")
	}

	detailURL, err := s.GetURLCtx(ctx, id)
	if err != nil {
		return nil, err
	}

	html, status, err := s.fetchPageCtx(ctx, detailURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Jav321 detail page: %w", err)
	}
	if status != 200 {
		return nil, models.NewScraperStatusError("Jav321", status, fmt.Sprintf("Jav321 returned status code %d", status))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse Jav321 detail page: %w", err)
	}

	return parseDetailPage(doc, detailURL, id), nil
}

func parseDetailPage(doc *goquery.Document, sourceURL, fallbackID string) *models.ScraperResult {
	result := &models.ScraperResult{
		Source:    "jav321",
		SourceURL: sourceURL,
		Language:  "ja",
	}

	if t := scraperutil.CleanString(doc.Find(".panel-heading h3").First().Text()); t != "" {
		result.Title = stripTrailingID(t)
		result.OriginalTitle = result.Title
	}
	if result.Title == "" {
		title := scraperutil.CleanString(doc.Find("meta[property='og:title']").AttrOr("content", ""))
		if title == "" {
			title = scraperutil.CleanString(doc.Find("title").First().Text())
		}
		result.Title = stripTrailingSiteName(title)
		result.OriginalTitle = result.Title
	}

	htmlText, _ := doc.Html()
	if id := extractLabeledValue(htmlText, []string{"品番", "識別碼", "识别码", "ID"}); id != "" {
		result.ID = strings.ToUpper(extractID(id))
	}
	if result.ID == "" {
		result.ID = strings.ToUpper(extractID(result.Title))
	}
	if result.ID == "" {
		result.ID = strings.ToUpper(strings.TrimSpace(fallbackID))
	}
	result.ContentID = result.ID

	if dateRaw := extractLabeledValue(htmlText, []string{"発売日", "配信開始日", "日期", "date"}); dateRaw != "" {
		if m := releaseDateRegex.FindStringSubmatch(dateRaw); len(m) > 1 {
			if t, err := time.Parse("2006-01-02", m[1]); err == nil {
				result.ReleaseDate = &t
			}
		}
	}

	if runtimeRaw := extractLabeledValue(htmlText, []string{"収録時間", "長度", "长度", "length", "runtime"}); runtimeRaw != "" {
		if m := runtimeRegex.FindStringSubmatch(runtimeRaw); len(m) > 1 {
			if v, err := strconv.Atoi(m[1]); err == nil {
				result.Runtime = v
			}
		}
	}

	result.Maker = extractLabeledAnchorValue(htmlText, []string{"メーカー", "片商", "maker", "studio"})
	result.Series = extractLabeledAnchorValue(htmlText, []string{"シリーズ", "系列", "series"})
	result.Description = extractDescription(doc, htmlText)

	result.Genres = extractGenres(doc)
	result.Actresses = extractActresses(htmlText)

	result.CoverURL = extractCoverURL(doc, sourceURL)
	result.PosterURL = result.CoverURL
	result.ScreenshotURL = extractScreenshotURLs(doc, sourceURL)
	result.ShouldCropPoster = true

	if result.Title == "" {
		result.Title = result.ID
		result.OriginalTitle = result.ID
	}

	return result
}

func extractLabeledValue(html string, labels []string) string {
	for _, label := range labels {
		re := regexp.MustCompile(`(?is)<b>\s*` + regexp.QuoteMeta(label) + `\s*</b>\s*:\s*(.*?)<br`) //nolint:gocritic
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return scraperutil.CleanString(stripTags(m[1]))
		}
	}
	return ""
}

func extractLabeledAnchorValue(html string, labels []string) string {
	for _, label := range labels {
		re := regexp.MustCompile(`(?is)<b>\s*` + regexp.QuoteMeta(label) + `\s*</b>\s*:\s*.*?<a[^>]*>(.*?)</a>`) //nolint:gocritic
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return scraperutil.CleanString(stripTags(m[1]))
		}
	}
	return ""
}

func extractDescription(doc *goquery.Document, html string) string {
	candidates := []string{
		scraperutil.CleanString(doc.Find("meta[name='description']").AttrOr("content", "")),
		scraperutil.CleanString(doc.Find("meta[property='og:description']").AttrOr("content", "")),
		extractLabeledValue(html, []string{"説明", "介紹", "介绍", "内容", "description"}),
	}

	for _, candidate := range candidates {
		if isUsableDescription(candidate) {
			return candidate
		}
	}

	return ""
}

func isUsableDescription(v string) bool {
	v = scraperutil.CleanString(v)
	if v == "" {
		return false
	}
	if len(v) < 20 {
		return false
	}

	lower := strings.ToLower(v)
	badMarkers := []string{
		"adsbyjuicy",
		"adzone",
		"juicyads",
		"jads.js",
		"adxadserv",
		"window.ads",
		"videojs(",
		"$(document",
		"function(",
		"push({'adzone'",
	}
	for _, marker := range badMarkers {
		if strings.Contains(lower, marker) {
			return false
		}
	}

	return true
}

func extractGenres(doc *goquery.Document) []string {
	seen := map[string]bool{}
	genres := make([]string, 0)
	doc.Find("a[href*='/genre/']").Each(func(_ int, a *goquery.Selection) {
		v := scraperutil.CleanString(a.Text())
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		genres = append(genres, v)
	})
	return genres
}

func extractActresses(html string) []models.ActressInfo {
	seen := map[string]bool{}
	out := make([]models.ActressInfo, 0)

	names := extractLabeledAnchorValues(html, []string{"出演者", "女優", "女优", "演員", "演员", "actress", "actor"})
	if len(names) == 0 {
		if raw := extractLabeledValue(html, []string{"出演者", "女優", "女优", "演員", "演员", "actress", "actor"}); raw != "" {
			names = splitValues(raw)
		}
	}

	for _, rawName := range names {
		name := scraperutil.CleanString(rawName)
		if name == "" || seen[name] || len(name) > 80 {
			continue
		}
		seen[name] = true

		info := models.ActressInfo{}
		if hasJapanese(name) {
			info.JapaneseName = strings.ReplaceAll(name, "（", "(")
		} else {
			parts := strings.Fields(name)
			switch len(parts) {
			case 0:
			case 1:
				info.FirstName = parts[0]
			default:
				info.FirstName = parts[0]
				info.LastName = strings.Join(parts[1:], " ")
			}
		}
		out = append(out, info)
	}

	return out
}

func extractLabeledAnchorValues(html string, labels []string) []string {
	seen := map[string]bool{}
	values := make([]string, 0)

	for _, label := range labels {
		re := regexp.MustCompile(`(?is)<b>\s*` + regexp.QuoteMeta(label) + `\s*</b>\s*:\s*(.*?)<br`) //nolint:gocritic
		m := re.FindStringSubmatch(html)
		if len(m) <= 1 {
			continue
		}

		section := m[1]
		linkRe := regexp.MustCompile(`(?is)<a[^>]*>(.*?)</a>`) //nolint:gocritic
		matches := linkRe.FindAllStringSubmatch(section, -1)
		for _, match := range matches {
			if len(match) <= 1 {
				continue
			}
			v := scraperutil.CleanString(stripTags(match[1]))
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			values = append(values, v)
		}

		if len(values) == 0 {
			for _, v := range splitValues(stripTags(section)) {
				v = scraperutil.CleanString(v)
				if v == "" || seen[v] {
					continue
				}
				seen[v] = true
				values = append(values, v)
			}
		}
	}

	return values
}

func splitValues(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', '、', '/', '|', '・', '&':
			return true
		default:
			return unicode.IsSpace(r)
		}
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = scraperutil.CleanString(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func extractCoverURL(doc *goquery.Document, base string) string {
	selectors := []string{
		"a[href*='/snapshot/'] img[src]",
		"meta[property='og:image']",
	}
	for _, sel := range selectors {
		node := doc.Find(sel).First()
		if node.Length() == 0 {
			continue
		}
		attr := "src"
		if strings.HasPrefix(sel, "meta") {
			attr = "content"
		}
		raw := scraperutil.CleanString(node.AttrOr(attr, ""))
		if raw != "" {
			return scraperutil.ResolveURL(base, raw)
		}
	}
	return ""
}

func extractScreenshotURLs(doc *goquery.Document, base string) []string {
	seen := map[string]bool{}
	urls := make([]string, 0)
	add := func(raw string) {
		raw = scraperutil.CleanString(raw)
		if raw == "" {
			return
		}
		u := scraperutil.ResolveURL(base, raw)
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		urls = append(urls, u)
	}

	doc.Find("a[href*='/snapshot/'] img[src]").Each(func(_ int, img *goquery.Selection) {
		add(img.AttrOr("src", ""))
	})

	if len(urls) == 0 {
		doc.Find("a[href*='/snapshot/']").Each(func(_ int, a *goquery.Selection) {
			add(a.AttrOr("href", ""))
		})
	}

	return urls
}

func (s *Scraper) fetchPageCtx(ctx context.Context, targetURL string) (string, int, error) {
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return "", 0, err
	}

	resp, err := s.client.R().SetContext(ctx).Get(targetURL)
	if err != nil {
		return "", 0, err
	}
	html := resp.String()
	if resp.StatusCode() == 200 && models.IsCloudflareChallengePage(html) {
		return "", resp.StatusCode(), models.NewScraperChallengeError(
			"Jav321",
			"Jav321 returned a Cloudflare challenge page (request blocked; adjust proxy/IP)",
		)
	}
	return html, resp.StatusCode(), nil
}

func normalizeID(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	return nonAlphaNum.ReplaceAllString(v, "")
}

func extractID(v string) string {
	v = strings.TrimSpace(v)
	if m := idRegex.FindStringSubmatch(v); len(m) > 1 {
		return strings.ToUpper(strings.ReplaceAll(m[1], "_", "-"))
	}
	return ""
}

func stripTrailingID(title string) string {
	title = scraperutil.CleanString(title)
	title = idRegex.ReplaceAllString(title, "")
	return scraperutil.CleanString(title)
}

func stripTrailingSiteName(v string) string {
	v = scraperutil.CleanString(v)
	for _, suffix := range []string{" - JAV321", " | JAV321", " - Jav321"} {
		v = strings.TrimSuffix(v, suffix)
	}
	return scraperutil.CleanString(v)
}

func stripTags(v string) string {
	re := regexp.MustCompile(`(?s)<[^>]*>`) //nolint:gocritic
	return re.ReplaceAllString(v, "")
}

func hasJapanese(v string) bool {
	for _, r := range v {
		if unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Han) {
			return true
		}
	}
	return false
}

func isHTTPURL(v string) bool {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
