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
	"github.com/javinizer/javinizer-go/internal/worker"

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

		result, prov, compensate, err := job.ApplyFieldOverride(c.Request.Context(), resultID, req.Field, req.Source)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "not found") {
				status = http.StatusNotFound
			}
			logging.Debugf("[FieldOverride] %s/%s field=%s source=%s: %v", jobID, resultID, req.Field, req.Source, err)
			c.JSON(status, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		// The overridden state lives only in the job envelope: surface a failed
		// persist instead of acknowledging an override a restart would drop.
		// The same failure after a poster-source refresh used to strand the
		// divergence permanently (restart state vs. refreshed cache), so the
		// compensation captured by ApplyFieldOverride reverts the in-memory
		// parts and rolls the cache back; its failures surface alongside the
		// persist error, not swallowed.
		//
		// Cover the persist + compensation pair with the poster-source lock
		// ApplyFieldOverride already released. Re-keying hazard: the lock key
		// is derived from the RETURNED result — ApplyFieldOverride re-resolves
		// the movie ID under the lock and persists the parts under that final
		// key, so a key computed from pre-call state could serialize against
		// nothing (F3). Ordering INSIDE the critical section: persist first,
		// compensate only on failure. Without the lock, a manual crop (or
		// source edit) landing in the gap would persist its own state after
		// the override's whole-movie writes and then be silently erased when
		// the compensation reverts those parts to their pre-override movies.
		// Lock ordering: only this lock is taken here — no overrideMu, no
		// second poster-source lock — matching updateBatchMoviePosterCrop.
		compensateLockKey := ""
		if result != nil {
			compensateLockKey = posterLockKeyFor(result)
		}
		releaseCompensateLock := worker.AcquirePosterSourceLock(jobID, compensateLockKey)
		defer releaseCompensateLock()
		if perr := deps.GetJobStore().PersistJobByID(jobID); perr != nil {
			errMsg := fmt.Sprintf("Failed to persist job state: %v", perr)
			if compensate != nil {
				if compErr := compensate(); compErr != nil {
					errMsg = fmt.Sprintf("%s (override revert failed: %v)", errMsg, compErr)
				}
			}
			logging.Errorf("Failed to persist field override for job %s: %v", jobID, perr)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: errMsg})
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
		c.JSON(http.StatusOK, contracts.FieldOverrideResponse{
			Movie:          movieView,
			FieldSources:   fieldSources,
			ActressSources: actressSources,
		})
	}
}
