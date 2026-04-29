package javlibrary

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
)

// SupportedLanguages lists the language codes supported by JavLibrary.
// en = English, ja = Japanese, cn = Chinese (Simplified), tw = Chinese (Traditional)
var SupportedLanguages = []string{"en", "ja", "cn", "tw"}

// Scraper implements the models.Scraper interface for JavLibrary
type Scraper struct {
	client        *resty.Client
	flaresolverr  *httpclient.FlareSolverr
	enabled       bool
	baseURL       string
	language      string
	proxyOverride *config.ProxyConfig
	downloadProxy *config.ProxyConfig
	rateLimiter   *ratelimit.Limiter
	settings      config.ScraperSettings // stores the full settings for Config() method
	cookieMu      sync.Mutex             // protects cookie mutations on shared client
}

// New creates a new JavLibrary scraper.
func New(settings config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) *Scraper {
	configForHTTP := &config.ScraperSettings{
		Enabled:         settings.Enabled,
		Language:        settings.Language,
		RateLimit:       settings.RateLimit,
		Timeout:         settings.Timeout,
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

	var resolvedProxy *config.ProxyProfile
	usingProxy := false
	if globalProxy != nil {
		resolvedProxy = config.ResolveScraperProxy(*globalProxy, settings.Proxy)
		usingProxy = err == nil && globalProxy.Enabled && strings.TrimSpace(resolvedProxy.URL) != ""
	}
	if err != nil {
		logging.Errorf("JavLibrary: Failed to create HTTP client with proxy/flaresolverr: %v, using explicit no-proxy fallback", err)
		client = httpclient.NewRestyClientNoProxy(time.Duration(settings.Timeout)*time.Second, settings.RetryCount)
		flaresolverr = nil
	}

	baseURL := settings.BaseURL
	if baseURL == "" {
		baseURL = "http://www.javlibrary.com"
	}

	language := settings.Language
	if language == "" {
		language = "en"
	}
	if !isValidLanguage(language) {
		logging.Warnf("JavLibrary: unsupported language %q, falling back to 'en' (supported: %v)", language, SupportedLanguages)
		language = "en"
	}

	if usingProxy && resolvedProxy != nil {
		logging.Infof("JavLibrary: Using proxy %s", httpclient.SanitizeProxyURL(resolvedProxy.URL))
	}

	return &Scraper{
		client:        client,
		flaresolverr:  flaresolverr,
		enabled:       settings.Enabled,
		baseURL:       baseURL,
		language:      language,
		proxyOverride: settings.Proxy,
		downloadProxy: settings.DownloadProxy,
		rateLimiter:   ratelimit.NewLimiter(time.Duration(settings.RateLimit) * time.Millisecond),
		settings:      settings,
	}
}

// Name returns the scraper name
func (s *Scraper) Name() string {
	return "javlibrary"
}

// GetLanguage returns the configured language
func (s *Scraper) GetLanguage() string {
	return s.language
}

// IsEnabled returns whether the scraper is enabled
func (s *Scraper) IsEnabled() bool {
	return s.enabled
}

// Config returns the scraper's configuration
func (s *Scraper) Config() *config.ScraperSettings {
	return s.settings.DeepCopy()
}

// Close cleans up resources held by the scraper (HTTP client, FlareSolverr).
func (s *Scraper) Close() error {
	if s.flaresolverr != nil {
		if closeErr := s.flaresolverr.Close(); closeErr != nil {
			logging.Debugf("JavLibrary: Error closing FlareSolverr: %v", closeErr)
		}
	}
	return nil
}

// ResolveDownloadProxyForHost declares JavLibrary-owned media hosts for downloader proxy routing.
func (s *Scraper) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil, nil, false
	}
	// JavLibrary uses c.impact.jp for its image CDN alongside javlibrary.com
	if host == "javlibrary.com" || strings.HasSuffix(host, ".javlibrary.com") ||
		host == "c.impact.jp" || strings.HasSuffix(host, ".c.impact.jp") {
		return s.downloadProxy, s.proxyOverride, true
	}
	return nil, nil, false
}

