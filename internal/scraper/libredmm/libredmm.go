package libredmm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/javinizer/javinizer-go/internal/scraper/image/placeholder"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

const (
	defaultBaseURL      = "https://www.libredmm.com"
	defaultPollAttempts = 4
	defaultPollInterval = 1500 * time.Millisecond
)

var (
	cidRegex                 = regexp.MustCompile(`(?i)(?:^|[?&])(cid|id)=([^&]+)`)
	dmmPrefixedCIDRegex      = regexp.MustCompile(`^(\d{3,}[a-z]+)0+(\d+.*)$`)
	dmmSampleFilenamePattern = regexp.MustCompile(`(?i)\.jpe?g$`)
	// URL extraction pattern
	libreDMPathRegex = regexp.MustCompile(`/movies/([^/?&]+)`)
	// ANSI escape sequence pattern for stripping terminal color codes
	ansiEscapeRegex = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")
	// Pattern to strip bare ESC character (0x1b) that may be injected by proxies
	// This handles cases where ESC appears without the bracket `[`
	escCharRegex = regexp.MustCompile("\x1b")
	// Pattern to strip other control characters (0x00-0x1F) except tab, newline, carriage return
	// These can be injected by broken proxies or terminal wrappers
	controlCharRegex = regexp.MustCompile("[\x00-\x08\x0b\x0c\x0e-\x1f]")
)

type actressPayload struct {
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
}

type moviePayload struct {
	Err               string           `json:"err"`
	Actresses         []actressPayload `json:"actresses"`
	CoverImageURL     string           `json:"cover_image_url"`
	Date              string           `json:"date"`
	Description       string           `json:"description"`
	Directors         []string         `json:"directors"`
	Genres            []string         `json:"genres"`
	Labels            []string         `json:"labels"`
	Makers            []string         `json:"makers"`
	NormalizedID      string           `json:"normalized_id"`
	Review            float64          `json:"review"`
	Subtitle          string           `json:"subtitle"`
	ThumbnailImageURL string           `json:"thumbnail_image_url"`
	Title             string           `json:"title"`
	URL               string           `json:"url"`
	Volume            int              `json:"volume"`
	SampleImageURLs   []string         `json:"sample_image_urls"`
}

// Scraper implements the LibreDMM scraper.
type Scraper struct {
	client          *resty.Client
	enabled         bool
	baseURL         string
	proxyOverride   *config.ProxyConfig
	downloadProxy   *config.ProxyConfig
	rateLimiter     *ratelimit.Limiter
	pollInterval    time.Duration
	maxPollAttempts int
	settings        config.ScraperSettings // stores the full settings for Config() method
}

// New creates a new LibreDMM scraper.
func New(settings config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) *Scraper {
	result := httpclient.InitScraperClient(&settings, globalProxy, globalFlareSolverr,
		httpclient.WithScraperHeaders(httpclient.CombineHeaders(
			httpclient.JSONAPIHeaders(),
			httpclient.UserAgentHeader(settings.UserAgent),
			map[string]string{"Accept-Language": "ja,en-US;q=0.8,en;q=0.6"},
		)),
	)
	client := result.Client

	base := strings.TrimSpace(settings.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")

	s := &Scraper{
		client:          client,
		enabled:         settings.Enabled,
		baseURL:         base,
		proxyOverride:   settings.Proxy,
		downloadProxy:   settings.DownloadProxy,
		rateLimiter:     ratelimit.NewLimiter(time.Duration(settings.RateLimit) * time.Millisecond),
		pollInterval:    defaultPollInterval,
		maxPollAttempts: defaultPollAttempts,
		settings:        settings,
	}

	if result.ProxyEnabled && strings.TrimSpace(result.ProxyProfile.URL) != "" {
		logging.Infof("LibreDMM: Using proxy %s", httpclient.SanitizeProxyURL(result.ProxyProfile.URL))
	}

	return s
}

// Name returns scraper identifier.
func (s *Scraper) Name() string { return "libredmm" }

// IsEnabled returns whether scraper is enabled.
func (s *Scraper) IsEnabled() bool { return s.enabled }

// Config returns the scraper's configuration
func (s *Scraper) Config() *config.ScraperSettings {
	return s.settings.DeepCopy()
}

// Close cleans up resources held by the scraper
func (s *Scraper) Close() error {
	return nil
}

// CanHandleURL returns true if this scraper can handle the given URL
func (s *Scraper) CanHandleURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "libredmm.com" || strings.HasSuffix(host, ".libredmm.com")
}

