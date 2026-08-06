package batch

import (
	"fmt"

	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"

	"github.com/gin-gonic/gin"
	"github.com/spf13/afero"

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
		opErr := job.UpdateMovieFamily(c.Request.Context(), movieID, resultID, movie, worker.FamilySaveOptions{CarryCropGeometry: !req.PosterCropBoundsFieldPresent, ExpectedResultRevision: req.ExpectedResultRevision, ExpectedResultRevisions: req.ExpectedResultRevisions})
		if opErr != nil {
			logging.Errorf("Failed to update movie family %s: %v", movieID, opErr)
			writeEditOpError(c, fmt.Errorf("failed to update movie: %w", opErr))
			return
		}

		c.JSON(http.StatusOK, contracts.MovieResponse{Movie: contracts.MovieViewFromModel(movie), Revision: currentResultRevision(job, resultID), Revisions: familyRevisions(job, resultID)})
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
			backup := backupPosterPair(rt.Deps().GetFs(), snap.APIConfig().TempDir, jobID, posterID)
			cropResult, err := snap.PosterManager().CropWithBounds(c.Request.Context(), jobID, posterID, req.X, req.Y, req.Width, req.Height, maxPosterHeight)
			if err != nil {
				cropErr = err
				backup.restore() // manager may have truncated the cropped file before failing (codex P6-C)
				return nil       //nolint:nilerr // captured to cropErr; mapped to 400 after lock release — keep legacy status
			}
			croppedURL = cropResult.CroppedURL
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
			cerr := m.UpdatePosterCrop(croppedURL, bounds, bounds != nil)
			if cerr != nil {
				backup.restore()
			}
			return cerr
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

		// Echo the server-side baseline snapshot so the client Reset flow
		// restores exactly what the server would (no client-side guessing).
		resp := contracts.PosterCropResponse{CroppedPosterURL: croppedURL, PosterCropBounds: bounds, ShouldCropPoster: false, PosterCropSourceFull: bounds != nil, Revision: currentResultRevision(job, resultID), Revisions: familyRevisions(job, resultID)}
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
		stageID := posterID + ".stage-" + fmt.Sprintf("%x", time.Now().UnixNano())
		var dlErr error
		var croppedURL string
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
				finalize, pErr := promoteStagedPosterPair(rt.Deps().GetFs(), snap.APIConfig().TempDir, jobID, stageID, posterID)
				if pErr != nil {
					backup.restore()
					return pErr
				}
				// Commit failure also finalizes: it removes the parked .bak
				// files (the backup restore returns canonical bytes), so a
				// failed from-URL+commit leaves no backup litter (codex r27).
				croppedURL = strings.Replace(posterResult.CroppedURL, url.PathEscape(stageID)+".jpg", url.PathEscape(posterID)+".jpg", 1)
				cerr := m.UpdatePosterFromURL(c.Request.Context(), req.URL, croppedURL)
				if cerr != nil {
					backup.restore()
					finalize() // reap the .bak parking spots — commit did not land
					return cerr
				}
				finalize()
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
			Revision:         currentResultRevision(job, resultID),
			Revisions:        familyRevisions(job, resultID),
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

// posterPairBackup snapshots the temp poster pair (<posterID>.jpg and
// <posterID>-full.jpg) so a failed commit can restore the previous bytes
// (POSTER-WRITE-HARDENING D4 applies to served asset bytes too — codex P4-B).
// Plain os ops: the poster manager writes these paths via OsFs in production
// and the crop tests exercise them through the test chdir trick.
type posterPairBackup struct {
	fs            afero.Fs
	dir           string
	fullPath      string
	croppedPath   string
	fullBytes     []byte
	croppedBytes  []byte
	fullExisted   bool
	croppedExists bool

	// unreadable marks files that exist but could not be snapshotted (perm /
	// I/O errors). Restore NEVER deletes them (codex r12): remove-if-absent
	// semantics apply only to files that were genuinely absent pre-op.
	fullUnreadable    bool
	croppedUnreadable bool
}

// fs must be the same afero.Fs the PosterManager writes through (codex
// P9-A: a host-os os.Open reads nothing when an injected fs backs the
// manager); callers pass rt.Deps().GetFs().
func backupPosterPair(fs afero.Fs, tempDir, jobID, posterID string) *posterPairBackup {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	b := &posterPairBackup{
		fs:          fs,
		dir:         filepath.Join(tempDir, "posters", jobID),
		fullPath:    filepath.Join(tempDir, "posters", jobID, fmt.Sprintf("%s-full.jpg", posterID)),
		croppedPath: filepath.Join(tempDir, "posters", jobID, fmt.Sprintf("%s.jpg", posterID)),
	}
	fs = b.fs
	if data, err := afero.ReadFile(fs, b.fullPath); err == nil {
		b.fullBytes = data
		b.fullExisted = true
	} else if !os.IsNotExist(err) {
		b.fullUnreadable = true
		logging.Warnf("poster rollback: %s unreadable (%v) — restore will leave it untouched", b.fullPath, err)
	}
	if data, err := afero.ReadFile(fs, b.croppedPath); err == nil {
		b.croppedBytes = data
		b.croppedExists = true
	} else if !os.IsNotExist(err) {
		b.croppedUnreadable = true
		logging.Warnf("poster rollback: %s unreadable (%v) — restore will leave it untouched", b.croppedPath, err)
	}
	return b
}

// restore rewinds the two poster files to their pre-op bytes: existing files
// are rewritten, previously-absent ones are removed. Best-effort: restore
// failures are logged (the next sweep reconciles leftovers).
func (b *posterPairBackup) restore() {
	if !b.fullExisted && !b.fullUnreadable {
		if err := b.fs.Remove(b.fullPath); err != nil && !os.IsNotExist(err) {
			logging.Warnf("poster rollback: remove %s: %v", b.fullPath, err)
		}
	} else if b.fullExisted {
		if err := afero.WriteFile(b.fs, b.fullPath, b.fullBytes, 0o644); err != nil {
			logging.Warnf("poster rollback: restore %s: %v", b.fullPath, err)
		}
	}
	if !b.croppedExists && !b.croppedUnreadable {
		if err := b.fs.Remove(b.croppedPath); err != nil && !os.IsNotExist(err) {
			logging.Warnf("poster rollback: remove %s: %v", b.croppedPath, err)
		}
	} else if b.croppedExists {
		if err := afero.WriteFile(b.fs, b.croppedPath, b.croppedBytes, 0o644); err != nil {
			logging.Warnf("poster rollback: restore %s: %v", b.croppedPath, err)
		}
	}
}

// promoteStagedPosterPair atomically renames the staged poster files into
// the canonical <posterID> names (codex r18): callers run this inside the
// family key; a backupPosterPair taken just before covers commit-failure
// rollback.
// promoteStagedPosterPair relocates the staged poster files into the
// canonical <posterID> names and returns `finalize`; callers MUST run
// finalize only AFTER the state commit lands (codex r22: .bak rotation
// survives until the commit witness, so a crash can be reconciled).
func promoteStagedPosterPair(fs afero.Fs, tempDir, jobID, stageID, posterID string) (finalize func(), err error) {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	dir := filepath.Join(tempDir, "posters", jobID)
	srcs := []struct{ src, dst string }{
		{filepath.Join(dir, stageID+"-full.jpg"), filepath.Join(dir, posterID+"-full.jpg")},
		{filepath.Join(dir, stageID+".jpg"), filepath.Join(dir, posterID+".jpg")},
	}
	// Promote: park canonical → staged-rename; .bak files persist until the
	// caller's finalize runs at the commit witness. Mid-promote failure
	// reverses whatever was moved (unpark + un-promote) so a partial error
	// leaves the canonical pair untouched and no .bak litter (codex r19+r28).
	var parked []string
	var promoted []string
	rollbackPromote := func() {
		// un-promote the already-installed new bytes (they were never committed)
		for i := len(promoted) - 1; i >= 0; i-- {
			if rbErr := fs.Remove(promoted[i]); rbErr != nil && !os.IsNotExist(rbErr) {
				logging.Warnf("poster promote unpromote %s: %v", promoted[i], rbErr)
			}
		}
		for _, bak := range parked {
			orig := strings.TrimSuffix(bak, ".bak")
			if rbErr := fs.Rename(bak, orig); rbErr != nil {
				logging.Warnf("poster promote un->park %s: %v", bak, rbErr)
			}
		}
	}
	for _, mv := range srcs {
		if _, err := fs.Stat(mv.src); err != nil {
			if os.IsNotExist(err) {
				continue // manager may not have produced this leg
			}
			rollbackPromote()
			return nil, err
		}
		bak := mv.dst + ".bak"
		_ = fs.Remove(bak)
		if _, err := fs.Stat(mv.dst); err == nil {
			if err := fs.Rename(mv.dst, bak); err != nil {
				rollbackPromote()
				return nil, fmt.Errorf("park previous poster %s: %w", mv.dst, err)
			}
			parked = append(parked, bak)
		}
		if err := fs.Rename(mv.src, mv.dst); err != nil {
			rollbackPromote()
			return nil, fmt.Errorf("promote staged poster %s: %w", mv.src, err)
		}
		promoted = append(promoted, mv.dst)
	}
	return func() {
		for _, bak := range parked {
			if err := fs.Remove(bak); err != nil && !os.IsNotExist(err) {
				logging.Warnf("poster promote finalize %s: %v", bak, err)
			}
		}
	}, nil
}

// cleanupStagedPosterPair removes leftover staged files after a failed
// promote/commit. Callers own the stage namespace (unique per request), so
// no lock is needed.
func cleanupStagedPosterPair(fs afero.Fs, tempDir, jobID, stageID string) {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	dir := filepath.Join(tempDir, "posters", jobID)
	for _, name := range []string{stageID + "-full.jpg", stageID + ".jpg"} {
		if err := fs.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			logging.Warnf("staged poster cleanup %s: %v", name, err)
		}
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
