package worker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
)

// RunBatchScrapeOnce performs a single scrape operation for a file within a batch job context
// This function extracts the core scraping logic that can be reused for both initial batch scraping
// and rescraping operations.
//
// Parameters:
//   - ctx: Context for cancellation support
//   - job: Batch job for logging and state tracking
//   - filePath: Path to the video file being scraped
//   - fileIndex: Index of file in batch (for logging, can be 0 for rescrape)
//   - queryOverride: If non-empty, use this as the search query instead of extracting from filename
//   - registry: Scraper registry for querying scrapers
//   - agg: Aggregator for merging scraper results
//   - movieRepo: Movie repository for database operations
//   - matcher: Matcher for extracting IDs from filenames
//   - httpClient: Pre-configured HTTP client with proxy settings
//   - userAgent: User-Agent header value from config
//   - referer: Referer header value from config
//   - force: If true, skip cache and delete existing data
//   - selectedScrapers: If non-empty, use these scrapers instead of defaults
//
// Returns:
//   - movie: Successfully scraped and saved movie metadata
//   - fileResult: FileResult object for updating job status
//   - error: Any error encountered during scraping
//
// Note: This function does NOT call job.UpdateFileResult() - the caller should do that
// to allow for custom timing or additional processing before updating the job state
func RunBatchScrapeOnce(
	ctx context.Context,
	job *BatchJob,
	filePath string,
	fileIndex int,
	queryOverride string,
	registry *models.ScraperRegistry,
	agg *aggregator.Aggregator,
	movieRepo *database.MovieRepository,
	matcher *matcher.Matcher,
	httpClient *http.Client,
	userAgent string,
	referer string,
	force bool,
	selectedScrapers []string,
) (*models.Movie, *FileResult, error) {
	logging.Debugf("[Batch %s] Starting scrape for file %d: %s (force=%v, customScrapers=%v, queryOverride=%s)",
		job.ID, fileIndex, filePath, force, selectedScrapers, queryOverride)

	startTime := time.Now()

	// Step 1: Determine the query (use queryOverride if provided, otherwise extract from filename)
	var movieID string
	if queryOverride != "" {
		movieID = queryOverride
		logging.Debugf("[Batch %s] File %d: Using manual search query: %s", job.ID, fileIndex, movieID)
	} else {
		// Extract ID from filename using matcher
		fileInfo := scanner.FileInfo{
			Path:      filePath,
			Name:      filepath.Base(filePath),
			Extension: filepath.Ext(filePath),
			Dir:       filepath.Dir(filePath),
		}

		matchResults := matcher.Match([]scanner.FileInfo{fileInfo})
		if len(matchResults) == 0 {
			errMsg := "Could not extract movie ID from filename"
			logging.Debugf("[Batch %s] File %d: %s", job.ID, fileIndex, errMsg)

			return nil, &FileResult{
				FilePath:  filePath,
				Status:    JobStatusFailed,
				Error:     errMsg,
				StartedAt: startTime,
			}, errors.New(errMsg)
		}

		movieID = matchResults[0].ID
		logging.Debugf("[Batch %s] File %d: Extracted movie ID: %s", job.ID, fileIndex, movieID)
	}

	// Step 2: Check cache (unless force or custom scrapers)
	usingCustomScrapers := len(selectedScrapers) > 0
	skipCache := force || usingCustomScrapers

	if !skipCache {
		logging.Debugf("[Batch %s] File %d: Checking cache for %s", job.ID, fileIndex, movieID)
		if cached, err := movieRepo.FindByID(movieID); err == nil {
			logging.Debugf("[Batch %s] File %d: Found %s in cache (Title=%s, Maker=%s)",
				job.ID, fileIndex, movieID, cached.Title, cached.Maker)

			now := time.Now()
			return cached, &FileResult{
				FilePath:  filePath,
				MovieID:   movieID,
				Status:    JobStatusCompleted,
				Data:      cached,
				StartedAt: startTime,
				EndedAt:   &now,
			}, nil
		}
		logging.Debugf("[Batch %s] File %d: %s not found in cache, will scrape", job.ID, fileIndex, movieID)
	} else if force {
		// Clear cache if force refresh
		logging.Debugf("[Batch %s] File %d: Force refresh enabled, clearing cache for %s", job.ID, fileIndex, movieID)
		if err := movieRepo.Delete(movieID); err != nil {
			logging.Debugf("[Batch %s] File %d: Cache delete failed (movie may not exist): %v", job.ID, fileIndex, err)
		}
	} else if usingCustomScrapers {
		logging.Debugf("[Batch %s] File %d: Custom scrapers specified, bypassing cache", job.ID, fileIndex)
	}

	// Step 3: Perform DMM content-ID resolution (only if not using manual query)
	var resolvedID string
	if queryOverride == "" {
		if dmmScraper, exists := registry.Get("dmm"); exists {
			if dmmScraperTyped, ok := dmmScraper.(*dmm.Scraper); ok {
				contentID, err := dmmScraperTyped.ResolveContentID(movieID)
				if err != nil {
					logging.Debugf("[Batch %s] File %d: DMM content-ID resolution failed for %s: %v, using original ID",
						job.ID, fileIndex, movieID, err)
					resolvedID = movieID // Fallback to original ID
				} else {
					resolvedID = contentID
					logging.Debugf("[Batch %s] File %d: Resolved content-ID for %s: %s",
						job.ID, fileIndex, movieID, resolvedID)
				}
			} else {
				logging.Debugf("[Batch %s] File %d: DMM scraper type assertion failed, using original ID", job.ID, fileIndex)
				resolvedID = movieID
			}
		} else {
			logging.Debugf("[Batch %s] File %d: DMM scraper not available, using original ID", job.ID, fileIndex)
			resolvedID = movieID
		}
	} else {
		// Manual query - use as-is without resolution
		resolvedID = movieID
	}

	// Step 4: Query scrapers (use selectedScrapers if provided, otherwise use registry defaults)
	results := make([]*models.ScraperResult, 0)
	scraperErrors := make([]string, 0)

	// GetByPriority returns all enabled scrapers when passed empty/nil
	scrapersToUse := registry.GetByPriority(selectedScrapers)
	if len(selectedScrapers) > 0 {
		logging.Debugf("[Batch %s] File %d: Using custom scraper priority: %v (%d scrapers)",
			job.ID, fileIndex, selectedScrapers, len(scrapersToUse))
	} else {
		logging.Debugf("[Batch %s] File %d: Using default scraper priority (%d scrapers)",
			job.ID, fileIndex, len(scrapersToUse))
	}

	for _, scraper := range scrapersToUse {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			logging.Debugf("[Batch %s] File %d: Context cancelled", job.ID, fileIndex)
			now := time.Now()
			return nil, &FileResult{
				FilePath:  filePath,
				MovieID:   movieID,
				Status:    JobStatusCancelled,
				Error:     "Cancelled by user",
				StartedAt: startTime,
				EndedAt:   &now,
			}, ctx.Err()
		default:
		}

		logging.Debugf("[Batch %s] File %d: Querying scraper %s for %s", job.ID, fileIndex, scraper.Name(), resolvedID)
		scraperResult, err := scraper.Search(resolvedID)
		if err != nil {
			logging.Debugf("[Batch %s] File %d: Scraper %s failed: %v", job.ID, fileIndex, scraper.Name(), err)

			// If scraping with resolved ID fails, try with original ID before giving up
			// (but only if we're not using a manual query override)
			if resolvedID != movieID && queryOverride == "" {
				logging.Debugf("[Batch %s] File %d: Retrying scraper %s with original ID: %s",
					job.ID, fileIndex, scraper.Name(), movieID)
				scraperResult, err = scraper.Search(movieID)
				if err != nil {
					logging.Debugf("[Batch %s] File %d: Scraper %s failed with original ID: %v",
						job.ID, fileIndex, scraper.Name(), err)
					scraperErrors = append(scraperErrors, fmt.Sprintf("%s: %v", scraper.Name(), err))
					continue
				}
			} else {
				scraperErrors = append(scraperErrors, fmt.Sprintf("%s: %v", scraper.Name(), err))
				continue
			}
		}

		logging.Debugf("[Batch %s] File %d: Scraper %s returned: Title=%s, Language=%s, Actresses=%d, Genres=%d",
			job.ID, fileIndex, scraper.Name(), scraperResult.Title, scraperResult.Language,
			len(scraperResult.Actresses), len(scraperResult.Genres))
		results = append(results, scraperResult)
	}

	// Step 5: Check if any scrapers succeeded
	if len(results) == 0 {
		errMsg := fmt.Sprintf("Movie not found: %s", strings.Join(scraperErrors, "; "))
		logging.Debugf("[Batch %s] File %d: No results from any scraper for %s", job.ID, fileIndex, movieID)

		now := time.Now()
		return nil, &FileResult{
			FilePath:  filePath,
			MovieID:   movieID,
			Status:    JobStatusFailed,
			Error:     errMsg,
			StartedAt: startTime,
			EndedAt:   &now,
		}, errors.New(errMsg)
	}

	logging.Debugf("[Batch %s] File %d: Collected %d results from scrapers", job.ID, fileIndex, len(results))

	// Step 6: Aggregate results
	logging.Debugf("[Batch %s] File %d: Starting metadata aggregation", job.ID, fileIndex)

	var (
		movie *models.Movie
		err   error
	)
	if usingCustomScrapers {
		// Use custom priority order from manual scrape/rescrape dialog
		logging.Debugf("[Batch %s] File %d: Using custom scraper priority: %v", job.ID, fileIndex, selectedScrapers)
		movie, err = agg.AggregateWithPriority(results, selectedScrapers)
	} else {
		// Use config-defined field priorities
		movie, err = agg.Aggregate(results)
	}
	if err != nil {
		errMsg := fmt.Sprintf("Failed to aggregate: %v", err)
		logging.Debugf("[Batch %s] File %d: Aggregation failed: %v", job.ID, fileIndex, err)

		now := time.Now()
		return nil, &FileResult{
			FilePath:  filePath,
			MovieID:   movieID,
			Status:    JobStatusFailed,
			Error:     errMsg,
			StartedAt: startTime,
			EndedAt:   &now,
		}, errors.New(errMsg)
	}

	logging.Debugf("[Batch %s] File %d: Aggregation complete - Title: %s, Maker: %s, Actresses: %d, Genres: %d",
		job.ID, fileIndex, movie.Title, movie.Maker, len(movie.Actresses), len(movie.Genres))

	// Set original filename for tracking
	movie.OriginalFileName = filepath.Base(filePath)

	// Step 7: Download and crop poster temporarily for review page
	var posterErr *string
	if httpClient != nil {
		if _, err := GenerateTempPoster(ctx, job.ID, movie, httpClient, userAgent, referer); err != nil {
			logging.Warnf("[Batch %s] File %d: Failed to create temp poster: %v (continuing anyway)", job.ID, fileIndex, err)
			errMsg := err.Error()
			posterErr = &errMsg
			// Continue - temp poster is optional for review
		}
	}

	// Step 8: Save to database (KEEP THIS - Option A: maintain consistency with batch scraping)
	// We save immediately even though organize hasn't happened yet
	if !usingCustomScrapers {
		logging.Debugf("[Batch %s] File %d: Saving metadata to database", job.ID, fileIndex)

		if err := movieRepo.Upsert(movie); err != nil {
			logging.Errorf("[Batch %s] File %d: Database save failed: %v", job.ID, fileIndex, err)
			// Continue anyway - we have the data
		} else {
			logging.Debugf("[Batch %s] File %d: Successfully saved to database", job.ID, fileIndex)
		}
	} else {
		logging.Debugf("[Batch %s] File %d: Skipping database save (custom scrapers used)", job.ID, fileIndex)
	}

	// Step 9: Reload from database to get associations (only if saved)
	var finalMovie *models.Movie
	if !usingCustomScrapers {
		reloadedMovie, err := movieRepo.FindByID(movie.ID)
		if err != nil {
			logging.Debugf("[Batch %s] File %d: Failed to reload movie from database: %v", job.ID, fileIndex, err)
			finalMovie = movie // Fallback to aggregated movie
		} else {
			finalMovie = reloadedMovie
			// Preserve DisplayName from aggregated movie (DB may have stale/empty value)
			if movie.DisplayName != "" {
				finalMovie.DisplayName = movie.DisplayName
			}
			logging.Debugf("[Batch %s] File %d: Reloaded movie from database with associations", job.ID, fileIndex)
		}
	} else {
		finalMovie = movie
	}

	// Step 10: Create completed FileResult (caller will update job state)
	now := time.Now()
	fileResult := &FileResult{
		FilePath:    filePath,
		MovieID:     movieID,
		Status:      JobStatusCompleted,
		Data:        finalMovie,
		PosterError: posterErr,
		StartedAt:   startTime,
		EndedAt:     &now,
	}

	logging.Debugf("[Batch %s] File %d: Scrape completed successfully", job.ID, fileIndex)

	return finalMovie, fileResult, nil
}
