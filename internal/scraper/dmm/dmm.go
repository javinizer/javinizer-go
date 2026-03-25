package dmm

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper"
)

const (
	baseURL     = "https://www.dmm.co.jp"
	newBaseURL  = "https://video.dmm.co.jp"
	searchURL   = baseURL + "/search/=/searchstr=%s/"
	digitalURL  = baseURL + "/digital/videoa/-/detail/=/cid=%s/"
	physicalURL = baseURL + "/mono/dvd/-/detail/=/cid=%s/"
	// New URL format (video.dmm.co.jp)
	newDigitalURL = newBaseURL + "/av/content/?id=%s"
	// Amateur video URL format (video.dmm.co.jp/amateur/)
	newAmateurURL = newBaseURL + "/amateur/content/?id=%s"
	// Actress link selector used across old/new DMM pages
	actressLinkSelector = `a[href*='?actress='], a[href*='&actress='], a[href*='/article=actress/id=']`
)

// Package-level compiled regexes for performance
var (
	// After prefix cleaning, ID format is: letters + digits + optional suffix
	normalizeIDRegex        = regexp.MustCompile(`^([a-z]+)(\d+)(.*)$`)
	normalizeContentIDRegex = regexp.MustCompile(`^([a-z]+)(\d+)(.*)$`)
	contentIDUnpadRegex     = regexp.MustCompile(`^([a-z]+)0*(\d+.*)$`)
	// Generalized prefix cleaning for DMM content IDs (leading digits OR h_<digits>)
	cleanPrefixRegex = regexp.MustCompile(`^(?:\d+|h_\d+)?([a-z]+\d+.*)$`)
	// Actress parsing helpers
	actressIDRegex        = regexp.MustCompile(`[?&]actress=(\d+)`)
	actressArticleIDRegex = regexp.MustCompile(`/article=actress/id=(\d+)`)
	actressParenRegex     = regexp.MustCompile(`\(.*\)|（.*）`)
	actressJapaneseCharRe = regexp.MustCompile(`\p{Hiragana}|\p{Katakana}|\p{Han}`)
)

// Scraper implements the DMM/Fanza scraper
type Scraper struct {
	client         *resty.Client
	cfg            *config.DMMConfig
	enabled        bool
	scrapeActress  bool
	enableBrowser  bool
	browserTimeout int
	contentIDRepo  *database.ContentIDMappingRepository
	proxyConfig    *config.ProxyConfig // Store proxy config for browser operations
	proxyOverride  *config.ProxyConfig
	downloadProxy  *config.ProxyConfig
}

// New creates a new DMM scraper
func New(cfg *config.Config, contentIDRepo *database.ContentIDMappingRepository) *Scraper {
	// Create HTTP client via per-scraper NewHTTPClient (HTTP-01)
	client, proxyConfig, err := NewHTTPClientWithDefaults(cfg)
	if err != nil {
		// NewHTTPClientWithDefaults already logged and fell back; client is non-nil
		logging.Debugf("DMM: Using fallback HTTP client: %v", err)
	}

	// Log proxy usage if enabled
	if proxyConfig != nil && proxyConfig.Enabled {
		logging.Infof("DMM: Using proxy %s", httpclient.SanitizeProxyURL(proxyConfig.URL))
	}

	return &Scraper{
		client:         client,
		cfg:            &cfg.Scrapers.DMM,
		enabled:        cfg.Scrapers.DMM.Enabled,
		scrapeActress:  cfg.Scrapers.DMM.ScrapeActress,
		enableBrowser:  cfg.Scrapers.DMM.EnableBrowser,
		browserTimeout: cfg.Scrapers.DMM.BrowserTimeout,
		contentIDRepo:  contentIDRepo,
		proxyConfig:    proxyConfig, // Store effective proxy config for browser operations
		proxyOverride:  cfg.Scrapers.DMM.Proxy,
		downloadProxy:  cfg.Scrapers.DMM.DownloadProxy,
	}
}

// Name returns the scraper identifier
func (s *Scraper) Name() string {
	return "dmm"
}

// IsEnabled returns whether the scraper is enabled
func (s *Scraper) IsEnabled() bool {
	return s.enabled
}

// Config returns the scraper's configuration
func (s *Scraper) Config() *config.ScraperConfig {
	return &config.ScraperConfig{
		Enabled:          s.cfg.Enabled,
		UseFakeUserAgent: s.cfg.UseFakeUserAgent,
		UserAgent:        s.cfg.FakeUserAgent,
		Proxy:            s.cfg.Proxy,
		DownloadProxy:    s.cfg.DownloadProxy,
	}
}

// Close cleans up resources held by the scraper
func (s *Scraper) Close() error {
	return nil
}

// ResolveDownloadProxyForHost declares DMM-owned media hosts for downloader proxy routing.
func (s *Scraper) ResolveDownloadProxyForHost(host string) (*config.ProxyConfig, *config.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil, nil, false
	}
	// Exclude LibreDMM hosts to avoid conflicting matches.
	if strings.Contains(host, "libredmm") {
		return nil, nil, false
	}
	if strings.Contains(host, "dmm") {
		return s.downloadProxy, s.proxyOverride, true
	}
	return nil, nil, false
}

// ResolveContentID attempts to resolve the display ID to an actual DMM content ID
// by first checking the cache, then scraping DMM search if needed
func (s *Scraper) ResolveContentID(id string) (string, error) {
	// If no repository available, skip resolution
	if s.contentIDRepo == nil {
		return "", fmt.Errorf("content ID repository not available")
	}

	// 1. Check cache first (fast, no network)
	normalizedID := strings.ToUpper(id)
	if cached, err := s.contentIDRepo.FindBySearchID(normalizedID); err == nil {
		logging.Debugf("DMM: Found cached content-id for %s: %s", id, cached.ContentID)
		return cached.ContentID, nil
	}

	logging.Debugf("DMM: Content-id not cached for %s, attempting to resolve via search", id)

	// 2. Try multiple DMM search query variations.
	// Example fallback: CLT-069 -> clt069, clt00069, clt69.
	contentID := normalizeContentID(id)
	searchQuery := strings.ToLower(strings.ReplaceAll(id, "-", ""))
	cleanSearchID := normalizedContentIDWithoutPadding(contentID)
	matchIDs := uniqueNonEmptyStrings([]string{searchQuery, cleanSearchID, contentID})
	searchQueries := buildResolveContentIDSearchQueries(id, contentID)

	logging.Debugf("DMM: Searching for matches to searchQuery=%s, cleanSearchID=%s or contentID=%s", searchQuery, cleanSearchID, contentID)

	candidates := make([]contentIDCandidate, 0)
	for _, query := range searchQueries {
		searchURLFormatted := fmt.Sprintf(searchURL, query)
		logging.Debugf("DMM: Resolving content-id using search query variation: %s", query)

		// Fetch the search page (cookies are set globally on the client)
		resp, err := s.client.R().Get(searchURLFormatted)
		if err != nil {
			return "", fmt.Errorf("DMM search unavailable (possible geo-restriction or network error): %w", err)
		}

		// Check for explicit geo-blocking or access denial
		if resp.StatusCode() == 403 || resp.StatusCode() == 451 {
			return "", models.NewScraperStatusError(
				"DMM",
				resp.StatusCode(),
				fmt.Sprintf("DMM access blocked (status %d, likely geo-restriction)", resp.StatusCode()),
			)
		}

		if resp.StatusCode() != 200 {
			return "", models.NewScraperStatusError(
				"DMM",
				resp.StatusCode(),
				fmt.Sprintf("DMM search returned status code %d", resp.StatusCode()),
			)
		}

		// Parse search results to extract actual content-id.
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
		if err != nil {
			return "", fmt.Errorf("failed to parse DMM search results: %w", err)
		}

		candidates = append(candidates, extractContentIDCandidates(doc, matchIDs)...)
		if len(candidates) > 0 {
			break
		}
	}

	if len(candidates) == 0 {
		return "", models.NewScraperNotFoundError("DMM", "no matching content-id found in DMM search results")
	}

	// Select best candidate: prefer shorter content IDs (e.g., "abp071" over "abp071dod")
	// Sort by length (ascending) and pick the first one
	foundContentID := candidates[0].contentID
	minLength := candidates[0].length

	for _, c := range candidates[1:] {
		if c.length < minLength {
			minLength = c.length
			foundContentID = c.contentID
		}
	}

	logging.Debugf("DMM: Selected shortest candidate: %s (length: %d) from %d total candidates", foundContentID, minLength, len(candidates))

	// 4. Cache the mapping for future lookups
	mapping := &models.ContentIDMapping{
		SearchID:  normalizedID,
		ContentID: foundContentID,
		Source:    "dmm",
	}

	// Ignore cache write errors - they shouldn't break the flow
	if err := s.contentIDRepo.Create(mapping); err != nil {
		logging.Debugf("DMM: Failed to cache content-id mapping for %s: %v", id, err)
	} else {
		logging.Debugf("DMM: Cached content-id mapping: %s -> %s", normalizedID, foundContentID)
	}

	return foundContentID, nil
}

