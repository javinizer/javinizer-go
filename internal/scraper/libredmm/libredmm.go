package libredmm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
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
	requestDelay    time.Duration
	lastRequestTime atomic.Value
	pollInterval    time.Duration
	maxPollAttempts int
}

// New creates a new LibreDMM scraper.
func New(cfg *config.Config) *Scraper {
	scraperCfg := cfg.Scrapers.LibreDMM
	proxyCfg := config.ResolveScraperProxy(cfg.Scrapers.Proxy, scraperCfg.Proxy)

	client, err := httpclient.NewRestyClient(proxyCfg, 30*time.Second, 3)
	usingProxy := err == nil && proxyCfg.Enabled && strings.TrimSpace(proxyCfg.URL) != ""
	if err != nil {
		logging.Errorf("LibreDMM: Failed to create HTTP client with proxy: %v, using explicit no-proxy fallback", err)
		client = httpclient.NewRestyClientNoProxy(30*time.Second, 3)
	}

	userAgent := config.ResolveScraperUserAgent(
		cfg.Scrapers.UserAgent,
		scraperCfg.UseFakeUserAgent,
		scraperCfg.FakeUserAgent,
	)
	client.SetHeader("User-Agent", userAgent)
	client.SetHeader("Accept", "application/json, text/plain;q=0.9, */*;q=0.8")
	client.SetHeader("Accept-Language", "ja,en-US;q=0.8,en;q=0.6")
	client.SetHeader("Connection", "keep-alive")

	base := strings.TrimSpace(scraperCfg.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")

	s := &Scraper{
		client:          client,
		enabled:         scraperCfg.Enabled,
		baseURL:         base,
		requestDelay:    time.Duration(scraperCfg.RequestDelay) * time.Millisecond,
		pollInterval:    defaultPollInterval,
		maxPollAttempts: defaultPollAttempts,
	}
	s.lastRequestTime.Store(time.Time{})

	if usingProxy {
		logging.Infof("LibreDMM: Using proxy %s", httpclient.SanitizeProxyURL(proxyCfg.URL))
	}

	return s
}

// Name returns scraper identifier.
func (s *Scraper) Name() string { return "libredmm" }

// IsEnabled returns whether scraper is enabled.
func (s *Scraper) IsEnabled() bool { return s.enabled }

// ResolveSearchQuery prioritizes this scraper when the input is a LibreDMM URL.
func (s *Scraper) ResolveSearchQuery(input string) (string, bool) {
	normalized, ok := normalizeMovieURL(strings.TrimSpace(input), s.baseURL)
	if !ok {
		return "", false
	}
	return normalized, true
}

// GetURL resolves a JSON movie endpoint for the provided input.
func (s *Scraper) GetURL(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("movie ID cannot be empty")
	}

	if normalized, ok := normalizeMovieURL(id, s.baseURL); ok {
		return normalized, nil
	}

	// If a foreign URL is provided, extract a likely ID when possible.
	if isHTTPURL(id) {
		if extracted := extractIDFromURL(id); extracted != "" {
			return buildSearchURL(s.baseURL, extracted), nil
		}
	}

	return buildSearchURL(s.baseURL, id), nil
}

