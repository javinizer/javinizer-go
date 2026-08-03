package batch

import (
	"fmt"

	"net/http"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
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
// @Router /api/v1/batch/{id}/results/{resultId} [patch]
func updateBatchMovie(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")
		resultID := c.Param("resultId")

		var req contracts.UpdateMovieRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		job, ok := deps.GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}

		result, filePaths, found := lookupResultByResultID(job, resultID)

		if !found {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
			return
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
		// lose a manual crop); an explicit null clears it. The worker-side
		// UpdateMovie sanitize additionally clears geometry when the poster
		// source or crop intent changes, so stale bounds carried by an
		// out-of-sync client cannot survive.
		if !req.PosterCropBoundsFieldPresent && result.Movie != nil {
			if stored := result.Movie.Poster.PosterCropBounds; stored != nil {
				b := *stored
				movie.Poster.PosterCropBounds = &b
				movie.Poster.PosterCropSourceFull = result.Movie.Poster.PosterCropSourceFull
			}
		}
		if snap := rt.Snapshot(); snap != nil {
			if factory, fErr := snap.WorkflowFactory(); fErr == nil && factory != nil {
				if rendered := factory.RenderDisplayTitle(c.Request.Context(), movie); rendered != "" {
					movie.DisplayTitle = rendered
				}
			} else if fErr != nil {
				logging.Warnf("Failed to create workflow for display-title re-derive on save: %v", fErr)
			}
		}

		// UpdateMovie now handles both DB persistence and in-memory
		// update atomically. No need to call MovieRepo directly.

		// Update ALL file parts for this movie ID (handles multi-part files like CD1, CD2, etc.)
		for _, filePath := range filePaths {
			err := job.UpdateMovie(c.Request.Context(), filePath, movie)

			if err != nil {
				logging.Errorf("Failed to update movie for %s: %v", filePath, err)
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Failed to update movie: %v", err)})
				return
			}
		}

		// Persist the job envelope so a manual-crop clear (explicit null, or
		// sanitize clearing it after a source/intent change) survives a restart
		// — otherwise a reboot reloads the stale bounds from the envelope and a
		// discarded crop silently resurrects. Parity with the crop and
		// field-override handlers.
		deps.GetJobStore().PersistJobByID(jobID)

		c.JSON(http.StatusOK, contracts.MovieResponse{Movie: contracts.MovieViewFromModel(movie)})
	}
}

// updateBatchMoviePosterCrop godoc
// @Summary Update manual poster crop in batch job
// @Description Re-crop a temp poster for the review page using fixed-size crop coordinates
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param resultId path string true "Result ID"
// @Param request body contracts.PosterCropRequest true "Crop coordinates"
// @Success 200 {object} contracts.PosterCropResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/results/{resultId}/poster-crop [post]
func updateBatchMoviePosterCrop(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")
		resultID := c.Param("resultId")

		var req contracts.PosterCropRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		job, ok := deps.GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}

		result, _, found := lookupResultByResultID(job, resultID)

		if !found {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
			return
		}

		movieID := result.FileMatchInfo.MovieID

		posterID, err := resolvePosterID(job, movieID)
		if err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		// Resolve the max poster height: request-level override wins over the
		// configured default. 0 means no cap (preserve source resolution).
		// Snapshot so apiCfg and the poster manager see the same reload epoch (issue #44).
		snap := rt.Snapshot()
		maxPosterHeight := snap.APIConfig().BatchConfig().MaxPosterHeight
		if req.MaxPosterHeight != nil {
			maxPosterHeight = *req.MaxPosterHeight
		}

		cropResult, err := snap.PosterManager().CropWithBounds(c.Request.Context(), jobID, posterID, req.X, req.Y, req.Width, req.Height, maxPosterHeight)
		if err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		croppedURL := cropResult.CroppedURL

		// Persist the manual crop geometry (normalized 0–1 fractions) next to
		// the preview URL so the apply phase can reproduce the crop on the
		// downloaded source image. Bounds are validated in integer pixel space
		// against the measured source dimensions (exact containment), then
		// normalized and re-validated. When the legacy already-cropped fallback
		// served as the source (or its dimensions are undecodable) no applyable
		// geometry exists: nothing is stored and the response reports null.
		var bounds *models.CropBounds
		if cropResult.SourceFull && cropResult.SourceWidth > 0 && cropResult.SourceHeight > 0 {
			// Normalize pixel bounds to 0–1 fractions of the measured full
			// source, then validate in the same space the geometry is stored in:
			// finite components in [0,1], positive size, and unit-square
			// containment (Valid tolerates float-division error on exact edges).
			nb := models.CropBounds{
				X:            float64(req.X) / float64(cropResult.SourceWidth),
				Y:            float64(req.Y) / float64(cropResult.SourceHeight),
				Width:        float64(req.Width) / float64(cropResult.SourceWidth),
				Height:       float64(req.Height) / float64(cropResult.SourceHeight),
				SourceAspect: float64(cropResult.SourceWidth) / float64(cropResult.SourceHeight),
			}
			// No extra validity check here: CropWithBounds rejects out-of-range
			// or non-positive pixel bounds (400 above), so the normalized geometry
			// is valid by construction (finite components, positive size, unit
			// containment); sanitize/apply still re-validate before use.
			bounds = &nb
		}

		if err := job.UpdatePosterCrop(movieID, croppedURL, bounds, bounds != nil); err != nil {
			logging.Errorf("Failed to update poster crop in job state for %s: %v", movieID, err)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Failed to update job state: %v", err)})
			return
		}

		// Persist the job envelope so the manual crop survives a restart before
		// organize (the geometry is runtime-only, never a DB column). Parity
		// with the field-override handler (field_override.go).
		deps.GetJobStore().PersistJobByID(jobID)

		// Echo the server-side baseline snapshot so the client Reset flow
		// restores exactly what the server would (no client-side guessing).
		resp := contracts.PosterCropResponse{CroppedPosterURL: croppedURL, PosterCropBounds: bounds, ShouldCropPoster: false, PosterCropSourceFull: bounds != nil}
		if stored, _, found2 := lookupResultByResultID(job, resultID); found2 && stored.Movie != nil {
			resp.OriginalPosterURL = stored.Movie.Poster.OriginalPosterURL
			resp.OriginalCroppedPosterURL = stored.Movie.Poster.OriginalCroppedPosterURL
			resp.OriginalShouldCropPoster = stored.Movie.Poster.OriginalShouldCropPoster
		}
		c.JSON(http.StatusOK, resp)
	}
}

