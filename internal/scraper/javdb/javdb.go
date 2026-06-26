package javdb

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/challengedetect"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/ssrf"
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
	// URL extraction pattern
	javdbVideoPathRegex = regexp.MustCompile(`/v/([A-Za-z0-9]+)`)
)

// scraper implements the JavDB scraper.
type scraper struct {
	client        *resty.Client
	flaresolverr  *httpclient.FlareSolverr
	enabled       bool
	baseURL       string
	proxyOverride *models.ProxyConfig
	downloadProxy *models.ProxyConfig
	rateLimiter   *ratelimit.Limiter
	settings      models.ScraperSettings // stores the full settings for Config() method
	cookieMu      sync.Mutex             // protects cookie mutations on shared client
}

// New creates a new JavDB scraper.
// newScraper creates a new JavDB scraper.
func newScraper(settings *models.ScraperSettings, globalProxy *models.ProxyConfig, globalFlareSolverr models.FlareSolverrConfig) *scraper {
	configForHTTP := &models.ScraperSettings{
		Enabled:         settings.Enabled,
		Timeout:         settings.Timeout,
		RateLimit:       settings.RateLimit,
		RetryCount:      settings.RetryCount,
		UserAgent:       settings.UserAgent,
		Proxy:           settings.Proxy,
		DownloadProxy:   settings.DownloadProxy,
		UseFlareSolverr: settings.UseFlareSolverr,
	}

	client, flaresolverr, err := httpclient.FromScraperSettings(configForHTTP, globalProxy, globalFlareSolverr,
		httpclient.WithHeaders(httpclient.StandardHTMLHeaders()),
		httpclient.WithHeaders(httpclient.UserAgentHeader(settings.UserAgent)),
	).BuildWithFlareSolverr()

	proxyEnabled := false
	var proxyCfg *models.ProxyProfile
	if globalProxy != nil {
		proxyEnabled = globalProxy.Enabled
		proxyCfg = models.ResolveScraperProxy(*globalProxy, settings.Proxy)
	}
	if settings.Proxy != nil && settings.Proxy.Enabled {
		proxyEnabled = true
	}
	usingProxy := err == nil && proxyEnabled && proxyCfg != nil && strings.TrimSpace(proxyCfg.URL) != ""
	if err != nil {
		logging.Errorf("JavDB: Failed to create HTTP client with proxy/flaresolverr: %v, using explicit no-proxy fallback", err)
		client = httpclient.NewRestyClientNoProxy(time.Duration(settings.Timeout)*time.Second, settings.RetryCount)
		flaresolverr = nil
	}

	baseURL := settings.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	s := &scraper{
		client:        client,
		flaresolverr:  flaresolverr,
		enabled:       settings.Enabled,
		baseURL:       strings.TrimRight(baseURL, "/"),
		rateLimiter:   ratelimit.NewLimiter(time.Duration(settings.RateLimit) * time.Millisecond),
		proxyOverride: settings.Proxy,
		downloadProxy: settings.DownloadProxy,
		settings:      *settings,
	}

	if usingProxy && proxyCfg != nil {
		logging.Infof("JavDB: Using proxy %s", httpclient.SanitizeProxyURL(proxyCfg.URL))
	}
	if settings.UseFlareSolverr && flaresolverr == nil {
		logging.Warn("JavDB: use_flaresolverr=true but no FlareSolverr client is configured")
	}

	return s
}

// Name returns the scraper identifier.
func (s *scraper) Name() string {
	return "javdb"
}

// IsEnabled returns whether the scraper is enabled.
func (s *scraper) IsEnabled() bool {
	return s.enabled
}

// Config returns the scraper's configuration
func (s *scraper) Config() *models.ScraperSettings {
	cloned := s.settings.Clone()
	return &cloned
}