type contentIDCandidate struct {
	contentID string
	url       string
	length    int
}

func buildResolveContentIDSearchQueries(id string, normalizedContentID string) []string {
	id = strings.ToLower(strings.TrimSpace(id))
	searchID := strings.ReplaceAll(id, "-", "")
	contentID := strings.ToLower(strings.TrimSpace(normalizedContentID))
	cleanContentID := normalizedContentIDWithoutPadding(contentID)

	return uniqueNonEmptyStrings([]string{
		searchID,
		contentID,
		cleanContentID,
		id,
	})
}

func normalizedContentIDWithoutPadding(contentID string) string {
	contentID = strings.ToLower(strings.TrimSpace(contentID))
	if contentID == "" {
		return ""
	}
	return contentIDUnpadRegex.ReplaceAllString(contentID, "$1$2")
}

func uniqueNonEmptyStrings(values []string) []string {
	uniqueValues := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		uniqueValues = append(uniqueValues, value)
	}

	return uniqueValues
}

func extractContentIDCandidates(doc *goquery.Document, searchIDs []string) []contentIDCandidate {
	candidates := make([]contentIDCandidate, 0)
	if doc == nil || len(searchIDs) == 0 {
		return candidates
	}

	doc.Find("a").Each(func(i int, sel *goquery.Selection) {
		href, exists := sel.Attr("href")
		if !exists {
			return
		}

		var urlCID string

		// Check for various DMM link patterns:
		// 1. Physical DVD: /mono/dvd/-/detail/=/cid=XXX
		// 2. Digital video: /digital/videoa/-/detail/=/cid=XXX
		// 3. Monthly subscription: /monthly/standard/-/detail/=/cid=XXX
		// 4. Video streaming: video.dmm.co.jp/av/content/?id=XXX
		if strings.Contains(href, "cid=") {
			// Extract CID from www.dmm.co.jp links
			cidRegex := regexp.MustCompile(`cid=([^/?&]+)`)
			matches := cidRegex.FindStringSubmatch(href)
			if len(matches) > 1 {
				urlCID = matches[1]
			}
		} else if strings.Contains(href, "video.dmm.co.jp") && strings.Contains(href, "id=") {
			// Extract ID from video.dmm.co.jp links
			idRegex := regexp.MustCompile(`id=([^/?&]+)`)
			matches := idRegex.FindStringSubmatch(href)
			if len(matches) > 1 {
				urlCID = matches[1]
			}
		}

		if urlCID == "" {
			return
		}

		// Clean the CID from URL using precompiled regex for consistency and performance
		// Strips DMM prefixes: "9ipx535" -> "ipx535", "h_796san167" -> "san167"
		// Normalize to lowercase and remove hyphens before regex (cleanPrefixRegex expects lowercase)
		normalizedHrefCID := strings.ToLower(strings.ReplaceAll(urlCID, "-", ""))
		cleanURLCID := cleanPrefixRegex.ReplaceAllString(normalizedHrefCID, "$1")
		if !matchesWithVariantSuffix(cleanURLCID, searchIDs...) {
			return
		}

		// Build full URL if it's a relative path
		fullURL := ""
		if strings.HasPrefix(href, "/") {
			fullURL = "https://www.dmm.co.jp" + href
		} else if strings.HasPrefix(href, "http") {
			fullURL = href
		}

		// Store canonical (cleaned) content ID for consistency downstream
		candidates = append(candidates, contentIDCandidate{
			contentID: cleanURLCID,
			url:       fullURL,
			length:    len(cleanURLCID),
		})
		logging.Debugf("DMM: ✓ Found candidate %s (canonical: %s, length: %d), URL: %s", urlCID, cleanURLCID, len(cleanURLCID), fullURL)
	})

	return candidates
}

// GetURL attempts to find the URL for a given movie ID using DMM search
func (s *Scraper) GetURL(id string) (string, error) {
	// Use ResolveContentID which now also captures the URL from search results
	contentID, err := s.ResolveContentID(id)

	if err != nil {
		logging.Debugf("DMM: Content-ID resolution failed for %s: %v", id, err)
		return "", fmt.Errorf("movie not found on DMM: %w", err)
	}

	// Try multiple search queries to find product pages
	// Extract base ID by stripping leading digits from content ID (e.g., "4sone860" -> "sone860")
	baseID := normalizeID(contentID)

	searchQueries := []string{
		strings.ToLower(strings.ReplaceAll(baseID, "-", "")), // Base ID without hyphen (e.g., "sone860")
		strings.ToLower(baseID),                              // Base ID with hyphen if present
		strings.ToLower(strings.ReplaceAll(id, "-", "")),     // Original passed ID without hyphen
		strings.ToLower(id),                                  // Original passed ID as-is
		strings.ToLower(contentID),                           // Content ID (e.g., "4sone860")
	}

	// Remove duplicates
	uniqueQueries := make([]string, 0, len(searchQueries))
	seen := make(map[string]bool)
	for _, q := range searchQueries {
		if !seen[q] && q != "" {
			seen[q] = true
			uniqueQueries = append(uniqueQueries, q)
		}
	}

	// Collect all candidate URLs from all search queries
	allCandidates := make([]urlCandidate, 0)

	for _, searchQuery := range uniqueQueries {
		searchURLFormatted := fmt.Sprintf(searchURL, searchQuery)
		logging.Debugf("DMM: Trying search query: %s", searchQuery)

		resp, err := s.client.R().Get(searchURLFormatted)
		if err != nil || resp.StatusCode() != 200 {
			logging.Debugf("DMM: Search failed for query '%s'", searchQuery)
			continue
		}

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
		if err != nil {
			logging.Debugf("DMM: Failed to parse search results for query '%s'", searchQuery)
			continue
		}

		candidates := s.extractCandidateURLs(doc, contentID)
		allCandidates = append(allCandidates, candidates...)
	}

	if len(allCandidates) == 0 {
		return "", fmt.Errorf("no scrapable URL found for movie on DMM")
	}

	// Sort by priority (highest first), then by content ID length (shortest first)
	sort.Slice(allCandidates, func(i, j int) bool {
		if allCandidates[i].priority != allCandidates[j].priority {
			return allCandidates[i].priority > allCandidates[j].priority
		}
		// If priorities are equal, prefer shorter content IDs (e.g., "abp071" over "abp071dod")
		return allCandidates[i].idLength < allCandidates[j].idLength
	})

	// If the best candidate has low priority (search results), try direct URLs as fallback
	// Direct URLs often work even when not linked from search results
	if allCandidates[0].priority < 2 {
		// Strip leading digits from content ID to get base ID (e.g., "4sone860" -> "sone860")
		baseID := regexp.MustCompile(`^\d+`).ReplaceAllString(contentID, "")
		baseID = strings.ToLower(baseID) // Ensure lowercase

		directURLs := []string{
			fmt.Sprintf(physicalURL, baseID),    // /mono/dvd/ with base ID - priority 3
			fmt.Sprintf(digitalURL, baseID),     // /digital/videoa/ with base ID - priority 2
			fmt.Sprintf(physicalURL, contentID), // /mono/dvd/ with content ID - priority 3
			fmt.Sprintf(digitalURL, contentID),  // /digital/videoa/ with content ID - priority 2
			fmt.Sprintf(newDigitalURL, baseID),  // video.dmm.co.jp/av/ with base ID - priority 1
			fmt.Sprintf(newAmateurURL, baseID),  // video.dmm.co.jp/amateur/ with base ID - priority 1
		}

		logging.Debugf("DMM: Best candidate has low priority (%d), trying direct URLs for %s", allCandidates[0].priority, baseID)

		for _, directURL := range directURLs {
			// Quick GET request to check if URL exists (HEAD doesn't follow redirects reliably)
			resp, err := s.client.R().
				SetDoNotParseResponse(true). // Don't parse body, just check status
				Get(directURL)
			if err == nil && (resp.StatusCode() == 200 || resp.StatusCode() == 302) {
				// Determine priority based on URL pattern
				priority := 0
				if strings.Contains(directURL, "/mono/dvd/") {
					priority = 6 // Physical DVD pages - full metadata
				} else if strings.Contains(directURL, "/digital/videoa/") {
					priority = 5 // Digital video DVD - full metadata
				} else if strings.Contains(directURL, "video.dmm.co.jp/amateur/content/") {
					priority = 4 // Amateur pages
				} else if strings.Contains(directURL, "video.dmm.co.jp/av/content/") {
					priority = 3 // Digital streaming video (av pages)
				}

				extractedID := extractContentIDFromURL(directURL)
				idLen := len(extractedID)
				logging.Debugf("DMM: ✓ Found direct URL (priority %d, ID: %s, len: %d): %s", priority, extractedID, idLen, directURL)
				allCandidates = append(allCandidates, urlCandidate{
					url:       directURL,
					priority:  priority,
					contentID: extractedID,
					idLength:  idLen,
				})

				// Re-sort after adding direct URLs (by priority, then by ID length)
				sort.Slice(allCandidates, func(i, j int) bool {
					if allCandidates[i].priority != allCandidates[j].priority {
						return allCandidates[i].priority > allCandidates[j].priority
					}
					return allCandidates[i].idLength < allCandidates[j].idLength
				})
				break // Found a working direct URL, stop trying
			}
		}
	}

	foundURL := allCandidates[0].url
	logging.Debugf("DMM: Selected URL for %s (priority %d): %s", id, allCandidates[0].priority, foundURL)
	return foundURL, nil
}

