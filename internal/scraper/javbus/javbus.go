package javbus

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper"
)

const (
	defaultBaseURL  = "https://www.javbus.com"
	defaultAge      = "verified"
	defaultDV       = "1"
	defaultExistMag = "mag"
)

var (
	nonAlphaNumRegex = regexp.MustCompile(`[^a-z0-9]+`)
	titleRegex       = regexp.MustCompile(`(?i)^([a-z0-9_-]+)\s+(.*?)\s*-\s*javbus`)
	runtimeRegex     = regexp.MustCompile(`(\d+)`)
	dateFormats      = []string{"2006-01-02", "2006/01/02", "2006.01.02"}
)

// Scraper implements the JavBus scraper.
type Scraper struct {
	client          *resty.Client
	enabled         bool
	baseURL         string
	language        string
	requestDelay    time.Duration
	proxyOverride   *config.ProxyConfig
	downloadProxy   *config.ProxyConfig
	lastRequestTime atomic.Value
	settings        config.ScraperSettings // stores the full settings for Config() method
}

// New creates a new JavBus scraper.
func New(settings config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) *Scraper {
	// Build ScraperConfig for HTTP client (HTTP-01 pattern)
	configForHTTP := &config.ScraperSettings{
		Enabled:       settings.Enabled,
		Timeout:       30,
		RateLimit:     settings.RateLimit,
		RetryCount:    3,
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
		logging.Errorf("JavBus: Failed to create HTTP client with proxy: %v, using explicit no-proxy fallback", err)
		client = httpclient.NewRestyClientNoProxy(30*time.Second, 3)
	}

	base := strings.TrimSpace(settings.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")

	lang := normalizeLanguage(settings.Language)

	s := &Scraper{
		client:        client,
		enabled:       settings.Enabled,
		baseURL:       base,
		language:      lang,
		requestDelay:  time.Duration(settings.RateLimit) * time.Millisecond,
		proxyOverride: settings.Proxy,
		downloadProxy: settings.DownloadProxy,
		settings:      settings,
	}
	s.lastRequestTime.Store(time.Time{})

	if usingProxy {
		logging.Infof("JavBus: Using proxy %s", httpclient.SanitizeProxyURL(proxyCfg.URL))
	}

	return s
}

// Name returns the scraper identifier.
func (s *Scraper) Name() string {
	return "javbus"
}

// IsEnabled returns whether the scraper is enabled.
func (s *Scraper) IsEnabled() bool {
	return s.enabled
}

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
		return fmt.Errorf("javbus: config is nil")
	}
	if !cfg.Enabled {
		return nil // Disabled is valid
	}
	if cfg.RateLimit < 0 {
		return fmt.Errorf("javbus: rate_limit must be non-negative, got %d", cfg.RateLimit)
	}
	if cfg.RetryCount < 0 {
		return fmt.Errorf("javbus: retry_count must be non-negative, got %d", cfg.RetryCount)
	}
	if cfg.Timeout < 0 {
		return fmt.Errorf("javbus: timeout must be non-negative, got %d", cfg.Timeout)
	}
	return nil
}

// ResolveDownloadProxyForHost declares JavBus-owned media hosts for downloader proxy routing.
func (s *Scraper) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil, nil, false
	}
	if strings.HasSuffix(host, "javbus.com") || strings.HasSuffix(host, "javbus.org") {
		return s.downloadProxy, s.proxyOverride, true
	}
	return nil, nil, false
}

// GetURL attempts to find a detail URL for the given movie ID.
func (s *Scraper) CanHandleURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return strings.HasSuffix(host, "javbus.com") || strings.HasSuffix(host, "javbus.org")
}

func (s *Scraper) ExtractIDFromURL(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return "", fmt.Errorf("URL has no path, not a detail page")
	}
	parts := strings.Split(path, "/")
	var candidates []string
	for _, p := range parts {
		if p == "en" || p == "ja" || p == "zh" || p == "cn" || p == "tw" {
			continue
		}
		if p != "" {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("URL has no ID path segment, not a detail page")
	}
	if len(candidates) > 1 {
		return "", fmt.Errorf("URL has multiple path segments (%v), not a detail page", candidates)
	}
	return strings.ToUpper(candidates[0]), nil
}

