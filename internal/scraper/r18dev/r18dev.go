package r18dev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-resty/resty/v2"
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
var (
	r18IDRegex            = regexp.MustCompile(`/(id|combined)=([^/?&]+)`)
	dmmPrefixRegex        = regexp.MustCompile(`^(\d+)([a-zA-Z].*)$`)
	contentIDFullRegex    = regexp.MustCompile(`^(\d*)([a-z]+)(\d+)(.*)$`)
	underscorePrefixRegex = regexp.MustCompile(`^([a-z])_(\d+)([a-z]+)(\d+)(.*)$`)
	specialCharsRegex     = regexp.MustCompile(`[^a-z0-9_]`)
)

// scraper implements the R18.dev scraper.
type scraper struct {
	client            *resty.Client
	enabled           bool
	language          string
	maxRetries        int
	respectRetryAfter bool
	proxyOverride     *models.ProxyConfig
	downloadProxy     *models.ProxyConfig
	rateLimiter       *ratelimit.Limiter
	settings          models.ScraperSettings  // stores the full settings for Config() method
	dumpLookup        models.R18DevDumpLookup // optional local dump for exact content_id resolution
}

// New creates a new R18.dev scraper
// newScraper creates a new R18.dev scraper.
func newScraper(settings *models.ScraperSettings, globalProxy *models.ProxyConfig, globalFlareSolverr models.FlareSolverrConfig, dumpLookup models.R18DevDumpLookup) *scraper {
	result := httpclient.InitScraperClient(settings, globalProxy, globalFlareSolverr,
		httpclient.WithScraperHeaders(httpclient.R18DevHeaders()),
		httpclient.WithScraperHeaders(httpclient.RefererHeader("https://r18.dev/")),
		httpclient.WithScraperHeaders(httpclient.UserAgentHeader(settings.UserAgent)),
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

	respectRetryAfter := settings.ShouldRespectRetryAfter(true) // Default: respect Cloudflare Retry-After header on 429 responses

	scraper := &scraper{
		client:            client,
		enabled:           settings.Enabled,
		language:          language,
		rateLimiter:       ratelimit.NewLimiter(time.Duration(settings.RateLimit) * time.Millisecond),
		maxRetries:        maxRetries,
		respectRetryAfter: respectRetryAfter,
		proxyOverride:     settings.Proxy,
		downloadProxy:     settings.DownloadProxy,
		settings:          *settings,
		dumpLookup:        dumpLookup,
	}

	if settings.RateLimit > 0 {
		logging.Infof("R18Dev: Rate limiting enabled with %v delay between requests", time.Duration(settings.RateLimit)*time.Millisecond)
	}

	return scraper
}

// Name returns the scraper identifier
func (s *scraper) Name() string {
	return "r18dev"
}

// IsEnabled returns whether the scraper is enabled
func (s *scraper) IsEnabled() bool {
	return s.enabled
}

// Config returns the scraper's configuration
func (s *scraper) Config() *models.ScraperSettings {
	cloned := s.settings.Clone()
	return &cloned
}

// Close cleans up resources held by the scraper
func (s *scraper) Close() error {
	return nil
}

// CanHandleURL returns true if this scraper can handle the given URL
func (s *scraper) CanHandleURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "r18.dev" || strings.HasSuffix(host, ".r18.dev") ||
		host == "r18.com" || strings.HasSuffix(host, ".r18.com")
}

// ExtractIDFromURL extracts the movie ID from an R18.dev URL
func (s *scraper) ExtractIDFromURL(urlStr string) (string, error) {
	matches := r18IDRegex.FindStringSubmatch(urlStr)
	if len(matches) > 2 {
		return matches[2], nil
	}

	return "", fmt.Errorf("failed to extract ID from R18.dev URL")
}

func (s *scraper) ScrapeURL(ctx context.Context, urlStr string) (*models.ScraperResult, error) {
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

	var data r18Response
	if err := json.Unmarshal(resp.Body(), &data); err != nil {
		bodyPreview := string(resp.Body())
		if len(bodyPreview) > 200 {
			bodyPreview = bodyPreview[:200]
		}
		return nil, fmt.Errorf("failed to parse R18.dev response (preview: %s): %w", bodyPreview, err)
	}

	return s.parseResponse(ctx, &data, urlStr)
}

