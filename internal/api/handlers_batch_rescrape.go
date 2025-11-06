package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
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
		for filePath, result := range status.Results {
			if result.MovieID == movieID {
				foundFilePath = filePath
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

		// Create HTTP client for poster downloads
		requestTimeout := time.Duration(cfg.Scrapers.RequestTimeoutSeconds) * time.Second
		if requestTimeout <= 0 {
			requestTimeout = 30 * time.Second
		}
		httpClient, err := httpclient.NewHTTPClient(&cfg.Scrapers.Proxy, requestTimeout)
		if err != nil {
			logging.Warnf("Failed to create HTTP client for poster downloads: %v", err)
			httpClient = nil // Continue without poster generation
		}

		// Use RunBatchScrapeOnce to perform the rescrape
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
		defer cancel()

		// Determine query override (for manual search)
		queryOverride := req.ManualSearchInput
		if queryOverride == "" {
			// Use movieID as query if no manual input provided
			queryOverride = movieID
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
			req.SelectedScrapers,   // selectedScrapers (empty = use defaults)
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
		job.UpdateFileResult(foundFilePath, result)

		// Verify the update was persisted
		verifyStatus := job.GetStatus()
		if verifyResult, ok := verifyStatus.Results[foundFilePath]; ok {
			logging.Infof("[Rescrape] Verified update for %s: movieID=%s, status=%s",
				foundFilePath, verifyResult.MovieID, verifyResult.Status)
		} else {
			logging.Errorf("[Rescrape] Failed to verify update for %s", foundFilePath)
		}

		// Generate and persist cropped poster
		if httpClient != nil && movie != nil {
			croppedURL, posterErr := worker.GenerateCroppedPoster(
				ctx,
				movie,
				httpClient,
				cfg.Scrapers.UserAgent,
				cfg.Scrapers.Referer,
			)
			if posterErr != nil {
				logging.Warnf("Failed to generate cropped poster: %v", posterErr)
			} else {
				// Update movie's cropped poster URL
				movie.CroppedPosterURL = croppedURL
				// Save to database
				if err := deps.MovieRepo.Update(movie); err != nil {
					logging.Warnf("Failed to save cropped poster URL to database: %v", err)
				}
			}
		}

		c.JSON(http.StatusOK, BatchRescrapeResponse{
			Movie: movie,
		})
	}
}
