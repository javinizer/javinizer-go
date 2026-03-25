package fc2

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

const defaultBaseURL = "https://adult.contents.fc2.com"

var (
	articleURLRegex   = regexp.MustCompile(`(?i)contents\.fc2\.com/article/(\d{5,10})`)
	fc2IDRegex        = regexp.MustCompile(`(?i)fc2[\s_-]*ppv[\s_-]*(\d{5,10})`)
	ppvIDRegex        = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])ppv[\s_-]*(\d{5,10})(?:$|[^a-z0-9])`)
	digitsOnlyRegex   = regexp.MustCompile(`^\d{5,10}$`)
	idPrefixRegex     = regexp.MustCompile(`(?i)^fc2[\s_-]*ppv[\s_-]*\d+\s*[-:：]?\s*`)
	runtimeClockRegex = regexp.MustCompile(`^(\d{1,2}):(\d{2})(?::(\d{2}))?$`)
	runtimeMinRegex   = regexp.MustCompile(`(\d{1,4})\s*(?:minutes|min|分)?`)
	releaseDateRegex  = regexp.MustCompile(`(\d{4})[/-](\d{1,2})[/-](\d{1,2})`)
	productIDRegex    = regexp.MustCompile(`(?i)商品ID\s*:\s*FC2\s*PPV\s*(\d{5,10})`)
)

// Scraper implements the FC2 scraper.
type Scraper struct {
	client          *resty.Client
	cfg             *config.FC2Config
	enabled         bool
	baseURL         string
	requestDelay    time.Duration
	proxyOverride   *config.ProxyConfig
	downloadProxy   *config.ProxyConfig
	lastRequestTime atomic.Value
}

// New creates a new FC2 scraper.
func New(cfg *config.Config) *Scraper {
	scraperCfg := cfg.Scrapers.FC2
	proxyCfg := config.ResolveScraperProxy(cfg.Scrapers.Proxy, scraperCfg.Proxy)

	client, err := httpclient.NewRestyClient(proxyCfg, 30*time.Second, 3)
	usingProxy := err == nil && proxyCfg.Enabled && strings.TrimSpace(proxyCfg.URL) != ""
	if err != nil {
		logging.Errorf("FC2: Failed to create HTTP client with proxy: %v, using explicit no-proxy fallback", err)
		client = httpclient.NewRestyClientNoProxy(30*time.Second, 3)
	}

	userAgent := config.ResolveScraperUserAgent(
		cfg.Scrapers.UserAgent,
		scraperCfg.UseFakeUserAgent,
		scraperCfg.FakeUserAgent,
	)

	client.SetHeader("User-Agent", userAgent)
	client.SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	client.SetHeader("Accept-Language", "ja,en-US;q=0.8,en;q=0.6")
	client.SetHeader("Connection", "keep-alive")
	client.SetHeader("Upgrade-Insecure-Requests", "1")

	base := strings.TrimSpace(scraperCfg.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")

	s := &Scraper{
		client:        client,
		cfg:           &cfg.Scrapers.FC2,
		enabled:       scraperCfg.Enabled,
		baseURL:       base,
		requestDelay:  time.Duration(scraperCfg.RequestDelay) * time.Millisecond,
		proxyOverride: scraperCfg.Proxy,
		downloadProxy: scraperCfg.DownloadProxy,
	}
	s.lastRequestTime.Store(time.Time{})

	if usingProxy {
		logging.Infof("FC2: Using proxy %s", httpclient.SanitizeProxyURL(proxyCfg.URL))
	}

	return s
}

// Name returns the scraper identifier.
func (s *Scraper) Name() string { return "fc2" }

// IsEnabled returns whether scraper is enabled.
func (s *Scraper) IsEnabled() bool { return s.enabled }

// Config returns the scraper's configuration
func (s *Scraper) Config() *config.ScraperConfig {
	return &config.ScraperConfig{
		Enabled:          s.cfg.Enabled,
		RequestDelay:     s.cfg.RequestDelay,
		UseFakeUserAgent: s.cfg.UseFakeUserAgent,
		FakeUserAgent:    s.cfg.FakeUserAgent,
		Proxy:            s.cfg.Proxy,
		DownloadProxy:    s.cfg.DownloadProxy,
	}
}

// Close cleans up resources held by the scraper
func (s *Scraper) Close() error {
	return nil
}

// ResolveDownloadProxyForHost declares FC2-owned media hosts for downloader proxy routing.
func (s *Scraper) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || !strings.HasSuffix(host, "fc2.com") {
		return nil, nil, false
	}
	return s.downloadProxy, s.proxyOverride, true
}

// ResolveSearchQuery normalizes FC2/PPV identifiers from free-form input.
func (s *Scraper) ResolveSearchQuery(input string) (string, bool) {
	articleID := extractArticleID(input)
	if articleID == "" {
		return "", false
	}
	return canonicalFC2ID(articleID), true
}

// GetURL resolves the FC2 detail URL for an ID.
func (s *Scraper) GetURL(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("movie ID cannot be empty")
	}

	if isHTTPURL(id) {
		articleID := extractArticleID(id)
		if articleID == "" {
			return "", models.NewScraperNotFoundError("FC2", fmt.Sprintf("movie %s does not match FC2 ID format", id))
		}
		return s.buildArticleURL(articleID), nil
	}

	articleID := extractArticleID(id)
	if articleID == "" {
		return "", models.NewScraperNotFoundError("FC2", fmt.Sprintf("movie %s does not match FC2 ID format", id))
	}

	return s.buildArticleURL(articleID), nil
}

// Search scrapes metadata for a given FC2 ID.
func (s *Scraper) Search(id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("FC2 scraper is disabled")
	}

	expectedArticleID := extractArticleID(id)
	if expectedArticleID == "" {
		return nil, models.NewScraperNotFoundError("FC2", fmt.Sprintf("movie %s does not match FC2 ID format", id))
	}

	detailURL, err := s.GetURL(id)
	if err != nil {
		return nil, err
	}

	html, status, err := s.fetchPage(detailURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch FC2 detail page: %w", err)
	}

	if status == 404 {
		return nil, models.NewScraperNotFoundError("FC2", fmt.Sprintf("movie %s not found on FC2", id))
	}
	if status != 200 {
		return nil, models.NewScraperStatusError("FC2", status, fmt.Sprintf("FC2 returned status code %d", status))
	}
	if isFC2NotFoundPage(html) {
		return nil, models.NewScraperNotFoundError("FC2", fmt.Sprintf("movie %s not found on FC2", id))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse FC2 detail page: %w", err)
	}

	result := parseDetailPage(doc, html, detailURL, expectedArticleID)
	if result == nil || strings.TrimSpace(result.ID) == "" {
		return nil, models.NewScraperNotFoundError("FC2", fmt.Sprintf("movie %s not found on FC2", id))
	}

	if actualArticleID := extractArticleID(result.ID); actualArticleID != "" && expectedArticleID != actualArticleID {
		return nil, models.NewScraperNotFoundError("FC2", fmt.Sprintf("movie %s not found on FC2", id))
	}

	return result, nil
}

func parseDetailPage(doc *goquery.Document, html, sourceURL, fallbackArticleID string) *models.ScraperResult {
	articleID := strings.TrimSpace(fallbackArticleID)
	if id := extractArticleID(sourceURL); id != "" {
		articleID = id
	}
	if id := extractProductIDFromHTML(html); id != "" {
		articleID = id
	}

	if articleID == "" {
		return nil
	}

	result := &models.ScraperResult{
		Source:           "fc2",
		SourceURL:        sourceURL,
		Language:         "ja",
		ID:               canonicalFC2ID(articleID),
		ContentID:        canonicalFC2ID(articleID),
		ShouldCropPoster: false,
	}

	fullTitle := cleanString(doc.Find("meta[property='og:title']").AttrOr("content", ""))
	if fullTitle == "" {
		fullTitle = cleanString(doc.Find("title").First().Text())
	}
	fullTitle = stripSiteSuffix(fullTitle)
	result.Title = stripFC2IDPrefix(fullTitle)
	if result.Title == "" {
		result.Title = fullTitle
	}
	if result.Title == "" {
		result.Title = result.ID
	}
	result.OriginalTitle = result.Title

	description := cleanString(doc.Find("meta[property='og:description']").AttrOr("content", ""))
	if description == "" {
		description = cleanString(doc.Find("meta[name='description']").AttrOr("content", ""))
	}
	description = stripFC2IDPrefix(description)
	result.Description = description

	if releaseDate := parseReleaseDate(extractInfoValue(doc, "販売日")); releaseDate != nil {
		result.ReleaseDate = releaseDate
	}

	runtimeText := cleanString(doc.Find(".items_article_MainitemThumb .items_article_info").First().Text())
	result.Runtime = parseRuntime(runtimeText)

	result.Maker = cleanString(doc.Find(".items_article_headerInfo a[href*='/users/']").First().Text())
	result.Genres = extractTags(doc)

	coverURL := normalizeURL(doc.Find("meta[property='og:image']").AttrOr("content", ""), sourceURL)
	if coverURL == "" {
		coverURL = normalizeURL(doc.Find(".items_article_MainitemThumb img").First().AttrOr("src", ""), sourceURL)
	}
	result.CoverURL = coverURL
	result.PosterURL = coverURL

	result.ScreenshotURL = extractScreenshotURLs(doc, sourceURL)
	result.TrailerURL = normalizeURL(doc.Find("meta[property='og:video']").AttrOr("content", ""), sourceURL)
	result.Rating = extractRating(doc)

	return result
}

func extractRating(doc *goquery.Document) *models.Rating {
	var rating *models.Rating

	doc.Find("script[type='application/ld+json']").EachWithBreak(func(_ int, script *goquery.Selection) bool {
		raw := strings.TrimSpace(script.Text())
		if raw == "" {
			return true
		}

		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return true
		}

		aggRaw, ok := payload["aggregateRating"]
		if !ok {
			return true
		}
		agg, ok := aggRaw.(map[string]interface{})
		if !ok {
			return true
		}

		score := toFloat64(agg["ratingValue"])
		votes := toInt(agg["reviewCount"])
		if score <= 0 && votes == 0 {
			return true
		}

		rating = &models.Rating{Score: score, Votes: votes}
		return false
	})

	return rating
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, err := n.Float64()
		if err == nil {
			return f
		}
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err == nil {
			return f
		}
	}
	return 0
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return int(i)
		}
		f, err := n.Float64()
		if err == nil {
			return int(f)
		}
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		if err == nil {
			return i
		}
	}
	return 0
}

func extractInfoValue(doc *goquery.Document, label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}

	var value string
	doc.Find(".items_article_softDevice p").EachWithBreak(func(_ int, p *goquery.Selection) bool {
		text := cleanString(p.Text())
		if !strings.Contains(text, label) {
			return true
		}

		parts := strings.SplitN(text, ":", 2)
		if len(parts) == 2 {
			value = cleanString(parts[1])
		} else {
			parts = strings.SplitN(text, "：", 2)
			if len(parts) == 2 {
				value = cleanString(parts[1])
			}
		}
		if value == "" {
			value = text
		}
		return false
	})

	return value
}

func extractTags(doc *goquery.Document) []string {
	seen := map[string]bool{}
	tags := make([]string, 0)

	doc.Find(".items_article_TagArea a.tagTag").Each(func(_ int, a *goquery.Selection) {
		tag := cleanString(a.Text())
		if tag == "" || seen[tag] {
			return
		}
		seen[tag] = true
		tags = append(tags, tag)
	})

	return tags
}

func extractScreenshotURLs(doc *goquery.Document, sourceURL string) []string {
	seen := map[string]bool{}
	urls := make([]string, 0)

	doc.Find(".items_article_SampleImagesArea a[href], .items_article_SampleImages a[href]").Each(func(_ int, a *goquery.Selection) {
		href := normalizeURL(a.AttrOr("href", ""), sourceURL)
		if href == "" || seen[href] {
			return
		}
		seen[href] = true
		urls = append(urls, href)
	})

	return urls
}

func parseReleaseDate(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if matches := releaseDateRegex.FindStringSubmatch(raw); len(matches) == 4 {
		year, _ := strconv.Atoi(matches[1])
		month, _ := strconv.Atoi(matches[2])
		day, _ := strconv.Atoi(matches[3])
		if year > 0 && month >= 1 && month <= 12 && day >= 1 && day <= 31 {
			t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
			return &t
		}
	}
	return nil
}

func parseRuntime(raw string) int {
	raw = cleanString(raw)
	if raw == "" {
		return 0
	}

	if m := runtimeClockRegex.FindStringSubmatch(raw); len(m) == 4 {
		if m[3] == "" {
			minutes, _ := strconv.Atoi(m[1])
			seconds, _ := strconv.Atoi(m[2])
			if seconds >= 30 {
				minutes++
			}
			return minutes
		}

		hours, _ := strconv.Atoi(m[1])
		minutes, _ := strconv.Atoi(m[2])
		seconds, _ := strconv.Atoi(m[3])
		total := hours*60 + minutes
		if seconds >= 30 {
			total++
		}
		return total
	}

	if m := runtimeMinRegex.FindStringSubmatch(raw); len(m) > 1 {
		minutes, _ := strconv.Atoi(m[1])
		return minutes
	}

	return 0
}

func isFC2NotFoundPage(html string) bool {
	content := strings.ToLower(html)
	return strings.Contains(content, "お探しの商品が見つかりませんでした") ||
		strings.Contains(content, "this page may have been deleted")
}

func extractArticleID(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	if m := articleURLRegex.FindStringSubmatch(input); len(m) > 1 {
		return m[1]
	}
	if m := fc2IDRegex.FindStringSubmatch(input); len(m) > 1 {
		return m[1]
	}
	if m := ppvIDRegex.FindStringSubmatch(input); len(m) > 1 {
		return m[1]
	}
	if digitsOnlyRegex.MatchString(input) {
		return input
	}

	return ""
}

func extractProductIDFromHTML(html string) string {
	if m := productIDRegex.FindStringSubmatch(html); len(m) > 1 {
		return m[1]
	}
	return ""
}

func canonicalFC2ID(articleID string) string {
	return "FC2-PPV-" + strings.TrimSpace(articleID)
}

func stripFC2IDPrefix(value string) string {
	return strings.TrimSpace(idPrefixRegex.ReplaceAllString(strings.TrimSpace(value), ""))
}

func stripSiteSuffix(title string) string {
	title = cleanString(title)
	if title == "" {
		return ""
	}

	for _, sep := range []string{"|", "｜"} {
		idx := strings.LastIndex(title, sep)
		if idx <= 0 {
			continue
		}
		suffix := strings.TrimSpace(title[idx+len(sep):])
		if strings.Contains(strings.ToLower(suffix), "fc2") {
			return cleanString(title[:idx])
		}
	}

	return title
}

func cleanString(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

func normalizeURL(raw, sourceURL string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.IsAbs() {
		return parsed.String()
	}

	base, err := url.Parse(sourceURL)
	if err != nil {
		return ""
	}
	return base.ResolveReference(parsed).String()
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func (s *Scraper) buildArticleURL(articleID string) string {
	return fmt.Sprintf("%s/article/%s/", s.baseURL, strings.TrimSpace(articleID))
}

func (s *Scraper) fetchPage(targetURL string) (string, int, error) {
	s.waitForRateLimit()
	defer s.updateLastRequestTime()

	resp, err := s.client.R().Get(targetURL)
	if err != nil {
		return "", 0, err
	}
	return resp.String(), resp.StatusCode(), nil
}

func (s *Scraper) waitForRateLimit() {
	if s.requestDelay <= 0 {
		return
	}

	last, _ := s.lastRequestTime.Load().(time.Time)
	if last.IsZero() {
		return
	}

	elapsed := time.Since(last)
	if elapsed < s.requestDelay {
		time.Sleep(s.requestDelay - elapsed)
	}
}

func (s *Scraper) updateLastRequestTime() {
	s.lastRequestTime.Store(time.Now())
}