func (s *Scraper) ScrapeURL(url string) (*models.ScraperResult, error) {
	if !s.CanHandleURL(url) {
		return nil, models.NewScraperNotFoundError("JavBus", "URL not handled by JavBus scraper")
	}

	detailURL := s.applyLanguageToURL(url)
	html, status, err := s.fetchPage(detailURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JavBus detail page: %w", err)
	}
	if status == 404 {
		return nil, models.NewScraperNotFoundError("JavBus", "page not found")
	}
	if status == 429 {
		return nil, models.NewScraperStatusError("JavBus", 429, "rate limited")
	}
	if status == 403 || status == 451 {
		return nil, models.NewScraperStatusError("JavBus", status, "access blocked")
	}
	if status != 200 {
		return nil, models.NewScraperStatusError("JavBus", status, fmt.Sprintf("JavBus returned status code %d", status))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse JavBus detail page: %w", err)
	}

	id, _ := s.ExtractIDFromURL(url)
	return s.parseDetailPage(doc, detailURL, id)
}

func (s *Scraper) GetURL(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("movie ID cannot be empty")
	}

	if isHTTPURL(id) {
		return s.applyLanguageToURL(id), nil
	}

	searchHosts := []string{s.baseURL}
	if strings.Contains(s.baseURL, "javbus.com") && !strings.Contains(s.baseURL, "javbus.org") {
		searchHosts = append(searchHosts, "https://www.javbus.org")
	}

	searchPaths := []string{
		fmt.Sprintf("/search/%s&type=0&parent=uc", url.PathEscape(id)),
		fmt.Sprintf("/uncensored/search/%s&type=0&parent=uc", url.PathEscape(id)),
	}

	for _, host := range searchHosts {
		for _, p := range searchPaths {
			target := strings.TrimRight(host, "/") + p
			html, status, err := s.fetchPage(target)
			if err != nil {
				if scraperErr, ok := models.AsScraperError(err); ok && scraperErr.Kind == models.ScraperErrorKindBlocked {
					return "", err
				}
				continue
			}
			if status != 200 {
				continue
			}

			if detail := s.findDetailURL(html, host, id); detail != "" {
				return s.applyLanguageToURL(detail), nil
			}
		}
	}

	return "", models.NewScraperNotFoundError("JavBus", fmt.Sprintf("movie %s not found on JavBus", id))
}

// Search searches JavBus for a movie and extracts metadata.
func (s *Scraper) Search(id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("JavBus scraper is disabled")
	}

	detailURL, err := s.GetURL(id)
	if err != nil {
		return nil, err
	}

	html, status, err := s.fetchPage(detailURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JavBus detail page: %w", err)
	}
	if status != 200 {
		return nil, models.NewScraperStatusError("JavBus", status, fmt.Sprintf("JavBus returned status code %d", status))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse JavBus detail page: %w", err)
	}

	return s.parseDetailPage(doc, detailURL, id)
}

func (s *Scraper) parseDetailPage(doc *goquery.Document, sourceURL, fallbackID string) (*models.ScraperResult, error) {
	result := &models.ScraperResult{
		Source:    s.Name(),
		SourceURL: sourceURL,
		Language:  s.language,
	}

	titleText := cleanString(doc.Find("title").First().Text())
	if m := titleRegex.FindStringSubmatch(strings.ToLower(titleText)); len(m) >= 3 {
		result.ID = strings.ToUpper(strings.TrimSpace(m[1]))
		result.Title = cleanString(strings.TrimSpace(titleText[len(m[1]):]))
		result.Title = strings.TrimSuffix(result.Title, " - JavBus")
	}

	if result.ID == "" {
		result.ID = strings.ToUpper(extractInfoValue(doc, []string{"品番", "識別碼", "识别码", "id"}))
	}
	if result.ID == "" {
		result.ID = strings.ToUpper(strings.TrimSpace(fallbackID))
	}
	result.ContentID = result.ID

	if result.Title == "" {
		title := cleanString(doc.Find("h3").First().Text())
		if title == "" {
			title = cleanString(doc.Find("a.bigImage img").First().AttrOr("title", ""))
		}
		if title == "" {
			title = result.ID
		}
		result.Title = title
	}
	result.OriginalTitle = result.Title

	if rawDate := extractInfoValue(doc, []string{"発売日", "發行日期", "发行日期", "date"}); rawDate != "" {
		result.ReleaseDate = parseDate(rawDate)
	}

	if rawRuntime := extractInfoValue(doc, []string{"収録時間", "長度", "长度", "runtime", "length"}); rawRuntime != "" {
		if m := runtimeRegex.FindStringSubmatch(rawRuntime); len(m) > 1 {
			if v, err := strconv.Atoi(m[1]); err == nil {
				result.Runtime = v
			}
		}
	}

	result.Director = extractInfoLinkValue(doc, []string{"監督", "導演", "导演", "director"})
	result.Maker = extractInfoLinkValue(doc, []string{"メーカー", "製作商", "制作商", "maker", "studio"})
	result.Label = extractInfoLinkValue(doc, []string{"レーベル", "發行商", "发行商", "label"})
	result.Series = extractInfoLinkValue(doc, []string{"シリーズ", "系列", "series"})
	result.Description = extractDescription(doc)

	result.Actresses = extractActresses(doc)
	result.Genres = extractGenres(doc)

	result.CoverURL = extractCoverURL(doc, sourceURL)
	result.PosterURL = result.CoverURL
	result.ScreenshotURL = extractScreenshotURLs(doc, sourceURL)
	result.TrailerURL = extractTrailerURL(doc)

	return result, nil
}

