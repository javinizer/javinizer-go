package batch

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

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

		// Snapshot the cached assets BEFORE CropWithBounds overwrites the
		// preview ({posterID}.jpg) so a failed envelope persist below can
		// restore the pre-crop cache — parity with the poster-from-URL
		// endpoint and the whole-movie PATCH refresh path (the AssetsSnapshot
		// machinery captures full+preview). Without it the revert keeps the
		// persisted movie at its old bounds+intent while the shared preview
		// file shows the REJECTED crop: an uncached reload displays the
		// rejected crop while Organize applies the old/default one. Snapshot
		// failures reject the request — never crop against a cache state the
		// request cannot roll back (manager.SnapshotAssets documents the same
		// covenant).
		assetSnap, snapErr := snap.PosterManager().SnapshotAssets(jobID, posterID)
		if snapErr != nil {
			logging.Errorf("Failed to snapshot poster assets before manual crop for %s: %v", posterID, snapErr)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Failed to snapshot poster assets: %v", snapErr)})
			return
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
		// Snapshot every part's pre-crop movie so a failed envelope persist can
		// revert the in-memory crop EXACTLY. UpdatePosterCrop mutates three
		// things per part (CroppedPosterURL preview, ShouldCropPoster=false,
		// CropBounds) and may lazily stamp the Original* backup baseline, so a
		// revert must restore the whole pre-crop movie per part — replaying
		// UpdatePosterCrop with the old bounds would leave ShouldCropPoster at
		// false even when the pre-crop intent was true. Mirrored from the
		// poster-from-URL compensation below. GetMovieResult clones, so the
		// atomic crop update cannot alias these.
		origMovies := make(map[string]*models.Movie)
		for _, fp := range job.FindFilePathsForMovieID(movieID) {
			if prev, gErr := job.GetMovieResult(fp); gErr == nil && prev != nil && prev.Movie != nil {
				origMovies[fp] = prev.Movie
			}
		}

		if err := job.UpdatePosterCrop(movieID, croppedURL, bounds); err != nil {
			logging.Errorf("Failed to update poster crop in job state for %s: %v", movieID, err)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Failed to update job state: %v", err)})
			return
		}

		// Crop state lives only in the job envelope (CropBounds is deliberately
		// not in the movies table) — persist immediately or a restart before
		// Organize silently restores the pre-crop poster. A failed persist must
		// NOT be acknowledged as success: the crop exists only in memory, so the
		// client would believe the crop is durable while a restart silently drops
		// it. Surface the failure as a 5xx (the upsert failure is also recorded
		// on the job's PersistError, unchanged) — and revert the in-memory crop
		// per part, so memory matches the unpersisted envelope instead of
		// letting Organize apply bounds a restart would drop (F7) — and the
		// pre-crop cache bytes come back (the CropWithBounds-overwritten
		// preview would otherwise keep showing the rejected crop while the
		// reverted movie applies the old bounds, Codex P2). The part reverts
		// run before the cache restore so no in-memory result references the
		// rejected crop while the cache flips back (the same ordering the
		// poster-from-URL compensate documents). The poster-source lock is
		// still held (deferred above), so the revert is serialized with the
		// same edits the crop itself was.
		if perr := deps.GetJobStore().PersistJobByID(jobID); perr != nil {
			errMsg := fmt.Sprintf("Failed to persist job state: %v", perr)
			for fp, orig := range origMovies {
				if revertErr := job.UpdateMovie(c.Request.Context(), fp, orig); revertErr != nil {
					errMsg = fmt.Sprintf("%s (revert of part %s failed: %v)", errMsg, fp, revertErr)
				}
			}
			if restoreErr := snap.PosterManager().RestoreAssets(assetSnap); restoreErr != nil {
				errMsg = fmt.Sprintf("%s (poster rollback failed: %v)", errMsg, restoreErr)
			}
			logging.Errorf("Failed to persist poster crop for job %s: %v", jobID, perr)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: errMsg})
			return
		}

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
		defer func() { releasePosterLock() }()

		// Re-resolve under the lock — the same convergence loop as
		// updateBatchMoviePosterCrop (see its comment for the full rationale):
		// a source-changing edit that waited ahead of this request can REPLACE
		// or RE-KEY the result (a rescrape committing a corrected match moves
		// FileMatchInfo.MovieID/Movie.ID from A to B). Refreshing only the
		// result would leave this request downloading into A's
		// {posterID}-full.jpg cache and calling UpdatePosterFromURL(A, ...) —
		// modifying the results still at A while returning success for B. So
		// BOTH the movie ID and the lock key are re-resolved from the
		// post-lock state; when the key changed, A's lock is released and B's
		// acquired (release-before-acquire — never two poster-source locks,
		// per the two-lock rule in worker.AcquirePosterSourceLock), then the
		// result is re-read once more under B. The loop converges because each
		// re-acquisition waits behind a writer whose re-key is already
		// committed. On a miss the pre-lock result is kept: the state update
		// degrades to a no-op for a vanished result either way.
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

		// Snapshot so apiCfg (user-agent/referer) and the poster manager see the
		// same reload epoch (issue #44).
		snap := rt.Snapshot()
		batchCfg := snap.APIConfig().BatchConfig()

		// Snapshot the cached assets BEFORE DownloadFromURL replaces them so a
		// later state-update or envelope-persist failure can restore the
		// pre-download cache (parity with RefreshPosterAssets'
		// snapshot/rollback): without it a failed persist leaves restart-
		// reconstructed (pre-URL) state pointing at a cache that holds the new
		// image. Snapshot failures reject the request — the caller must not
		// regenerate against a cache state it cannot roll back
		// (manager.SnapshotAssets documents the same covenant).
		assetSnap, snapErr := snap.PosterManager().SnapshotAssets(jobID, posterID)
		if snapErr != nil {
			logging.Errorf("Failed to snapshot poster assets before poster-from-URL for %s: %v", posterID, snapErr)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Failed to snapshot poster assets: %v", snapErr)})
			return
		}

		// Capture the pre-request per-part movies for compensation: a failure
		// after the download reverts the in-memory fan-out (mirroring the
		// whole-movie PATCH compensation) before the cache restore runs, so no
		// part keeps the new poster URL while the cache holds the old image.
		origMovies := make(map[string]*models.Movie)
		for _, fp := range job.FindFilePathsForMovieID(movieID) {
			if prev, gErr := job.GetMovieResult(fp); gErr == nil && prev != nil {
				origMovies[fp] = prev.Movie
			}
		}
		compensate := func(errMsg string) string {
			for fp, orig := range origMovies {
				if orig == nil {
					continue
				}
				if revertErr := job.UpdateMovie(c.Request.Context(), fp, orig); revertErr != nil {
					errMsg = fmt.Sprintf("%s (revert of part %s failed: %v)", errMsg, fp, revertErr)
				}
			}
			if restoreErr := snap.PosterManager().RestoreAssets(assetSnap); restoreErr != nil {
				errMsg = fmt.Sprintf("%s (poster rollback failed: %v)", errMsg, restoreErr)
			}
			return errMsg
		}

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
			// The state update failed after the download already replaced the
			// cached assets: restore them so the still-old job state and the
			// cache keep describing the same image.
			errMsg := compensate(fmt.Sprintf("Failed to update job state: %v", err))
			logging.Errorf("Failed to update poster from URL in job state for %s: %v", movieID, err)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: errMsg})
			return
		}
		// Same swallowed-persist class as the crop endpoint: the URL/bounds
		// change lives in the job envelope, so a failed persist must surface as
		// a 5xx instead of a false 200 ack — and the in-memory result must
		// revert with the cache restored (compensate), otherwise a restart
		// would resurrect pre-download job state while the cache holds the
		// downloaded image.
		if perr := deps.GetJobStore().PersistJobByID(jobID); perr != nil {
			errMsg := compensate(fmt.Sprintf("Failed to persist job state: %v", perr))
			logging.Errorf("Failed to persist poster-from-URL for job %s: %v", jobID, perr)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: errMsg})
			return
		}

		// Read the crop intent back from the post-update job state (still under
		// the poster-source lock): UpdatePosterFromURL derives ShouldCropPoster
		// from the PRIOR effective source / provenance, so the server — not the
		// request — owns the value. The client overlays it verbatim; omitting it
		// would let the client default to false and a later whole-movie Save
		// would resubmit that false while poster_source is unchanged, which
		// updateBatchMovie treats as a deliberate crop-intent edit.
		shouldCrop := false
		if updated, _, stillFound := job.GetFileResultByResultID(resultID); stillFound && updated != nil && updated.Movie != nil {
			shouldCrop = updated.Movie.Poster.ShouldCropPoster
		}
		c.JSON(http.StatusOK, contracts.PosterFromURLResponse{
			CroppedPosterURL: croppedURL,
			PosterURL:        req.URL,
			ShouldCropPoster: shouldCrop,
		})
	}
}

// errInvalidMovieIDForPoster is the rejection shared by every endpoint that
// resolves a poster-operation key from job state (pre-lock resolution and the
// post-lock convergence re-resolution alike).
var errInvalidMovieIDForPoster = errors.New("invalid movie ID for poster operation")

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
	if !validPosterLockKey(posterID) {
		return "", errInvalidMovieIDForPoster
	}
	return posterID, nil
}

// posterLockKeyFor derives the shared poster-source lock key for a stored movie
// result: Movie.ID when set, FileMatchInfo.MovieID otherwise — the same
// precedence the temp poster cache and the override path key on, and the key
// updateBatchMovie's convergence loop re-resolves from fresh post-lock state.
func posterLockKeyFor(result *resultstore.MovieResult) string {
	key := result.FileMatchInfo.MovieID
	if result.Movie != nil && result.Movie.ID != "" {
		key = result.Movie.ID
	}
	return key
}

// validPosterLockKey is the safe-filename validation (path traversal check)
// shared by resolvePosterID and the post-lock re-resolved keys: the poster
// cache paths are built from this key, so it must be a plain file name.
func validPosterLockKey(posterID string) bool {
	return posterID == filepath.Base(posterID) && posterID != "" && posterID != "."
}