// ResolveDownloadProxyForHost declares R18.dev-owned media hosts for downloader proxy routing.
func (s *scraper) ResolveDownloadProxyForHost(host string) (*models.ProxyConfig, *models.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil, nil, false
	}
	if host == "r18.dev" || strings.HasSuffix(host, ".r18.dev") {
		return s.downloadProxy, s.proxyOverride, true
	}
	return nil, nil, false
}

func (s *scraper) GetURL(ctx context.Context, id string) (string, error) {
	return s.getURLCtx(ctx, id)
}

func (s *scraper) getURLCtx(ctx context.Context, id string) (string, error) {
	normalized := normalizeID(id)
	return fmt.Sprintf(apiURL, normalized), nil
}

// doRequestWithRetry performs an HTTP request with retry logic for rate limiting
// doRequestWithRetryCtx performs an HTTP request with retry logic for rate limiting and context support
func (s *scraper) doRequestWithRetryCtx(ctx context.Context, url string) (*resty.Response, error) {
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
				// Calculate exponential backoff: 1s, 2s, 4s, 8s...
				backoffTime := time.Duration(1<<uint(attempt)) * time.Second

				var waitTime time.Duration

				// Parse Retry-After header if configured to respect it
				if s.respectRetryAfter && retryAfter != "" {
					if seconds, parseErr := strconv.Atoi(retryAfter); parseErr == nil {
						retryAfterTime := time.Duration(seconds) * time.Second
						// Use the maximum of Retry-After and exponential backoff
						waitTime = max(retryAfterTime, backoffTime)
					}
				}

				// Fall back to exponential backoff if no Retry-After or parse failed
				if waitTime == 0 {
					waitTime = backoffTime
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
			return nil, models.NewScraperStatusError("R18", resp.StatusCode(), fmt.Sprintf("rate limited after %d retries", s.maxRetries))
		}

		// Request successful or non-rate-limit error
		break
	}

	return resp, err
}

// contentIDURLResolver resolves a JAV ID to the R18.dev content-ID URL.
// This seam separates the "resolve URL" decision from the "fetch → parse" execution,
// making the search flow testable without HTTP calls.

// r18ContentIDResolver implements contentIDURLResolver using the R18.dev API.
type r18ContentIDResolver struct {
	scraper *scraper
}