// urlCandidate represents a URL with its priority
type urlCandidate struct {
	url       string
	priority  int
	contentID string // extracted content ID from URL for length comparison
	idLength  int    // length of content ID (shorter is better)
}

// extractCandidateURLs extracts and prioritizes URLs from search results
func (s *Scraper) extractCandidateURLs(doc *goquery.Document, contentID string) []urlCandidate {
	var candidates []urlCandidate

	// URL patterns to exclude (unsupported page structures)
	excludePatterns := []string{
		"/rental/",             // Rental pages
		"/search/",             // Search results pages
		"/list/",               // List/search pages
		"/-/search/",           // Mono search pages
		"/-/list/",             // Monthly list pages
		"/service/-/exchange/", // Exchange/redirect pages
	}

	// Only exclude video.dmm.co.jp if browser mode is disabled
	if !s.enableBrowser {
		excludePatterns = append(excludePatterns, "video.dmm.co.jp") // New streaming platform uses JavaScript rendering
		logging.Debug("DMM: Excluding video.dmm.co.jp URLs (browser mode disabled)")
	} else {
		logging.Debug("DMM: Including video.dmm.co.jp URLs (browser mode enabled)")
	}

	// Priority order (higher = better):
	// 6. /mono/dvd/ (physical DVD pages - full metadata including actress data)
	// 5. /digital/videoa/ or /digital/videoc/ (digital video DVD - full metadata)
	// 4. video.dmm.co.jp/amateur/ (amateur pages)
	// 3. video.dmm.co.jp/av/ (digital streaming video pages)
	// 2. /monthly/premium/ (monthly premium - LIMITED metadata, no actress data)
	// 1. /monthly/standard/ (monthly standard - LIMITED metadata, no actress data)

	// Extract canonical base ID by stripping DMM prefixes (leading digits OR h_<digits>)
	// Examples: "4sone860" -> "sone860", "61mdb087" -> "mdb087", "h_1472smkcx003" -> "smkcx003"
	// Keep lowercase for URL matching consistency
	contentIDLower := strings.ToLower(contentID)
	baseID := cleanPrefixRegex.ReplaceAllString(contentIDLower, "$1")
	if baseID == "" {
		baseID = contentIDLower // No prefix, use as-is
	}

	doc.Find("a").Each(func(i int, sel *goquery.Selection) {
		href, exists := sel.Attr("href")
		if !exists {
			return
		}

		// Check if this link contains our canonical content-ID or base ID
		// DMM product pages can use different ID formats (e.g., sone860, 4sone860, tksone860)
		// Use lowercase canonical forms for consistent matching
		hrefLower := strings.ToLower(href)
		containsID := strings.Contains(hrefLower, contentIDLower) || strings.Contains(hrefLower, baseID)
		if !containsID {
			return
		}

		// Build full URL
		var fullURL string
		if strings.HasPrefix(href, "/") {
			fullURL = "https://www.dmm.co.jp" + href
		} else if strings.HasPrefix(href, "http") {
			fullURL = href
		} else {
			return
		}

		// Skip excluded patterns
		excluded := false
		for _, pattern := range excludePatterns {
			if strings.Contains(fullURL, pattern) {
				logging.Debugf("DMM: Skipping excluded URL type: %s", fullURL)
				excluded = true
				break
			}
		}
		if excluded {
			return
		}

		// Assign priority
		priority := 0
		if strings.Contains(fullURL, "/mono/dvd/") {
			priority = 6 // Highest: physical DVD pages with full metadata including actress data
		} else if strings.Contains(fullURL, "/digital/videoa/") || strings.Contains(fullURL, "/digital/videoc/") {
			priority = 5 // Digital video DVD with full metadata
		} else if strings.Contains(fullURL, "video.dmm.co.jp/amateur/") {
			priority = 4 // Amateur pages
		} else if strings.Contains(fullURL, "video.dmm.co.jp") {
			priority = 3 // Digital streaming video (av pages)
		} else if strings.Contains(fullURL, "/monthly/premium/") {
			priority = 2 // Monthly premium - LIMITED metadata, no actress data
		} else if strings.Contains(fullURL, "/monthly/standard/") {
			priority = 1 // Lowest: monthly standard - LIMITED metadata, no actress data
		}

		// Extract content ID from URL for comparison
		extractedID := extractContentIDFromURL(fullURL)
		idLen := len(extractedID)

		candidates = append(candidates, urlCandidate{
			url:       fullURL,
			priority:  priority,
			contentID: extractedID,
			idLength:  idLen,
		})
		logging.Debugf("DMM: Found candidate URL (priority %d, ID: %s, len: %d): %s", priority, extractedID, idLen, fullURL)
	})

	return candidates
}

// Search searches for and scrapes metadata for a given movie ID
func (s *Scraper) Search(id string) (*models.ScraperResult, error) {
	url, err := s.GetURL(id)
	if err != nil {
		return nil, err
	}

	var doc *goquery.Document

	// Check if this is a video.dmm.co.jp URL and browser mode is enabled
	if strings.Contains(url, "video.dmm.co.jp") && s.enableBrowser {
		logging.Debug("DMM: Using browser mode for video.dmm.co.jp page")

		// Use browser to fetch JavaScript-rendered content
		bodyHTML, err := FetchWithBrowser(url, s.browserTimeout, s.proxyConfig)
		if err != nil {
			return nil, fmt.Errorf("browser fetch failed: %w", err)
		}

		// Parse the HTML from browser
		doc, err = goquery.NewDocumentFromReader(strings.NewReader(bodyHTML))
		if err != nil {
			return nil, fmt.Errorf("failed to parse HTML from browser: %w", err)
		}
	} else {
		// Use regular HTTP client (cookies are set globally on the client)
		resp, err := s.client.R().Get(url)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch data from DMM: %w", err)
		}

		if resp.StatusCode() != 200 {
			return nil, models.NewScraperStatusError(
				"DMM",
				resp.StatusCode(),
				fmt.Sprintf("DMM returned status code %d", resp.StatusCode()),
			)
		}

		doc, err = goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
		if err != nil {
			return nil, fmt.Errorf("failed to parse HTML: %w", err)
		}
	}

	return s.parseHTML(doc, url)
}