func (s *Scraper) fetchPage(targetURL string) (string, int, error) {
	s.waitForRateLimit()
	defer s.updateLastRequestTime()

	resp, err := s.client.R().Get(targetURL)
	if err != nil {
		return "", 0, err
	}
	html := resp.String()
	if raw := resp.RawResponse; raw != nil && raw.Request != nil && raw.Request.URL != nil {
		if strings.Contains(strings.ToLower(raw.Request.URL.Path), "/doc/driver-verify") {
			return "", resp.StatusCode(), models.NewScraperChallengeError(
				"JavBus",
				"JavBus returned a driver verification challenge page (request blocked; adjust proxy/IP)",
			)
		}
	}
	if resp.StatusCode() == 200 && isJavbusChallengePage(html) {
		return "", resp.StatusCode(), models.NewScraperChallengeError(
			"JavBus",
			"JavBus returned a driver verification challenge page (request blocked; adjust proxy/IP)",
		)
	}
	if resp.StatusCode() == 200 && models.IsCloudflareChallengePage(html) {
		return "", resp.StatusCode(), models.NewScraperChallengeError(
			"JavBus",
			"JavBus returned a Cloudflare challenge page (request blocked; adjust proxy/IP or cookies)",
		)
	}
	return html, resp.StatusCode(), nil
}

func (s *Scraper) findDetailURL(html, base, id string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}

	targetID := normalizeID(id)
	var found string

	doc.Find("a.movie-box[href]").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		href, ok := sel.Attr("href")
		if !ok || href == "" {
			return true
		}

		candidateID := cleanString(sel.Find("date").First().Text())
		title := cleanString(sel.AttrOr("title", ""))

		if idsMatch(candidateID, targetID) || idsMatch(title, targetID) || idsMatch(href, targetID) {
			found = resolveURL(base, href)
			return false
		}
		return true
	})

	if found != "" {
		return found
	}

	// Fallback: if exactly one detail link is present, use it.
	candidates := make([]string, 0, 1)
	doc.Find("a.movie-box[href]").Each(func(_ int, sel *goquery.Selection) {
		href, ok := sel.Attr("href")
		if !ok || href == "" {
			return
		}
		candidates = append(candidates, resolveURL(base, href))
	})
	if len(candidates) == 1 {
		return candidates[0]
	}

	return ""
}

func extractInfoValue(doc *goquery.Document, labels []string) string {
	labelMatches := func(label string) bool {
		norm := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(label, ":")))
		for _, needle := range labels {
			if strings.Contains(norm, strings.ToLower(needle)) {
				return true
			}
		}
		return false
	}

	var value string
	doc.Find("#info p, .info p").EachWithBreak(func(_ int, p *goquery.Selection) bool {
		header := cleanString(p.Find("span.header").First().Text())
		if !labelMatches(header) {
			text := cleanString(p.Text())
			parts := strings.SplitN(text, ":", 2)
			if len(parts) < 2 || !labelMatches(parts[0]) {
				return true
			}
		}

		text := cleanString(p.Text())
		text = strings.TrimSpace(strings.TrimPrefix(text, header))
		text = strings.TrimLeft(text, ":： ")
		value = cleanString(text)
		return false
	})

	return value
}