func (r *r18ContentIDResolver) ResolveURL(ctx context.Context, id string) (string, bool) {
	s := r.scraper

	// The local r18.dev dump is consulted once, up front, by Search via
	// searchFromDump (LookupMovie). By the time this resolver runs, the dump
	// has already missed, so there is no point re-querying the same dvd_id_norm
	// index here — fall straight through to HTTP resolution. ResolveURL is only
	// called from Search, so it never runs before searchFromDump.

	// Step 1: Try to lookup content_id using dvd_id with multiple ID variations.
	// Only an EXACT dvd_id match is trusted immediately. When the dvd_id= endpoint
	// returns a movie whose dvd_id field is null (r18.dev fell back to a fuzzy
	// content_id match), record it as a last-resort candidate and defer to
	// resolveByContentIDVariations below.
	//
	// This is necessary because r18.dev sometimes has multiple content_ids that
	// all claim the same dvd_id (a data-quality bug on their side). The dvd_id=
	// endpoint may return any of them. resolveByContentIDVariations tries the
	// known DMM prefixes for the series in canonical order (e.g. "118" before
	// "436" for the "abf" series), so it picks the canonical product rather
	// than a mislabeled one. See ABF-030: dvd_id=abf030 returns the mislabeled
	// 436abf00030 compilation, but the real movie is 118abf030.
	idVariations := []string{
		normalizeIDWithoutStripping(id),
		normalizeID(id),
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

	var fuzzyContentIDURL string
	for _, idVariation := range uniqueVariations {
		if err := ctx.Err(); err != nil {
			logging.Debugf("R18: Context cancelled during dvd_id lookup for %s", id)
			break
		}
		dvdIDURL := fmt.Sprintf("%s/videos/vod/movies/detail/-/dvd_id=%s/json", baseURL, idVariation)
		logging.Debugf("R18: Trying dvd_id lookup: %s (%s)", idVariation, dvdIDURL)

		resp, err := s.doRequestWithRetryCtx(ctx, dvdIDURL)
		if err != nil {
			logging.Debugf("R18: Failed to lookup with %s: %v", idVariation, err)
			continue
		}

		if resp.StatusCode() == 200 {
			contentType := resp.Header().Get("Content-Type")
			if !strings.Contains(contentType, "text/html") {
				var lookupData contentIDLookupResponse
				if err := json.Unmarshal(resp.Body(), &lookupData); err == nil && lookupData.ContentID != "" {
					returnedDVDID := normalizeDVDID(lookupData.DVDID)
					if returnedDVDID == idVariation {
						// Exact dvd_id match — trust it immediately.
						contentID := lookupData.ContentID
						logging.Debugf("R18: ✓ Resolved %s (tried: %s) to content-id: %s", id, idVariation, contentID)
						return fmt.Sprintf("%s/videos/vod/movies/detail/-/combined=%s/json", baseURL, contentID), true
					}
					// Fuzzy match: null dvd_id, but the content_id core-matches the
					// query. Record it as a fallback but keep trying variations —
					// the content-id variation lookup below prefers canonical
					// prefixes and avoids mislabeled duplicate dvd_id entries.
					if returnedDVDID == "" && fuzzyContentIDURL == "" && contentIDCoreMatch(lookupData.ContentID, idVariation) {
						fuzzyContentIDURL = fmt.Sprintf("%s/videos/vod/movies/detail/-/combined=%s/json", baseURL, lookupData.ContentID)
						logging.Debugf("R18: Recorded fuzzy content-id %s for %s (null dvd_id); deferring to variation lookup", lookupData.ContentID, idVariation)
					}
				}
			}
		} else {
			logging.Debugf("R18: Content-ID lookup returned status %d for %s", resp.StatusCode(), idVariation)
		}
	}

	// Step 2: Try content-ID variations. This tries the known DMM prefixes
	// for the series in canonical order, so it resolves to the canonical
	// product even when the dvd_id= endpoint returned a mislabeled duplicate.
	//
	// Known limitation: for multi-prefix series where multiple prefixes produce
	// 200 responses with the same series+number (e.g. "ap" with prefixes ["", "1"]),
	// variationCoreMatches accepts the first 200 that core-matches. The prefix
	// table order determines which prefix wins. This is the same tradeoff as
	// ABF-030 (prefixes ["118", "436"]): the canonical order ensures the correct
	// product is found first. If the table order is wrong for a series, the
	// wrong prefix's 200 could be accepted. The fuzzy content_id from Step 1
	// is NOT tried first because it may itself be a non-canonical prefix
	// (e.g. 436abf00030 for ABF-030) that core-matches but is a different product.
	contentIDURL, err := s.resolveByContentIDVariations(ctx, id)
	if err == nil && contentIDURL != "" {
		logging.Debugf("R18: Resolved via content-id variations: %s", contentIDURL)
		return contentIDURL, true
	}

	// Step 3: Fall back to the fuzzy dvd_id result (null dvd_id) if variation
	// lookup found nothing. This handles series whose real content_id uses an
	// uncommon prefix not in the prefix lookup table — the dvd_id= endpoint
	// surfaced it via its fuzzy match.
	if fuzzyContentIDURL != "" && ctx.Err() == nil {
		logging.Debugf("R18: Falling back to fuzzy dvd_id content-id: %s", fuzzyContentIDURL)
		return fuzzyContentIDURL, true
	}

	return "", false
}

// Search searches for and scrapes metadata for a given movie ID with context support
func (s *scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("R18.dev scraper is disabled")
	}

	// Zero-HTTP fast path: when the local r18.dev dump is present and has this
	// movie, return a complete ScraperResult with no r18.dev API call at all.
	if result, ok := s.searchFromDump(ctx, id); ok {
		return result, nil
	}

	// If the context was cancelled (e.g. user hit Ctrl+C) during the dump
	// lookup, do not fall through to rate-limit-prone HTTP work — surface the
	// cancellation immediately so the caller stops cleanly.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("R18.dev search cancelled before HTTP fallback: %w", err)
	}

	// HTTP fallback: resolve URL → fetch → parse
	resolver := &r18ContentIDResolver{scraper: s}

	var finalURL string
	if resolvedURL, ok := resolver.ResolveURL(ctx, id); ok {
		finalURL = resolvedURL
	} else {
		// Fallback: use normalized ID URL
		var err error
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
	if resp == nil {
		return nil, fmt.Errorf("R18.dev returned nil response for %s", finalURL)
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

	var data r18Response
	if err := json.Unmarshal(resp.Body(), &data); err != nil {
		bodyPreview := string(resp.Body())
		if len(bodyPreview) > 200 {
			bodyPreview = bodyPreview[:200]
		}
		return nil, fmt.Errorf("failed to parse R18.dev response (preview: %s): %w", bodyPreview, err)
	}

	return s.parseResponse(ctx, &data, finalURL)
}

// parseResponse converts R18 API response to ScraperResult.
// Decomposed: orchestrates resolveIDs → resolveLocalizedStrings → resolveActresses →
// resolveGenres → resolveMediaURLs so that each concern is independently testable.
func (s *scraper) parseResponse(ctx context.Context, data *r18Response, sourceURL string) (*models.ScraperResult, error) {
	movieID := resolveIDs(data)

	result := &models.ScraperResult{
		Source:    s.Name(),
		SourceURL: sourceURL,
		Language:  s.language,
		ID:        movieID,
		ContentID: data.ContentID,
		Runtime:   data.Runtime,
	}

	// Build translations for both languages if API provides both English and Japanese data
	result.Translations = s.buildTranslations(data, movieID)

	resolveLocalizedStrings(s.language, data, result)

	// Parse release date (now in YYYY-MM-DD format)
	if data.ReleaseDate != "" {
		t, err := time.Parse("2006-01-02", data.ReleaseDate)
		if err == nil {
			result.ReleaseDate = &t
		}
	}

	resolveActresses(data, result)
	resolveGenres(s.language, data, result)
	resolveMediaURLs(ctx, s, data, result)

	return result, nil
}

// resolveIDs determines the movie ID from DVDID or ContentID.
func resolveIDs(data *r18Response) string {
	movieID := data.DVDID
	if movieID == "" && data.ContentID != "" {
		movieID = contentIDToID(data.ContentID)
	}
	return movieID
}

// resolveLocalizedStrings populates title, description, director, maker, label, and series
// on the result based on the configured language preference.
func resolveLocalizedStrings(language string, data *r18Response, result *models.ScraperResult) {
	result.Title = scraperutil.CleanString(selectLocalizedString(language, data.TitleEn, data.TitleJA))
	result.OriginalTitle = scraperutil.CleanString(data.TitleJA) // Japanese title
	result.Description = scraperutil.CleanString(selectLocalizedString(language, data.DescriptionEn, data.Description))

	// Parse director based on configured language preference.
	// Try directors array first (new API format), then fall back to flat fields
	if len(data.Directors) > 0 {
		if language == "ja" {
			result.Director = scraperutil.CleanString(getPreferredString(data.Directors[0].NameKanji, data.Directors[0].NameRomaji))
		} else {
			result.Director = scraperutil.CleanString(getPreferredString(data.Directors[0].NameRomaji, data.Directors[0].NameKanji))
		}
	} else {
		// Deprecated: Legacy format fallback.
		result.Director = scraperutil.CleanString(selectLocalizedString(language, data.DirectorEn, data.Director))
	}

	// Parse maker/studio based on configured language preference.
	result.Maker = scraperutil.CleanString(selectLocalizedString(language, data.MakerNameEn, data.MakerNameJa))
	if result.Maker == "" {
		// Deprecated: Legacy format fallback.
		result.Maker = scraperutil.CleanString(selectLocalizedString(language, "", data.Maker.Name))
	}

	result.Label = scraperutil.CleanString(selectLocalizedString(language, data.LabelNameEn, data.LabelNameJa))
	if result.Label == "" {
		// Deprecated: Legacy format fallback.
		result.Label = scraperutil.CleanString(selectLocalizedString(language, "", data.Label.Name))
	}

	// Parse series based on configured language preference.
	if language == "ja" {
		result.Series = scraperutil.CleanString(getPreferredString(data.SeriesNameJa, getPreferredString(data.Series.Name, getPreferredString(data.SeriesName, data.SeriesNameEn))))
	} else {
		result.Series = scraperutil.CleanString(getPreferredString(data.SeriesNameEn, getPreferredString(data.SeriesNameJa, getPreferredString(data.Series.Name, data.SeriesName))))
	}
}

// resolveActresses parses actress details from the API response into the result.
func resolveActresses(data *r18Response, result *models.ScraperResult) {
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
				filename = strings.ToLower(parts[0])
			}
			filename = specialCharsRegex.ReplaceAllString(filename, "")
			if filename != "" {
				thumbURL = "https://pics.dmm.co.jp/mono/actjpgs/" + filename + ".jpg"
			}
		}

		// Parse romaji name into first/last names
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
			JapaneseName: scraperutil.CleanString(actress.NameKanji),
			ThumbURL:     thumbURL,
		})
	}
}