// parseHTML extracts metadata from DMM HTML
func (s *Scraper) parseHTML(doc *goquery.Document, sourceURL string) (*models.ScraperResult, error) {
	result := &models.ScraperResult{
		Source:    s.Name(),
		SourceURL: sourceURL,
		Language:  "ja", // DMM provides Japanese metadata
	}

	// Extract Content ID from URL
	if cid := extractContentIDFromURL(sourceURL); cid != "" {
		result.ContentID = cid
		result.ID = normalizeID(cid)
	}

	// Detect if this is video.dmm.co.jp (new site format)
	isNewSite := strings.Contains(sourceURL, "video.dmm.co.jp")

	// For new site (video.dmm.co.jp), try to extract metadata from JSON-LD first
	// JSON-LD provides cleaner, more reliable data than HTML scraping
	var jsonldMetadata map[string]interface{}
	if isNewSite {
		jsonldMetadata = extractMetadataFromJSONLD(doc)
	}

	// Extract title - prioritize JSON-LD for new site
	var japaneseTitle string
	if isNewSite {
		if title := getStringFromMetadata(jsonldMetadata, "title"); title != "" {
			japaneseTitle = title
		} else {
			// Fallback to HTML extraction
			japaneseTitle = cleanString(doc.Find("h1").First().Text())
			if japaneseTitle == "" {
				ogTitle, _ := doc.Find(`meta[property="og:title"]`).Attr("content")
				japaneseTitle = cleanString(ogTitle)
			}
		}
	} else {
		// www.dmm.co.jp uses h1#title.item
		japaneseTitle = cleanString(doc.Find("h1#title.item").Text())
	}

	result.Title = japaneseTitle
	result.OriginalTitle = japaneseTitle

	// Extract description - prioritize JSON-LD for new site
	if isNewSite {
		if desc := getStringFromMetadata(jsonldMetadata, "description"); desc != "" {
			result.Description = desc
		} else {
			result.Description = s.extractDescription(doc, isNewSite)
		}
	} else {
		result.Description = s.extractDescription(doc, isNewSite)
	}

	// Extract release date - prioritize JSON-LD for new site
	if isNewSite {
		if date := getTimeFromMetadata(jsonldMetadata, "release_date"); date != nil {
			result.ReleaseDate = date
		} else if date := s.extractReleaseDate(doc); date != nil {
			result.ReleaseDate = date
		}
	} else {
		if date := s.extractReleaseDate(doc); date != nil {
			result.ReleaseDate = date
		}
	}

	// Extract runtime
	result.Runtime = s.extractRuntime(doc)

	// Extract director
	result.Director = s.extractDirector(doc)

	// Extract maker/studio - prioritize JSON-LD for new site
	if isNewSite {
		if maker := getStringFromMetadata(jsonldMetadata, "maker"); maker != "" {
			result.Maker = maker
		} else {
			result.Maker = s.extractMaker(doc, isNewSite)
		}
	} else {
		result.Maker = s.extractMaker(doc, isNewSite)
	}

	// Extract label
	result.Label = s.extractLabel(doc)

	// Extract series
	result.Series = s.extractSeries(doc, isNewSite)

	// Extract rating - prioritize JSON-LD for new site
	if isNewSite {
		ratingValue := getFloat64FromMetadata(jsonldMetadata, "rating_value")
		ratingCount := getIntFromMetadata(jsonldMetadata, "rating_count")
		if ratingValue > 0 || ratingCount > 0 {
			result.Rating = &models.Rating{
				Score: ratingValue,
				Votes: ratingCount,
			}
		} else {
			result.Rating = s.extractRating(doc, isNewSite)
		}
	} else {
		result.Rating = s.extractRating(doc, isNewSite)
	}

	// Extract genres - prioritize JSON-LD for new site
	if isNewSite {
		if genres := getStringSliceFromMetadata(jsonldMetadata, "genres"); len(genres) > 0 {
			result.Genres = genres
		} else {
			result.Genres = s.extractGenres(doc)
		}
	} else {
		result.Genres = s.extractGenres(doc)
	}

	// Extract actresses (only if scrape_actress is enabled AND not a limited metadata page)
	// Pages with limited/incorrect actress data:
	// - Monthly pages (/monthly/standard/, /monthly/premium/) - no actress info in HTML
	isMonthlyPage := strings.Contains(sourceURL, "/monthly/")
	isStreamingPage := strings.Contains(sourceURL, "video.dmm.co.jp")

	if s.scrapeActress && !isMonthlyPage {
		if isStreamingPage {
			// Streaming pages have actress data, but mixed with recommendations
			// Use a more targeted extraction
			result.Actresses = s.extractActressesFromStreamingPage(doc)
			logging.Debugf("DMM: Extracted %d actresses from streaming page", len(result.Actresses))
		} else {
			// Standard pages have clean actress data
			result.Actresses = s.extractActresses(doc)
			logging.Debugf("DMM: Extracted %d actresses", len(result.Actresses))
		}
	} else if isMonthlyPage {
		logging.Debug("DMM: Skipping actress extraction (monthly page - no actress data)")
	} else {
		logging.Debug("DMM: Skipping actress extraction (scrape_actress=false)")
	}

	// Extract cover URL - prioritize JSON-LD for new site
	if isNewSite {
		if coverURL := getStringFromMetadata(jsonldMetadata, "cover_url"); coverURL != "" {
			result.CoverURL = coverURL
		} else {
			result.CoverURL = s.extractCoverURL(doc, isNewSite, result.ContentID)
		}
	} else {
		result.CoverURL = s.extractCoverURL(doc, isNewSite, result.ContentID)
	}

	// Try to get a high-quality poster from awsimgsrc
	// If the awsimgsrc poster is too low quality, we'll use the cover for cropping
	if result.CoverURL != "" {
		posterURL, shouldCrop := imageutil.GetOptimalPosterURL(result.CoverURL, s.client.GetClient())
		result.ShouldCropPoster = shouldCrop
		if shouldCrop {
			// Use cover for both, poster will be cropped during organization/display
			result.PosterURL = result.CoverURL
		} else {
			// Use the high-quality awsimgsrc poster directly (no cropping needed)
			result.PosterURL = posterURL
		}
	}

	// Extract screenshots - prioritize JSON-LD for new site
	if isNewSite {
		if screenshots := getStringSliceFromMetadata(jsonldMetadata, "screenshots"); len(screenshots) > 0 {
			result.ScreenshotURL = screenshots
		} else {
			result.ScreenshotURL = s.extractScreenshots(doc, isNewSite)
		}
	} else {
		result.ScreenshotURL = s.extractScreenshots(doc, isNewSite)
	}

	// Extract trailer URL - prioritize JSON-LD for new site
	if isNewSite {
		if trailerURL := getStringFromMetadata(jsonldMetadata, "trailer_url"); trailerURL != "" {
			result.TrailerURL = trailerURL
		} else {
			result.TrailerURL = s.extractTrailerURL(doc, sourceURL)
		}
	} else {
		result.TrailerURL = s.extractTrailerURL(doc, sourceURL)
	}

	return result, nil
}

// extractDescription extracts the plot description
func (s *Scraper) extractDescription(doc *goquery.Document, isNewSite bool) string {
	if isNewSite {
		return s.extractDescriptionNewSite(doc)
	}

	desc := doc.Find("div.mg-b20.lh4 p.mg-b20").Text()
	if desc == "" {
		desc = doc.Find("div.mg-b20.lh4").Text()
	}
	return cleanString(desc)
}

// extractReleaseDate extracts the release date
func (s *Scraper) extractReleaseDate(doc *goquery.Document) *time.Time {
	dateRegex := regexp.MustCompile(`\d{4}/\d{2}/\d{2}`)
	dateStr := dateRegex.FindString(doc.Text())

	if dateStr != "" {
		t, err := time.Parse("2006/01/02", dateStr)
		if err == nil {
			return &t
		}
	}
	return nil
}

// extractRuntime extracts the runtime in minutes
func (s *Scraper) extractRuntime(doc *goquery.Document) int {
	runtimeRegex := regexp.MustCompile(`(\d{2,3})\s?(?:minutes|分)`)
	matches := runtimeRegex.FindStringSubmatch(doc.Text())

	if len(matches) > 1 {
		runtime, _ := strconv.Atoi(matches[1])
		return runtime
	}
	return 0
}

// extractDirector extracts the director name
func (s *Scraper) extractDirector(doc *goquery.Document) string {
	directorRegex := regexp.MustCompile(`<a.*?href="[^"]*\?director=(\d+)".*?>([^<]+)</a>`)
	html, _ := doc.Html()
	matches := directorRegex.FindStringSubmatch(html)

	if len(matches) > 2 {
		return cleanString(matches[2])
	}
	return ""
}

// extractMaker extracts the studio/maker name
func (s *Scraper) extractMaker(doc *goquery.Document, isNewSite bool) string {
	if isNewSite {
		return s.extractMakerNewSite(doc)
	}

	// Use goquery to find the maker link and extract text (without HTML tags)
	// Supports both ?maker= and /article=maker/id= formats
	var maker string
	doc.Find("a[href*='?maker='], a[href*='/article=maker/id=']").Each(func(i int, sel *goquery.Selection) {
		if maker == "" {
			// Extract text content only (strips HTML tags automatically)
			maker = cleanString(sel.Text())
		}
	})
	return maker
}

