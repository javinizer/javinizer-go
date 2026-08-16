package minnanoav

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	xhtml "golang.org/x/net/html"
)

const (
	defaultBaseURL = "https://www.minnano-av.com"
	searchPath     = "/search_result.php"
)

// scraper ...
type scraper struct {
	client      *resty.Client
	enabled     bool
	baseURL     string
	rateLimiter *ratelimit.Limiter
	settings    models.ScraperSettings
}

// newScraper ...
// FlareSolverr is intentionally not threaded: minnanoav is a plain-HTML
// source without a challenge flow; the parameter stays for ctor parity.
func newScraper(settings *models.ScraperSettings, globalProxy *models.ProxyConfig, _ models.FlareSolverrConfig) *scraper {
	return newScraperWithClient(settings, buildClient(settings, globalProxy))
}

// newScraperWithClient ...
func newScraperWithClient(settings *models.ScraperSettings, client *resty.Client) *scraper {
	baseURL := settings.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	rateLimit := settings.RateLimit
	if rateLimit < 0 {
		rateLimit = 0
	}
	if rateLimit == 0 && !settings.RateLimitIsExplicit() {
		// Omitted: keep the conservative 1s crawl default; an explicitly
		// configured rate_limit: 0 means no delay (documented contract).
		rateLimit = 1000
	}
	return &scraper{
		client:      client,
		enabled:     settings.Enabled,
		baseURL:     strings.TrimRight(baseURL, "/"),
		rateLimiter: ratelimit.NewLimiter(time.Duration(rateLimit) * time.Millisecond),
		settings:    *settings,
	}
}

// buildClient ...
// redirectAllowlist builds the set of hostnames redirects may target: the
// canonical minnano-av.com hosts plus any configured mirror base.
func redirectAllowlist(baseURL string) map[string]struct{} {
	allowed := map[string]struct{}{"minnano-av.com": {}, "www.minnano-av.com": {}}
	if u, err := url.Parse(strings.TrimSpace(baseURL)); err == nil && u.Hostname() != "" {
		allowed[strings.ToLower(u.Hostname())] = struct{}{}
	}
	return allowed
}

func buildClient(settings *models.ScraperSettings, globalProxy *models.ProxyConfig) *resty.Client {
	timeout := time.Duration(settings.Timeout) * time.Second
	if timeout <= 0 && !settings.TimeoutIsExplicit() {
		timeout = 30 * time.Second
	}
	retries := settings.RetryCount
	if retries < 0 {
		retries = 0
	}
	// Explicit `retry_count: 0` disables retries (codex round 12).
	if globalProxy == nil {
		globalProxy = &models.ProxyConfig{}
	}
	proxyProfile := models.ResolveScraperProxy(*globalProxy, settings.Proxy)
	client, err := httpclient.NewRestyClient(proxyProfile, timeout, retries)
	if err != nil {
		logging.Warnf("MinnanoAV: failed to create HTTP client, falling back to no-proxy: %v", err)
		client = httpclient.NewRestyClientNoProxy(timeout, retries)
	}
	// codex round 14: a 200 with a nil transport error skips resty's built-in
	// retry gate; explicitly list retryable conditions. Must be applied to the
	// final client (proxy or fallback).
	client.AddRetryCondition(func(r *resty.Response, err error) bool {
		return err != nil || r == nil || r.StatusCode() == http.StatusTooManyRequests || r.StatusCode() >= 500
	})
	client.SetHeaders(httpclient.StandardHTMLHeaders())
	if ua := strings.TrimSpace(settings.UserAgent); ua != "" {
		client.SetHeader("User-Agent", ua)
	}
	// Search/resolves may only ever land on MinnanoAV itself: a spoofed or
	// compromised page must not redirect the backend to loopback, private,
	// or cloud-metadata endpoints (SSRF), and chains stay bounded.
	allowed := redirectAllowlist(settings.BaseURL)
	// Codex round 11: per-hop SSRF revalidation — the allowlist admits hosts,
	// but DNS can still rebind them to private/loopback before dial. Every
	// redirect (and via the pinned dialer every request) is re-checked.
	client.SetRedirectPolicy(resty.RedirectPolicyFunc(func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("minnanoav: stopped after 5 redirects")
		}
		host := strings.ToLower(req.URL.Hostname())
		matched := false
		for ok := range allowed {
			if host == ok || strings.HasSuffix(host, "."+ok) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("minnanoav: refusing redirect to %s", req.URL.Redacted())
		}
		// Hostname allowlist is checked per hop; DNS-level revalidation happens
		// at dial time (pinned transport), protecting against rebinding.
		return nil
	}))
	// Pin the dialer ONLY for the direct path: with a configured proxy the
	// connection target IS the proxy (trusted infra, often loopback) — pinning
	// it would reject every request. Proxy destinations are instead validated
	// by the redirect policy's per-hop allowlist above (codex round 12).
	if proxyProfile.URL == "" {
		if base, ok := client.GetClient().Transport.(*http.Transport); ok {
			pinned, err := ssrf.NewPinnedDialTransport(base)
			if err == nil {
				client.SetTransport(pinned)
			}
		}
	}
	return client
}

