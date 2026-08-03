package batch

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
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
		// here across the asset migration/refresh, the multipart UpdateMovie
		// loop (including compensation) and the final PersistJobByID; the
		// deferred release covers every error/return path and always
		// releases whichever key was LAST acquired (the convergence loop
		// below can hand the lock off to a re-keyed movie) plus the
		// rename-destination lock (a request that RENAMES the movie ID holds
		// the lexical key pair for the whole edit — see below).
		movie := contracts.MovieViewToModel(req.Movie)
		var renameTarget string
		var destRelease func()
		posterLockKey := posterLockKeyFor(current)
		releasePosterLock := worker.AcquirePosterSourceLock(jobID, posterLockKey)
		defer func() {
			if destRelease != nil {
				destRelease()
			}
			releasePosterLock()
		}()

		for {
			// Post-lock convergence loop — the same shape as
			// updateBatchMoviePosterCrop / updateBatchMoviePosterFromURL (see
			// their comments for the full rationale): the writer this request
			// waited behind can REPLACE or RE-KEY the result (a rescrape
			// committing a corrected match moves FileMatchInfo.MovieID/Movie.ID
			// from A to B). Refreshing only current/filePaths while still
			// holding A's lock would let the poster refresh and the whole-movie
			// writes below interleave with a crop, poster-from-URL, or field
			// override holding B's lock — pairing a freshly refreshed cache or
			// newly stored movie state with a writer that believes it owns the
			// key. So the lock key is re-resolved from the fresh post-lock
			// result on every iteration; an invalid re-resolved ID is rejected
			// (same validation as resolvePosterID) with the deferred release
			// freeing the acquired key; when the key changed, the old key's lock
			// is released BEFORE the new one is acquired (never two
			// poster-source locks at once) and the result is re-read under the
			// new lock — it may have been re-keyed yet again by the writer that
			// released it. The loop converges because each re-acquisition waits
			// behind a writer whose re-key is already committed.
			for {
				freshCurrent, freshFilePaths, freshFound := lookupResultByResultID(job, resultID)
				if !freshFound {
					c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
					return
				}
				current, filePaths = freshCurrent, freshFilePaths
				resolvedKey := posterLockKeyFor(current)
				if !validPosterLockKey(resolvedKey) {
					c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: errInvalidMovieIDForPoster.Error()})
					return
				}
				if resolvedKey == posterLockKey {
					break
				}
				releasePosterLock()
				posterLockKey = resolvedKey
				releasePosterLock = worker.AcquirePosterSourceLock(jobID, posterLockKey)
			}

			// A whole-movie PATCH can RENAME the movie ID: the request's
			// movie.ID becomes the effective key the temp poster cache and
			// every crop/preview lookup resolve, so the migration below
			// (MovePosterAssets A→B) and any source refresh must run under
			// BOTH keys' locks — holding only the converged old key would let
			// the new key's crop/edit writers interleave with the move.
			// Mirrors the id-override path's lexical pair rule
			// (jobEditorImpl.ApplyFieldOverride): when the destination sorts
			// AFTER the held key it stacks directly on top — the held key
			// keeps the converged state stable, so no re-read gap exists;
			// when it sorts BEFORE, the held key is released first, both are
			// acquired in order, and the result is re-verified (an edit could
			// have landed — and re-keyed the result — in the gap; on a change
			// BOTH locks are dropped and pairing reconverges).
			renameKey := movie.ID
			if renameKey == "" || renameKey == posterLockKey {
				break
			}
			if !validPosterLockKey(renameKey) {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: errInvalidMovieIDForPoster.Error()})
				return
			}
			if renameKey > posterLockKey {
				destRelease = worker.AcquirePosterSourceLock(jobID, renameKey)
				renameTarget = renameKey
				break
			}
			originKey := posterLockKey
			releasePosterLock()
			destRelease = worker.AcquirePosterSourceLock(jobID, renameKey)
			releasePosterLock = worker.AcquirePosterSourceLock(jobID, originKey)
			verify, verifyPaths, verifyFound := lookupResultByResultID(job, resultID)
			if !verifyFound {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
				return
			}
			if posterLockKeyFor(verify) == originKey {
				current, filePaths = verify, verifyPaths
				renameTarget = renameKey
				break
			}
			// The gap re-keyed the result: drop the pair and reconverge (the
			// inner loop re-validates and re-resolves below).
			destRelease()
			destRelease = nil
			releasePosterLock()
			posterLockKey = posterLockKeyFor(verify)
			releasePosterLock = worker.AcquirePosterSourceLock(jobID, posterLockKey)
		}

		// Convert once and re-derive display_title so a title edit is reflected
		// immediately in persisted state and any client that renders display_title
		// (grid cards, metadata headers) without waiting for organize. Title is
		// never modified — RenderDisplayTitle only writes DisplayTitle — so this
		// cannot reintroduce the title-doubling bug this PR fixes. If the workflow
		// factory is unavailable, fall back to DisplayTitle = Title, matching the
		// canonical no-template/error degradation (display_title.go).
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
		// Nil-movie edge (F-G): with no stored movie there is no prior crop intent
		// or stored bounds to invalidate, so the intent-sync / bounds-invalidation
		// block above is deliberately skipped and the client-sent ShouldCropPoster
		// stands; the cache refresh below is NOT skipped — a first-time PATCHed
		// source must still populate the poster cache.
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
		// UpdateMovie or envelope persistence below fails, surface a failed
		// rollback.
		//
		// This runs even when the stored result has NO movie yet (the nil-movie
		// edge): persisting a PATCHed-in source without the cache would
		// otherwise leave the reviewer cropping the stale/none image while
		// Organize downloads the new one. The old effective source is empty
		// then, so any posted source regenerates from scratch. A PATCH that
		// RENAMES the movie ID additionally MIGRATES the cached poster assets
		// old→new under BOTH keys' locks (worker.MigratePosterCacheAssets,
		// mirroring the id-override path) and re-points the persisted
		// CroppedPosterURL/OriginalCroppedPosterURL to the new key — the
		// refresh below alone would leave the old key's
		// {oldID}-full.jpg/preview orphaned while every crop/preview lookup
		// resolves the new key. A failed refresh afterwards reverses the
		// migration best-effort before the edit is rejected.
		var rollback func() error
		var oldPosterURL, oldCoverURL string
		if current.Movie != nil {
			oldPosterURL, oldCoverURL = current.Movie.Poster.PosterURL, current.Movie.Poster.CoverURL
		}
		var moveAssetsBack func() error
		if renameTarget != "" {
			back, moveErr := worker.MigratePosterCacheAssets(rt.Snapshot().PosterGen(), jobID, posterLockKey, renameTarget)
			if moveErr != nil {
				logging.Errorf("Failed to migrate poster assets for renamed movie edit on result %s: %v", resultID, moveErr)
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Failed to migrate poster assets: %v", moveErr)})
				return
			}
			moveAssetsBack = back
			movie.Poster.CroppedPosterURL = worker.RewritePosterIDInPreviewURL(movie.Poster.CroppedPosterURL, posterLockKey, renameTarget)
			movie.Poster.OriginalCroppedPosterURL = worker.RewritePosterIDInPreviewURL(movie.Poster.OriginalCroppedPosterURL, posterLockKey, renameTarget)
		}
		rollback, refreshErr := worker.RefreshPosterAssets(c.Request.Context(), rt.Snapshot().PosterGen(), jobID, movie, oldPosterURL, oldCoverURL)
		if refreshErr != nil {
			// The rename migration already ran: reverse the completed legs so
			// the old key is not left empty while the (rejected) edit keeps
			// the persisted state at the old movie ID.
			errMsg := compensateMoveBack(moveAssetsBack, fmt.Sprintf("Failed to refresh poster source: %v", refreshErr))
			logging.Errorf("Failed to refresh poster source after whole-movie edit for result %s: %s", resultID, errMsg)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: errMsg})
			return
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
		// Whole-job envelope serialization (worker.AcquireJobEnvelopeLock — the
		// API-handler parity of the rescrape phase's commit window): a concurrent
		// crop, poster-from-URL, or field override on a DIFFERENT movie of this
		// job holds only its own poster-source lock, so without per-job
		// serialization a peer's whole-envelope persist could durably capture
		// THIS request's just-committed part updates — which the
		// UpdateMovie/persist failure branches below then roll back in memory,
		// resurrecting the rejected edit on restart. Held across the multipart
		// UpdateMovie loop, the final PersistJobByID, and compensateEdit;
		// acquired AFTER the poster-source lock(s) (ordering poster → envelope)
		// so the deferred release runs before theirs (LIFO). The asset
		// migration/refresh above is cache-level (not envelope state), so it
		// stays outside this window.
		releaseEnvelopeLock := worker.AcquireJobEnvelopeLock(jobID)
		defer func() { releaseEnvelopeLock() }()

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
		// compensateEdit reverts every part already updated in this request and
		// rolls the refreshed poster cache back, in that order: restoring the
		// cache while a part still holds the new poster source URL would desync
		// job state from the cached -full.jpg. Every failure is surfaced in the
		// returned message, never swallowed. Shared by the UpdateMovie-failure
		// branch and the envelope-persist-failure branch — the latter can
		// strand the exact same divergence on restart (reconstructBatchJob reads
		// only the envelope).
		compensateEdit := func(errMsg string) string {
			for _, part := range updated {
				if part.original == nil {
					continue
				}
				if revertErr := job.UpdateMovie(c.Request.Context(), part.filePath, part.original); revertErr != nil {
					errMsg = fmt.Sprintf("%s (revert of part %s failed: %v)", errMsg, part.filePath, revertErr)
				}
			}
			if rollback != nil {
				if rollbackErr := rollback(); rollbackErr != nil {
					errMsg = fmt.Sprintf("%s (poster rollback failed: %v)", errMsg, rollbackErr)
				}
			}
			// Asset moves run LAST: after a rename the cache rollback restored
			// the new key's post-move snapshot, and this moves those assets
			// back to the old key — no part holds the renamed state anymore.
			return compensateMoveBack(moveAssetsBack, errMsg)
		}
		if updateErr != nil {
			errMsg := compensateEdit(fmt.Sprintf("Failed to update movie: %v", updateErr))
			logging.Errorf("Failed to update movie for %s: %v", updateFailPath, updateErr)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: errMsg})
			return
		}
		// A failed envelope persist is surfaced, not swallowed: in-memory edits
		// that never reach the database would be silently dropped by a restart
		// while the client believes they succeeded. The same compensation as the
		// UpdateMovie-failure branch keeps restart state coherent: the in-memory
		// parts revert to their pre-request movies and a successful poster
		// refresh rolls back, so the envelope the restart reconstructs and the
		// poster cache describe the same image.
		if perr := deps.GetJobStore().PersistJobByID(jobID); perr != nil {
			errMsg := compensateEdit(fmt.Sprintf("Failed to persist job state: %v", perr))
			logging.Errorf("Failed to persist movie edit for job %s: %v", jobID, perr)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: errMsg})
			return
		}
		c.JSON(http.StatusOK, contracts.MovieResponse{Movie: contracts.MovieViewFromModel(movie)})
	}
}

// compensateMoveBack appends the rename-migration reversal to a PATCH
// failure message. moveAssetsBack is nil for non-rename edits; a failed
// reversal rides along on the message instead of being swallowed.
func compensateMoveBack(moveAssetsBack func() error, errMsg string) string {
	if moveAssetsBack == nil {
		return errMsg
	}
	if backErr := moveAssetsBack(); backErr != nil {
		return fmt.Sprintf("%s (poster asset move-back failed: %v)", errMsg, backErr)
	}
	return errMsg
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
