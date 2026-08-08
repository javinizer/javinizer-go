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
// @Failure 409 {object} contracts.ErrorResponse "job busy (pending or scrape-phase) or content-id change rejected"
// @Failure 410 {object} contracts.ErrorResponse "job deleted"
// @Failure 500 {object} contracts.ErrorResponse "transactional commit failed; all writes rolled back"
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

		// Admission gate + lease (D16). The override commits movie row +
		// provenance + envelope transactionally inside the op (D4) — no
		// post-op PersistJobByID.
		job, release, admitted := admitOrWriteError(c, deps.GetJobStore().AcquireEditAccess)
		if !admitted {
			return
		}
		defer release()

		result, prov, err := job.ApplyFieldOverride(c.Request.Context(), resultID, req.Field, req.Source)
		if err != nil {
			logging.Debugf("[FieldOverride] %s/%s field=%s source=%s: %v", jobID, resultID, req.Field, req.Source, err)
			if mapBatchEditError(c, err) {
				return
			}
			// Domain validation failures keep the historical 400; transactional
			// commit failures (composite tx leg) are internal — 500 per the
			// swagger contract, never silently swallowed as 400.
			if strings.Contains(err.Error(), "unsupported field") ||
				strings.Contains(err.Error(), "did not contribute") ||
				strings.Contains(err.Error(), "no provenance available") {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		var movieView *contracts.MovieView
		if result != nil && result.Movie != nil {
			movieView = contracts.MovieViewFromModel(result.Movie)
		}
		var fieldSources, actressSources map[string]string
		if prov != nil {
			fieldSources = prov.FieldSources
			actressSources = prov.ActressSources
		}
		// audit F-R8-3: ApplyFieldOverride's result was read INSIDE the family
		// key post-commit — echoing it directly sidesteps the off-key echo race
		// (a concurrent commit in the gap would misheal the CAS baseline).
		var revEcho *uint64
		if result != nil {
			rv := result.Revision
			revEcho = &rv
		}
		c.JSON(http.StatusOK, contracts.FieldOverrideResponse{
			Movie:          movieView,
			FieldSources:   fieldSources,
			ActressSources: actressSources,
			Revision:       revEcho,
		})
	}
}