// Name ...
func (s *scraper) Name() string { return "minnanoav" }

// IsEnabled ...
func (s *scraper) IsEnabled() bool { return s.enabled }

// Config ...
func (s *scraper) Config() *models.ScraperSettings {
	cloned := s.settings.Clone()
	return &cloned
}

// Close ...
func (s *scraper) Close() error { return nil }

// Search ...
func (s *scraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, models.NewScraperNotFoundError("MinnanoAV", "minnanoav is an actress-metadata-only source and does not support movie search")
}

// SupportsMovieSearch ...
func (s *scraper) SupportsMovieSearch() bool { return false }

// GetURL ...
func (s *scraper) GetURL(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("minnanoav does not support URL construction")
}

// ResolveActressMetadata ...
var _ models.ActressFieldCapable = (*scraper)(nil)

// ActressFields ... MinnanoAV actress profiles carry names and thumbnails.
func (s *scraper) ActressFields() []string {
	return []string{"actress", "actress_japanese_name", "actress_first_name", "actress_last_name", "actress_url"}
}

func (s *scraper) ResolveActressMetadata(ctx context.Context, actress models.ActressInfo) (models.ActressInfo, error) {
	metadata := models.ActressInfo{DMMID: actress.DMMID}
	if !s.enabled {
		return metadata, nil
	}
	name := scraperutil.CleanString(actress.JapaneseName)
	if name == "" {
		return metadata, nil
	}
	pageURL, html, err := s.searchActress(ctx, name)
	if err != nil {
		return metadata, err
	}
	if html == "" {
		return metadata, nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return metadata, fmt.Errorf("parse actress page: %w", err)
	}
	parsed := parseActressPage(doc, pageURL)
	if !parsed.containsName(name) {
		return metadata, nil
	}
	logging.Debugf("MinnanoAV: matched actress %s at %s", name, pageURL)
	romaji := parsed.romajiForName(name)
	if romaji != "" {
		if first, last, ok := splitRomajiName(romaji); ok {
			metadata.FirstName = first
			metadata.LastName = last
		}
	}
	metadata.JapaneseName = name
	metadata.ThumbURL = stripQuery(parsed.thumbURL)
	return metadata, nil
}

func (s *scraper) searchActress(ctx context.Context, name string) (string, string, error) {
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return "", "", err
	}
	searchURL := fmt.Sprintf("%s%s?search_scope=actress&search_word=%s&search=+Go+",
		s.baseURL, searchPath, url.QueryEscape(name))
	resp, err := s.client.R().SetContext(ctx).
		SetHeader("Referer", s.baseURL+"/").
		Get(searchURL)
	if err != nil {
		return "", "", fmt.Errorf("minnanoav search failed: %w", err)
	}
	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices {
		return "", "", models.NewScraperStatusError("minnanoav", resp.StatusCode(), fmt.Sprintf("minnanoav search: unexpected status %s", resp.Status()))
	}
	finalURL := ""
	if resp.RawResponse != nil && resp.RawResponse.Request != nil && resp.RawResponse.Request.URL != nil {
		reqURL := resp.RawResponse.Request.URL
		if !strings.Contains(reqURL.Path, "/actress") {
			return "", "", nil
		}
		finalURL = reqURL.String()
	}
	if finalURL == "" {
		return "", "", nil
	}
	return finalURL, resp.String(), nil
}

// ActressProfile ...
type ActressProfile struct {
	DMMID        int
	FirstName    string
	LastName     string
	JapaneseName string
	Aliases      []string
	ThumbURL     string
}

// ParseActressProfile ...
func ParseActressProfile(rawHTML, sourceURL string) (ActressProfile, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return ActressProfile{}, err
	}
	page := parseActressPage(doc, sourceURL)
	profile := ActressProfile{
		DMMID:        parseDMMActressID(doc),
		JapaneseName: page.primaryName,
		ThumbURL:     stripQuery(page.thumbURL),
		Aliases:      make([]string, 0, len(page.aliases)),
	}
	if firstName, lastName, ok := splitRomajiName(page.romaji); ok {
		profile.FirstName = firstName
		profile.LastName = lastName
	}
	seen := make(map[string]struct{}, len(page.aliases))
	for _, alias := range page.aliases {
		aliasName := strings.TrimSpace(alias.japanese)
		if aliasName == "" || aliasName == profile.JapaneseName {
			continue
		}
		if _, exists := seen[aliasName]; exists {
			continue
		}
		seen[aliasName] = struct{}{}
		profile.Aliases = append(profile.Aliases, aliasName)
	}
	return profile, nil
}

