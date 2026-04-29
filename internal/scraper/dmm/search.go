package dmm

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

func maxPriority(candidates []urlCandidate) int {
	best := 0
	for _, c := range candidates {
		if c.priority > best {
			best = c.priority
		}
	}
	return best
}

func sortCandidates(candidates []urlCandidate) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority > candidates[j].priority
		}
		return candidates[i].idLength < candidates[j].idLength
	})
}

func (s *Scraper) GetURL(id string) (string, error) {
	return s.getURLCtx(context.Background(), id)
}

func (s *Scraper) getURLCtx(ctx context.Context, id string) (string, error) {
	contentID, err := s.ResolveContentIDCtx(ctx, id)

	if err != nil {
		logging.Debugf("DMM: Content-ID resolution failed for %s: %v", id, err)
		return "", fmt.Errorf("movie not found on DMM: %w", err)
	}

	baseID := normalizeID(contentID)

	searchQueries := []string{
		strings.ToLower(strings.ReplaceAll(baseID, "-", "")),
		strings.ToLower(baseID),
		strings.ToLower(strings.ReplaceAll(id, "-", "")),
		strings.ToLower(id),
		strings.ToLower(contentID),
	}

	uniqueQueries := make([]string, 0, len(searchQueries))
	seen := make(map[string]bool)
	for _, q := range searchQueries {
		if !seen[q] && q != "" {
			seen[q] = true
			uniqueQueries = append(uniqueQueries, q)
		}
	}

	allCandidates := make([]urlCandidate, 0)

	for _, searchQuery := range uniqueQueries {
		searchURLFormatted := fmt.Sprintf(searchURL, searchQuery)
		logging.Debugf("DMM: Trying search query: %s", searchQuery)
		logging.Debugf("DMM: Search URL: %s", searchURLFormatted)
		logging.Debugf("DMM: About to make HTTP GET request to: %s", searchURLFormatted)
		logging.Debugf("DMM: HTTP client transport proxy setting: %v", s.client.GetClient().Transport != nil)

		if err := s.rateLimiter.Wait(ctx); err != nil {
			if ctx.Err() != nil {
				logging.Debugf("DMM: Context cancelled before search query '%s'", searchQuery)
				return "", fmt.Errorf("DMM search cancelled: %w", ctx.Err())
			}
			logging.Debugf("DMM: Rate limit wait failed for query '%s': %v", searchQuery, err)
			continue
		}

		resp, err := s.client.R().SetContext(ctx).Get(searchURLFormatted)
		if err != nil {
			logging.Debugf("DMM: Search request failed for query '%s': err=%v", searchQuery, err)
			continue
		}
		if resp.StatusCode() != 200 {
			logging.Debugf("DMM: Search failed for query '%s': status=%d", searchQuery, resp.StatusCode())
			continue
		}

		respBody := resp.String()
		logging.Debugf("DMM: Search response size: %d bytes", len(respBody))

		if len(respBody) > 0 {
			snippet := respBody
			if len(snippet) > 500 {
				snippet = snippet[:500]
			}
			logging.Debugf("DMM: Response snippet: %s", snippet)
		}

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(respBody))
		if err != nil {
			logging.Debugf("DMM: Failed to parse search results for query '%s'", searchQuery)
			continue
		}

		linkCount := doc.Find("a").Length()
		logging.Debugf("DMM: Total links found on search page: %d", linkCount)

		candidates := s.extractCandidateURLs(doc, contentID)
		logging.Debugf("DMM: Found %d candidates from search query '%s'", len(candidates), searchQuery)
		allCandidates = append(allCandidates, candidates...)
	}

	if len(allCandidates) == 0 {
		logging.Debugf("DMM: No candidates from search, trying direct URL construction for %s", contentID)
		allCandidates = s.tryDirectURLs(ctx, contentID)
	} else if maxPriority(allCandidates) < 200 {
		logging.Debugf("DMM: Best search candidate has low priority, trying direct URLs for %s", contentID)
		directCandidates := s.tryDirectURLs(ctx, contentID)
		allCandidates = append(allCandidates, directCandidates...)
	}

	if len(allCandidates) == 0 {
		return "", fmt.Errorf("no scrapable URL found for movie on DMM")
	}

	sortCandidates(allCandidates)

	foundURL := allCandidates[0].url
	logging.Debugf("DMM: Selected URL for %s (priority %d): %s", id, allCandidates[0].priority, foundURL)
	return foundURL, nil
}

