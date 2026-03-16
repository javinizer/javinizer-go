package aventertainment

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

const defaultBaseURL = "https://www.aventertainments.com"

var (
	nonAlphaNumRegex = regexp.MustCompile(`[^a-z0-9]+`)
	tokenSplitRegex  = regexp.MustCompile(`[^\w-]+`)
	standardIDRegex  = regexp.MustCompile(`(?i)^([a-z]{2,12}[-_]\d{2,8}[a-z]?)$`)
	compactIDRegex   = regexp.MustCompile(`(?i)^([a-z]{2,12}\d{2,8}[a-z]?)$`)
	onePondoRegex    = regexp.MustCompile(`(?i)^1pon[_-](\d{6})[_-](\d{3})$`)
	onePondoDateIDRe = regexp.MustCompile(`(?i)(?:^|[^0-9])(\d{6})[_-](\d{3})(?:[^0-9]|$)`)
	caribRegex       = regexp.MustCompile(`(?i)^carib(?:bean)?[_-](\d{6})[_-](\d{3})$`)
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
	scrapeBonus     bool
	proxyOverride   *config.ProxyConfig
	downloadProxy   *config.ProxyConfig
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
		client:        client,
		enabled:       scraperCfg.Enabled,
		baseURL:       base,
		language:      normalizeLanguage(scraperCfg.Language),
		requestDelay:  time.Duration(scraperCfg.RequestDelay) * time.Millisecond,
		scrapeBonus:   scraperCfg.ScrapeBonusScreens,
		proxyOverride: scraperCfg.Proxy,
		downloadProxy: scraperCfg.DownloadProxy,
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

// ResolveDownloadProxyForHost declares AVEntertainment-owned media hosts for downloader proxy routing.
func (s *Scraper) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || !strings.HasSuffix(host, "aventertainments.com") {
		return nil, nil, false
	}
	return s.downloadProxy, s.proxyOverride, true
}

// ResolveSearchQuery maps non-standard filename IDs to AVEntertainment-friendly
// query formats.
func (s *Scraper) ResolveSearchQuery(input string) (string, bool) {
	norm := normalizeResolverInput(input)
	if norm == "" {
		return "", false
	}

	// OnePondo style IDs (example: 1pon_020326_001)
	if m := onePondoRegex.FindStringSubmatch(norm); len(m) == 3 {
		return "1pon_" + m[1] + "_" + m[2], true
	}

	// Caribbeancom style IDs (example: carib_020326_001)
	if m := caribRegex.FindStringSubmatch(norm); len(m) == 3 {
		return "carib_" + m[1] + "_" + m[2], true
	}

	// Flexible 1Pondo/Caribbean style date IDs embedded in filenames
	// (examples: 050419_844-1pon-1080p, 021226_001-carib-720p).
	if m := onePondoDateIDRe.FindStringSubmatch(norm); len(m) == 3 {
		if strings.Contains(norm, "carib") {
			return "carib_" + m[1] + "_" + m[2], true
		}
		if strings.Contains(norm, "1pon") || strings.Contains(norm, "1pondo") {
			return "1pon_" + m[1] + "_" + m[2], true
		}
		// Default to 1Pondo for bare YYMMDD_NNN tokens so files like
		// "050419_844.mp4" can still be resolved by AVEntertainment.
		return "1pon_" + m[1] + "_" + m[2], true
	}

	return "", false
}

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
		fmt.Sprintf("/ppv/search?keyword=%s&searchby=keyword", url.QueryEscape(id)),
		fmt.Sprintf("/ppv/search?keyword=%s", url.QueryEscape(id)),
	}

	candidateSet := map[string]struct{}{}
	candidateOrder := make([]string, 0, 8)
	for _, endpoint := range searchEndpoints {
		searchURL := s.applyLanguage(s.baseURL + endpoint)
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
		return "", models.NewScraperNotFoundError("AVEntertainment", fmt.Sprintf("movie %s not found on AVEntertainment", id))
	}

	target := normalizeComparableID(id)
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
		candidateNorm := normalizeComparableID(candidateID)
		if candidateNorm != "" && (candidateNorm == target || strings.HasSuffix(candidateNorm, target) || strings.HasSuffix(target, candidateNorm)) {
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
		return nil, models.NewScraperStatusError("AVEntertainment", status, fmt.Sprintf("AVEntertainment returned status code %d", status))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse AVEntertainment detail page: %w", err)
	}

	return parseDetailPage(doc, html, detailURL, id, s.language, s.scrapeBonus), nil
}

