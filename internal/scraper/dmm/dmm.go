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
)

// Scraper implements the DMM/Fanza scraper
type Scraper struct {
	client          *resty.Client
	enabled         bool
	scrapeActress   bool
	enableHeadless  bool
	headlessTimeout int
	contentIDRepo   *database.ContentIDMappingRepository
}

// New creates a new DMM scraper
func New(cfg *config.Config, contentIDRepo *database.ContentIDMappingRepository) *Scraper {
	client := resty.New()
	client.SetTimeout(30 * time.Second)
	client.SetRetryCount(3)

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

	return &Scraper{
		client:          client,
		enabled:         cfg.Scrapers.DMM.Enabled,
		scrapeActress:   cfg.Scrapers.DMM.ScrapeActress,
		enableHeadless:  cfg.Scrapers.DMM.EnableHeadless,
		headlessTimeout: cfg.Scrapers.DMM.HeadlessTimeout,
		contentIDRepo:   contentIDRepo,
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
	var foundContentID string
	var foundURL string
	cleanSearchID := regexp.MustCompile(`^([a-z]+)0*(\d+.*)$`).ReplaceAllString(contentID, "$1$2")

	logging.Debugf("DMM: Searching for matches to searchQuery=%s, cleanSearchID=%s or contentID=%s", searchQuery, cleanSearchID, contentID)

	doc.Find("a").Each(func(i int, sel *goquery.Selection) {
		href, exists := sel.Attr("href")
		if !exists || foundContentID != "" {
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
				foundContentID = urlCID
				// Build full URL if it's a relative path
				if strings.HasPrefix(href, "/") {
					foundURL = "https://www.dmm.co.jp" + href
				} else if strings.HasPrefix(href, "http") {
					foundURL = href
				}
				logging.Debugf("DMM: ✓ Resolved %s to content-id: %s, URL: %s", id, urlCID, foundURL)
			}
		}
	})

	if foundContentID == "" {
		return "", fmt.Errorf("no matching content-id found in DMM search results")
	}

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

	// The URL is embedded in the search results - extract it from the href we found
	// We need to search again to get the URL (TODO: refactor to return both from ResolveContentID)
	searchQuery := strings.ToLower(strings.ReplaceAll(id, "-", ""))
	searchURLFormatted := fmt.Sprintf(searchURL, searchQuery)

	resp, err := s.client.R().Get(searchURLFormatted)
	if err != nil || resp.StatusCode() != 200 {
		return "", fmt.Errorf("DMM search unavailable")
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
	if err != nil {
		return "", fmt.Errorf("failed to parse DMM search results")
	}

	// Collect all URLs that match our content-ID and prioritize them
	type urlCandidate struct {
		url      string
		priority int
	}

	var candidates []urlCandidate

	// URL patterns to exclude (unsupported page structures)
	excludePatterns := []string{
		"/monthly/", // Monthly subscription pages have different structure
		"/rental/",  // Rental pages
	}

	// Only exclude video.dmm.co.jp if headless browser is disabled
	if !s.enableHeadless {
		excludePatterns = append(excludePatterns, "video.dmm.co.jp") // New streaming platform uses JavaScript rendering
		logging.Debug("DMM: Excluding video.dmm.co.jp URLs (headless browser disabled)")
	} else {
		logging.Debug("DMM: Including video.dmm.co.jp URLs (headless browser enabled)")
	}

	// Priority order (higher = better):
	// 1. /digital/videoa/ or /digital/videoc/ (digital video DVD on www.dmm.co.jp)
	// 2. /mono/dvd/ (physical DVD pages)
	// 3. video.dmm.co.jp (digital streaming video pages)

	doc.Find("a").Each(func(i int, sel *goquery.Selection) {
		href, exists := sel.Attr("href")
		if !exists {
			return
		}

		// Check if this link contains our content-ID
		if !strings.Contains(href, contentID) {
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
		if strings.Contains(fullURL, "/digital/videoa/") || strings.Contains(fullURL, "/digital/videoc/") {
			priority = 3 // Highest priority: digital video DVD
		} else if strings.Contains(fullURL, "/mono/dvd/") {
			priority = 2 // Second: physical DVD
		} else if strings.Contains(fullURL, "video.dmm.co.jp") {
			priority = 1 // Third: digital streaming video
		}

		candidates = append(candidates, urlCandidate{url: fullURL, priority: priority})
		logging.Debugf("DMM: Found candidate URL (priority %d): %s", priority, fullURL)
	})

	if len(candidates) == 0 {
		return "", fmt.Errorf("no scrapable URL found for movie on DMM")
	}

	// Sort by priority (highest first)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].priority > candidates[j].priority
	})

	foundURL := candidates[0].url
	logging.Debugf("DMM: Selected URL for %s (priority %d): %s", id, candidates[0].priority, foundURL)
	return foundURL, nil
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
		bodyHTML, err := FetchWithHeadless(url, s.headlessTimeout)
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

	// Extract actresses
	result.Actresses = s.extractActresses(doc)

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

	// Updated pattern to match PowerShell: supports both ?maker= and /article=maker/id= formats
	makerRegex := regexp.MustCompile(`<a[^>]*href="[^"]*(?:\?maker=|/article=maker/id=)(\d+)[^"]*"[^>]*>([\s\S]*?)</a>`)
	html, _ := doc.Html()
	matches := makerRegex.FindStringSubmatch(html)

	if len(matches) > 2 {
		return cleanString(matches[2])
	}
	return ""
}

// extractLabel extracts the label name
func (s *Scraper) extractLabel(doc *goquery.Document) string {
	// Updated pattern to match PowerShell: supports both ?label= and /article=label/id= formats
	labelRegex := regexp.MustCompile(`<a[^>]*href="[^"]*(?:\?label=|/article=label/id=)(\d+)[^"]*"[^>]*>([\s\S]*?)</a>`)
	html, _ := doc.Html()
	matches := labelRegex.FindStringSubmatch(html)

	if len(matches) > 2 {
		return cleanString(matches[2])
	}
	return ""
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
func normalizeContentID(id string) string {
	// Convert to lowercase
	id = strings.ToLower(id)

	// Remove hyphens
	id = strings.ReplaceAll(id, "-", "")

	// Extract prefix and number
	re := regexp.MustCompile(`^(\d*)([a-z]+)(\d+)(.*)$`)
	matches := re.FindStringSubmatch(id)

	if len(matches) > 3 {
		prefix := matches[2]
		number := matches[3]
		suffix := ""
		if len(matches) > 4 {
			suffix = matches[4]
		}

		// Pad number with zeros (DMM uses 5-digit padding)
		paddedNumber := fmt.Sprintf("%05s", number)

		return prefix + paddedNumber + suffix
	}

	return id
}

// normalizeID converts content ID back to standard ID format
// Example: "abp00420" -> "ABP-420"
func normalizeID(contentID string) string {
	re := regexp.MustCompile(`^(\d*)([a-z]+)(\d+)(.*)$`)
	matches := re.FindStringSubmatch(contentID)

	if len(matches) > 3 {
		prefix := strings.ToUpper(matches[2])
		number := matches[3]
		suffix := ""
		if len(matches) > 4 {
			suffix = strings.ToUpper(matches[4])
		}

		// Remove leading zeros
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