// GetURL returns the search URL for a given ID
func (s *Scraper) CanHandleURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "javlibrary.com" || strings.HasSuffix(host, ".javlibrary.com")
}

func (s *Scraper) ExtractIDFromURL(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}
	query := u.Query()
	if v := query.Get("v"); v != "" {
		return v, nil
	}
	if keyword := query.Get("keyword"); keyword != "" {
		return strings.ToUpper(keyword), nil
	}
	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" && len(parts[i]) > 4 {
			return parts[i], nil
		}
	}
	return "", fmt.Errorf("failed to extract ID from URL")
}

func (s *Scraper) ScrapeURL(ctx context.Context, rawURL string) (*models.ScraperResult, error) {
	if !s.CanHandleURL(rawURL) {
		return nil, models.NewScraperNotFoundError("JavLibrary", "URL not handled by JavLibrary scraper")
	}

	id, err := s.ExtractIDFromURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to extract ID from URL: %w", err)
	}

	var detailURL string
	var resultLanguage string
	if u, parseErr := url.Parse(rawURL); parseErr == nil && u.Query().Get("v") != "" {
		detailURL = rawURL
		path := strings.Trim(u.Path, "/")
		parts := strings.Split(path, "/")
		if len(parts) > 0 && isValidLanguage(parts[0]) {
			resultLanguage = parts[0]
		}
	}
	if detailURL == "" {
		detailURL = fmt.Sprintf("%s/%s/?v=%s", s.baseURL, s.language, url.QueryEscape(id))
	}
	if resultLanguage == "" {
		resultLanguage = s.language
	}

	html, err := s.fetchPageCtx(ctx, detailURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JavLibrary page: %w", err)
	}

	if strings.Contains(html, `id="video_info"`) {
		return s.parseDetailPage(html, id, detailURL, resultLanguage)
	}

	return nil, models.NewScraperNotFoundError("JavLibrary", "page does not contain video info")
}

func (s *Scraper) GetURL(id string) (string, error) {
	return fmt.Sprintf("%s/%s/vl_searchbyid.php?keyword=%s", s.baseURL, s.language, url.QueryEscape(id)), nil
}

// Search searches for a movie by ID with context support.
func (s *Scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("JavLibrary scraper is disabled")
	}

	searchURL, err := s.GetURL(id)
	if err != nil {
		return nil, err
	}

	// Fetch the search page
	html, err := s.fetchPageCtx(ctx, searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch search page: %w", err)
	}

	// Check if search landed directly on a detail page (contains video_info div)
	if strings.Contains(html, `id="video_info"`) {
		logging.Debugf("JavLibrary: Search landed directly on detail page for %s", id)
		return s.parseDetailPage(html, id, searchURL, s.language)
	}

	// Otherwise, look for a movie link in search results
	detailPath := s.extractMovieURLFromHTML(html, id)
	if detailPath == "" {
		return nil, models.NewScraperNotFoundError("JavLibrary", fmt.Sprintf("movie %s not found on JavLibrary", id))
	}

	// Build full detail URL
	detailURL := detailPath
	if !strings.HasPrefix(detailPath, "http") {
		trimmed := strings.TrimPrefix(detailPath, "/")
		if strings.HasPrefix(trimmed, s.language+"/") || strings.HasPrefix(trimmed, s.language+"?") {
			detailURL = s.baseURL + "/" + trimmed
		} else {
			detailURL = s.baseURL + "/" + s.language + "/" + trimmed
		}
	}

	logging.Debugf("JavLibrary: Fetching detail page: %s", detailURL)
	detailHTML, err := s.fetchPageCtx(ctx, detailURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch detail page: %w", err)
	}

	return s.parseDetailPage(detailHTML, id, detailURL, s.language)
}

