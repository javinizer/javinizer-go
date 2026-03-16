package caribbeancom

import (
	"fmt"
	"net/url"
	"path"
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
	"golang.org/x/net/html/charset"
)

const defaultBaseURL = "https://www.caribbeancom.com"

var (
	movieIDTokenRegex  = regexp.MustCompile(`(?i)(?:^|[^0-9])(\d{6})[-_](\d{2,3})(?:[^0-9]|$)`)
	movieIDFromPageRe  = regexp.MustCompile(`(?i)/moviepages/(\d{6}[-_]\d{3})/`)
	movieIDFromJSONRe  = regexp.MustCompile(`(?i)"movie_id"\s*:\s*"(\d{6}-\d{3})"`)
	trailerURLJSONRe   = regexp.MustCompile(`(?i)"sample_flash_url"\s*:\s*"([^"]+)"`)
	trailerURLAssignRe = regexp.MustCompile(`(?i)sample_flash_url\s*=\s*['"]([^'"]+)['"]`)
	coverImagePathRe   = regexp.MustCompile(`(?i)(/moviepages/\d{6}-\d{3}/images/l(?:_l)?\.jpg)`)
	runtimeISORegex    = regexp.MustCompile(`(?i)T(?:(\d{1,2})H)?(?:(\d{1,2})M)?(?:(\d{1,2})S)?`)
	runtimeClockRegex  = regexp.MustCompile(`(\d{1,2}):(\d{2})(?::(\d{2}))?`)
	runtimeMinuteRegex = regexp.MustCompile(`(?i)(\d{1,3})\s*(?:min|minutes|分)`)
	dateYMDRegex       = regexp.MustCompile(`(\d{4}[/-]\d{1,2}[/-]\d{1,2}|\d{1,2}[/-]\d{1,2}[/-]\d{4})`)
)

// Scraper implements the Caribbeancom scraper.
type Scraper struct {
	client          *resty.Client
	enabled         bool
	baseURL         string
	language        string
	requestDelay    time.Duration
	proxyOverride   *config.ProxyConfig
	downloadProxy   *config.ProxyConfig
	lastRequestTime atomic.Value
}

// New creates a new Caribbeancom scraper.
func New(cfg *config.Config) *Scraper {
	scraperCfg := cfg.Scrapers.Caribbeancom
	proxyCfg := config.ResolveScraperProxy(cfg.Scrapers.Proxy, scraperCfg.Proxy)

	client, err := httpclient.NewRestyClient(proxyCfg, 30*time.Second, 3)
	usingProxy := err == nil && proxyCfg.Enabled && strings.TrimSpace(proxyCfg.URL) != ""
	if err != nil {
		logging.Errorf("Caribbeancom: Failed to create HTTP client with proxy: %v, using explicit no-proxy fallback", err)
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

	lang := normalizeLanguage(scraperCfg.Language)

	s := &Scraper{
		client:        client,
		enabled:       scraperCfg.Enabled,
		baseURL:       base,
		language:      lang,
		requestDelay:  time.Duration(scraperCfg.RequestDelay) * time.Millisecond,
		proxyOverride: scraperCfg.Proxy,
		downloadProxy: scraperCfg.DownloadProxy,
	}
	s.lastRequestTime.Store(time.Time{})

	if usingProxy {
		logging.Infof("Caribbeancom: Using proxy %s", httpclient.SanitizeProxyURL(proxyCfg.URL))
	}

	return s
}

// Name returns scraper identifier.
func (s *Scraper) Name() string { return "caribbeancom" }

// IsEnabled returns whether scraper is enabled.
func (s *Scraper) IsEnabled() bool { return s.enabled }

// ResolveDownloadProxyForHost declares Caribbeancom-owned media hosts for downloader proxy routing.
func (s *Scraper) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || !strings.HasSuffix(host, "caribbeancom.com") {
		return nil, nil, false
	}
	return s.downloadProxy, s.proxyOverride, true
}

// ResolveSearchQuery normalizes Caribbeancom-style IDs from free-form input.
func (s *Scraper) ResolveSearchQuery(input string) (string, bool) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return "", false
	}

	if m := movieIDFromPageRe.FindStringSubmatch(input); len(m) > 1 {
		return normalizeMovieID(m[1]), true
	}
	if m := movieIDTokenRegex.FindStringSubmatch(input); len(m) == 3 {
		return normalizeMovieID(m[1] + "-" + m[2]), true
	}
	return "", false
}