// Search scrapes metadata from LibreDMM JSON endpoints.
func (s *Scraper) Search(id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("LibreDMM scraper is disabled")
	}

	targetURL, err := s.GetURL(id)
	if err != nil {
		return nil, err
	}

	attempts := s.maxPollAttempts
	if attempts < 1 {
		attempts = 1
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		payload, finalURL, status, err := s.fetchMovieJSON(targetURL)
		if err != nil {
			return nil, fmt.Errorf("failed to query LibreDMM: %w", err)
		}
		if finalURL != "" {
			targetURL = finalURL
		}

		switch status {
		case 200:
			if msg := cleanString(payload.Err); msg != "" {
				return nil, fmt.Errorf("LibreDMM returned error: %s", msg)
			}
			return payloadToResult(payload, targetURL, id, s.client.GetClient()), nil
		case 202:
			msg := cleanString(payload.Err)
			if msg == "" {
				msg = "processing"
			}
			if attempt == attempts {
				return nil, fmt.Errorf("LibreDMM is still %s for %s", msg, id)
			}
			time.Sleep(s.pollInterval)
		case 404:
			return nil, models.NewScraperNotFoundError("LibreDMM", fmt.Sprintf("movie %s not found on LibreDMM", id))
		case 502:
			msg := cleanString(payload.Err)
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
			if msg := cleanString(payload.Err); msg != "" {
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

func (s *Scraper) fetchMovieJSON(targetURL string) (*moviePayload, string, int, error) {
	s.waitForRateLimit()
	resp, err := s.client.R().Get(targetURL)
	s.updateLastRequestTime()
	if err != nil {
		return nil, "", 0, err
	}

	payload := &moviePayload{}
	body := resp.Body()
	if len(body) > 0 {
		if err := json.Unmarshal(body, payload); err != nil && resp.StatusCode() == 200 {
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

	source := cleanString(payload.URL)
	if source == "" {
		source = stripJSONSuffix(sourceURL)
	}
	result.SourceURL = source

	id := cleanString(payload.NormalizedID)
	if id == "" {
		id = cleanString(extractIDFromURL(sourceURL))
	}
	if id == "" {
		id = cleanString(fallbackID)
	}
	result.ID = id

	contentID := cleanString(payload.Subtitle)
	if contentID == "" {
		contentID = id
	}
	result.ContentID = contentID

	title := cleanString(payload.Title)
	if title == "" {
		title = id
	}
	result.Title = title
	result.OriginalTitle = title
	result.Description = cleanString(payload.Description)

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

	result.CoverURL = resolveURL(sourceURL, payload.CoverImageURL)
	result.PosterURL = resolveURL(sourceURL, payload.ThumbnailImageURL)
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
	raw = cleanString(raw)
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
		name := cleanString(actress.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		info := models.ActressInfo{
			ThumbURL: toHTTPS(resolveURL(base, actress.ImageURL)),
		}
		if hasJapanese(name) {
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
		u := normalizeLibredmmScreenshotURL(resolveURL(base, raw))
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
		v := cleanString(value)
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
		if cleaned := cleanString(v); cleaned != "" {
			return cleaned
		}
	}
	return ""
}

func (s *Scraper) waitForRateLimit() {
	if s.requestDelay <= 0 {
		return
	}
	lastReq := s.lastRequestTime.Load()
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

func normalizeMovieURL(raw, base string) (string, bool) {
	if !isHTTPURL(raw) {
		return "", false
	}

	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.Contains(host, "libredmm.com") {
		return "", false
	}

	queryID := cleanString(parsed.Query().Get("q"))
	if queryID != "" && strings.Contains(strings.ToLower(parsed.Path), "/search") {
		return buildSearchURL(base, queryID), true
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) >= 2 && strings.EqualFold(parts[0], "movies") {
		id, _ := url.PathUnescape(parts[1])
		id = strings.TrimSuffix(id, ".json")
		id = cleanString(id)
		if id == "" {
			return "", false
		}
		return strings.TrimRight(base, "/") + "/movies/" + url.PathEscape(id) + ".json", true
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
	if len(parts) >= 2 && strings.EqualFold(parts[0], "movies") {
		id, _ := url.PathUnescape(parts[1])
		return strings.TrimSpace(strings.TrimSuffix(id, ".json"))
	}

	return ""
}

func resolveURL(base, raw string) string {
	raw = cleanString(raw)
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

func normalizeLibredmmScreenshotURL(raw string) string {
	raw = cleanString(raw)
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
	if strings.HasSuffix(host, "dmm.co.jp") || strings.HasSuffix(host, "dmm.com") {
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

func hasJapanese(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) {
			return true
		}
	}
	return false
}

func toHTTPS(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "http://") {
		return "https://" + strings.TrimPrefix(raw, "http://")
	}
	return raw
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