// fetchPageCtx fetches a page via FlareSolverr (if enabled) or direct HTTP with context support.
func (s *Scraper) fetchPageCtx(ctx context.Context, url string) (string, error) {
	// Rate limit before fetching
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("JavLibrary: rate limit wait failed: %w", err)
	}

	// Try direct request first and only escalate to FlareSolverr on blocked/challenge responses.
	resp, err := s.client.R().SetContext(ctx).Get(url)
	if err == nil && resp != nil && resp.StatusCode() == 200 {
		html := string(resp.Body())
		if !models.IsCloudflareChallengePage(html) {
			return html, nil
		}
		logging.Warnf("JavLibrary: Direct request returned Cloudflare challenge, escalating to FlareSolverr: %s", url)
	} else if err == nil && resp != nil {
		logging.Debugf("JavLibrary: Direct request returned status %d for %s", resp.StatusCode(), url)
	}

	// Fallback to FlareSolverr if client was created.
	// The flaresolverr client is only non-nil when it was successfully initialized,
	// which happens when useFlareSolverr=true (based on scraper/global FlareSolverr config).
	if s.flaresolverr != nil {
		logging.Infof("JavLibrary: Using FlareSolverr for %s", url)
		html, cookies, fsErr := s.flaresolverr.ResolveURL(url)
		if fsErr == nil {
			if models.IsCloudflareChallengePage(html) {
				return "", models.NewScraperChallengeError(
					"JavLibrary",
					"JavLibrary returned a Cloudflare challenge page (request blocked; check FlareSolverr/proxy configuration)",
				)
			}
			// Apply cookies to client for subsequent requests (thread-safe)
			s.cookieMu.Lock()
			for _, c := range cookies {
				s.client.SetCookie(&c)
			}
			s.cookieMu.Unlock()
			return html, nil
		}
		logging.Warnf("JavLibrary: FlareSolverr failed, falling back to direct request result: %v", fsErr)
	}

	if err != nil {
		return "", err
	}
	if resp.StatusCode() != 200 {
		return "", models.NewScraperStatusError(
			"JavLibrary",
			resp.StatusCode(),
			fmt.Sprintf("JavLibrary returned status code %d", resp.StatusCode()),
		)
	}

	html := string(resp.Body())
	if models.IsCloudflareChallengePage(html) {
		return "", models.NewScraperChallengeError(
			"JavLibrary",
			"JavLibrary returned a Cloudflare challenge page (request blocked; enable FlareSolverr or adjust proxy/IP)",
		)
	}

	return html, nil
}

// parseDetailPage parses a JavLibrary detail page HTML
func (s *Scraper) parseDetailPage(html string, id string, sourceURL string, language string) (*models.ScraperResult, error) {
	result := &models.ScraperResult{
		Source:    s.Name(),
		SourceURL: sourceURL,
		Language:  language,
		ID:        id,
	}

	// Title: from <title> tag, strip " - JAVLibrary" suffix and ID prefix
	result.Title = s.extractTitle(html, id)

	// Structured fields from video_info div
	result.ReleaseDate = s.extractReleaseDate(html)
	result.Runtime = s.extractRuntime(html)
	result.Director = s.extractField(html, "video_director")
	result.Maker = s.extractField(html, "video_maker")
	result.Label = s.extractField(html, "video_label")
	result.Series = s.extractSeries(html)
	result.Genres = s.extractGenres(html)
	result.Actresses = s.extractActresses(html)

	// Description from video_review div
	result.Description = s.extractDescription(html)

	// Rating from video_rating div
	result.Rating = s.extractRating(html)

	// Media URLs
	result.CoverURL = s.extractCoverURL(html)
	result.ScreenshotURL = s.extractScreenshotURLs(html)
	result.TrailerURL = s.extractTrailerURL(html)

	// Filter out cover URL from screenshots if present
	if result.CoverURL != "" && len(result.ScreenshotURL) > 0 {
		filtered := make([]string, 0, len(result.ScreenshotURL))
		for _, ss := range result.ScreenshotURL {
			// Skip if screenshot URL exactly matches cover URL
			if ss == result.CoverURL {
				continue
			}
			posterVariant := strings.Replace(result.CoverURL, "pl.jpg", "ps.jpg", 1)
			if ss == posterVariant {
				continue
			}
			filtered = append(filtered, ss)
		}
		result.ScreenshotURL = filtered
	}

	// Poster: derive from cover URL (replace "pl.jpg" with "ps.jpg")
	if result.CoverURL != "" {
		result.PosterURL = strings.Replace(result.CoverURL, "pl.jpg", "ps.jpg", 1)
	}

	return result, nil
}

