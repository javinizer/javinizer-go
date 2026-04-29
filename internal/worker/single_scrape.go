package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	httpclientiface "github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
)

var processedMovieIDsMutex sync.Mutex

func safeSearch(ctx context.Context, scraper models.Scraper, id string) (result *models.ScraperResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("scraper panic: %v", r)
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	result, err = scraper.Search(ctx, id)
	if result != nil {
		result.NormalizeMediaURLs()
	}
	return result, err
}

func safeScrapeURL(ctx context.Context, scraper models.DirectURLScraper, url string) (result *models.ScraperResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("scraper panic: %v", r)
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	result, err = scraper.ScrapeURL(ctx, url)
	if result != nil {
		result.NormalizeMediaURLs()
	}
	return result, err
}

func scraperListContains(scrapers []string, target string) bool {
	for _, scraper := range scrapers {
		if strings.EqualFold(strings.TrimSpace(scraper), target) {
			return true
		}
	}
	return false
}

func extractIDFromURL(urlStr string, registry *models.ScraperRegistry) string {
	for _, scraper := range registry.GetAll() {
		if handler, ok := scraper.(models.URLHandler); ok {
			if handler.CanHandleURL(urlStr) {
				if id, err := handler.ExtractIDFromURL(urlStr); err == nil && id != "" {
					return id
				}
			}
		}
	}
	return ""
}

func resolveScraperQueryForInputs(scraper models.Scraper, inputs ...string) (string, bool) {
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		key := strings.ToLower(input)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if mappedQuery, ok := models.ResolveSearchQueryForScraper(scraper, input); ok {
			return mappedQuery, true
		}
	}
	return "", false
}

func RunBatchScrapeOnce(
	ctx context.Context,
	job *BatchJob,
	filePath string,
	fileIndex int,
	queryOverride string,
	registry *models.ScraperRegistry,
	agg *aggregator.Aggregator,
	movieRepo *database.MovieRepository,
	fileMatcher *matcher.Matcher,
	httpClient httpclientiface.HTTPClient,
	userAgent string,
	referer string,
	force bool,
	updateMode bool,
	selectedScrapers []string,
	scraperPriorityOverride []string,
	processedMovieIDs map[string]bool,
	cfg *config.Config,
	scalarStrategy string,
	arrayStrategy string,
) (*models.Movie, *FileResult, error) {
	logging.Debugf("[Batch %s] Starting scrape for file %d: %s (force=%v, customScrapers=%v, priorityOverride=%v, queryOverride=%s)",
		job.ID, fileIndex, filePath, force, selectedScrapers, scraperPriorityOverride, queryOverride)

	startTime := time.Now()

	query, earlyResult, err := resolveScrapeQuery(ctx, job, filePath, fileIndex, queryOverride, registry, fileMatcher, startTime)
	if earlyResult != nil {
		return nil, earlyResult, err
	}

	usingCustomScrapers := len(selectedScrapers) > 0
	skipCache := force || usingCustomScrapers || query.matcherMissFallback

	var actressRepo *database.ActressRepository
	if cfg != nil && cfg.Metadata.ActressDatabase.Enabled {
		actressRepo = database.NewActressRepository(movieRepo.GetDB())
	}

	if !skipCache {
		cachedMovie, cachedResult, cacheErr := handleCacheHit(ctx, job, filePath, fileIndex, query.movieID, query.matchResultPtr, movieRepo, actressRepo, httpClient, userAgent, referer, processedMovieIDs, cfg, updateMode, scalarStrategy, arrayStrategy, startTime)
		if cachedResult != nil {
			return cachedMovie, cachedResult, nil
		}
		if cacheErr != nil {
			return nil, nil, cacheErr
		}
	} else {
		clearCacheIfForced(job, fileIndex, query.movieID, force, movieRepo)
		if usingCustomScrapers {
			logging.Debugf("[Batch %s] File %d: Custom scrapers specified, bypassing cache", job.ID, fileIndex)
		}
	}

	query.resolvedID = resolveDMMContentID(job, fileIndex, query.movieID, queryOverride, query.matcherMissFallback, selectedScrapers, registry, query)

	results, scraperFailures, cancelResult, queryErr := queryScrapers(ctx, job, filePath, fileIndex, query, queryOverride, registry, selectedScrapers, scraperPriorityOverride, cfg, startTime)
	if cancelResult != nil {
		return nil, cancelResult, queryErr
	}

	if len(results) == 0 {
		errMsg := buildScraperNoResultsError(scraperFailures)
		logging.Debugf("[Batch %s] File %d: No results from any scraper for %s", job.ID, fileIndex, query.movieID)
		return nil, newFailedFileResult(filePath, query.movieID, errMsg, startTime), errors.New(errMsg)
	}

	logging.Debugf("[Batch %s] File %d: Collected %d results from scrapers", job.ID, fileIndex, len(results))

	var movie *models.Movie
	var translationWarning string
	if usingCustomScrapers {
		movie, translationWarning, err = agg.AggregateWithPriority(results, selectedScrapers)
	} else {
		movie, translationWarning, err = agg.Aggregate(results)
	}
	if err != nil {
		errMsg := fmt.Sprintf("Failed to aggregate: %v", err)
		logging.Debugf("[Batch %s] File %d: Aggregation failed: %v", job.ID, fileIndex, err)
		return nil, newAggregationFailedResult(filePath, query.movieID, errMsg, translationWarning, startTime), errors.New(errMsg)
	}

	logging.Debugf("[Batch %s] File %d: Aggregation complete - Title: %s, Maker: %s, Actresses: %d, Genres: %d",
		job.ID, fileIndex, movie.Title, movie.Maker, len(movie.Actresses), len(movie.Genres))

	if movie.ContentID == "" && query.resolvedID != "" && query.resolvedID != query.movieID {
		movie.ContentID = query.resolvedID
		logging.Debugf("[Batch %s] File %d: Using DMM-resolved ContentID %q as fallback (aggregator produced empty ContentID)",
			job.ID, fileIndex, query.resolvedID)
	}

	fieldSources := buildFieldSourcesFromScrapeResults(results, agg.GetResolvedPriorities(), selectedScrapers)
	actressSources := buildActressSourcesFromScrapeResults(results, agg.GetResolvedPriorities(), selectedScrapers, movie.Actresses)
	movie.OriginalFileName = filepath.Base(filePath)

	if actressRepo != nil {
		if enriched := EnrichActressesFromDB(movie, actressRepo, cfg); enriched > 0 {
			logging.Debugf("[Batch %s] File %d: Enriched %d actresses from database after aggregation", job.ID, fileIndex, enriched)
		}
	}

	if updateMode && cfg != nil {
		movie, fieldSources, actressSources = mergeScrapedNFO(ctx, job, fileIndex, filePath, movie, query.matchResultPtr, cfg, scalarStrategy, arrayStrategy, fieldSources, actressSources)
		if actressRepo != nil {
			if enriched := EnrichActressesFromDB(movie, actressRepo, cfg); enriched > 0 {
				logging.Debugf("[Batch %s] File %d: Enriched %d actresses from database after NFO merge", job.ID, fileIndex, enriched)
			}
		}
	}

	posterErr := generateScrapedPoster(ctx, job, fileIndex, movie, httpClient, userAgent, referer, processedMovieIDs, cfg)

	return saveScrapedResult(job, fileIndex, filePath, movie, query.movieID, query.resolvedID, results, usingCustomScrapers, movieRepo, fieldSources, actressSources, posterErr, translationWarning, query.matchResultPtr, startTime)
}
