package r18dev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

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
	baseURL = "https://r18.dev"
	apiURL  = baseURL + "/videos/vod/movies/detail/-/combined=%s/json"
)

// Package-level compiled regex for performance
var r18IDRegex = regexp.MustCompile(`/(id|combined)=([^/?&]+)`)

// Scraper implements the R18.dev scraper
type Scraper struct {
	client            *resty.Client
	enabled           bool
	language          string
	maxRetries        int
	respectRetryAfter bool
	proxyOverride     *config.ProxyConfig
	downloadProxy     *config.ProxyConfig
	rateLimiter       *ratelimit.Limiter
	settings          config.ScraperSettings // stores the full settings for Config() method
}

// New creates a new R18.dev scraper
func New(settings config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) *Scraper {
	result := httpclient.InitScraperClient(&settings, globalProxy, globalFlareSolverr,
		httpclient.WithScraperHeaders(httpclient.R18DevHeaders()),
		httpclient.WithScraperHeaders(httpclient.RefererHeader("https://r18.dev/")),
		httpclient.WithScraperUserAgent(settings.UserAgent),
	)
	client := result.Client

	language := scraperutil.NormalizeLanguage(settings.Language)

	// Add browser-like headers to help bypass protection
	client.SetHeader("Accept", "application/json, text/html, */*")
	if language == "ja" {
		client.SetHeader("Accept-Language", "ja,en-US;q=0.8,en;q=0.6")
	} else {
		client.SetHeader("Accept-Language", "en-US,en;q=0.9,ja;q=0.8")
	}
	client.SetHeader("Accept-Encoding", "gzip, deflate, br")
	client.SetHeader("Connection", "keep-alive")

	if result.ProxyEnabled && result.ProxyProfile.URL != "" {
		logging.Infof("R18Dev: Using proxy %s", httpclient.SanitizeProxyURL(result.ProxyProfile.URL))
	}

	// Set defaults for rate limiting if not configured
	maxRetries := settings.RetryCount
	if maxRetries == 0 {
		maxRetries = 3 // Default to 3 retries
	}

	respectRetryAfter := false // Note: Now in R18DevConfig, not accessible via ScraperSettings

	scraper := &Scraper{
		client:            client,
		enabled:           settings.Enabled,
		language:          language,
		rateLimiter:       ratelimit.NewLimiter(time.Duration(settings.RateLimit) * time.Millisecond),
		maxRetries:        maxRetries,
		respectRetryAfter: respectRetryAfter,
		proxyOverride:     settings.Proxy,
		downloadProxy:     settings.DownloadProxy,
		settings:          settings,
	}

	if settings.RateLimit > 0 {
		logging.Infof("R18Dev: Rate limiting enabled with %v delay between requests", time.Duration(settings.RateLimit)*time.Millisecond)
	}

	return scraper
}

// Name returns the scraper identifier
func (s *Scraper) Name() string {
	return "r18dev"
}

// IsEnabled returns whether the scraper is enabled
func (s *Scraper) IsEnabled() bool {
	return s.enabled
}

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
	return host == "r18.dev" || strings.HasSuffix(host, ".r18.dev") ||
		host == "r18.com" || strings.HasSuffix(host, ".r18.com")
}

// ExtractIDFromURL extracts the movie ID from an R18.dev URL
func (s *Scraper) ExtractIDFromURL(urlStr string) (string, error) {
	matches := r18IDRegex.FindStringSubmatch(urlStr)
	if len(matches) > 2 {
		return matches[2], nil
	}

	return "", fmt.Errorf("failed to extract ID from R18.dev URL")
}

