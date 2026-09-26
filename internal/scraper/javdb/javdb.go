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

	// Separator-pinned remaster identity: the series shape a separator-
	// bearing display id pins in its first segment, the compact
	// series/number split whose t28 branch decodes compact spellings
	// under the shared t28 rule, and the raw channel-prefixed cid shape
	// whose h_/n_ letter is maker junk, not the series.
	remasterSeriesSegmentRegex = regexp.MustCompile(`^\d*(?:t28|[a-z]+)$`)
	remasterCompactSplitRegex  = regexp.MustCompile(`^(\d*)(T28|[A-Z]+)(\d+)$`)
	remasterUnderscoreCIDRegex = regexp.MustCompile(`^[hn]_\d+(?:t28|[a-z]+)\d+[a-z]{0,3}$`)
	// The raw cid shape a separator-free spelling takes when it is a
	// content id rather than a display id — the raw-vs-display distinction
	// the AI number-free exemption keys on (see isRawRemasterCIDShape),
	// mirroring the DMM/R18 classifiers' shape (rawRemasterCIDShapeRegex):
	// a catalog-digit prefix, a five-digit zero-padded number (the leading
	// zero is the padding evidence — display numbers never carry one), or
	// the prefix-free t28 tail whose compact display spelling doubles as
	// the raw cid (t28123h is T-28123H). A non-padded five-digit
	// separator-free form (abc12345h) is a display id whose server cid is
	// catalog-prefixed, so it must not read as raw.
	remasterRawCIDShapeRegex = regexp.MustCompile(`^(?:\d+(?:t28|[a-z]+)\d+[a-z]{0,3}|(?:t28(?:0\d{4}|\d{3})|[a-z]+0\d{4})[a-z]{0,3})$`)
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
			return nil, models.NewScraperNotFoundError("JavDB", fmt.Sprintf("non-detail content for %s", urlStr))
		}
	}

	logging.Debugf("JavDB ScrapeURL: Successfully scraped %s (ID=%s, Title=%s)", redactLogURL(urlStr), result.ID, result.Title)
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
				if err == nil && hasDetailMetadata(result, cleanID) && detailIdentityMatchesQuery(result, cleanID) {
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
		if !detailIdentityMatchesQuery(result, id) {
			// The selected detail page publishes another release's identity
			// than the marker query's — the post-fetch guard the DMM side's
			// pageDisplayIdentityMatchesQuery performs (see
			// detailIdentityMatchesQuery): miss honestly instead of returning
			// that release's metadata.
			return nil, models.NewScraperNotFoundError("JavDB", fmt.Sprintf("JavDB page for %s publishes a different release", id))
		}
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
	if !detailIdentityMatchesQuery(retryResult, id) {
		// The retried page publishes another release's identity too: the
		// same post-fetch guard as the primary parse above.
		return nil, models.NewScraperNotFoundError("JavDB", fmt.Sprintf("JavDB page for %s publishes a different release", id))
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
	// A marker-suffixed display ID (the folded spelling the matcher
	// propagates for HD/AI remaster queries, e.g. RCT-156H from RCT-156-HD,
	// including the E/Z-catalog-suffixed spellings like IPX-535ZH) targets
	// the remaster release itself: a variant match or the single-link
	// fallback would silently return the base release's page, so only
	// exact/padding-equal identities qualify — the raw AI cid number-free
	// exemption below included — and the query misses honestly when the
	// remaster is not among the results.
	markerQuery := remasterMarkerSuffix(id) != ""
	// Marker queries compare the separator-pinned series, number and
	// folded marker class rather than raw normalized strings: JavDB lists
	// the same HD remaster under either marker spelling (RCT-156H and
	// RCT-156-HD), so HD folds to H — mirroring the matcher's
	// foldRemasterMarker — while AI stays its own class. The E/Z catalog
	// suffix letter rides along the fold (IPX-535-Z-HD and IPX-535ZH fold
	// alike) and stays part of the compared identity. The fold key pins
	// the series/number boundary before punctuation is discarded, as the
	// DMM/R18 identity code does: T-28123H — and its compact t28123h
	// spelling, per the shared t28 rule — folds to the key T+28123, never
	// the T28+123 identity of the distinct T28-123H release, so a search
	// result for that other series does not rank as a match. The base
	// release carries no marker and still compares unequal; only the
	// marker spelling is bridged. Raw marker-bearing content ids the
	// matcher's tier-2 propagates fold onto the display listing too: the
	// leading h_/n_ channel letter strips first (see
	// stripRemasterChannelPrefix), then the catalog/channel prefix digits
	// (1rct00156h -> the RCT+156H-class identity, h_003abc00123hd -> the
	// ABC+123H-class one; see stripCompactCatalogPrefix), so the RCT-156-HD
	// and ABC-123-HD listings rank as its match.
	// Raw AI cids get one further bridge: their number is a server slot,
	// not a display number (dv00899ai is DV-818AI), so a raw AI cid query
	// compares series, catalog suffix and AI marker class against a display
	// listing without the number — the number-free exception the DMM/R18
	// identity code already applies (rawDisplayMatchesCID /
	// pageDisplayIdentityForCID). Raw H/HD cids keep binding the number
	// (1rct00156h is RCT-156H, and 1rct00157h must not match RCT-156-HD),
	// display AI queries keep binding it too, and a raw cid listing facing
	// a raw cid query still binds literally — only the raw-cid-query vs
	// display-listing direction is exempt (see remasterFoldMatchRank).
	var foldedTargetKey remasterFoldKey
	if markerQuery {
		foldedTargetKey = foldRemasterMarkerKey(id)
	}
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
			var match idMatchType
			if markerQuery {
				// Marker queries compare the separator-pinned identity +
				// folded marker class: H and HD both spell the HD
				// remaster, so an HD-labeled listing matches the folded
				// H query and vice versa, while the pinned series and
				// number keep compaction-equal spellings of different
				// releases apart — the raw AI cid query's number-free
				// exemption aside (see remasterFoldMatchRank).
				match = remasterFoldMatchRank(c, foldedTargetKey)
			} else {
				match = idMatchRank(c, targetID)
			}
			if markerQuery && match == idMatchVariant {
				// The trailing H/HD/AI is a remaster marker, not a variant
				// suffix: the base release must not stand in for the remaster.
				match = idMatchNone
			}
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
	// Marker queries skip the fallback too: a lone listed item cannot be
	// assumed to be the requested remaster (DV-818 must not stand in for
	// DV-818AI).
	if len(detailLinks) == 1 && !markerQuery {
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

// redactLogURL strips userinfo and secret query parameters from a URL for
// safe logging. Non-secret identifiers (id, v, sn, keyword) are preserved.
func redactLogURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = nil
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if lk == "keyword" {
			continue
		}
		for _, frag := range []string{"sign", "secret", "token", "pass", "pwd", "cred", "auth", "session", "oauth", "key"} {
			if strings.Contains(lk, frag) {
				q.Del(k)
				break
			}
		}
	}
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	} else {
		u.RawQuery = ""
	}
	return u.String()
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
		logging.Warnf("JavDB: Direct request returned Cloudflare challenge, escalating to FlareSolverr: %s", redactLogURL(targetURL))
	} else if err == nil && resp != nil {
		logging.Debugf("JavDB: Direct request returned status %d for %s", resp.StatusCode(), redactLogURL(targetURL))
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

// detailIdentityMatchesQuery reports whether the parsed detail result's
// published ID satisfies the query's remaster identity — the post-fetch
// guard the DMM side performs on its fetched pages
// (pageDisplayIdentityMatchesQuery, rounds 16a/18/20a). findDetailURLCtx
// validates only the ID the SEARCH card showed, so a stale or redirected
// detail URL can serve another release's page: an RCT-156H query whose
// selected link serves RCT-156 (the base) or RCT-157-HD (another
// remaster) must miss honestly instead of returning that release's
// metadata under the query's identity. The comparison rides the same fold
// machinery the search-card check uses (foldRemasterMarkerKey /
// remasterFoldMatchRank): H and HD spell the same remaster (an RCT-156H
// query accepts a page serving RCT-156-HD), the pinned series/number
// boundary, E/Z catalog suffix and marker class stay bound, and the
// variant rung still rejects the base release. The round-42 AI slot
// exemption rides the same rank function rather than a stricter literal
// comparison: a raw AI cid query (dv00899ai) accepts the display listing
// it resolved to (DV-818AI) through the number-free rung, while raw H/HD
// cids and display queries keep binding the number. Non-marker queries
// carry no remaster identity to validate and keep the existing behavior,
// as do results that publish no ID of their own: the page-served fallback
// spelling (the query itself) ranks exact.
func detailIdentityMatchesQuery(result *models.ScraperResult, query string) bool {
	if remasterMarkerSuffix(query) == "" {
		return true
	}
	if result == nil || strings.TrimSpace(result.ID) == "" {
		return true
	}
	rank := remasterFoldMatchRank(result.ID, foldRemasterMarkerKey(query))
	return rank == idMatchExact || rank == idMatchNormalized
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
	result.ShouldCropPoster = true
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

// remasterMarkerSuffix reports the remaster marker spelling (H, HD or AI) a
// display ID carries at the end of its release number, behind the optional
// E/Z catalog suffix letter, e.g. "RCT-156H" -> "H", "IPX-535ZH" -> "H" (Z
// catalog suffix), "IPX-535-Z-HD" -> "HD" (the same Z-suffixed H-class
// release), "DV-818AI" -> "AI". The matcher folds HD/AI spellings into
// exactly this position when it propagates a remaster query, so a non-empty
// result means the query targets a remaster release: its trailing letters
// must never be read as a variant suffix of the base release, and the E/Z
// suffix letter stays part of the release identity.
func remasterMarkerSuffix(id string) string {
	_, _, marker, ok := splitRemasterMarkerTail(normalizeIDForCompare(id))
	if !ok {
		return ""
	}
	return marker
}

// splitRemasterMarkerTail splits a normalized comparison id into the base
// (series and release number), the optional E/Z catalog suffix letter, and
// the leading marker spelling of its trailing remaster tail. The tail
// grammar mirrors the matcher's: an optional E or Z catalog suffix rides
// immediately in front of the terminal marker, so IPX-535ZH, IPX-535-Z-HD
// and IPX-535-ZHD all carry the same Z-suffixed release, and a display
// spelling may repeat the marker after the folded one (IPX-535-ZH-HD).
// ok is false when the letters carry no marker at all (IPX-535Z is the
// suffix-only base release, IPX-535A a part letter) or when no release
// number precedes them.
func splitRemasterMarkerTail(normalized string) (base, catalogSuffix, marker string, ok bool) {
	// Locate the trailing ASCII letter run; the release number must sit
	// immediately before it for the letters to be a marker tail.
	runStart := len(normalized)
	for runStart > 0 && normalized[runStart-1] >= 'A' && normalized[runStart-1] <= 'Z' {
		runStart--
	}
	if runStart == len(normalized) || runStart == 0 {
		return "", "", "", false
	}
	run := normalized[runStart:]
	// The optional E/Z catalog suffix is parsed separately from the
	// terminal marker: a bare E/Z without a following marker spells the
	// suffix-only base release and carries no marker.
	rest := run
	if rest[0] == 'E' || rest[0] == 'Z' {
		catalogSuffix = rest[:1]
		rest = rest[1:]
	}
	marker, rest, ok = takeRemasterMarkerSpelling(rest)
	if !ok {
		return "", "", "", false
	}
	// A display spelling may repeat the marker behind the folded one
	// (IPX-535-ZH-HD), so redundant trailing marker spellings collapse
	// into the same tail; any other trailing letters end it, keeping
	// run-of-the-mill part letters out of the marker class.
	for rest != "" {
		if _, rest, ok = takeRemasterMarkerSpelling(rest); !ok {
			return "", "", "", false
		}
	}
	return normalized[:runStart], catalogSuffix, marker, true
}

// takeRemasterMarkerSpelling strips one leading H/HD/AI marker spelling
// from rest, reporting whether the head is a marker at all. HD is tried
// before H so the two-letter spelling is not split in two.
func takeRemasterMarkerSpelling(rest string) (marker, remaining string, ok bool) {
	switch {
	case strings.HasPrefix(rest, "HD"):
		return "HD", rest[2:], true
	case strings.HasPrefix(rest, "AI"):
		return "AI", rest[2:], true
	case strings.HasPrefix(rest, "H"):
		return "H", rest[1:], true
	default:
		return "", rest, false
	}
}

// foldRemasterMarkerID rewrites an id's trailing remaster tail to its
// folded equivalence-class spelling, mirroring the matcher's
// foldRemasterMarker (HD -> H): H and HD both spell the HD remaster while
// AI stays its own class, the optional E/Z catalog suffix letter survives
// the fold as part of the identity, and redundant trailing marker spellings
// collapse — IPX-535ZH, IPX-535-Z-HD and IPX-535-ZH-HD all fold to
// IPX535ZH. This lets a marker-carrying query match a candidate whose marker
// spelling differs only within the H/HD class (RCT-156H vs RCT-156-HD); the
// base release (no marker) and the suffix-only release (IPX-535Z) still
// compare unequal. The compact spelling discards punctuation, so the
// series/number identity it names belongs to remasterFoldKey: ids whose
// boundary pins (T-28123H vs T28-123H) compare through the pinned key, and
// this spelling serves the forms whose grammar does not pin.
func foldRemasterMarkerID(id string) string {
	normalized := normalizeIDForCompare(id)
	base, catalogSuffix, marker, ok := splitRemasterMarkerTail(normalized)
	if !ok {
		return normalized
	}
	return base + catalogSuffix + foldRemasterMarkerClass(marker)
}

// foldRemasterMarkerClass folds a marker spelling into its comparison
// class: H and HD both spell the HD remaster, while AI stays its own.
func foldRemasterMarkerClass(marker string) string {
	if marker == "AI" {
		return "AI"
	}
	return "H"
}

// remasterFoldKey is a marker query's folded identity: the separator-pinned
// series and number parsed BEFORE punctuation is discarded, the E/Z catalog
// suffix that survives the fold, and the folded marker class. pinned is true
// only when the id's grammar exposes that series/number boundary; compact
// keeps the round-30 folded spelling the unpinned forms — spellings whose
// series the segment regexes do not model — still compare under. rawCID
// marks that the folded id spells a raw DMM content id rather than a
// display spelling (see isRawRemasterCIDShape): remasterFoldMatchRank's
// AI number-free exemption consults it on the query side, and the literal
// cid binding consults it on the listing side.
type remasterFoldKey struct {
	series        string
	number        string
	catalogSuffix string
	marker        string
	compact       string
	pinned        bool
	rawCID        bool
}

// foldRemasterMarkerKey parses a display id's remaster identity with the
// series/number boundary pinned before punctuation is discarded, as the
// DMM/R18 identity code does (displayIdentityTuple / r18ParseRemasterTail):
// a separator-bearing id takes its first separator-delimited segment as the
// series — T-28123H is T+28123 while T28-123H is T28+123 — and a compact id
// decodes through the t28-first split under the shared t28 rule, so a
// prefix-free t28123h reads as the T-series T+28123 while its
// catalog-prefixed spellings (9t28123h) keep the T28 label's T28+123. A
// compact id's leading 1-4 digit run is a DMM catalog/channel prefix, not
// part of the series (1rct00156h is the RCT-156H remaster's cid), so it is
// stripped from the pinned series the same way the DMM/R18 classifiers read
// the spelling — see stripCompactCatalogPrefix — and a channel-prefixed
// raw cid first drops its leading h_/n_ maker letter
// (h_003abc00123hd -> ABC+00123+H; see stripRemasterChannelPrefix). The
// tail grammar is splitRemasterMarkerTail's: the E/Z catalog
// suffix rides in front of the marker, redundant marker spellings collapse
// and HD folds into the H class. Ids whose grammar does not pin — no marker
// tail, or a series/number split the regexes do not model — stay unpinned
// and compare under the round-30 compact fold. The key additionally
// records whether the id spells a raw content id rather than a display
// spelling (rawCID, see isRawRemasterCIDShape) — the distinction the AI
// number-free exemption in remasterFoldMatchRank keys on.
func foldRemasterMarkerKey(id string) remasterFoldKey {
	// A raw channel-prefixed content id (h_003abc00123hd) carries maker
	// junk in front of its identity: the h_/n_ channel letter never spells
	// the series, and the underscore it sits behind would pin a phantom
	// single-letter series segment before the compact split ever reads the
	// cid. Strip the channel prefix the same way the DMM/R18 classifiers
	// read the spelling (underscoreCIDShapeRegex / r18PrefixedCIDRegex),
	// so the stripped cid composes with the round-40b catalog-prefix rules
	// below: h_003abc00123hd pins ABC+00123+H, the identity the ABC-123-HD
	// display listing carries.
	trimmed := strings.ToLower(strings.TrimSpace(id))
	lower := stripRemasterChannelPrefix(trimmed)
	key := remasterFoldKey{
		compact: foldRemasterMarkerID(lower),
		rawCID:  isRawRemasterCIDShape(trimmed),
	}
	// Separator-bearing spellings pin the series boundary that compaction
	// erases; a remainder the tail grammar cannot read as the bare number
	// plus marker falls through to the compact split, mirroring DMM.
	if series, rest, ok := remasterSeparatorSeriesTail(lower); ok {
		if number, suffix, marker, ok := remasterPinnedNumberTail(rest); ok {
			key.pin(series, number, suffix, marker)
			return key
		}
	}
	normalized := normalizeIDForCompare(lower)
	base, suffix, marker, ok := splitRemasterMarkerTail(normalized)
	if !ok {
		return key
	}
	if m := remasterCompactSplitRegex.FindStringSubmatch(base); m != nil {
		// The t28 branch mirrors the rule the matcher, DMM and R18
		// classifiers decode by (parseRemasterTail / r18ParseRemasterTail):
		// a genuinely compact, prefix-free t28 with a three- or four-digit
		// number reads as the T-series release — t28123h is T+28123 and
		// t281234h is T+281234, the six-digit run the matcher's re-anchoring
		// grammar decodes the same way, the release a manual search for
		// either spelling targets on every source (see
		// t28TailDecodesTSeries) — while the catalog digits prefixing a
		// t28 cid (9t28123h, 55t28123h) are maker junk that leaves the T28
		// label's T28+123 identity intact. Separator-bearing spellings never
		// reinterpret: their series boundary is pinned — or falls through
		// unresolved — above.
		if m[2] == "T28" {
			if m[1] == "" && t28TailDecodesTSeries(m[3]) && !strings.ContainsAny(lower, "-_. ") {
				key.pin("T", "28"+m[3], suffix, marker)
			} else {
				key.pin("T28", m[3], suffix, marker)
			}
		} else {
			// A leading 1-4 digit run on a compact marker-bearing id is a
			// DMM catalog/channel prefix, not part of the series — every
			// real prefixed cid's series part is letters-only, and the
			// DMM/R18 classifiers read the same spelling through the same
			// split — so 1rct00156h pins RCT+00156+H, the identity the
			// RCT-156-HD display listing carries, instead of a 1RCT series
			// no listing spells. stripCompactCatalogPrefix bounds the
			// ambiguous digit runs.
			key.pin(stripCompactCatalogPrefix(m[1], m[2]), m[3], suffix, marker)
		}
	}
	return key
}

// t28TailDecodesTSeries reports whether a genuinely compact, prefix-free
// t28 tail of this digit count reads as the T series rather than the T28
// label: three-digit tails spell the five-digit T-series numbers (t28123h
// is T+28123) and four-digit tails the six-digit ones (t281234h is T+281234,
// the digit run the matcher's re-anchoring grammar decodes the same way),
// while zero-padded five-digit tails (t2800123h) are the T28 label's padded
// cids.
func t28TailDecodesTSeries(tail string) bool {
	return len(tail) == 3 || len(tail) == 4
}

// maxCatalogPrefixDigits bounds the digit run stripCompactCatalogPrefix
// treats as a DMM catalog/channel prefix: the prefixes real prefixed cids
// carry (the 1 in 1rct00156, the 9/55/118/874/1038 on the t28 cids) never
// run past four digits.
const maxCatalogPrefixDigits = 4

// stripCompactCatalogPrefix removes a compact id's leading DMM catalog or
// channel prefix digits from its series when the digit run is unambiguous
// maker junk. The catalog prefixes real prefixed cids carry (1rct00156h,
// 118ipx00535h) run 1-4 digits and the series parts behind them are
// letters-only in the r18.dev content-id prefix lookup, so a 1-4 digit run
// is never part of the series — the DMM/R18 classifiers read the same
// spellings through the same split (parseRemasterTail /
// r18ParseRemasterTail return the digit-free series group), and the raw
// content ids the matcher's tier-2 propagates must fold onto the display
// listing's identity (1rct00156h -> RCT+00156+H, matching RCT-156-HD).
// Longer digit runs match no known catalog prefix, so they stay glued to
// the series and an ambiguous spelling is not over-stripped.
func stripCompactCatalogPrefix(digits, series string) string {
	if len(digits) > 0 && len(digits) <= maxCatalogPrefixDigits {
		return series
	}
	return digits + series
}

// stripRemasterChannelPrefix removes a raw content id's leading h_/n_
// channel prefix (with the letter's underscore) when the lowercase id has
// the underscore cid shape: h_003abc00123hd -> 003abc00123hd. The channel
// letter is maker junk — the DMM/R18 classifiers read the same spelling
// through their prefixed-cid regexes (underscoreCIDShapeRegex /
// r18PrefixedCIDRegex) and never treat it as the series — while the
// catalog digit run it fronts stays for stripCompactCatalogPrefix to
// bound, composing round 40b's rules. Spellings the cid shape does not
// model keep every character: display ids and ambiguous forms are
// untouched.
func stripRemasterChannelPrefix(lower string) string {
	if remasterUnderscoreCIDRegex.MatchString(lower) {
		return lower[2:]
	}
	return lower
}

// isRawRemasterCIDShape reports whether a lowercased, trimmed marker-bearing
// id spells a raw DMM content id rather than a display id — the raw-vs-
// display distinction remasterFoldMatchRank's AI number-free exemption
// keys on. The shape mirrors the DMM/R18 classifiers' raw-cid rules
// (r18dev's isRawRemasterContentIDQuery / rawRemasterCIDShapeRegex): the
// h_/n_ channel-prefixed cid shape is raw outright, any separator (-, _
// . or space) pins a display spelling, and a separator-free form is raw
// when it carries a catalog-digit prefix, a five-digit zero-padded number
// (the leading zero is the padding evidence — display numbers never carry
// one) or the prefix-free t28 tail whose compact display spelling doubles
// as the raw cid (t28123h is T-28123H). A non-padded five-digit
// separator-free form (abc12345h) is a display id whose server cid is
// catalog-prefixed, so it must not read as raw.
func isRawRemasterCIDShape(lower string) bool {
	if remasterUnderscoreCIDRegex.MatchString(lower) {
		return true
	}
	if strings.ContainsAny(lower, "-_. ") {
		return false
	}
	return remasterRawCIDShapeRegex.MatchString(lower)
}

// pin fills the key's pinned identity, folding the marker spelling into its
// comparison class and uppercasing the separator-pinned series so pinned
// keys and compact-decoded keys compare alike.
func (k *remasterFoldKey) pin(series, number, catalogSuffix, marker string) {
	k.series = strings.ToUpper(series)
	k.number = number
	k.catalogSuffix = catalogSuffix
	k.marker = foldRemasterMarkerClass(marker)
	k.pinned = true
}

// remasterSeparatorSeriesTail splits a separator-bearing display id into its
// series segment and the joined remainder. Separators pin the series
// boundary that compaction erases — T-28123-HD is series T number 28123,
// not the T28+123 identity the compact form T28123HD decodes to. ok is
// false when the id carries no separator or its first segment is not a
// series shape (FC2-PPV spellings keep the compact comparison).
func remasterSeparatorSeriesTail(lower string) (series, rest string, ok bool) {
	if !strings.ContainsAny(lower, "-_. ") {
		return "", "", false
	}
	parts := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == ' '
	})
	if len(parts) < 2 || !remasterSeriesSegmentRegex.MatchString(parts[0]) {
		return "", "", false
	}
	return parts[0], strings.Join(parts[1:], ""), true
}