// GetURL resolves detail URL for an ID.
func (s *Scraper) GetURL(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("movie ID cannot be empty")
	}
	if isHTTPURL(id) {
		return s.applyLanguage(id), nil
	}

	if normalized, ok := s.ResolveSearchQuery(id); ok {
		return s.buildMoviePageURL(normalized), nil
	}

	return "", models.NewScraperNotFoundError(
		"Caribbeancom",
		fmt.Sprintf("movie %s does not match Caribbeancom ID format", id),
	)
}

// Search scrapes metadata from Caribbeancom.
func (s *Scraper) Search(id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("caribbeancom scraper is disabled")
	}

	detailURL, err := s.GetURL(id)
	if err != nil {
		return nil, err
	}

	html, status, err := s.fetchPage(detailURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Caribbeancom detail page: %w", err)
	}
	if status == 404 {
		return nil, models.NewScraperNotFoundError("Caribbeancom", fmt.Sprintf("movie %s not found on Caribbeancom", id))
	}
	if status != 200 {
		return nil, models.NewScraperStatusError("Caribbeancom", status, fmt.Sprintf("Caribbeancom returned status code %d", status))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse Caribbeancom detail page: %w", err)
	}

	return parseDetailPage(doc, html, detailURL, id, s.language), nil
}

func parseDetailPage(doc *goquery.Document, html, sourceURL, fallbackID, language string) *models.ScraperResult {
	result := &models.ScraperResult{
		Source:    "caribbeancom",
		SourceURL: sourceURL,
		Language:  language,
	}

	resolvedID := extractMovieID(html, sourceURL, fallbackID)
	result.ID = strings.ToUpper(normalizeMovieID(resolvedID))
	result.ContentID = result.ID

	title := cleanString(doc.Find("h1[itemprop='name']").First().Text())
	if title == "" {
		title = cleanString(doc.Find("meta[property='og:title']").AttrOr("content", ""))
	}
	if title == "" {
		title = cleanString(doc.Find("title").First().Text())
	}
	title = stripSiteSuffix(title)
	result.Title = title
	result.OriginalTitle = title

	description := cleanString(doc.Find("p[itemprop='description']").First().Text())
	if description == "" {
		description = cleanString(doc.Find("meta[name='description']").AttrOr("content", ""))
	}
	result.Description = description

	runtimeRaw := extractSpecValue(doc, []string{"再生時間", "Duration", "Runtime", "Length"})
	if runtimeRaw == "" {
		runtimeRaw = cleanString(doc.Find("span[itemprop='duration']").First().AttrOr("content", ""))
	}
	result.Runtime = parseRuntime(runtimeRaw)

	releaseRaw := extractSpecValue(doc, []string{"配信日", "公開日", "Release Date", "Date"})
	if t := parseReleaseDate(releaseRaw); t != nil {
		result.ReleaseDate = t
	} else {
		result.ReleaseDate = parseReleaseDateFromID(result.ID)
	}

	result.Actresses = extractActresses(doc)
	result.Genres = extractGenres(doc)

	coverURL := extractCoverURL(doc, html, sourceURL, result.ID)
	result.CoverURL = coverURL
	result.PosterURL = coverURL
	result.ScreenshotURL = extractScreenshots(doc, sourceURL)
	result.TrailerURL = extractTrailerURL(html, sourceURL)
	result.ShouldCropPoster = true

	if result.Title == "" {
		result.Title = result.ID
		result.OriginalTitle = result.ID
	}

	return result
}

func extractMovieID(html, sourceURL, fallbackID string) string {
	if m := movieIDFromJSONRe.FindStringSubmatch(html); len(m) > 1 {
		return normalizeMovieID(m[1])
	}
	if m := movieIDFromPageRe.FindStringSubmatch(sourceURL); len(m) > 1 {
		return normalizeMovieID(m[1])
	}
	if m := movieIDTokenRegex.FindStringSubmatch(fallbackID); len(m) == 3 {
		return normalizeMovieID(m[1] + "-" + m[2])
	}
	return strings.TrimSpace(strings.ToUpper(strings.ReplaceAll(fallbackID, "_", "-")))
}