func (s *Scraper) ScrapeURL(ctx context.Context, urlStr string) (*models.ScraperResult, error) {
	if !s.CanHandleURL(urlStr) {
		return nil, models.NewScraperNotFoundError("R18.dev", "URL not handled by R18.dev scraper")
	}

	if !s.enabled {
		return nil, fmt.Errorf("R18.dev scraper is disabled")
	}

	id, err := s.ExtractIDFromURL(urlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to extract ID from URL: %w", err)
	}

	logging.Debugf("R18.dev ScrapeURL: Extracted ID %s from URL %s", id, urlStr)

	resp, err := s.doRequestWithRetryCtx(ctx, urlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data from R18.dev: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, models.NewScraperStatusError(
			"R18.dev",
			resp.StatusCode(),
			fmt.Sprintf("R18.dev returned status code %d", resp.StatusCode()),
		)
	}

	contentType := resp.Header().Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		return nil, models.NewScraperNotFoundError("R18.dev", "movie not found on R18.dev (returned HTML)")
	}

	var data R18Response
	if err := json.Unmarshal(resp.Body(), &data); err != nil {
		bodyPreview := string(resp.Body())
		if len(bodyPreview) > 200 {
			bodyPreview = bodyPreview[:200]
		}
		return nil, fmt.Errorf("failed to parse R18.dev response (preview: %s): %w", bodyPreview, err)
	}

	return s.parseResponse(ctx, &data, urlStr)
}

// ValidateConfig validates the scraper configuration.
// Returns error if config is invalid, nil if valid.
// Called by callers before creating the scraper to validate configuration.
func (s *Scraper) ValidateConfig(cfg *config.ScraperSettings) error {
	if cfg == nil {
		return fmt.Errorf("r18dev: config is nil")
	}
	if !cfg.Enabled {
		return nil // Disabled is valid
	}
	// Validate language if set
	switch strings.ToLower(strings.TrimSpace(cfg.Language)) {
	case "", "en":
		// Valid
	case "ja":
		// Valid
	default:
		return fmt.Errorf("r18dev: language must be 'en' or 'ja', got %q", cfg.Language)
	}
	// Validate rate limit
	if cfg.RateLimit < 0 {
		return fmt.Errorf("r18dev: rate_limit must be non-negative, got %d", cfg.RateLimit)
	}
	// Validate retry count
	if cfg.RetryCount < 0 {
		return fmt.Errorf("r18dev: retry_count must be non-negative, got %d", cfg.RetryCount)
	}
	// Validate timeout
	if cfg.Timeout < 0 {
		return fmt.Errorf("r18dev: timeout must be non-negative, got %d", cfg.Timeout)
	}
	return nil
}

// ResolveDownloadProxyForHost declares R18.dev-owned media hosts for downloader proxy routing.
func (s *Scraper) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil, nil, false
	}
	if host == "r18.dev" || strings.HasSuffix(host, ".r18.dev") {
		return s.downloadProxy, s.proxyOverride, true
	}
	return nil, nil, false
}

func (s *Scraper) GetURL(id string) (string, error) {
	return s.getURLCtx(context.Background(), id)
}

func (s *Scraper) getURLCtx(ctx context.Context, id string) (string, error) {
	normalized := normalizeID(id)
	return fmt.Sprintf(apiURL, normalized), nil
}

// doRequestWithRetry performs an HTTP request with retry logic for rate limiting
// doRequestWithRetryCtx performs an HTTP request with retry logic for rate limiting and context support
func (s *Scraper) doRequestWithRetryCtx(ctx context.Context, url string) (*resty.Response, error) {
	var resp *resty.Response
	var err error

	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		if err := s.rateLimiter.Wait(ctx); err != nil {
			return nil, err
		}

		resp, err = s.client.R().
			SetContext(ctx).
			SetHeader("Accept-Encoding", "").
			Get(url)

		// Handle rate limiting
		if resp != nil && (resp.StatusCode() == 429 || resp.StatusCode() == 503) {
			retryAfter := resp.Header().Get("Retry-After")

			if attempt < s.maxRetries {
				var waitTime time.Duration

				// Parse Retry-After header if configured to respect it
				if s.respectRetryAfter && retryAfter != "" {
					// Try to parse as seconds (integer)
					if seconds, parseErr := strconv.Atoi(retryAfter); parseErr == nil {
						waitTime = time.Duration(seconds) * time.Second
					}
				}

				// Use exponential backoff if no Retry-After or not configured to respect it
				if waitTime == 0 {
					waitTime = time.Duration(1<<uint(attempt)) * time.Second // 1s, 2s, 4s, 8s...
				}

				logging.Warnf("R18: Rate limited (429), retrying in %v (attempt %d/%d)", waitTime, attempt+1, s.maxRetries)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(waitTime):
				}
				continue
			}

			// Max retries exceeded
			return nil, models.NewScraperHTTPError("R18", resp.StatusCode(), fmt.Sprintf("rate limited after %d retries", s.maxRetries))
		}

		// Request successful or non-rate-limit error
		break
	}

	return resp, err
}

