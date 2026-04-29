package dmm

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

var stripNumericPrefixRegex = regexp.MustCompile(`^\d+`)

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
			logging.Debugf("DMM: Rate limit wait failed for query '%s': %v", searchQuery, err)
			continue
		}

		resp, err := s.client.R().SetContext(ctx).Get(searchURLFormatted)
		if err != nil || resp.StatusCode() != 200 {
			logging.Debugf("DMM: Search failed for query '%s': status=%d, err=%v", searchQuery, resp.StatusCode(), err)
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
	}

	if len(allCandidates) == 0 {
		return "", fmt.Errorf("no scrapable URL found for movie on DMM")
	}

	sort.Slice(allCandidates, func(i, j int) bool {
		if allCandidates[i].priority != allCandidates[j].priority {
			return allCandidates[i].priority > allCandidates[j].priority
		}
		return allCandidates[i].idLength < allCandidates[j].idLength
	})

	if allCandidates[0].priority < 2 {
		logging.Debugf("DMM: Best candidate has low priority (%d), trying direct URLs for %s", allCandidates[0].priority, contentID)
		directCandidates := s.tryDirectURLs(ctx, contentID)
		if len(directCandidates) > 0 {
			allCandidates = append(allCandidates, directCandidates...)
			sort.Slice(allCandidates, func(i, j int) bool {
				if allCandidates[i].priority != allCandidates[j].priority {
					return allCandidates[i].priority > allCandidates[j].priority
				}
				return allCandidates[i].idLength < allCandidates[j].idLength
			})
		}
	}

	foundURL := allCandidates[0].url
	logging.Debugf("DMM: Selected URL for %s (priority %d): %s", id, allCandidates[0].priority, foundURL)
	return foundURL, nil
}

func (s *Scraper) tryDirectURLs(ctx context.Context, contentID string) []urlCandidate {
	strippedID := stripNumericPrefixRegex.ReplaceAllString(contentID, "")
	strippedID = strings.ToLower(strippedID)

	directURLs := []string{
		fmt.Sprintf(physicalURL, strippedID),
		fmt.Sprintf(digitalURL, strippedID),
		fmt.Sprintf(physicalURL, contentID),
		fmt.Sprintf(digitalURL, contentID),
		fmt.Sprintf(newDigitalURL, strippedID),
		fmt.Sprintf(newAmateurURL, strippedID),
	}

	var candidates []urlCandidate
	for _, directURL := range directURLs {
		if err := s.rateLimiter.Wait(ctx); err != nil {
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
			idLen := len(extractedID)
			logging.Debugf("DMM: ✓ Found direct URL (priority %d, ID: %s, len: %d): %s", priority, extractedID, idLen, directURL)
			candidates = append(candidates, urlCandidate{
				url:       directURL,
				priority:  priority,
				contentID: extractedID,
				idLength:  idLen,
			})
			break
		}
		logging.Debugf("DMM: Direct URL %s returned status %d", directURL, resp.StatusCode())
	}
	return candidates
}

func urlPriority(rawURL string) int {
	if strings.Contains(rawURL, "/mono/dvd/") {
		return 6
	} else if strings.Contains(rawURL, "/digital/videoa/") {
		return 5
	} else if strings.Contains(rawURL, "video.dmm.co.jp/amateur/content/") {
		return 4
	} else if strings.Contains(rawURL, "video.dmm.co.jp/av/content/") {
		return 3
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