// extractLabel extracts the label name
func (s *Scraper) extractLabel(doc *goquery.Document) string {
	// Use goquery to find the label link and extract text (without HTML tags)
	// Supports both ?label= and /article=label/id= formats
	var label string
	doc.Find("a[href*='?label='], a[href*='/article=label/id=']").Each(func(i int, sel *goquery.Selection) {
		if label == "" {
			// Extract text content only (strips HTML tags automatically)
			label = cleanString(sel.Text())
		}
	})
	return label
}

// extractSeries extracts the series name
func (s *Scraper) extractSeries(doc *goquery.Document, isNewSite bool) string {
	if isNewSite {
		return s.extractSeriesNewSite(doc)
	}

	seriesRegex := regexp.MustCompile(`<a href="(?:/digital/videoa/|(?:/en)?/mono/dvd/)-/list/=/article=series/id=\d*/"[^>]*?>(.*)</a></td>`)
	html, _ := doc.Html()
	matches := seriesRegex.FindStringSubmatch(html)

	if len(matches) > 1 {
		return cleanString(matches[1])
	}
	return ""
}

// extractRating extracts the rating information
func (s *Scraper) extractRating(doc *goquery.Document, isNewSite bool) *models.Rating {
	if isNewSite {
		rating, votes := s.extractRatingNewSite(doc)
		if rating > 0 || votes > 0 {
			return &models.Rating{
				Score: rating,
				Votes: votes,
			}
		}
		return nil
	}

	ratingRegex := regexp.MustCompile(`<strong>(.*)\s?(?:points|点)</strong>`)
	html, _ := doc.Html()
	matches := ratingRegex.FindStringSubmatch(html)

	if len(matches) > 1 {
		ratingStr := matches[1]

		// Convert word ratings to numbers
		ratingMap := map[string]float64{
			"One":   1.0,
			"Two":   2.0,
			"Three": 3.0,
			"Four":  4.0,
			"Five":  5.0,
		}

		rating := 0.0
		if val, exists := ratingMap[ratingStr]; exists {
			rating = val
		} else {
			rating, _ = strconv.ParseFloat(ratingStr, 64)
		}

		// Multiply by 2 to conform to 1-10 scale
		rating = rating * 2

		// Extract vote count
		votesRegex := regexp.MustCompile(`<p class="d-review__evaluates">.*?<strong>(\d+)</strong>`)
		votesMatches := votesRegex.FindStringSubmatch(html)
		votes := 0
		if len(votesMatches) > 1 {
			votes, _ = strconv.Atoi(votesMatches[1])
		}

		if rating > 0 || votes > 0 {
			return &models.Rating{
				Score: rating,
				Votes: votes,
			}
		}
	}
	return nil
}

// extractGenres extracts genre tags
func (s *Scraper) extractGenres(doc *goquery.Document) []string {
	genres := make([]string, 0)
	html, _ := doc.Html()

	if strings.Contains(html, "Genre:") || strings.Contains(html, "ジャンル：") {
		genreSection := ""
		parts := strings.Split(html, "Genre:")
		if len(parts) < 2 {
			parts = strings.Split(html, "ジャンル：")
		}

		if len(parts) >= 2 {
			endParts := strings.Split(parts[1], "</tr>")
			if len(endParts) > 0 {
				genreSection = endParts[0]
			}
		}

		// Extract genre names from anchor tags
		genreNameRegex := regexp.MustCompile(`>([^<]+)</a>`)
		matches := genreNameRegex.FindAllStringSubmatch(genreSection, -1)

		for _, match := range matches {
			if len(match) > 1 {
				genre := cleanString(match[1])
				if genre != "" {
					genres = append(genres, genre)
				}
			}
		}
	}

	return genres
}

// extractActresses extracts actress information
func (s *Scraper) extractActresses(doc *goquery.Document) []models.ActressInfo {
	actresses := make([]models.ActressInfo, 0)
	actressIndexByID := make(map[int]int) // Track seen actress IDs to avoid duplicates and merge metadata

	// Find table rows and look for actress-specific rows
	doc.Find("tr").Each(func(i int, row *goquery.Selection) {
		labelCell := row.Find("td").First()
		if labelCell.Length() == 0 {
			return
		}

		labelText := strings.TrimSpace(labelCell.Text())

		// Check if this row is for actresses (supports multiple label variations)
		isActressRow := strings.Contains(labelText, "Actress") ||
			strings.Contains(labelText, "actress") ||
			strings.Contains(labelText, "出演者") ||
			strings.Contains(labelText, "演者")

		if !isActressRow {
			return
		}

		// Get the second td (content cell) which contains actress links
		contentCell := row.Find("td").Eq(1)
		if contentCell.Length() == 0 {
			return
		}

		// Extract all actress links from this specific content cell only
		contentCell.Find(actressLinkSelector).Each(func(j int, sel *goquery.Selection) {
			actress := s.extractActressFromLink(sel)
			if actress.DMMID == 0 {
				return
			}
			// Historical behavior for old DMM table pages treated English names as
			// LastName FirstName (FirstName stored in second token).
			if actress.JapaneseName == "" && actress.FirstName != "" && actress.LastName != "" {
				actress.FirstName, actress.LastName = actress.LastName, actress.FirstName
			}

			if upsertActressInfo(&actresses, actressIndexByID, actress) {
				logging.Debugf("DMM: Actress extracted - Name: %s, ThumbURL: %s, ID: %d", actress.FullName(), actress.ThumbURL, actress.DMMID)
			}
		})
	})

	return actresses
}

// extractActressesFromStreamingPage extracts actresses from video.dmm.co.jp pages
// These pages load content via JavaScript and may include actress links from recommendations.
// This function uses a more targeted approach to extract only the movie's actual cast.
func (s *Scraper) extractActressesFromStreamingPage(doc *goquery.Document) []models.ActressInfo {
	actresses := make([]models.ActressInfo, 0)
	actressIndexByID := make(map[int]int) // Track actress IDs to avoid duplicates and merge metadata

	// Strategy 0: Prefer the dedicated cast block used on video.dmm.co.jp:
	// "この商品に出演しているAV女優" (data-e2eid="actress-information")
	if castSection := doc.Find(`[data-e2eid='actress-information']`).First(); castSection.Length() > 0 {
		castSection.Find(actressLinkSelector).Each(func(i int, sel *goquery.Selection) {
			actress := s.extractActressFromLink(sel)
			if actress.DMMID == 0 {
				return
			}
			if upsertActressInfo(&actresses, actressIndexByID, actress) {
				logging.Debugf("DMM Streaming: Actress extracted from cast section - Name: %s, ID: %d", actress.FullName(), actress.DMMID)
			}
		})

		if len(actresses) > 0 {
			logging.Debugf("DMM Streaming: Found %d actresses in data-e2eid cast section", len(actresses))
			return actresses
		}
	}

	// Strategy 0b: Heading-based fallback for cast block when data-e2eid is absent
	doc.Find("h2").Each(func(i int, heading *goquery.Selection) {
		if len(actresses) > 0 {
			return
		}
		if !strings.Contains(cleanString(heading.Text()), "この商品に出演しているAV女優") {
			return
		}

		container := findNearestActressContainer(heading)
		if container == nil || container.Length() == 0 {
			return
		}

		container.Find(actressLinkSelector).Each(func(j int, sel *goquery.Selection) {
			actress := s.extractActressFromLink(sel)
			if actress.DMMID == 0 {
				return
			}
			if upsertActressInfo(&actresses, actressIndexByID, actress) {
				logging.Debugf("DMM Streaming: Actress extracted from heading-matched cast section - Name: %s, ID: %d", actress.FullName(), actress.DMMID)
			}
		})
	})

	if len(actresses) > 0 {
		logging.Debugf("DMM Streaming: Found %d actresses via heading-matched cast section", len(actresses))
		return actresses
	}

	// Strategy 1: Look for actresses in a metadata/details section
	// video.dmm.co.jp pages typically have actress links in:
	// - A "performer" or "cast" section near the top
	// - Within the main product details area
	// Try to find actress links within specific containers first

	// Try finding actress links within a table or definition list (common for metadata)
	metadataSelectors := []string{
		buildScopedActressSelector("table"),         // Actress links within tables
		buildScopedActressSelector("dl"),            // Actress links within definition lists
		buildScopedActressSelector(".productData"),  // Product data section
		buildScopedActressSelector(".cmn-detail"),   // Common detail section
		buildScopedActressSelector(".product-info"), // Product info section
	}

	for _, selector := range metadataSelectors {
		doc.Find(selector).Each(func(i int, sel *goquery.Selection) {
			actress := s.extractActressFromLink(sel)
			if actress.DMMID > 0 {
				if !upsertActressInfo(&actresses, actressIndexByID, actress) {
					return
				}
				logging.Debugf("DMM Streaming: Actress extracted from metadata - Name: %s, ID: %d", actress.FullName(), actress.DMMID)
			}
		})

		// If we found actresses with this selector, return them
		if len(actresses) > 0 {
			logging.Debugf("DMM Streaming: Found %d actresses using selector: %s", len(actresses), selector)
			return actresses
		}
	}

	// Strategy 2 intentionally does not scrape all actress links on the page.
	// video.dmm.co.jp pages often contain recommendation rails with many actress links,
	// which causes false positives for the current title.
	logging.Debug("DMM Streaming: No reliable cast section found; skipping global actress-link fallback")

	return actresses
}

