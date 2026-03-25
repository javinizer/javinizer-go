package tokyohot

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
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

const defaultBaseURL = "https://www.tokyo-hot.com"

var (
	nonAlphaNumRegex = regexp.MustCompile(`[^a-z0-9]+`)
	runtimeRegex     = regexp.MustCompile(`(\d{1,3})`)
	timeRuntimeRegex = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})`)
	dateRegex        = regexp.MustCompile(`(\d{4}/\d{2}/\d{2}|\d{4}-\d{2}-\d{2})`)
)

// Scraper implements the TokyoHot scraper.
type Scraper struct {
	client          *resty.Client
	cfg             *config.TokyoHotConfig
	enabled         bool
	baseURL         string
	language        string
	requestDelay    time.Duration
	proxyOverride   *config.ProxyConfig
	downloadProxy   *config.ProxyConfig
	lastRequestTime atomic.Value
}

// New creates a new TokyoHot scraper.
func New(cfg *config.Config) *Scraper {
	scraperCfg := cfg.Scrapers.TokyoHot
	proxyCfg := config.ResolveScraperProxy(cfg.Scrapers.Proxy, scraperCfg.Proxy)

	client, err := httpclient.NewRestyClient(proxyCfg, 30*time.Second, 3)
	usingProxy := err == nil && proxyCfg.Enabled && strings.TrimSpace(proxyCfg.URL) != ""
	if err != nil {
		logging.Errorf("TokyoHot: Failed to create HTTP client with proxy: %v, using explicit no-proxy fallback", err)
		client = httpclient.NewRestyClientNoProxy(30*time.Second, 3)
	}

	userAgent := config.ResolveScraperUserAgent(
		cfg.Scrapers.UserAgent,
		scraperCfg.UseFakeUserAgent,
		scraperCfg.FakeUserAgent,
	)
	client.SetHeader("User-Agent", userAgent)
	client.SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	client.SetHeader("Accept-Language", "ja,en-US;q=0.8,en;q=0.6,zh;q=0.5")
	client.SetHeader("Connection", "keep-alive")
	client.SetHeader("Upgrade-Insecure-Requests", "1")

	base := strings.TrimSpace(scraperCfg.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")

	lang := normalizeLanguage(scraperCfg.Language)

	s := &Scraper{
		client:        client,
		cfg:           &cfg.Scrapers.TokyoHot,
		enabled:       scraperCfg.Enabled,
		baseURL:       base,
		language:      lang,
		requestDelay:  time.Duration(scraperCfg.RequestDelay) * time.Millisecond,
		proxyOverride: scraperCfg.Proxy,
		downloadProxy: scraperCfg.DownloadProxy,
	}
	s.lastRequestTime.Store(time.Time{})

	if usingProxy {
		logging.Infof("TokyoHot: Using proxy %s", httpclient.SanitizeProxyURL(proxyCfg.URL))
	}

	return s
}

// Name returns the scraper identifier.
func (s *Scraper) Name() string { return "tokyohot" }

// IsEnabled returns whether the scraper is enabled.
func (s *Scraper) IsEnabled() bool { return s.enabled }

// Config returns the scraper's configuration
func (s *Scraper) Config() *config.ScraperConfig {
	return &config.ScraperConfig{
		Enabled:          s.cfg.Enabled,
		Language:         s.cfg.Language,
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

// ResolveDownloadProxyForHost declares TokyoHot-owned media hosts for downloader proxy routing.
func (s *Scraper) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || !strings.HasSuffix(host, "tokyo-hot.com") {
		return nil, nil, false
	}
	return s.downloadProxy, s.proxyOverride, true
}

// GetURL finds the TokyoHot detail URL for an ID.
func (s *Scraper) GetURL(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("movie ID cannot be empty")
	}
	if isHTTPURL(id) {
		return s.applyLanguage(id), nil
	}

	target := fmt.Sprintf("%s/product/?q=%s", s.baseURL, url.QueryEscape(id))
	html, status, err := s.fetchPage(target)
	if err != nil {
		return "", fmt.Errorf("failed to search TokyoHot: %w", err)
	}
	if status != 200 {
		return "", models.NewScraperStatusError("TokyoHot", status, fmt.Sprintf("TokyoHot search returned status code %d", status))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", fmt.Errorf("failed to parse TokyoHot search page: %w", err)
	}

	targetID := normalizeID(id)
	var found string
	doc.Find("a[href*='/product/']").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href := strings.TrimSpace(a.AttrOr("href", ""))
		if href == "" || strings.Contains(href, "?q=") {
			return true
		}
		card := a.Closest("a")
		text := cleanString(card.Text() + " " + a.Text())
		cand := extractID(text)
		if cand == "" {
			cand = extractID(href)
		}
		if cand != "" && normalizeID(cand) == targetID {
			found = resolveURL(s.baseURL, href)
			return false
		}
		return true
	})

	if found == "" {
		candidates := make([]string, 0, 1)
		doc.Find("a[href*='/product/']").Each(func(_ int, a *goquery.Selection) {
			href := strings.TrimSpace(a.AttrOr("href", ""))
			if href == "" || strings.Contains(href, "?q=") {
				return
			}
			if strings.Contains(href, "/product/") && !strings.Contains(href, "type=genre") {
				candidates = append(candidates, resolveURL(s.baseURL, href))
			}
		})
		if len(candidates) == 1 {
			found = candidates[0]
		}
	}

	if found == "" {
		return "", models.NewScraperNotFoundError("TokyoHot", fmt.Sprintf("movie %s not found on TokyoHot", id))
	}

	return s.applyLanguage(found), nil
}

// Search searches TokyoHot and extracts metadata.
func (s *Scraper) Search(id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("TokyoHot scraper is disabled")
	}

	detailURL, err := s.GetURL(id)
	if err != nil {
		return nil, err
	}

	html, status, err := s.fetchPage(detailURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch TokyoHot detail page: %w", err)
	}
	if status != 200 {
		return nil, models.NewScraperStatusError("TokyoHot", status, fmt.Sprintf("TokyoHot returned status code %d", status))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse TokyoHot detail page: %w", err)
	}

	return parseDetailPage(doc, detailURL, id, s.language), nil
}

func parseDetailPage(doc *goquery.Document, sourceURL, fallbackID, language string) *models.ScraperResult {
	result := &models.ScraperResult{
		Source:    "tokyohot",
		SourceURL: sourceURL,
		Language:  language,
	}

	title := cleanString(doc.Find("title").First().Text())
	if idx := strings.Index(title, "|"); idx > 0 {
		title = cleanString(title[:idx])
	}
	result.Title = title
	result.OriginalTitle = title

	if id := extractInfoDD(doc, []string{"Product ID", "品番", "商品番号"}); id != "" {
		result.ID = strings.ToUpper(extractID(id))
	}
	if result.ID == "" {
		result.ID = strings.ToUpper(extractID(sourceURL))
	}
	if result.ID == "" {
		result.ID = strings.ToUpper(strings.TrimSpace(fallbackID))
	}
	result.ContentID = result.ID

	if dateRaw := extractInfoDD(doc, []string{"配信開始日", "配信日", "Release", "販売日"}); dateRaw != "" {
		if m := dateRegex.FindStringSubmatch(dateRaw); len(m) > 1 {
			raw := strings.ReplaceAll(m[1], "/", "-")
			if t, err := time.Parse("2006-01-02", raw); err == nil {
				result.ReleaseDate = &t
			}
		}
	}

	if runtimeRaw := extractInfoDD(doc, []string{"収録時間", "再生時間", "Length", "Runtime"}); runtimeRaw != "" {
		if m := timeRuntimeRegex.FindStringSubmatch(runtimeRaw); len(m) == 4 {
			h, _ := strconv.Atoi(m[1])
			min, _ := strconv.Atoi(m[2])
			sec, _ := strconv.Atoi(m[3])
			result.Runtime = h*60 + min
			if sec >= 30 {
				result.Runtime += 1
			}
		} else if m := runtimeRegex.FindStringSubmatch(runtimeRaw); len(m) > 1 {
			if v, err := strconv.Atoi(m[1]); err == nil {
				result.Runtime = v
			}
		}
	}

	result.Maker = extractInfoLinkValue(doc, []string{"メーカー", "Maker", "Studio"})
	result.Series = extractInfoLinkValue(doc, []string{"シリーズ", "Series", "Genre"})
	result.Description = cleanString(doc.Find("div.sentence").First().Text())

	result.Actresses = extractActresses(doc)
	result.Genres = extractGenres(doc)

	result.CoverURL = extractCoverURL(doc, sourceURL)
	result.PosterURL = result.CoverURL
	result.ScreenshotURL = extractScreenshotURLs(doc, sourceURL)
	result.TrailerURL = extractTrailerURL(doc, sourceURL)
	result.ShouldCropPoster = true

	if result.Title == "" {
		result.Title = result.ID
		result.OriginalTitle = result.ID
	}

	return result
}

func extractInfoDD(doc *goquery.Document, labels []string) string {
	labelMatch := func(label string) bool {
		label = strings.ToLower(cleanString(strings.TrimSuffix(label, ":")))
		for _, needle := range labels {
			if strings.Contains(label, strings.ToLower(needle)) {
				return true
			}
		}
		return false
	}

	var value string
	doc.Find("dl.info").EachWithBreak(func(_ int, dl *goquery.Selection) bool {
		dts := dl.Find("dt")
		dts.EachWithBreak(func(i int, dt *goquery.Selection) bool {
			if !labelMatch(dt.Text()) {
				return true
			}
			dd := dl.Find("dd").Eq(i)
			value = cleanString(dd.Text())
			return false
		})
		return value == ""
	})
	return value
}

func extractInfoLinkValue(doc *goquery.Document, labels []string) string {
	labelMatch := func(label string) bool {
		label = strings.ToLower(cleanString(strings.TrimSuffix(label, ":")))
		for _, needle := range labels {
			if strings.Contains(label, strings.ToLower(needle)) {
				return true
			}
		}
		return false
	}

	var value string
	doc.Find("dl.info").EachWithBreak(func(_ int, dl *goquery.Selection) bool {
		dts := dl.Find("dt")
		dts.EachWithBreak(func(i int, dt *goquery.Selection) bool {
			if !labelMatch(dt.Text()) {
				return true
			}
			dd := dl.Find("dd").Eq(i)
			link := cleanString(dd.Find("a").First().Text())
			if link == "" {
				link = cleanString(dd.Text())
			}
			value = link
			return false
		})
		return value == ""
	})
	return value
}

func extractActresses(doc *goquery.Document) []models.ActressInfo {
	seen := map[string]bool{}
	out := make([]models.ActressInfo, 0)

	raw := extractInfoDD(doc, []string{"Model", "出演者", "女優"})
	if raw == "" {
		raw = cleanString(doc.Find("dl.info dd a[href*='actress'], dl.info dd a[href*='model']").First().Text())
	}

	if raw != "" {
		for _, name := range splitNames(raw) {
			name = cleanString(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			info := models.ActressInfo{}
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
			out = append(out, info)
		}
	}

	return out
}

func extractGenres(doc *goquery.Document) []string {
	seen := map[string]bool{}
	genres := make([]string, 0)

	raw := extractInfoDD(doc, []string{"Play", "プレイ内容", "玩法內容", "ジャンル", "Genre"})
	for _, g := range splitNames(raw) {
		g = cleanString(g)
		if g == "" || seen[g] {
			continue
		}
		seen[g] = true
		genres = append(genres, g)
	}

	doc.Find("dl.info a[href*='type=genre'], dl.info a[href*='genre']").Each(func(_ int, a *goquery.Selection) {
		g := cleanString(a.Text())
		if g == "" || seen[g] {
			return
		}
		seen[g] = true
		genres = append(genres, g)
	})

	return genres
}

func extractCoverURL(doc *goquery.Document, base string) string {
	patterns := []string{
		"img[src*='jacket']",
		"img[src*='list_image']",
		"video[poster]",
		"dl8-video[poster]",
		"meta[property='og:image']",
	}

	for _, sel := range patterns {
		node := doc.Find(sel).First()
		if node.Length() == 0 {
			continue
		}
		attr := "src"
		if strings.Contains(sel, "poster") {
			attr = "poster"
		}
		if strings.HasPrefix(sel, "meta") {
			attr = "content"
		}
		raw := cleanString(node.AttrOr(attr, ""))
		if raw != "" {
			return resolveURL(base, raw)
		}
	}

	return ""
}

func extractScreenshotURLs(doc *goquery.Document, base string) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	add := func(raw string) {
		raw = cleanString(raw)
		if raw == "" {
			return
		}
		u := resolveURL(base, raw)
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}

	doc.Find("div.scap a[href], a[rel='cap'][href]").Each(func(_ int, a *goquery.Selection) {
		add(a.AttrOr("href", ""))
	})
	doc.Find("img[src*='vcap'][src*='.jpg']").Each(func(_ int, img *goquery.Selection) {
		add(img.AttrOr("src", ""))
	})

	return out
}

func extractTrailerURL(doc *goquery.Document, base string) string {
	if src := cleanString(doc.Find("video source[src$='.mp4']").First().AttrOr("src", "")); src != "" {
		return resolveURL(base, src)
	}
	if src := cleanString(doc.Find("source[src$='.mp4']").First().AttrOr("src", "")); src != "" {
		return resolveURL(base, src)
	}
	return ""
}

func splitNames(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', '、', '/', '／', '|':
			return true
		default:
			return unicode.IsSpace(r)
		}
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = cleanString(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func normalizeLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "ja":
		return "ja"
	case "zh", "cn", "tw":
		return "zh"
	default:
		return "en"
	}
}

func (s *Scraper) applyLanguage(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	switch s.language {
	case "ja":
		q.Set("lang", "ja")
	case "zh":
		q.Set("lang", "zh-TW")
	default:
		q.Set("lang", "en")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *Scraper) fetchPage(targetURL string) (string, int, error) {
	s.waitForRateLimit()
	defer s.updateLastRequestTime()

	resp, err := s.client.R().Get(targetURL)
	if err != nil {
		return "", 0, err
	}
	html := resp.String()
	if resp.StatusCode() == 200 && models.IsCloudflareChallengePage(html) {
		return "", resp.StatusCode(), models.NewScraperChallengeError(
			"TokyoHot",
			"TokyoHot returned a Cloudflare challenge page (request blocked; adjust proxy/IP)",
		)
	}
	return html, resp.StatusCode(), nil
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

func normalizeID(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	return nonAlphaNumRegex.ReplaceAllString(v, "")
}

func extractID(v string) string {
	m := regexp.MustCompile(`([A-Za-z]+-?\d+[A-Za-z]?)`).FindStringSubmatch(v)
	if len(m) > 1 {
		return strings.ToUpper(strings.ReplaceAll(m[1], "_", "-"))
	}
	return ""
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