func extractSpecValue(doc *goquery.Document, labels []string) string {
	labelMatch := func(v string) bool {
		v = strings.ToLower(cleanString(strings.TrimSuffix(strings.TrimSuffix(v, ":"), "：")))
		for _, label := range labels {
			if strings.Contains(v, strings.ToLower(label)) {
				return true
			}
		}
		return false
	}

	var value string
	doc.Find("li.movie-spec, li.movie-detail__spec").EachWithBreak(func(_ int, li *goquery.Selection) bool {
		label := cleanString(li.Find("span.spec-title").First().Text())
		if !labelMatch(label) {
			return true
		}
		value = cleanString(li.Find("span.spec-content").First().Text())
		if value == "" {
			value = cleanString(li.Text())
		}
		return false
	})
	return value
}

func extractActresses(doc *goquery.Document) []models.ActressInfo {
	root := selectMovieInfoRoot(doc)
	seen := map[string]bool{}
	out := make([]models.ActressInfo, 0)

	appendName := func(name string) {
		name = cleanString(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, models.ActressInfo{JapaneseName: name})
	}

	root.Find("li.movie-spec, li.movie-detail__spec").Each(func(_ int, li *goquery.Selection) {
		label := strings.ToLower(cleanString(li.Find("span.spec-title").First().Text()))
		if !strings.Contains(label, "出演") && !strings.Contains(label, "starring") {
			return
		}
		li.Find("a[itemprop='actor'] span[itemprop='name'], a.spec__tag span[itemprop='name'], a.spec-item, a.spec__tag, a").Each(func(_ int, n *goquery.Selection) {
			appendName(n.Text())
		})
	})

	return out
}

func selectMovieInfoRoot(doc *goquery.Document) *goquery.Selection {
	candidates := []string{
		"#moviepages .movie-info.section",
		"#moviepages .movie-info",
		".movie-info.section",
		".movie-info",
	}
	for _, selector := range candidates {
		if sel := doc.Find(selector).First(); sel.Length() > 0 {
			return sel
		}
	}
	return doc.Selection
}

func extractGenres(doc *goquery.Document) []string {
	seen := map[string]bool{}
	out := make([]string, 0)

	appendGenre := func(v string) {
		v = cleanString(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}

	doc.Find("li.movie-spec, li.movie-detail__spec").Each(func(_ int, li *goquery.Selection) {
		label := strings.ToLower(cleanString(li.Find("span.spec-title").First().Text()))
		if !strings.Contains(label, "タグ") && !strings.Contains(label, "tags") {
			return
		}
		li.Find("a").Each(func(_ int, a *goquery.Selection) {
			appendGenre(a.Text())
		})
	})

	return out
}

func extractCoverURL(doc *goquery.Document, html, sourceURL, movieID string) string {
	if og := cleanString(doc.Find("meta[property='og:image']").AttrOr("content", "")); og != "" {
		return resolveURL(sourceURL, og)
	}
	if m := coverImagePathRe.FindStringSubmatch(html); len(m) > 1 {
		return resolveURL(sourceURL, m[1])
	}
	if movieID != "" {
		return resolveURL(sourceURL, "/moviepages/"+strings.ToLower(movieID)+"/images/l_l.jpg")
	}
	return ""
}

func extractScreenshots(doc *goquery.Document, sourceURL string) []string {
	seen := map[string]bool{}
	out := make([]string, 0)

	doc.Find("a.fancy-gallery, a.gallery-item, a.gallery-image-wrap").Each(func(_ int, a *goquery.Selection) {
		href := cleanString(a.AttrOr("href", ""))
		if href == "" {
			return
		}

		isSample := strings.TrimSpace(a.AttrOr("data-is_sample", ""))
		if isSample != "" && isSample != "1" {
			return
		}

		full := resolveURL(sourceURL, href)
		if full == "" || seen[full] {
			return
		}
		seen[full] = true
		out = append(out, full)
	})

	return out
}

func extractTrailerURL(html, sourceURL string) string {
	candidate := ""
	if m := trailerURLJSONRe.FindStringSubmatch(html); len(m) > 1 {
		candidate = m[1]
	} else if m := trailerURLAssignRe.FindStringSubmatch(html); len(m) > 1 {
		candidate = m[1]
	}
	if candidate == "" {
		return ""
	}

	candidate = strings.ReplaceAll(candidate, `\/`, `/`)
	candidate = strings.ReplaceAll(candidate, `\u0026`, "&")
	return resolveURL(sourceURL, candidate)
}

func parseRuntime(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}

	if m := runtimeISORegex.FindStringSubmatch(raw); len(m) == 4 {
		h := atoiSafe(m[1])
		min := atoiSafe(m[2])
		sec := atoiSafe(m[3])
		total := h*60 + min
		if sec >= 30 {
			total++
		}
		if total > 0 {
			return total
		}
	}

	if m := runtimeClockRegex.FindStringSubmatch(raw); len(m) >= 3 {
		h := atoiSafe(m[1])
		min := atoiSafe(m[2])
		sec := 0
		if len(m) > 3 {
			sec = atoiSafe(m[3])
		}
		total := h*60 + min
		if sec >= 30 {
			total++
		}
		if total > 0 {
			return total
		}
	}

	if m := runtimeMinuteRegex.FindStringSubmatch(raw); len(m) > 1 {
		return atoiSafe(m[1])
	}

	return atoiSafe(raw)
}

