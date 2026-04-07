package batch

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// rescrapeBatchMovie godoc
// @Summary Rescrape a single movie within a batch job
// @Description Rescrape a movie with custom scrapers or manual search input, and regenerate temp poster
// @Tags batch
// @Accept json
// @Produce json
// @Param id path string true "Batch Job ID"
// @Param movieId path string true "Movie ID to rescrape"
// @Param request body BatchRescrapeRequest true "Rescrape options"
// @Success 200 {object} BatchRescrapeResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/batch/{id}/movies/{movieId}/rescrape [post]
func rescrapeBatchMovie(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		movieID := c.Param("movieId")

		var req BatchRescrapeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		// Apply preset if specified (overrides individual strategy fields)
		if req.Preset != "" {
			var presetErr error
			req.ScalarStrategy, req.ArrayStrategy, presetErr = nfo.ApplyPreset(req.Preset, req.ScalarStrategy, req.ArrayStrategy)
			if presetErr != nil {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: presetErr.Error()})
				return
			}
			logging.Infof("Applied preset '%s': scalar=%s, array=%s", req.Preset, req.ScalarStrategy, req.ArrayStrategy)
		}

		// Validate request
		if len(req.SelectedScrapers) == 0 && req.ManualSearchInput == "" {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error: "either selected_scrapers or manual_search_input must be provided",
			})
			return
		}

		logging.Infof("Batch rescrape request for job %s, movie %s: scrapers=%v, manual_input=%s, force=%v",
			jobID, movieID, req.SelectedScrapers, req.ManualSearchInput, req.Force)

		// Get the actual batch job from JobQueue (using GetJobPointer for mutations)
		// Note: We use GetJobPointer() instead of GetJob() because we need to modify
		// the real job state, not a snapshot. GetJob() returns a deep copy which would
		// cause our UpdateFileResult() call to only affect the local copy.
		job, ok := deps.JobQueue.GetJobPointer(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "Job not found"})
			return
		}

		// Find the file result with this movie ID
		status := job.GetStatus()
		var foundFilePath string
		var oldMovieID string
		for filePath, result := range status.Results {
			if result.MovieID == movieID {
				foundFilePath = filePath
				// Get the actual movie ID from the Movie object, not the query string
				// Posters are stored as {movie.ID}.jpg, and movie.ID may differ from
				// result.MovieID if ID normalization occurred during scraping
				if result.Data != nil {
					if oldMovie, ok := result.Data.(*models.Movie); ok {
						oldMovieID = oldMovie.ID
					}
				}
				if oldMovieID == "" {
					oldMovieID = result.MovieID
				}
				break
			}
		}

		if foundFilePath == "" {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error: fmt.Sprintf("Movie %s not found in batch job", movieID),
			})
			return
		}

		// Get configuration
		cfg := deps.GetConfig()

		// Create HTTP client for poster downloads with scraper-level download proxy support.
		httpClient, err := downloader.NewHTTPClientForDownloaderWithRegistry(cfg, deps.GetRegistry())
		if err != nil {
			logging.Warnf("Failed to create HTTP client for poster downloads: %v", err)
			httpClient = nil // Continue without poster generation
		}

		// Use RunBatchScrapeOnce to perform the rescrape
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
		defer cancel()

		// Determine query override (for manual search)
		var queryOverride string

		// Parse manual input first if provided
		var parsed *matcher.ParsedInput
		if req.ManualSearchInput != "" {
			var err error
			parsed, err = matcher.ParseInput(req.ManualSearchInput, deps.GetRegistry())
			if err != nil {
				logging.Warnf("Failed to parse manual input '%s': %v, using as-is", req.ManualSearchInput, err)
				queryOverride = strings.TrimSpace(req.ManualSearchInput)
			} else {
				if parsed.IsURL {
					queryOverride = req.ManualSearchInput
					logging.Infof("Manual input is a URL, preserving for direct scraping: %s (extracted ID: %s, scraper hint: %s)", req.ManualSearchInput, parsed.ID, parsed.ScraperHint)
				} else {
					queryOverride = parsed.ID
					logging.Debugf("Manual input is not a URL, using as movie ID: %s", parsed.ID)
				}
			}
		} else {
			// Use movieID as query if no manual input provided
			queryOverride = movieID
		}

		// Determine which scrapers to use:
		// - selectedScrapers: only set when user explicitly selected (triggers custom mode, skips cache)
		// - scraperPriorityOverride: set when URL filtering needed (optimizes scraper order without skipping cache)
		var selectedScrapers []string
		var scraperPriorityOverride []string

		if len(req.SelectedScrapers) > 0 {
			// User explicitly selected scrapers -> custom mode
			selectedScrapers = matcher.CalculateOptimalScrapers(
				req.SelectedScrapers,
				deps.GetConfig().Scrapers.Priority,
				parsed,
			)
		} else if parsed != nil && parsed.IsURL && len(parsed.CompatibleScrapers) > 0 {
			// URL detected -> use priority override for filtering (doesn't skip cache)
			scraperPriorityOverride = matcher.CalculateOptimalScrapers(
				nil,
				deps.GetConfig().Scrapers.Priority,
				parsed,
			)
		}

		// Log scraper selection for debugging
		if parsed != nil && parsed.IsURL && len(parsed.CompatibleScrapers) > 0 {
			if len(req.SelectedScrapers) > 0 {
				logging.Infof("URL provided: filtered scrapers from %v to URL-compatible: %v", req.SelectedScrapers, selectedScrapers)
			} else if parsed.ScraperHint != "" {
				logging.Infof("URL provided: using compatible scrapers with %s prioritized: %v", parsed.ScraperHint, scraperPriorityOverride)
			} else {
				logging.Infof("URL provided: using URL-compatible scrapers: %v", scraperPriorityOverride)
			}
		} else if len(req.SelectedScrapers) > 0 {
			logging.Infof("Using custom scrapers: %v", selectedScrapers)
		}

		// Warn if URL detected but no compatible scrapers
		if parsed != nil && parsed.IsURL && len(parsed.CompatibleScrapers) == 0 {
			logging.Warnf("URL detected but no registered scrapers can handle it. Input may fail to scrape.")
		}

		movie, result, err := worker.RunBatchScrapeOnce(
			ctx,
			job,
			foundFilePath,          // originalFileName (use actual file path from job)
			0,                      // fileIndex (not used for rescrape)
			queryOverride,          // queryOverride for manual search
			deps.GetRegistry(),     // registry
			deps.GetAggregator(),   // aggregator
			deps.MovieRepo,         // movieRepo
			deps.GetMatcher(),      // matcher
			httpClient,             // httpClient for poster generation
			cfg.Scrapers.UserAgent, // userAgent
			cfg.Scrapers.Referer,   // referer
			req.Force,              // force rescrape
			req.Preset != "" || req.ScalarStrategy != "" || req.ArrayStrategy != "", // updateMode - true if preset or either strategy provided
			selectedScrapers,        // selectedScrapers (nil unless user explicitly selected)
			scraperPriorityOverride, // scraperPriorityOverride (for URL filtering, doesn't skip cache)
			nil,                     // processedMovieIDs (nil = no deduplication for single file rescrape)
			cfg,                     // cfg (needed for NFO path construction)
			req.ScalarStrategy,      // scalarStrategy - scalar field merge behavior (prefer-scraper, prefer-nfo)
			req.ArrayStrategy,       // arrayStrategy - array field merge behavior (merge, replace)
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{
				Error: fmt.Sprintf("Rescrape failed: %v", err),
			})
			return
		}

		// HIGH: Check if result is nil (pre-commit review fix)
		if result == nil {
			logging.Errorf("[Rescrape] RunBatchScrapeOnce returned nil result for %s", foundFilePath)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Rescrape produced no result"})
			return
		}

		// MEDIUM: Check if scraping failed (using typed constant instead of string literal)
		if result.Status != worker.JobStatusCompleted {
			errorMsg := "Unknown error"
			if result.Error != "" {
				errorMsg = result.Error
			}
			// MEDIUM: Use 422 Unprocessable Entity instead of 404 for scraping failures
			c.JSON(http.StatusUnprocessableEntity, ErrorResponse{
				Error: fmt.Sprintf("Rescrape failed: %s", errorMsg),
			})
			return
		}

		// Update the job state with the rescrape result (persist the change)
		// Note: RunBatchScrapeOnce doesn't call UpdateFileResult, so we must do it here
		// Using GetJobPointer() above ensures we modify the real job, not a snapshot

		// Preserve multipart metadata from discovery phase (for letter patterns like -A, -B)
		// This prevents losing multipart status when rescraping letter-pattern files
		if info, ok := job.GetFileMatchInfo(foundFilePath); ok {
			result.IsMultiPart = info.IsMultiPart
			result.PartNumber = info.PartNumber
			result.PartSuffix = info.PartSuffix
			logging.Debugf("[Rescrape] Applied discovery multipart metadata for %s: IsMultiPart=%v, PartNumber=%d",
				foundFilePath, info.IsMultiPart, info.PartNumber)
		}

		// Clean up old temp poster if movie ID changed during rescrape
		// IMPORTANT: We use movie.ID (the actual normalized ID from the Movie object)
		// instead of result.MovieID (the query string), because posters are stored as
		// {movie.ID}.jpg. If the scraper normalizes the ID (e.g., "ipx-123" -> "IPX-123"),
		// result.MovieID would be "ipx-123" but the poster file is "IPX-123.jpg".
		// We also check if any other result's movie has the same ID to protect multipart siblings.
		if movie != nil && movie.ID != "" && oldMovieID != "" && movie.ID != oldMovieID {
			otherMovieUsingOldID := false
			for filePath, otherResult := range status.Results {
				if filePath != foundFilePath && otherResult.Data != nil {
					if otherMovie, ok := otherResult.Data.(*models.Movie); ok && otherMovie.ID == oldMovieID {
						otherMovieUsingOldID = true
						logging.Debugf("[Rescrape] Skipping poster cleanup for %s - other result %s still uses this ID", oldMovieID, filePath)
						break
					}
				}
			}

			if !otherMovieUsingOldID {
				oldPosterPath := filepath.Join(cfg.System.TempDir, "posters", jobID, oldMovieID+".jpg")
				oldPosterFullPath := filepath.Join(cfg.System.TempDir, "posters", jobID, oldMovieID+"-full.jpg")
				if _, err := os.Stat(oldPosterPath); err == nil {
					if err := os.Remove(oldPosterPath); err != nil {
						logging.Warnf("[Rescrape] Failed to remove old temp poster %s: %v", oldPosterPath, err)
					} else {
						logging.Infof("[Rescrape] Removed old temp poster for movie ID %s (replaced by %s)", oldMovieID, movie.ID)
					}
				}
				if _, err := os.Stat(oldPosterFullPath); err == nil {
					_ = os.Remove(oldPosterFullPath)
				}
			}
		}

		job.UpdateFileResult(foundFilePath, result)

		// Persist the updated job state to database
		// This ensures the /jobs page shows the updated poster URL after rescrape
		deps.JobQueue.PersistJob(job)

		// Verify the update was persisted
		verifyStatus := job.GetStatus()
		if verifyResult, ok := verifyStatus.Results[foundFilePath]; ok {
			logging.Infof("[Rescrape] Verified update for %s: movieID=%s, status=%s",
				foundFilePath, verifyResult.MovieID, verifyResult.Status)
		} else {
			logging.Errorf("[Rescrape] Failed to verify update for %s", foundFilePath)
		}

		c.JSON(http.StatusOK, BatchRescrapeResponse{
			Movie:          movie,
			FieldSources:   result.FieldSources,
			ActressSources: result.ActressSources,
		})
	}
}