// ExtractIDFromURL extracts the movie ID from a LibreDMM URL
func (s *Scraper) ExtractIDFromURL(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}

	// Check for search query parameter
	if q := strings.TrimSpace(u.Query().Get("q")); q != "" {
		return q, nil
	}

	// Extract from /movies/{id}, /movies/{id}.json, or /cid/{id} path
	matches := libreDMPathRegex.FindStringSubmatch(u.Path)
	if len(matches) > 1 {
		return strings.TrimSuffix(matches[1], ".json"), nil
	}

	// Check for /cid/{id} format (case-insensitive)
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 && strings.EqualFold(parts[0], "cid") {
		id, _ := url.PathUnescape(parts[1])
		id = strings.TrimSuffix(id, ".json")
		if id != "" {
			return id, nil
		}
	}

	return "", fmt.Errorf("failed to extract ID from LibreDMM URL")
}

func (s *Scraper) ScrapeURL(ctx context.Context, urlStr string) (*models.ScraperResult, error) {
	if !s.CanHandleURL(urlStr) {
		return nil, models.NewScraperNotFoundError("LibreDMM", "URL not handled by LibreDMM scraper")
	}

	if !s.enabled {
		return nil, fmt.Errorf("LibreDMM scraper is disabled")
	}

	id, err := s.ExtractIDFromURL(urlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to extract ID from URL: %w", err)
	}

	var targetURL string
	if normalized, ok := normalizeMovieURL(urlStr, s.baseURL); ok {
		targetURL = normalized
	} else {
		targetURL, err = s.getURLCtx(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to normalize URL: %w", err)
		}
	}

	attempts := s.maxPollAttempts
	if attempts < 1 {
		attempts = 1
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		payload, finalURL, status, err := s.fetchMovieJSONCtx(ctx, targetURL)
		if err != nil {
			return nil, fmt.Errorf("failed to query LibreDMM: %w", err)
		}
		if finalURL != "" {
			targetURL = finalURL
		}

		switch status {
		case 200:
			if msg := scraperutil.CleanString(payload.Err); msg != "" {
				return nil, fmt.Errorf("LibreDMM returned error: %s", msg)
			}
			result := payloadToResult(payload, targetURL, id, s.client.GetClient())
			s.filterPlaceholderScreenshotsCtx(ctx, result)
			return result, nil
		case 202:
			msg := scraperutil.CleanString(payload.Err)
			if msg == "" {
				msg = "processing"
			}
			if attempt == attempts {
				return nil, fmt.Errorf("LibreDMM is still %s for %s", msg, id)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(s.pollInterval):
			}
		case 404:
			return nil, models.NewScraperNotFoundError("LibreDMM", fmt.Sprintf("movie %s not found on LibreDMM", id))
		case 502:
			msg := scraperutil.CleanString(payload.Err)
			if msg != "" {
				return nil, models.NewScraperStatusError(
					"LibreDMM",
					502,
					fmt.Sprintf("LibreDMM is temporarily unavailable (HTTP 502 Bad Gateway; host may be down): %s", msg),
				)
			}
			return nil, models.NewScraperStatusError(
				"LibreDMM",
				502,
				"LibreDMM is temporarily unavailable (HTTP 502 Bad Gateway; host may be down)",
			)
		default:
			if msg := scraperutil.CleanString(payload.Err); msg != "" {
				return nil, models.NewScraperStatusError(
					"LibreDMM",
					status,
					fmt.Sprintf("LibreDMM returned status code %d: %s", status, msg),
				)
			}
			return nil, models.NewScraperStatusError(
				"LibreDMM",
				status,
				fmt.Sprintf("LibreDMM returned status code %d", status),
			)
		}
	}

	return nil, models.NewScraperNotFoundError("LibreDMM", fmt.Sprintf("movie %s not found on LibreDMM", id))
}

