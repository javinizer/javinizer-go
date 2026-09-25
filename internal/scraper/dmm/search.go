package dmm

import (
	"context"
	"fmt"
	"net/http"
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

func (s *scraper) GetURL(ctx context.Context, id string) (string, error) {
	return s.getURLCtx(ctx, id)
}

func (s *scraper) getURLCtx(ctx context.Context, id string) (string, error) {
	url, _, err := s.getURLCtxWithResolution(ctx, id)
	return url, err
}

// getURLCtxWithResolution is getURLCtx plus the resolution origin: fromCache
// reports whether the content id came from the persistent cache instead of
// this call's verified resolver. Search gates its display-id fill on the
// origin — a cached AI mapping whose page publishes no 品番 cannot be
// verified in-flow, while a freshly resolved mapping was just verified.
func (s *scraper) getURLCtxWithResolution(ctx context.Context, id string) (string, bool, error) {
	contentID, fromCache, err := s.resolveContentIDWithOrigin(ctx, id)

	if err != nil {
		logging.Debugf("DMM: Content-ID resolution failed for %s: %v", id, err)
		return "", fromCache, fmt.Errorf("movie not found on DMM: %w", err)
	}

	boundMarker, _, _, rawQuery := classifyRemasterQuery(id)
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
				return "", fromCache, fmt.Errorf("DMM search cancelled: %w", ctx.Err())
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
		for _, candidate := range candidates {
			if boundMarker == "" || bindResolvedCID(candidate.contentID, contentID, rawQuery) {
				allCandidates = append(allCandidates, candidate)
			}
		}
	}

	if len(allCandidates) == 0 {
		logging.Debugf("DMM: No candidates from search, trying direct URL construction for %s", contentID)
		allCandidates = s.tryDirectURLs(ctx, contentID)
	} else if maxPriority(allCandidates) < 200 {
		logging.Debugf("DMM: Best search candidate has low priority, trying direct URLs for %s", contentID)
		directCandidates := s.tryDirectURLs(ctx, contentID)
		allCandidates = append(allCandidates, directCandidates...)
	}

	if boundMarker != "" {
		boundCandidates := make([]urlCandidate, 0, len(allCandidates))
		for _, c := range allCandidates {
			if bindResolvedCID(c.contentID, contentID, rawQuery) {
				boundCandidates = append(boundCandidates, c)
			}
		}
		allCandidates = boundCandidates
	}

	if len(allCandidates) == 0 {
		return "", fromCache, fmt.Errorf("no scrapable URL found for movie on DMM")
	}

	sortCandidates(allCandidates)

	foundURL := allCandidates[0].url
	logging.Debugf("DMM: Selected URL for %s (priority %d): %s", id, allCandidates[0].priority, foundURL)
	return foundURL, fromCache, nil
}