// Close cleans up resources held by the scraper (HTTP client, FlareSolverr).
func (s *scraper) Close() error {
	if s.flaresolverr != nil {
		if closeErr := s.flaresolverr.Close(); closeErr != nil {
			logging.Debugf("JavDB: Error closing FlareSolverr: %v", closeErr)
		}
	}
	return nil
}

// CanHandleURL returns true if this scraper can handle the given URL
func (s *scraper) CanHandleURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	baseURLHost := s.baseURL
	if baseURLHost == "" {
		baseURLHost = defaultBaseURL
	}
	if parsedBase, err := url.Parse(baseURLHost); err == nil {
		baseURLHost = parsedBase.Hostname()
	}
	return host == strings.ToLower(baseURLHost) || strings.HasSuffix(host, "."+strings.ToLower(baseURLHost))
}

// ExtractIDFromURL extracts the movie ID from a JavDB URL
func (s *scraper) ExtractIDFromURL(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse JavDB URL: %w", err)
	}
	matches := javdbVideoPathRegex.FindStringSubmatch(u.Path)
	if len(matches) > 1 {
		return matches[1], nil
	}

	return "", fmt.Errorf("failed to extract ID from JavDB URL")
}

// ScrapeURL directly scrapes metadata from a JavDB URL.
// This provides more accurate results than ID-based search when the exact URL is known.
func (s *scraper) ScrapeURL(ctx context.Context, urlStr string) (*models.ScraperResult, error) {
	if !s.CanHandleURL(urlStr) {
		return nil, models.NewScraperNotFoundError("JavDB", "URL not handled by JavDB scraper")
	}

	if !s.enabled {
		return nil, fmt.Errorf("JavDB scraper is disabled")
	}

	// Extract video ID from URL for fallback
	videoID, err := s.ExtractIDFromURL(urlStr)
	if err != nil {
		logging.Debugf("JavDB ScrapeURL: Failed to extract ID from URL: %v", err)
		videoID = ""
	}

	// Fetch the page using existing method (handles FlareSolverr, rate limiting, Cloudflare)
	html, err := s.fetchPageCtx(ctx, urlStr)
	if err != nil {
		// Check if it's a scraper error and return as-is
		if scraperErr, ok := models.AsScraperError(err); ok {
			return nil, scraperErr
		}
		return nil, fmt.Errorf("failed to fetch JavDB page: %w", err)
	}

	// Parse HTML into document
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse JavDB HTML: %w", err)
	}

	// Use existing parseDetailPage method
	result, err := s.parseDetailPage(doc, urlStr, videoID)
	if err != nil {
		return nil, err
	}

	// Verify we got meaningful metadata
	if !hasDetailMetadata(result, videoID) {
		// Check if this might be a Cloudflare challenge page or login page
		if challengedetect.IsCloudflareChallengePage(html) {
			return nil, models.NewScraperChallengeError("JavDB",
				"JavDB returned a Cloudflare challenge page (request blocked; check FlareSolverr/proxy configuration)")
		}

		// Retry once with direct HTTP request
		logging.Warnf("JavDB ScrapeURL: Parsed sparse detail response, retrying via direct request")
		retryHTML, err := s.fetchPageDirectCtx(ctx, urlStr)
		if err != nil {
			return nil, fmt.Errorf("parsed sparse detail page and direct retry failed: %w", err)
		}
		retryDoc, err := goquery.NewDocumentFromReader(strings.NewReader(retryHTML))
		if err != nil {
			return nil, fmt.Errorf("failed to parse retried detail page HTML: %w", err)
		}
		result, err = s.parseDetailPage(retryDoc, urlStr, videoID)
		if err != nil {
			return nil, err
		}
		if !hasDetailMetadata(result, videoID) {
			return nil, fmt.Errorf("JavDB returned non-detail content for %s", urlStr)
		}
	}

	logging.Debugf("JavDB ScrapeURL: Successfully scraped %s (ID=%s, Title=%s)", urlStr, result.ID, result.Title)
	return result, nil
}