// updateBatchMoviePosterFromURL godoc
// @Summary Download poster from URL
// @Description Download a poster image from a URL and set it as the movie's poster in the batch job
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param resultId path string true "Result ID"
// @Param request body contracts.PosterFromURLRequest true "Poster URL"
// @Success 200 {object} contracts.PosterFromURLResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/results/{resultId}/poster-from-url [post]
func updateBatchMoviePosterFromURL(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")
		resultID := c.Param("resultId")

		var req contracts.PosterFromURLRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		job, ok := deps.GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}

		result, _, found := lookupResultByResultID(job, resultID)

		if !found {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
			return
		}

		movieID := result.FileMatchInfo.MovieID

		posterID, err := resolvePosterID(job, movieID)
		if err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		// Snapshot so apiCfg (user-agent/referer) and the poster manager see the
		// same reload epoch (issue #44).
		snap := rt.Snapshot()
		batchCfg := snap.APIConfig().BatchConfig()
		posterResult, err := snap.PosterManager().DownloadFromURL(c.Request.Context(), jobID, posterID, req.URL, batchCfg.ScraperUserAgent, batchCfg.ScraperReferer)
		if err != nil {
			if strings.Contains(err.Error(), "SSRF") || strings.Contains(err.Error(), "invalid URL") {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			} else if strings.Contains(err.Error(), "download") || strings.Contains(err.Error(), "status") {
				c.JSON(http.StatusBadGateway, contracts.ErrorResponse{Error: err.Error()})
			} else {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			}
			return
		}
		croppedURL := posterResult.CroppedURL

		// UpdatePosterFromURL handles both DB persistence and
		// in-memory update. No need to call MovieRepo directly.
		if err := job.UpdatePosterFromURL(c.Request.Context(), movieID, req.URL, croppedURL); err != nil {
			logging.Errorf("Failed to update poster from URL in job state for %s: %v", movieID, err)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Failed to update job state: %v", err)})
			return
		}

		// Persist the job envelope after the source change cleared the stored
		// manual crop geometry — otherwise a restart reloads the stale bounds
		// from the envelope and undoes the invalidation.
		deps.GetJobStore().PersistJobByID(jobID)

		c.JSON(http.StatusOK, contracts.PosterFromURLResponse{
			CroppedPosterURL: croppedURL,
			PosterURL:        req.URL,
		})
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
// @Router /api/v1/batch/{id}/results/{resultId}/exclude [post]
func excludeBatchMovie(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")
		resultID := c.Param("resultId")

		job, ok := deps.GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}

		result, filePaths, found := lookupResultByResultID(job, resultID)

		if !found {
			logging.Debugf("[ExcludeBatchMovie] No matches found for resultID=%s", resultID)
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
			return
		}

		movieID := result.FileMatchInfo.MovieID

		// Mark ALL parts as excluded (handles multi-part files like CD1, CD2, etc.)
		// ExcludeFile auto-cancels the job when all files are excluded.
		logging.Debugf("[ExcludeBatchMovie] Excluding %d file(s) for movieID=%s", len(filePaths), movieID)
		for _, filePath := range filePaths {
			job.ExcludeFile(filePath)
			logging.Debugf("[ExcludeBatchMovie] Excluded: %s", filePath)
		}

		logging.Infof("Movie %s (%d file(s)) excluded from batch job %s", movieID, len(filePaths), jobID)

		c.JSON(http.StatusOK, gin.H{"message": "Movie excluded from organization"})
	}
}

// resolvePosterID resolves the effective poster identifier for a movie within a
// batch job. It starts with the URL parameter movieID, then looks up the movie
// result to use the canonical Movie.ID if available. Returns an error if the
// resolved ID fails safe-filename validation (path traversal check).
func resolvePosterID(lookup resultstore.MovieLookup, movieID string) (string, error) {
	posterID := movieID
	movieResult, _ := lookup.FindMovieResultForMovieID(movieID)
	if movieResult != nil && movieResult.Movie != nil && movieResult.Movie.ID != "" {
		posterID = movieResult.Movie.ID
	}
	if posterID != filepath.Base(posterID) || posterID == "" || posterID == "." {
		return "", fmt.Errorf("invalid movie ID for poster operation")
	}
	return posterID, nil
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

		job, ok := deps.GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}

		var excluded []string
		var failed []contracts.BatchExcludeFailed

		for _, resultID := range req.ResultIDs {
			result, filePaths, found := lookupResultByResultID(job, resultID)

			if !found {
				failed = append(failed, contracts.BatchExcludeFailed{
					ResultID: resultID,
					Error:    fmt.Sprintf("Result %s not found in job", resultID),
				})
				continue
			}

			for _, filePath := range filePaths {
				job.ExcludeFile(filePath)
			}
			excluded = append(excluded, result.FileMatchInfo.MovieID)
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