// remasterPinnedNumberTail splits the separator-pinned remainder into the
// release number, E/Z catalog suffix and marker spelling through the same
// tail grammar splitRemasterMarkerTail applies, requiring the base to be
// the bare release number: a remainder carrying letters of its own
// (IPX-PPV-535H) does not pin and falls back to the compact comparison.
func remasterPinnedNumberTail(rest string) (number, catalogSuffix, marker string, ok bool) {
	base, suffix, spelling, tailOK := splitRemasterMarkerTail(strings.ToUpper(rest))
	if !tailOK || !allDigits(base) {
		return "", "", "", false
	}
	return base, suffix, spelling, true
}

// allDigits reports whether s is a nonempty run of ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// trimRemasterNumberPadding normalizes a release number's leading zeros for
// the padding-equal rank — RCT-0156-HD and RCT-156H number the same
// release — mirroring the DMM identity code's trimDisplayZeros.
func trimRemasterNumberPadding(number string) string {
	trimmed := strings.TrimLeft(number, "0")
	if trimmed == "" {
		return "0"
	}
	return trimmed
}

// remasterFoldMatchRank ranks a candidate listing id against a marker
// query's folded key. With both sides pinned, the separator-pinned series,
// catalog suffix, marker class and number compare directly — exact when
// the spellings agree, padding-normalized when only the number's leading
// zeros differ — so T-28123H and T28-123H never cross-match even though
// their compact spellings coincide. A raw AI cid target adds one
// number-free rung below exact: AI cid numbers are server slots that
// diverge from the display number (dv00899ai is DV-818AI), so a raw AI
// cid query accepts a same-series, same-suffix, same-class display
// listing without the number — the DMM/R18 precedent's exception
// (rawDisplayMatchesCID / pageDisplayIdentityForCID) — while raw H/HD
// queries, display queries of every class and raw cid listings facing
// raw cid queries all keep binding the number (different slots are
// different releases, mirroring r18dev's rawRemasterCIDEqual). Any unpinned
// side keeps the round-30 compact comparison instead; its variant rung
// still dies at the caller, where the base release must not stand in
// for the remaster.
func remasterFoldMatchRank(candidate string, target remasterFoldKey) idMatchType {
	c := foldRemasterMarkerKey(candidate)
	if target.pinned && c.pinned {
		if c.series != target.series || c.catalogSuffix != target.catalogSuffix || c.marker != target.marker {
			return idMatchNone
		}
		if c.number == target.number {
			return idMatchExact
		}
		if trimRemasterNumberPadding(c.number) == trimRemasterNumberPadding(target.number) {
			return idMatchNormalized
		}
		if target.rawCID && target.marker == "AI" && !c.rawCID {
			// The raw AI number-free exemption: the slot number carries
			// no display information (dv00899ai is DV-818AI), so any
			// same-series, same-suffix, same-class display listing
			// satisfies the identity — the honest reading the DMM/R18
			// identity code already applies (rawDisplayMatchesCID /
			// pageDisplayIdentityForCID). The rank stays below exact so
			// a listing numbering the cid's own digits still outranks
			// it, and the candidate guard keeps raw cid listings on
			// the literal comparison: a different slot is a different
			// release.
			return idMatchNormalized
		}
		return idMatchNone
	}
	return idMatchRank(c.compact, target.compact)
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
		name           string
		actorID        string
		genderHint     string // "female", "male", or ""
		maleHeuristic  bool
		explicitFemale bool
		thumbURL       string
	}
	candidates := make([]actressCandidate, 0)
	usedMarkers := make(map[*html.Node]bool)

	sel.Find("a").Each(func(_ int, a *goquery.Selection) {
		name := scraperutil.CleanString(a.Text())
		if name == "" || seen[name] {
			return
		}
		actorID := javdbActorIDFromLink(a)
		genderHint := genderHintForAnchor(a, usedMarkers)
		candidates = append(candidates, actressCandidate{
			name:           name,
			actorID:        actorID,
			genderHint:     genderHint,
			maleHeuristic:  isLikelyMaleActorLink(a),
			explicitFemale: genderHint == "female" || hasExplicitFemaleMarker(a),
			thumbURL:       javdbActorThumbURL(a, actorID),
		})
	})

	// javdb.com marks every female performer (actor-female, ♀, data-gender) and
	// leaves male co-stars entirely unmarked. When a panel advertises female
	// markers, an unmarked row is therefore male. Panels with no gender
	// information at all keep unmarked rows so mirrors never lose actresses.
	panelAdvertisesFemale := false
	for _, c := range candidates {
		if c.explicitFemale {
			panelAdvertisesFemale = true
			break
		}
	}

	for _, c := range candidates {
		if c.genderHint == "male" {
			continue
		}
		if c.genderHint == "" && c.maleHeuristic {
			continue
		}
		if !c.explicitFemale && panelAdvertisesFemale {
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
			ThumbURL:     c.thumbURL,
		})
	}

	// Fallback to plain text parsing when the panel exposes no links at all.
	// Once link candidates existed they were already gender-filtered, so using
	// the unfiltered text list would re-introduce rows that were dropped as male.
	if len(actresses) == 0 && len(candidates) == 0 {
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

// javdbActorIDFromLink extracts the JavDB actor ID from an <a href="/actors/XXX"> link.
// Returns "" when the link is not an actor profile link.
func javdbActorIDFromLink(a *goquery.Selection) string {
	href, ok := a.Attr("href")
	if !ok {
		return ""
	}
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	path := href
	if parsed, err := url.Parse(href); err == nil {
		path = parsed.Path
	} else if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	const prefix = "/actors/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	return strings.Trim(strings.TrimPrefix(path, prefix), "/")
}

// javdbActorAvatarURL builds the avatar image URL for a JavDB actor ID.
// JavDB stores actor avatars at https://c0.jdbstatic.com/avatars/<pp>/<ID>.jpg
// where <pp> is the first two characters of the ID, lowercased. Returns ""
// when the ID is too short to form a valid path.
func javdbActorAvatarURL(actorID string) string {
	if len(actorID) < 2 {
		return ""
	}
	prefix := strings.ToLower(actorID[:2])
	return fmt.Sprintf("https://c0.jdbstatic.com/avatars/%s/%s.jpg", prefix, actorID)
}

// javdbActorThumbURL resolves an actress thumbnail. Mirrors often render the
// avatar directly inside the actor link, so prefer that image when present and
// fall back to the avatar path derived from the actor ID.
func javdbActorThumbURL(a *goquery.Selection, actorID string) string {
	if a != nil && a.Length() > 0 {
		if img := a.Find("img").First(); img.Length() > 0 {
			for _, attr := range []string{"data-src", "data-original", "data-lazy-src", "src"} {
				if absolute := javdbAbsoluteImageURL(img.AttrOr(attr, "")); absolute != "" {
					return absolute
				}
			}
		}
	}
	return javdbActorAvatarURL(actorID)
}

// javdbAbsoluteImageURL normalizes mirrored image attributes into an absolute
// https URL, rejecting lazy-load placeholders and inline data URIs.
func javdbAbsoluteImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "data:") {
		return ""
	}
	for _, placeholder := range []string{"blank.gif", "placeholder", "loading.gif", "noavatar"} {
		if strings.Contains(lower, placeholder) {
			return ""
		}
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "/") {
		return defaultBaseURL + raw
	}
	return ""
}