// ResolveDownloadProxyForHost declares LibreDMM-owned media hosts for downloader proxy routing.
func (s *Scraper) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil, nil, false
	}
	if host == "libredmm.com" || strings.HasSuffix(host, ".libredmm.com") {
		return s.downloadProxy, s.proxyOverride, true
	}
	return nil, nil, false
}

// ResolveSearchQuery prioritizes this scraper when the input is a LibreDMM URL.
func (s *Scraper) ResolveSearchQuery(input string) (string, bool) {
	normalized, ok := normalizeMovieURL(strings.TrimSpace(input), s.baseURL)
	if !ok {
		return "", false
	}
	return normalized, true
}

func (s *Scraper) GetURL(id string) (string, error) {
	return s.getURLCtx(context.Background(), id)
}

func (s *Scraper) getURLCtx(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("movie ID cannot be empty")
	}

	if normalized, ok := normalizeMovieURL(id, s.baseURL); ok {
		return normalized, nil
	}

	if scraperutil.IsHTTPURL(id) {
		if extracted := extractIDFromURL(id); extracted != "" {
			return buildSearchURL(s.baseURL, extracted), nil
		}
	}

	return buildSearchURL(s.baseURL, id), nil
}

// Search scrapes metadata from LibreDMM JSON endpoints.
func (s *Scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("LibreDMM scraper is disabled")
	}

	targetURL, err := s.getURLCtx(ctx, id)
	if err != nil {
		return nil, err
	}

	attempts := s.maxPollAttempts
	if attempts < 1 {
		attempts = 1
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		payload, finalURL, status, err := s.fetchMovieJSONCtx(ctx, targetURL)
		if err != nil {
			return nil, fmt.Errorf("failed to query LibreDMM: %w", err)
		}
		if finalURL != "" {
			targetURL = finalURL
		}

		switch status {
		case 200:
			if msg := scraperutil.CleanString(payload.Err); msg != "" {
				return nil, fmt.Errorf("LibreDMM returned error: %s", msg)
			}
			result := payloadToResult(payload, targetURL, id, s.client.GetClient())
			s.filterPlaceholderScreenshotsCtx(ctx, result)
			return result, nil
		case 202:
			msg := scraperutil.CleanString(payload.Err)
			if msg == "" {
				msg = "processing"
			}
			if attempt == attempts {
				return nil, fmt.Errorf("LibreDMM is still %s for %s", msg, id)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(s.pollInterval):
			}
		case 404:
			return nil, models.NewScraperNotFoundError("LibreDMM", fmt.Sprintf("movie %s not found on LibreDMM", id))
		case 502:
			msg := scraperutil.CleanString(payload.Err)
			if msg != "" {
				return nil, models.NewScraperStatusError(
					"LibreDMM",
					502,
					fmt.Sprintf("LibreDMM is temporarily unavailable (HTTP 502 Bad Gateway; host may be down): %s", msg),
				)
			}
			return nil, models.NewScraperStatusError(
				"LibreDMM",
				502,
				"LibreDMM is temporarily unavailable (HTTP 502 Bad Gateway; host may be down)",
			)
		default:
			if msg := scraperutil.CleanString(payload.Err); msg != "" {
				return nil, models.NewScraperStatusError(
					"LibreDMM",
					status,
					fmt.Sprintf("LibreDMM returned status code %d: %s", status, msg),
				)
			}
			return nil, models.NewScraperStatusError(
				"LibreDMM",
				status,
				fmt.Sprintf("LibreDMM returned status code %d", status),
			)
		}
	}

	return nil, models.NewScraperNotFoundError("LibreDMM", fmt.Sprintf("movie %s not found on LibreDMM", id))
}