func extractInfoLinkValue(doc *goquery.Document, labels []string) string {
	labelMatches := func(label string) bool {
		norm := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(label, ":")))
		for _, needle := range labels {
			if strings.Contains(norm, strings.ToLower(needle)) {
				return true
			}
		}
		return false
	}

	var value string
	doc.Find("#info p, .info p").EachWithBreak(func(_ int, p *goquery.Selection) bool {
		header := cleanString(p.Find("span.header").First().Text())
		if !labelMatches(header) {
			return true
		}
		if link := cleanString(p.Find("a").First().Text()); link != "" {
			value = link
			return false
		}
		text := cleanString(p.Text())
		text = strings.TrimSpace(strings.TrimPrefix(text, header))
		text = strings.TrimLeft(text, ":： ")
		value = cleanString(text)
		return false
	})
	return value
}

func extractActresses(doc *goquery.Document) []models.ActressInfo {
	seen := map[string]bool{}
	actresses := make([]models.ActressInfo, 0)

	appendName := func(name string, thumb string) {
		name = cleanString(name)
		if isInvalidActressName(name) || seen[name] {
			return
		}
		seen[name] = true

		info := models.ActressInfo{ThumbURL: cleanString(thumb)}
		if hasJapanese(name) {
			info.JapaneseName = name
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
		actresses = append(actresses, info)
	}

	doc.Find("#star-div a[href*='/star/'], #avatar-waterfall a[href*='/star/'], .star-show a[href*='/star/'], .star-name a[href*='/star/']").Each(func(_ int, a *goquery.Selection) {
		name := a.Find("img").AttrOr("title", "")
		if name == "" {
			name = a.AttrOr("title", "")
		}
		if name == "" {
			name = a.Text()
		}
		thumb := a.Find("img").AttrOr("src", "")
		appendName(name, thumb)
	})

	doc.Find("#info a[href*='/star/'], .info a[href*='/star/']").Each(func(_ int, a *goquery.Selection) {
		appendName(a.Text(), "")
	})

	return actresses
}

func isInvalidActressName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}

	lower := strings.ToLower(name)
	if strings.Contains(lower, "<") || strings.Contains(lower, ">") {
		return true
	}

	// JavBus occasionally emits placeholder/malformed star names for some pages.
	if strings.Contains(name, "画像を拡大") ||
		strings.Contains(name, "点击放大") ||
		strings.Contains(name, "點擊放大") ||
		strings.Contains(lower, "click to enlarge") ||
		name == "出演者" ||
		name == "演員" ||
		name == "演员" {
		return true
	}

	return false
}