func parseReleaseDate(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	if m := dateYMDRegex.FindStringSubmatch(raw); len(m) > 1 {
		raw = m[1]
	}
	raw = strings.ReplaceAll(raw, "/", "-")

	for _, layout := range []string{"2006-01-02", "01-02-2006"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return &t
		}
	}
	return nil
}

func parseReleaseDateFromID(id string) *time.Time {
	id = normalizeMovieID(id)
	m := movieIDTokenRegex.FindStringSubmatch(id)
	if len(m) != 3 {
		return nil
	}

	dateToken := m[1]
	if len(dateToken) != 6 {
		return nil
	}

	month := atoiSafe(dateToken[0:2])
	day := atoiSafe(dateToken[2:4])
	year := 2000 + atoiSafe(dateToken[4:6])
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return nil
	}

	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day {
		return nil
	}
	return &t
}

func atoiSafe(v string) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

func normalizeMovieID(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if m := movieIDTokenRegex.FindStringSubmatch(v); len(m) == 3 {
		suffix := m[2]
		if len(suffix) == 2 {
			suffix = "0" + suffix
		}
		return m[1] + "-" + suffix
	}
	if m := movieIDFromPageRe.FindStringSubmatch(v); len(m) > 1 {
		return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(m[1])), "_", "-")
	}
	return strings.ReplaceAll(strings.ToLower(v), "_", "-")
}

func stripSiteSuffix(v string) string {
	v = cleanString(v)
	suffixes := []string{
		"| 無修正アダルト動画 カリビアンコム",
		"| Caribbeancom",
	}
	for _, suffix := range suffixes {
		if strings.Contains(v, suffix) {
			v = strings.TrimSpace(strings.Split(v, suffix)[0])
		}
	}
	return strings.TrimSpace(v)
}

func normalizeLanguage(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "en" {
		return "en"
	}
	return "ja"
}

func (s *Scraper) buildMoviePageURL(movieID string) string {
	movieID = normalizeMovieID(movieID)
	if s.language == "en" {
		return strings.TrimRight(s.baseURL, "/") + "/eng/moviepages/" + movieID + "/index.html"
	}
	return strings.TrimRight(s.baseURL, "/") + "/moviepages/" + movieID + "/index.html"
}

func (s *Scraper) applyLanguage(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return rawURL
	}

	hostname := strings.ToLower(u.Hostname())
	if !strings.Contains(hostname, "caribbeancom.com") {
		return rawURL
	}

	port := u.Port()
	host := "www.caribbeancom.com"
	if s.language == "en" {
		host = "en.caribbeancom.com"
		if !strings.HasPrefix(u.Path, "/eng/") {
			if strings.HasPrefix(u.Path, "/moviepages/") {
				u.Path = "/eng" + u.Path
			}
		}
	} else {
		u.Path = strings.TrimPrefix(u.Path, "/eng")
		if u.Path == "" {
			u.Path = "/"
		}
	}

	if port != "" {
		u.Host = host + ":" + port
	} else {
		u.Host = host
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

	decoded, err := decodeBody(resp)
	if err != nil {
		html := resp.String()
		if resp.StatusCode() == 200 && models.IsCloudflareChallengePage(html) {
			return "", resp.StatusCode(), models.NewScraperChallengeError(
				"Caribbeancom",
				"Caribbeancom returned a Cloudflare challenge page (request blocked; adjust proxy/IP)",
			)
		}
		return html, resp.StatusCode(), nil
	}

	if resp.StatusCode() == 200 && models.IsCloudflareChallengePage(decoded) {
		return "", resp.StatusCode(), models.NewScraperChallengeError(
			"Caribbeancom",
			"Caribbeancom returned a Cloudflare challenge page (request blocked; adjust proxy/IP)",
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
		baseURL.Fragment = ""
		return baseURL.String()
	}
	baseURL.Path = path.Join(path.Dir(baseURL.Path), raw)
	return baseURL.String()
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
