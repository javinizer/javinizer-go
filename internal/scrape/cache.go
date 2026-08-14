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

	scrapedToReturn := cached
	fieldSources := buildFieldSourcesFromCachedMovie(cached)
	actressEnriched := false

	if actressRepo != nil {
		if enriched := enrichActressesFromDB(ctx, scrapedToReturn, actressRepo, s.cfg); enriched > 0 {
			logging.Debugf("[scrape] Enriched %d actresses from database after cache hit", enriched)
			actressEnriched = true
		}
	}
	var cacheEffectivePriority []string
	if explicitSelection {
		cacheEffectivePriority = resolveScraperNames(cmd.SelectedScrapers, cmd.PriorityOverride, s.cfg)
	}
	if builtinCacheAllowedForPriority(s.cfg, cacheEffectivePriority) {
		if enriched := enrichActressesFromBuiltinCache(scrapedToReturn); enriched > 0 {
			needsPersistence = true
			actressEnriched = true
			logging.Debugf("[scrape] Enriched %d actresses from built-in cache after cache hit", enriched)
		}
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
		if enriched := enrichActressesFromResolvers(ctx, scrapedToReturn, s.registry, s.cfg, &resolverWarnings, s.breaker, resolverOverride); enriched > 0 {
			needsPersistence = true
			actressEnriched = true
			logging.Debugf("[scrape] Enriched %d actresses from metadata resolvers after cache hit", enriched)
		}
	}

	actressSources := buildActressSourcesFromCachedMovie(scrapedToReturn)

	translationWarning := ""
	var translationOutput *translation.TranslationOutput
	if s.cfg != nil && s.cfg.TranslationEnabled {
		currentHash := s.cfg.TranslationSettingsHash
		targetLang := s.cfg.TranslationTargetLang
		hasValidTranslation := false
		if actressEnriched {
			hasValidTranslation = false
		} else {
			for _, trans := range cached.Translations {
				if trans.Language == targetLang && trans.SettingsHash == currentHash {
					hasValidTranslation = true
					break
				}
			}
		}
		if !hasValidTranslation {
			logging.Infof("[scrape] Translation settings changed, re-translating cached result for %s", cmd.MovieID)
			warn, transOutput := applyTranslation(ctx, scrapedToReturn, s.translator)
			if warn != "" {
				translationWarning = warn
				logging.Warnf("[scrape] Partial translation warning for cached %s: %s", cmd.MovieID, warn)
			}
			translationOutput = transOutput
			needsPersistence = true
		}
	}

	now := time.Now()
	warning := ""
	if len(resolverWarnings) > 0 {
		warning = strings.Join(resolverWarnings, "; ")
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
		Warning:            warning,
	}
}