func (s *Scraper) fetchMovieJSONCtx(ctx context.Context, targetURL string) (*moviePayload, string, int, error) {
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, "", 0, err
	}

	resp, err := s.client.R().SetContext(ctx).Get(targetURL)
	if err != nil {
		return nil, "", 0, err
	}

	payload := &moviePayload{}
	body := resp.Body()
	if len(body) > 0 {
		// Strip ANSI escape codes and control characters before JSON parsing.
		// These can appear in responses from proxies or terminal wrappers.
		cleanBody := stripANSICodes(string(body))
		if err := json.Unmarshal([]byte(cleanBody), payload); err != nil && resp.StatusCode() == 200 {
			return nil, "", resp.StatusCode(), fmt.Errorf("failed to parse JSON response: %w", err)
		}
	}

	finalURL := targetURL
	if raw := resp.RawResponse; raw != nil && raw.Request != nil && raw.Request.URL != nil {
		finalURL = raw.Request.URL.String()
	}

	// Once redirected to a movie page, keep polling that canonical endpoint.
	if canonical, ok := normalizeMovieURL(finalURL, s.baseURL); ok && strings.Contains(canonical, "/movies/") {
		finalURL = canonical
	}

	return payload, finalURL, resp.StatusCode(), nil
}

func payloadToResult(payload *moviePayload, sourceURL, fallbackID string, client *http.Client) *models.ScraperResult {
	result := &models.ScraperResult{
		Source:   "libredmm",
		Language: "ja",
	}

	if payload == nil {
		payload = &moviePayload{}
	}

	source := scraperutil.CleanString(payload.URL)
	if source == "" {
		source = stripJSONSuffix(sourceURL)
	}
	result.SourceURL = source

	id := scraperutil.CleanString(payload.NormalizedID)
	if id == "" {
		id = scraperutil.CleanString(extractIDFromURL(sourceURL))
	}
	if id == "" {
		id = scraperutil.CleanString(fallbackID)
	}
	result.ID = id

	contentID := scraperutil.CleanString(payload.Subtitle)
	if contentID == "" {
		contentID = id
	}
	result.ContentID = contentID

	title := scraperutil.CleanString(payload.Title)
	if title == "" {
		title = id
	}
	result.Title = title
	result.OriginalTitle = title
	result.Description = scraperutil.CleanString(payload.Description)

	if t := parseReleaseDate(payload.Date); t != nil {
		result.ReleaseDate = t
	}
	if payload.Volume > 0 {
		result.Runtime = payload.Volume / 60
	}

	result.Director = firstNonEmpty(payload.Directors)
	result.Maker = firstNonEmpty(payload.Makers)
	result.Label = firstNonEmpty(payload.Labels)
	result.Genres = dedupeStrings(payload.Genres)
	result.Actresses = parseActresses(payload.Actresses, sourceURL)

	result.CoverURL = scraperutil.ResolveURL(sourceURL, payload.CoverImageURL)
	result.PosterURL = scraperutil.ResolveURL(sourceURL, payload.ThumbnailImageURL)
	if result.CoverURL != "" {
		posterURL, shouldCrop := imageutil.GetOptimalPosterURL(result.CoverURL, client)
		result.ShouldCropPoster = shouldCrop
		if shouldCrop || posterURL == "" {
			result.PosterURL = result.CoverURL
		} else {
			result.PosterURL = posterURL
		}
	}
	if result.PosterURL == "" {
		result.PosterURL = result.CoverURL
	}
	result.ScreenshotURL = dedupeResolvedURLs(payload.SampleImageURLs, sourceURL)

	if payload.Review > 0 {
		result.Rating = &models.Rating{
			Score: payload.Review,
		}
	}

	return result
}