// dmmActressIDPattern ...
var dmmActressIDPattern = regexp.MustCompile("(?:^|[?&])actress=([0-9]+)")

// parseDMMActressID ...
func parseDMMActressID(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	id := 0
	doc.Find("a[href]").EachWithBreak(func(_ int, link *goquery.Selection) bool {
		href := html.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		for range 3 {
			decoded, err := url.QueryUnescape(href)
			if err != nil || decoded == href {
				break
			}
			href = decoded
		}
		match := dmmActressIDPattern.FindStringSubmatch(href)
		if len(match) != 2 {
			return true
		}
		id, _ = strconv.Atoi(match[1])
		return false
	})
	return id
}

// actressPage ...
type actressPage struct {
	primaryName string
	reading     string
	romaji      string
	aliases     []aliasEntry
	thumbURL    string
}

// aliasEntry ...
type aliasEntry struct {
	japanese string
	reading  string
	romaji   string
}

func (p actressPage) containsName(name string) bool {
	if p.primaryName == name {
		return true
	}
	for _, a := range p.aliases {
		if a.japanese == name {
			return true
		}
	}
	return false
}

func (p actressPage) romajiForName(name string) string {
	if p.primaryName == name {
		return p.romaji
	}
	for _, a := range p.aliases {
		if a.japanese == name {
			return a.romaji
		}
	}
	return ""
}

// parseActressPage ...
func parseActressPage(doc *goquery.Document, sourceURL string) actressPage {
	page := actressPage{}
	if doc == nil {
		return page
	}
	doc.Find("h1").First().Each(func(_ int, h1 *goquery.Selection) {
		page.primaryName = scraperutil.CleanString(directText(h1))
		reading, romaji := parseReadingRomaji(scraperutil.CleanString(h1.Find("span").Text()))
		page.reading = reading
		page.romaji = romaji
	})
	if page.primaryName == "" {
		h2 := doc.Find(".act-profile h2").First()
		if h2.Length() > 0 {
			name, reading, romaji := parseNameEntry(scraperutil.CleanString(h2.Text()))
			page.primaryName = name
			page.reading = reading
			page.romaji = romaji
		}
	}
	doc.Find(".act-profile tr").Each(func(_ int, tr *goquery.Selection) {
		label := scraperutil.CleanString(tr.Find("span").Text())
		value := scraperutil.CleanString(tr.Find("p").Text())
		if label != "別名" || value == "" {
			return
		}
		jp, reading, romaji := parseNameEntry(value)
		if jp == "" {
			return
		}
		page.aliases = append(page.aliases, aliasEntry{japanese: jp, reading: reading, romaji: romaji})
	})
	if thumb := doc.Find(`meta[property="og:image"]`).First(); thumb.Length() > 0 {
		if src := strings.TrimSpace(thumb.AttrOr("content", "")); src != "" {
			page.thumbURL = scraperutil.ResolveURL(sourceURL, src)
		}
	}
	return page
}

// parseNameEntry ...
func parseNameEntry(raw string) (japanese, reading, romaji string) {
	parts := strings.SplitN(raw, "（", 2)
	japanese = scraperutil.CleanString(parts[0])
	if len(parts) < 2 {
		return japanese, "", ""
	}
	inner := strings.TrimSuffix(strings.TrimSpace(parts[1]), "）")
	reading, romaji = parseReadingRomaji(scraperutil.CleanString(inner))
	return japanese, reading, romaji
}

// parseReadingRomaji ...
func parseReadingRomaji(inner string) (reading, romaji string) {
	if inner == "" {
		return "", ""
	}
	parts := strings.SplitN(inner, "/", 2)
	reading = strings.TrimSpace(parts[0])
	if len(parts) < 2 {
		return reading, ""
	}
	romaji = strings.TrimSpace(parts[1])
	return reading, romaji
}

// splitRomajiName ...
func splitRomajiName(romaji string) (string, string, bool) {
	romaji = strings.TrimSpace(romaji)
	if romaji == "" {
		return "", "", false
	}
	parts := strings.Fields(romaji)
	if len(parts) < 2 {
		// One-token romanization ('AIKA') is a FirstName (codex latest head).
		if len(parts) == 1 {
			return parts[0], "", true
		}
		return "", "", false
	}
	return parts[1], parts[0], true
}

// stripQuery ...
func stripQuery(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery = ""
	return u.String()
}

// directText ...
func directText(sel *goquery.Selection) string {
	if sel == nil || len(sel.Nodes) == 0 {
		return ""
	}
	// b ...
	var b strings.Builder
	for _, node := range sel.Nodes {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == xhtml.TextNode {
				b.WriteString(child.Data)
			}
		}
	}
	return b.String()
}

// _ ...
var _ models.Scraper = (*scraper)(nil)

// _ ...
var _ models.ActressMetadataResolver = (*scraper)(nil)

// _ ...
var _ models.MovieSearchCapable = (*scraper)(nil)