// extractTitle extracts the movie title from HTML
func (s *Scraper) extractTitle(html string, id string) string {
	re := regexp.MustCompile(`<title>([^<]+)</title>`)
	matches := re.FindStringSubmatch(html)
	if len(matches) < 2 {
		return ""
	}
	title := strings.TrimSpace(matches[1])

	// Strip " - JAVLibrary" suffix
	if idx := strings.LastIndex(title, " - JAVLibrary"); idx > 0 {
		title = title[:idx]
	}

	// Strip the ID prefix (e.g., "IPX-123 " from the beginning)
	idPrefix := id + " "
	title = strings.TrimPrefix(title, idPrefix)

	return strings.TrimSpace(title)
}

// extractCoverURL extracts the cover image URL from the video_jacket_img element
func (s *Scraper) extractCoverURL(html string) string {
	re := regexp.MustCompile(`id="video_jacket_img"[^>]*src="([^"]+)"`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return matches[1]
	}

	// Fallback: look for jacket image in video_jacket link
	re = regexp.MustCompile(`id="video_jacket"[^>]*href="([^"]+)"`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 1 {
		url := matches[1]
		if strings.HasPrefix(url, "//") {
			url = "https:" + url
		}
		return url
	}

	return ""
}

// extractReleaseDate extracts release date from video_date div
func (s *Scraper) extractReleaseDate(html string) *time.Time {
	re := regexp.MustCompile(`id="video_date"[^>]*>[\s\S]*?class="text">(\d{4}-\d{2}-\d{2})<`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		t, err := time.Parse("2006-01-02", matches[1])
		if err == nil {
			return &t
		}
	}

	// Fallback: any date near "Release Date:"
	re = regexp.MustCompile(`Release Date:[\s\S]*?(\d{4}-\d{2}-\d{2})`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 1 {
		t, err := time.Parse("2006-01-02", matches[1])
		if err == nil {
			return &t
		}
	}
	return nil
}

// extractRuntime extracts runtime from video_length div
func (s *Scraper) extractRuntime(html string) int {
	// JavLibrary uses "Length:" with <span class="text">120</span> min(s)
	re := regexp.MustCompile(`id="video_length"[^>]*>[\s\S]*?class="text">(\d+)<`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		val, err := strconv.Atoi(matches[1])
		if err == nil {
			return val
		}
	}

	// Fallback
	re = regexp.MustCompile(`(?:Length|Duration):[^0-9]*(\d+)\s*min`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 1 {
		val, err := strconv.Atoi(matches[1])
		if err == nil {
			return val
		}
	}
	return 0
}

// extractField extracts a field value from a video_info div by its ID
// Works for video_director, video_maker, video_label
func (s *Scraper) extractField(html string, divID string) string {
	// Pattern: <div id="video_director" ...> ... <a ...>Value</a> ...
	pattern := fmt.Sprintf(`id="%s"[^>]*>[\s\S]*?<a[^>]*>([^<]+)</a>`, divID)
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// extractGenres extracts genres from the video_genres div
func (s *Scraper) extractGenres(html string) []string {
	// Genres are in: <span class="genre"><a href="..." rel="tag">GenreName</a></span>
	re := regexp.MustCompile(`class="genre"[^>]*><a[^>]*>([^<]+)</a>`)
	matches := re.FindAllStringSubmatch(html, -1)

	var genres []string
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) > 1 {
			genre := strings.TrimSpace(m[1])
			if genre != "" && !seen[genre] {
				genres = append(genres, genre)
				seen[genre] = true
			}
		}
	}
	return genres
}

// extractActresses extracts actress info from the video_cast div
func (s *Scraper) extractActresses(html string) []models.ActressInfo {
	// Cast is in: <span class="star"><a href="..." rel="tag">ActressName</a></span>
	re := regexp.MustCompile(`class="star"[^>]*><a[^>]*>([^<]+)</a>`)
	matches := re.FindAllStringSubmatch(html, -1)

	var actresses []models.ActressInfo
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) > 1 {
			name := strings.TrimSpace(m[1])
			if name != "" && !seen[name] {
				seen[name] = true
				// Parse name into first/last (JavLibrary uses Western order)
				parts := strings.Fields(name)
				firstName := ""
				lastName := ""
				if len(parts) > 0 {
					firstName = parts[0]
				}
				if len(parts) > 1 {
					lastName = strings.Join(parts[1:], " ")
				}
				actresses = append(actresses, models.ActressInfo{
					FirstName: firstName,
					LastName:  lastName,
				})
			}
		}
	}
	return actresses
}