func parseReleaseDate(raw string) *time.Time {
	raw = scraperutil.CleanString(raw)
	if raw == "" {
		return nil
	}

	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02",
		"2006-01-02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return &t
		}
	}
	return nil
}

func parseActresses(entries []actressPayload, base string) []models.ActressInfo {
	seen := make(map[string]bool, len(entries))
	out := make([]models.ActressInfo, 0, len(entries))

	for _, actress := range entries {
		name := scraperutil.CleanString(actress.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		info := models.ActressInfo{
			ThumbURL: toHTTPS(scraperutil.ResolveURL(base, actress.ImageURL)),
		}
		if scraperutil.HasJapanese(name) {
			info.JapaneseName = name
		} else {
			parts := strings.Fields(name)
			switch len(parts) {
			case 1:
				info.FirstName = parts[0]
			default:
				info.FirstName = parts[0]
				info.LastName = strings.Join(parts[1:], " ")
			}
		}

		out = append(out, info)
	}

	return out
}

func dedupeResolvedURLs(urls []string, base string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(urls))

	for _, raw := range urls {
		u := normalizeLibredmmScreenshotURL(scraperutil.ResolveURL(base, raw))
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}

	return out
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))

	for _, value := range values {
		v := scraperutil.CleanString(value)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}

	return out
}

func firstNonEmpty(values []string) string {
	for _, v := range values {
		if cleaned := scraperutil.CleanString(v); cleaned != "" {
			return cleaned
		}
	}
	return ""
}

func normalizeMovieURL(raw, base string) (string, bool) {
	if !scraperutil.IsHTTPURL(raw) {
		return "", false
	}

	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "libredmm.com" && !strings.HasSuffix(host, ".libredmm.com") {
		return "", false
	}

	queryID := scraperutil.CleanString(parsed.Query().Get("q"))
	if queryID != "" && strings.Contains(strings.ToLower(parsed.Path), "/search") {
		return buildSearchURL(base, queryID), true
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) >= 2 {
		if strings.EqualFold(parts[0], "movies") {
			id, _ := url.PathUnescape(parts[1])
			id = strings.TrimSuffix(id, ".json")
			id = scraperutil.CleanString(id)
			if id == "" {
				return "", false
			}
			return strings.TrimRight(base, "/") + "/movies/" + url.PathEscape(id) + ".json", true
		}
		if strings.EqualFold(parts[0], "cid") {
			id, _ := url.PathUnescape(parts[1])
			id = strings.TrimSuffix(id, ".json")
			id = scraperutil.CleanString(id)
			if id == "" {
				return "", false
			}
			return strings.TrimRight(base, "/") + "/movies/" + url.PathEscape(id) + ".json", true
		}
	}

	return "", false
}

func buildSearchURL(baseURL, query string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return fmt.Sprintf("%s/search?q=%s&format=json", baseURL, url.QueryEscape(strings.TrimSpace(query)))
}

func extractIDFromURL(raw string) string {
	if raw == "" {
		return ""
	}
	if matches := cidRegex.FindStringSubmatch(raw); len(matches) > 2 {
		decoded, err := url.QueryUnescape(matches[2])
		if err == nil && strings.TrimSpace(decoded) != "" {
			return strings.TrimSpace(decoded)
		}
		return strings.TrimSpace(matches[2])
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) >= 2 {
		if strings.EqualFold(parts[0], "movies") {
			id, _ := url.PathUnescape(parts[1])
			return strings.TrimSpace(strings.TrimSuffix(id, ".json"))
		}
		if strings.EqualFold(parts[0], "cid") {
			id, _ := url.PathUnescape(parts[1])
			return strings.TrimSpace(strings.TrimSuffix(id, ".json"))
		}
	}

	return ""
}