func (s *Scraper) tryDirectURLs(ctx context.Context, contentID string) []urlCandidate {
	strippedID := cleanPrefixRegex.ReplaceAllString(strings.ToLower(contentID), "$1")

	// DMM rental PPR catalog prefixes: 1=general, 2=genre, 4=HD, 5=download. Prefix 3 not used for rental.
	rentalPrefixes := []string{"1", "2", "4", "5"}
	var rentalURLs []string
	for _, prefix := range rentalPrefixes {
		rentalURLs = append(rentalURLs, fmt.Sprintf(rentalURL, prefix+strippedID+"r"))
	}

	directURLs := []string{
		fmt.Sprintf(physicalURL, strippedID),
		fmt.Sprintf(digitalURL, strippedID),
		fmt.Sprintf(physicalURL, contentID),
		fmt.Sprintf(digitalURL, contentID),
		fmt.Sprintf(newDigitalURL, strippedID),
		fmt.Sprintf(newAmateurURL, strippedID),
	}
	directURLs = append(directURLs, rentalURLs...)

	var candidates []urlCandidate
	for _, directURL := range directURLs {
		if err := s.rateLimiter.Wait(ctx); err != nil {
			if ctx.Err() != nil {
				logging.Debugf("DMM: Context cancelled trying direct URL")
				return candidates
			}
			logging.Debugf("DMM: Rate limit wait failed for direct URL: %v", err)
			continue
		}

		resp, err := s.client.R().
			SetDoNotParseResponse(true).
			Get(directURL)
		if err != nil {
			logging.Debugf("DMM: Direct URL %s request failed: %v", directURL, err)
			continue
		}
		if resp == nil {
			logging.Debugf("DMM: Direct URL %s returned nil response", directURL)
			continue
		}
		if resp.StatusCode() == 200 || resp.StatusCode() == 302 {
			priority := urlPriority(directURL)

			extractedID := extractContentIDFromURL(directURL)
			// Always strip rental 'r' suffix - DMM uses it across all URL types, not just /rental/
			extractedID = stripRentalSuffix(extractedID)
			idLen := len(extractedID)
			logging.Debugf("DMM: ✓ Found direct URL (priority %d, ID: %s, len: %d): %s", priority, extractedID, idLen, directURL)
			candidates = append(candidates, urlCandidate{
				url:       directURL,
				priority:  priority,
				contentID: extractedID,
				idLength:  idLen,
			})
		}
		logging.Debugf("DMM: Direct URL %s returned status %d", directURL, resp.StatusCode())
	}
	return candidates
}

func urlPriority(rawURL string) int {
	if strings.Contains(rawURL, "/mono/dvd/") {
		return 350
	} else if strings.Contains(rawURL, "/digital/videoa/") || strings.Contains(rawURL, "/digital/videoc/") {
		return 300
	} else if strings.Contains(rawURL, "video.dmm.co.jp/amateur/content/") {
		return 250
	} else if strings.Contains(rawURL, "video.dmm.co.jp/av/content/") {
		return 200
	} else if strings.Contains(rawURL, "/monthly/premium/") {
		return 150
	} else if strings.Contains(rawURL, "/monthly/standard/") {
		return 100
	} else if strings.Contains(rawURL, "/rental/") {
		return 0
	}
	return 0
}

func (s *Scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	url, err := s.getURLCtx(ctx, id)
	if err != nil {
		return nil, err
	}

	var doc *goquery.Document

	if strings.Contains(url, "video.dmm.co.jp") && s.useBrowser {
		logging.Debug("DMM: Using browser mode for video.dmm.co.jp page")

		bodyHTML, err := FetchWithBrowser(ctx, url, s.browserConfig.Timeout, s.proxyProfile)
		if err != nil {
			return nil, fmt.Errorf("browser fetch failed: %w", err)
		}

		doc, err = goquery.NewDocumentFromReader(strings.NewReader(bodyHTML))
		if err != nil {
			return nil, fmt.Errorf("failed to parse HTML from browser: %w", err)
		}
	} else {
		if err := s.rateLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("DMM: rate limit wait failed: %w", err)
		}

		resp, err := s.client.R().SetContext(ctx).Get(url)
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

	return s.parseHTML(ctx, doc, url)
}

func (s *Scraper) ScrapeURL(ctx context.Context, url string) (*models.ScraperResult, error) {
	if !s.CanHandleURL(url) {
		return nil, models.NewScraperNotFoundError("DMM", "URL not handled by DMM scraper")
	}

	var doc *goquery.Document

	if strings.Contains(url, "video.dmm.co.jp") && s.useBrowser {
		logging.Debug("DMM ScrapeURL: Using browser mode for video.dmm.co.jp page")

		bodyHTML, err := FetchWithBrowser(ctx, url, s.browserConfig.Timeout, s.proxyProfile)
		if err != nil {
			return nil, models.NewScraperStatusError("DMM", 0, fmt.Sprintf("browser fetch failed: %v", err))
		}

		doc, err = goquery.NewDocumentFromReader(strings.NewReader(bodyHTML))
		if err != nil {
			return nil, models.NewScraperStatusError("DMM", 0, fmt.Sprintf("failed to parse HTML from browser: %v", err))
		}
	} else {
		if err := s.rateLimiter.Wait(ctx); err != nil {
			return nil, models.NewScraperStatusError("DMM", 0, fmt.Sprintf("rate limit wait failed: %v", err))
		}

		resp, err := s.client.R().SetContext(ctx).Get(url)
		if err != nil {
			return nil, models.NewScraperStatusError("DMM", 0, fmt.Sprintf("failed to fetch URL: %v", err))
		}

		if resp.StatusCode() == 404 {
			return nil, models.NewScraperNotFoundError("DMM", "page not found")
		}

		if resp.StatusCode() == 429 {
			return nil, models.NewScraperStatusError("DMM", 429, "rate limited")
		}

		if resp.StatusCode() == 403 || resp.StatusCode() == 451 {
			return nil, models.NewScraperStatusError("DMM", resp.StatusCode(),
				fmt.Sprintf("DMM access blocked (status %d, likely geo-restriction)", resp.StatusCode()))
		}

		if resp.StatusCode() >= 500 {
			return nil, models.NewScraperStatusError("DMM", resp.StatusCode(),
				fmt.Sprintf("DMM returned server error %d", resp.StatusCode()))
		}

		if resp.StatusCode() != 200 {
			return nil, models.NewScraperStatusError("DMM", resp.StatusCode(),
				fmt.Sprintf("DMM returned status code %d", resp.StatusCode()))
		}

		doc, err = goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
		if err != nil {
			return nil, fmt.Errorf("failed to parse HTML: %w", err)
		}
	}

	return s.parseHTML(ctx, doc, url)
}
