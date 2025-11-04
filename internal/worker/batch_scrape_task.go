package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/database"
	imageutil "github.com/javinizer/javinizer-go/internal/image"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
)

// BatchScrapeTask represents a task for scraping metadata for a single file in a batch operation
type BatchScrapeTask struct {
	BaseTask
	filePath         string
	fileIndex        int
	job              *BatchJob
	registry         *models.ScraperRegistry
	aggregator       *aggregator.Aggregator
	movieRepo        *database.MovieRepository
	matcher          *matcher.Matcher
	progressTracker  *ProgressTracker
	force            bool
	selectedScrapers []string // empty = use default
}

// NewBatchScrapeTask creates a new batch scrape task
func NewBatchScrapeTask(
	taskID string,
	filePath string,
	fileIndex int,
	job *BatchJob,
	registry *models.ScraperRegistry,
	agg *aggregator.Aggregator,
	movieRepo *database.MovieRepository,
	mat *matcher.Matcher,
	progressTracker *ProgressTracker,
	force bool,
	selectedScrapers []string,
) *BatchScrapeTask {
	desc := fmt.Sprintf("Scraping metadata for %s", filepath.Base(filePath))

	return &BatchScrapeTask{
		BaseTask: BaseTask{
			id:          taskID,
			taskType:    TaskTypeBatchScrape,
			description: desc,
		},
		filePath:         filePath,
		fileIndex:        fileIndex,
		job:              job,
		registry:         registry,
		aggregator:       agg,
		movieRepo:        movieRepo,
		matcher:          mat,
		progressTracker:  progressTracker,
		force:            force,
		selectedScrapers: selectedScrapers,
	}
}