func normalizeLibredmmScreenshotURL(raw string) string {
	raw = scraperutil.CleanString(raw)
	if raw == "" {
		return ""
	}

	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	raw = strings.Replace(raw, "awsimgsrc.dmm.co.jp/pics_dig", "pics.dmm.co.jp", 1)

	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	u.RawQuery = ""
	u.Fragment = ""

	host := strings.ToLower(u.Hostname())
	if host == "dmm.co.jp" || strings.HasSuffix(host, ".dmm.co.jp") ||
		host == "dmm.com" || strings.HasSuffix(host, ".dmm.com") {
		segments := strings.Split(u.Path, "/")
		for i, seg := range segments {
			if seg == "" {
				continue
			}
			segments[i] = canonicalizeDMMPrefixedContentID(seg)
		}
		u.Path = strings.Join(segments, "/")

		base := path.Base(u.Path)
		lowerBase := strings.ToLower(base)
		if dmmSampleFilenamePattern.MatchString(lowerBase) &&
			strings.Contains(base, "-") &&
			!strings.Contains(lowerBase, "jp-") &&
			!strings.HasSuffix(lowerBase, "pl.jpg") &&
			!strings.HasSuffix(lowerBase, "ps.jpg") {
			base = strings.Replace(base, "-", "jp-", 1)
			u.Path = strings.TrimSuffix(u.Path, path.Base(u.Path)) + base
		}
	}

	return u.String()
}

func canonicalizeDMMPrefixedContentID(seg string) string {
	ext := ""
	if idx := strings.LastIndex(seg, "."); idx > 0 {
		ext = seg[idx:]
		seg = seg[:idx]
	}

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

	if matches := dmmPrefixedCIDRegex.FindStringSubmatch(lower); len(matches) == 3 {
		tail := matches[2]
		digitPrefixLen := 0
		for digitPrefixLen < len(tail) && tail[digitPrefixLen] >= '0' && tail[digitPrefixLen] <= '9' {
			digitPrefixLen++
		}
		if digitPrefixLen > 0 && digitPrefixLen < 3 {
			tail = strings.Repeat("0", 3-digitPrefixLen) + tail
		}
		seg = matches[1] + tail
	}

	return seg + suffix + ext
}

func stripJSONSuffix(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSuffix(raw, ".json")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, ".json")
	return parsed.String()
}

func toHTTPS(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "http://") {
		return "https://" + strings.TrimPrefix(raw, "http://")
	}
	return raw
}

// stripANSICodes removes ANSI escape sequences and control characters from a string.
// These can appear in responses from proxies or terminal wrappers.
// Handles ANSI sequences, bare ESC characters, and other control characters.
// Also attempts to extract valid JSON from garbage data.
func stripANSICodes(s string) string {
	// First strip standard ANSI sequences
	s = ansiEscapeRegex.ReplaceAllString(s, "")
	// Then strip any remaining bare ESC characters
	s = escCharRegex.ReplaceAllString(s, "")
	// Finally strip other control characters (except tab, newline, CR)
	s = controlCharRegex.ReplaceAllString(s, "")

	// If the string doesn't start with valid JSON characters,
	// try to find the start of the JSON content
	s = strings.TrimSpace(s)
	if len(s) > 0 && s[0] != '{' && s[0] != '[' {
		// Find the first '{' or '[' which should be the start of JSON
		for i, c := range s {
			if c == '{' || c == '[' {
				s = s[i:]
				break
			}
		}
	}

	return s
}

func (s *Scraper) filterPlaceholderScreenshotsCtx(ctx context.Context, result *models.ScraperResult) {
	if len(result.ScreenshotURL) == 0 {
		return
	}

	cfg := placeholder.ConfigFromSettings(&s.settings, placeholder.DefaultDMMPlaceholderHashes)
	if !cfg.Enabled {
		return
	}

	filtered, count, err := placeholder.FilterURLs(ctx, s.client, result.ScreenshotURL, cfg)
	if err != nil {
		logging.Warnf("libredmm: placeholder filter error: %v", err)
		return
	}
	if count > 0 {
		logging.Debugf("libredmm: Filtered %d placeholder screenshots", count)
		result.ScreenshotURL = filtered
	}
}