// Search searches for and scrapes metadata for a given movie ID
// Search searches for and scrapes metadata for a given movie ID with context support
func (s *Scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	// Step 1: Try to lookup content_id using dvd_id with multiple ID variations
	// R18.dev uses dvd_id to find the content_id, then uses content_id for the full data

	// Generate ID variations to try (original first, then with DMM prefix stripped)
	idVariations := []string{
		normalizeIDWithoutStripping(id), // Try original ID first (e.g., "61mdb087")
		normalizeID(id),                 // Then try with DMM prefix stripped (e.g., "mdb087")
	}

	// Remove duplicates
	seen := make(map[string]bool)
	uniqueVariations := []string{}
	for _, variation := range idVariations {
		if !seen[variation] {
			seen[variation] = true
			uniqueVariations = append(uniqueVariations, variation)
		}
	}

	var contentID string
	var successfulVariation string

	// Try each variation until we find a match
	for _, idVariation := range uniqueVariations {
		dvdIDURL := fmt.Sprintf("%s/videos/vod/movies/detail/-/dvd_id=%s/json", baseURL, idVariation)
		logging.Debugf("R18: Trying dvd_id lookup: %s", idVariation)

		resp, err := s.doRequestWithRetryCtx(ctx, dvdIDURL)
		if err != nil {
			logging.Debugf("R18: Failed to lookup with %s: %v", idVariation, err)
			continue
		}

		// If dvd_id lookup succeeds, extract and validate content_id
		if resp.StatusCode() == 200 {
			contentType := resp.Header().Get("Content-Type")
			if !strings.Contains(contentType, "text/html") {
				var lookupData struct {
					ContentID string `json:"content_id"`
					DVDID     string `json:"dvd_id"`
				}
				if err := json.Unmarshal(resp.Body(), &lookupData); err == nil && lookupData.ContentID != "" {
					returnedDVDID := strings.ToLower(strings.ReplaceAll(lookupData.DVDID, "-", ""))
					expectedDVDID := idVariation

					if returnedDVDID == expectedDVDID {
						contentID = lookupData.ContentID
						successfulVariation = idVariation
						logging.Debugf("R18: ✓ Resolved %s (tried: %s) to content-id: %s", id, idVariation, contentID)
						break
					} else if returnedDVDID == "" && contentIDMatchesExpected(lookupData.ContentID, expectedDVDID) {
						contentID = lookupData.ContentID
						successfulVariation = idVariation
						logging.Debugf("R18: ✓ Resolved %s (tried: %s) to content-id: %s (dvd_id empty, validated via content_id)", id, idVariation, contentID)
						break
					} else {
						logging.Debugf("R18: Returned dvd_id '%s' doesn't match expected '%s', skipping", returnedDVDID, expectedDVDID)
					}
				}
			}
		} else {
			logging.Debugf("R18: Content-ID lookup returned status %d for %s", resp.StatusCode(), idVariation)
		}
	}

	if contentID == "" && successfulVariation == "" {
		logging.Debugf("R18: No valid content-id found after trying all variations")
	}

	// Step 2: Fetch full movie data using content_id (or fall back to normalized ID)
	var finalURL string
	var err error
	if contentID != "" {
		// Use content_id if we got it from the lookup
		finalURL = fmt.Sprintf("%s/videos/vod/movies/detail/-/combined=%s/json", baseURL, contentID)
		logging.Debugf("R18: Using resolved content-id URL: %s", finalURL)
	} else {
		// Fall back to using the normalized ID directly
		finalURL, err = s.getURLCtx(ctx, id)
		if err != nil {
			return nil, err
		}
		logging.Debugf("R18: Using normalized ID URL (no content-id found): %s", finalURL)
	}

	resp, err := s.doRequestWithRetryCtx(ctx, finalURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data from R18.dev: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, models.NewScraperStatusError(
			"R18.dev",
			resp.StatusCode(),
			fmt.Sprintf("R18.dev returned status code %d", resp.StatusCode()),
		)
	}

	// Check if response is HTML (404 or error page)
	contentType := resp.Header().Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		return nil, models.NewScraperNotFoundError("R18.dev", "movie not found on R18.dev (returned HTML)")
	}

	var data R18Response
	if err := json.Unmarshal(resp.Body(), &data); err != nil {
		// Log first 200 chars for debugging
		bodyPreview := string(resp.Body())
		if len(bodyPreview) > 200 {
			bodyPreview = bodyPreview[:200]
		}
		return nil, fmt.Errorf("failed to parse R18.dev response (preview: %s): %w", bodyPreview, err)
	}

	return s.parseResponse(ctx, &data, finalURL)
}