// ResolveDownloadProxyForHost declares JavDB-owned media hosts for downloader proxy routing.
func (s *scraper) ResolveDownloadProxyForHost(host string) (*models.ProxyConfig, *models.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil, nil, false
	}
	if host == "jdbstatic.com" || strings.HasSuffix(host, ".jdbstatic.com") ||
		host == "javdb.com" || strings.HasSuffix(host, ".javdb.com") {
		return s.settings.DownloadProxy, s.settings.Proxy, true
	}
	return nil, nil, false
}

func (s *scraper) GetURL(ctx context.Context, id string) (string, error) {
	_ = ctx // pure URL formatter — no I/O to cancel, ctx accepted for interface compliance
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("movie ID cannot be empty")
	}
	return fmt.Sprintf(s.baseURL+searchPath, url.QueryEscape(strings.TrimSpace(id))), nil
}

// isJavDBVideoCode checks if an ID looks like a JavDB video code
// JavDB video codes are alphanumeric (case-insensitive) and typically 4-10 characters
// Examples: AbJEe, 5aB3d, etc.
func isJavDBVideoCode(id string) bool {
	if len(id) < 3 || len(id) > 12 {
		return false
	}
	for _, c := range id {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

// Search looks up a movie by ID and scrapes metadata.
// Search looks up a movie by ID and scrapes metadata with context support.
func (s *scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("JavDB scraper is disabled")
	}

	// If ID looks like a JavDB video code (alphanumeric, short), try direct URL first
	// JavDB URLs are /v/{code} where code is typically 4-6 alphanumeric characters
	cleanID := strings.TrimSpace(id)
	if isJavDBVideoCode(cleanID) {
		directURL := fmt.Sprintf("%s/v/%s", s.baseURL, cleanID)
		logging.Debugf("JavDB: ID '%s' looks like video code, trying direct URL: %s", cleanID, directURL)

		html, err := s.fetchPageCtx(ctx, directURL)
		if err == nil {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
			if err == nil {
				result, err := s.parseDetailPage(doc, directURL, cleanID)
				if err == nil && hasDetailMetadata(result, cleanID) {
					logging.Debugf("JavDB: Found movie via direct URL: %s", directURL)
					return result, nil
				}
			}
		}
		logging.Debugf("JavDB: Direct URL lookup failed for '%s', falling back to search", cleanID)
	}

	detailURL, err := s.findDetailURLCtx(ctx, id)
	if err != nil {
		return nil, err
	}

	html, err := s.fetchPageCtx(ctx, detailURL)
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
	retryHTML, err := s.fetchPageDirectCtx(ctx, detailURL)
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

func (s *scraper) findDetailURLCtx(ctx context.Context, id string) (string, error) {
	searchURL, err := s.GetURL(ctx, id)
	if err != nil {
		return "", err
	}

	html, err := s.fetchPageCtx(ctx, searchURL)
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
				foundURL = scraperutil.ResolveURL(s.baseURL, href)
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
			detailLinks = append(detailLinks, scraperutil.ResolveURL(s.baseURL, href))
		}
	})
	if len(detailLinks) == 1 {
		return detailLinks[0], nil
	}

	return "", models.NewScraperNotFoundError("JavDB", fmt.Sprintf("movie %s not found on JavDB", id))
}