func parseDetailPage(doc *goquery.Document, html, sourceURL, fallbackID, language string, scrapeBonus bool) *models.ScraperResult {
	result := &models.ScraperResult{
		Source:    "aventertainment",
		SourceURL: sourceURL,
		Language:  language,
	}

	detail := extractDetailInfo(doc)

	id := extractID(detail.ProductID)
	if id == "" {
		id = extractID(cleanString(doc.Find("span.tag-title").First().Text()))
	}
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

	title := cleanString(detail.Title)
	if title == "" {
		title = cleanString(doc.Find(".section-title h1, .section-title h2, .section-title h3").First().Text())
	}
	if title == "" {
		title = cleanString(doc.Find("title").First().Text())
	}
	if title == "" {
		title = cleanString(doc.Find("meta[property='og:title']").AttrOr("content", ""))
	}
	result.Title = stripSiteSuffix(title)
	result.OriginalTitle = result.Title

	dateRaw := cleanString(detail.ReleaseDateRaw)
	if dateRaw == "" {
		dateRaw = findDate(html)
	}
	if dateRaw != "" {
		if t := parseDate(dateRaw); t != nil {
			result.ReleaseDate = t
		}
	}

	runtimeRaw := cleanString(detail.RuntimeRaw)
	if runtimeRaw == "" {
		runtimeRaw = findRuntime(html)
	}
	if runtimeRaw != "" {
		result.Runtime = parseRuntime(runtimeRaw)
	}

	result.Maker = cleanString(detail.Studio)
	if result.Maker == "" {
		result.Maker = cleanString(findMaker(html))
	}
	result.Description = extractDescription(doc)
	result.Genres = detail.Categories
	if len(result.Genres) == 0 {
		result.Genres = extractGenres(doc.Selection)
	}
	result.Actresses = detail.Actresses
	if len(result.Actresses) == 0 {
		result.Actresses = extractActresses(doc.Selection)
	}

	posterURL := extractPosterURL(doc, html, sourceURL)
	if posterURL == "" {
		posterURL = extractCoverURL(doc, html, sourceURL)
	}

	result.PosterURL = posterURL
	// AVEntertainment cover/fanart should use the same original source image
	// used before poster cropping.
	result.CoverURL = posterURL
	result.ScreenshotURL = extractScreenshotURLs(doc, html, sourceURL, scrapeBonus)
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
		if !strings.Contains(href, "/ppv/detail") && !strings.Contains(href, "new_detail") && !strings.Contains(href, "product_lists") {
			return
		}
		full := resolveURL(base, href)
		if _, ok := set[full]; ok {
			return
		}
		set[full] = struct{}{}
		out = append(out, full)
	})

	return out
}