// parseResponse converts R18 API response to ScraperResult
func (s *Scraper) parseResponse(ctx context.Context, data *R18Response, sourceURL string) (*models.ScraperResult, error) {
	// Use DVDID if available, otherwise convert ContentID to ID format
	movieID := data.DVDID
	if movieID == "" && data.ContentID != "" {
		movieID = contentIDToID(data.ContentID)
	}

	result := &models.ScraperResult{
		Source:        s.Name(),
		SourceURL:     sourceURL,
		Language:      s.language,
		ID:            movieID,
		ContentID:     data.ContentID,
		Title:         scraperutil.CleanString(selectLocalizedString(s.language, data.TitleEn, data.TitleJA)),
		OriginalTitle: scraperutil.CleanString(data.TitleJA), // Japanese title
		Description:   scraperutil.CleanString(selectLocalizedString(s.language, data.DescriptionEn, data.Description)),
		Runtime:       data.Runtime,
	}

	// Build translations for both languages if API provides both English and Japanese data
	result.Translations = s.buildTranslations(data, movieID)

	// Parse release date (now in YYYY-MM-DD format)
	if data.ReleaseDate != "" {
		t, err := time.Parse("2006-01-02", data.ReleaseDate)
		if err == nil {
			result.ReleaseDate = &t
		}
	}

	// Parse director based on configured language preference.
	// Try directors array first (new API format), then fall back to flat fields
	if len(data.Directors) > 0 {
		// Use directors array (new API format)
		if s.language == "ja" {
			result.Director = scraperutil.CleanString(getPreferredString(data.Directors[0].NameKanji, data.Directors[0].NameRomaji))
		} else {
			result.Director = scraperutil.CleanString(getPreferredString(data.Directors[0].NameRomaji, data.Directors[0].NameKanji))
		}
	} else {
		// Use legacy flat fields
		result.Director = scraperutil.CleanString(selectLocalizedString(s.language, data.DirectorEn, data.Director))
	}

	// Parse maker/studio based on configured language preference.
	result.Maker = scraperutil.CleanString(selectLocalizedString(s.language, data.MakerNameEn, data.MakerNameJa))
	if result.Maker == "" {
		// Fallback to nested structure (legacy format)
		result.Maker = scraperutil.CleanString(selectLocalizedString(s.language, "", data.Maker.Name))
	}

	result.Label = scraperutil.CleanString(selectLocalizedString(s.language, data.LabelNameEn, data.LabelNameJa))
	if result.Label == "" {
		// Fallback to nested structure (legacy format)
		result.Label = scraperutil.CleanString(selectLocalizedString(s.language, "", data.Label.Name))
	}

	// Parse series based on configured language preference.
	if s.language == "ja" {
		result.Series = scraperutil.CleanString(getPreferredString(data.SeriesNameJa, getPreferredString(data.Series.Name, getPreferredString(data.SeriesName, data.SeriesNameEn))))
	} else {
		result.Series = scraperutil.CleanString(getPreferredString(data.SeriesNameEn, getPreferredString(data.SeriesNameJa, getPreferredString(data.Series.Name, data.SeriesName))))
	}

	// Parse actresses with detailed information
	result.Actresses = make([]models.ActressInfo, 0, len(data.Actresses))
	for _, actress := range data.Actresses {
		// Build thumb URL from image_url field
		thumbURL := actress.ImageURL
		if thumbURL != "" && !strings.HasPrefix(thumbURL, "http") {
			thumbURL = "https://pics.dmm.co.jp/mono/actjpgs/" + thumbURL
		}

		// If no image URL provided, construct from romaji name
		if thumbURL == "" && actress.NameRomaji != "" {
			parts := strings.Fields(actress.NameRomaji)
			var filename string
			if len(parts) >= 2 {
				// Reverse the order: lastname_firstname
				lastname := strings.ToLower(parts[1])
				firstname := strings.ToLower(parts[0])
				filename = lastname + "_" + firstname
			} else if len(parts) == 1 {
				// Single name
				filename = strings.ToLower(parts[0])
			}
			// Remove any special characters that might break the URL
			filename = regexp.MustCompile(`[^a-z0-9_]`).ReplaceAllString(filename, "")
			if filename != "" {
				thumbURL = "https://pics.dmm.co.jp/mono/actjpgs/" + filename + ".jpg"
			}
		}

		// Parse romaji name into first/last names
		// Note: R18.dev's name_romaji field is inconsistent - sometimes Western order (First Last),
		// sometimes Japanese order (Last First). We treat it as Western order by default since
		// that's the more common case in their API responses.
		firstName := ""
		lastName := ""
		if actress.NameRomaji != "" {
			parts := strings.Fields(actress.NameRomaji)
			if len(parts) > 0 {
				firstName = parts[0]
			}
			if len(parts) > 1 {
				lastName = parts[1]
			}
		}

		result.Actresses = append(result.Actresses, models.ActressInfo{
			DMMID:        actress.ID,
			FirstName:    firstName,
			LastName:     lastName,
			JapaneseName: scraperutil.CleanString(actress.NameKanji), // Use kanji name as Japanese name
			ThumbURL:     thumbURL,
		})
	}

	// Parse genres (categories) - try new name_en/name_ja fields first, then legacy name field
	result.Genres = make([]string, 0, len(data.Categories))
	for _, category := range data.Categories {
		var genreName string
		if s.language == "ja" {
			// Japanese mode: prefer name_ja, fallback to name_en, then legacy name
			genreName = scraperutil.CleanString(getPreferredString(category.NameJa, getPreferredString(category.NameEn, category.Name)))
		} else {
			// English mode: prefer name_en, fallback to name_ja, then legacy name
			genreName = scraperutil.CleanString(getPreferredString(category.NameEn, getPreferredString(category.NameJa, category.Name)))
		}
		if genreName != "" {
			result.Genres = append(result.Genres, genreName)
		}
	}

	// Parse cover image - R18.dev provides the large version (pl.jpg)
	var coverImageURL string

	// Try top-level jacket URLs first (newer API format)
	if data.JacketFullURL != "" {
		coverImageURL = strings.TrimSpace(data.JacketFullURL)
	} else if data.Images.JacketImage.Large2 != "" {
		// Fallback to nested structure (older API format)
		coverImageURL = strings.TrimSpace(data.Images.JacketImage.Large2)
	} else if data.Images.JacketImage.Large != "" {
		coverImageURL = strings.TrimSpace(data.Images.JacketImage.Large)
	}

	if coverImageURL != "" {
		result.CoverURL = coverImageURL

		// Try to get a high-quality poster from awsimgsrc
		// If the awsimgsrc poster is too low quality, we'll use the cover for cropping
		posterURL, shouldCrop := imageutil.GetOptimalPosterURL(coverImageURL, s.client.GetClient())
		result.ShouldCropPoster = shouldCrop
		if shouldCrop {
			// Use cover for both, poster will be cropped during organization/display
			result.PosterURL = coverImageURL
		} else {
			// Use the high-quality awsimgsrc poster directly (no cropping needed)
			result.PosterURL = posterURL
		}
	}

	// Parse screenshots - try gallery first (newer API), then Images.SampleImages (older API)
	if len(data.Gallery) > 0 {
		// Extract full-size URLs from gallery
		result.ScreenshotURL = make([]string, 0, len(data.Gallery))
		for _, item := range data.Gallery {
			if item.ImageFull != "" {
				result.ScreenshotURL = append(result.ScreenshotURL, item.ImageFull)
			}
		}
	} else if len(data.Images.SampleImages) > 0 {
		result.ScreenshotURL = data.Images.SampleImages
	}

	// Filter placeholder screenshots using DMM default hashes
	if len(result.ScreenshotURL) > 0 {
		cfg := placeholder.ConfigFromSettings(&s.settings, placeholder.DefaultDMMPlaceholderHashes)
		if cfg.Enabled {
			filtered, count, err := placeholder.FilterURLs(ctx, s.client, result.ScreenshotURL, cfg)
			if err != nil {
				logging.Warnf("r18dev: placeholder filter error: %v", err)
			} else if count > 0 {
				logging.Debugf("r18dev: Filtered %d placeholder screenshots", count)
				result.ScreenshotURL = filtered
			}
		}
	}

	// Parse trailer - try top-level sample_url first (newer API), then nested Sample (older API)
	if data.SampleURL != "" {
		result.TrailerURL = data.SampleURL
	} else if data.Sample.High != "" {
		result.TrailerURL = data.Sample.High
	} else if data.Sample.Low != "" {
		result.TrailerURL = data.Sample.Low
	}

	return result, nil
}