func extractGenres(doc *goquery.Document) []string {
	seen := map[string]bool{}
	genres := make([]string, 0)

	add := func(v string) {
		v = cleanString(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		genres = append(genres, v)
	}

	doc.Find("#genre-toggle a").Each(func(_ int, a *goquery.Selection) {
		add(a.Text())
	})
	doc.Find("#info a[href*='/genre/'], .info a[href*='/genre/']").Each(func(_ int, a *goquery.Selection) {
		add(a.Text())
	})

	return genres
}

func extractCoverURL(doc *goquery.Document, base string) string {
	selectors := []string{
		"a.bigImage[href]",
		"a.bigImage img[src]",
		"a.bigImage img[data-src]",
		"a.bigImage img[data-original]",
		"#cover img[src]",
		"#cover img[data-src]",
		"#cover img[data-original]",
	}
	for _, sel := range selectors {
		node := doc.Find(sel).First()
		if node.Length() == 0 {
			continue
		}
		attr := "src"
		if strings.Contains(sel, "href") {
			attr = "href"
		}
		raw := cleanString(node.AttrOr(attr, ""))
		if raw == "" {
			continue
		}
		u := resolveURL(base, raw)
		if isLikelyImageURL(u) {
			return u
		}
	}
	return ""
}

func extractScreenshotURLs(doc *goquery.Document, base string) []string {
	seen := map[string]bool{}
	list := make([]string, 0)

	add := func(raw string) {
		raw = cleanString(raw)
		if raw == "" {
			return
		}
		u := resolveURL(base, raw)
		u = normalizeJavbusImageURL(u)
		if u == "" || seen[u] || !isLikelyImageURL(u) {
			return
		}
		seen[u] = true
		list = append(list, u)
	}

	// Primary: use canonical sample links from <a class="sample-box" href="...">.
	// These are usually full-size screenshot URLs.
	doc.Find("a.sample-box[href]").Each(func(_ int, a *goquery.Selection) {
		add(a.AttrOr("href", ""))
	})
	doc.Find("#sample-waterfall a[href]").Each(func(_ int, a *goquery.Selection) {
		add(a.AttrOr("href", ""))
	})

	// Fallback: if href extraction yields nothing, use preview image URLs.
	if len(list) == 0 {
		doc.Find("a.sample-box img[src], #sample-waterfall img[src], .photo-frame img[src]").Each(func(_ int, img *goquery.Selection) {
			add(img.AttrOr("src", ""))
		})
		doc.Find("a.sample-box img[data-src], #sample-waterfall img[data-src], .photo-frame img[data-src]").Each(func(_ int, img *goquery.Selection) {
			add(img.AttrOr("data-src", ""))
		})
		doc.Find("a.sample-box img[data-original], #sample-waterfall img[data-original], .photo-frame img[data-original]").Each(func(_ int, img *goquery.Selection) {
			add(img.AttrOr("data-original", ""))
		})
	}

	return list
}

func extractTrailerURL(doc *goquery.Document) string {
	if src := cleanString(doc.Find("video source[src]").First().AttrOr("src", "")); src != "" {
		return src
	}
	return ""
}

func extractDescription(doc *goquery.Document) string {
	description := cleanString(doc.Find("meta[name='description']").AttrOr("content", ""))
	if description == "" {
		description = cleanString(doc.Find("meta[property='og:description']").AttrOr("content", ""))
	}
	return description
}

func parseDate(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	for _, f := range dateFormats {
		if t, err := time.Parse(f, raw); err == nil {
			return &t
		}
	}
	return nil
}

func (s *Scraper) waitForRateLimit() {
	if s.requestDelay <= 0 {
		return
	}
	lastReq := s.lastRequestTime.Load()
	if lastReq == nil {
		return
	}
	lastTime, ok := lastReq.(time.Time)
	if !ok || lastTime.IsZero() {
		return
	}
	if elapsed := time.Since(lastTime); elapsed < s.requestDelay {
		time.Sleep(s.requestDelay - elapsed)
	}
}

func (s *Scraper) updateLastRequestTime() {
	s.lastRequestTime.Store(time.Now())
}

func normalizeLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "en":
		return "en"
	case "ja":
		return "ja"
	case "zh", "cn", "tw":
		return "zh"
	default:
		return "zh"
	}
}

func (s *Scraper) applyLanguageToURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 {
		switch parts[0] {
		case "en", "ja", "zh", "cn", "tw":
			parts = parts[1:]
		}
	}

	if s.language == "en" || s.language == "ja" {
		parts = append([]string{s.language}, parts...)
	}

	u.Path = "/" + strings.Join(parts, "/")
	return u.String()
}

func normalizeID(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	return nonAlphaNumRegex.ReplaceAllString(v, "")
}

func idsMatch(candidate, targetNormalized string) bool {
	if targetNormalized == "" {
		return false
	}
	c := normalizeID(candidate)
	return c != "" && (c == targetNormalized || strings.Contains(c, targetNormalized) || strings.Contains(targetNormalized, c))
}

func resolveURL(base, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}

	baseURL, err := url.Parse(base)
	if err != nil {
		return raw
	}

	if strings.HasPrefix(raw, "/") {
		baseURL.Path = raw
		baseURL.RawQuery = ""
		return baseURL.String()
	}

	baseURL.Path = path.Join(path.Dir(baseURL.Path), raw)
	return baseURL.String()
}

func hasJapanese(v string) bool {
	for _, r := range v {
		if unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Han) {
			return true
		}
	}
	return false
}

func cleanString(v string) string {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, "\u00a0", " ")
	v = strings.Join(strings.Fields(v), " ")
	return v
}