func extractCandidateID(html string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?is)<span class="tag-title">\s*([^<]+?)\s*</span>`),
		regexp.MustCompile(`(?is)item_no=([A-Za-z0-9_-]+)`),
		regexp.MustCompile(`(?is)vodimages/(?:x?large|screenshot/large|gallery/large)/([A-Za-z0-9_-]+)`),
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
		regexp.MustCompile(`(?is)(?:発売日|配信日|release(?:\s*date)?)\s*</span>\s*<span class="value">\s*([^<]+)`),
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
	if token := dateRegex.FindString(raw); token != "" {
		raw = token
	}
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
		regexp.MustCompile(`(?is)(?:収録時間|再生時間|runtime|running\s*time)\s*</span>\s*<span class="value">\s*([^<]+)`),
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
		regexp.MustCompile(`(?is)<span class="title">\s*Studio\s*</span>\s*<span class="value">\s*<a[^>]*>([^<]+)</a>`),
		regexp.MustCompile(`(?is)<span class="title">\s*スタジオ\s*</span>\s*<span class="value">\s*<a[^>]*>([^<]+)</a>`),
		regexp.MustCompile(`(?is)/ppv/studio\?[^"']*["'][^>]*>\s*([^<]+)\s*</a>`),
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
	if block := doc.Find(".product-description").First(); block.Length() > 0 {
		clone := block.Clone()
		clone.Find("script,style").Remove()
		clone.Find("a[data-toggle='collapse'], a[data-target], .text-black a").Each(func(_ int, a *goquery.Selection) {
			a.Remove()
		})
		if v := cleanString(clone.Text()); v != "" {
			return v
		}
	}

	for _, sel := range []string{".product-detail .description", ".value-description", "meta[name='description']"} {
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

func extractGenres(scope *goquery.Selection) []string {
	if scope == nil {
		return nil
	}

	seen := map[string]bool{}
	genres := make([]string, 0)

	scope.Find(".value-category a, a[href*='cat_id'], a[href*='dept']").Each(func(_ int, a *goquery.Selection) {
		v := cleanString(a.Text())
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		genres = append(genres, v)
	})

	return genres
}

type detailInfo struct {
	Title          string
	ProductID      string
	Studio         string
	ReleaseDateRaw string
	RuntimeRaw     string
	Categories     []string
	Actresses      []models.ActressInfo
}

func extractDetailInfo(doc *goquery.Document) detailInfo {
	info := detailInfo{
		Title: cleanString(doc.Find(".section-title h1, .section-title h2, .section-title h3").First().Text()),
	}

	doc.Find(".product-info-block-rev .single-info").Each(func(_ int, row *goquery.Selection) {
		label := normalizeInfoLabel(row.Find(".title").First().Text())
		value := cleanString(row.Find(".value").First().Text())

		switch {
		case isProductIDLabel(label):
			if info.ProductID == "" {
				if tagID := cleanString(row.Find(".tag-title").First().Text()); tagID != "" {
					info.ProductID = tagID
				} else if value != "" {
					info.ProductID = value
				}
			}
		case isActressLabel(label):
			if len(info.Actresses) == 0 {
				info.Actresses = extractActresses(row)
			}
		case isStudioLabel(label):
			if info.Studio == "" {
				info.Studio = cleanString(row.Find(".value a, .value").First().Text())
			}
		case isCategoryLabel(label):
			if len(info.Categories) == 0 {
				info.Categories = extractGenres(row)
			}
		case isReleaseDateLabel(label):
			if info.ReleaseDateRaw == "" {
				info.ReleaseDateRaw = value
			}
		case isRuntimeLabel(label):
			if info.RuntimeRaw == "" {
				info.RuntimeRaw = value
			}
		}
	})

	return info
}

func normalizeInfoLabel(v string) string {
	v = strings.ToLower(cleanString(v))
	replacer := strings.NewReplacer(" ", "", "\u3000", "", ":", "", "：", "", "-", "", "_", "", "#", "")
	return replacer.Replace(v)
}

func isProductIDLabel(label string) bool {
	return strings.Contains(label, "商品番号") ||
		strings.Contains(label, "品番") ||
		strings.Contains(label, "productid") ||
		strings.Contains(label, "itemno") ||
		strings.Contains(label, "item#") ||
		label == "item"
}

func isActressLabel(label string) bool {
	return strings.Contains(label, "主演女優") ||
		strings.Contains(label, "女優") ||
		strings.Contains(label, "actress") ||
		strings.Contains(label, "starring")
}

func isStudioLabel(label string) bool {
	return strings.Contains(label, "スタジオ") || strings.Contains(label, "studio")
}

func isCategoryLabel(label string) bool {
	return strings.Contains(label, "カテゴリ") || strings.Contains(label, "category") || strings.Contains(label, "categories")
}

func isReleaseDateLabel(label string) bool {
	return strings.Contains(label, "発売日") ||
		strings.Contains(label, "配信日") ||
		strings.Contains(label, "releasedate") ||
		strings.Contains(label, "release") ||
		label == "date"
}

func isRuntimeLabel(label string) bool {
	return strings.Contains(label, "収録時間") ||
		strings.Contains(label, "再生時間") ||
		strings.Contains(label, "runtime") ||
		strings.Contains(label, "runningtime") ||
		strings.Contains(label, "playtime") ||
		strings.Contains(label, "length")
}

func extractActresses(scope *goquery.Selection) []models.ActressInfo {
	if scope == nil {
		return nil
	}

	seen := map[string]bool{}
	out := make([]models.ActressInfo, 0)
	scope.Find("a[href*='ppv_actressdetail'], a[href*='ppv_ActressDetail'], a[href*='/ppv/idoldetail']").Each(func(_ int, a *goquery.Selection) {
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

func extractPosterURL(doc *goquery.Document, html, base string) string {
	if v := cleanString(doc.Find("#PlayerCover img").First().AttrOr("src", "")); v != "" {
		return resolveURL(base, v)
	}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?is)vodimages/xlarge/[A-Za-z0-9._-]+\.(?:jpg|webp)`),
		regexp.MustCompile(`(?is)<meta property="og:image" content="([^"]+)"`),
	} {
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return resolveURL(base, cleanString(m[1]))
		}
		if m := re.FindString(html); m != "" {
			return resolveURL(base, cleanString(m))
		}
	}
	return ""
}