// normalizeIDWithoutStripping normalizes the movie ID without stripping DMM prefix
// Used as first attempt when searching, to avoid incorrectly stripping valid ID parts
func normalizeIDWithoutStripping(id string) string {
	id = strings.ToLower(id)
	id = strings.ReplaceAll(id, "-", "")

	// Remove ALL Unicode whitespace characters to ensure valid API URLs
	id = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1 // Remove the character
		}
		return r
	}, id)

	return id
}

// normalizeID normalizes the movie ID for R18.dev API
func normalizeID(id string) string {
	// R18.dev expects IDs in format like "ipx00535" or "ABP00420"
	// Convert "IPX-535" to "ipx00535" and remove all Unicode whitespace (spaces, tabs, non-breaking spaces, etc.)

	// First, strip DMM content ID prefix if present (e.g., "4sone860" -> "sone860")
	id = stripDMMPrefix(id)

	id = strings.ToLower(id)
	id = strings.ReplaceAll(id, "-", "")

	// Remove ALL Unicode whitespace characters to ensure valid API URLs
	// This handles ASCII spaces, tabs, non-breaking spaces (\u00a0), etc.
	id = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1 // Remove the character
		}
		return r
	}, id)

	return id
}

// stripDMMPrefix removes DMM content ID prefix (leading digits)
// Example: "4sone860" -> "sone860", "118abw001" -> "abw001", "sone-860" -> "sone-860" (unchanged)
func contentIDMatchesExpected(contentID, expectedDVDID string) bool {
	if contentID == "" {
		return false
	}
	stripped := strings.ToLower(stripDMMPrefix(contentID))
	re := regexp.MustCompile(`^(\d*)([a-z]+)(\d+)(.*)$`)
	strippedMatches := re.FindStringSubmatch(stripped)
	expectedMatches := re.FindStringSubmatch(expectedDVDID)
	if len(strippedMatches) < 4 || len(expectedMatches) < 4 {
		return false
	}
	if strippedMatches[2] != expectedMatches[2] {
		return false
	}
	strippedNum, err1 := strconv.Atoi(strippedMatches[3])
	expectedNum, err2 := strconv.Atoi(expectedMatches[3])
	if err1 != nil || err2 != nil {
		return false
	}
	if strippedNum != expectedNum {
		return false
	}
	strippedSuffix := strings.ToLower(strippedMatches[4])
	expectedSuffix := strings.ToLower(expectedMatches[4])
	return strippedSuffix == expectedSuffix
}

