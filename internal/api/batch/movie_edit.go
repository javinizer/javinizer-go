package batch

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// lookupResultByResultID resolves a resultID to the corresponding MovieResult
// and all file paths for the same movie ID (handles multi-part files).
// Returns (result, filePaths, found).
func lookupResultByResultID(job worker.BatchJobInterface, resultID string) (*resultstore.MovieResult, []string, bool) {
	result, filePath, found := job.GetFileResultByResultID(resultID)
	if !found {
		return nil, nil, false
	}
	// Collect ALL file paths for the same movie ID (handles multi-part files)
	filePaths := job.FindFilePathsForMovieID(result.FileMatchInfo.MovieID)
	if len(filePaths) == 0 {
		filePaths = []string{filePath}
	}
	return result, filePaths, true
}

// updateBatchMovie godoc
// @Summary Update movie in batch job
// @Description Update a movie's metadata within a batch job's results
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param resultId path string true "Result ID"
// @Param request body contracts.UpdateMovieRequest true "Updated movie data"
// @Success 200 {object} contracts.MovieResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse "job busy (pending or scrape-phase)"
// @Failure 410 {object} contracts.ErrorResponse "job deleted"
// @Failure 500 {object} contracts.ErrorResponse "transactional save failed; all writes rolled back"
// @Router /api/v1/batch/{id}/results/{resultId} [patch]
func updateBatchMovie(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		resultID := c.Param("resultId")

		var req contracts.UpdateMovieRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		// POSTER-WRITE-HARDENING D16: admission gate (410 gone / 404 unknown /
		// 409 while Pending or scrape-Running) + shared lease held through the
		// whole op so delete can never reclaim state mid-save.
		job, release, admitted := admitOrWriteError(c, deps.GetJobStore().AcquireEditAccess)
		if !admitted {
			return
		}
		defer release()

		result, _, found := lookupResultByResultID(job, resultID)

		if !found {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
			return
		}
		movieID := result.FileMatchInfo.MovieID

		// P4 write-time validation: a malformed persisted source fingerprint is
		// not a harmless legacy omission. Reject it before the family transaction
		// so the client can correct its crop payload.
		if req.Movie != nil && req.Movie.PosterCropBounds != nil {
			fingerprint := strings.ToLower(strings.TrimSpace(req.Movie.PosterCropBounds.SourceFingerprint))
			if fingerprint != "" && !assetidentity.ValidFingerprint(fingerprint) {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "poster_crop_bounds.source_fingerprint must be a 64-character SHA-256 hex digest"})
				return
			}
			req.Movie.PosterCropBounds.SourceFingerprint = fingerprint
		}

		// Convert once and re-derive display_title so a title edit is reflected
		// immediately in persisted state and any client that renders display_title
		// (grid cards, metadata headers) without waiting for organize. Title is
		// never modified — RenderDisplayTitle only writes DisplayTitle — so this
		// cannot reintroduce the title-doubling bug this PR fixes. If the workflow
		// factory is unavailable, fall back to DisplayTitle = Title, matching the
		// canonical no-template/error degradation (display_title.go).
		movie := contracts.MovieViewToModel(req.Movie)
		movie.DisplayTitle = movie.Title

		// poster_crop_bounds PATCH semantics: an omitted key preserves the
		// stored geometry (legacy clients and unrelated edits must not silently
		// lose a manual crop); an explicit null clears it. The carry is
		// resolved INSIDE the family key (below) so a concurrent crop commit
		// between admission and lock-order can never be silently reverted
		// by a stale pre-lock read.
		if snap := rt.Snapshot(); snap != nil {
			if factory, fErr := snap.WorkflowFactory(); fErr == nil && factory != nil {
				if rendered := factory.RenderDisplayTitle(c.Request.Context(), movie); rendered != "" {
					movie.DisplayTitle = rendered
				}
			} else if fErr != nil {
				logging.Warnf("Failed to create workflow for display-title re-derive on save: %v", fErr)
			}
		}

		// ONE family-scoped transactional save (POSTER-WRITE-HARDENING D1/D4):
		// movie row + actress renames + envelope upsert commit in a single
		// composite transaction; any leg failing rolls ALL back (5xx) and the
		// in-memory state is never published. No post-save persist call — the
		// envelope is part of the tx.
		// Dual-key-locked family commit (D1): identity-changing PATCHes hold
		// old+new keys atomically; the omitted-bounds carry re-reads stored
		// geometry INSIDE the keys (never the handler's pre-lock read).
		opRev, opFam, opErr := job.UpdateMovieFamilyWithEcho(c.Request.Context(), movieID, resultID, movie, worker.FamilySaveOptions{CarryCropGeometry: !req.PosterCropBoundsFieldPresent, ExpectedResultRevision: req.ExpectedResultRevision, ExpectedResultRevisions: req.ExpectedResultRevisions})
		if opErr != nil {
			logging.Errorf("Failed to update movie family %s: %v", movieID, opErr)
			writeEditOpError(c, fmt.Errorf("failed to update movie: %w", opErr))
			return
		}

		// audit F-R15-1: the echo comes from the OP's own keyed section —
		// revision content the client adopts always matches OUR saved state.
		c.JSON(http.StatusOK, contracts.MovieResponse{Movie: contracts.MovieViewFromModel(movie), Revision: opRev, Revisions: opFam})
	}
}