// extractDescription extracts the movie description from the page.
// JavLibrary typically doesn't include movie descriptions on detail pages.
// We check the meta description tag first, then fall back to any review text.
func (s *Scraper) extractDescription(html string) string {
	// First check meta description tag
	re := regexp.MustCompile(`(?i)<meta[^>]*name=["']description["'][^>]*content=["']([^"']+)["']`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	// Fallback 1: look for text in video_review div
	re = regexp.MustCompile(`id="video_review"[^>]*>[\s\S]*?class="text"[^>]*>([\s\S]*?)</td>`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 1 {
		desc := strings.TrimSpace(matches[1])
		// Filter out if it's just rating stars without actual review text
		if len(desc) >= 20 && !strings.Contains(desc, "star-rating-control") {
			return desc
		}
	}
	// Fallback 2: look for any text content that looks like a description
	// (more than 50 chars and not just a rating)
	re = regexp.MustCompile(`id="video_review"[^>]*>([\s\S]*?)</div>`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 1 {
		// Extract just the text content, stripping HTML tags
		text := strings.TrimSpace(matches[1])
		text = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(text, " ")
		text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
		if len(text) > 50 && !strings.Contains(text, "star-rating-control") {
			return text
		}
	}
	return ""
}

// extractSeries extracts the series name from the video_series div
func (s *Scraper) extractSeries(html string) string {
	// Series is in: <div id="video_series"><a href="...">SeriesName</a></div>
	re := regexp.MustCompile(`id="video_series"[^>]*>[\s\S]*?<a[^>]*>([^<]+)</a>`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	// Fallback: any text near "Series:"
	re = regexp.MustCompile(`Series:[\s\S]*?<a[^>]*>([^<]+)</a>`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// extractRating extracts the movie rating (score) from the video_rating div
func (s *Scraper) extractRating(html string) *models.Rating {
	// Rating is in: <div id="video_rating"><span class="num">4.5</span> / 5.0</div>
	re := regexp.MustCompile(`id="video_rating"[^>]*>[\s\S]*?<span[^>]*class="num"[^>]*>([\d.]+)</span>`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		if score, err := strconv.ParseFloat(matches[1], 64); err == nil {
			return &models.Rating{Score: score}
		}
	}
	// Fallback: any score pattern like "4.5 / 5.0" or "4.5 out of 5.0"
	re = regexp.MustCompile(`([\d.]+)\s*(?:/|out\s+of|of)\s*([\d.]+)`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 2 {
		if score, err := strconv.ParseFloat(matches[1], 64); err == nil {
			if maxScore, parseErr := strconv.ParseFloat(matches[2], 64); parseErr == nil && maxScore > 0 {
				// Normalize to 5-star scale
				score = score * 5 / maxScore
			}
			return &models.Rating{Score: score}
		}
	}
	return nil
}

// extractScreenshotURLs extracts screenshot/image gallery URLs from the page
func (s *Scraper) extractScreenshotURLs(html string) []string {
	var screenshotURLs []string
	seen := make(map[string]bool)

	// Helper to add URL if not seen and not a placeholder
	addURL := func(url string) {
		if url == "" || seen[url] {
			return
		}
		// Filter out redirect URLs
		if strings.Contains(url, "redirect.php") || strings.Contains(url, "redirect%") {
			return
		}
		// Filter out placeholder/loading images
		if strings.Contains(url, "loading") || strings.Contains(url, "blank") ||
			strings.Contains(url, "placeholder") || strings.Contains(url, "icon") ||
			strings.Contains(url, "head2.jpg") { // Non-screenshot URLs on JavLibrary
			return
		}
		// Filter out thumbnail versions - prefer full-size images
		// The jp-XX.jpg pattern appears to be thumbnails while XX.jpg are full images
		// e.g., 118abp880-1.jpg is full, 118abp880jp-1.jpg is thumbnail
		if strings.Contains(url, "jp-") || strings.HasSuffix(url, "jp.jpg") {
			return
		}
		// Filter out DMM cover URLs (pl.jpg = cover, ps.jpg = poster)
		// These should not be included as screenshots
		if strings.Contains(url, "pl.jpg") || strings.Contains(url, "ps.jpg") {
			return
		}
		seen[url] = true
		screenshotURLs = append(screenshotURLs, url)
	}

	// Look for data-src attributes (lazy loading) - must check before src
	re := regexp.MustCompile(`data-src="([^"]+\.jpg[^"]*)"`)
	matches := re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) > 1 {
			addURL(m[1])
		}
	}

	// Look for sample movie images - pattern: src="URL" with "sample" in URL
	re = regexp.MustCompile(`src="([^"]*sample[^"]*\.jpg[^"]*)"`)
	matches = re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) > 1 {
			addURL(m[1])
		}
	}

	// Also look for jp-1.jpg, jp-2.jpg, etc. patterns (JavLibrary/DMM screenshot pattern)
	// Pattern: ID + jp + number + .jpg (e.g., abp880jp-1.jpg, 118abp880jp-1.jpg)
	re = regexp.MustCompile(`src="([^"]*jp-\d+\.jpg[^"]*)"`)
	matches = re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) > 1 {
			addURL(m[1])
		}
	}

	// Also look for pic01, pic02, etc. patterns
	re = regexp.MustCompile(`src="([^"]*pic\d+\.jpg[^"]*)"`)
	matches = re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) > 1 {
			addURL(m[1])
		}
	}

	// Look for c.impact.jp URLs (JavLibrary's image CDN) with digit-digit.jpg pattern
	// e.g., /abc123/01.jpg, /abak-001/01.jpg
	re = regexp.MustCompile(`src="([^"]*c\.impact\.jp[^"]*\/(\d+)\.jpg[^"]*)"`)
	matches = re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) > 1 {
			addURL(m[1])
		}
	}

	// Also try broader pattern for c.impact.jp with any digits before .jpg
	re = regexp.MustCompile(`src="([^"]*impact\.jp[^"]*\d{2,}\.jpg[^"]*)"`)
	matches = re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) > 1 {
			addURL(m[1])
		}
	}

	// Look for any src attribute containing .jpg that looks like a screenshot
	// Require numeric pattern (e.g., /04.jpg, /001.jpg) typical of gallery screenshots
	re = regexp.MustCompile(`src="([^"]*\/(\d+)\.jpg[^"]*)"`)
	matches = re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) > 1 {
			addURL(m[1])
		}
	}

	// Look for href links to images in gallery divs (gallery links to full-size images)
	re = regexp.MustCompile(`id="video_gallery"[^>]*>[\s\S]*?href="([^"]*\.jpg[^"]*)"`)
	matches = re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) > 1 {
			addURL(m[1])
		}
	}

	// Fallback: look for any href links to jpg files in the page body
	// that look like screenshots (contain sample, jp-, pic, or impact)
	re = regexp.MustCompile(`href="([^"]*sample[^"]*\.jpg[^"]*)"`)
	matches = re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) > 1 {
			addURL(m[1])
		}
	}

	re = regexp.MustCompile(`href="([^"]*jp-\d+\.jpg[^"]*)"`)
	matches = re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) > 1 {
			addURL(m[1])
		}
	}

	re = regexp.MustCompile(`href="([^"]*impact\.jp[^"]*\.jpg[^"]*)"`)
	matches = re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) > 1 {
			addURL(m[1])
		}
	}

	// Broader fallback: any href to .jpg with numeric pattern typical of gallery screenshots
	re = regexp.MustCompile(`href="([^"]*\/(\d+)\.jpg[^"]*)"`)
	matches = re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) > 1 {
			addURL(m[1])
		}
	}

	return screenshotURLs
}