func extractCoverURL(doc *goquery.Document, html, base string) string {
	if v := cleanString(doc.Find("a.lightbox[href*='/vodimages/gallery/large/']").First().AttrOr("href", "")); v != "" {
		return resolveURL(base, v)
	}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?is)class='lightbox'\s+href='([^']+/vodimages/gallery/large/[^']+\.(?:jpg|webp))'`),
		regexp.MustCompile(`(?is)vodimages/gallery/large/[A-Za-z0-9._/-]+\.(?:jpg|webp)`),
		regexp.MustCompile(`(?is)vodimages/xlarge/[A-Za-z0-9._/-]+\.(?:jpg|webp)`),
		regexp.MustCompile(`(?is)<meta property="og:image" content="([^"]+)"`),
	} {
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return resolveURL(base, cleanString(m[1]))
		}
		if m := re.FindString(html); m != "" {
			return resolveURL(base, cleanString(m))
		}
	}
	return ""
}

func extractScreenshotURLs(doc *goquery.Document, html, base string, scrapeBonus bool) []string {
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

	if scrapeBonus {
		doc.Find("a[href], img[src], img[data-src], img[data-original]").Each(func(_ int, sel *goquery.Selection) {
			for _, attr := range []string{"href", "src", "data-src", "data-original"} {
				raw := cleanString(sel.AttrOr(attr, ""))
				if !isAVEBonusScreenshotURL(raw) {
					continue
				}
				add(raw)
			}
		})

		// Some bonus images are injected in page scripts and not present as DOM nodes.
		re := regexp.MustCompile(`(?is)(?:href|src|data-src|data-original)=['"]([^'"]+/vodimages/gallery/large/[A-Za-z0-9_-]+/\d{2,4}\.(?:jpg|jpeg|png|webp))['"]`)
		for _, m := range re.FindAllStringSubmatch(html, -1) {
			if len(m) > 1 && isAVEBonusScreenshotURL(m[1]) {
				add(m[1])
			}
		}
	}

	return out
}

func isAVEBonusScreenshotURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}

	path := raw
	if parsed, err := url.Parse(raw); err == nil && parsed.Path != "" {
		path = parsed.Path
	}

	path = strings.ToLower(path)
	// Bonus screenshots are gallery "extra file" images with numbered file names:
	// /vodimages/gallery/large/<content_id>/<nnn>.webp
	re := regexp.MustCompile(`(?i)/vodimages/gallery/large/[a-z0-9_-]+/\d{2,4}\.(jpg|jpeg|png|webp)$`)
	return re.MatchString(path)
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
	if s.language == "ja" {
		q.Set("lang", "2")
		q.Set("culture", "ja-JP")
	} else {
		q.Set("lang", "1")
		q.Set("culture", "en-US")
	}
	if !q.Has("v") {
		q.Set("v", "1")
	}
	if q.Has("languageID") {
		if s.language == "ja" {
			q.Set("languageID", "2")
		} else {
			q.Set("languageID", "1")
		}
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
			"AVEntertainment",
			"AVEntertainment returned a Cloudflare challenge page (request blocked; adjust proxy/IP)",
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

func normalizeComparableID(v string) string {
	v = normalizeID(v)
	for _, prefix := range []string{"dl", "st"} {
		if strings.HasPrefix(v, prefix) {
			v = strings.TrimPrefix(v, prefix)
			break
		}
	}
	return v
}

func extractID(v string) string {
	normalizeToken := func(token string) string {
		token = strings.TrimSpace(strings.ToLower(token))
		token = strings.Trim(token, "[](){}<>\"'`.,;:/\\?&=#")
		token = strings.Trim(token, "_-")
		if token == "" {
			return ""
		}
		for _, prefix := range []string{"dl", "st"} {
			if strings.HasPrefix(token, prefix) {
				tail := strings.Trim(token[len(prefix):], "_-")
				if tail != "" {
					token = tail
				}
				break
			}
		}
		return token
	}

	raw := normalizeToken(v)
	if raw != "" {
		if m := onePondoRegex.FindStringSubmatch(raw); len(m) == 3 {
			return strings.ToUpper("1pon-" + m[1] + "-" + m[2])
		}
		if m := caribRegex.FindStringSubmatch(raw); len(m) == 3 {
			return strings.ToUpper("carib-" + m[1] + "-" + m[2])
		}
		if m := standardIDRegex.FindStringSubmatch(raw); len(m) > 1 {
			return strings.ToUpper(strings.ReplaceAll(m[1], "_", "-"))
		}
		if m := compactIDRegex.FindStringSubmatch(raw); len(m) > 1 {
			return strings.ToUpper(m[1])
		}
	}

	for _, token := range tokenSplitRegex.Split(v, -1) {
		token = normalizeToken(token)
		if token == "" {
			continue
		}
		if m := onePondoRegex.FindStringSubmatch(token); len(m) == 3 {
			return strings.ToUpper("1pon-" + m[1] + "-" + m[2])
		}
		if m := caribRegex.FindStringSubmatch(token); len(m) == 3 {
			return strings.ToUpper("carib-" + m[1] + "-" + m[2])
		}
		if m := standardIDRegex.FindStringSubmatch(token); len(m) > 1 {
			return strings.ToUpper(strings.ReplaceAll(m[1], "_", "-"))
		}
		if m := compactIDRegex.FindStringSubmatch(token); len(m) > 1 {
			return strings.ToUpper(m[1])
		}
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
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	resolved := baseURL.ResolveReference(ref)
	if resolved == nil {
		return raw
	}
	if resolved.Path == "" {
		resolved.Path = path.Join(path.Dir(baseURL.Path), raw)
	}
	return resolved.String()
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
	for _, suffix := range []string{
		" - AV Entertainment",
		" | AV Entertainment",
		" - AVEntertainment",
		" | AV ENTERTAINMENT PAY-PER-VIEW",
		" | AVエンターテインメント ペイパービュー",
		" | AVエンターテインメント",
	} {
		if len(v) >= len(suffix) && strings.EqualFold(v[len(v)-len(suffix):], suffix) {
			v = strings.TrimSpace(v[:len(v)-len(suffix)])
		}
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

func normalizeResolverInput(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	// Allow passing full paths/filenames; resolver operates on basename without extension.
	input = strings.ReplaceAll(input, "\\", "/")
	base := path.Base(input)
	ext := path.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}

	return strings.ToLower(strings.TrimSpace(base))
}