// validateFetchURL enforces the SSRF/allowed-host contract at the fetch
// boundary. targetURL can originate from parsed page links rather than the
// configured base URL, so every outbound fetch (direct Resty and FlareSolverr
// resolution) must re-validate it. The allow-list is the JavDB-owned hosts
// (javdb.com, *.javdb.com) plus the operator-configured base URL's host; a
// host on that list is trusted and allowed through. Any other host — e.g. a
// parsed link pointing at a private/loopback/link-local IP or a metadata
// endpoint — is rejected via ssrf.CheckURL and the host-allow check. This is
// the per-fetch defense-in-depth for the path instruction that
// internal/scraper/** outbound URLs be constrained to the source's allowed
// hosts.
func (s *scraper) validateFetchURL(targetURL string) error {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return models.NewScraperStatusError("JavDB", 0, "invalid URL: "+err.Error())
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return models.NewScraperStatusError("JavDB", 0, "non-http(s) scheme rejected: "+parsed.Scheme)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return models.NewScraperStatusError("JavDB", 0, "empty hostname")
	}
	// Allow JavDB-owned hosts and the operator-configured base URL host.
	if host == "javdb.com" || strings.HasSuffix(host, ".javdb.com") {
		return nil
	}
	if s.baseURL != "" {
		if base, perr := url.Parse(s.baseURL); perr == nil && strings.ToLower(base.Hostname()) == host {
			return nil
		}
	}
	// Arbitrary page-link host: reject private/loopback/link-local IPs and
	// non-allowed hosts.
	if err := ssrf.CheckURL(targetURL); err != nil {
		return models.NewScraperStatusError("JavDB", 0, err.Error())
	}
	return models.NewScraperStatusError("JavDB", 0, "non-JavDB host rejected: "+host)
}

