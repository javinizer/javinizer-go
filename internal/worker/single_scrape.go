package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	httpclientiface "github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

// processedMovieIDsMutex protects concurrent access to processedMovieIDs map
var processedMovieIDsMutex sync.Mutex

// scraperSearchWithContext wraps a scraper.Search() call with context cancellation support.
// Since the Scraper interface doesn't accept context, we run the search in a goroutine
// and cancel it if the context is cancelled.
func scraperSearchWithContext(ctx context.Context, scraper models.Scraper, id string) (*models.ScraperResult, error) {
	type result struct {
		scraperResult *models.ScraperResult
		err           error
	}

	resultCh := make(chan result, 1)

	// Run scraper.Search() in a goroutine
	go func() {
		scraperResult, err := scraper.Search(id)
		resultCh <- result{scraperResult, err}
	}()

	// Wait for either result or context cancellation
	select {
	case <-ctx.Done():
		// Context cancelled - scraper goroutine will continue but result will be ignored
		return nil, ctx.Err()
	case res := <-resultCh:
		return res.scraperResult, res.err
	}
}

func scraperListContains(scrapers []string, target string) bool {
	for _, scraper := range scrapers {
		if strings.EqualFold(strings.TrimSpace(scraper), target) {
			return true
		}
	}
	return false
}