// extractTrailerURL extracts the trailer/sample video URL
func (s *Scraper) extractTrailerURL(html string) string {
	// Look for video source tags with sample/trailer context
	re := regexp.MustCompile(`src="([^"]*sample[^"]*\.mp4[^"]*)"`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return matches[1]
	}

	// Look for video tag with sample context
	re = regexp.MustCompile(`<video[^>]*src="([^"]+)"[^>]*>[^<]*sample`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return matches[1]
	}

	// Look for a href with mp4 in sample_movie or similar
	re = regexp.MustCompile(`href="([^"]*sample_movie[^"]*\.mp4[^"]*)"`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return matches[1]
	}

	// Fallback: any mp4 URL in a sample context
	re = regexp.MustCompile(`href="([^"]*\.mp4[^"]*)"`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return matches[1]
	}

	return ""
}

// Compiled regexes for extracting video entries from JavLibrary search result pages.
var reVideoThumbDiv = regexp.MustCompile(`<div[^>]*class="video"[^>]*>[\s\S]*?<div class="id">([^<]+)</div>`)
var reVideoThumbID = regexp.MustCompile(`id="vid_([a-zA-Z0-9]+)"`)
var reLegacyHrefLang = regexp.MustCompile(`href="(/?(?:en|ja|cn|tw)/\?v=[a-zA-Z0-9]+)"`)
var reLegacyHrefQuery = regexp.MustCompile(`href="(\?v=[a-zA-Z0-9]+)"`)