// previewDisplayTitle godoc
// @Summary Preview the rendered display_title template
// @Description Render the configured display_title template for the provided movie using the shared workflow template engine, without mutating persisted state. Used by the review page to show a live NFO title preview as the user edits the base title.
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param resultId path string true "Result ID"
// @Param request body contracts.DisplayTitlePreviewRequest true "Edited movie to render"
// @Success 200 {object} contracts.DisplayTitlePreviewResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/results/{resultId}/display-title-preview [post]
func previewDisplayTitle(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req contracts.DisplayTitlePreviewRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		if req.Movie == nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "movie is required"})
			return
		}

		snap := rt.Snapshot()
		factory, err := snap.WorkflowFactory()
		if err != nil || factory == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Failed to create workflow for display-title preview: %v", err)})
			return
		}

		rendered := factory.RenderDisplayTitle(c.Request.Context(), contracts.MovieViewToModel(req.Movie))
		c.JSON(http.StatusOK, contracts.DisplayTitlePreviewResponse{DisplayTitle: rendered})
	}
}

// excludeBatchMovie godoc
// @Summary Exclude movie from batch organization
// @Description Mark a movie in a batch job as excluded from file organization
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Param resultId path string true "Result ID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse "a phase is running"
// @Failure 410 {object} contracts.ErrorResponse "job deleted"
// @Router /api/v1/batch/{id}/results/{resultId}/exclude [post]
func excludeBatchMovie(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")
		resultID := c.Param("resultId")

		// Exclusion is an admitted editor operation (D16): 409 while any phase
		// is Running; the lease spans commit through response.
		job, release, admitted := admitOrWriteError(c, deps.GetJobStore().AcquireExclusionAccess)
		if !admitted {
			return
		}
		defer release()

		result, filePaths, found := lookupResultByResultID(job, resultID)

		if !found {
			logging.Debugf("[ExcludeBatchMovie] No matches found for resultID=%s", resultID)
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
			return
		}

		movieID := result.FileMatchInfo.MovieID

		logging.Debugf("[ExcludeBatchMovie] Excluding family for movieID=%s (%d file(s))", movieID, len(filePaths))
		if err := job.ExcludeMovieFamily(c.Request.Context(), movieID); err != nil {
			writeEditOpError(c, err)
			return
		}

		logging.Infof("Movie %s (%d file(s)) excluded from batch job %s", movieID, len(filePaths), jobID)

		c.JSON(http.StatusOK, gin.H{"message": "Movie excluded from organization"})
	}
}

const bulkExcludeMaxMovies = 100

// batchExcludeMovies godoc
// @Summary Bulk exclude movies from batch organization
// @Description Exclude multiple movies from a batch job in a single request. Best-effort: excludes as many as possible and returns per-result failures.
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param request body contracts.BatchExcludeRequest true "Result IDs to exclude"
// @Success 200 {object} contracts.BatchExcludeResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse "a phase is running"
// @Failure 410 {object} contracts.ErrorResponse "job deleted"
// @Router /api/v1/batch/{id}/movies/batch-exclude [post]
func batchExcludeMovies(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")

		var req contracts.BatchExcludeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		if len(req.ResultIDs) == 0 {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "result_ids is required and must not be empty"})
			return
		}

		if len(req.ResultIDs) > bulkExcludeMaxMovies {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: fmt.Sprintf("result_ids must not exceed %d items", bulkExcludeMaxMovies)})
			return
		}

		// One admission lease spans the whole bulk-op loop (D16 gate at the top).
		job, release, admitted := admitOrWriteError(c, deps.GetJobStore().AcquireExclusionAccess)
		if !admitted {
			return
		}
		defer release()

		var excluded []string
		var failed []contracts.BatchExcludeFailed

		for _, resultID := range req.ResultIDs {
			result, _, found := lookupResultByResultID(job, resultID)

			if !found {
				failed = append(failed, contracts.BatchExcludeFailed{
					ResultID: resultID,
					Error:    fmt.Sprintf("Result %s not found in job", resultID),
				})
				continue
			}

			movieID := result.FileMatchInfo.MovieID
			if err := job.ExcludeMovieFamily(c.Request.Context(), movieID); err != nil {
				failed = append(failed, contracts.BatchExcludeFailed{
					ResultID: resultID,
					Error:    err.Error(),
				})
				continue
			}
			excluded = append(excluded, movieID)
		}

		logging.Infof("Batch exclude: %d movie(s) excluded, %d failed from batch job %s", len(excluded), len(failed), jobID)

		updatedStatus := job.GetStatus()
		jobResponse := buildBatchJobResponse(updatedStatus)

		if excluded == nil {
			excluded = []string{}
		}
		if failed == nil {
			failed = []contracts.BatchExcludeFailed{}
		}

		c.JSON(http.StatusOK, contracts.BatchExcludeResponse{
			Excluded: excluded,
			Failed:   failed,
			Job:      jobResponse,
		})
	}
}