func (s *scraper) tryDirectURLs(ctx context.Context, contentID string) []urlCandidate {
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

func (s *scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	url, resolvedFromCache, err := s.getURLCtxWithResolution(ctx, id)
	if err != nil {
		return nil, err
	}

	var doc *goquery.Document

	if strings.Contains(url, "video.dmm.co.jp") && s.useBrowser {
		logging.Debug("DMM: Using browser mode for video.dmm.co.jp page")

		bodyHTML, err := s.fetchBrowserPage(ctx, url)
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

	foldedMarker, _, _, isCID := classifyRemasterQuery(id)
	res, err := s.parseHTMLWithOptions(ctx, doc, url, foldedMarker != "")
	if err == nil && foldedMarker != "" && !isCID {
		// The page's 品番 is the authoritative display identity even for AI
		// remasters (unlike the cid digits, which diverge from the display
		// number): a page publishing another release's identity means
		// resolution landed on the wrong product (e.g. a stale same-series
		// cache mapping), so miss honestly instead of returning it.
		if !pageDisplayIdentityMatchesQuery(doc, id) {
			return nil, models.NewScraperNotFoundError("DMM", fmt.Sprintf("DMM page for %s publishes a different release", id))
		}
		// The page's authoritative 品番 (zero-trimmed by the site) outranks the
		// query-derived spelling; only fill in when the page provided nothing.
		if res.ID == "" {
			if resolvedFromCache {
				// A cached AI mapping whose page publishes no 品番 is
				// unverified: a stale same-series sibling (dv00999ai under
				// DV-818AI) would otherwise publish its metadata under the
				// query's identity. Invalidate the mapping (its existence is
				// proven by the cache-hit resolution) and re-resolve through
				// the verified resolver — the round-15-style cache repair —
				// falling back to a miss when re-resolution cannot verify the
				// release. The retried pass resolves freshly, so it may fill
				// the canonical spelling from this-call verification.
				if derr := s.contentIDRepo.Delete(ctx, id); derr != nil {
					logging.Debugf("DMM: cannot invalidate content-id mapping for %s: %v", id, derr)
					return nil, models.NewScraperNotFoundError("DMM", fmt.Sprintf("DMM page for %s publishes no identity to verify its cached content-id mapping", id))
				}
				logging.Debugf("DMM: invalidated unverified content-id mapping for %s; re-resolving", id)
				return s.Search(ctx, id)
			}
			res.ID = canonicalRemasterDisplayID(id)
		}
	}
	return res, err
}

// pageDisplayIdentityMatchesQuery reports whether the page's 品番, when it
// publishes a parseable remaster display identity, names the queried release.
// The page-supplied display identity is authoritative for display numbers
// even on AI remasters (unlike cid numbers, which diverge), so a 品番 whose
// padding-normalized identity names a different series, number, catalog
// suffix or marker than the query belongs to the wrong release. A nonempty
// markerless 品番 conflicts the same way on a marker-bearing query: it names
// the base release — plain or carrying the E/Z catalog suffix — not the
// remaster, so the page belongs to the wrong product. Pages without a 品番
// row and unparseable queries publish nothing authoritative to compare and
// keep the existing behavior.
func pageDisplayIdentityMatchesQuery(doc *goquery.Document, query string) bool {
	if doc == nil {
		return true
	}
	display := extractDisplayID(doc)
	if display == "" {
		return true
	}
	// A parseable query is marker-bearing by construction — the tail regex
	// requires the marker — so the query parse doubles as the marker-bearing
	// request gate for the markerless row below.
	qSeries, qNumber, qSuffix, qMarker, qOK := displayIdentityTuple(query)
	pSeries, pNumber, pSuffix, pMarker, ok := displayIdentityTuple(display)
	if !ok {
		// A nonempty markerless 品番 (a cached RCT-156H query whose page
		// publishes RCT-157, or its E/Z-suffixed spelling RCT-157E) names the
		// base release, not the query's remaster: the page is the wrong
		// product's and must miss honestly instead of publishing its metadata
		// under the query's identity.
		if qOK && isMarkerlessDisplayID(display) {
			return false
		}
		return true
	}
	if !qOK {
		return true
	}
	return pSeries == qSeries && pNumber == qNumber && pSuffix == qSuffix && pMarker == qMarker
}

func (s *scraper) ScrapeURL(ctx context.Context, url string) (*models.ScraperResult, error) {
	if !s.CanHandleURL(url) {
		return nil, models.NewScraperNotFoundError("DMM", "URL not handled by DMM scraper")
	}

	var doc *goquery.Document

	if strings.Contains(url, "video.dmm.co.jp") && s.useBrowser {
		logging.Debug("DMM ScrapeURL: Using browser mode for video.dmm.co.jp page")

		bodyHTML, err := s.fetchBrowserPage(ctx, url)
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
			return nil, models.NewScraperStatusError("DMM", http.StatusTooManyRequests, "rate limited")
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

	res, err := s.parseHTML(ctx, doc, url)
	if err == nil {
		fillMarkerIDFromURL(res, url)
	}
	return res, err
}

// fillMarkerIDFromURL ports Search's canonical-spelling fill to direct URL
// scrapes: an H/HD-marker page whose 品番 row is absent publishes an empty
// display ID, so derive the canonical spelling from the URL cid, whose
// number matches the display number for H/HD releases. AI-marker cids do
// not encode the display number (dv00899ai maps to DV-818AI), so they never
// derive a spelling — only the page's authoritative 品番 may publish an
// AI-remaster identity, and an absent row leaves the ID empty.
func fillMarkerIDFromURL(res *models.ScraperResult, url string) {
	if res == nil || res.ID != "" {
		return
	}
	cid := stripRentalSuffixMarkerAware(extractContentIDFromURL(url))
	if cid == "" {
		return
	}
	if marker, _, _, _ := classifyRemasterQuery(cid); marker == "h" {
		res.ID = canonicalRemasterDisplayID(cid)
	}
}
