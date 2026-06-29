package batch

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
)

// rescrapeBatchMovie godoc
// @Summary Rescrape movie in batch job
// @Description Rescrape a specific movie within a batch job using selected scrapers or manual search input
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param resultId path string true "Result ID"
// @Param request body contracts.BatchRescrapeRequest true "Rescrape options"
// @Success 200 {object} contracts.BatchRescrapeResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse
// @Failure 410 {object} contracts.ErrorResponse
// @Failure 422 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/results/{resultId}/rescrape [post]
func rescrapeBatchMovie(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		resultID := c.Param("resultId")

		var req contracts.BatchRescrapeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		if httpStatus, errMsg := validateRescrapeRequest(&req); errMsg != "" {
			writeErrorResponse(c, httpStatus, false, errMsg)
			return
		}

		job, err := prepareBatchRequest(rt.Deps(), rt, c, withSkipRunningCheck())
		if err != nil {
			return
		}

		logging.Infof("Batch rescrape request for job %s, result %s: scrapers=%v, manual_input=%s, force=%v",
			job.GetID(), resultID, req.SelectedScrapers, scrape.RedactURLQuery(req.ManualSearchInput), req.Force)

		// Resolve result by resultID to get movieID and filePath
		result, filePath, found := job.GetFileResultByResultID(resultID)
		if !found {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
			return
		}

		// Use the current authoritative movie ID from the typed Movie result
		// (matching result_read_store.GetCurrentMovieID), falling back to the
		// original match ID only when the result movie is absent. This ensures
		// edits/prior rescrapes that selected a different movie use the current
		// selection rather than the stale match ID.
		movieID := result.FileMatchInfo.MovieID
		if result.Movie != nil && result.Movie.ID != "" {
			movieID = result.Movie.ID
		}

		// Additional status check for rescrape-specific allowed states.
		snap := job.GetStatus()
		if rescrapeNotAllowed(snap) {
			if snap.IsDeleted {
				writeErrorResponse(c, http.StatusGone, true, "Job has been deleted")
			} else {
				writeErrorResponse(c, http.StatusConflict, false, fmt.Sprintf("Cannot rescrape %s job", snap.Status))
			}
			return
		}

		// Delegate to orchestrator for resolve→construct→execute pipeline
		orch := NewRescrapeOrchestrator(RescrapeDeps{
			JobStore:  rt.Deps().GetJobStore(),
			WfFactory: &apiWorkflowFactory{rt: rt},
			Factory:   rt.GetBatchJobFactory(),
			Persist:   rt.Deps().GetJobStore(),
			Broadcast: nil, // no progress broadcast for single rescrape
			ServerCtx: rt.ServerCtx(),
		})

		rescrapeResult, err := orch.Rescrape(c.Request.Context(), job.GetID(), movieID, filePath, &req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Rescrape failed: %v", err)})
			return
		}

		rr := rescrapeResult.RescrapeResult
		if rr.Status == models.RescrapeStatusGone {
			writeErrorResponse(c, http.StatusGone, true, "Job was deleted during rescrape")
			return
		}

		if rr.Status == models.RescrapeStatusConflict {
			writeErrorResponse(c, http.StatusConflict, true, "File was concurrently rescraped, discarding stale result")
			return
		}

		if rr.Status == models.RescrapeStatusFailed {
			c.JSON(http.StatusUnprocessableEntity, contracts.ErrorResponse{Error: fmt.Sprintf("Rescrape failed: %s", rr.Error)})
			return
		}

		logging.Infof("[Rescrape] Verified update for job %s, result %s (movieID=%s): status=%s",
			job.GetID(), resultID, movieID, rr.Status)

		c.JSON(http.StatusOK, contracts.BatchRescrapeResponse{
			Movie:          contracts.MovieViewFromModel(rr.Movie),
			FieldSources:   rr.FieldSources,
			ActressSources: rr.ActressSources,
		})
	}
}
