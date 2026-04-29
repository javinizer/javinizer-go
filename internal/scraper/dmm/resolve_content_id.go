package dmm

import (
	"context"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

func (s *Scraper) ResolveContentID(id string) (string, error) {
	return s.ResolveContentIDCtx(context.Background(), id)
}

func (s *Scraper) ResolveContentIDCtx(ctx context.Context, id string) (string, error) {
	if s.contentIDRepo == nil {
		return "", fmt.Errorf("content ID repository not available")
	}

	normalizedID := strings.ToUpper(id)
	if cached, err := s.contentIDRepo.FindBySearchID(normalizedID); err == nil {
		logging.Debugf("DMM: Found cached content-id for %s: %s", id, cached.ContentID)
		return cached.ContentID, nil
	}

	logging.Debugf("DMM: Content-id not cached for %s, attempting to resolve via search", id)

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

		if err := s.rateLimiter.Wait(ctx); err != nil {
			return "", fmt.Errorf("DMM: rate limit wait failed: %w", err)
		}

		resp, err := s.client.R().SetContext(ctx).Get(searchURLFormatted)
		if err != nil {
			return "", fmt.Errorf("DMM search unavailable (possible geo-restriction or network error): %w", err)
		}

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

	foundContentID := candidates[0].contentID
	minLength := candidates[0].length

	for _, c := range candidates[1:] {
		if c.length < minLength {
			minLength = c.length
			foundContentID = c.contentID
		}
	}

	logging.Debugf("DMM: Selected shortest candidate: %s (length: %d) from %d total candidates", foundContentID, minLength, len(candidates))

	mapping := &models.ContentIDMapping{
		SearchID:  normalizedID,
		ContentID: foundContentID,
		Source:    "dmm",
	}

	if err := s.contentIDRepo.Create(mapping); err != nil {
		logging.Debugf("DMM: Failed to cache content-id mapping for %s: %v", id, err)
	} else {
		logging.Debugf("DMM: Cached content-id mapping: %s -> %s", normalizedID, foundContentID)
	}

	return foundContentID, nil
}