// resolveGenres parses genre categories from the API response into the result.
func resolveGenres(language string, data *r18Response, result *models.ScraperResult) {
	result.Genres = make([]string, 0, len(data.Categories))
	for _, category := range data.Categories {
		var genreName string
		if language == "ja" {
			// Deprecated: Legacy format fallback.
			genreName = scraperutil.CleanString(getPreferredString(category.NameJa, getPreferredString(category.NameEn, category.Name)))
		} else {
			// Deprecated: Legacy format fallback.
			genreName = scraperutil.CleanString(getPreferredString(category.NameEn, getPreferredString(category.NameJa, category.Name)))
		}
		if genreName != "" {
			result.Genres = append(result.Genres, genreName)
		}
	}
}

// resolveMediaURLs parses cover, poster, screenshots, and trailer URLs from the API response.
func resolveMediaURLs(ctx context.Context, s *scraper, data *r18Response, result *models.ScraperResult) {
	// Parse cover image - R18.dev provides the large version (pl.jpg)
	var coverImageURL string

	if data.JacketFullURL != "" {
		coverImageURL = strings.TrimSpace(data.JacketFullURL)
	} else if data.Images.JacketImage.Large2 != "" {
		coverImageURL = strings.TrimSpace(data.Images.JacketImage.Large2)
	} else if data.Images.JacketImage.Large != "" {
		coverImageURL = strings.TrimSpace(data.Images.JacketImage.Large)
	}

	if coverImageURL != "" {
		coverImageURL = imageutil.NormalizeDMMScreenshotURL(coverImageURL)
		coverImageURL = imageutil.UpgradeCoverResolution(coverImageURL)
		// Keep coverImageURL on pics.dmm.co.jp for now — DiscoverScreenshots
		// (below) requires it. UpgradeDMMCoverCDN is applied after discovery.
		result.CoverURL = coverImageURL

		posterURL, shouldCrop := imageutil.GetOptimalPosterURL(coverImageURL, s.client.GetClient())
		result.ShouldCropPoster = shouldCrop
		if shouldCrop {
			result.PosterURL = coverImageURL
		} else {
			result.PosterURL = posterURL
		}

		if result.PosterURL == coverImageURL && data.ContentID != "" {
			if awsURL := s.resolveAwsimgsrcPoster(ctx, data.ContentID, s.client.GetClient()); awsURL != "" {
				result.PosterURL = awsURL
				result.ShouldCropPoster = false
			}
		}
	}

	// Parse screenshots - try gallery first (newer API), then Images.SampleImages (older API)
	if len(data.Gallery) > 0 {
		result.ScreenshotURL = make([]string, 0, len(data.Gallery))
		for _, item := range data.Gallery {
			if item.ImageFull != "" {
				result.ScreenshotURL = append(result.ScreenshotURL, imageutil.NormalizeDMMScreenshotURL(item.ImageFull))
			}
		}
	} else if len(data.Images.SampleImages) > 0 {
		result.ScreenshotURL = make([]string, 0, len(data.Images.SampleImages))
		for _, url := range data.Images.SampleImages {
			result.ScreenshotURL = append(result.ScreenshotURL, imageutil.NormalizeDMMScreenshotURL(url))
		}
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

	// Fallback: discover screenshots by probing pics.dmm.co.jp when the API returns none
	// or when the placeholder filter removes all screenshots
	if len(result.ScreenshotURL) == 0 && result.CoverURL != "" {
		if discovered := imageutil.DiscoverScreenshots(result.CoverURL, s.client.GetClient()); len(discovered) > 0 {
			logging.Debugf("r18dev: Discovered %d screenshots via cover URL probing for %s", len(discovered), result.ID)
			result.ScreenshotURL = discovered
		}
	}

	// Upgrade cover (and any cover-derived poster) to the high-res awsimgsrc
	// CDN. Applied after DiscoverScreenshots, which requires the pics.dmm.co.jp
	// host. If the poster was set to the cover for cropping (shouldCrop=true
	// and resolveAwsimgsrcPoster found nothing), upgrade it too so the crop uses
	// the high-res source.
	if result.CoverURL != "" {
		upgradedCover := imageutil.UpgradeDMMCoverCDN(result.CoverURL)
		if result.PosterURL == result.CoverURL {
			result.PosterURL = upgradedCover
		}
		result.CoverURL = upgradedCover
	}

	// Parse trailer - try top-level sample_url first (newer API), then nested Sample (older API)
	if data.SampleURL != "" {
		result.TrailerURL = data.SampleURL
	} else if data.Sample.High != "" {
		result.TrailerURL = data.Sample.High
	} else if data.Sample.Low != "" {
		result.TrailerURL = data.Sample.Low
	}
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

func contentIDCoreMatch(contentID, expectedDVDID string) bool {
	if contentID == "" {
		return false
	}
	stripped := strings.ToLower(stripDMMPrefix(contentID))

	var cSeries, cNumStr string
	if um := underscorePrefixRegex.FindStringSubmatch(stripped); len(um) == 6 {
		cSeries = um[3]
		cNumStr = um[4]
	} else if sm := contentIDFullRegex.FindStringSubmatch(stripped); len(sm) >= 4 {
		cSeries = sm[2]
		cNumStr = sm[3]
	} else {
		return false
	}

	em := contentIDFullRegex.FindStringSubmatch(expectedDVDID)
	if len(em) < 4 {
		return false
	}
	if cSeries != em[2] {
		return false
	}
	cNum, err1 := strconv.Atoi(cNumStr)
	eNum, err2 := strconv.Atoi(em[3])
	if err1 != nil || err2 != nil {
		return cNumStr == em[3]
	}
	return cNum == eNum
}

// contentIDLookupResponse is the minimal response shape used for dvd_id and
// combined= endpoint lookups during content-id resolution.
type contentIDLookupResponse struct {
	ContentID string `json:"content_id"`
	DVDID     string `json:"dvd_id"`
}

// normalizeDVDID lowercases a dvd_id and removes hyphens, matching the
// normalized form used for comparison against normalized query variations.
func normalizeDVDID(dvdID string) string {
	return strings.ToLower(strings.ReplaceAll(dvdID, "-", ""))
}

// variationCoreMatches parses a combined= response body and reports whether the
// returned movie core-matches the requested id (same series + number). It
// accepts on either an exact normalized dvd_id match or a content_id
// core-match, so a 200 response for a different movie (e.g. a mislabeled
// duplicate under the same prefix slot) is rejected and the next variation is
// tried. A body that fails to parse or carries neither field is treated as a
// non-match so the caller falls through to the fuzzy fallback.
func variationCoreMatches(body []byte, normalizedQuery string) bool {
	var data contentIDLookupResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return false
	}
	if data.DVDID != "" {
		if normalizeDVDID(data.DVDID) == normalizedQuery {
			return true
		}
	}
	return contentIDCoreMatch(data.ContentID, normalizedQuery)
}

// stripDMMPrefix removes DMM content ID prefix (leading digits)
// Example: "4sone860" -> "sone860", "118abw001" -> "abw001", "sone-860" -> "sone-860" (unchanged)
func stripDMMPrefix(id string) string {
	matches := dmmPrefixRegex.FindStringSubmatch(id)

	if len(matches) == 3 {
		// matches[1] = leading digits (DMM prefix)
		// matches[2] = rest of ID (series + number)
		logging.Debugf("R18: Stripped DMM prefix '%s' from ID '%s' -> '%s'", matches[1], id, matches[2])
		return matches[2]
	}

	// No DMM prefix found, return as-is
	return id
}

// resolveByContentIDVariations tries multiple content-id format variations when dvd_id lookup fails.
// Some titles (digital-only releases) have null dvd_id, so the dvd_id endpoint returns 404.
// We construct content_id variations (with DMM prefix, zero-padded) and try the combined endpoint.
//
// Each 200 response is validated: the returned content_id or dvd_id must core-match the
// requested id (same series + number) before the variation is accepted. This guards against
// r18.dev returning a 200 for a different movie that happens to share a prefix slot, so the
// result does not depend solely on global prefix-table ordering.
func (s *scraper) resolveByContentIDVariations(ctx context.Context, id string) (string, error) {
	variations := generateContentIDVariations(id)
	if len(variations) == 0 {
		return "", nil
	}

	normalizedDVDID := normalizeIDWithoutStripping(id)

	logging.Debugf("R18: dvd_id lookup failed, trying %d content-id variation(s) for %s", len(variations), id)

	notFound, failed, invalid := 0, 0, 0
	for _, variation := range variations {
		if err := ctx.Err(); err != nil {
			logging.Debugf("R18: Context cancelled during content-id variation lookup for %s", id)
			break
		}
		u := fmt.Sprintf("%s/videos/vod/movies/detail/-/combined=%s/json", baseURL, variation)
		logging.Debugf("R18: Trying content-id variation: %s (%s)", variation, u)

		resp, err := s.doRequestWithRetryCtx(ctx, u)
		if err != nil {
			failed++
			logging.Debugf("R18: Failed content-id variation %s: %v", variation, err)
			continue
		}

		if resp.StatusCode() == 200 {
			contentType := resp.Header().Get("Content-Type")
			if !strings.Contains(contentType, "text/html") {
				if variationCoreMatches(resp.Body(), normalizedDVDID) {
					logging.Debugf("R18: ✓ Content-id variation %s resolved for %s", variation, id)
					return u, nil
				}
				logging.Debugf("R18: Content-id variation %s returned 200 but did not core-match %s; skipping", variation, normalizedDVDID)
			} else {
				logging.Debugf("R18: Content-id variation %s returned HTML 200; skipping", variation)
			}
			invalid++
			continue
		}
		if resp.StatusCode() == 404 {
			notFound++
		} else {
			failed++
		}
		logging.Debugf("R18: Content-id variation %s returned status %d", variation, resp.StatusCode())
	}

	logging.Debugf("R18: No content-id variation matched for %s (%d not-found, %d invalid response, %d request/status failures)",
		id, notFound, invalid, failed)

	return "", nil
}

// resolveAwsimgsrcPoster tries multiple awsimgsrc poster URL variations when the
// standard construction fails. The pics.dmm.co.jp URL path and content_id format
// don't always match awsimgsrc, so we use the prefix lookup to try variations.
// Returns the first valid awsimgsrc ps.jpg URL that meets quality requirements.
func (s *scraper) resolveAwsimgsrcPoster(ctx context.Context, contentID string, client *http.Client) string {
	series, numStr := splitSeriesAndNumber(contentIDToID(contentID))
	if series == "" || numStr == "" {
		return ""
	}

	series = strings.ToLower(series)
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return ""
	}

	padded3 := fmt.Sprintf("%03d", num)

	// Look up known prefixes for this series
	var prefixes []string
	if lookup, ok := contentIDPrefixLookup[series]; ok {
		prefixes = lookup
	} else {
		prefixes = []string{"", "1"}
	}

	// Try each prefix + 3-digit padded number on awsimgsrc
	for _, prefix := range prefixes {
		id := prefix + series + padded3
		u := fmt.Sprintf("https://awsimgsrc.dmm.com/dig/mono/movie/%s/%sps.jpg", id, id)

		width, height, imgErr := imageutil.GetImageDimensions(u, client)
		if imgErr != nil {
			continue
		}

		if width >= imageutil.MinPosterWidth && height >= imageutil.MinPosterHeight {
			logging.Debugf("R18: Resolved awsimgsrc poster for %s: %s (%dx%d)", contentID, u, width, height)
			return u
		}
	}

	return ""
}