func appendUniqueInput(inputs []string, input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return inputs
	}
	for _, existing := range inputs {
		if strings.EqualFold(existing, input) {
			return inputs
		}
	}
	return append(inputs, input)
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
//   - updateMode: If true, merge scraped data with existing NFO file
//   - selectedScrapers: If non-empty, use these scrapers instead of defaults
//   - processedMovieIDs: Map to track which movie IDs have already had posters generated (pass nil to disable tracking)
//   - cfg: Config for NFO path construction (required if updateMode is true)
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
	fileMatcher *matcher.Matcher,
	httpClient httpclientiface.HTTPClient,
	userAgent string,
	referer string,
	force bool,
	updateMode bool,
	selectedScrapers []string,
	processedMovieIDs map[string]bool,
	cfg *config.Config,
	scalarStrategy string,
	arrayStrategy string,
) (*models.Movie, *FileResult, error) {
	logging.Debugf("[Batch %s] Starting scrape for file %d: %s (force=%v, customScrapers=%v, queryOverride=%s)",
		job.ID, fileIndex, filePath, force, selectedScrapers, queryOverride)

	startTime := time.Now()

	// Step 1: Determine the query (use queryOverride if provided, otherwise extract from filename)
	var movieID string
	var rawFilenameQuery string
	var matchResultPtr *matcher.MatchResult // Store full match result for multi-part info
	matcherMissFallback := false

	if queryOverride != "" {
		movieID = queryOverride
		matchResultPtr = nil // No match result when using manual override
		logging.Debugf("[Batch %s] File %d: Using manual search query: %s", job.ID, fileIndex, movieID)
	} else {
		// Extract ID from filename using matcher
		fileInfo := scanner.FileInfo{
			Path:      filePath,
			Name:      filepath.Base(filePath),
			Extension: filepath.Ext(filePath),
			Dir:       filepath.Dir(filePath),
		}
		rawFilenameQuery = strings.TrimSpace(strings.TrimSuffix(fileInfo.Name, fileInfo.Extension))

		matchResults := fileMatcher.Match([]scanner.FileInfo{fileInfo})
		if len(matchResults) == 0 {
			// Matcher couldn't extract a standard ID. Fall back to using the raw
			// filename (without extension) so scraper-specific query resolvers can
			// route non-standard formats to the right scraper.
			movieID = rawFilenameQuery
			if movieID == "" {
				errMsg := "could not extract movie ID from filename"
				logging.Debugf("[Batch %s] File %d: %s", job.ID, fileIndex, errMsg)

				return nil, &FileResult{
					FilePath:  filePath,
					Status:    JobStatusFailed,
					Error:     errMsg,
					StartedAt: startTime,
				}, errors.New(errMsg)
			}
			matcherMissFallback = true
			matchResultPtr = nil
			logging.Debugf("[Batch %s] File %d: Matcher could not extract ID, using raw filename query: %s",
				job.ID, fileIndex, movieID)
		} else {
			// Store pointer to match result for later use
			matchResultPtr = &matchResults[0]
			movieID = matchResultPtr.ID
			logging.Debugf("[Batch %s] File %d: Extracted movie ID: %s", job.ID, fileIndex, movieID)
		}
	}

	// Step 2: Check cache (unless force or custom scrapers)
	usingCustomScrapers := len(selectedScrapers) > 0
	skipCache := force || usingCustomScrapers || matcherMissFallback

	if !skipCache {
		logging.Debugf("[Batch %s] File %d: Checking cache for %s", job.ID, fileIndex, movieID)
		if cached, err := movieRepo.FindByID(movieID); err == nil {
			logging.Debugf("[Batch %s] File %d: Found %s in cache (Title=%s, Maker=%s)",
				job.ID, fileIndex, movieID, cached.Title, cached.Maker)

			// IMPORTANT: Generate temp poster for review page even when using cache
			// This ensures multi-part files (CD1/CD2/CD3) all have accessible posters
			// Without this, cached results would have 404 poster URLs
			var posterErr *string
			if httpClient != nil {
				shouldGenerate := true

				// If processedMovieIDs tracking is enabled, check if we've already generated this poster
				if processedMovieIDs != nil {
					processedMovieIDsMutex.Lock()
					shouldGenerate = !processedMovieIDs[cached.ID]
					if shouldGenerate {
						processedMovieIDs[cached.ID] = true
					}
					processedMovieIDsMutex.Unlock()
				}

				if shouldGenerate {
					if tempPosterURL, err := GenerateTempPoster(ctx, job.ID, cached, httpClient, userAgent, referer, downloader.ResolveMediaReferer); err != nil {
						logging.Warnf("[Batch %s] File %d: Failed to create temp poster for cached movie: %v", job.ID, fileIndex, err)
						errMsg := err.Error()
						posterErr = &errMsg
					} else {
						cached.CroppedPosterURL = tempPosterURL
					}
				} else {
					// Check if temp poster file exists (may have been cleaned up after previous job)
					tempPosterPath := filepath.Join("data", "temp", "posters", job.ID, cached.ID+".jpg")
					if _, err := os.Stat(tempPosterPath); err != nil {
						// Temp poster doesn't exist - regenerate it
						logging.Debugf("[Batch %s] File %d: Temp poster missing for %s, regenerating", job.ID, fileIndex, cached.ID)
						if tempPosterURL, err := GenerateTempPoster(ctx, job.ID, cached, httpClient, userAgent, referer, downloader.ResolveMediaReferer); err != nil {
							logging.Warnf("[Batch %s] File %d: Failed to regenerate temp poster: %v", job.ID, fileIndex, err)
							errMsg := err.Error()
							posterErr = &errMsg
						} else {
							cached.CroppedPosterURL = tempPosterURL
						}
					} else {
						// Reuse already-generated temp poster URL for multi-part files
						cached.CroppedPosterURL = fmt.Sprintf("/api/v1/temp/posters/%s/%s.jpg", job.ID, cached.ID)
					}
				}
			}

			// CACHE HIT NFO MERGE: Merge cached data with existing NFO if updateMode is true
			movieToReturn := cached
			fieldSources := buildFieldSourcesFromCachedMovie(cached)
			actressSources := buildActressSourcesFromCachedMovie(cached)
			if updateMode && cfg != nil {
				logging.Debugf("[Batch %s] File %d: Update mode enabled with cache hit, checking for existing NFO to merge", job.ID, fileIndex)

				// Construct expected NFO path (same logic as fresh scrape path)
				sourceDir := filepath.Dir(filePath)
				tmplCtx := template.NewContextFromMovie(cached)
				tmplCtx.GroupActress = cfg.Output.GroupActress

				// Detect part suffix for multi-part files
				if cfg.Metadata.NFO.PerFile && matchResultPtr != nil && matchResultPtr.IsMultiPart {
					tmplCtx.PartNumber = matchResultPtr.PartNumber
					tmplCtx.PartSuffix = matchResultPtr.PartSuffix
					tmplCtx.IsMultiPart = true
				}

				// Generate expected NFO filename using template
				templateEngine := template.NewEngine()
				nfoFilename, err := templateEngine.Execute(cfg.Metadata.NFO.FilenameTemplate, tmplCtx)
				if err != nil {
					logging.Warnf("[Batch %s] File %d: Failed to execute NFO filename template: %v, using default", job.ID, fileIndex, err)
					sanitized := template.SanitizeFilename(cached.ID)
					if sanitized == "" {
						sanitized = "metadata"
					}
					nfoFilename = sanitized + ".nfo"
				} else {
					basename := nfoFilename
					lower := strings.ToLower(basename)
					if strings.HasSuffix(lower, ".nfo") {
						basename = basename[:len(basename)-4]
					}
					sanitized := template.SanitizeFilename(basename)
					if sanitized == "" {
						sanitized = template.SanitizeFilename(cached.ID)
						if sanitized == "" {
							sanitized = "metadata"
						}
					}
					nfoFilename = sanitized + ".nfo"
				}

				nfoPath := filepath.Join(sourceDir, nfoFilename)

				// Also try legacy paths
				legacyPaths := []string{}
				if nfoFilename != cached.ID+".nfo" {
					legacyPaths = append(legacyPaths, filepath.Join(sourceDir, cached.ID+".nfo"))
				}
				if cfg.Metadata.NFO.PerFile && matchResultPtr != nil && matchResultPtr.IsMultiPart {
					videoName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
					videoNFO := filepath.Join(sourceDir, videoName+".nfo")
					if videoNFO != nfoPath {
						legacyPaths = append(legacyPaths, videoNFO)
					}
				}

				// Check if NFO exists
				foundPath := ""
				if _, err := os.Stat(nfoPath); err == nil {
					foundPath = nfoPath
				} else {
					for _, legacyPath := range legacyPaths {
						if _, err := os.Stat(legacyPath); err == nil {
							foundPath = legacyPath
							logging.Debugf("[Batch %s] File %d: Found NFO at legacy path: %s", job.ID, fileIndex, legacyPath)
							break
						}
					}
				}

				if foundPath != "" {
					// NFO exists - parse and merge with cached data
					logging.Infof("[Batch %s] File %d: Found existing NFO, merging with cached data: %s", job.ID, fileIndex, foundPath)
					logging.Infof("[Batch %s] File %d: *** DEBUG *** Scalar=%s Array=%s", job.ID, fileIndex, scalarStrategy, arrayStrategy)

					parseResult, err := nfo.ParseNFO(afero.NewOsFs(), foundPath)
					if err != nil {
						logging.Warnf("[Batch %s] File %d: Failed to parse existing NFO %s: %v (will use cached data only)", job.ID, fileIndex, foundPath, err)
					} else {
						// Merge with user-selected strategies for Update Mode
						scalar := nfo.ParseScalarStrategy(scalarStrategy)
						mergeArrays := nfo.ParseArrayStrategy(arrayStrategy)
						logging.Debugf("[Batch %s] File %d: Parsed scalar strategy: %v (from string: %s)", job.ID, fileIndex, scalar, scalarStrategy)
						mergeResult, err := nfo.MergeMovieMetadataWithOptions(cached, parseResult.Movie, scalar, mergeArrays)
						if err != nil {
							logging.Warnf("[Batch %s] File %d: Failed to merge NFO data for %s: %v (using cached data only)", job.ID, fileIndex, cached.ID, err)
						} else {
							movieToReturn = mergeResult.Merged
							fieldSources = applyNFOMergeProvenance(fieldSources, mergeResult.Provenance)
							actressSources = applyActressMergeProvenance(actressSources, mergeResult.Provenance, movieToReturn.Actresses)
							logging.Infof("[Batch %s] File %d: NFO merge complete for cached %s: %d from cache, %d from NFO, %d conflicts resolved",
								job.ID, fileIndex, cached.ID, mergeResult.Stats.FromScraper, mergeResult.Stats.FromNFO, mergeResult.Stats.ConflictsResolved)

							// Handle DisplayName: if Title is already templated (starts with [ID]), use it directly
							// Otherwise, regenerate DisplayName from Title using the template
							titleAlreadyTemplated := strings.HasPrefix(movieToReturn.Title, "["+movieToReturn.ID+"]")
							if titleAlreadyTemplated {
								// Title already has template applied, use it directly
								movieToReturn.DisplayName = movieToReturn.Title
								logging.Debugf("[Batch %s] File %d: Using templated Title as DisplayName: %s", job.ID, fileIndex, movieToReturn.DisplayName)
							} else if cfg != nil && cfg.Metadata.NFO.DisplayName != "" {
								// Title not templated, regenerate DisplayName from Title
								tmplEngine := template.NewEngine()
								ctx := template.NewContextFromMovie(movieToReturn)
								if displayName, err := tmplEngine.Execute(cfg.Metadata.NFO.DisplayName, ctx); err == nil {
									movieToReturn.DisplayName = displayName
									logging.Debugf("[Batch %s] File %d: Regenerated DisplayName from Title: %s", job.ID, fileIndex, displayName)
								}
							}
						}
					}
				} else {
					logging.Debugf("[Batch %s] File %d: No existing NFO found, using cached data only", job.ID, fileIndex)
				}
			}

			now := time.Now()
			fileResult := &FileResult{
				FilePath:       filePath,
				MovieID:        movieID,
				Status:         JobStatusCompleted,
				Data:           movieToReturn,
				FieldSources:   fieldSources,
				ActressSources: actressSources,
				PosterError:    posterErr,
				StartedAt:      startTime,
				EndedAt:        &now,
			}

			// Populate multi-part fields (only valid if not using query override)
			if matchResultPtr != nil {
				fileResult.IsMultiPart = matchResultPtr.IsMultiPart
				fileResult.PartNumber = matchResultPtr.PartNumber
				fileResult.PartSuffix = matchResultPtr.PartSuffix
			}

			return movieToReturn, fileResult, nil
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
	shouldResolveDMMContentID := queryOverride == "" && !matcherMissFallback &&
		(len(selectedScrapers) == 0 || scraperListContains(selectedScrapers, "dmm"))
	if shouldResolveDMMContentID {
		if dmmScraper, exists := registry.Get("dmm"); exists {
			if !dmmScraper.IsEnabled() {
				logging.Debugf("[Batch %s] File %d: DMM scraper disabled, skipping content-ID resolution", job.ID, fileIndex)
				resolvedID = movieID
			} else if dmmScraperTyped, ok := dmmScraper.(*dmm.Scraper); ok {
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
		// Manual query OR custom scraper set that does not include DMM.
		if queryOverride == "" && len(selectedScrapers) > 0 && !scraperListContains(selectedScrapers, "dmm") {
			logging.Debugf("[Batch %s] File %d: Skipping DMM content-ID resolution (DMM not selected in custom scrapers)", job.ID, fileIndex)
		}
		resolvedID = movieID
	}

	// Step 4: Query scrapers (use selectedScrapers if provided, otherwise use registry defaults)
	results := make([]*models.ScraperResult, 0)
	scraperFailures := make([]scraperFailure, 0)

	// Normalize empty slice to nil for explicit GetByPriority semantics
	var scraperNames []string
	if len(selectedScrapers) > 0 {
		scraperNames = selectedScrapers
		logging.Debugf("[Batch %s] File %d: Using custom scraper priority: %v", job.ID, fileIndex, selectedScrapers)
	} else {
		scraperNames = nil // Explicitly pass nil to use registry defaults
		logging.Debugf("[Batch %s] File %d: Using default scraper priority", job.ID, fileIndex)
	}

	// GetByPriority returns all enabled scrapers when passed nil
	priorityInput := movieID
	if rawFilenameQuery != "" {
		priorityInput = rawFilenameQuery
	}
	scrapersToUse := registry.GetByPriorityForInput(scraperNames, priorityInput)

	// If matcher fallback is active, route only to scrapers that explicitly
	// claim support for this input via ScraperQueryResolver.
	if matcherMissFallback {
		matchedScrapers := make([]models.Scraper, 0, len(scrapersToUse))
		for _, scraper := range scrapersToUse {
			if _, ok := resolveScraperQueryForInputs(scraper, rawFilenameQuery, movieID); ok {
				matchedScrapers = append(matchedScrapers, scraper)
			}
		}

		if len(matchedScrapers) == 0 {
			errMsg := fmt.Sprintf("No scraper query resolver matched filename input: %s", movieID)
			logging.Debugf("[Batch %s] File %d: %s", job.ID, fileIndex, errMsg)
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

		scrapersToUse = matchedScrapers
		logging.Debugf("[Batch %s] File %d: Routed filename input %s to resolver-matched scrapers: %d",
			job.ID, fileIndex, movieID, len(scrapersToUse))
	}
	logging.Debugf("[Batch %s] File %d: Resolved to %d scrapers", job.ID, fileIndex, len(scrapersToUse))

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

		scraperQuery := resolvedID
		if mappedQuery, ok := resolveScraperQueryForInputs(scraper, rawFilenameQuery, movieID, resolvedID); ok {
			scraperQuery = mappedQuery
		}

		logging.Debugf("[Batch %s] File %d: Querying scraper %s for %s", job.ID, fileIndex, scraper.Name(), scraperQuery)
		scraperResult, err := scraperSearchWithContext(ctx, scraper, scraperQuery)
		if err != nil {
			// Check if error is due to context cancellation
			if err == ctx.Err() {
				logging.Debugf("[Batch %s] File %d: Context cancelled during scraper %s", job.ID, fileIndex, scraper.Name())
				now := time.Now()
				return nil, &FileResult{
					FilePath:  filePath,
					MovieID:   movieID,
					Status:    JobStatusCancelled,
					Error:     "Cancelled by user",
					StartedAt: startTime,
					EndedAt:   &now,
				}, ctx.Err()
			}

			logging.Debugf("[Batch %s] File %d: Scraper %s failed: %v", job.ID, fileIndex, scraper.Name(), err)

			// If scraping with resolved ID fails, try with original ID before giving up
			// (but only if we're not using a manual query override)
			if scraperQuery != movieID && queryOverride == "" {
				logging.Debugf("[Batch %s] File %d: Retrying scraper %s with original ID: %s",
					job.ID, fileIndex, scraper.Name(), movieID)
				scraperResult, err = scraperSearchWithContext(ctx, scraper, movieID)
				if err != nil {
					// Check if error is due to context cancellation
					if err == ctx.Err() {
						logging.Debugf("[Batch %s] File %d: Context cancelled during scraper %s retry", job.ID, fileIndex, scraper.Name())
						now := time.Now()
						return nil, &FileResult{
							FilePath:  filePath,
							MovieID:   movieID,
							Status:    JobStatusCancelled,
							Error:     "Cancelled by user",
							StartedAt: startTime,
							EndedAt:   &now,
						}, ctx.Err()
					}

					logging.Debugf("[Batch %s] File %d: Scraper %s failed with original ID: %v",
						job.ID, fileIndex, scraper.Name(), err)
					scraperFailures = append(scraperFailures, scraperFailure{
						Scraper: scraper.Name(),
						Err:     err,
					})
					continue
				}
			} else {
				scraperFailures = append(scraperFailures, scraperFailure{
					Scraper: scraper.Name(),
					Err:     err,
				})
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
		errMsg := buildScraperNoResultsError(scraperFailures)
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

	// Track which scraper won each field for frontend debugging display.
	fieldSources := buildFieldSourcesFromScrapeResults(results, agg.GetResolvedPriorities(), selectedScrapers)
	actressSources := buildActressSourcesFromScrapeResults(results, agg.GetResolvedPriorities(), selectedScrapers, movie.Actresses)

	// Set original filename for tracking
	movie.OriginalFileName = filepath.Base(filePath)

	// Step 6.5: NFO Merge (if updateMode is true)
	if updateMode && cfg != nil {
		logging.Debugf("[Batch %s] File %d: Update mode enabled, checking for existing NFO to merge", job.ID, fileIndex)

		// Construct expected NFO path (similar to processUpdateMode logic)
		sourceDir := filepath.Dir(filePath)
		tmplCtx := template.NewContextFromMovie(movie)
		tmplCtx.GroupActress = cfg.Output.GroupActress

		// Detect part suffix for multi-part files
		if cfg.Metadata.NFO.PerFile && matchResultPtr != nil && matchResultPtr.IsMultiPart {
			tmplCtx.PartNumber = matchResultPtr.PartNumber
			tmplCtx.PartSuffix = matchResultPtr.PartSuffix
			tmplCtx.IsMultiPart = true
		}

		// Generate expected NFO filename using template
		templateEngine := template.NewEngine()
		nfoFilename, err := templateEngine.Execute(cfg.Metadata.NFO.FilenameTemplate, tmplCtx)
		if err != nil {
			// Fall back to default naming
			logging.Warnf("[Batch %s] File %d: Failed to execute NFO filename template: %v, using default", job.ID, fileIndex, err)
			sanitized := template.SanitizeFilename(movie.ID)
			if sanitized == "" {
				sanitized = "metadata"
			}
			nfoFilename = sanitized + ".nfo"
		} else {
			// Sanitize and ensure .nfo extension
			basename := nfoFilename
			lower := strings.ToLower(basename)
			if strings.HasSuffix(lower, ".nfo") {
				basename = basename[:len(basename)-4]
			}
			sanitized := template.SanitizeFilename(basename)
			if sanitized == "" {
				sanitized = template.SanitizeFilename(movie.ID)
				if sanitized == "" {
					sanitized = "metadata"
				}
			}
			nfoFilename = sanitized + ".nfo"
		}

		nfoPath := filepath.Join(sourceDir, nfoFilename)

		// Also try legacy paths for backward compatibility
		legacyPaths := []string{}
		if nfoFilename != movie.ID+".nfo" {
			legacyPaths = append(legacyPaths, filepath.Join(sourceDir, movie.ID+".nfo"))
		}
		if cfg.Metadata.NFO.PerFile && matchResultPtr != nil && matchResultPtr.IsMultiPart {
			videoName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
			videoNFO := filepath.Join(sourceDir, videoName+".nfo")
			if videoNFO != nfoPath {
				legacyPaths = append(legacyPaths, videoNFO)
			}
		}

		// Check if NFO exists (try template path first, then legacy)
		foundPath := ""
		if _, err := os.Stat(nfoPath); err == nil {
			foundPath = nfoPath
		} else {
			for _, legacyPath := range legacyPaths {
				if _, err := os.Stat(legacyPath); err == nil {
					foundPath = legacyPath
					logging.Debugf("[Batch %s] File %d: Found NFO at legacy path: %s", job.ID, fileIndex, legacyPath)
					break
				}
			}
		}

		if foundPath != "" {
			// NFO exists - parse and merge
			logging.Infof("[Batch %s] File %d: Found existing NFO, merging data: %s", job.ID, fileIndex, foundPath)

			parseResult, err := nfo.ParseNFO(afero.NewOsFs(), foundPath)
			if err != nil {
				logging.Warnf("[Batch %s] File %d: Failed to parse existing NFO %s: %v (will use scraper data only)", job.ID, fileIndex, foundPath, err)
			} else {
				// Merge with user-selected strategies for Update Mode
				scalar := nfo.ParseScalarStrategy(scalarStrategy)
				mergeArrays := nfo.ParseArrayStrategy(arrayStrategy)
				mergeResult, err := nfo.MergeMovieMetadataWithOptions(movie, parseResult.Movie, scalar, mergeArrays)
				if err != nil {
					logging.Warnf("[Batch %s] File %d: Failed to merge NFO data for %s: %v (using scraper data only)", job.ID, fileIndex, movie.ID, err)
				} else {
					movie = mergeResult.Merged
					fieldSources = applyNFOMergeProvenance(fieldSources, mergeResult.Provenance)
					actressSources = applyActressMergeProvenance(actressSources, mergeResult.Provenance, movie.Actresses)
					logging.Infof("[Batch %s] File %d: NFO merge complete for %s: %d from scraper, %d from NFO, %d conflicts resolved",
						job.ID, fileIndex, movie.ID, mergeResult.Stats.FromScraper, mergeResult.Stats.FromNFO, mergeResult.Stats.ConflictsResolved)

					// Handle DisplayName: if Title is already templated (starts with [ID]), use it directly
					// Otherwise, regenerate DisplayName from Title using the template
					titleAlreadyTemplated := strings.HasPrefix(movie.Title, "["+movie.ID+"]")
					if titleAlreadyTemplated {
						// Title already has template applied, use it directly
						movie.DisplayName = movie.Title
						logging.Debugf("[Batch %s] File %d: Using templated Title as DisplayName: %s", job.ID, fileIndex, movie.DisplayName)
					} else if cfg != nil && cfg.Metadata.NFO.DisplayName != "" {
						// Title not templated, regenerate DisplayName from Title
						tmplEngine := template.NewEngine()
						ctx := template.NewContextFromMovie(movie)
						if displayName, err := tmplEngine.Execute(cfg.Metadata.NFO.DisplayName, ctx); err == nil {
							movie.DisplayName = displayName
							logging.Debugf("[Batch %s] File %d: Regenerated DisplayName from Title: %s", job.ID, fileIndex, displayName)
						}
					}
				}
			}
		} else {
			logging.Debugf("[Batch %s] File %d: No existing NFO found, using scraper data only", job.ID, fileIndex)
		}
	}

	// Step 7: Download and crop poster temporarily for review page
	// Skip if we've already processed this movie ID (for multi-part files)
	var posterErr *string
	if httpClient != nil {
		shouldGeneratePoster := true

		// Check if we've already generated a poster for this movie ID (thread-safe)
		if processedMovieIDs != nil {
			processedMovieIDsMutex.Lock()
			if processedMovieIDs[movie.ID] {
				shouldGeneratePoster = false
				logging.Debugf("[Batch %s] File %d: Skipping poster generation for %s (already processed for multi-part file)",
					job.ID, fileIndex, movie.ID)
			} else {
				// Mark this movie ID as processed
				processedMovieIDs[movie.ID] = true
			}
			processedMovieIDsMutex.Unlock()
		}

		if shouldGeneratePoster {
			if tempPosterURL, err := GenerateTempPoster(ctx, job.ID, movie, httpClient, userAgent, referer, downloader.ResolveMediaReferer); err != nil {
				logging.Warnf("[Batch %s] File %d: Failed to create temp poster: %v (continuing anyway)", job.ID, fileIndex, err)
				errMsg := err.Error()
				posterErr = &errMsg
				// Continue - temp poster is optional for review
			} else {
				// Set the temp poster URL so frontend can display it
				movie.CroppedPosterURL = tempPosterURL
			}
		} else {
			// For multi-part files that skip generation, set the temp poster URL to the already-generated one
			// This ensures all parts of a multi-part file show the same poster in the review page
			movie.CroppedPosterURL = fmt.Sprintf("/api/v1/temp/posters/%s/%s.jpg", job.ID, movie.ID)
		}
	}

	// Step 8: Save to database (KEEP THIS - Option A: maintain consistency with batch scraping)
	// We save immediately even though organize hasn't happened yet
	if !usingCustomScrapers {
		logging.Debugf("[Batch %s] File %d: Saving metadata to database", job.ID, fileIndex)

		// IMPORTANT: Don't save temp poster URLs to database
		// Temp posters are ephemeral and cleaned up after job completion
		// Only persistent poster URLs (created during organize workflow) should be stored in the database
		tempPosterURL := movie.CroppedPosterURL
		movie.CroppedPosterURL = "" // Clear temp URL before saving

		if err := movieRepo.Upsert(movie); err != nil {
			logging.Errorf("[Batch %s] File %d: Database save failed: %v", job.ID, fileIndex, err)
			// Continue anyway - we have the data
		} else {
			logging.Debugf("[Batch %s] File %d: Successfully saved to database", job.ID, fileIndex)
		}

		// Restore temp URL for the FileResult (needed for review page display)
		movie.CroppedPosterURL = tempPosterURL
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
			// Preserve temp poster URL from Step 7 (DB should never have temp URLs)
			finalMovie.CroppedPosterURL = movie.CroppedPosterURL
			logging.Debugf("[Batch %s] File %d: Reloaded movie from database with associations", job.ID, fileIndex)
		}
	} else {
		// Custom scraper mode: Use aggregated movie directly without database reload.
		// This is intentional - custom scraper results are temporary (not persisted to DB)
		// and the Movie object contains all necessary data from the aggregator.
		// Note: ORM associations (Actresses, Genres) won't be lazily loaded since the
		// movie is not from the database, but all data is already populated by the aggregator.
		finalMovie = movie
	}

	// Step 10: Create completed FileResult (caller will update job state)
	now := time.Now()
	fileResult := &FileResult{
		FilePath:       filePath,
		MovieID:        movieID,
		Status:         JobStatusCompleted,
		Data:           finalMovie,
		FieldSources:   fieldSources,
		ActressSources: actressSources,
		PosterError:    posterErr,
		StartedAt:      startTime,
		EndedAt:        &now,
	}

	// Populate multi-part fields (only valid if not using query override)
	if matchResultPtr != nil {
		fileResult.IsMultiPart = matchResultPtr.IsMultiPart
		fileResult.PartNumber = matchResultPtr.PartNumber
		fileResult.PartSuffix = matchResultPtr.PartSuffix
	}

	logging.Debugf("[Batch %s] File %d: Scrape completed successfully", job.ID, fileIndex)

	return finalMovie, fileResult, nil
}
