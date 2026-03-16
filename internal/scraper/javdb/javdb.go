package javdb

import (
	"fmt"
	"hash/fnv"
	"net/url"
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
	"golang.org/x/net/html"
)

const (
	defaultBaseURL = "https://javdb.com"
	searchPath     = "/search?q=%s&f=all"
)

var (
	nonAlphaNumRegex = regexp.MustCompile(`[^A-Za-z0-9]+`)
	runtimeRegex     = regexp.MustCompile(`(\d+)`)
	ratingRegex      = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)`)
	votesRegex       = regexp.MustCompile(`([0-9][0-9,]*)`)
	dateFormats      = []string{
		"2006-01-02",
		"2006/01/02",
		"2006.01.02",
	}
)

// Scraper implements the JavDB scraper.
type Scraper struct {
	client          *resty.Client
	flaresolverr    *httpclient.FlareSolverr
	cfg             *config.JavDBConfig
	enabled         bool
	baseURL         string
	requestDelay    time.Duration
	proxyOverride   *config.ProxyConfig
	downloadProxy   *config.ProxyConfig
	lastRequestTime atomic.Value
}

// New creates a new JavDB scraper.
func New(cfg *config.Config) *Scraper {
	scraperCfg := cfg.Scrapers.JavDB
	proxyCfg := config.ResolveScraperProxy(cfg.Scrapers.Proxy, scraperCfg.Proxy)

	client, fs, err := httpclient.NewRestyClientWithFlareSolverr(proxyCfg, 30*time.Second, 3)
	usingProxy := err == nil && proxyCfg.Enabled && strings.TrimSpace(proxyCfg.URL) != ""
	if err != nil {
		logging.Errorf("JavDB: Failed to create HTTP client with proxy/flaresolverr: %v, using explicit no-proxy fallback", err)
		client = httpclient.NewRestyClientNoProxy(30*time.Second, 3)
		fs = nil
	}

	userAgent := config.ResolveScraperUserAgent(
		cfg.Scrapers.UserAgent,
		scraperCfg.UseFakeUserAgent,
		scraperCfg.FakeUserAgent,
	)

	client.SetHeader("User-Agent", userAgent)
	client.SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	client.SetHeader("Accept-Language", "en-US,en;q=0.9,ja;q=0.8,zh;q=0.7")
	client.SetHeader("Connection", "keep-alive")
	client.SetHeader("Upgrade-Insecure-Requests", "1")

	baseURL := scraperCfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	s := &Scraper{
		client:        client,
		flaresolverr:  fs,
		cfg:           &scraperCfg,
		enabled:       scraperCfg.Enabled,
		baseURL:       strings.TrimRight(baseURL, "/"),
		requestDelay:  time.Duration(scraperCfg.RequestDelay) * time.Millisecond,
		proxyOverride: scraperCfg.Proxy,
		downloadProxy: scraperCfg.DownloadProxy,
	}

	s.lastRequestTime.Store(time.Time{})

	if usingProxy {
		logging.Infof("JavDB: Using proxy %s", httpclient.SanitizeProxyURL(proxyCfg.URL))
	}
	if scraperCfg.UseFlareSolverr && fs == nil {
		logging.Warn("JavDB: use_flaresolverr=true but no FlareSolverr client is configured")
	}

	return s
}

// Name returns the scraper identifier.
func (s *Scraper) Name() string {
	return "javdb"
}

// IsEnabled returns whether the scraper is enabled.
func (s *Scraper) IsEnabled() bool {
	return s.enabled
}

// ResolveDownloadProxyForHost declares JavDB-owned media hosts for downloader proxy routing.
func (s *Scraper) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil, nil, false
	}
	if strings.HasSuffix(host, "jdbstatic.com") || strings.HasSuffix(host, "javdb.com") {
		return s.downloadProxy, s.proxyOverride, true
	}
	return nil, nil, false
}

// GetURL returns JavDB search URL for a given ID.
func (s *Scraper) GetURL(id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("movie ID cannot be empty")
	}
	return fmt.Sprintf(s.baseURL+searchPath, url.QueryEscape(strings.TrimSpace(id))), nil
}

// Search looks up a movie by ID and scrapes metadata.
func (s *Scraper) Search(id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("JavDB scraper is disabled")
	}

	detailURL, err := s.findDetailURL(id)
	if err != nil {
		return nil, err
	}

	html, err := s.fetchPage(detailURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch detail page: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse detail page HTML: %w", err)
	}

	result, err := s.parseDetailPage(doc, detailURL, id)
	if err != nil {
		return nil, err
	}

	if hasDetailMetadata(result, id) {
		return result, nil
	}

	// FlareSolverr occasionally returns non-detail pages for JavDB detail URLs.
	// Retry once with direct HTTP using any cookies already set on the client.
	logging.Warnf("JavDB: Parsed sparse detail response for %s, retrying via direct request", detailURL)
	retryHTML, err := s.fetchPageDirect(detailURL)
	if err != nil {
		return nil, fmt.Errorf("parsed sparse detail page and direct retry failed: %w", err)
	}
	retryDoc, err := goquery.NewDocumentFromReader(strings.NewReader(retryHTML))
	if err != nil {
		return nil, fmt.Errorf("failed to parse retried detail page HTML: %w", err)
	}
	retryResult, err := s.parseDetailPage(retryDoc, detailURL, id)
	if err != nil {
		return nil, err
	}
	if !hasDetailMetadata(retryResult, id) {
		return nil, fmt.Errorf("JavDB returned non-detail content for %s", detailURL)
	}
	return retryResult, nil
}

func (s *Scraper) findDetailURL(id string) (string, error) {
	searchURL, err := s.GetURL(id)
	if err != nil {
		return "", err
	}

	html, err := s.fetchPage(searchURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch search page: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", fmt.Errorf("failed to parse search page HTML: %w", err)
	}

	targetID := normalizeIDForCompare(id)
	var (
		foundURL  string
		bestMatch idMatchType
	)

	doc.Find(".movie-list .item").EachWithBreak(func(i int, item *goquery.Selection) bool {
		link := item.Find("a[href]").First()
		href, exists := link.Attr("href")
		if !exists {
			return true
		}

		candidates := []string{
			item.Find(".uid").First().Text(),
			item.Find(".video-title strong").First().Text(),
			item.Find(".video-title").First().Text(),
		}

		for _, c := range candidates {
			match := idMatchRank(c, targetID)
			if match > bestMatch {
				bestMatch = match
				foundURL = resolveURL(s.baseURL, href)
			}
			if match == idMatchExact {
				return false
			}
		}
		return true
	})

	if foundURL != "" {
		return foundURL, nil
	}

	// Fallback: if only one detail link exists, use it.
	detailLinks := make([]string, 0, 1)
	doc.Find(".movie-list .item a[href]").Each(func(_ int, sel *goquery.Selection) {
		if href, ok := sel.Attr("href"); ok && strings.Contains(href, "/v/") {
			detailLinks = append(detailLinks, resolveURL(s.baseURL, href))
		}
	})
	if len(detailLinks) == 1 {
		return detailLinks[0], nil
	}

	return "", models.NewScraperNotFoundError("JavDB", fmt.Sprintf("movie %s not found on JavDB", id))
}

func (s *Scraper) fetchPage(targetURL string) (string, error) {
	s.waitForRateLimit()
	defer s.updateLastRequestTime()

	resp, err := s.client.R().Get(targetURL)
	if err == nil && resp != nil && resp.StatusCode() == 200 {
		html := resp.String()
		if !models.IsCloudflareChallengePage(html) {
			return html, nil
		}
		logging.Warnf("JavDB: Direct request returned Cloudflare challenge, escalating to FlareSolverr: %s", targetURL)
	} else if err == nil && resp != nil {
		logging.Debugf("JavDB: Direct request returned status %d for %s", resp.StatusCode(), targetURL)
	}

	if s.cfg.UseFlareSolverr && s.flaresolverr != nil {
		logging.Debugf("JavDB: Resolving via FlareSolverr: %s", targetURL)
		html, cookies, fsErr := s.flaresolverr.ResolveURL(targetURL)
		if fsErr == nil {
			for _, c := range cookies {
				s.client.SetCookie(&c)
			}
			if models.IsCloudflareChallengePage(html) {
				return "", models.NewScraperChallengeError(
					"JavDB",
					"JavDB returned a Cloudflare challenge page (request blocked; check FlareSolverr/proxy configuration)",
				)
			}
			return html, nil
		}
		logging.Warnf("JavDB: FlareSolverr failed, falling back to direct request result: %v", fsErr)
	}

	return s.fetchPageDirectResponse(resp, err)
}

func (s *Scraper) fetchPageDirect(targetURL string) (string, error) {
	s.waitForRateLimit()
	defer s.updateLastRequestTime()

	resp, err := s.client.R().Get(targetURL)
	return s.fetchPageDirectResponse(resp, err)
}

func (s *Scraper) fetchPageDirectResponse(resp *resty.Response, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if resp.StatusCode() != 200 {
		return "", models.NewScraperStatusError(
			"JavDB",
			resp.StatusCode(),
			fmt.Sprintf("JavDB returned status code %d", resp.StatusCode()),
		)
	}

	html := resp.String()
	if models.IsCloudflareChallengePage(html) {
		return "", models.NewScraperChallengeError(
			"JavDB",
			"JavDB returned a Cloudflare challenge page (request blocked; enable FlareSolverr or adjust proxy/IP)",
		)
	}

	return html, nil
}

func hasDetailMetadata(result *models.ScraperResult, fallbackID string) bool {
	if result == nil {
		return false
	}
	if result.CoverURL != "" ||
		result.Runtime > 0 ||
		result.ReleaseDate != nil ||
		result.Director != "" ||
		result.Maker != "" ||
		result.Label != "" ||
		result.Series != "" ||
		len(result.Actresses) > 0 ||
		len(result.Genres) > 0 ||
		len(result.ScreenshotURL) > 0 {
		return true
	}
	return strings.TrimSpace(result.Title) != "" && !idsMatch(result.Title, fallbackID)
}

func (s *Scraper) parseDetailPage(doc *goquery.Document, sourceURL, fallbackID string) (*models.ScraperResult, error) {
	result := &models.ScraperResult{
		Source:    s.Name(),
		SourceURL: sourceURL,
		Language:  "ja",
	}

	titleNode := doc.Find(".title.is-4").First()
	fullTitle := cleanString(titleNode.Text())
	idFromTitle := cleanString(titleNode.Find("strong").First().Text())
	if idFromTitle != "" {
		result.ID = idFromTitle
	}

	if fullTitle != "" && result.ID != "" {
		fullTitle = strings.TrimSpace(strings.TrimPrefix(fullTitle, result.ID))
	}

	if fullTitle == "" {
		fullTitle = cleanString(doc.Find("meta[property='og:title']").AttrOr("content", ""))
	}
	result.Title = fullTitle
	result.OriginalTitle = fullTitle

	result.CoverURL = extractFirstURL(doc, []string{
		".column-video-cover img.video-cover",
		".column-video-cover img",
		".video-meta-panel img.video-cover",
	}, s.baseURL)
	result.PosterURL = result.CoverURL
	result.TrailerURL = extractTrailerURL(doc, s.baseURL)
	result.ScreenshotURL = extractScreenshotURLs(doc, s.baseURL)

	description := cleanString(doc.Find("span[itemprop='description']").First().Text())
	if description == "" {
		description = cleanString(doc.Find(".movie-panel-info .movie-description").First().Text())
	}
	result.Description = description

	hasFemaleActressRow := false

	doc.Find(".movie-panel-info .panel-block").Each(func(_ int, block *goquery.Selection) {
		label := normalizeLabel(block.Find("strong").First().Text())
		valueNode := block.Find(".value").First()
		if valueNode.Length() == 0 {
			valueNode = block
		}
		valueText := cleanString(valueNode.Text())

		switch {
		case labelContains(label, "番號", "番号", "識別碼", "识别码", "ID"):
			if result.ID == "" && valueText != "" {
				result.ID = valueText
			}
		case labelContains(label, "日期", "發行日期", "发行日期", "release"):
			if t := parseDate(valueText); t != nil {
				result.ReleaseDate = t
			}
		case labelContains(label, "時長", "长度", "長度", "runtime", "length", "duration"):
			result.Runtime = parseRuntime(valueText)
		case labelContains(label, "導演", "导演", "director"):
			result.Director = extractFirstText(valueNode)
		case labelContains(label, "片商", "maker", "studio"):
			result.Maker = extractFirstText(valueNode)
		case labelContains(label, "發行", "发行", "label", "publisher"):
			result.Label = extractFirstText(valueNode)
		case labelContains(label, "系列", "series"):
			result.Series = extractFirstText(valueNode)
		case labelContains(label, "評分", "评分", "rating", "score"):
			result.Rating = parseRating(valueText)
		case labelContains(label, "類別", "类别", "genre", "tag", "tags"):
			result.Genres = extractStringList(valueNode)
		default:
			switch classifyCastLabel(label) {
			case castLabelFemale:
				if actresses := extractActresses(valueNode); len(actresses) > 0 {
					result.Actresses = actresses
					hasFemaleActressRow = true
				}
			case castLabelGeneric:
				// Generic cast rows may include male actors. Use only as fallback
				// when no female-specific row was found.
				if hasFemaleActressRow || len(result.Actresses) > 0 {
					return
				}
				if actresses := extractActresses(valueNode); len(actresses) > 0 {
					result.Actresses = actresses
				}
			case castLabelMale:
				// Explicit male actor rows should not be merged into actresses.
			}
		}
	})

	if result.ID == "" {
		result.ID = fallbackID
	}
	result.ID = cleanString(result.ID)
	result.ContentID = result.ID

	if result.Title == "" {
		result.Title = result.ID
		result.OriginalTitle = result.ID
	}

	return result, nil
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
	elapsed := time.Since(lastTime)
	if elapsed < s.requestDelay {
		time.Sleep(s.requestDelay - elapsed)
	}
}

func (s *Scraper) updateLastRequestTime() {
	s.lastRequestTime.Store(time.Now())
}

func normalizeIDForCompare(id string) string {
	return strings.ToUpper(nonAlphaNumRegex.ReplaceAllString(strings.TrimSpace(id), ""))
}

func idsMatch(candidate, target string) bool {
	return idMatchRank(candidate, target) != idMatchNone
}

type idMatchType int

const (
	idMatchNone idMatchType = iota
	idMatchVariant
	idMatchNormalized
	idMatchExact
)

func idMatchRank(candidate, target string) idMatchType {
	c := normalizeIDForCompare(candidate)
	t := normalizeIDForCompare(target)
	if c == "" || t == "" {
		return idMatchNone
	}
	if c == t {
		return idMatchExact
	}

	cNoPadding := trimNumericPadding(c)
	tNoPadding := trimNumericPadding(t)
	if cNoPadding == tNoPadding {
		return idMatchNormalized
	}

	if trimVariantSuffix(cNoPadding) == trimVariantSuffix(tNoPadding) {
		return idMatchVariant
	}

	return idMatchNone
}

func trimNumericPadding(id string) string {
	var prefix strings.Builder
	var number strings.Builder
	var suffix strings.Builder
	seenDigit := false
	for _, r := range id {
		if unicode.IsDigit(r) {
			seenDigit = true
			number.WriteRune(r)
			continue
		}
		if !seenDigit {
			prefix.WriteRune(r)
			continue
		}
		suffix.WriteRune(r)
	}
	if number.Len() == 0 {
		return id
	}
	n := strings.TrimLeft(number.String(), "0")
	if n == "" {
		n = "0"
	}
	return prefix.String() + n + suffix.String()
}

func trimVariantSuffix(id string) string {
	if len(id) < 2 {
		return id
	}
	last := id[len(id)-1]
	prev := id[len(id)-2]
	if last >= 'A' && last <= 'Z' && prev >= '0' && prev <= '9' {
		return id[:len(id)-1]
	}
	return id
}

func resolveURL(baseURL, rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "/") {
		return strings.TrimRight(baseURL, "/") + rawURL
	}
	return strings.TrimRight(baseURL, "/") + "/" + rawURL
}

func cleanString(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

func normalizeLabel(s string) string {
	s = cleanString(s)
	s = strings.TrimSuffix(s, ":")
	s = strings.TrimSuffix(s, "：")
	return strings.ToLower(s)
}

func labelContains(label string, keys ...string) bool {
	for _, k := range keys {
		if strings.Contains(label, strings.ToLower(k)) {
			return true
		}
	}
	return false
}

type castLabelKind int

const (
	castLabelUnknown castLabelKind = iota
	castLabelMale
	castLabelGeneric
	castLabelFemale
)

func classifyCastLabel(label string) castLabelKind {
	if labelContains(label, "male actor", "male actors", "男優", "男演员", "男演員") {
		return castLabelMale
	}
	if labelContains(label, "女優", "女优", "actress", "actress(es)") {
		return castLabelFemale
	}
	if labelContains(label, "演員", "演员", "actor", "actor(s)", "出演者", "cast") {
		return castLabelGeneric
	}
	return castLabelUnknown
}

func extractFirstText(sel *goquery.Selection) string {
	if text := cleanString(sel.Find("a").First().Text()); text != "" {
		return text
	}
	return cleanString(sel.Text())
}

func parseDate(s string) *time.Time {
	s = cleanString(s)
	for _, f := range dateFormats {
		if t, err := time.Parse(f, s); err == nil {
			return &t
		}
	}
	return nil
}

func parseRuntime(s string) int {
	matches := runtimeRegex.FindStringSubmatch(cleanString(s))
	if len(matches) < 2 {
		return 0
	}
	v, _ := strconv.Atoi(matches[1])
	return v
}

func parseRating(s string) *models.Rating {
	s = cleanString(s)
	if s == "" {
		return nil
	}

	score := 0.0
	votes := 0

	if m := ratingRegex.FindStringSubmatch(s); len(m) > 1 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			score = v
			// JavDB usually shows ratings on a 5-point scale.
			if score > 0 && score <= 5 {
				score *= 2
			}
		}
	}

	allVotes := votesRegex.FindAllString(s, -1)
	if len(allVotes) > 1 {
		if v, err := strconv.Atoi(strings.ReplaceAll(allVotes[len(allVotes)-1], ",", "")); err == nil {
			votes = v
		}
	}

	if score <= 0 && votes <= 0 {
		return nil
	}
	return &models.Rating{
		Score: score,
		Votes: votes,
	}
}

func extractActresses(sel *goquery.Selection) []models.ActressInfo {
	actresses := make([]models.ActressInfo, 0)
	seen := make(map[string]bool)
	type actressCandidate struct {
		name          string
		genderHint    string // "female", "male", or ""
		maleHeuristic bool
	}
	candidates := make([]actressCandidate, 0)
	hasSymbolGender := false

	sel.Find("a").Each(func(_ int, a *goquery.Selection) {
		name := cleanString(a.Text())
		if name == "" || seen[name] {
			return
		}
		genderHint := genderHintFromSymbolSibling(a)
		if genderHint != "" {
			hasSymbolGender = true
		}
		candidates = append(candidates, actressCandidate{
			name:          name,
			genderHint:    genderHint,
			maleHeuristic: isLikelyMaleActorLink(a),
		})
	})

	for _, c := range candidates {
		if hasSymbolGender {
			// When symbol markers are present, trust them as source of truth.
			if c.genderHint != "female" {
				continue
			}
		} else if c.genderHint == "male" || c.maleHeuristic {
			continue
		}

		if seen[c.name] {
			continue
		}
		seen[c.name] = true
		actresses = append(actresses, models.ActressInfo{
			// JavDB doesn't expose DMM actress IDs; use a stable negative surrogate so
			// multiple actresses can coexist under the DB's unique dmm_id constraint.
			DMMID:        syntheticActressID(c.name),
			JapaneseName: c.name,
		})
	}

	// Fallback to plain text parsing when no links are available.
	if len(actresses) == 0 {
		names := extractStringList(sel)
		for _, n := range names {
			if seen[n] {
				continue
			}
			seen[n] = true
			actresses = append(actresses, models.ActressInfo{
				DMMID:        syntheticActressID(n),
				JapaneseName: n,
			})
		}
	}

	if len(actresses) == 0 {
		return nil
	}
	return actresses
}

func isLikelyMaleActorLink(sel *goquery.Selection) bool {
	classAttr := strings.ToLower(sel.AttrOr("class", ""))
	if strings.Contains(classAttr, "male") || strings.Contains(classAttr, "gender-male") {
		return true
	}

	for _, attr := range []string{"data-gender", "gender", "title", "aria-label"} {
		v := strings.ToLower(strings.TrimSpace(sel.AttrOr(attr, "")))
		if hasWordToken(v, "male") || strings.Contains(v, "男優") || strings.Contains(v, "男演员") || strings.Contains(v, "男演員") {
			return true
		}
	}

	// Common patterns: male marker appears near the anchor in sibling text.
	context := strings.ToLower(cleanString(sel.Parent().Text()))
	if context == "" {
		context = strings.ToLower(cleanString(sel.Text()))
	}

	hasMaleMarker := strings.Contains(context, "♂") ||
		hasWordToken(context, "male") ||
		strings.Contains(context, "男優") ||
		strings.Contains(context, "男演员") ||
		strings.Contains(context, "男演員")

	hasFemaleMarker := strings.Contains(context, "♀") ||
		hasWordToken(context, "female") ||
		strings.Contains(context, "女優") ||
		strings.Contains(context, "女优")

	if hasMaleMarker && !hasFemaleMarker {
		return true
	}

	return false
}

func genderHintFromSymbolSibling(sel *goquery.Selection) string {
	if sel == nil || len(sel.Nodes) == 0 {
		return ""
	}
	node := sel.Nodes[0]

	if hint := scanSymbolSibling(node, true); hint != "" {
		return hint
	}
	if hint := scanSymbolSibling(node, false); hint != "" {
		return hint
	}
	return ""
}

func scanSymbolSibling(anchor *html.Node, forward bool) string {
	step := func(n *html.Node) *html.Node {
		if forward {
			return n.NextSibling
		}
		return n.PrevSibling
	}

	for n := step(anchor); n != nil; n = step(n) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "a") {
			break
		}
		if n.Type != html.ElementNode || !strings.EqualFold(n.Data, "strong") {
			continue
		}

		classAttr := strings.ToLower(strings.TrimSpace(nodeAttr(n, "class")))
		if !strings.Contains(classAttr, "symbol") {
			continue
		}

		if strings.Contains(classAttr, "female") {
			return "female"
		}
		if strings.Contains(classAttr, "male") {
			return "male"
		}

		text := strings.TrimSpace(nodeText(n))
		switch {
		case strings.Contains(text, "♀"):
			return "female"
		case strings.Contains(text, "♂"):
			return "male"
		}
	}
	return ""
}

func nodeAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur == nil {
			return
		}
		if cur.Type == html.TextNode {
			b.WriteString(cur.Data)
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return b.String()
}

func hasWordToken(text, token string) bool {
	for _, part := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if part == token {
			return true
		}
	}
	return false
}

func syntheticActressID(name string) int {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(normalized))
	id := int(h.Sum32() & 0x7fffffff)
	if id == 0 {
		id = 1
	}
	return -id
}

func extractStringList(sel *goquery.Selection) []string {
	values := make([]string, 0)
	seen := make(map[string]bool)

	sel.Find("a").Each(func(_ int, a *goquery.Selection) {
		v := cleanString(a.Text())
		if v != "" && !seen[v] {
			seen[v] = true
			values = append(values, v)
		}
	})
	if len(values) > 0 {
		return values
	}

	raw := cleanString(sel.Text())
	if raw == "" || isNotAvailableValue(raw) {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '/' || r == '、'
	})
	for _, p := range parts {
		v := cleanString(p)
		if v == "" || isNotAvailableValue(v) {
			continue
		}
		if !seen[v] {
			seen[v] = true
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func isNotAvailableValue(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return false
	}

	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "／", "/")

	switch normalized {
	case "n/a", "n.a.", "na", "none", "null", "nil", "notavailable", "notapplicable", "無し", "なし", "-", "--":
		return true
	default:
		return false
	}
}

func extractFirstURL(doc *goquery.Document, selectors []string, baseURL string) string {
	for _, selector := range selectors {
		node := doc.Find(selector).First()
		if node.Length() == 0 {
			continue
		}
		for _, attr := range []string{"data-original", "data-src", "src"} {
			if val := node.AttrOr(attr, ""); val != "" {
				return resolveURL(baseURL, val)
			}
		}
	}
	return ""
}

func extractScreenshotURLs(doc *goquery.Document, baseURL string) []string {
	urls := make([]string, 0)
	seen := make(map[string]bool)

	addURL := func(raw string) {
		if strings.Contains(raw, "/login") {
			return
		}
		u := resolveURL(baseURL, raw)
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		urls = append(urls, u)
	}

	doc.Find(".tile-images.preview-images a[href], .preview-images a[href]").Each(func(_ int, sel *goquery.Selection) {
		if strings.Contains(sel.AttrOr("class", ""), "preview-video-container") {
			return
		}
		if href, ok := sel.Attr("href"); ok {
			addURL(href)
		}
	})

	if len(urls) == 0 {
		doc.Find(".tile-images.preview-images img, .preview-images img").Each(func(_ int, sel *goquery.Selection) {
			for _, attr := range []string{"data-original", "data-src", "src"} {
				if src, ok := sel.Attr(attr); ok {
					addURL(src)
					return
				}
			}
		})
	}

	return urls
}

func extractTrailerURL(doc *goquery.Document, baseURL string) string {
	for _, selector := range []string{
		"#preview-video source[src]",
		"video#preview-video source[src]",
		"video source[src]",
	} {
		if src := doc.Find(selector).First().AttrOr("src", ""); src != "" {
			return resolveURL(baseURL, src)
		}
	}
	return ""
}
