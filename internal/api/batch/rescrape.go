package batch

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

		// Sanitize manual search input
		if req.ManualSearchInput != "" {
			// Remove invisible/zero-width characters
			req.ManualSearchInput = strings.Map(func(r rune) rune {
				// Remove zero-width characters (U+200B, U+200C, U+200D, U+FEFF)
				if r == '\u200B' || r == '\u200C' || r == '\u200D' || r == '\uFEFF' {
					return -1 // Remove
				}
				return r
			}, req.ManualSearchInput)

			// Trim whitespace
			req.ManualSearchInput = strings.TrimSpace(req.ManualSearchInput)

			// Reject if empty after sanitization
			if req.ManualSearchInput == "" {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Manual search input cannot be empty"})
				return
			}
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

		// Lock the job for atomic state validation
		// We validate twice: before work starts and after work completes
		job.Lock()

		// Check IsDeleted FIRST before status checks
		// Deleted jobs should return 410 Gone even if they also have terminal status
		if job.IsDeleted() {
			job.Unlock()
			c.JSON(http.StatusGone, gin.H{
				"error":   "Job has been deleted",
				"skipped": true,
			})
			return
		}

		currentStatus := job.Status
		if currentStatus == worker.JobStatusRunning ||
			currentStatus == worker.JobStatusOrganized ||
			currentStatus == worker.JobStatusFailed ||
			currentStatus == worker.JobStatusCancelled {
			job.Unlock()
			c.JSON(http.StatusConflict, ErrorResponse{
				Error: fmt.Sprintf("Cannot rescrape %s job", currentStatus),
			})
			return
		}
		job.Unlock()

		// Find the file result with this movie ID
		// Use GetStatus() for a consistent snapshot of results map
		status := job.GetStatus()

		// HIGH FIX: Collect all matching files for deterministic selection
		// Map iteration order is random, so for multipart jobs with same MovieID,
		// we must pick deterministically to avoid rescraping wrong file
		var matchingFiles []string
		for filePath, result := range status.Results {
			if result == nil {
				continue
			}
			if result.MovieID == movieID {
				matchingFiles = append(matchingFiles, filePath)
			}
		}

		var foundFilePath string
		var oldMovieID string

		if len(matchingFiles) == 0 {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error: fmt.Sprintf("Movie %s not found in batch job", movieID),
			})
			return
		} else if len(matchingFiles) > 1 {
			sort.Slice(matchingFiles, func(i, j int) bool {
				fi := status.FileMatchInfo[matchingFiles[i]]
				fj := status.FileMatchInfo[matchingFiles[j]]

				if fi.PartNumber != fj.PartNumber {
					if fi.PartNumber == 0 {
						return false
					}
					if fj.PartNumber == 0 {
						return true
					}
					return fi.PartNumber < fj.PartNumber
				}

				si := suffixOrder(fi.PartSuffix)
				sj := suffixOrder(fj.PartSuffix)
				if si != sj {
					return si < sj
				}

				return matchingFiles[i] < matchingFiles[j]
			})

			// After sorting by PartNumber and PartSuffix, the first file is the "primary" part
			// No need for additional primary selection logic - sort already handles it
			foundFilePath = matchingFiles[0]

			logging.Infof("[Rescrape] Multiple files found for movieID %s, selected %s from %v", movieID, foundFilePath, matchingFiles)
		} else {
			foundFilePath = matchingFiles[0]
		}

		// Get the old movie ID from the selected result
		var capturedRevision uint64
		if result, ok := status.Results[foundFilePath]; ok && result != nil {
			// Capture Revision for CAS check - this is the stable token for detecting concurrent writes
			capturedRevision = result.Revision

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

		// Warn if input looks like a URL but no compatible scrapers found
		// Note: We don't reject outright because some URLs might be handled by scrapers
		// that aren't enabled in the current configuration
		// Use case-insensitive detection to catch HTTP://, HTTPS://, etc.
		if req.ManualSearchInput != "" && (strings.HasPrefix(strings.ToLower(req.ManualSearchInput), "http://") || strings.HasPrefix(strings.ToLower(req.ManualSearchInput), "https://")) {
			if parsed == nil || !parsed.IsURL || len(parsed.CompatibleScrapers) == 0 {
				logging.Warnf("[Rescrape] URL detected but no compatible scrapers available: input=%s, compatible_scrapers=%v. Proceeding anyway, scraping may fail.",
					req.ManualSearchInput,
					func() []string {
						if parsed == nil {
							return []string{}
						}
						return parsed.CompatibleScrapers
					}())
			}
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

		// Check if job was deleted during rescrape (before any other checks)
		job.Lock()
		if job.IsDeleted() {
			job.Unlock()
			logging.Infof("[Rescrape] Job %s was deleted during rescrape, discarding results", jobID)

			// Clean up the new poster we just created (orphaned)
			if movie != nil && movie.ID != "" {
				newPosterPath := filepath.Join(cfg.System.TempDir, "posters", jobID, movie.ID+".jpg")
				newPosterFullPath := filepath.Join(cfg.System.TempDir, "posters", jobID, movie.ID+"-full.jpg")
				if _, err := os.Stat(newPosterPath); err == nil {
					_ = os.Remove(newPosterPath)
				}
				if _, err := os.Stat(newPosterFullPath); err == nil {
					_ = os.Remove(newPosterFullPath)
				}
			}

			c.JSON(http.StatusGone, gin.H{
				"error":   "Job was deleted during rescrape",
				"skipped": true,
			})
			return
		}
		job.Unlock()

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

		// TOCTOU FIX: Revalidate job state after long-running operation
		// The job could have transitioned to an invalid state during rescrape
		job.Lock()

		// MEDIUM FIX: Check tombstone FIRST before status checks
		// DeleteJob sets tombstone without changing status on completed jobs
		if job.IsDeleted() {
			job.Unlock()
			logging.Infof("[Rescrape] Job %s was deleted during rescrape, aborting update", jobID)

			// Clean up orphaned temp poster
			if movie != nil && movie.ID != "" {
				newPosterPath := filepath.Join(cfg.System.TempDir, "posters", jobID, movie.ID+".jpg")
				newPosterFullPath := filepath.Join(cfg.System.TempDir, "posters", jobID, movie.ID+"-full.jpg")
				if _, err := os.Stat(newPosterPath); err == nil {
					_ = os.Remove(newPosterPath)
					logging.Infof("[Rescrape] Cleaned up orphaned temp poster %s (job deleted)", newPosterPath)
				}
				if _, err := os.Stat(newPosterFullPath); err == nil {
					_ = os.Remove(newPosterFullPath)
				}
			}

			c.JSON(http.StatusGone, gin.H{
				"error":   "Job was deleted during rescrape",
				"skipped": true,
			})
			return
		}

		postRescrapeStatus := job.Status
		if postRescrapeStatus == worker.JobStatusRunning ||
			postRescrapeStatus == worker.JobStatusOrganized ||
			postRescrapeStatus == worker.JobStatusFailed ||
			postRescrapeStatus == worker.JobStatusCancelled {
			job.Unlock()
			logging.Warnf("[Rescrape] Job %s transitioned to %s during rescrape, aborting update", jobID, postRescrapeStatus)

			// MEDIUM FIX: Clean up orphaned temp poster on abort
			// RunBatchScrapeOnce may have created a new temp poster that we need to remove
			if movie != nil && movie.ID != "" {
				newPosterPath := filepath.Join(cfg.System.TempDir, "posters", jobID, movie.ID+".jpg")
				newPosterFullPath := filepath.Join(cfg.System.TempDir, "posters", jobID, movie.ID+"-full.jpg")
				if _, err := os.Stat(newPosterPath); err == nil {
					if err := os.Remove(newPosterPath); err != nil {
						logging.Warnf("[Rescrape] Failed to clean up orphaned temp poster %s: %v", newPosterPath, err)
					} else {
						logging.Infof("[Rescrape] Cleaned up orphaned temp poster %s (aborted rescrape)", newPosterPath)
					}
				}
				if _, err := os.Stat(newPosterFullPath); err == nil {
					_ = os.Remove(newPosterFullPath)
				}
			}

			c.JSON(http.StatusConflict, ErrorResponse{
				Error: fmt.Sprintf("Job transitioned to %s during rescrape, changes discarded", postRescrapeStatus),
			})
			return
		}

		// Preserve multipart metadata from discovery phase (for letter patterns like -A, -B)
		// This prevents losing multipart status when rescraping letter-pattern files
		// Access directly since we hold the lock
		if info, ok := job.FileMatchInfo[foundFilePath]; ok {
			result.IsMultiPart = info.IsMultiPart
			result.PartNumber = info.PartNumber
			result.PartSuffix = info.PartSuffix
			logging.Debugf("[Rescrape] Applied discovery multipart metadata for %s: IsMultiPart=%v, PartNumber=%d",
				foundFilePath, info.IsMultiPart, info.PartNumber)
		}

		// LOW FIX: Capture current result's MovieID BEFORE updating to detect concurrent modification
		// If another rescrape changed the result while we were running, skip poster cleanup
		var currentMovieIDBeforeUpdate string
		if existingResult := job.Results[foundFilePath]; existingResult != nil {
			if existingResult.Data != nil {
				if existingMovie, ok := existingResult.Data.(*models.Movie); ok {
					currentMovieIDBeforeUpdate = existingMovie.ID
				}
			}
			if currentMovieIDBeforeUpdate == "" {
				currentMovieIDBeforeUpdate = existingResult.MovieID
			}
		}

		// CAS FIX: Check for concurrent modification using Revision
		// Revision is the stable token for detecting concurrent writes, regardless of
		// MovieID normalization differences. If Revision changed, someone else wrote first.
		currentResult := job.Results[foundFilePath]
		currentRevision := uint64(0)
		if currentResult != nil {
			currentRevision = currentResult.Revision
		}
		if currentRevision != capturedRevision {
			job.Unlock()
			logging.Infof("[Rescrape] Concurrent rescrape detected for %s - discarding stale result (current revision=%d, our captured=%d)",
				foundFilePath, currentRevision, capturedRevision)

			// Clean up the poster we just created (orphaned since we're aborting)
			// HIGH FIX: Skip cleanup if our movie.ID matches the winner's movie.ID
			// This prevents the loser from deleting the winner's poster when both resolved to same ID
			// Use case-insensitive comparison on case-insensitive filesystems
			shouldCleanupPoster := true
			if movie != nil && movie.ID != "" && currentResult != nil {
				movieIDMatches := currentResult.MovieID == movie.ID
				posterDir := filepath.Join(cfg.System.TempDir, "posters", jobID)
				if !movieIDMatches && isCaseInsensitiveFSCached(posterDir) {
					movieIDMatches = strings.EqualFold(currentResult.MovieID, movie.ID)
				}
				if movieIDMatches {
					shouldCleanupPoster = false
					logging.Infof("[Rescrape] Skipping poster cleanup - winner has same movie.ID (%s)", movie.ID)
				} else if currentResult.Data != nil {
					if winnerMovie, ok := currentResult.Data.(*models.Movie); ok {
						winnerIDMatches := winnerMovie.ID == movie.ID
						if !winnerIDMatches && isCaseInsensitiveFSCached(posterDir) {
							winnerIDMatches = strings.EqualFold(winnerMovie.ID, movie.ID)
						}
						if winnerIDMatches {
							shouldCleanupPoster = false
							logging.Infof("[Rescrape] Skipping poster cleanup - winner has same canonical movie.ID (%s)", movie.ID)
						}
					}
				}
			}
			if shouldCleanupPoster && movie != nil && movie.ID != "" {
				newPosterPath := filepath.Join(cfg.System.TempDir, "posters", jobID, movie.ID+".jpg")
				newPosterFullPath := filepath.Join(cfg.System.TempDir, "posters", jobID, movie.ID+"-full.jpg")
				if _, err := os.Stat(newPosterPath); err == nil {
					_ = os.Remove(newPosterPath)
					logging.Infof("[Rescrape] Cleaned up orphaned poster from aborted rescrape: %s", newPosterPath)
				}
				if _, err := os.Stat(newPosterFullPath); err == nil {
					_ = os.Remove(newPosterFullPath)
				}
			}

			c.JSON(http.StatusConflict, gin.H{
				"error":   "File was concurrently rescraped, discarding stale result",
				"skipped": true,
			})
			return
		}

		// Update the job state with the rescrape result (persist the change)
		// Note: RunBatchScrapeOnce doesn't call UpdateFileResult, so we must do it here
		// We're already holding the lock, so update directly
		// Increment Revision to mark this as a new version
		result.Revision = capturedRevision + 1
		job.Results[foundFilePath] = result

		// Update counters
		completed := 0
		failed := 0
		for _, r := range job.Results {
			if r == nil {
				continue
			}
			switch r.Status {
			case worker.JobStatusCompleted:
				completed++
			case worker.JobStatusFailed:
				failed++
			}
		}
		job.Completed = completed
		job.Failed = failed
		if job.TotalFiles == 0 {
			job.Progress = 100
		} else {
			job.Progress = float64(completed+failed) / float64(job.TotalFiles) * 100
		}

		// CRITICAL FIX: Determine poster cleanup paths under lock, but perform actual
		// file operations outside lock to avoid deadlock with PersistJob (which takes RLock)
		var oldPosterPathsToCleanup []string

		// OVERWRITE CLEANUP: Always clean up what we're overwriting (currentMovieIDBeforeUpdate)
		// This handles the case where concurrent rescrape resolves back to original ID:
		// - Rescrape 1: A → B
		// - Rescrape 2: sees current="B", changes to "A" (back to original)
		// Without this, "B.jpg" would be orphaned because movie.ID == oldMovieID == "A"
		if currentMovieIDBeforeUpdate != "" && currentMovieIDBeforeUpdate != movie.ID {
			otherMovieUsingCurrentID := false
			for filePath, otherResult := range job.Results {
				if filePath != foundFilePath && otherResult != nil {
					if strings.EqualFold(otherResult.MovieID, currentMovieIDBeforeUpdate) {
						otherMovieUsingCurrentID = true
						break
					} else if otherResult.Data != nil {
						if otherMovie, ok := otherResult.Data.(*models.Movie); ok && strings.EqualFold(otherMovie.ID, currentMovieIDBeforeUpdate) {
							otherMovieUsingCurrentID = true
							break
						}
					}
				}
			}

			if !otherMovieUsingCurrentID {
				currentPosterPath := filepath.Join(cfg.System.TempDir, "posters", jobID, currentMovieIDBeforeUpdate+".jpg")
				currentPosterFullPath := filepath.Join(cfg.System.TempDir, "posters", jobID, currentMovieIDBeforeUpdate+"-full.jpg")
				posterDir := filepath.Join(cfg.System.TempDir, "posters", jobID)

				if strings.EqualFold(currentMovieIDBeforeUpdate, movie.ID) {
					if isCaseInsensitiveFSCached(posterDir) {
						logging.Infof("[Rescrape] Concurrent case change detected (%s → %s), skipping poster cleanup (case-insensitive filesystem)", currentMovieIDBeforeUpdate, movie.ID)
					} else {
						logging.Infof("[Rescrape] Concurrent case change detected (%s → %s) on case-sensitive filesystem, cleaning up poster", currentMovieIDBeforeUpdate, movie.ID)
						oldPosterPathsToCleanup = append(oldPosterPathsToCleanup, currentPosterPath, currentPosterFullPath)
					}
				} else {
					oldPosterPathsToCleanup = append(oldPosterPathsToCleanup, currentPosterPath, currentPosterFullPath)
					logging.Infof("[Rescrape] Concurrent modification detected, cleaning up poster for %s (overwritten)", currentMovieIDBeforeUpdate)
				}
			}
		}

		// ORIGINAL CHANGE CLEANUP: Only when movie.ID changed from original oldMovieID
		// Skip if currentMovieIDBeforeUpdate already covers this (concurrent modification)
		if movie != nil && movie.ID != "" && oldMovieID != "" && movie.ID != oldMovieID {
			if currentMovieIDBeforeUpdate == oldMovieID {
				skipCleanup := false
				posterDir := filepath.Join(cfg.System.TempDir, "posters", jobID)
				if strings.EqualFold(movie.ID, oldMovieID) {
					if isCaseInsensitiveFSCached(posterDir) {
						logging.Infof("[Rescrape] ID case change detected (%s → %s), skipping poster cleanup (case-insensitive filesystem)", oldMovieID, movie.ID)
						skipCleanup = true
					} else {
						logging.Infof("[Rescrape] ID case change detected (%s → %s) on case-sensitive filesystem, will clean up old poster", oldMovieID, movie.ID)
					}
				}

				if !skipCleanup {
					otherMovieUsingOldID := false
					for filePath, otherResult := range job.Results {
						if filePath != foundFilePath && otherResult != nil {
							if strings.EqualFold(otherResult.MovieID, oldMovieID) {
								otherMovieUsingOldID = true
								logging.Debugf("[Rescrape] Skipping poster cleanup for %s - other result %s still uses this ID (via MovieID field)", oldMovieID, filePath)
								break
							} else if otherResult.Data != nil {
								if otherMovie, ok := otherResult.Data.(*models.Movie); ok && strings.EqualFold(otherMovie.ID, oldMovieID) {
									otherMovieUsingOldID = true
									logging.Debugf("[Rescrape] Skipping poster cleanup for %s - other result %s still uses this ID (via Data field)", oldMovieID, filePath)
									break
								}
							}
						}
					}

					if !otherMovieUsingOldID {
						oldPosterPath := filepath.Join(cfg.System.TempDir, "posters", jobID, oldMovieID+".jpg")
						oldPosterFullPath := filepath.Join(cfg.System.TempDir, "posters", jobID, oldMovieID+"-full.jpg")
						oldPosterPathsToCleanup = append(oldPosterPathsToCleanup, oldPosterPath, oldPosterFullPath)
					}
				}
			}
		}

		// CRITICAL FIX: Unlock before PersistJob to avoid deadlock
		// persistToDatabase() takes RLock on job.mu, which would deadlock if we hold Lock
		job.Unlock()

		// Perform poster cleanup outside lock (file operations don't need mutex)
		for _, posterPath := range oldPosterPathsToCleanup {
			if _, err := os.Stat(posterPath); err == nil {
				if err := os.Remove(posterPath); err != nil {
					logging.Warnf("[Rescrape] Failed to remove old temp poster %s: %v", posterPath, err)
				} else {
					logging.Infof("[Rescrape] Removed old temp poster %s", posterPath)
				}
			}
		}

		// Persist the updated job state to database (outside lock to avoid deadlock)
		deps.JobQueue.PersistJob(job)

		logging.Infof("[Rescrape] Verified update for %s: movieID=%s, status=%s",
			foundFilePath, result.MovieID, result.Status)

		c.JSON(http.StatusOK, BatchRescrapeResponse{
			Movie:          movie,
			FieldSources:   result.FieldSources,
			ActressSources: result.ActressSources,
		})
	}
}
