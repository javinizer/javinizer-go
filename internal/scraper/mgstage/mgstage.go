package mgstage

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

const (
	baseURL    = "https://www.mgstage.com"
	searchURL  = baseURL + "/search/cSearch.php?search_word=%s&type=top&page=1&list_cnt=120"
	productURL = baseURL + "/product/product_detail/%s/"
)

// Scraper implements the MGStage scraper
type Scraper struct {
	client          *resty.Client
	enabled         bool
	usingProxy      bool
	requestDelay    time.Duration
	lastRequestTime atomic.Value // stores time.Time of last request for rate limiting
}

var (
	mgstageURLIDRe       = regexp.MustCompile(`(?i)mgstage\.com/product/product_detail/([^/?#]+)/?`)
	mgstagePrefixedIDRe  = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(\d{2,4}[a-z]{2,8})[-_]?(\d{3,5}[a-z]?)(?:$|[^a-z0-9])`)
	mgstageIDPartsStrict = regexp.MustCompile(`(?i)^([a-z0-9]+)-([0-9]+[a-z]?)$`)
	mgstageCompactIDRe   = regexp.MustCompile(`^(\d{2,4}[A-Z]{2,8})(\d{3,5}[A-Z]?)$`)
)

// New creates a new MGStage scraper
func New(cfg *config.Config) *Scraper {
	proxyConfig := config.ResolveScraperProxy(cfg.Scrapers.Proxy, cfg.Scrapers.MGStage.Proxy)

	// Create resty client with proxy support
	client, err := httpclient.NewRestyClient(
		proxyConfig,
		30*time.Second,
		3,
	)
	usingProxy := err == nil && proxyConfig.Enabled && strings.TrimSpace(proxyConfig.URL) != ""
	if err != nil {
		logging.Errorf("MGStage: Failed to create HTTP client with proxy: %v, using explicit no-proxy fallback", err)
		client = httpclient.NewRestyClientNoProxy(30*time.Second, 3)
	}

	userAgent := config.ResolveScraperUserAgent(
		cfg.Scrapers.UserAgent,
		cfg.Scrapers.MGStage.UseFakeUserAgent,
		cfg.Scrapers.MGStage.FakeUserAgent,
	)
	client.SetHeader("User-Agent", userAgent)

	// Add browser-like headers
	client.SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	client.SetHeader("Accept-Language", "ja,en-US;q=0.7,en;q=0.3")
	client.SetHeader("Accept-Encoding", "gzip, deflate, br")
	client.SetHeader("Connection", "keep-alive")
	client.SetHeader("Upgrade-Insecure-Requests", "1")

	// Set age verification cookie (required for MGStage)
	client.SetHeader("Cookie", "adc=1")

	if proxyConfig.Enabled {
		logging.Infof("MGStage: Using proxy %s", httpclient.SanitizeProxyURL(proxyConfig.URL))
	}

	// Calculate request delay from config (milliseconds to duration)
	requestDelay := time.Duration(cfg.Scrapers.MGStage.RequestDelay) * time.Millisecond

	scraper := &Scraper{
		client:       client,
		enabled:      cfg.Scrapers.MGStage.Enabled,
		usingProxy:   usingProxy,
		requestDelay: requestDelay,
	}

	// Initialize lastRequestTime with zero time
	scraper.lastRequestTime.Store(time.Time{})

	if requestDelay > 0 {
		logging.Infof("MGStage: Rate limiting enabled with %v delay between requests", requestDelay)
	}

	return scraper
}

// Name returns the scraper identifier
func (s *Scraper) Name() string {
	return "mgstage"
}

// IsEnabled returns whether the scraper is enabled
func (s *Scraper) IsEnabled() bool {
	return s.enabled
}

// ResolveSearchQuery normalizes MGStage-specific IDs from free-form input.
// This is primarily used by batch scraping to preserve 3-digit numeric prefixes
// (e.g., "259LUXU-1806"), which generic filename matching can strip to "LUXU-1806".
func (s *Scraper) ResolveSearchQuery(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false
	}

	// Prefer explicit MGStage product URLs when provided.
	if m := mgstageURLIDRe.FindStringSubmatch(input); len(m) > 1 {
		if normalized, ok := normalizeMGStageIDToken(m[1]); ok {
			return normalized, true
		}
		return strings.ToUpper(strings.TrimSpace(m[1])), true
	}

	// Detect prefixed IDs embedded in filenames/text.
	if m := mgstagePrefixedIDRe.FindStringSubmatch(input); len(m) == 3 {
		if normalized, ok := normalizeMGStageIDToken(m[1] + "-" + m[2]); ok {
			return normalized, true
		}
	}

	return "", false
}

// GetURL attempts to find the URL for a given movie ID using MGStage search
func (s *Scraper) GetURL(id string) (string, error) {
	// Normalize ID for search (remove hyphens, lowercase)
	searchID := normalizeIDForSearch(id)
	url := fmt.Sprintf(searchURL, searchID)

	s.waitForRateLimit()

	resp, err := s.client.R().Get(url)
	s.updateLastRequestTime()

	if err != nil {
		return "", fmt.Errorf("failed to search MGStage: %w", err)
	}

	if resp.StatusCode() != 200 {
		// Search can be blocked while direct product URLs still work.
		// Try direct URL fallback before returning hard failure.
		directURL := fmt.Sprintf(productURL, id)
		s.waitForRateLimit()

		directResp, directErr := s.client.R().Get(directURL)
		s.updateLastRequestTime()

		if directErr == nil && directResp.StatusCode() == 200 {
			return directURL, nil
		}

		return "", s.httpStatusError("search", resp.StatusCode())
	}

	// Parse search results to find product URL
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
	if err != nil {
		return "", fmt.Errorf("failed to parse search results: %w", err)
	}

	// Look for product links in search results
	var foundURL string
	normalizedID := normalizeIDForSearch(id)

	doc.Find("a[href*='/product/product_detail/']").Each(func(i int, sel *goquery.Selection) {
		if foundURL != "" {
			return
		}

		href, exists := sel.Attr("href")
		if !exists {
			return
		}

		// Match by normalized product ID extracted from URL path.
		hrefID := extractIDFromURL(href)
		if hrefID != "" && normalizeIDForSearch(hrefID) == normalizedID {
			// Make URL absolute if needed
			if strings.HasPrefix(href, "/") {
				foundURL = baseURL + href
			} else {
				foundURL = href
			}
		}
	})

	if foundURL != "" {
		return foundURL, nil
	}

	// If no match found in search, try direct product URL
	directURL := fmt.Sprintf(productURL, id)
	s.waitForRateLimit()

	resp, err = s.client.R().Get(directURL)
	s.updateLastRequestTime()

	if err == nil && resp.StatusCode() == 200 {
		return directURL, nil
	}

	return "", models.NewScraperNotFoundError("MGStage", "movie not found on MGStage")
}

// Search searches for and scrapes metadata for a given movie ID
func (s *Scraper) Search(id string) (*models.ScraperResult, error) {
	url, err := s.GetURL(id)
	if err != nil {
		return nil, err
	}

	s.waitForRateLimit()

	resp, err := s.client.R().Get(url)
	s.updateLastRequestTime()

	if err != nil {
		return nil, fmt.Errorf("failed to fetch data from MGStage: %w", err)
	}

	if resp.StatusCode() != 200 {
		return nil, s.httpStatusError("detail", resp.StatusCode())
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	result, err := s.parseHTML(doc, url)
	if err != nil {
		return nil, err
	}

	if !mgstageIDsMatch(id, result.ID) {
		return nil, models.NewScraperNotFoundError("MGStage", fmt.Sprintf("movie %s not found on MGStage", strings.TrimSpace(id)))
	}

	return result, nil
}

// parseHTML extracts metadata from MGStage HTML
func (s *Scraper) parseHTML(doc *goquery.Document, sourceURL string) (*models.ScraperResult, error) {
	result := &models.ScraperResult{
		Source:           s.Name(),
		SourceURL:        sourceURL,
		Language:         "ja", // MGStage provides Japanese metadata
		ShouldCropPoster: true, // MGStage provides landscape cover images
	}

	// Extract ID from URL or page
	urlID := extractIDFromURL(sourceURL)
	result.ID = urlID
	tableID := extractTableValue(doc, "品番：")
	if tableID != "" {
		result.ID = tableID
	}

	// Set ContentID to same as ID for MGStage (they use standard DVD-style IDs)
	result.ContentID = result.ID

	logging.Debugf("MGStage: Extracted ID=%s, ContentID=%s", result.ID, result.ContentID)

	// Extract title from <title> tag
	title := doc.Find("title").Text()
	title = cleanTitle(title)
	result.Title = title
	result.OriginalTitle = title

	// Extract description
	result.Description = extractDescription(doc)

	// Extract release date
	dateStr := extractTableValue(doc, "配信開始日：")
	if dateStr != "" {
		t, err := time.Parse("2006/01/02", dateStr)
		if err == nil {
			result.ReleaseDate = &t
		}
	}

	// Extract runtime
	runtimeStr := extractTableValue(doc, "収録時間：")
	if runtimeStr != "" {
		re := regexp.MustCompile(`(\d+)\s*(?:min|分)`)
		matches := re.FindStringSubmatch(runtimeStr)
		if len(matches) > 1 {
			runtime, _ := strconv.Atoi(matches[1])
			result.Runtime = runtime
		}
	}

	// Extract maker
	result.Maker = extractTableLinkValue(doc, "メーカー：")

	// Extract label
	result.Label = extractTableLinkValue(doc, "レーベル：")

	// Extract series
	result.Series = extractTableLinkValue(doc, "シリーズ：")

	// Extract genres
	result.Genres = extractGenres(doc)

	// Extract actresses
	result.Actresses = extractActresses(doc)

	// Extract rating
	result.Rating = extractRating(doc)

	// Extract cover URL
	result.CoverURL = extractCoverURL(doc)
	result.PosterURL = result.CoverURL // Will be cropped from cover

	// Extract screenshots
	result.ScreenshotURL = extractScreenshots(doc)

	// Extract trailer URL
	result.TrailerURL = extractTrailerURL(doc, s.client)

	// Guard against generic MGStage landing/search pages that can return 200
	// with site-wide title/description and no product metadata.
	if !hasProductSignals(result, tableID) {
		lookupID := result.ID
		if lookupID == "" {
			lookupID = urlID
		}
		if lookupID == "" {
			lookupID = "unknown"
		}
		return nil, models.NewScraperNotFoundError("MGStage", fmt.Sprintf("movie %s not found on MGStage", lookupID))
	}

	return result, nil
}

// waitForRateLimit enforces the request delay between requests
func (s *Scraper) waitForRateLimit() {
	if s.requestDelay == 0 {
		return // No rate limiting configured
	}

	// Get last request time
	lastReq := s.lastRequestTime.Load()
	if lastReq == nil {
		return // First request, no need to wait
	}

	lastTime := lastReq.(time.Time)
	if lastTime.IsZero() {
		return // First request, no need to wait
	}

	// Calculate how long to wait
	elapsed := time.Since(lastTime)
	if elapsed < s.requestDelay {
		waitTime := s.requestDelay - elapsed
		logging.Debugf("MGStage: Rate limit wait: %v", waitTime)
		time.Sleep(waitTime)
	}
}

// updateLastRequestTime updates the timestamp of the last request
func (s *Scraper) updateLastRequestTime() {
	s.lastRequestTime.Store(time.Now())
}

func (s *Scraper) httpStatusError(stage string, statusCode int) error {
	msg := fmt.Sprintf("MGStage %s returned status code %d", stage, statusCode)
	if statusCode == 403 {
		if s.usingProxy {
			msg += " (proxy likely blocked by MGStage; disable proxy for this scraper or use a different proxy)"
		} else {
			msg += " (access blocked by MGStage)"
		}
	}
	return models.NewScraperStatusError("MGStage", statusCode, msg)
}

// normalizeIDForSearch normalizes ID for MGStage search
func normalizeIDForSearch(id string) string {
	id = strings.ToLower(id)
	id = strings.ReplaceAll(id, "-", "")
	return id
}

func normalizeMGStageIDToken(token string) (string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}

	token = strings.ReplaceAll(token, "_", "-")
	token = strings.ToUpper(token)

	if !strings.Contains(token, "-") {
		compact := strings.ReplaceAll(token, "-", "")
		m := mgstageCompactIDRe.FindStringSubmatch(compact)
		if len(m) == 3 {
			token = m[1] + "-" + m[2]
		}
	}

	if m := mgstageIDPartsStrict.FindStringSubmatch(token); len(m) == 3 {
		return m[1] + "-" + m[2], true
	}

	return "", false
}

// extractIDFromURL extracts the ID from MGStage product URL
func extractIDFromURL(url string) string {
	re := regexp.MustCompile(`/product/product_detail/([^/]+)/`)
	matches := re.FindStringSubmatch(url)
	if len(matches) > 1 {
		return strings.ToUpper(matches[1])
	}
	return ""
}

// extractTableValue extracts a value from table by header text
// Supports both <tr><th>...</th><td>...</td></tr> and <th>...</th><td>...</td> patterns
func extractTableValue(doc *goquery.Document, headerText string) string {
	var value string

	// First try the standard <tr> pattern
	doc.Find("tr").Each(func(i int, row *goquery.Selection) {
		if value != "" {
			return
		}

		th := row.Find("th").First()
		if strings.Contains(th.Text(), headerText) {
			td := row.Find("td").First()
			value = cleanString(td.Text())
		}
	})

	// If not found, try the MGStage pattern where <th> and <td> are siblings
	if value == "" {
		doc.Find(".detail_data th, .detail_data td").Each(func(i int, sel *goquery.Selection) {
			if value != "" {
				return
			}

			if sel.Is("th") && strings.Contains(sel.Text(), headerText) {
				// Get the next sibling which should be the <td>
				next := sel.Next()
				if next.Is("td") {
					value = cleanString(next.Text())
				}
			}
		})
	}

	return value
}

// extractTableLinkValue extracts a link text from table by header text
// Supports both <tr><th>...</th><td><a>...</a></td></tr> and <th>...</th><td><a>...</a></td> patterns
func extractTableLinkValue(doc *goquery.Document, headerText string) string {
	var value string

	// First try the standard <tr> pattern
	doc.Find("tr").Each(func(i int, row *goquery.Selection) {
		if value != "" {
			return
		}

		th := row.Find("th").First()
		if strings.Contains(th.Text(), headerText) {
			link := row.Find("td a").First()
			value = cleanString(link.Text())
		}
	})

	// If not found, try the MGStage pattern where <th> and <td> are siblings
	if value == "" {
		doc.Find(".detail_data th").Each(func(i int, sel *goquery.Selection) {
			if value != "" {
				return
			}

			if strings.Contains(sel.Text(), headerText) {
				// Get the next sibling which should be the <td>
				next := sel.Next()
				if next.Is("td") {
					link := next.Find("a").First()
					value = cleanString(link.Text())
				}
			}
		})
	}

	return value
}

// extractGenres extracts genre tags from the page
func extractGenres(doc *goquery.Document) []string {
	genres := make([]string, 0)
	seen := make(map[string]bool)

	// Extract from standard <tr> pattern
	doc.Find("tr").Each(func(i int, row *goquery.Selection) {
		th := row.Find("th").First()
		if strings.Contains(th.Text(), "ジャンル：") {
			row.Find("td a").Each(func(j int, link *goquery.Selection) {
				genre := cleanString(link.Text())
				if genre != "" && !seen[genre] {
					seen[genre] = true
					genres = append(genres, genre)
				}
			})
		}
	})

	// Also try MGStage pattern where <th> and <td> are siblings
	doc.Find(".detail_data th").Each(func(i int, sel *goquery.Selection) {
		if strings.Contains(sel.Text(), "ジャンル：") {
			next := sel.Next()
			if next.Is("td") {
				next.Find("a").Each(func(j int, link *goquery.Selection) {
					genre := cleanString(link.Text())
					if genre != "" && !seen[genre] {
						seen[genre] = true
						genres = append(genres, genre)
					}
				})
				// Also extract text content if no links (genres might be plain text)
				if len(genres) == 0 {
					text := cleanString(next.Text())
					if text != "" && !seen[text] {
						seen[text] = true
						genres = append(genres, text)
					}
				}
			}
		}
	})

	return genres
}

// extractActresses extracts actress information from the page
func extractActresses(doc *goquery.Document) []models.ActressInfo {
	actresses := make([]models.ActressInfo, 0)
	seen := make(map[string]bool)

	// Extract from standard <tr> pattern
	doc.Find("tr").Each(func(i int, row *goquery.Selection) {
		th := row.Find("th").First()
		if strings.Contains(th.Text(), "出演：") {
			row.Find("td a").Each(func(j int, link *goquery.Selection) {
				name := cleanString(link.Text())
				if name == "" || seen[name] {
					return
				}
				seen[name] = true
				actresses = append(actresses, createActressInfo(name))
			})
		}
	})

	// Also try MGStage pattern where <th> and <td> are siblings
	doc.Find(".detail_data th").Each(func(i int, sel *goquery.Selection) {
		if strings.Contains(sel.Text(), "出演：") {
			next := sel.Next()
			if next.Is("td") {
				next.Find("a").Each(func(j int, link *goquery.Selection) {
					name := cleanString(link.Text())
					if name == "" || seen[name] {
						return
					}
					seen[name] = true
					actresses = append(actresses, createActressInfo(name))
				})
			}
		}
	})

	return actresses
}

// createActressInfo creates an ActressInfo from a name string
func createActressInfo(name string) models.ActressInfo {
	// Check if name is Japanese
	isJapanese := regexp.MustCompile(`\p{Han}|\p{Hiragana}|\p{Katakana}`).MatchString(name)

	actress := models.ActressInfo{}
	if isJapanese {
		actress.JapaneseName = name
	} else {
		// Try to split Western names
		parts := strings.Fields(name)
		if len(parts) >= 2 {
			actress.LastName = parts[0]
			actress.FirstName = parts[1]
		} else if len(parts) == 1 {
			actress.FirstName = parts[0]
		}
	}

	return actress
}

// extractRating extracts rating from the page
func extractRating(doc *goquery.Document) *models.Rating {
	// MGStage uses star ratings displayed as CSS classes
	// Look for elements with star_ class
	var rating float64

	doc.Find(".star_, [class*='star']").Each(func(i int, sel *goquery.Selection) {
		class, _ := sel.Attr("class")
		// Extract rating from class like "star_40" (means 4.0/5.0)
		re := regexp.MustCompile(`star_(\d+)`)
		matches := re.FindStringSubmatch(class)
		if len(matches) > 1 {
			if val, err := strconv.ParseFloat(matches[1], 64); err == nil {
				// Convert from 0-50 scale to 0-10 scale
				rating = val / 5.0
			}
		}
	})

	// Also try looking for review/evaluator count
	var votes int
	doc.Find(".review_cnt, .evaluator_cnt").Each(func(i int, sel *goquery.Selection) {
		text := sel.Text()
		re := regexp.MustCompile(`(\d+)`)
		matches := re.FindStringSubmatch(text)
		if len(matches) > 1 {
			votes, _ = strconv.Atoi(matches[1])
		}
	})

	if rating > 0 || votes > 0 {
		return &models.Rating{
			Score: rating,
			Votes: votes,
		}
	}

	return nil
}

// extractCoverURL extracts the cover image URL
func extractCoverURL(doc *goquery.Document) string {
	var coverURL string

	// Look for enlarge link
	doc.Find("a.link_magnify, a.enlarge, a[href*='jacket']").Each(func(i int, sel *goquery.Selection) {
		if coverURL != "" {
			return
		}

		href, exists := sel.Attr("href")
		if exists && (strings.Contains(href, ".jpg") || strings.Contains(href, ".png")) {
			coverURL = href
		}
	})

	// Also check for main image
	if coverURL == "" {
		doc.Find("img[src*='jacket'], img[src*='cover']").Each(func(i int, sel *goquery.Selection) {
			if coverURL != "" {
				return
			}

			src, exists := sel.Attr("src")
			if exists {
				// Try to get larger version
				src = strings.Replace(src, "ps.", "pl.", 1)
				coverURL = src
			}
		})
	}

	// Make URL absolute if needed
	if coverURL != "" && !strings.HasPrefix(coverURL, "http") {
		coverURL = baseURL + coverURL
	}

	return coverURL
}

// extractScreenshots extracts screenshot URLs from the page
func extractScreenshots(doc *goquery.Document) []string {
	screenshots := make([]string, 0)

	// Look for sample image links
	doc.Find("a.sample_image, a[href*='sample'], a[href*='screenshot']").Each(func(i int, sel *goquery.Selection) {
		href, exists := sel.Attr("href")
		if !exists {
			return
		}

		if strings.Contains(href, ".jpg") || strings.Contains(href, ".png") {
			// Make URL absolute if needed
			if !strings.HasPrefix(href, "http") {
				href = baseURL + href
			}
			screenshots = append(screenshots, href)
		}
	})

	return screenshots
}

// extractTrailerURL extracts the trailer video URL
// MGStage uses a two-step process: iframe -> .ism manifest -> .mp4 conversion
func extractTrailerURL(doc *goquery.Document, client *resty.Client) string {
	// Step 1: Find iframe or trailer link
	var trailerID string

	doc.Find("iframe, a[href*='sample'], a[href*='trailer']").Each(func(i int, sel *goquery.Selection) {
		if trailerID != "" {
			return
		}

		// Check src for iframe
		src, exists := sel.Attr("src")
		if !exists {
			// Check href for links
			src, exists = sel.Attr("href")
		}

		if !exists {
			return
		}

		// Look for video ID patterns
		re := regexp.MustCompile(`(?:video|sample)[=/]([a-zA-Z0-9_-]+)`)
		matches := re.FindStringSubmatch(src)
		if len(matches) > 1 {
			trailerID = matches[1]
		}
	})

	if trailerID == "" {
		return ""
	}

	// Step 2: Try to construct .ism/.mp4 URL
	// MGStage typically uses: /sample/{id}/{id}.ism or similar patterns
	// For now, return empty as trailer extraction requires site-specific knowledge
	// that may change. Users can add trailers manually or use other scrapers.
	return ""
}

func extractDescription(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}

	// MGStage markup changes frequently; try multiple selectors in order.
	// Current SIRO pages commonly render synopsis in #introduction dd.
	selectors := []string{
		"p.txt.introduction",
		"#introduction .txt.introduction",
		"#introduction dd",
	}

	for _, selector := range selectors {
		text := cleanString(doc.Find(selector).First().Text())
		if text != "" {
			return text
		}
	}

	// Fallback to meta description fields.
	for _, selector := range []string{"meta[property='og:description']", "meta[name='Description']"} {
		content, exists := doc.Find(selector).First().Attr("content")
		if !exists {
			continue
		}
		text := cleanString(content)
		if text != "" && !isGenericMGStageDescription(text) {
			return text
		}
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

// cleanTitle extracts the clean title from MGStage page title
// MGStage format: 「Movie Title」：エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞
func cleanTitle(title string) string {
	// Try to extract content within Japanese brackets 「」
	re := regexp.MustCompile(`「([^」]+)」`)
	matches := re.FindStringSubmatch(title)
	if len(matches) > 1 {
		return cleanString(matches[1])
	}

	// Fallback: Remove site suffix patterns
	// Split on Japanese colon (：) which separates title from site suffix
	if idx := strings.Index(title, "："); idx > 0 {
		title = title[:idx]
	}

	// Also try regular colon
	if idx := strings.Index(title, ":"); idx > 0 {
		title = title[:idx]
	}

	// Remove pipe separators
	title = strings.Split(title, "|")[0]
	title = strings.Split(title, "｜")[0]

	// Remove common prefixes/suffixes
	title = strings.TrimSuffix(title, " - MGStage")
	title = strings.TrimSuffix(title, "- MGStage")

	cleaned := cleanString(title)
	if isGenericMGStageTitle(cleaned) {
		return ""
	}

	return cleaned
}

func hasProductSignals(result *models.ScraperResult, tableID string) bool {
	if result == nil {
		return false
	}

	if tableID != "" {
		return true
	}

	if result.Runtime > 0 || result.ReleaseDate != nil {
		return true
	}
	if result.Maker != "" || result.Label != "" || result.Series != "" {
		return true
	}
	if len(result.Genres) > 0 || len(result.Actresses) > 0 {
		return true
	}
	if result.CoverURL != "" || result.PosterURL != "" || len(result.ScreenshotURL) > 0 {
		return true
	}

	return false
}

func mgstageIDsMatch(requestedID, parsedID string) bool {
	requested := normalizeIDForSearch(strings.TrimSpace(requestedID))
	parsed := normalizeIDForSearch(strings.TrimSpace(parsedID))
	if requested == "" || parsed == "" {
		return false
	}
	return requested == parsed
}

func isGenericMGStageTitle(title string) bool {
	title = cleanString(title)
	if title == "" {
		return false
	}

	if title == "エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞" {
		return true
	}

	return strings.Contains(title, "MGS動画") && strings.Contains(title, "エロ動画・アダルトビデオ")
}

func isGenericMGStageDescription(description string) bool {
	description = cleanString(description)
	if description == "" {
		return false
	}

	return strings.Contains(description, "MGS動画") &&
		(strings.Contains(description, "エロ動画") || strings.Contains(description, "アダルトビデオ"))
}