func (s *scraper) fetchPageCtx(ctx context.Context, targetURL string) (string, error) {
	if err := s.validateFetchURL(targetURL); err != nil {
		return "", err
	}
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return "", err
	}

	resp, err := s.client.R().SetContext(ctx).Get(targetURL)
	if err == nil && resp != nil && resp.StatusCode() == 200 {
		html := resp.String()
		if !challengedetect.IsCloudflareChallengePage(html) {
			return html, nil
		}
		logging.Warnf("JavDB: Direct request returned Cloudflare challenge, escalating to FlareSolverr: %s", targetURL)
	} else if err == nil && resp != nil {
		logging.Debugf("JavDB: Direct request returned status %d for %s", resp.StatusCode(), targetURL)
	}

	if s.settings.UseFlareSolverr && s.flaresolverr != nil {
		logging.Debugf("JavDB: Resolving via FlareSolverr: %s", targetURL)
		html, cookies, fsErr := s.flaresolverr.ResolveURL(targetURL)
		if fsErr == nil {
			s.cookieMu.Lock()
			for _, c := range cookies {
				s.client.SetCookie(&c)
			}
			s.cookieMu.Unlock()
			if challengedetect.IsCloudflareChallengePage(html) {
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

func (s *scraper) fetchPageDirectCtx(ctx context.Context, targetURL string) (string, error) {
	if err := s.validateFetchURL(targetURL); err != nil {
		return "", err
	}
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return "", err
	}

	resp, err := s.client.R().SetContext(ctx).Get(targetURL)
	return s.fetchPageDirectResponse(resp, err)
}

func (s *scraper) fetchPageDirectResponse(resp *resty.Response, err error) (string, error) {
	if err != nil {
		// Wrap raw transport errors as a classified ScraperStatusError so request
		// details (URL/headers) are not leaked through API/job errors; keep the
		// raw detail only in debug logs.
		logging.Debugf("JavDB: request failed: %v", err)
		return "", models.NewScraperStatusError("JavDB", 0, "request failed: "+err.Error())
	}
	if resp.StatusCode() != 200 {
		return "", models.NewScraperStatusError(
			"JavDB",
			resp.StatusCode(),
			fmt.Sprintf("JavDB returned status code %d", resp.StatusCode()),
		)
	}

	html := resp.String()
	if challengedetect.IsCloudflareChallengePage(html) {
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

// ParseHTML parses a JavDB detail page from a goquery.Document.
// This is the documented parsing seam for testing; it delegates to the
// internal parseDetailPage method.
func (s *scraper) ParseHTML(doc *goquery.Document, sourceURL string) (*models.ScraperResult, error) {
	return s.parseDetailPage(doc, sourceURL, "")
}

// labelRoute is a single entry in the labelRouter: it matches a normalized DOM label
// to a setter closure that populates the appropriate field on the result.
type labelRoute struct {
	keys   []string // label substrings to match against
	handle func(label string, valueNode *goquery.Selection, valueText string)
}

// labelRouter maps normalized DOM labels to field-setter closures.
// The DOM loop in parseDetailPage becomes: normalize label → lookup in router → call setter.
type labelRouter struct {
	routes  []labelRoute
	result  *models.ScraperResult
	castCtx *castRouterContext
}

// castRouterContext tracks actress-parsing state across DOM block iterations.
type castRouterContext struct {
	hasFemaleActressRow bool
}

// newLabelRouter builds a router with all field handlers bound to the given result.
func newLabelRouter(result *models.ScraperResult) *labelRouter {
	castCtx := &castRouterContext{}
	r := &labelRouter{result: result, castCtx: castCtx}

	r.routes = []labelRoute{
		{keys: []string{"番號", "番号", "識別碼", "识别码", "ID"}, handle: func(_ string, _ *goquery.Selection, valueText string) {
			if result.ID == "" && valueText != "" {
				result.ID = valueText
			}
		}},
		{keys: []string{"日期", "發行日期", "发行日期", "release"}, handle: func(_ string, _ *goquery.Selection, valueText string) {
			if t := scraperutil.ParseDate(valueText); t != nil {
				result.ReleaseDate = t
			}
		}},
		{keys: []string{"時長", "长度", "長度", "runtime", "length", "duration"}, handle: func(_ string, _ *goquery.Selection, valueText string) {
			result.Runtime = parseRuntime(valueText)
		}},
		{keys: []string{"導演", "导演", "director"}, handle: func(_ string, valueNode *goquery.Selection, _ string) {
			result.Director = extractFirstText(valueNode)
		}},
		{keys: []string{"片商", "maker", "studio"}, handle: func(_ string, valueNode *goquery.Selection, _ string) {
			result.Maker = extractFirstText(valueNode)
		}},
		{keys: []string{"發行", "发行", "label", "publisher"}, handle: func(_ string, valueNode *goquery.Selection, _ string) {
			result.Label = extractFirstText(valueNode)
		}},
		{keys: []string{"系列", "series"}, handle: func(_ string, valueNode *goquery.Selection, _ string) {
			result.Series = extractFirstText(valueNode)
		}},
		{keys: []string{"評分", "评分", "rating", "score"}, handle: func(_ string, _ *goquery.Selection, valueText string) {
			result.Rating = parseRating(valueText)
		}},
		{keys: []string{"類別", "类别", "genre", "tag", "tags"}, handle: func(_ string, valueNode *goquery.Selection, _ string) {
			result.Genres = extractStringList(valueNode)
		}},
	}

	return r
}

// dispatch looks up the normalized label in the route table. If a route matches,
// it calls the setter. Returns true if a route handled the label.
func (lr *labelRouter) dispatch(label string, valueNode *goquery.Selection, valueText string) bool {
	for _, route := range lr.routes {
		if labelContains(label, route.keys...) {
			route.handle(label, valueNode, valueText)
			return true
		}
	}
	return false
}

// dispatchCast handles actress/cast label classification when no standard route matches.
func (lr *labelRouter) dispatchCast(label string, valueNode *goquery.Selection) {
	switch classifyCastLabel(label) {
	case castLabelFemale:
		if actresses := extractActresses(valueNode); len(actresses) > 0 {
			lr.result.Actresses = actresses
			lr.castCtx.hasFemaleActressRow = true
		}
	case castLabelGeneric:
		// Generic cast rows may include male actors. Use only as fallback
		// when no female-specific row was found.
		if lr.castCtx.hasFemaleActressRow || len(lr.result.Actresses) > 0 {
			return
		}
		if actresses := extractActresses(valueNode); len(actresses) > 0 {
			lr.result.Actresses = actresses
		}
	case castLabelMale:
		// Explicit male actor rows should not be merged into actresses.
	}
}

func (s *scraper) parseDetailPage(doc *goquery.Document, sourceURL, fallbackID string) (*models.ScraperResult, error) {
	result := &models.ScraperResult{
		Source:    s.Name(),
		SourceURL: sourceURL,
		Language:  "ja",
	}

	titleNode := doc.Find(".title.is-4").First()
	fullTitle := scraperutil.CleanString(titleNode.Text())
	idFromTitle := scraperutil.CleanString(titleNode.Find("strong").First().Text())
	if idFromTitle != "" {
		result.ID = idFromTitle
	}

	if fullTitle != "" && result.ID != "" {
		fullTitle = strings.TrimSpace(strings.TrimPrefix(fullTitle, result.ID))
	}

	if fullTitle == "" {
		fullTitle = scraperutil.CleanString(doc.Find("meta[property='og:title']").AttrOr("content", ""))
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

	description := scraperutil.CleanString(doc.Find("span[itemprop='description']").First().Text())
	if description == "" {
		description = scraperutil.CleanString(doc.Find(".movie-panel-info .movie-description").First().Text())
	}
	result.Description = description

	router := newLabelRouter(result)

	doc.Find(".movie-panel-info .panel-block").Each(func(_ int, block *goquery.Selection) {
		label := normalizeLabel(block.Find("strong").First().Text())
		valueNode := block.Find(".value").First()
		if valueNode.Length() == 0 {
			valueNode = block
		}
		valueText := scraperutil.CleanString(valueNode.Text())

		if !router.dispatch(label, valueNode, valueText) {
			router.dispatchCast(label, valueNode)
		}
	})

	if result.ID == "" {
		result.ID = fallbackID
	}
	result.ID = scraperutil.CleanString(result.ID)
	result.ContentID = result.ID

	if result.Title == "" {
		result.Title = result.ID
		result.OriginalTitle = result.ID
	}

	return result, nil
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

func normalizeLabel(s string) string {
	s = scraperutil.CleanString(s)
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
	if text := scraperutil.CleanString(sel.Find("a").First().Text()); text != "" {
		return text
	}
	return scraperutil.CleanString(sel.Text())
}

func parseRuntime(s string) int {
	matches := runtimeRegex.FindStringSubmatch(scraperutil.CleanString(s))
	if len(matches) < 2 {
		return 0
	}
	v, _ := strconv.Atoi(matches[1])
	return v
}

func parseRating(s string) *models.Rating {
	s = scraperutil.CleanString(s)
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
		name := scraperutil.CleanString(a.Text())
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
			// JavDB doesn't expose real DMM actress IDs.
			// Keep unknown as zero and let downstream matching use names.
			DMMID:        0,
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
				DMMID:        0,
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
	context := strings.ToLower(scraperutil.CleanString(sel.Parent().Text()))
	if context == "" {
		context = strings.ToLower(scraperutil.CleanString(sel.Text()))
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

func extractStringList(sel *goquery.Selection) []string {
	values := make([]string, 0)
	seen := make(map[string]bool)

	sel.Find("a").Each(func(_ int, a *goquery.Selection) {
		v := scraperutil.CleanString(a.Text())
		if v != "" && !seen[v] {
			seen[v] = true
			values = append(values, v)
		}
	})
	if len(values) > 0 {
		return values
	}

	raw := scraperutil.CleanString(sel.Text())
	if raw == "" || isNotAvailableValue(raw) {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '/' || r == '、'
	})
	for _, p := range parts {
		v := scraperutil.CleanString(p)
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
				return scraperutil.ResolveURL(baseURL, val)
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
		u := scraperutil.ResolveURL(baseURL, raw)
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
			return scraperutil.ResolveURL(baseURL, src)
		}
	}
	return ""
}
