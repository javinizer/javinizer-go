package aventertainment

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
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

const defaultBaseURL = "https://www.aventertainments.com"

var (
	nonAlphaNumRegex = regexp.MustCompile(`[^a-z0-9]+`)
	idRegex          = regexp.MustCompile(`([A-Za-z]+[_-]?\d+[A-Za-z]?)`)
	runtimeClockRe   = regexp.MustCompile(`(\d{1,2}):(\d{2})(?::\d{2})?`)
	runtimeMinuteRe  = regexp.MustCompile(`(?i)(\d{1,3})\s*(?:min|minutes|分)`)
	dateRegex        = regexp.MustCompile(`(\d{1,2}/\d{1,2}/\d{4}|\d{4}-\d{2}-\d{2}|\d{4}/\d{2}/\d{2})`)
)

// Scraper implements the AVEntertainment scraper.
type Scraper struct {
	client          *resty.Client
	enabled         bool
	baseURL         string
	language        string
	requestDelay    time.Duration
	lastRequestTime atomic.Value
}

// New creates a new AVEntertainment scraper.
func New(cfg *config.Config) *Scraper {
	scraperCfg := cfg.Scrapers.AVEntertainment
	proxyCfg := config.ResolveScraperProxy(cfg.Scrapers.Proxy, scraperCfg.Proxy)

	client, err := httpclient.NewRestyClient(proxyCfg, 30*time.Second, 3)
	usingProxy := err == nil && proxyCfg.Enabled && strings.TrimSpace(proxyCfg.URL) != ""
	if err != nil {
		logging.Errorf("AVEntertainment: Failed to create HTTP client with proxy: %v, using explicit no-proxy fallback", err)
		client = httpclient.NewRestyClientNoProxy(30*time.Second, 3)
	}

	userAgent := config.ResolveScraperUserAgent(
		cfg.Scrapers.UserAgent,
		scraperCfg.UseFakeUserAgent,
		scraperCfg.FakeUserAgent,
	)
	client.SetHeader("User-Agent", userAgent)
	client.SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	client.SetHeader("Accept-Language", "en-US,en;q=0.9,ja;q=0.8")
	client.SetHeader("Connection", "keep-alive")
	client.SetHeader("Upgrade-Insecure-Requests", "1")

	base := strings.TrimSpace(scraperCfg.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")

	s := &Scraper{
		client:       client,
		enabled:      scraperCfg.Enabled,
		baseURL:      base,
		language:     normalizeLanguage(scraperCfg.Language),
		requestDelay: time.Duration(scraperCfg.RequestDelay) * time.Millisecond,
	}
	s.lastRequestTime.Store(time.Time{})

	if usingProxy {
		logging.Infof("AVEntertainment: Using proxy %s", httpclient.SanitizeProxyURL(proxyCfg.URL))
	}

	return s
}

// Name returns scraper identifier.
func (s *Scraper) Name() string { return "aventertainment" }

// IsEnabled returns whether scraper is enabled.
func (s *Scraper) IsEnabled() bool { return s.enabled }

// GetURL resolves a detail page URL from movie ID.
func (s *Scraper) GetURL(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("movie ID cannot be empty")
	}
	if isHTTPURL(id) {
		return s.applyLanguage(id), nil
	}

	searchEndpoints := []string{
		fmt.Sprintf("/ppv/ppv_searchproducts.aspx?languageID=1&vodtypeid=1&keyword=%s", url.QueryEscape(id)),
		fmt.Sprintf("/ppv/ppv_searchproducts.aspx?languageID=1&vodtypeid=2&keyword=%s", url.QueryEscape(id)),
		fmt.Sprintf("/search_Products.aspx?languageID=1&dept_id=29&keyword=%s&searchby=keyword", url.QueryEscape(id)),
		fmt.Sprintf("/search_Products.aspx?languageID=1&dept_id=43&keyword=%s&searchby=keyword", url.QueryEscape(id)),
	}

	candidateSet := map[string]struct{}{}
	candidateOrder := make([]string, 0, 8)
	for _, endpoint := range searchEndpoints {
		searchURL := s.baseURL + endpoint
		html, status, err := s.fetchPage(searchURL)
		if err != nil || status != 200 {
			continue
		}
		links := extractDetailLinks(html, s.baseURL)
		for _, link := range links {
			if _, exists := candidateSet[link]; exists {
				continue
			}
			candidateSet[link] = struct{}{}
			candidateOrder = append(candidateOrder, link)
		}
	}

	if len(candidateOrder) == 0 {
		return "", fmt.Errorf("movie %s not found on AVEntertainment", id)
	}

	target := normalizeID(id)
	maxInspect := len(candidateOrder)
	if maxInspect > 12 {
		maxInspect = 12
	}

	for i := 0; i < maxInspect; i++ {
		candidate := candidateOrder[i]
		html, status, err := s.fetchPage(candidate)
		if err != nil || status != 200 {
			continue
		}
		candidateID := extractCandidateID(html)
		if candidateID == "" {
			candidateID = extractID(candidate)
		}
		if candidateID != "" && (normalizeID(candidateID) == target || strings.HasSuffix(normalizeID(candidateID), target)) {
			return s.applyLanguage(candidate), nil
		}
	}

	// Fallback: choose first candidate if exact ID wasn't parsed.
	return s.applyLanguage(candidateOrder[0]), nil
}

