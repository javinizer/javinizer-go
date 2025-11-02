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
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
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
)

// Scraper implements the DMM/Fanza scraper
type Scraper struct {
	client          *resty.Client
	enabled         bool
	scrapeActress   bool
	enableHeadless  bool
	headlessTimeout int
	contentIDRepo   *database.ContentIDMappingRepository
	proxyConfig     *config.ProxyConfig // Store proxy config for headless browser
}

// New creates a new DMM scraper
func New(cfg *config.Config, contentIDRepo *database.ContentIDMappingRepository) *Scraper {
	// Create resty client with proxy support
	client, err := httpclient.NewRestyClient(
		&cfg.Scrapers.Proxy,
		30*time.Second,
		3,
	)
	if err != nil {
		logging.Errorf("DMM: Failed to create HTTP client with proxy: %v, using default", err)
		// Fallback to client without proxy
		client = resty.New()
		client.SetTimeout(30 * time.Second)
		client.SetRetryCount(3)
	}

	// Set user agent
	userAgent := cfg.Scrapers.UserAgent
	if userAgent == "" {
		userAgent = "Javinizer (+https://github.com/javinizer/Javinizer)"
	}
	client.SetHeader("User-Agent", userAgent)

	// Add browser-like headers to help with scraping
	client.SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	client.SetHeader("Accept-Language", "en-US,en;q=0.9,ja;q=0.8")
	client.SetHeader("Accept-Encoding", "gzip, deflate, br")
	client.SetHeader("Connection", "keep-alive")
	client.SetHeader("Upgrade-Insecure-Requests", "1")

	// Set age verification cookies once on the client
	// These will be sent with all requests automatically
	client.SetHeader("Cookie", "age_check_done=1; cklg=ja")

	if cfg.Scrapers.Proxy.Enabled {
		logging.Infof("DMM: Using proxy %s", httpclient.SanitizeProxyURL(cfg.Scrapers.Proxy.URL))
	}

	return &Scraper{
		client:          client,
		enabled:         cfg.Scrapers.DMM.Enabled,
		scrapeActress:   cfg.Scrapers.DMM.ScrapeActress,
		enableHeadless:  cfg.Scrapers.DMM.EnableHeadless,
		headlessTimeout: cfg.Scrapers.DMM.HeadlessTimeout,
		contentIDRepo:   contentIDRepo,
		proxyConfig:     &cfg.Scrapers.Proxy, // Store proxy config for headless browser
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

	// 2. Try to scrape DMM search page
	// Use the original ID (with zeros) for search to get precise results
	// Remove hyphen but keep zeros: "MDB-087" -> "mdb087"
	searchQuery := strings.ToLower(strings.ReplaceAll(id, "-", ""))
	contentID := normalizeContentID(id) // Still needed for comparison
	searchURLFormatted := fmt.Sprintf(searchURL, searchQuery)

	// Fetch the search page (cookies are set globally on the client)
	resp, err := s.client.R().Get(searchURLFormatted)
	if err != nil {
		return "", fmt.Errorf("DMM search unavailable (possible geo-restriction or network error): %w", err)
	}

	// Check for explicit geo-blocking or access denial
	if resp.StatusCode() == 403 || resp.StatusCode() == 451 {
		return "", fmt.Errorf("DMM access blocked (status %d, likely geo-restriction)", resp.StatusCode())
	}

	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("DMM search returned status code %d", resp.StatusCode())
	}

	// 3. Parse search results to extract actual content-id
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
	if err != nil {
		return "", fmt.Errorf("failed to parse DMM search results: %w", err)
	}

	// Extract content-id and URL from various DMM link types
	// Collect ALL candidates to choose the best one (shortest ID preferred)
	type candidate struct {
		contentID string
		url       string
		length    int
	}
	candidates := make([]candidate, 0)
	cleanSearchID := regexp.MustCompile(`^([a-z]+)0*(\d+.*)$`).ReplaceAllString(contentID, "$1$2")

	logging.Debugf("DMM: Searching for matches to searchQuery=%s, cleanSearchID=%s or contentID=%s", searchQuery, cleanSearchID, contentID)

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

		if urlCID != "" {
			// Clean the CID from URL (remove prepended numbers like "9ipx535" -> "ipx535")
			cleanURLCID := regexp.MustCompile(`^\d*([a-z]+\d+.*)$`).ReplaceAllString(urlCID, "$1")

			logging.Debugf("DMM: Found urlCID=%s, cleanURLCID=%s (comparing with searchQuery=%s, cleanSearchID=%s, contentID=%s)",
				urlCID, cleanURLCID, searchQuery, cleanSearchID, contentID)

			// Match against our search ID (with zeros, without zeros, and normalized)
			if cleanURLCID == searchQuery || cleanURLCID == cleanSearchID || cleanURLCID == contentID {
				// Build full URL if it's a relative path
				fullURL := ""
				if strings.HasPrefix(href, "/") {
					fullURL = "https://www.dmm.co.jp" + href
				} else if strings.HasPrefix(href, "http") {
					fullURL = href
				}

				candidates = append(candidates, candidate{
					contentID: urlCID,
					url:       fullURL,
					length:    len(urlCID),
				})
				logging.Debugf("DMM: ✓ Found candidate %s (length: %d), URL: %s", urlCID, len(urlCID), fullURL)
			}
		}
	})

	if len(candidates) == 0 {
		return "", fmt.Errorf("no matching content-id found in DMM search results")
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
					priority = 3
				} else if strings.Contains(directURL, "/digital/videoa/") {
					priority = 2
				} else if strings.Contains(directURL, "video.dmm.co.jp/av/content/") {
					priority = 1 // New digital video format
				} else if strings.Contains(directURL, "video.dmm.co.jp/amateur/content/") {
					priority = 1 // Amateur video format (same priority as new digital)
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
		"/rental/", // Rental pages
	}

	// Only exclude video.dmm.co.jp if headless browser is disabled
	if !s.enableHeadless {
		excludePatterns = append(excludePatterns, "video.dmm.co.jp") // New streaming platform uses JavaScript rendering
		logging.Debug("DMM: Excluding video.dmm.co.jp URLs (headless browser disabled)")
	} else {
		logging.Debug("DMM: Including video.dmm.co.jp URLs (headless browser enabled)")
	}

	// Priority order (higher = better):
	// 1. /monthly/standard/ (monthly standard subscription - most accessible)
	// 2. /monthly/premium/ (monthly premium subscription)
	// 3. /digital/videoa/ or /digital/videoc/ (digital video DVD on www.dmm.co.jp)
	// 4. /mono/dvd/ (physical DVD pages)
	// 5. video.dmm.co.jp (digital streaming video pages)

	// Extract base ID from content ID (e.g., "4sone860" -> "sone860", "61mdb087" -> "mdb087")
	// Strip leading digits to get base ID, keep lowercase for URL matching
	baseID := regexp.MustCompile(`^\d+`).ReplaceAllString(contentID, "")
	if baseID == "" {
		baseID = contentID // No leading digits, use as-is
	}

	doc.Find("a").Each(func(i int, sel *goquery.Selection) {
		href, exists := sel.Attr("href")
		if !exists {
			return
		}

		// Check if this link contains our content-ID or base ID
		// DMM product pages can use different ID formats (e.g., sone860, 4sone860, tksone860)
		containsID := strings.Contains(href, contentID) || strings.Contains(href, baseID)
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
		if strings.Contains(fullURL, "/monthly/standard/") {
			priority = 5 // Highest priority: monthly standard subscription (most accessible)
		} else if strings.Contains(fullURL, "/monthly/premium/") {
			priority = 4 // Monthly premium subscription
		} else if strings.Contains(fullURL, "/mono/dvd/") {
			priority = 3 // Physical DVD pages
		} else if strings.Contains(fullURL, "/digital/videoa/") || strings.Contains(fullURL, "/digital/videoc/") {
			priority = 2 // Digital video DVD
		} else if strings.Contains(fullURL, "video.dmm.co.jp") {
			priority = 1 // Digital streaming video
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

	// Check if this is a video.dmm.co.jp URL and headless is enabled
	if strings.Contains(url, "video.dmm.co.jp") && s.enableHeadless {
		logging.Debug("DMM: Using headless browser for video.dmm.co.jp page")

		// Use headless browser to fetch JavaScript-rendered content
		bodyHTML, err := FetchWithHeadless(url, s.headlessTimeout, s.proxyConfig)
		if err != nil {
			return nil, fmt.Errorf("headless browser failed: %w", err)
		}

		// Parse the HTML from headless browser
		doc, err = goquery.NewDocumentFromReader(strings.NewReader(bodyHTML))
		if err != nil {
			return nil, fmt.Errorf("failed to parse HTML from headless browser: %w", err)
		}
	} else {
		// Use regular HTTP client (cookies are set globally on the client)
		resp, err := s.client.R().Get(url)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch data from DMM: %w", err)
		}

		if resp.StatusCode() != 200 {
			return nil, fmt.Errorf("DMM returned status code %d", resp.StatusCode())
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

	var japaneseTitle string

	// Extract Content ID from URL
	if cid := extractContentIDFromURL(sourceURL); cid != "" {
		result.ContentID = cid
		result.ID = normalizeID(cid)
	}

	// Detect if this is video.dmm.co.jp (new site format)
	isNewSite := strings.Contains(sourceURL, "video.dmm.co.jp")

	// Extract title (different selectors for new site)
	if isNewSite {
		// video.dmm.co.jp uses simple h1 tags or og:title meta tag
		japaneseTitle = cleanString(doc.Find("h1").First().Text())
		if japaneseTitle == "" {
			// Fallback to og:title meta tag
			ogTitle, _ := doc.Find(`meta[property="og:title"]`).Attr("content")
			japaneseTitle = cleanString(ogTitle)
		}
	} else {
		// www.dmm.co.jp uses h1#title.item
		japaneseTitle = cleanString(doc.Find("h1#title.item").Text())
	}

	// For DMM, both Title and OriginalTitle are the Japanese title
	result.Title = japaneseTitle
	result.OriginalTitle = japaneseTitle

	// Extract description
	result.Description = s.extractDescription(doc, isNewSite)

	// Extract release date
	if date := s.extractReleaseDate(doc); date != nil {
		result.ReleaseDate = date
	}

	// Extract runtime
	result.Runtime = s.extractRuntime(doc)

	// Extract director
	result.Director = s.extractDirector(doc)

	// Extract maker/studio
	result.Maker = s.extractMaker(doc, isNewSite)

	// Extract label
	result.Label = s.extractLabel(doc)

	// Extract series
	result.Series = s.extractSeries(doc, isNewSite)

	// Extract rating
	result.Rating = s.extractRating(doc, isNewSite)

	// Extract genres
	result.Genres = s.extractGenres(doc)

	// Extract actresses (only if scrape_actress is enabled)
	if s.scrapeActress {
		result.Actresses = s.extractActresses(doc)
		logging.Debugf("DMM: Extracted %d actresses", len(result.Actresses))
	} else {
		logging.Debug("DMM: Skipping actress extraction (scrape_actress=false)")
	}

	// Extract cover URL
	result.CoverURL = s.extractCoverURL(doc, isNewSite)

	// Poster URL is the same as cover URL (both use large pl.jpg image)
	result.PosterURL = result.CoverURL

	// Extract screenshots
	result.ScreenshotURL = s.extractScreenshots(doc, isNewSite)

	// Extract trailer URL
	result.TrailerURL = s.extractTrailerURL(doc, sourceURL)

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

	// Look for performer block
	actressRegex := regexp.MustCompile(`<a.*?href="[^"]*\?actress=(\d+)".*?>([^<]+)</a>`)
	html, _ := doc.Html()
	matches := actressRegex.FindAllStringSubmatch(html, -1)

	for _, match := range matches {
		if len(match) > 2 {
			// Extract actress ID from URL
			actressID := 0
			if len(match) > 1 {
				actressID, _ = strconv.Atoi(match[1])
			}

			actressName := cleanString(match[2])

			// Remove parenthetical content
			actressName = regexp.MustCompile(`\(.*\)|（.*）`).ReplaceAllString(actressName, "")
			actressName = strings.TrimSpace(actressName)

			// Filter out known non-actress text patterns (DMM UI elements)
			if strings.Contains(actressName, "購入前") ||
				strings.Contains(actressName, "レビュー") ||
				strings.Contains(actressName, "ポイント") ||
				actressName == "" {
				continue
			}

			// Determine if name is Japanese (using Unicode properties for Go 1.25+ compatibility)
			isJapanese := regexp.MustCompile(`\p{Hiragana}|\p{Katakana}|\p{Han}`).MatchString(actressName)

			actress := models.ActressInfo{
				DMMID: actressID,
			}

			if isJapanese {
				actress.JapaneseName = actressName
			} else {
				// Split English name
				parts := strings.Fields(actressName)
				if len(parts) == 1 {
					actress.FirstName = parts[0]
				} else if len(parts) >= 2 {
					actress.LastName = parts[0]
					actress.FirstName = parts[1]
				}
			}

			actresses = append(actresses, actress)
		}
	}

	return actresses
}

// extractCoverURL extracts the cover image URL
func (s *Scraper) extractCoverURL(doc *goquery.Document, isNewSite bool) string {
	if isNewSite {
		return s.extractCoverURLNewSite(doc)
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
	// This would require additional requests to iframe URLs
	// Simplified implementation - returns empty for now
	// Full implementation would need to parse the sample player iframe
	return ""
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

	// Extract components: optional leading digits, letters, numbers, optional suffix
	re := regexp.MustCompile(`^(\d*)([a-z]+)(\d+)(.*)$`)
	matches := re.FindStringSubmatch(idNoHyphen)

	if len(matches) > 3 {
		prefix := matches[2]
		number := matches[3]
		suffix := ""
		if len(matches) > 4 {
			suffix = matches[4]
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

		// Standard JAV ID or ambiguous - apply zero-padding to be safe
		// Cache will correct if this was actually an amateur ID
		paddedNumber := fmt.Sprintf("%05s", number)
		return prefix + paddedNumber + suffix
	}

	return idNoHyphen
}

// normalizeID converts content ID back to standard ID format
// Example: "abp00420" -> "ABP-420"
// Amateur IDs like "oreco183", "cap123" are returned in uppercase without modification
//
// Strategy: Use same conservative heuristic as normalizeContentID to detect amateur IDs.
// Heuristic: If contentID matches 4-6 letters + 3-4 digits pattern, treat as amateur (no hyphen).
// Otherwise, insert hyphen for standard JAV format.
func normalizeID(contentID string) string {
	re := regexp.MustCompile(`^(\d*)([a-z]+)(\d+)(.*)$`)
	matches := re.FindStringSubmatch(strings.ToLower(contentID))

	if len(matches) > 3 {
		prefix := matches[2]
		number := matches[3]
		suffix := ""
		if len(matches) > 4 {
			suffix = strings.ToUpper(matches[4])
		}

		// Conservative heuristic: If prefix is 4-6 chars and number is 3-4 digits, treat as amateur
		// Amateur IDs: oreco183 (5+3), luxu456 (4+3), maan789 (4+3)
		// Standard IDs get hyphen: ipx00535 (3+5) -> IPX-535, cap00123 (3+5) -> CAP-123
		if len(prefix) >= 4 && len(prefix) <= 6 && len(number) >= 3 && len(number) <= 4 {
			// Likely amateur - return in uppercase without hyphen
			return strings.ToUpper(prefix + number + suffix)
		}

		// Standard JAV ID or ambiguous: insert hyphen
		prefix = strings.ToUpper(prefix)
		numberInt, _ := strconv.Atoi(number)
		paddedNumber := fmt.Sprintf("%03d", numberInt)

		return prefix + "-" + paddedNumber + suffix
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