// extractMovieURLFromHTML extracts the movie detail link from search results
func (s *Scraper) extractMovieURLFromHTML(html string, searchID string) string {
	// Pattern 1: Current JavLibrary format uses relative HTML links
	// e.g., <div class="video" id="vid_javliat76u"> with <div class="id">ONED-025</div>
	matches := reVideoThumbDiv.FindAllStringSubmatch(html, -1)

	firstVidID := ""
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		displayID := strings.ToUpper(strings.ReplaceAll(m[1], "-", ""))
		normalizedSearch := strings.ToUpper(strings.ReplaceAll(searchID, "-", ""))

		// Extract vid_ ID from the full match (order-independent)
		vidMatch := reVideoThumbID.FindStringSubmatch(m[0])
		if len(vidMatch) < 2 {
			continue
		}
		vidID := vidMatch[1]

		if displayID == normalizedSearch {
			return "?v=" + vidID
		}
		if firstVidID == "" {
			firstVidID = vidID
		}
	}

	if firstVidID != "" {
		logging.Debugf("JavLibrary: No exact match for %s, using first result vid=%s", searchID, firstVidID)
		return "?v=" + firstVidID
	}

	// Pattern 2: Legacy format with ?v= query parameter
	matches2 := reLegacyHrefLang.FindStringSubmatch(html)
	if len(matches2) > 1 {
		return matches2[1]
	}

	matches2 = reLegacyHrefQuery.FindStringSubmatch(html)
	if len(matches2) > 1 {
		return matches2[1]
	}
	return ""
}

// isValidLanguage checks if the language code is supported by JavLibrary
func isValidLanguage(lang string) bool {
	for _, l := range SupportedLanguages {
		if l == lang {
			return true
		}
	}
	return false
}