func isHTTPURL(v string) bool {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func isLikelyImageURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	ext := strings.ToLower(path.Ext(u.Path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".avif":
		return true
	default:
		return false
	}
}

func isJavbusChallengePage(html string) bool {
	lower := strings.ToLower(strings.TrimSpace(html))
	if lower == "" {
		return false
	}

	markers := []string{
		"/doc/driver-verify",
		"age verification javbus",
		"driver verification",
		"driver-verify?referer=",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	return false
}

// normalizeJavbusImageURL normalizes extracted image URLs (especially DMM-hosted URLs)
// to improve compatibility with downloader logic.
func normalizeJavbusImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// Handle protocol-relative URLs.
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}

	// Normalize awsimgsrc URLs to pics.dmm.co.jp when present.
	raw = strings.Replace(raw, "awsimgsrc.dmm.co.jp/pics_dig", "pics.dmm.co.jp", 1)

	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	// Strip query/fragment for stable deduplication.
	u.RawQuery = ""
	u.Fragment = ""

	host := strings.ToLower(u.Hostname())
	if strings.HasSuffix(host, "dmm.co.jp") || strings.HasSuffix(host, "dmm.com") {
		segments := strings.Split(u.Path, "/")
		for i, seg := range segments {
			if seg == "" {
				continue
			}
			segments[i] = canonicalizeDMMPrefixedContentID(seg)
		}
		u.Path = strings.Join(segments, "/")

		base := path.Base(u.Path)
		if strings.HasSuffix(strings.ToLower(base), ".jpg") &&
			strings.Contains(base, "-") &&
			!strings.Contains(strings.ToLower(base), "jp-") &&
			!strings.HasSuffix(strings.ToLower(base), "pl.jpg") &&
			!strings.HasSuffix(strings.ToLower(base), "ps.jpg") {
			// Mirror DMM old-site normalization: xxx-1.jpg -> xxxjp-1.jpg
			base = strings.Replace(base, "-", "jp-", 1)
			u.Path = strings.TrimSuffix(u.Path, path.Base(u.Path)) + base
		}
	}

	return u.String()
}

// canonicalizeDMMPrefixedContentID normalizes DMM path segments that contain
// a numeric prefix before the studio code (e.g., 118abp00880 -> 118abp880).
// This aligns JavBus-linked DMM URLs with the canonical URLs returned by DMM itself.
func canonicalizeDMMPrefixedContentID(seg string) string {
	// Split extension so normalization only targets the stem.
	ext := ""
	if idx := strings.LastIndex(seg, "."); idx > 0 {
		ext = seg[idx:]
		seg = seg[:idx]
	}

	// Preserve known suffixes like jp-1 / pl / ps while normalizing core CID.
	suffix := ""
	lower := strings.ToLower(seg)
	for _, marker := range []string{"jp-", "pl", "ps"} {
		if marker == "jp-" {
			if idx := strings.Index(lower, marker); idx > 0 {
				suffix = seg[idx:]
				seg = seg[:idx]
				lower = strings.ToLower(seg)
				break
			}
			continue
		}
		if strings.HasSuffix(lower, marker) && len(seg) > len(marker) {
			suffix = seg[len(seg)-len(marker):]
			seg = seg[:len(seg)-len(marker)]
			lower = strings.ToLower(seg)
			break
		}
	}

	// Only canonicalize segments with numeric prefix before letters.
	// Examples:
	// - 118abp00880 -> 118abp880
	// - 118abp880   -> 118abp880 (unchanged)
	// - ipx00535    -> ipx00535  (unchanged: no numeric prefix)
	re := regexp.MustCompile(`^(\d+[a-z]+)0+(\d+.*)$`)
	if matches := re.FindStringSubmatch(lower); len(matches) == 3 {
		seg = matches[1] + matches[2]
	}

	return seg + suffix + ext
}

func buildCookieHeader() string {
	parts := make([]string, 0, 4)
	appendCookie := func(name, value string) {
		value = sanitizeCookieValue(value)
		if value == "" {
			return
		}
		parts = append(parts, name+"="+value)
	}

	appendCookie("age", defaultAge)
	appendCookie("dv", defaultDV)
	appendCookie("existmag", defaultExistMag)

	return strings.Join(parts, "; ")
}

func sanitizeCookieValue(v string) string {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, ";", "")
	v = strings.ReplaceAll(v, "\r", "")
	v = strings.ReplaceAll(v, "\n", "")
	return v
}

func init() {
	scraper.RegisterScraper("javbus", func(settings config.ScraperSettings, db *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
		return New(settings, &globalConfig.Proxy, globalConfig.FlareSolverr), nil
	})
	// Register default settings and priority
	scraper.RegisterScraperDefaults("javbus", scraper.DefaultSettings{
		Settings: config.ScraperSettings{
			Enabled:   false,
			Language:  "ja",
			RateLimit: 1000,
		},
		Priority: 70,
	})
}