// generateContentIDVariations constructs possible content_id formats from a dvd_id.
// For "START-575", generates: ["1start00575", "1start575"]
// For "ABF-346", generates: ["118abf00346", "118abf346", "436abf00346", "436abf346"]
// The r18.dev content_id format is: [DMM-prefix][series][zero-padded-number]
// Uses the contentIDPrefixLookup table built from r18.dev database dumps to find
// known prefixes per series. Falls back to common prefixes if the series is unknown.
func generateContentIDVariations(id string) []string {
	series, numStr := splitSeriesAndNumber(id)
	if series == "" || numStr == "" {
		return nil
	}

	series = strings.ToLower(series)
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return nil
	}

	padded3 := fmt.Sprintf("%03d", num)
	padded5 := fmt.Sprintf("%05d", num)

	// Look up known prefixes for this series from the r18.dev database dump
	var prefixes []string
	if lookup, ok := contentIDPrefixLookup[series]; ok {
		prefixes = lookup
	} else {
		// Fallback: try common prefixes for unknown series
		prefixes = []string{"", "1"}
	}

	var variations []string
	seen := make(map[string]bool)

	add := func(v string) {
		if !seen[v] {
			seen[v] = true
			variations = append(variations, v)
		}
	}

	for _, prefix := range prefixes {
		// 5-digit padded (standard DMM content_id format)
		add(prefix + series + padded5)
		// 3-digit padded (used by many r18.dev content_ids)
		add(prefix + series + padded3)
	}

	return variations
}