// extractActressFromLink extracts actress information from a single link element
func (s *Scraper) extractActressFromLink(sel *goquery.Selection) models.ActressInfo {
	href, exists := sel.Attr("href")
	if !exists {
		return models.ActressInfo{}
	}

	// Extract actress ID from URL
	actressID := extractActressID(href)
	if actressID == 0 {
		return models.ActressInfo{}
	}

	actressName := cleanActressName(sel.Text())
	if shouldSkipActressName(actressName) {
		return models.ActressInfo{}
	}

	thumbURL := extractActressThumbURL(sel)

	// Determine if name is Japanese (using Unicode properties for Go 1.25+ compatibility)
	isJapanese := actressJapaneseCharRe.MatchString(actressName)

	actress := models.ActressInfo{
		DMMID:    actressID,
		ThumbURL: thumbURL,
	}

	if isJapanese {
		// Japanese names: only populate japanese_name field
		actress.JapaneseName = actressName
	} else {
		// English names: populate first_name and last_name
		parts := strings.Fields(actressName)
		if len(parts) >= 2 {
			// Given name comes first in English
			actress.FirstName = parts[0]
			actress.LastName = parts[1]
		} else if len(parts) == 1 {
			actress.FirstName = parts[0]
		}
	}

	// Try to construct fallback thumbnail URLs if we have no thumb yet
	if actress.ThumbURL == "" {
		actress.ThumbURL = s.tryActressThumbURLs(actress.FirstName, actress.LastName, actress.DMMID)
	}

	return actress
}

func buildScopedActressSelector(scope string) string {
	return fmt.Sprintf(
		"%s a[href*='?actress='], %s a[href*='&actress='], %s a[href*='/article=actress/id=']",
		scope, scope, scope,
	)
}

// findNearestActressContainer climbs parent nodes to find the nearest container
// with actress links, used for heading-based cast block extraction.
func findNearestActressContainer(sel *goquery.Selection) *goquery.Selection {
	if sel == nil {
		return nil
	}

	container := sel.Parent()
	for depth := 0; depth < 8 && container.Length() > 0; depth++ {
		if container.Find(actressLinkSelector).Length() > 0 {
			return container
		}
		container = container.Parent()
	}

	return nil
}

func extractActressID(href string) int {
	if matches := actressIDRegex.FindStringSubmatch(href); len(matches) > 1 {
		if actressID, err := strconv.Atoi(matches[1]); err == nil {
			return actressID
		}
	}
	if matches := actressArticleIDRegex.FindStringSubmatch(href); len(matches) > 1 {
		if actressID, err := strconv.Atoi(matches[1]); err == nil {
			return actressID
		}
	}
	return 0
}

func cleanActressName(name string) string {
	name = cleanString(name)
	name = actressParenRegex.ReplaceAllString(name, "")
	return strings.TrimSpace(name)
}

func shouldSkipActressName(name string) bool {
	return name == "" ||
		strings.Contains(name, "購入前") ||
		strings.Contains(name, "レビュー") ||
		strings.Contains(name, "ポイント")
}

func extractActressThumbURL(sel *goquery.Selection) string {
	extractFrom := func(root *goquery.Selection) string {
		if root == nil || root.Length() == 0 {
			return ""
		}

		// Prefer img attributes first.
		if img := root.Find("img").First(); img.Length() > 0 {
			for _, attr := range []string{"data-src", "src", "srcset"} {
				if value, exists := img.Attr(attr); exists && value != "" && !strings.HasPrefix(value, "data:image") {
					return value
				}
			}
		}

		// Next.js often stores the real URL in <source srcset>.
		if source := root.Find("source").First(); source.Length() > 0 {
			if value, exists := source.Attr("srcset"); exists && value != "" {
				return value
			}
		}

		return ""
	}

	if thumbURL := normalizeActressThumbURL(extractFrom(sel)); thumbURL != "" {
		return thumbURL
	}

	return normalizeActressThumbURL(extractFrom(sel.Parent()))
}

func normalizeActressThumbURL(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}

	// Handle HTML-escaped query separators and srcset URL lists.
	url = strings.ReplaceAll(url, "&amp;", "&")
	if commaIdx := strings.Index(url, ","); commaIdx != -1 {
		url = strings.TrimSpace(url[:commaIdx])
	}
	if whitespaceIdx := strings.IndexAny(url, " \t\r\n"); whitespaceIdx != -1 {
		url = url[:whitespaceIdx]
	}

	// Normalize protocol-relative paths and DMM image hosts.
	if strings.HasPrefix(url, "//") {
		url = "https:" + url
	}
	if strings.HasPrefix(url, "/") {
		url = "https://video.dmm.co.jp" + url
	}
	url = strings.Replace(url, "awsimgsrc.dmm.co.jp/pics_dig", "pics.dmm.co.jp", 1)

	// Use canonical path without size/query hints.
	if queryIdx := strings.Index(url, "?"); queryIdx != -1 {
		url = url[:queryIdx]
	}

	return strings.TrimSpace(url)
}

// upsertActressInfo appends new actress records and merges duplicate IDs so
// later links can fill missing fields (e.g., thumbnail URL).
func upsertActressInfo(actresses *[]models.ActressInfo, indexByID map[int]int, actress models.ActressInfo) bool {
	if actress.DMMID == 0 {
		return false
	}

	if idx, exists := indexByID[actress.DMMID]; exists {
		existing := &(*actresses)[idx]
		if existing.ThumbURL == "" && actress.ThumbURL != "" {
			existing.ThumbURL = actress.ThumbURL
		}
		if existing.JapaneseName == "" && actress.JapaneseName != "" {
			existing.JapaneseName = actress.JapaneseName
		}
		if existing.FirstName == "" && actress.FirstName != "" {
			existing.FirstName = actress.FirstName
		}
		if existing.LastName == "" && actress.LastName != "" {
			existing.LastName = actress.LastName
		}
		return false
	}

	indexByID[actress.DMMID] = len(*actresses)
	*actresses = append(*actresses, actress)
	return true
}

// tryActressThumbURLs tries to construct and test actress thumbnail URLs
// Strategy:
// 1. If we have English/romaji names, try constructing URL directly
// 2. If we have DMM ID, fetch actress profile page and extract romaji from hiragana
// 3. Test each candidate URL and return the first working one
func (s *Scraper) tryActressThumbURLs(firstName, lastName string, dmmID int) string {
	candidates := make([]string, 0)

	// Strategy 1: Try with provided English/romaji names if available
	if firstName != "" && lastName != "" {
		firstLower := strings.ToLower(firstName)
		lastLower := strings.ToLower(lastName)

		candidates = append(candidates,
			fmt.Sprintf("https://pics.dmm.co.jp/mono/actjpgs/%s_%s.jpg", lastLower, firstLower), // lastname_firstname (most common)
			fmt.Sprintf("https://pics.dmm.co.jp/mono/actjpgs/%s_%s.jpg", firstLower, lastLower), // firstname_lastname
		)
	}

	// Strategy 2: If we have DMM ID, fetch actress page and extract romaji from hiragana
	if dmmID > 0 {
		romajiVariants := s.extractRomajiVariantsFromActressPage(dmmID)
		for _, romaji := range romajiVariants {
			candidates = append(candidates,
				fmt.Sprintf("https://pics.dmm.co.jp/mono/actjpgs/%s.jpg", romaji),
			)
		}
	}

	// Create a client that doesn't follow redirects for URL testing
	// We want to detect 302s and only accept exact 200 responses
	testClient, err := httpclient.NewRestyClient(s.proxyConfig, 5*time.Second, 0)
	if err != nil {
		logging.Debugf("DMM: Failed to create thumbnail probe client with scraper proxy: %v, using explicit no-proxy fallback", err)
		testClient = httpclient.NewRestyClientNoProxy(5*time.Second, 0)
	}
	testClient.SetRedirectPolicy(resty.NoRedirectPolicy())

	// Test each candidate URL
	for _, url := range candidates {
		resp, err := testClient.R().
			SetDoNotParseResponse(true).
			Head(url)

		// Only accept exact 200 OK, not redirects (302) or other status codes
		if err == nil && resp.StatusCode() == 200 {
			logging.Debugf("DMM: Found actress thumbnail via fallback: %s", url)
			return url
		}
	}

	logging.Debugf("DMM: No actress thumbnail found (tried %d candidates)", len(candidates))
	return ""
}

