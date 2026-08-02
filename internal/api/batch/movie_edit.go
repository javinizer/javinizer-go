package batch

import (
	"errors"
	"fmt"

	"net/http"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
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

		current, _, found := lookupResultByResultID(job, resultID)
		var filePaths []string

		if !found {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
			return
		}

		// Serialize the poster-source snapshot → refresh/cleanup → persist
		// sequence below against the field-override path
		// (jobEditorImpl.ApplyFieldOverride): an interleaved pair of
		// concurrent source-changing edits can refresh the cached -full.jpg
		// from one image while persisting the other's URL, leaving a
		// subsequent manual crop measured against the wrong image. Held from
		// here across the refresh, the multipart UpdateMovie loop (including
		// compensation) and the final PersistJobByID; the deferred release
		// covers every error/return path. Keyed on the same movie ID the
		// temp poster cache and the override path use.
		posterLockKey := current.FileMatchInfo.MovieID
		if current.Movie != nil && current.Movie.ID != "" {
			posterLockKey = current.Movie.ID
		}
		releasePosterLock := worker.AcquirePosterSourceLock(jobID, posterLockKey)
		defer releasePosterLock()

		freshCurrent, freshFilePaths, freshFound := lookupResultByResultID(job, resultID)
		if !freshFound {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
			return
		}
		current, filePaths = freshCurrent, freshFilePaths

		// Convert once and re-derive display_title so a title edit is reflected
		// immediately in persisted state and any client that renders display_title
		// (grid cards, metadata headers) without waiting for organize. Title is
		// never modified — RenderDisplayTitle only writes DisplayTitle — so this
		// cannot reintroduce the title-doubling bug this PR fixes. If the workflow
		// factory is unavailable, fall back to DisplayTitle = Title, matching the
		// canonical no-template/error degradation (display_title.go).
		movie := contracts.MovieViewToModel(req.Movie)
		// Whole-movie PATCHes from cached or external clients that predate
		// poster_crop_bounds omit the field entirely; decoded as a nil pointer
		// it would replace the stored bounds, and Organize would re-download
		// the uncropped poster despite the source and crop decision being
		// unchanged. Preserve the stored bounds only when the field was absent
		// from the JSON — an explicit null remains a deliberate clear, per the
		// pre-existing semantics for present fields. The copy avoids aliasing
		// job state.
		if !req.PosterCropBoundsPresent && current.Movie != nil && current.Movie.Poster.CropBounds != nil {
			bounds := *current.Movie.Poster.CropBounds
			movie.Poster.CropBounds = &bounds
		}
		if b := movie.Poster.CropBounds; b != nil && (b.X < 0 || b.Y < 0 || b.Width <= 0 || b.Height <= 0 || b.MaxPosterHeight < 0 || b.ImageWidth < 0 || b.ImageHeight < 0) {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "invalid poster_crop_bounds: x/y must be >= 0 and width/height must be > 0"})
			return
		}
		// A whole-movie PATCH that changes the poster source or crop decision
		// invalidates bounds measured against the previous image (defense in
		// depth — the shipped client clears these itself via the overlay and
		// field-override paths; the crop and poster-from-url endpoints manage
		// poster state directly and are unaffected).
		// downloadPoster reads PosterURL ?? CoverURL — only those fields plus
		// the crop decision can invalidate stored bounds; a fanart-only change
		// (CoverURL while PosterURL is set) must not drop the user's crop.
		posterSourceChanged := false
		if current.Movie != nil {
			cm := current.Movie.Poster
			oldSource, newSource := cm.PosterURL, movie.Poster.PosterURL
			if oldSource == "" {
				oldSource = cm.CoverURL
			}
			if newSource == "" {
				newSource = movie.Poster.CoverURL
			}
			posterSourceChanged = oldSource != newSource
			// The auto-crop decision the PATCH carried belongs to the image it
			// described. When the effective source changed, re-derive it: first
			// from a KNOWN scraper source — the persisted provenance ScraperResults
			// ship each source's ShouldCropPoster paired with that source's own
			// effective poster URL, and scrapers like javdb/mgstage populate
			// PosterURL from their landscape CoverURL WITH ShouldCropPoster=true,
			// so a PATCH selecting such a source must keep the true intent
			// (temp previews are always auto-cropped; without this, Organize
			// would write the landscape image whole under a cropped preview).
			// Only when no recorded source describes the new image does the
			// URL-class fallback kick in, so a stale cover intent cannot survive
			// onto a poster-grade image: the crop endpoint records
			// ShouldCropPoster as CropBounds.SourceWasCover, and on an apply-time
			// geometry failure the downloader degrades SourceWasCover=true bounds
			// to the default cover crop instead of keeping the poster whole
			// (internal/downloader/media.go). A cleared poster URL conversely
			// regains cover-backed semantics. An unchanged source keeps the
			// client-sent flag untouched — an explicit should_crop_poster flip
			// with no source change is a deliberate decision. Parity with the
			// field-override path (applyFieldOverride's poster_url/cover_url
			// cases).
			if posterSourceChanged {
				var sources []*models.ScraperResult
				for _, fp := range filePaths {
					if prov := job.GetProvenance(fp); prov != nil {
						sources = append(sources, prov.ScraperResults...)
					}
				}
				movie.Poster.SyncCropIntentWithSource(sources...)
			}
			if movie.Poster.CropBounds != nil &&
				(posterSourceChanged || cm.ShouldCropPoster != movie.Poster.ShouldCropPoster) {
				movie.Poster.CropBounds = nil
			}
		}
		// A whole-movie PATCH that changes the effective poster source (poster_url,
		// or cover_url while no poster URL is set) must also regenerate the cached
		// full-size poster before the new URLs are persisted: the review client
		// treats the persisted URL as already synced and skips its poster-from-url
		// call, so a missed refresh would let a subsequent manual crop measure the
		// stale pre-PATCH -full.jpg while Organize downloads the new one. A PATCH
		// that instead clears the LAST source succeeds as a cleanup: the cached
		// -full.jpg/preview are removed (regenerating from no URL would 500) so
		// no stale crop source lingers. Shares the field-override path's
		// machinery (worker.RefreshPosterAssets) with the same atomicity:
		// snapshot before refresh/cleanup, roll the cache back when the
		// UpdateMovie persistence below fails, surface a failed rollback.
		var rollback func() error
		if current.Movie != nil {
			cm := current.Movie.Poster
			var refreshErr error
			rollback, refreshErr = worker.RefreshPosterAssets(c.Request.Context(), rt.Snapshot().PosterGen(), jobID, movie, cm.PosterURL, cm.CoverURL)
			if refreshErr != nil {
				logging.Errorf("Failed to refresh poster source after whole-movie edit for result %s: %v", resultID, refreshErr)
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Failed to refresh poster source: %v", refreshErr)})
				return
			}
		}
		movie.DisplayTitle = movie.Title
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

		// Update ALL file parts for this movie ID (handles multi-part files
		// like CD1, CD2, etc.). There is no store-level transaction across
		// parts, so the multipart update is made atomic by compensation: when
		// a later part's UpdateMovie fails after an earlier one succeeded, the
		// earlier parts are reverted (re-persisted through UpdateMovie with
		// their pre-request stored movies) BEFORE the poster-cache rollback
		// runs. Rolling the cache back while a part still holds the new poster
		// source URL would desync job state from the cached -full.jpg: a retry
		// routed through that part's resultID would see the persisted URL as
		// unchanged, skip the asset refresh, and a subsequent manual crop would
		// then be measured against the restored old image while Organize
		// downloads the new source.
		type updatedPart struct {
			filePath string
			original *models.Movie // pre-update stored movie, held for revert
		}
		var updated []updatedPart
		var updateErr error
		var updateFailPath string
		for _, filePath := range filePaths {
			var original *models.Movie
			if prev, gErr := job.GetMovieResult(filePath); gErr == nil && prev != nil {
				original = prev.Movie
			}
			// OriginalFileName is per-part: it is populated from each part's
			// own FileMatchInfo (scrapeResultToMovieResult) and read by
			// template contexts (<FILENAME>/the NFO original path). A
			// whole-movie PATCH round-trips the SELECTED part's value; fanning
			// it out unchanged would relabel every sibling part with that file
			// name (CD1's onto CD2). Preserve each part's stored value whenever
			// the request merely carries the selection's current name — a
			// request value that differs from the selection's stored name is a
			// deliberate whole-movie rename and is applied to all parts.
			// (Sibling parity with ApplyFieldOverride's per-part merge.)
			partMovie := movie
			if current.Movie != nil && original != nil &&
				movie.OriginalFileName == current.Movie.OriginalFileName &&
				original.OriginalFileName != movie.OriginalFileName {
				partMovie = movie.Clone()
				partMovie.OriginalFileName = original.OriginalFileName
			}
			if err := job.UpdateMovie(c.Request.Context(), filePath, partMovie); err != nil {
				updateErr, updateFailPath = err, filePath
				break
			}
			updated = append(updated, updatedPart{filePath: filePath, original: original})
		}
		if updateErr != nil {
			errMsg := fmt.Sprintf("Failed to update movie: %v", updateErr)
			// Revert the parts already updated so no part keeps the new poster
			// metadata that the poster-cache rollback below erases; a failed
			// revert is surfaced alongside the primary error, not swallowed.
			for _, part := range updated {
				if part.original == nil {
					continue
				}
				if revertErr := job.UpdateMovie(c.Request.Context(), part.filePath, part.original); revertErr != nil {
					errMsg = fmt.Sprintf("%s (revert of part %s failed: %v)", errMsg, part.filePath, revertErr)
				}
			}
			if rollback != nil {
				// Persistence failed after the refresh replaced the cached
				// poster assets: restore the pre-refresh cache so a subsequent
				// crop measures the image the still-persisted source URL
				// describes. A failed restore is surfaced, not swallowed
				// (parity with the field-override path).
				if rollbackErr := rollback(); rollbackErr != nil {
					errMsg = fmt.Sprintf("%s (poster rollback failed: %v)", errMsg, rollbackErr)
				}
			}
			logging.Errorf("Failed to update movie for %s: %v", updateFailPath, updateErr)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: errMsg})
			return
		}
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

		// Serialize the crop against poster-source refreshes: CropWithBounds
		// measures the cached {posterID}-full.jpg while UpdatePosterCrop +
		// PersistJobByID attach the result to the movie. Without the shared
		// per-(job, movie) lock, a concurrent whole-movie PATCH or poster-url
		// field override can refresh the cache from image B and persist B's
		// URL between this request's crop of image A and its state update —
		// attaching A's preview and A-measured bounds to the movie now at B,
		// so Organize crops the wrong region. Held across source resolution,
		// the crop itself, the state update, and the persistence. posterID is
		// the same key updateBatchMovie and ApplyFieldOverride use (Movie.ID
		// when set, FileMatchInfo.MovieID otherwise). Conversely, a refresh
		// that wins the race after this crop persisted clears these bounds via
		// its source-change invalidation, and a refresh that finished first is
		// accounted for via the post-lock re-read below.
		// Lock ordering: this endpoint acquires ONLY this lock — no
		// overrideMu, matching updateBatchMovie; ApplyFieldOverride takes its
		// per-resultID overrideMu BEFORE this lock and no path reverses that.
		// The store-internal locks inside UpdatePosterCrop/PersistJobByID are
		// taken while this lock is held, but no path acquires this lock while
		// holding one of those, so the acquisition order is cycle-free.
		releasePosterLock := worker.AcquirePosterSourceLock(jobID, posterID)
		defer func() { releasePosterLock() }()

		// Re-read the result under the lock: a source-changing edit may have
		// persisted while this request waited, replacing the movie — and the
		// crop-intent flags the SourceWasCover recording below reads — that
		// the pre-lock lookup saw. Keep the pre-lock result on a miss: the
		// state update degrades to a no-op for a vanished result either way.
		//
		// That edit can also RE-KEY the result: a rescrape that corrected the
		// match from movie A to movie B holds (or held) A's lock and commits
		// the result with FileMatchInfo.MovieID/Movie.ID = B. Refreshing only
		// the result here would leave this request cropping A's cached source
		// and calling UpdatePosterCrop(A, ...) — hitting every result still at
		// A — while returning success for B. So BOTH the movie ID and the
		// poster lock key are re-resolved from the post-lock state; when the
		// key changed, A's lock is released and B's acquired, then the result
		// is re-read once more under B (it may have been re-keyed yet again by
		// the writer that released B). The release-then-acquire handoff never
		// holds two poster-source locks at once, so it introduces no lock-
		// ordering cycle (the order stays: overrideMu → ONE poster-source
		// lock → result-store locks), and the loop converges because each
		// re-acquisition waits behind a writer whose re-key is already
		// committed.
		for {
			if fresh, _, stillFound := job.GetFileResultByResultID(resultID); stillFound && fresh != nil {
				result = fresh
			}
			resolvedMovieID := result.FileMatchInfo.MovieID
			resolvedPosterID, resolveErr := resolvePosterID(job, resolvedMovieID)
			if resolveErr != nil {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: resolveErr.Error()})
				return
			}
			if resolvedPosterID == posterID {
				movieID = resolvedMovieID
				break
			}
			// movieID is (re-)resolved from the result on every
			// iteration, so only the lock key carries across here.
			releasePosterLock()
			posterID = resolvedPosterID
			releasePosterLock = worker.AcquirePosterSourceLock(jobID, posterID)
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
			if errors.Is(err, poster.ErrLegacyPreviewSource) {
				// The full-size source is gone (legacy job): the crop box was
				// measured on the already-cropped preview and cannot survive
				// Organize, which re-downloads the full image.
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "full-size poster source unavailable for this older job; re-scrape the file or use poster-from-URL to enable manual cropping"})
				return
			}
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		croppedURL := cropResult.CroppedURL

		bounds := &models.CropBounds{
			X: req.X, Y: req.Y, Width: req.Width, Height: req.Height,
			MaxPosterHeight: maxPosterHeight,
			ImageWidth:      cropResult.SourceWidth,
			ImageHeight:     cropResult.SourceHeight,
		}
		if result.Movie != nil {
			bounds.SourceWasCover = result.Movie.Poster.ShouldCropPoster
			// Repeated crops re-measure the same source: the first crop
			// already flipped ShouldCropPoster=false, so inherit the intent
			// recorded with the existing bounds instead.
			if !bounds.SourceWasCover && result.Movie.Poster.CropBounds != nil {
				bounds.SourceWasCover = result.Movie.Poster.CropBounds.SourceWasCover
			}
		}
		if err := job.UpdatePosterCrop(movieID, croppedURL, bounds); err != nil {
			logging.Errorf("Failed to update poster crop in job state for %s: %v", movieID, err)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Failed to update job state: %v", err)})
			return
		}

		// Crop state lives only in the job envelope (CropBounds is deliberately
		// not in the movies table) — persist immediately or a restart before
		// Organize silently restores the pre-crop poster.
		deps.GetJobStore().PersistJobByID(jobID)

		resp := contracts.PosterCropResponse{
			CroppedPosterURL: croppedURL,
			PosterCropBounds: &contracts.CropBounds{
				X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height,
				MaxPosterHeight: bounds.MaxPosterHeight, ImageWidth: bounds.ImageWidth, ImageHeight: bounds.ImageHeight,
				SourceWasCover: bounds.SourceWasCover,
			},
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

		// A poster-from-URL refresh mutates the same poster state as a manual
		// crop or a source-changing PATCH/field override: DownloadFromURL
		// replaces the shared {posterID}-full.jpg and preview, then
		// UpdatePosterFromURL + PersistJobByID attach the new URL and clear
		// any recorded crop bounds. Without the shared per-(job, movie) lock
		// this sequence can interleave with those edits — e.g. a concurrent
		// crop measures the pre-refresh -full.jpg yet persists after this
		// request, attaching stale bounds to the new source. Held across the
		// download, the state update, and the persistence; the deferred
		// release covers every error/return path. posterID is the same key the
		// crop endpoint, updateBatchMovie and ApplyFieldOverride use.
		// Lock ordering: this endpoint acquires ONLY this lock (parity with
		// updateBatchMoviePosterCrop/updateBatchMovie); ApplyFieldOverride
		// takes overrideMu BEFORE it and no path reverses either order, so
		// acquisition stays cycle-free.
		releasePosterLock := worker.AcquirePosterSourceLock(jobID, posterID)
		defer releasePosterLock()

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