func stripDMMPrefix(id string) string {
	// DMM content IDs have leading digits before the series code
	// Use regex to detect and remove them
	re := regexp.MustCompile(`^(\d+)([a-zA-Z].*)$`)
	matches := re.FindStringSubmatch(id)

	if len(matches) == 3 {
		// matches[1] = leading digits (DMM prefix)
		// matches[2] = rest of ID (series + number)
		logging.Debugf("R18: Stripped DMM prefix '%s' from ID '%s' -> '%s'", matches[1], id, matches[2])
		return matches[2]
	}

	// No DMM prefix found, return as-is
	return id
}

// contentIDToID converts content ID to standard ID format
// Example: "118abw00001" -> "ABW-001", "ipx00535" -> "IPX-535"
func contentIDToID(contentID string) string {
	// Remove any leading digits (e.g., "118abw00001" -> "abw00001")
	re := regexp.MustCompile(`^(\d*)([a-z]+)(\d+)(.*)$`)
	matches := re.FindStringSubmatch(strings.ToLower(contentID))

	if len(matches) > 3 {
		prefix := strings.ToUpper(matches[2])
		number := matches[3]
		suffix := ""
		if len(matches) > 4 {
			suffix = strings.ToUpper(matches[4])
		}

		// Remove leading zeros from number, but format to 3 digits
		numberInt, err := strconv.Atoi(number)
		if err == nil {
			number = fmt.Sprintf("%03d", numberInt)
		}

		return prefix + "-" + number + suffix
	}

	return strings.ToUpper(contentID)
}