func isLikelyMaleActorLink(sel *goquery.Selection) bool {
	classAttr := strings.ToLower(sel.AttrOr("class", ""))
	if hasWordToken(classAttr, "male") && !hasWordToken(classAttr, "female") {
		return true
	}

	for _, attr := range []string{"data-gender", "gender", "title", "aria-label"} {
		v := strings.ToLower(strings.TrimSpace(sel.AttrOr(attr, "")))
		if hasWordToken(v, "male") || strings.Contains(v, "男優") || strings.Contains(v, "男演员") || strings.Contains(v, "男演員") {
			return true
		}
	}

	// Only the anchor's own text and the text directly touching it count as
	// gender evidence. Scanning the enclosing panel would label every row male
	// as soon as one co-star carries a ♂ marker and the ♀ marker is absent.
	if hasMaleToken(scraperutil.CleanString(sel.Text())) {
		return true
	}
	for _, forward := range []bool{false, true} {
		if adjacentTextHasMaleToken(sel, forward) {
			return true
		}
	}

	return false
}

// hasExplicitFemaleMarker reports a female gender signal on the anchor itself:
// javdb.com renders actresses as <a class="actor-female"> while male co-stars
// carry no class at all.
func hasExplicitFemaleMarker(sel *goquery.Selection) bool {
	if sel == nil || sel.Length() == 0 {
		return false
	}
	if hasWordToken(strings.ToLower(sel.AttrOr("class", "")), "female") {
		return true
	}
	for _, attr := range []string{"data-gender", "gender", "title", "aria-label"} {
		v := strings.ToLower(strings.TrimSpace(sel.AttrOr(attr, "")))
		if hasWordToken(v, "female") || strings.Contains(v, "女優") {
			return true
		}
	}
	return false
}