// extractRomajiVariantsFromActressPage fetches the actress profile page and returns multiple romaji variants
// Returns variants with different split points for lastname_firstname format
func (s *Scraper) extractRomajiVariantsFromActressPage(dmmID int) []string {
	url := fmt.Sprintf("https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=%d/", dmmID)

	resp, err := s.client.R().Get(url)
	if err != nil || resp.StatusCode() != 200 {
		logging.Debugf("DMM: Failed to fetch actress page for ID %d", dmmID)
		return nil
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
	if err != nil {
		return nil
	}

	// Extract title: "白上咲花(しらかみえみか) - アダルトDVD..."
	title := doc.Find("title").Text()

	// Extract hiragana reading from parentheses
	re := regexp.MustCompile(`\(([ぁ-ん]+)\)`)
	matches := re.FindStringSubmatch(title)

	if len(matches) < 2 {
		logging.Debugf("DMM: No hiragana reading found in actress page title")
		return nil
	}

	hiragana := matches[1]
	logging.Debugf("DMM: Extracted hiragana reading: %s", hiragana)

	// Convert hiragana to romaji
	romaji := hiraganaToRomaji(hiragana)
	logging.Debugf("DMM: Converted to romaji: %s", romaji)

	// Generate multiple split variants to try
	// Japanese family names are typically 2-6 characters in romaji
	variants := make([]string, 0)

	if len(romaji) >= 4 {
		// Try different split points (most common first)
		// Japanese family names are typically 4-8 chars in romaji
		splitPoints := []int{8, 7, 6, 5, 4, 3, 9, 10, 2} // Order by likelihood (longer names first)
		for _, splitPoint := range splitPoints {
			if splitPoint < len(romaji)-1 {
				lastName := romaji[:splitPoint]
				firstName := romaji[splitPoint:]
				variant := lastName + "_" + firstName
				variants = append(variants, variant)
			}
		}
	}

	// Also add the unsplit version as a fallback
	variants = append(variants, romaji)

	logging.Debugf("DMM: Generated %d romaji variants from hiragana", len(variants))
	return variants
}

// extractCoverURL extracts the cover image URL
func (s *Scraper) extractCoverURL(doc *goquery.Document, isNewSite bool, contentID string) string {
	if isNewSite {
		return s.extractCoverURLNewSite(doc, contentID)
	}

	coverRegex := regexp.MustCompile(`(https://pics\.dmm\.co\.jp/(?:mono/movie/adult|digital/(?:video|amateur))/(.*)/(.*).jpg)`)
	html, _ := doc.Html()
	matches := coverRegex.FindStringSubmatch(html)

	if len(matches) > 1 {
		// Replace 'ps.jpg' with 'pl.jpg' for larger image
		return strings.Replace(matches[1], "ps.jpg", "pl.jpg", 1)
	}
	return ""
}

// extractScreenshots extracts screenshot URLs
func (s *Scraper) extractScreenshots(doc *goquery.Document, isNewSite bool) []string {
	if isNewSite {
		return s.extractScreenshotsNewSite(doc)
	}

	screenshots := make([]string, 0)

	doc.Find("a[name='sample-image']").Each(func(i int, sel *goquery.Selection) {
		if imgSrc, exists := sel.Find("img").Attr("data-lazy"); exists {
			// Add 'jp-' prefix
			imgSrc = strings.Replace(imgSrc, "-", "jp-", 1)
			screenshots = append(screenshots, imgSrc)
		}
	})

	return screenshots
}

// extractTrailerURL extracts the trailer video URL
func (s *Scraper) extractTrailerURL(doc *goquery.Document, sourceURL string) string {
	// Check if this is a new site (video.dmm.co.jp)
	isNewSite := strings.Contains(sourceURL, "video.dmm.co.jp")

	if isNewSite {
		return s.extractTrailerURLNewSite(doc)
	}

	// For www.dmm.co.jp, extract from onclick attribute of sample video button
	// Pattern: onclick="gaEventVideoStart('{&quot;video_url&quot;:&quot;https:\/\/...\/xxx.mp4&quot;'..."
	var trailerURL string

	doc.Find("a.fn-sampleVideoBtn").Each(func(i int, sel *goquery.Selection) {
		if trailerURL != "" {
			return // Already found
		}

		onclick, exists := sel.Attr("onclick")
		if !exists {
			return
		}

		// Extract video_url from the onclick JSON-like data
		// The onclick contains HTML-encoded JSON: &quot; = "
		// Pattern: "video_url":"https:\/\/cc3001.dmm.co.jp\/...\/xxxhhb.mp4"
		if idx := strings.Index(onclick, `video_url`); idx != -1 {
			// Find the URL value after "video_url"
			remaining := onclick[idx:]

			// Look for the URL pattern (after &quot; or ")
			// URLs are escaped with \/ instead of /
			urlStart := -1
			if idx := strings.Index(remaining, `https:`); idx != -1 {
				urlStart = idx
			} else if idx := strings.Index(remaining, `http:`); idx != -1 {
				urlStart = idx
			}

			if urlStart != -1 {
				urlPart := remaining[urlStart:]
				// Find the end of the URL (before &quot; or " or ')
				endMarkers := []string{`\&quot;`, `&quot;`, `"`, `'`}
				urlEnd := len(urlPart)

				for _, marker := range endMarkers {
					if idx := strings.Index(urlPart, marker); idx != -1 && idx < urlEnd {
						urlEnd = idx
					}
				}

				rawURL := urlPart[:urlEnd]
				// Unescape the URL: \/ -> /
				trailerURL = strings.ReplaceAll(rawURL, `\/`, `/`)
				logging.Debugf("DMM: Found trailer URL from onclick: %s", trailerURL)
			}
		}
	})

	return trailerURL
}

// normalizeContentID converts movie ID to DMM content ID format
// Example: "ABP-420" -> "abp00420"
// Amateur IDs like "oreco183", "cap123" are returned as-is (no padding)
//
// Strategy: Use heuristics to detect amateur vs standard IDs, avoiding hardcoded prefix lists.
// Conservative heuristic: Only skip padding if ID has BOTH:
// 1. No hyphen in original (amateur IDs don't use hyphens)
// 2. 4-6 letter prefix (standard studios are usually 2-3 letters like IPX, ABP)
// 3. 3-4 digit number
//
// This ensures standard studio IDs like "ABP420" (3 letters) still get padding → "abp00420"
// while amateur IDs like "oreco183" (5 letters) don't → "oreco183"
// The cache will correct any edge case misidentifications after the first successful search.
func normalizeContentID(id string) string {
	// Convert to lowercase
	idLower := strings.ToLower(id)

	// Check if original ID had a hyphen (standard JAV format)
	hadHyphen := strings.Contains(idLower, "-")

	// Remove hyphens for processing
	idNoHyphen := strings.ReplaceAll(idLower, "-", "")

	// Strip DMM-specific prefixes (leading digits or h_<digits> pattern)
	// Examples: h_1472smkcx003 -> smkcx003, 9ipx535 -> ipx535, h_796san167 -> san167
	// Uses precompiled cleanPrefixRegex for performance
	if cleaned := cleanPrefixRegex.ReplaceAllString(idNoHyphen, "$1"); cleaned != "" {
		idNoHyphen = cleaned
	}

	// Extract components: letters, numbers, optional suffix
	matches := normalizeContentIDRegex.FindStringSubmatch(idNoHyphen)

	if len(matches) > 2 {
		prefix := matches[1]
		number := matches[2]
		suffix := ""
		if len(matches) > 3 {
			suffix = matches[3]
		}

		// Conservative heuristic for amateur detection:
		// - No hyphen in original ID (amateur IDs rarely use hyphens)
		// - 4-6 letter prefix (standard studios are 2-3 letters: IPX, ABP, SSIS)
		// - 3-4 digit number
		// Examples that match: oreco183 (5+3), luxu456 (4+3), maan789 (4+3)
		// Examples that DON'T match: abp420 (3+3), cap123 (3+3) → these get padding
		if !hadHyphen && len(prefix) >= 4 && len(prefix) <= 6 && len(number) >= 3 && len(number) <= 4 {
			// Likely amateur - return as-is without padding
			return prefix + number + suffix
		}

		// Standard JAV ID or ambiguous - apply zero-padding to width 5 (string-based, safe for all lengths)
		// Cache will correct if this was actually an amateur ID
		if len(number) < 5 {
			number = strings.Repeat("0", 5-len(number)) + number
		}
		return prefix + number + suffix
	}

	return idNoHyphen
}

// normalizeID converts content ID back to standard DVD-ID format with hyphen
//
// Examples:
//
//	"ipx00535"   -> "IPX-535"
//	"sone860"    -> "SONE-860"
//	"oreco183"   -> "ORECO-183"
//	"4sone860"   -> "SONE-860"   (leading digits stripped - DMM catalog prefix)
//	"61mdb087"   -> "MDB-087"    (leading digits stripped - DMM channel prefix)
//	"t28123"     -> "T-28123"    (5-digit number preserved)
//	"h_1472smkcx003" -> "SMKCX-003" (h_<digits> prefix stripped)
//
// Strategy:
//  1. Strip h_<digits> prefix if present (DMM content-ID format)
//  2. Split by word-digit boundary (letters vs numbers)
//  3. Strip leading numeric prefixes (DMM uses catalog/channel codes)
//  4. Remove leading zeros from number (e.g., "00535" -> "535")
//  5. Ensure at least 3 digits remain (pad with zeros if needed)
//  6. Always add hyphen between prefix and number for consistency
func normalizeID(contentID string) string {
	idLower := strings.ToLower(contentID)

	// Strip DMM-specific prefixes (leading digits or h_<digits> pattern)
	// Examples: h_1472smkcx003 -> smkcx003, 4sone860 -> sone860, 61mdb087 -> mdb087
	// Uses precompiled cleanPrefixRegex for performance
	if cleaned := cleanPrefixRegex.ReplaceAllString(idLower, "$1"); cleaned != "" {
		idLower = cleaned
	}

	// Match pattern: letter prefix, number, optional suffix
	// Examples: "sone860", "ipx00535", "oreco183"
	matches := normalizeIDRegex.FindStringSubmatch(idLower)

	if len(matches) > 2 {
		prefix := strings.ToUpper(matches[1])
		number := matches[2]
		suffix := ""
		if len(matches) > 3 {
			suffix = strings.ToUpper(matches[3])
		}

		// Remove leading zeros from number, but keep at least 3 digits (string-based, no overflow)
		// Examples: "00535" -> "535", "860" -> "860", "01" -> "001", "00000" -> "000"
		trimmed := strings.TrimLeft(number, "0")
		if trimmed == "" {
			trimmed = "0" // All zeros case
		}
		// Pad to minimum 3 digits
		if len(trimmed) < 3 {
			number = strings.Repeat("0", 3-len(trimmed)) + trimmed
		} else {
			number = trimmed
		}

		// Always add hyphen between prefix and number for consistency
		// This works for all JAV IDs: standard, amateur, and studio series
		return prefix + "-" + number + suffix
	}

	return strings.ToUpper(contentID)
}

// extractContentIDFromURL extracts content ID from DMM URL
// Supports both www.dmm.co.jp (cid=) and video.dmm.co.jp (id=) formats
func extractContentIDFromURL(url string) string {
	// Try cid= format first (www.dmm.co.jp)
	cidRegex := regexp.MustCompile(`cid=([^/?&]+)`)
	matches := cidRegex.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}

	// Try id= format (video.dmm.co.jp)
	idRegex := regexp.MustCompile(`[?&]id=([^/?&]+)`)
	matches = idRegex.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}

	return ""
}

