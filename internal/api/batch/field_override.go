package batch

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"

	"github.com/gin-gonic/gin"
)

// getBatchMovieSources godoc
// @Summary Get per-source raw scraper results for a movie
// @Description Returns each successful scraper's raw ScraperResult for the movie, used by the review-page source viewer to offer per-field overrides. ScraperResults are persisted in the job envelope and survive server restarts. A synthesized single-source fallback is returned only for legacy envelopes persisted before this feature or when provenance was never set.
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Param resultId path string true "Result ID"
// @Success 200 {object} contracts.SourceResultsResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/results/{resultId}/sources [get]
func getBatchMovieSources(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")
		resultID := c.Param("resultId")

		job, ok := deps.GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}

		result, filePath, found := job.GetFileResultByResultID(resultID)
		if !found {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
			return
		}

		prov := job.GetProvenance(filePath)
		results := []*models.ScraperResult{}
		if prov != nil && prov.ScraperResults != nil {
			results = prov.ScraperResults
		}
		// Fallback: synthesize a single-source result from the aggregated movie
		// when ScraperResults is empty. This covers legacy envelopes persisted
		// before ScraperResults were persisted, or cases where provenance was
		// never set (e.g. cache-hit scrapes that pre-date this feature).
		if len(results) == 0 && result != nil && result.Movie != nil {
			if synth := scrape.ScraperResultFromCachedMovie(result.Movie); synth != nil {
				results = []*models.ScraperResult{synth}
			}
		}
		c.JSON(http.StatusOK, contracts.SourceResultsResponse{Results: results})
	}
}

// overrideBatchMovieField godoc
// @Summary Override a field with a source's value
// @Description Cherry-pick a single field's value from the named source's raw scraper results, overwriting the aggregated movie field and updating provenance attribution. Mirrors the original Javinizer "Replace" button (javinizergui.ps1:2538).
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param resultId path string true "Result ID"
// @Param request body contracts.FieldOverrideRequest true "Field + source override"
// @Success 200 {object} contracts.FieldOverrideResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/results/{resultId}/field-override [post]
func overrideBatchMovieField(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")
		resultID := c.Param("resultId")

		var req contracts.FieldOverrideRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		job, ok := deps.GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}

		result, prov, err := job.ApplyFieldOverride(c.Request.Context(), resultID, req.Field, req.Source)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "not found") {
				status = http.StatusNotFound
			}
			logging.Debugf("[FieldOverride] %s/%s field=%s source=%s: %v", jobID, resultID, req.Field, req.Source, err)
			c.JSON(status, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		deps.GetJobStore().PersistJobByID(jobID)

		var movieView *contracts.MovieView
		if result != nil && result.Movie != nil {
			movieView = contracts.MovieViewFromModel(result.Movie)
		}
		var fieldSources, actressSources map[string]string
		if prov != nil {
			fieldSources = prov.FieldSources
			actressSources = prov.ActressSources
		}
		c.JSON(http.StatusOK, contracts.FieldOverrideResponse{
			Movie:          movieView,
			FieldSources:   fieldSources,
			ActressSources: actressSources,
		})
	}
}