// Search scrapes metadata for an ID.
func (s *Scraper) Search(id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("AVEntertainment scraper is disabled")
	}

	detailURL, err := s.GetURL(id)
	if err != nil {
		return nil, err
	}

	html, status, err := s.fetchPage(detailURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch AVEntertainment detail page: %w", err)
	}
	if status != 200 {
		return nil, fmt.Errorf("AVEntertainment returned status code %d", status)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse AVEntertainment detail page: %w", err)
	}

	return parseDetailPage(doc, html, detailURL, id, s.language), nil
}

func parseDetailPage(doc *goquery.Document, html, sourceURL, fallbackID, language string) *models.ScraperResult {
	result := &models.ScraperResult{
		Source:    "aventertainment",
		SourceURL: sourceURL,
		Language:  language,
	}

	id := cleanString(doc.Find("span.tag-title").First().Text())
	if id == "" {
		id = extractCandidateID(html)
	}
	if id == "" {
		id = extractID(sourceURL)
	}
	if id == "" {
		id = strings.TrimSpace(fallbackID)
	}
	result.ID = strings.ToUpper(strings.ReplaceAll(id, "_", "-"))
	result.ContentID = result.ID

	title := cleanString(doc.Find("title").First().Text())
	if title == "" {
		title = cleanString(doc.Find("meta[property='og:title']").AttrOr("content", ""))
	}
	result.Title = stripSiteSuffix(title)
	result.OriginalTitle = result.Title

	if dateRaw := findDate(html); dateRaw != "" {
		if t := parseDate(dateRaw); t != nil {
			result.ReleaseDate = t
		}
	}

	if runtimeRaw := findRuntime(html); runtimeRaw != "" {
		result.Runtime = parseRuntime(runtimeRaw)
	}

	result.Maker = cleanString(findMaker(html))
	result.Description = extractDescription(doc)
	result.Genres = extractGenres(doc)
	result.Actresses = extractActresses(doc)

	result.CoverURL = extractCoverURL(doc, html, sourceURL)
	result.PosterURL = result.CoverURL
	result.ScreenshotURL = extractScreenshotURLs(doc, html, sourceURL)
	result.ShouldCropPoster = true

	if result.Title == "" {
		result.Title = result.ID
		result.OriginalTitle = result.ID
	}

	return result
}

func extractDetailLinks(html, base string) []string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}

	set := map[string]struct{}{}
	out := make([]string, 0, 8)
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href := strings.TrimSpace(a.AttrOr("href", ""))
		if href == "" {
			return
		}
		if !(strings.Contains(href, "new_detail") || strings.Contains(href, "product_lists")) {
			return
		}
		full := resolveURL(base, href)
		if _, ok := set[full]; ok {
			return
		}
		set[full] = struct{}{}
		out = append(out, full)
	})

	sort.Strings(out)
	return out
}

func extractCandidateID(html string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?is)<span class="tag-title">\s*([^<]+?)\s*</span>`),
		regexp.MustCompile(`(?is)(?:Product\s*ID|品番|品號|识别码|識別碼)\s*[:：]?\s*([A-Za-z0-9_-]+)`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			if id := extractID(cleanString(m[1])); id != "" {
				return id
			}
		}
	}
	return ""
}

func findDate(html string) string {
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?is)<span class="value">\s*(\d{1,2}/\d{1,2}/\d{4}|\d{4}/\d{2}/\d{2}|\d{4}-\d{2}-\d{2})`),
		regexp.MustCompile(`(?is)(\d{1,2}/\d{1,2}/\d{4}|\d{4}/\d{2}/\d{2}|\d{4}-\d{2}-\d{2})`),
	} {
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

func parseDate(raw string) *time.Time {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "/", "-"))
	for _, f := range []string{"2006-01-02", "01-02-2006"} {
		if t, err := time.Parse(f, raw); err == nil {
			return &t
		}
	}
	return nil
}