// hasMaleToken reports male gender words in a single string.
func hasMaleToken(text string) bool {
	if text == "" {
		return false
	}
	lowered := strings.ToLower(text)
	return strings.Contains(lowered, "♂") ||
		hasWordToken(lowered, "male") ||
		strings.Contains(lowered, "男優") ||
		strings.Contains(lowered, "男演员") ||
		strings.Contains(lowered, "男演員")
}

// adjacentTextHasMaleToken inspects the text siblings touching the anchor,
// stopping at the next element so neighbouring rows cannot leak markers.
func adjacentTextHasMaleToken(sel *goquery.Selection, forward bool) bool {
	if sel == nil || len(sel.Nodes) == 0 {
		return false
	}
	step := func(n *html.Node) *html.Node {
		if forward {
			return n.NextSibling
		}
		return n.PrevSibling
	}
	var b strings.Builder
	for n := step(sel.Nodes[0]); n != nil; n = step(n) {
		if n.Type == html.ElementNode {
			break
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
	}
	return hasMaleToken(b.String())
}

// genderHintForAnchor resolves the gender symbol that belongs to this anchor.
// A marker is attributed to its nearest anchor: markers already claimed by a
// neighbouring row are skipped so that layouts printing the symbol before the
// name and layouts printing it after the name both resolve correctly.
func genderHintForAnchor(sel *goquery.Selection, used map[*html.Node]bool) string {
	if sel == nil || len(sel.Nodes) == 0 {
		return ""
	}
	node := sel.Nodes[0]

	if hint, marker := scanSymbolSiblingUsed(node, false, used); hint != "" {
		markMarkerUsed(used, marker)
		return hint
	}
	if hint, marker := scanSymbolSiblingUsed(node, true, used); hint != "" {
		markMarkerUsed(used, marker)
		return hint
	}
	return ""
}

func markMarkerUsed(used map[*html.Node]bool, marker *html.Node) {
	if used != nil && marker != nil {
		used[marker] = true
	}
}

func scanSymbolSiblingUsed(anchor *html.Node, forward bool, used map[*html.Node]bool) (string, *html.Node) {
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
		if used != nil && used[n] {
			continue
		}

		if strings.Contains(classAttr, "female") {
			return "female", n
		}
		if strings.Contains(classAttr, "male") {
			return "male", n
		}

		text := strings.TrimSpace(nodeText(n))
		switch {
		case strings.Contains(text, "♀"):
			return "female", n
		case strings.Contains(text, "♂"):
			return "male", n
		}
	}
	return "", nil
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