// getPreferredString returns the first non-empty string from the arguments
func getPreferredString(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}

func selectLocalizedString(language, englishValue, japaneseValue string) string {
	if language == "ja" {
		return getPreferredString(japaneseValue, englishValue)
	}
	return getPreferredString(englishValue, japaneseValue)
}

// R18Response represents the JSON response from R18.dev API (current format)
type R18Response struct {
	DVDID         string `json:"dvd_id"`
	ContentID     string `json:"content_id"`
	TitleJA       string `json:"title_ja"`       // Japanese title
	TitleEn       string `json:"title_en"`       // English title (may be null)
	Description   string `json:"description"`    // Legacy field (not used by current API)
	DescriptionEn string `json:"description_en"` // English description field
	ReleaseDate   string `json:"release_date"`
	Runtime       int    `json:"runtime_mins"` // API uses runtime_mins, not runtime

	// Top-level jacket URLs
	JacketFullURL  string `json:"jacket_full_url"`
	JacketThumbURL string `json:"jacket_thumb_url"`

	// Gallery/screenshots
	Gallery []struct {
		ImageFull  string `json:"image_full"`
		ImageThumb string `json:"image_thumb"`
	} `json:"gallery"`

	// Sample video URL
	SampleURL string `json:"sample_url"`

	// Director - support both flat string and directors array
	Director   string `json:"director"`    // Legacy flat string
	DirectorEn string `json:"director_en"` // Legacy English director field
	Directors  []struct {
		ID         int    `json:"id"`
		NameKana   string `json:"name_kana"`
		NameKanji  string `json:"name_kanji"`
		NameRomaji string `json:"name_romaji"`
	} `json:"directors"` // New directors array format

	// Maker - support both nested and flat structures
	Maker struct {
		Name string `json:"name"`
	} `json:"maker"`
	MakerNameEn string `json:"maker_name_en"` // Flat English field
	MakerNameJa string `json:"maker_name_ja"` // Flat Japanese field

	// Label - support both nested and flat structures
	Label struct {
		Name string `json:"name"`
	} `json:"label"`
	LabelNameEn string `json:"label_name_en"` // Flat English field
	LabelNameJa string `json:"label_name_ja"` // Flat Japanese field

	// Series can be nested object or string
	Series struct {
		Name string `json:"name"`
	} `json:"series"`
	SeriesName   string `json:"series_name"`    // Fallback
	SeriesNameEn string `json:"series_name_en"` // English series field
	SeriesNameJa string `json:"series_name_ja"` // Japanese series field

	// Categories - support both old name field and new name_en/name_ja fields
	Categories []struct {
		ID                         int    `json:"id"`
		Name                       string `json:"name"`    // Legacy field
		NameEn                     string `json:"name_en"` // New English field
		NameJa                     string `json:"name_ja"` // New Japanese field
		NameEnIsMachineTranslation bool   `json:"name_en_is_machine_translation"`
	} `json:"categories"`

	// Actresses with detailed fields
	Actresses []struct {
		ID         int    `json:"id"`
		ImageURL   string `json:"image_url"`
		NameKana   string `json:"name_kana"`
		NameKanji  string `json:"name_kanji"`
		NameRomaji string `json:"name_romaji"`
	} `json:"actresses"`

	// Images are now nested differently
	Images struct {
		JacketImage struct {
			Large  string `json:"large"`
			Large2 string `json:"large2"`
		} `json:"jacket_image"`
		SampleImages []string `json:"sample_images"`
	} `json:"images"`

	// Sample/trailer
	Sample struct {
		High string `json:"high"`
		Low  string `json:"low"`
	} `json:"sample"`
}