func findRuntime(html string) string {
	for _, re := range []*regexp.Regexp{
		runtimeClockRe,
		runtimeMinuteRe,
		regexp.MustCompile(`(?is)Apx\.?\s*(\d{1,3})\s*Min`),
	} {
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return m[0]
		}
	}
	return ""
}

func parseRuntime(raw string) int {
	raw = cleanString(raw)
	if m := runtimeClockRe.FindStringSubmatch(raw); len(m) >= 3 {
		h, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		return h*60 + min
	}
	if m := runtimeMinuteRe.FindStringSubmatch(raw); len(m) >= 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			return v
		}
	}
	if m := regexp.MustCompile(`(\d{1,3})`).FindStringSubmatch(raw); len(m) >= 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			return v
		}
	}
	return 0
}

func findMaker(html string) string {
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?is)studio_products\.aspx\?StudioID=.*?>([^<]+)</a>`),
		regexp.MustCompile(`(?is)ppv_studioproducts.*?>([^<]+)</a>`),
	} {
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

func extractDescription(doc *goquery.Document) string {
	for _, sel := range []string{"meta[name='description']", ".product-detail .description", ".value-description"} {
		node := doc.Find(sel).First()
		if node.Length() == 0 {
			continue
		}
		if strings.HasPrefix(sel, "meta") {
			if v := cleanString(node.AttrOr("content", "")); v != "" {
				return v
			}
			continue
		}
		if v := cleanString(node.Text()); v != "" {
			return v
		}
	}
	return ""
}

func extractGenres(doc *goquery.Document) []string {
	seen := map[string]bool{}
	genres := make([]string, 0)

	doc.Find(".value-category a, a[href*='cat_id'], a[href*='dept']").Each(func(_ int, a *goquery.Selection) {
		v := cleanString(a.Text())
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		genres = append(genres, v)
	})

	return genres
}

func extractActresses(doc *goquery.Document) []models.ActressInfo {
	seen := map[string]bool{}
	out := make([]models.ActressInfo, 0)
	doc.Find("a[href*='ppv_actressdetail'], a[href*='ppv_ActressDetail']").Each(func(_ int, a *goquery.Selection) {
		name := cleanString(a.Text())
		if name == "" || seen[name] {
			return
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
	})
	return out
}

func extractCoverURL(doc *goquery.Document, html, base string) string {
	if v := cleanString(doc.Find("#PlayerCover img").First().AttrOr("src", "")); v != "" {
		return resolveURL(base, v)
	}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?is)class='lightbox'\s+href='([^']+/vodimages/gallery/large/[^']+\.(?:jpg|webp))'`),
		regexp.MustCompile(`(?is)<meta property="og:image" content="([^"]+)"`),
	} {
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return resolveURL(base, cleanString(m[1]))
		}
	}
	return ""
}

func extractScreenshotURLs(doc *goquery.Document, html, base string) []string {
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

	doc.Find("a.lightbox[href]").Each(func(_ int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		if strings.Contains(href, "/vodimages/screenshot/") {
			add(href)
		}
	})

	if len(out) == 0 {
		re := regexp.MustCompile(`(?is)href='([^']+/vodimages/screenshot/large/[^']+\.(?:jpg|webp))'`)
		for _, m := range re.FindAllStringSubmatch(html, -1) {
			if len(m) > 1 {
				add(m[1])
			}
		}
	}

	return out
}

func (s *Scraper) applyLanguage(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	if strings.Contains(u.Path, "/ppv/") {
		if s.language == "ja" {
			u.Path = regexp.MustCompile(`/ppv/(\d+)/1/1/new_detail`).ReplaceAllString(u.Path, `/ppv/$1/2/1/new_detail`)
		} else {
			u.Path = regexp.MustCompile(`/ppv/(\d+)/2/1/new_detail`).ReplaceAllString(u.Path, `/ppv/$1/1/1/new_detail`)
		}
	}

	q := u.Query()
	if q.Has("languageID") {
		if s.language == "ja" {
			q.Set("languageID", "2")
		} else {
			q.Set("languageID", "1")
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
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
	if strings.ToLower(strings.TrimSpace(lang)) == "ja" {
		return "ja"
	}
	return "en"
}

func normalizeID(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	return nonAlphaNumRegex.ReplaceAllString(v, "")
}

func extractID(v string) string {
	if m := idRegex.FindStringSubmatch(v); len(m) > 1 {
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

func stripSiteSuffix(v string) string {
	v = cleanString(v)
	for _, suffix := range []string{" - AV Entertainment", " | AV Entertainment", " - AVEntertainment"} {
		v = strings.TrimSuffix(v, suffix)
	}
	return cleanString(v)
}

func isHTTPURL(v string) bool {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