// Execute implements the Task interface
func (t *BatchScrapeTask) Execute(ctx context.Context) error {
	logging.Debugf("[Batch %s] Starting scrape task for file %d: %s (force=%v, customScrapers=%v)",
		t.job.ID, t.fileIndex, t.filePath, t.force, t.selectedScrapers)

	// Step 1: Create FileInfo from filePath
	fileInfo := scanner.FileInfo{
		Path:      t.filePath,
		Name:      filepath.Base(t.filePath),
		Extension: filepath.Ext(t.filePath),
		Dir:       filepath.Dir(t.filePath),
	}

	// Step 2: Use matcher to extract movieID
	matchResults := t.matcher.Match([]scanner.FileInfo{fileInfo})
	if len(matchResults) == 0 {
		errMsg := "Could not extract movie ID from filename"
		logging.Debugf("[Batch %s] File %d: %s", t.job.ID, t.fileIndex, errMsg)

		result := &FileResult{
			FilePath:  t.filePath,
			Status:    JobStatusFailed,
			Error:     errMsg,
			StartedAt: time.Now(),
		}
		now := time.Now()
		result.EndedAt = &now
		t.job.UpdateFileResult(t.filePath, result)

		return errors.New(errMsg)
	}

	movieID := matchResults[0].ID
	logging.Debugf("[Batch %s] File %d: Extracted movie ID: %s", t.job.ID, t.fileIndex, movieID)

	// Step 3: Create FileResult and update job status to running
	result := &FileResult{
		FilePath:  t.filePath,
		MovieID:   movieID,
		Status:    JobStatusRunning,
		StartedAt: time.Now(),
	}
	t.job.UpdateFileResult(t.filePath, result)

	// Update progress tracker
	t.progressTracker.Update(t.id, 0.1, fmt.Sprintf("Scraping %s", movieID), 0)

	// Step 4: Check cache (unless force or custom scrapers)
	usingCustomScrapers := len(t.selectedScrapers) > 0
	skipCache := t.force || usingCustomScrapers

	if !skipCache {
		logging.Debugf("[Batch %s] File %d: Checking cache for %s", t.job.ID, t.fileIndex, movieID)
		if cached, err := t.movieRepo.FindByID(movieID); err == nil {
			logging.Debugf("[Batch %s] File %d: Found %s in cache (Title=%s, Maker=%s)",
				t.job.ID, t.fileIndex, movieID, cached.Title, cached.Maker)

			// Create new FileResult to avoid race conditions
			now := time.Now()
			completeResult := &FileResult{
				FilePath:  t.filePath,
				MovieID:   movieID,
				Status:    JobStatusCompleted,
				Data:      cached,
				StartedAt: result.StartedAt,
				EndedAt:   &now,
			}
			t.job.UpdateFileResult(t.filePath, completeResult)

			t.progressTracker.Complete(t.id, "Found in cache")
			return nil
		}
		logging.Debugf("[Batch %s] File %d: %s not found in cache, will scrape", t.job.ID, t.fileIndex, movieID)
	} else if t.force {
		// Clear cache if force refresh
		logging.Debugf("[Batch %s] File %d: Force refresh enabled, clearing cache for %s", t.job.ID, t.fileIndex, movieID)
		if err := t.movieRepo.Delete(movieID); err != nil {
			logging.Debugf("[Batch %s] File %d: Cache delete failed (movie may not exist): %v", t.job.ID, t.fileIndex, err)
		}
	} else if usingCustomScrapers {
		logging.Debugf("[Batch %s] File %d: Custom scrapers specified, bypassing cache", t.job.ID, t.fileIndex)
	}

	// Step 5: Perform DMM content-ID resolution
	var resolvedID string
	if dmmScraper, exists := t.registry.Get("dmm"); exists {
		if dmmScraperTyped, ok := dmmScraper.(*dmm.Scraper); ok {
			contentID, err := dmmScraperTyped.ResolveContentID(movieID)
			if err != nil {
				logging.Debugf("[Batch %s] File %d: DMM content-ID resolution failed for %s: %v, using original ID",
					t.job.ID, t.fileIndex, movieID, err)
				resolvedID = movieID // Fallback to original ID
			} else {
				resolvedID = contentID
				logging.Debugf("[Batch %s] File %d: Resolved content-ID for %s: %s",
					t.job.ID, t.fileIndex, movieID, resolvedID)
			}
		} else {
			logging.Debugf("[Batch %s] File %d: DMM scraper type assertion failed, using original ID", t.job.ID, t.fileIndex)
			resolvedID = movieID
		}
	} else {
		logging.Debugf("[Batch %s] File %d: DMM scraper not available, using original ID", t.job.ID, t.fileIndex)
		resolvedID = movieID
	}

	t.progressTracker.Update(t.id, 0.2, "Querying scrapers...", 0)

	// Step 6: Query scrapers (use selectedScrapers if provided, otherwise default)
	results := make([]*models.ScraperResult, 0)
	scraperErrors := make([]string, 0)

	var scrapersToUse []models.Scraper
	if len(t.selectedScrapers) > 0 {
		scrapersToUse = t.registry.GetByPriority(t.selectedScrapers)
		logging.Debugf("[Batch %s] File %d: Using custom scraper priority: %v (%d scrapers)",
			t.job.ID, t.fileIndex, t.selectedScrapers, len(scrapersToUse))
	} else {
		scrapersToUse = t.registry.GetByPriority([]string{"r18dev", "dmm"})
		logging.Debugf("[Batch %s] File %d: Using default scraper priority (%d scrapers)",
			t.job.ID, t.fileIndex, len(scrapersToUse))
	}

	for i, scraper := range scrapersToUse {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			logging.Debugf("[Batch %s] File %d: Context cancelled", t.job.ID, t.fileIndex)

			// Create new FileResult to avoid race conditions
			now := time.Now()
			cancelledResult := &FileResult{
				FilePath:  t.filePath,
				MovieID:   movieID,
				Status:    JobStatusCancelled,
				Error:     "Cancelled by user",
				StartedAt: result.StartedAt,
				EndedAt:   &now,
			}
			t.job.UpdateFileResult(t.filePath, cancelledResult)

			// Notify progress tracker of cancellation
			t.progressTracker.Cancel(t.id)
			return ctx.Err()
		default:
		}

		progress := 0.2 + (float64(i) / float64(len(scrapersToUse)) * 0.5)
		t.progressTracker.Update(t.id, progress, fmt.Sprintf("Scraping from %s...", scraper.Name()), 0)

		logging.Debugf("[Batch %s] File %d: Querying scraper %s for %s", t.job.ID, t.fileIndex, scraper.Name(), resolvedID)
		scraperResult, err := scraper.Search(resolvedID)
		if err != nil {
			logging.Debugf("[Batch %s] File %d: Scraper %s failed: %v", t.job.ID, t.fileIndex, scraper.Name(), err)

			// If scraping with resolved ID fails, try with original ID before giving up
			if resolvedID != movieID {
				logging.Debugf("[Batch %s] File %d: Retrying scraper %s with original ID: %s",
					t.job.ID, t.fileIndex, scraper.Name(), movieID)
				scraperResult, err = scraper.Search(movieID)
				if err != nil {
					logging.Debugf("[Batch %s] File %d: Scraper %s failed with original ID: %v",
						t.job.ID, t.fileIndex, scraper.Name(), err)
					scraperErrors = append(scraperErrors, fmt.Sprintf("%s: %v", scraper.Name(), err))
					continue
				}
			} else {
				scraperErrors = append(scraperErrors, fmt.Sprintf("%s: %v", scraper.Name(), err))
				continue
			}
		}

		logging.Debugf("[Batch %s] File %d: Scraper %s returned: Title=%s, Language=%s, Actresses=%d, Genres=%d",
			t.job.ID, t.fileIndex, scraper.Name(), scraperResult.Title, scraperResult.Language,
			len(scraperResult.Actresses), len(scraperResult.Genres))
		results = append(results, scraperResult)
	}

	// Step 7: Check if any scrapers succeeded
	if len(results) == 0 {
		errMsg := fmt.Sprintf("Movie not found: %s", strings.Join(scraperErrors, "; "))
		logging.Debugf("[Batch %s] File %d: No results from any scraper for %s", t.job.ID, t.fileIndex, movieID)

		// Create new FileResult to avoid race conditions
		now := time.Now()
		failedResult := &FileResult{
			FilePath:  t.filePath,
			MovieID:   movieID,
			Status:    JobStatusFailed,
			Error:     errMsg,
			StartedAt: result.StartedAt,
			EndedAt:   &now,
		}
		t.job.UpdateFileResult(t.filePath, failedResult)

		err := errors.New(errMsg)
		t.progressTracker.Fail(t.id, err)
		return err
	}

	logging.Debugf("[Batch %s] File %d: Collected %d results from scrapers", t.job.ID, t.fileIndex, len(results))

	// Step 8: Aggregate results
	t.progressTracker.Update(t.id, 0.8, "Aggregating metadata...", 0)
	logging.Debugf("[Batch %s] File %d: Starting metadata aggregation", t.job.ID, t.fileIndex)

	movie, err := t.aggregator.Aggregate(results)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to aggregate: %v", err)
		logging.Debugf("[Batch %s] File %d: Aggregation failed: %v", t.job.ID, t.fileIndex, err)

		// Create new FileResult to avoid race conditions
		now := time.Now()
		failedResult := &FileResult{
			FilePath:  t.filePath,
			MovieID:   movieID,
			Status:    JobStatusFailed,
			Error:     errMsg,
			StartedAt: result.StartedAt,
			EndedAt:   &now,
		}
		t.job.UpdateFileResult(t.filePath, failedResult)

		aggErr := errors.New(errMsg)
		t.progressTracker.Fail(t.id, aggErr)
		return aggErr
	}

	logging.Debugf("[Batch %s] File %d: Aggregation complete - Title: %s, Maker: %s, Actresses: %d, Genres: %d",
		t.job.ID, t.fileIndex, movie.Title, movie.Maker, len(movie.Actresses), len(movie.Genres))

	// Set original filename for tracking
	movie.OriginalFileName = filepath.Base(t.filePath)

	// Step 8.5: Download and crop poster temporarily for review page
	if err := t.downloadTempPoster(ctx, movie); err != nil {
		logging.Warnf("[Batch %s] File %d: Failed to create temp poster: %v (continuing anyway)", t.job.ID, t.fileIndex, err)
		// Continue - temp poster is optional for review
	}

	// Step 9: Save to database (skip if using custom scrapers)
	if !usingCustomScrapers {
		t.progressTracker.Update(t.id, 0.9, "Saving to database...", 0)
		logging.Debugf("[Batch %s] File %d: Saving metadata to database", t.job.ID, t.fileIndex)

		if err := t.movieRepo.Upsert(movie); err != nil {
			logging.Errorf("[Batch %s] File %d: Database save failed: %v", t.job.ID, t.fileIndex, err)
			// Continue anyway - we have the data
		} else {
			logging.Debugf("[Batch %s] File %d: Successfully saved to database", t.job.ID, t.fileIndex)
		}
	} else {
		logging.Debugf("[Batch %s] File %d: Skipping database save (custom scrapers used)", t.job.ID, t.fileIndex)
	}

	// Step 10: Reload from database to get associations (only if saved)
	var finalMovie *models.Movie
	if !usingCustomScrapers {
		reloadedMovie, err := t.movieRepo.FindByID(movie.ID)
		if err != nil {
			logging.Debugf("[Batch %s] File %d: Failed to reload movie from database: %v", t.job.ID, t.fileIndex, err)
			finalMovie = movie // Fallback to aggregated movie
		} else {
			finalMovie = reloadedMovie
			// Preserve DisplayName from aggregated movie (DB may have stale/empty value)
			if movie.DisplayName != "" {
				finalMovie.DisplayName = movie.DisplayName
			}
			logging.Debugf("[Batch %s] File %d: Reloaded movie from database with associations", t.job.ID, t.fileIndex)
		}
	} else {
		finalMovie = movie
	}

	// Step 11: Update job with completed FileResult containing movie data
	// Create new FileResult to avoid race conditions
	now := time.Now()
	successResult := &FileResult{
		FilePath:  t.filePath,
		MovieID:   movieID,
		Status:    JobStatusCompleted,
		Data:      finalMovie,
		StartedAt: result.StartedAt,
		EndedAt:   &now,
	}
	t.job.UpdateFileResult(t.filePath, successResult)

	t.progressTracker.Complete(t.id, fmt.Sprintf("Scraped %s successfully", movieID))
	logging.Debugf("[Batch %s] File %d: Task completed successfully", t.job.ID, t.fileIndex)

	return nil
}

