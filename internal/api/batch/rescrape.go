package batch

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
)

func rescrapeBatchMovie(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		movieID := c.Param("movieId")

		var req BatchRescrapeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}

		if httpStatus, errMsg := validateRescrapeRequest(&req); errMsg != "" {
			writeErrorResponse(c, httpStatus, false, errMsg)
			return
		}

		logging.Infof("Batch rescrape request for job %s, movie %s: scrapers=%v, manual_input=%s, force=%v",
			jobID, movieID, req.SelectedScrapers, req.ManualSearchInput, req.Force)

		job, ok := deps.JobQueue.GetJobPointer(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "Job not found"})
			return
		}

		isGone, httpStatus, errMsg := validateJobState(job)
		if errMsg != "" {
			writeErrorResponse(c, httpStatus, isGone, errMsg)
			return
		}

		lookup, httpStatus, errMsg := findFileForMovieID(job, movieID)
		if errMsg != "" {
			c.JSON(httpStatus, ErrorResponse{Error: errMsg})
			return
		}

		cfg := deps.GetConfig()

		// Check if job was deleted during rescrape (before starting work)
		job.Lock()
		if job.IsDeleted() {
			job.Unlock()
			writeErrorResponse(c, http.StatusGone, true, "Job has been deleted")
			return
		}
		job.Unlock()

		params, _ := resolveScrapeParams(&req, movieID, deps)

		result, err := executeRescrape(c.Request.Context(), params, job, lookup.foundFilePath, deps, &req, cfg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: fmt.Sprintf("Rescrape failed: %v", err)})
			return
		}

		if result == nil {
			logging.Errorf("[Rescrape] RunBatchScrapeOnce returned nil result for %s", lookup.foundFilePath)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Rescrape produced no result"})
			return
		}

		if result.Status != worker.JobStatusCompleted {
			errorMsg := "Unknown error"
			if result.Error != "" {
				errorMsg = result.Error
			}
			c.JSON(http.StatusUnprocessableEntity, ErrorResponse{Error: fmt.Sprintf("Rescrape failed: %s", errorMsg)})
			return
		}

		// Get movie from result data for response and poster cleanup
		var movie *models.Movie
		if result.Data != nil {
			if m, ok := result.Data.(*models.Movie); ok {
				movie = m
			}
		}

		updateRes := validateAndUpdateResult(job, result, lookup.foundFilePath, lookup.capturedRevision, movie, lookup.oldMovieID, cfg, jobID)
		if updateRes.shouldAbort {
			writeErrorResponse(c, updateRes.httpStatus, updateRes.isGone, updateRes.errorMessage)
			return
		}

		cleanupPosterPaths(updateRes.posterPaths)
		deps.JobQueue.PersistJob(job)

		logging.Infof("[Rescrape] Verified update for %s: movieID=%s, status=%s",
			lookup.foundFilePath, result.MovieID, result.Status)

		c.JSON(http.StatusOK, BatchRescrapeResponse{
			Movie:          movie,
			FieldSources:   result.FieldSources,
			ActressSources: result.ActressSources,
		})
	}
}
