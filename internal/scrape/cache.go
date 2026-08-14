package scrape

import (
	"context"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
)

// tryCache checks the movie database for a previously scraped result.
// On cache hit, returns a ScrapeResult with the cached movie data.
// Poster generation is intentionally NOT triggered for cache hits — the poster
// already exists on disk from the original scrape, and re-generating it would
// be redundant (posters are keyed by movie ID + format, not translation hash).
func (s *Scraper) tryCache(ctx context.Context, cmd ScrapeCmd, actressRepo database.ActressRepositoryInterface, explicitSelection bool, startTime time.Time) *ScrapeResult {
	if s.movieRepo == nil {
		return nil
	}

	cached, err := s.movieRepo.FindByID(ctx, cmd.MovieID)
	if err != nil {
		if !database.IsNotFound(err) {
			logging.Debugf("[scrape] Cache lookup failed for %s: %v", cmd.MovieID, err)
		}
		return nil
	}

	logging.Debugf("[scrape] Found %s in cache (Title=%s, Maker=%s)", cmd.MovieID, cached.Title, cached.Maker)

	needsPersistence := false
	translationWarning := ""
	var translationOutput *translation.TranslationOutput
	if s.cfg != nil && s.cfg.TranslationEnabled {
		currentHash := s.cfg.TranslationSettingsHash
		targetLang := s.cfg.TranslationTargetLang
		hasValidTranslation := false
		for _, trans := range cached.Translations {
			if trans.Language == targetLang && trans.SettingsHash == currentHash {
				hasValidTranslation = true
				break
			}
		}
		if !hasValidTranslation {
			logging.Infof("[scrape] Translation settings changed, re-translating cached result for %s", cmd.MovieID)
			warn, transOutput := applyTranslation(ctx, cached, s.translator)
			if warn != "" {
				translationWarning = warn
				logging.Warnf("[scrape] Partial translation warning for cached %s: %s", cmd.MovieID, warn)
			}
			translationOutput = transOutput
			needsPersistence = true
		}
	}

	scrapedToReturn := cached
	fieldSources := buildFieldSourcesFromCachedMovie(cached)

	if actressRepo != nil {
		if enriched := enrichActressesFromDB(ctx, scrapedToReturn, actressRepo, s.cfg); enriched > 0 {
			logging.Debugf("[scrape] Enriched %d actresses from database after cache hit", enriched)
		}
	}
	if enriched := enrichActressesFromBuiltinCache(scrapedToReturn); enriched > 0 {
		needsPersistence = true
		logging.Debugf("[scrape] Enriched %d actresses from built-in cache after cache hit", enriched)
	}
	if invalid := validateActressThumbnails(scrapedToReturn, s.cfg); invalid > 0 {
		logging.Debugf("[scrape] Rejected %d invalid actress thumbnails after cache hit", invalid)
		needsPersistence = true
	}
	var resolverWarnings []string
	if s.registry != nil {
		var resolverOverride []string
		if explicitSelection {
			resolverOverride = resolveScraperNames(cmd.SelectedScrapers, cmd.PriorityOverride, s.cfg)
		}
		if enriched := enrichActressesFromResolvers(ctx, scrapedToReturn, s.registry, s.cfg, &resolverWarnings, resolverOverride); enriched > 0 {
			needsPersistence = true
			logging.Debugf("[scrape] Enriched %d actresses from metadata resolvers after cache hit", enriched)
		}
	}

	actressSources := buildActressSourcesFromCachedMovie(scrapedToReturn)

	now := time.Now()
	if len(resolverWarnings) > 0 {
		result := &ScrapeResult{
			Movie:              scrapedToReturn,
			FieldSources:       fieldSources,
			ActressSources:     actressSources,
			ScraperResults:     []*models.ScraperResult{ScraperResultFromCachedMovie(cached)},
			Cached:             true,
			TranslationWarning: translationWarning,
			TranslationOutput:  translationOutput,
			Status:             StatusCompleted,
			NeedsPersistence:   needsPersistence,
			StartedAt:          startTime,
			EndedAt:            now,
			Warning:            strings.Join(resolverWarnings, "; "),
		}
		return result
	}
	return &ScrapeResult{
		Movie:              scrapedToReturn,
		FieldSources:       fieldSources,
		ActressSources:     actressSources,
		ScraperResults:     []*models.ScraperResult{ScraperResultFromCachedMovie(cached)},
		Cached:             true,
		TranslationWarning: translationWarning,
		TranslationOutput:  translationOutput,
		Status:             StatusCompleted,
		NeedsPersistence:   needsPersistence,
		StartedAt:          startTime,
		EndedAt:            now,
	}
}
