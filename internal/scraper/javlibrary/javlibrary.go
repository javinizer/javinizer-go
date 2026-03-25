package javlibrary

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	scraper "github.com/javinizer/javinizer-go/internal/scraper"
)

// SupportedLanguages lists the language codes supported by JavLibrary.
// en = English, ja = Japanese, cn = Chinese (Simplified), tw = Chinese (Traditional)
var SupportedLanguages = []string{"en", "ja", "cn", "tw"}

// Scraper implements the models.Scraper interface for JavLibrary
type Scraper struct {
	client        *resty.Client
	flaresolverr  *httpclient.FlareSolverr
	cfg           *config.JavLibraryConfig
	enabled       bool
	baseURL       string
	language      string
	proxyOverride *config.ProxyConfig
	downloadProxy *config.ProxyConfig
}

// New creates a new JavLibrary scraper.
func New(cfg *config.Config) *Scraper {
	scraperCfg := cfg.Scrapers.JavLibrary
	proxyConfig := cfg.Scrapers.Proxy

	// Build FlareSolverrConfig for NewHTTPClient.
	// When scraper-specific proxy is set (scraperCfg.Proxy != nil), use its FlareSolverr.
	// Otherwise, inherit FlareSolverr from global proxy (proxyConfig).
	// This allows use_flaresolverr=true with only global proxy config.
	flaresolverrConfig := config.FlareSolverrConfig{}
	if scraperCfg.Proxy != nil {
		flaresolverrConfig = scraperCfg.Proxy.FlareSolverr
	} else if proxyConfig.FlareSolverr.Enabled {
		// No scraper-specific proxy; inherit FlareSolverr from global proxy
		flaresolverrConfig = proxyConfig.FlareSolverr
	}
	// If scraper-level use_flaresolverr is set, ensure FlareSolverr is enabled
	// (can be used with global proxy + global flaresolverr without scraper-specific proxy)
	if scraperCfg.UseFlareSolverr {
		flaresolverrConfig.Enabled = true
	}

	// Build ScraperConfig for NewHTTPClient (HTTP-01 pattern)
	configForHTTP := &config.ScraperConfig{
		Enabled:          scraperCfg.Enabled,
		Language:         scraperCfg.Language,
		RateLimit:        scraperCfg.RequestDelay,
		Timeout:          30, // default, will be overridden if ScraperConfig has it
		RetryCount:       3,  // default
		UseFakeUserAgent: scraperCfg.UseFakeUserAgent,
		UserAgent:        scraperCfg.FakeUserAgent,
		Proxy:            scraperCfg.Proxy,
		DownloadProxy:    scraperCfg.DownloadProxy,
		FlareSolverr:     flaresolverrConfig,
	}

	client, flaresolverr, err := NewHTTPClient(configForHTTP, &proxyConfig, scraperCfg.UseFlareSolverr)
	usingProxy := err == nil && proxyConfig.Enabled && strings.TrimSpace(proxyConfig.URL) != ""
	if err != nil {
		logging.Errorf("JavLibrary: Failed to create HTTP client with proxy/flaresolverr: %v, using explicit no-proxy fallback", err)
		client = httpclient.NewRestyClientNoProxy(30*time.Second, 3)
		flaresolverr = nil
	}

	baseURL := scraperCfg.BaseURL
	if baseURL == "" {
		baseURL = "http://www.javlibrary.com"
	}

	// Normalize language to a supported value
	language := scraperCfg.Language
	if language == "" {
		language = "en"
	}
	if !isValidLanguage(language) {
		logging.Warnf("JavLibrary: unsupported language %q, falling back to 'en' (supported: %v)", language, SupportedLanguages)
		language = "en"
	}

	userAgent := config.ResolveScraperUserAgent(
		cfg.Scrapers.UserAgent,
		scraperCfg.UseFakeUserAgent,
		scraperCfg.FakeUserAgent,
	)
	client.SetHeader("User-Agent", userAgent)

	if usingProxy {
		logging.Infof("JavLibrary: Using proxy %s", httpclient.SanitizeProxyURL(proxyConfig.URL))
	}

	return &Scraper{
		client:        client,
		flaresolverr:  flaresolverr,
		cfg:           &scraperCfg,
		enabled:       scraperCfg.Enabled,
		baseURL:       baseURL,
		language:      language,
		proxyOverride: scraperCfg.Proxy,
		downloadProxy: scraperCfg.DownloadProxy,
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
func (s *Scraper) Config() *config.ScraperConfig {
	return &config.ScraperConfig{
		Enabled:          s.cfg.Enabled,
		Language:         s.cfg.Language,
		RateLimit:        s.cfg.RequestDelay,
		Timeout:          30, // default, hardcoded in HTTP client creation
		RetryCount:       3,  // default, hardcoded in HTTP client creation
		UseFakeUserAgent: s.cfg.UseFakeUserAgent,
		UserAgent:        s.cfg.FakeUserAgent,
		Proxy:            s.cfg.Proxy,
		DownloadProxy:    s.cfg.DownloadProxy,
		FlareSolverr: config.FlareSolverrConfig{
			Enabled: s.cfg.UseFlareSolverr,
		},
	}
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
	if host == "" || !strings.Contains(host, "javlibrary") {
		return nil, nil, false
	}
	return s.downloadProxy, s.proxyOverride, true
}

// GetURL returns the search URL for a given ID
func (s *Scraper) GetURL(id string) (string, error) {
	return fmt.Sprintf("%s/%s/vl_searchbyid.php?keyword=%s", s.baseURL, s.language, id), nil
}

// Search searches for a movie by ID
func (s *Scraper) Search(id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("JavLibrary scraper is disabled")
	}

	searchURL, err := s.GetURL(id)
	if err != nil {
		return nil, err
	}

	// Fetch the search page
	html, err := s.fetchPage(searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch search page: %w", err)
	}

	// Check if search landed directly on a detail page (contains video_info div)
	if strings.Contains(html, `id="video_info"`) {
		logging.Debugf("JavLibrary: Search landed directly on detail page for %s", id)
		return s.parseDetailPage(html, id, searchURL)
	}

	// Otherwise, look for a movie link in search results
	detailPath := s.extractMovieURLFromHTML(html)
	if detailPath == "" {
		return nil, models.NewScraperNotFoundError("JavLibrary", fmt.Sprintf("movie %s not found on JavLibrary", id))
	}

	// Build full detail URL
	detailURL := detailPath
	if !strings.HasPrefix(detailPath, "http") {
		detailURL = s.baseURL + "/" + s.language + "/" + strings.TrimPrefix(detailPath, "/")
	}

	logging.Debugf("JavLibrary: Fetching detail page: %s", detailURL)
	detailHTML, err := s.fetchPage(detailURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch detail page: %w", err)
	}

	return s.parseDetailPage(detailHTML, id, detailURL)
}

// fetchPage fetches a page via FlareSolverr (if enabled) or direct HTTP
func (s *Scraper) fetchPage(url string) (string, error) {
	// Try direct request first and only escalate to FlareSolverr on blocked/challenge responses.
	resp, err := s.client.R().Get(url)
	if err == nil && resp != nil && resp.StatusCode() == 200 {
		html := string(resp.Body())
		if !models.IsCloudflareChallengePage(html) {
			return html, nil
		}
		logging.Warnf("JavLibrary: Direct request returned Cloudflare challenge, escalating to FlareSolverr: %s", url)
	} else if err == nil && resp != nil {
		logging.Debugf("JavLibrary: Direct request returned status %d for %s", resp.StatusCode(), url)
	}

	// Fallback to FlareSolverr if enabled.
	if s.flaresolverr != nil && s.cfg.UseFlareSolverr {
		logging.Infof("JavLibrary: Using FlareSolverr for %s", url)
		html, cookies, fsErr := s.flaresolverr.ResolveURL(url)
		if fsErr == nil {
			if models.IsCloudflareChallengePage(html) {
				return "", models.NewScraperChallengeError(
					"JavLibrary",
					"JavLibrary returned a Cloudflare challenge page (request blocked; check FlareSolverr/proxy configuration)",
				)
			}
			// Apply cookies to client for subsequent requests
			for _, c := range cookies {
				s.client.SetCookie(&c)
			}
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
func (s *Scraper) parseDetailPage(html string, id string, sourceURL string) (*models.ScraperResult, error) {
	result := &models.ScraperResult{
		Source:    s.Name(),
		SourceURL: sourceURL,
		Language:  s.language,
		ID:        id,
	}

	// Title: from <title> tag, strip " - JAVLibrary" suffix and ID prefix
	result.Title = s.extractTitle(html, id)

	// Cover image: video_jacket_img
	result.CoverURL = s.extractCoverURL(html)

	// Poster: derive from cover URL (replace "pl.jpg" with "ps.jpg")
	if result.CoverURL != "" {
		result.PosterURL = strings.Replace(result.CoverURL, "pl.jpg", "ps.jpg", 1)
	}

	// Structured fields from video_info div
	result.ReleaseDate = s.extractReleaseDate(html)
	result.Runtime = s.extractRuntime(html)
	result.Director = s.extractField(html, "video_director")
	result.Maker = s.extractField(html, "video_maker")
	result.Label = s.extractField(html, "video_label")
	result.Genres = s.extractGenres(html)
	result.Actresses = s.extractActresses(html)

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

// extractMovieURLFromHTML extracts the movie detail link from search results
func (s *Scraper) extractMovieURLFromHTML(html string) string {
	// Search results contain links like: href="?v=javli43uqe" or href="/en/?v=javli43uqe"
	re := regexp.MustCompile(`href="(/?\w{2}/\?v=[a-zA-Z0-9]+)"`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return matches[1]
	}

	// Also try relative format: href="?v=..."
	re = regexp.MustCompile(`href="(\?v=[a-zA-Z0-9]+)"`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return matches[1]
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

// ValidateConfig validates the scraper configuration.
// Returns error if config is invalid, nil if valid.
func (s *Scraper) ValidateConfig(cfg *config.ScraperConfig) error {
	if cfg == nil {
		return fmt.Errorf("javlibrary: config is nil")
	}
	if !cfg.Enabled {
		return nil // Disabled is valid
	}
	// Validate rate limit
	if cfg.RateLimit < 0 {
		return fmt.Errorf("javlibrary: rate_limit must be non-negative, got %d", cfg.RateLimit)
	}
	// Validate retry count
	if cfg.RetryCount < 0 {
		return fmt.Errorf("javlibrary: retry_count must be non-negative, got %d", cfg.RetryCount)
	}
	// Validate timeout
	if cfg.Timeout < 0 {
		return fmt.Errorf("javlibrary: timeout must be non-negative, got %d", cfg.Timeout)
	}
	return nil
}

func init() {
	scraper.RegisterScraper("javlibrary", func(cfg *config.Config, db *database.DB) (models.Scraper, error) {
		return New(cfg), nil
	})
}