// downloadTempPoster downloads and crops the poster temporarily for the review page
// Updates movie.PosterURL to point to the local temp file path
func (t *BatchScrapeTask) downloadTempPoster(ctx context.Context, movie *models.Movie) error {
	// Determine poster URL to download and save original URL
	originalPosterURL := movie.PosterURL
	if originalPosterURL == "" {
		originalPosterURL = movie.CoverURL
	}
	if originalPosterURL == "" {
		return fmt.Errorf("no poster or cover URL available")
	}

	// Create temp directory: data/temp/posters/{job_id}/
	tempDir := filepath.Join("data", "temp", "posters", t.job.ID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp poster directory: %w", err)
	}

	// Download poster to temp location
	tempFullPath := filepath.Join(tempDir, fmt.Sprintf("%s-full.jpg", movie.ID))
	tempCroppedPath := filepath.Join(tempDir, fmt.Sprintf("%s.jpg", movie.ID))

	// Download the poster
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", originalPosterURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Javinizer (+https://github.com/javinizer/Javinizer)")
	req.Header.Set("Referer", "https://www.dmm.co.jp/")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download poster: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("poster download failed with status %d", resp.StatusCode)
	}

	// Save to temp file
	outFile, err := os.Create(tempFullPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	_, err = io.Copy(outFile, resp.Body)
	outFile.Close()
	if err != nil {
		os.Remove(tempFullPath)
		return fmt.Errorf("failed to write poster: %w", err)
	}

	// Crop the poster using the smart cropping algorithm
	if err := imageutil.CropPosterFromCover(tempFullPath, tempCroppedPath); err != nil {
		os.Remove(tempFullPath)
		return fmt.Errorf("failed to crop poster: %w", err)
	}

	// Clean up the full image
	os.Remove(tempFullPath)

	// CRITICAL: Store temp poster path in a separate field
	// We MUST NOT overwrite movie.PosterURL because:
	// 1. NFO files need the original URL
	// 2. Database needs the original URL
	// 3. Update mode needs to know which file to use for download
	//
	// Instead, we create a response-only field for the frontend review page
	// The frontend will construct the URL: /api/v1/temp/posters/{job_id}/{movie_id}.jpg
	// This is handled in the BatchJobResponse serialization

	// Keep original PosterURL intact - it's needed for NFO/database
	// movie.PosterURL = originalPosterURL (already set, no change needed)

	// Set ShouldCropPoster to false because the temp image is already cropped
	// Frontend will display it normally without CSS cropping (min-width: 211.8%)
	movie.ShouldCropPoster = false

	logging.Debugf("[Batch %s] File %d: Created temp cropped poster at %s for review (original URL preserved: %s)",
		t.job.ID, t.fileIndex, tempCroppedPath, originalPosterURL)

	return nil
}