// buildTranslations creates translation records for both English and Japanese
// if the API provides data in both languages
func (s *Scraper) buildTranslations(data *R18Response, movieID string) []models.MovieTranslation {
	translations := make([]models.MovieTranslation, 0, 2)

	// Add English translation if English data is available
	if data.TitleEn != "" || data.MakerNameEn != "" || data.LabelNameEn != "" ||
		data.SeriesNameEn != "" || data.DescriptionEn != "" {

		// Build director from English preference
		directorEn := ""
		if len(data.Directors) > 0 {
			directorEn = scraperutil.CleanString(getPreferredString(data.Directors[0].NameRomaji, data.Directors[0].NameKanji))
		} else {
			directorEn = scraperutil.CleanString(getPreferredString(data.DirectorEn, data.Director))
		}

		translations = append(translations, models.MovieTranslation{
			Language:      "en",
			Title:         scraperutil.CleanString(data.TitleEn),
			OriginalTitle: scraperutil.CleanString(data.TitleJA),
			Description:   scraperutil.CleanString(data.DescriptionEn),
			Director:      directorEn,
			Maker:         scraperutil.CleanString(data.MakerNameEn),
			Label:         scraperutil.CleanString(data.LabelNameEn),
			Series:        scraperutil.CleanString(data.SeriesNameEn),
			SourceName:    s.Name(),
		})
	}

	// Add Japanese translation if Japanese data is available
	if data.TitleJA != "" || data.MakerNameJa != "" || data.LabelNameJa != "" ||
		data.SeriesNameJa != "" {

		// Build director from Japanese preference
		directorJa := ""
		if len(data.Directors) > 0 {
			directorJa = scraperutil.CleanString(getPreferredString(data.Directors[0].NameKanji, data.Directors[0].NameRomaji))
		} else {
			directorJa = scraperutil.CleanString(getPreferredString(data.Director, data.DirectorEn))
		}

		translations = append(translations, models.MovieTranslation{
			Language:      "ja",
			Title:         scraperutil.CleanString(data.TitleJA),
			OriginalTitle: scraperutil.CleanString(data.TitleJA),
			Description:   scraperutil.CleanString(data.Description),
			Director:      directorJa,
			Maker:         scraperutil.CleanString(data.MakerNameJa),
			Label:         scraperutil.CleanString(data.LabelNameJa),
			Series:        scraperutil.CleanString(data.SeriesNameJa),
			SourceName:    s.Name(),
		})
	}

	return translations
}
