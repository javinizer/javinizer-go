package batch

import (
	"errors"
	"fmt"

	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

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
// @Failure 409 {object} contracts.ErrorResponse "job busy (pending or scrape-phase)"
// @Failure 410 {object} contracts.ErrorResponse "job deleted"
// @Failure 500 {object} contracts.ErrorResponse "transactional commit failed; all writes rolled back"
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

		// Admission gate + lease (D1/D3/D16), held through measure+commit+response.
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

		// Resolve the max poster height: request-level override wins over the
		// configured default. 0 means no cap (preserve source resolution).
		// Snapshot so apiCfg and the poster manager see the same reload epoch (issue #44).
		snap := rt.Snapshot()
		maxPosterHeight := snap.APIConfig().BatchConfig().MaxPosterHeight
		if req.MaxPosterHeight != nil {
			maxPosterHeight = *req.MaxPosterHeight
		}

		// Measure + commit under ONE family key (POSTER-WRITE-HARDENING D1):
		// a concurrent PATCH/crop to the same movie can no longer straddle the
		// measure-commit pair, and the geometry commit lands in the same
		// composite envelope tx. The preview write is backed up first so a
		// failed commit restores the previous bytes (codex P4-B).
		var bounds *models.CropBounds
		var croppedURL string
		var cropErr error
		var echoRev *uint64
		var echoFam map[string]uint64
		var promoteErr error
		opErr := job.WithMovieEditLock(movieID, func(m *worker.LockedMovieOps) error {
			// Re-resolve the result family INSIDE the key (codex r38): if a
			// rekey moved the result to another movie between the handler's
			// pre-lock read and this lock acquisition, refusing guards against
			// cropping a stale sibling.
			if cur, _, rfound := job.GetFileResultByResultID(resultID); rfound && cur != nil && cur.FileMatchInfo.MovieID != "" &&
				!strings.EqualFold(strings.TrimSpace(cur.FileMatchInfo.MovieID), strings.TrimSpace(movieID)) {
				return &worker.EditAdmissionConflictError{Message: fmt.Sprintf("result %s moved to family %s during crop; retry", resultID, cur.FileMatchInfo.MovieID)}
			}
			// Re-resolve the poster ID inside the key (codex r26): a concurrent
			// rekey-PATCH may have moved the preview files to a NEW ID — the
			// pre-lock read would then crop stale-or-missing bytes.
			posterID, err := resolvePosterID(job, movieID)
			if err != nil {
				cropErr = err
				return nil //nolint:nilerr // captured to cropErr; mapped to 400 after lock release
			}
			// codex r51 P2/durability: the crop writes to a STAGED name and promotes
			// over the canonical pair only AFTER the state commit lands — a crash
			// mid-op leaves the canonical untouched (pre-commit) or a staged pair
			// plus a witness the startup reconciler completes. The crop manager
			// resolves its source by the handed name, so stage BOTH source legs
			// first; canonical <posterID>.jpg is never rewritten pre-commit.
			fs := rt.Deps().GetFs()
			tmpDir := snap.APIConfig().TempDir
			dir := filepath.Join(tmpDir, "posters", jobID)
			stageID := nextPosterStageID(posterID, "crop")
			for _, leg := range [][2]string{{posterID + "-full.jpg", stageID + "-full.jpg"}, {posterID + ".jpg", stageID + ".jpg"}} {
				data, rerr := afero.ReadFile(fs, filepath.Join(dir, leg[0]))
				if rerr != nil {
					if !os.IsNotExist(rerr) {
						// codex P2: only ABSENCE permits skipping a leg — a transient
						// read error must abort, else CropWithBounds silently falls
						// back to the cropped image while the UI measured its
						// coordinates against the full-size source.
						cleanupStagedPosterPair(fs, tmpDir, jobID, stageID)
						return &worker.EditAdmissionConflictError{Message: fmt.Sprintf("poster crop staging source %s: %v", leg[0], rerr)}
					}
					continue
				}
				if werr := afero.WriteFile(fs, filepath.Join(dir, leg[1]), data, 0o644); werr != nil {
					cleanupStagedPosterPair(fs, tmpDir, jobID, stageID)
					return &worker.EditAdmissionConflictError{Message: fmt.Sprintf("poster crop staging %s: %v", leg[1], werr)}
				}
			}
			cropResult, err := snap.PosterManager().CropWithBounds(c.Request.Context(), jobID, stageID, req.X, req.Y, req.Width, req.Height, maxPosterHeight)
			if err != nil {
				cleanupStagedPosterPair(fs, tmpDir, jobID, stageID)
				cropErr = err
				return nil //nolint:nilerr // captured to cropErr; mapped to 400 after lock release
			}
			croppedURL = strings.Replace(cropResult.CroppedURL, url.PathEscape(stageID)+".jpg", url.PathEscape(posterID)+".jpg", 1)
			// Persist the manual crop geometry (normalized 0–1 fractions) next
			// to the preview URL so the apply phase can reproduce the crop on
			// the downloaded source image. Bounds are validated in integer
			// pixel space against the measured source dimensions, then
			// normalized and re-validated. When the legacy already-cropped
			// fallback served as the source no applyable geometry exists.
			if cropResult.SourceFull && cropResult.SourceWidth > 0 && cropResult.SourceHeight > 0 {
				bounds = &models.CropBounds{
					X:            float64(req.X) / float64(cropResult.SourceWidth),
					Y:            float64(req.Y) / float64(cropResult.SourceHeight),
					Width:        float64(req.Width) / float64(cropResult.SourceWidth),
					Height:       float64(req.Height) / float64(cropResult.SourceHeight),
					SourceAspect: float64(cropResult.SourceWidth) / float64(cropResult.SourceHeight),
				}
			}
			// Durable witness BEFORE the commit — authorizes the reconciler to
			// complete the promote if we crash between commit and promote.
			var prerev uint64
			if live, _, found := job.GetFileResultByResultID(resultID); found && live != nil {
				prerev = live.Revision
			}
			cwPath, werr := writeCropWitnessGuarded(fs, tmpDir, jobID, cropWitness{
				PosterID: posterID, ResultID: resultID, StageID: stageID,
				CroppedURL: croppedURL, PrevRevision: prerev,
			})
			if werr != nil {
				cleanupStagedPosterPair(fs, tmpDir, jobID, stageID)
				return &worker.EditAdmissionConflictError{Message: fmt.Sprintf("poster crop witness: %v", werr)}
			}
			cerr := m.UpdatePosterCrop(croppedURL, bounds, bounds != nil)
			if cerr != nil {
				// Commit never landed: canonical untouched — just drop the staged
				// bytes and the witness.
				cleanupStagedPosterPair(fs, tmpDir, jobID, stageID)
				removeCropWitness(fs, cwPath)
				return cerr
			}
			if pErr := promoteCroppedLegWithRetry(fs, tmpDir, jobID, stageID, posterID); pErr != nil {
				// Committed but promotion failed even after immediate retries: KEEP
				// the witness — the startup reconciler completes the promote, and
				// the guarded witness write FENCES any later crop for this poster
				// (409) until reconciliation, so a stale stage can never promote
				// over a newer crop (codex P2 fence-followup). The handler surfaces the
				// deferred state as a 500 instead of a false 200 (local codex review P1).
				promoteErr = pErr
				logging.Warnf("crop promote %s→%s failed after retries (witness retained; crops fenced until reconciliation): %v", stageID, posterID, pErr)
			} else {
				removeCropWitness(fs, cwPath)
				// r52 P2: free the staged full-size copy left over from the
				// crop stage (only the crop leg is promoted; the reconcile arm
				// handles leftovers when the witness survives).
				if rmErr := fs.Remove(filepath.Join(tmpDir, "posters", jobID, stageID+"-full.jpg")); rmErr != nil && !os.IsNotExist(rmErr) {
					logging.Warnf("crop staged-full sweep %s: %v", stageID+"-full.jpg", rmErr)
				}
			}
			// audit F-R7-2: capture the CAS echo INSIDE the family key — reading
			// revisions after release would race a concurrent commit, healing
			// the client's baseline past content it never adopted.
			echoRev = currentResultRevision(job, resultID)
			echoFam = familyRevisions(job, resultID)
			return nil
		})
		if cropErr != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: cropErr.Error()})
			return
		}
		if opErr != nil {
			logging.Errorf("Failed to update poster crop in job state for %s: %v", movieID, opErr)
			writeEditOpError(c, opErr)
			return
		}
		// No PersistJobByID here — the crop commit IS the envelope write (D4).
		if promoteErr != nil {
			// local codex review P1: never answer 200 with the fresh URL while the
			// canonical bytes are still stale — report the deferred reconcile.
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("poster crop committed but canonical byte promotion failed after retries: %v — the stored image reconciles on next startup and further crop attempts are fenced until then", promoteErr)})
			return
		}

		// Echo the server-side baseline snapshot so the client Reset flow
		// restores exactly what the server would (no client-side guessing).
		resp := contracts.PosterCropResponse{CroppedPosterURL: croppedURL, PosterCropBounds: bounds, ShouldCropPoster: false, PosterCropSourceFull: bounds != nil, Revision: echoRev, Revisions: echoFam}
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
// @Failure 409 {object} contracts.ErrorResponse "job busy (pending or scrape-phase)"
// @Failure 410 {object} contracts.ErrorResponse "job deleted"
// @Failure 500 {object} contracts.ErrorResponse "transactional commit failed; all writes rolled back"
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

		// Admission gate + lease (D1/D3/D16). The lease spans the unlocked
		// network stage, the locked commit, and the response, so DeleteJob can
		// never reclaim the job's temp dir mid-op.
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

		posterID, err := resolvePosterID(job, movieID)
		if err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		// Snapshot so apiCfg (user-agent/referer) and the poster manager see the
		// same reload epoch (issue #44).
		snap := rt.Snapshot()
		batchCfg := snap.APIConfig().BatchConfig()

		// Stage → promote → commit (codex r18 / D6-lite): the network download
		// lands on a unique staged ID OUTSIDE the family key, so a slow remote
		// server never holds the lock window. Under the key we promote staged
		// bytes into the canonical names with a backup restore on failure.
		// Revision captured BEFORE the download — a stale revision on the
		// retry would compare against the post-download source.
		scalarRev := result.Revision
		stageID := nextPosterStageID(posterID, "stage")
		var dlErr error
		var croppedURL string
		var echoRev *uint64
		var echoFam map[string]uint64
		posterResult, err := snap.PosterManager().DownloadFromURL(c.Request.Context(), jobID, stageID, req.URL, batchCfg.ScraperUserAgent, batchCfg.ScraperReferer)
		if err != nil {
			dlErr = err
		}
		var opErr error
		if dlErr == nil {
			opErr = job.WithMovieEditLock(movieID, func(m *worker.LockedMovieOps) error {
				backup := backupPosterPair(rt.Deps().GetFs(), snap.APIConfig().TempDir, jobID, posterID)
				// codex r38: family revalidation before promotion — a rekeyed
				// multipart family must not promote into the stale sibling.
				if live, _, found := job.GetFileResultByResultID(resultID); found && live != nil && live.FileMatchInfo.MovieID != "" &&
					!strings.EqualFold(strings.TrimSpace(live.FileMatchInfo.MovieID), strings.TrimSpace(movieID)) {
					backup.restore()
					return &worker.EditAdmissionConflictError{Message: fmt.Sprintf("result %s moved to family %s during the URL download; retry", resultID, live.FileMatchInfo.MovieID)}
				}
				// Revalidate under the key (codex r22): a concurrent edit between
				// the unlocked download and this section cannot be allowed to
				// promote bytes measured against a state that has moved on.
				if live, _, found := job.GetFileResultByResultID(resultID); found && live.Revision != scalarRev {
					backup.restore()
					return &worker.EditAdmissionConflictError{Message: "poster source changed while downloading; retry"}
				}
				// codex r48 P2: durable promotion witness BEFORE promote — a crash
				// between promote and commit strands uncommitted bytes at canonical
				// with the old pair only as .bak; the startup reconciler
				// (worker.ReconcileRekeyWitnesses) arbitrates it against the row.
				// codex r51 P2: refuse promotion when a witness is ALREADY
				// outstanding (an incomplete restore left .bak + witness for the
				// startup reconciler) — overwriting it would re-snapshot the
				// partially-restored pair as the "old" state.
				pwPath, werr := writePromoteWitnessGuarded(rt.Deps().GetFs(), snap.APIConfig().TempDir, jobID, posterID, req.URL, resultID, scalarRev, backup)
				if werr != nil {
					backup.restore()
					if errors.Is(werr, errPromoteWitnessPending) {
						return &worker.EditAdmissionConflictError{Message: werr.Error()}
					}
					return fmt.Errorf("poster promote witness: %w", werr)
				}
				finalize, pErr := promoteStagedPosterPair(rt.Deps().GetFs(), snap.APIConfig().TempDir, jobID, stageID, posterID)
				if pErr != nil {
					// codex r48-followup P2: sweep the witness ONLY when the byte
					// restore is complete — a partial restore keeps .bak + witness
					// for the startup reconciler.
					if backup.restore() {
						removePromoteWitness(rt.Deps().GetFs(), pwPath)
					}
					return pErr
				}
				// Commit failure also finalizes: it removes the parked .bak
				// files (the backup restore returns canonical bytes), so a
				// failed from-URL+commit leaves no backup litter (codex r27).
				croppedURL = strings.Replace(posterResult.CroppedURL, url.PathEscape(stageID)+".jpg", url.PathEscape(posterID)+".jpg", 1)
				cerr := m.UpdatePosterFromURL(c.Request.Context(), req.URL, croppedURL)
				if cerr != nil {
					// reap parking + witness ONLY on a complete restore (r48-fu P2):
					// partial restores leave markers for the startup reconciler.
					if backup.restore() {
						finalize()
						removePromoteWitness(rt.Deps().GetFs(), pwPath)
					}
					return cerr
				}
				finalize() // reap .bak parking FIRST — a crash here strands
				// backups, but the witness survives to tell the reconciler.
				removePromoteWitness(rt.Deps().GetFs(), pwPath)
				// audit F-R7-2: CAS echo inside the key (see crop handler).
				echoRev = currentResultRevision(job, resultID)
				echoFam = familyRevisions(job, resultID)
				return nil
			})
			cleanupStagedPosterPair(rt.Deps().GetFs(), snap.APIConfig().TempDir, jobID, stageID)
		}
		if dlErr != nil {
			if strings.Contains(dlErr.Error(), "SSRF") || strings.Contains(dlErr.Error(), "invalid URL") {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: dlErr.Error()})
			} else if strings.Contains(dlErr.Error(), "download") || strings.Contains(dlErr.Error(), "status") {
				c.JSON(http.StatusBadGateway, contracts.ErrorResponse{Error: dlErr.Error()})
			} else {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: dlErr.Error()})
			}
			return
		}
		if opErr != nil {
			logging.Errorf("Failed to update poster from URL in job state for %s: %v", movieID, opErr)
			writeEditOpError(c, opErr)
			return
		}

		c.JSON(http.StatusOK, contracts.PosterFromURLResponse{
			CroppedPosterURL: croppedURL,
			PosterURL:        req.URL,
			Revision:         echoRev,
			Revisions:        echoFam,
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