// cleanString removes extra whitespace and newlines
func cleanString(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", " ")
	// Replace multiple spaces with single space
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

// matchesWithVariantSuffix checks if urlCID matches any of the search IDs,
// allowing for single-letter variant suffixes (a, b, c, d, etc.) that DMM uses
// to indicate different versions of the same video.
// Examples: akdl229a matches akdl229, ipx535b matches ipx535
func matchesWithVariantSuffix(urlCID string, searchIDs ...string) bool {
	for _, searchID := range searchIDs {
		// Exact match
		if urlCID == searchID {
			return true
		}

		// Check if urlCID is searchID + single letter suffix
		// Only match single lowercase letters a-z as variant suffixes
		if len(urlCID) == len(searchID)+1 && strings.HasPrefix(urlCID, searchID) {
			suffix := urlCID[len(searchID):]
			if len(suffix) == 1 && suffix[0] >= 'a' && suffix[0] <= 'z' {
				return true
			}
		}
	}
	return false
}

// hiraganaToRomaji converts hiragana to romaji using Nihon-shiki romanization
// DMM uses Nihon-shiki (si, ti, tu) not Hepburn (shi, chi, tsu)
// Example: しらかみえみか -> sirakamiemika
func hiraganaToRomaji(hiragana string) string {
	// Hiragana to romaji mapping (Nihon-shiki)
	mapping := map[string]string{
		"あ": "a", "い": "i", "う": "u", "え": "e", "お": "o",
		"か": "ka", "き": "ki", "く": "ku", "け": "ke", "こ": "ko",
		"が": "ga", "ぎ": "gi", "ぐ": "gu", "げ": "ge", "ご": "go",
		"さ": "sa", "し": "si", "す": "su", "せ": "se", "そ": "so",
		"ざ": "za", "じ": "zi", "ず": "zu", "ぜ": "ze", "ぞ": "zo",
		"た": "ta", "ち": "ti", "つ": "tu", "て": "te", "と": "to",
		"だ": "da", "ぢ": "di", "づ": "du", "で": "de", "ど": "do",
		"な": "na", "に": "ni", "ぬ": "nu", "ね": "ne", "の": "no",
		"は": "ha", "ひ": "hi", "ふ": "hu", "へ": "he", "ほ": "ho",
		"ば": "ba", "び": "bi", "ぶ": "bu", "べ": "be", "ぼ": "bo",
		"ぱ": "pa", "ぴ": "pi", "ぷ": "pu", "ぺ": "pe", "ぽ": "po",
		"ま": "ma", "み": "mi", "む": "mu", "め": "me", "も": "mo",
		"や": "ya", "ゆ": "yu", "よ": "yo",
		"ら": "ra", "り": "ri", "る": "ru", "れ": "re", "ろ": "ro",
		"わ": "wa", "を": "wo", "ん": "n",
		// Small kana
		"ゃ": "ya", "ゅ": "yu", "ょ": "yo",
		"ぁ": "a", "ぃ": "i", "ぅ": "u", "ぇ": "e", "ぉ": "o",
		"っ": "", // Small tsu (gemination marker, handled separately)
	}

	result := ""
	runes := []rune(hiragana)

	for i := 0; i < len(runes); i++ {
		char := string(runes[i])

		// Check for combined characters (きゃ, しゃ, etc.)
		if i+1 < len(runes) {
			next := string(runes[i+1])
			if next == "ゃ" || next == "ゅ" || next == "ょ" {
				// Consonant + small ya/yu/yo
				if romaji, ok := mapping[char]; ok {
					// Remove the vowel and add the y-sound
					if len(romaji) > 0 {
						consonant := romaji[:len(romaji)-1] // Remove last char (vowel)
						result += consonant + mapping[next]
						i++ // Skip next character
						continue
					}
				}
			}

			// Check for small tsu (gemination - doubles next consonant)
			if char == "っ" && i+1 < len(runes) {
				nextChar := string(runes[i+1])
				if romaji, ok := mapping[nextChar]; ok && len(romaji) > 0 {
					// Double the first consonant
					result += string(romaji[0])
					continue
				}
			}
		}

		// Regular character mapping
		if romaji, ok := mapping[char]; ok {
			result += romaji
		} else {
			// Unknown character, keep as-is
			result += char
		}
	}

	return result
}

func init() {
	scraper.RegisterScraper("dmm", func(cfg *config.Config, db *database.DB) (models.Scraper, error) {
		contentIDRepo := database.NewContentIDMappingRepository(db)
		return New(cfg, contentIDRepo), nil
	})
}