// splitSeriesAndNumber splits a dvd_id like "START-575" into ("START", "575")
func splitSeriesAndNumber(id string) (string, string) {
	// Try standard format: SERIES-NUMBER
	if parts := strings.SplitN(id, "-", 2); len(parts) == 2 {
		if isAlpha(parts[0]) && isDigit(parts[1]) {
			return parts[0], parts[1]
		}
	}

	// Try already-normalized format: series575 (from normalizeID)
	lowered := strings.ToLower(id)
	if m := contentIDFullRegex.FindStringSubmatch(lowered); len(m) >= 4 {
		return m[2], m[3]
	}

	return "", ""
}

func isAlpha(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return len(s) > 0
}

func isDigit(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// contentIDToID converts content ID to standard ID format
// Example: "118abw00001" -> "ABW-001", "ipx00535" -> "IPX-535", "h_086mesu00103" -> "MESU-103"
func contentIDToID(contentID string) string {
	lowered := strings.ToLower(contentID)

	if underscoreMatches := underscorePrefixRegex.FindStringSubmatch(lowered); len(underscoreMatches) == 6 {
		prefix := strings.ToUpper(underscoreMatches[3])
		number := underscoreMatches[4]
		suffix := strings.ToUpper(underscoreMatches[5])

		numberInt, err := strconv.Atoi(number)
		if err == nil {
			number = fmt.Sprintf("%03d", numberInt)
		}

		return prefix + "-" + number + suffix
	}

	matches := contentIDFullRegex.FindStringSubmatch(lowered)

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

// r18Response represents the JSON response from R18.dev API (current format)
type r18Response struct {
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
func (s *scraper) buildTranslations(data *r18Response, movieID string) []models.MovieTranslation {
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
