package downloader

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

func (d *Downloader) downloadCover(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo, options ...any) (*DownloadResult, error) {
	overwriteExisting, dedup := resolveDownloadOptions(options)
	if !d.config.DownloadCover || movie.Poster.CoverURL == "" {
		return &DownloadResult{Type: MediaTypeCover, Downloaded: false}, nil
	}

	tmplCtx := d.buildTemplateContext(movie, multipart)
	destPath := d.pathResolver.ResolveFanartPath(movie, nil, true, tmplCtx, destDir)

	return d.download(ctx, movie.Poster.CoverURL, destPath, MediaTypeCover, overwriteExisting, dedup, resolveDownloadLedger(options))
}

func (d *Downloader) downloadPoster(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo, options ...any) (finalResult *DownloadResult, finalErr error) {
	startTime := time.Now()
	overwriteExisting, dedup := resolveDownloadOptions(options)
	owner := resolveDownloadOwnerOptions(options)
	ledger := resolveDownloadLedger(options)
	if !d.config.DownloadPoster {
		releaseDownloadOwnerClaim(dedup, owner.logicalKey, owner.ownerKey)
		return &DownloadResult{Type: MediaTypePoster, Downloaded: false}, nil
	}

	posterURL := movie.Poster.PosterURL
	if posterURL == "" {
		posterURL = movie.Poster.CoverURL
	}
	if posterURL == "" {
		releaseDownloadOwnerClaim(dedup, owner.logicalKey, owner.ownerKey)
		return &DownloadResult{Type: MediaTypePoster, Downloaded: false}, nil
	}

	tmplCtx := d.buildTemplateContext(movie, multipart)
	destPath := d.pathResolver.ResolvePosterPath(movie, nil, true, tmplCtx, destDir)
	if !overwriteExisting {
		releaseDownloadOwnerClaim(dedup, owner.logicalKey, owner.ownerKey)
	}

	bounds := movie.Poster.PosterCropBounds
	geometryUsable := bounds != nil && movie.Poster.PosterCropSourceFull && bounds.Valid()

	if !geometryUsable && !movie.Poster.ShouldCropPoster {
		if !overwriteExisting {
			releaseDownloadOwnerClaim(dedup, owner.logicalKey, owner.ownerKey)
		}
		return d.download(ctx, posterURL, destPath, MediaTypePoster, overwriteExisting, dedup, ledger, owner)
	}

	result := &DownloadResult{
		URL:  posterURL,
		Type: MediaTypePoster,
	}
	var reservation *downloadReservation
	if overwriteExisting {
		var skipped bool
		var reservationErr error
		reservation, skipped, reservationErr = acquireDownloadReservation(ctx, dedup, destPath, owner.logicalKey, owner.ownerKey)
		if reservationErr != nil {
			result.Error = reservationErr
			result.Duration = time.Since(startTime)
			return result, result.Error
		}
		if skipped {
			result.Skipped = true
			result.Duration = time.Since(startTime)
			return result, nil
		}
		defer func() {
			finishDownloadReservation(dedup, destPath, reservation, finalErr == nil)
		}()
	}

	existed := false
	if overwriteExisting {
		_, err := d.fs.Stat(destPath)
		switch {
		case err == nil:
			existed = true
		case os.IsNotExist(err):
		default:
			result.Error = fmt.Errorf("failed to stat destination: %w", err)
			result.Duration = time.Since(startTime)
			return result, result.Error
		}
	} else if info, err := d.fs.Stat(destPath); err == nil {
		// Existing artwork is never replaced outside overwrite mode, even with
		// pending manual crop geometry: downloaded paths feed the revert
		// delete-list, so replacing here would leave NO poster after a revert.
		result.LocalPath = destPath
		result.Size = info.Size()
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// One GET feeds every poster-producing path below — manual crop, promote,
	// and auto crop all reuse the already-downloaded bytes, so a single-use or
	// signed poster URL cannot be consumed twice.
	fullPath := uniqueTempPath(destPath, "full.tmp")
	// Wave-42 (codex P2, PR#215): when the cropped install below returns an
	// error carrying fsutil.ErrPublishCompleted, the staged (candidate) name
	// could not be re-proven and may now address a FOREIGN occupant fsutil
	// deliberately left byte-intact. The deferred scratch unlinks must then
	// skip the candidate name — stagedRetained gates BOTH scratch legs (the
	// candidate is fullPath when no crop applied, cropPath when it did);
	// every other scratch cleans exactly as before. Plain failures keep the
	// prior cleanup (stagedRetained stays "").
	// Wave-65 (codex P2, PR#215 finding F1): BOTH scratch defers are now
	// identity-bound exactly like the wave-62 failed-install cleanup — the
	// scratch FileInfo is captured at the write instant (fullPath right after
	// the download; cropPath at the crop in BOTH modes — wave-66 deepened the
	// at-crop capture into the crop producer's identity record, see the
	// provenance bind below) and before removal the defer SameFile-probes the
	// name still names that record; a foreign write rotated into the
	// crop→cleanup window is preserved byte-intact for manual cleanup instead
	// of being destroyed by a pathname Remove.
	// Wave-67 (codex P2, PR#215 — producer-returned records): those captures
	// no longer run as caller-side lookups of the mutable names — the full
	// download's record is the install's post-publish-VERIFIED identity filed
	// on fullResult, and the crop records are the crop producers' own
	// post-write FileInfo handed back with the result.
	// stagedRetained's carve-outs still cover substitution / completed-publish
	// detection (the candidate skip); the binding covers the non-retained
	// scratch and the candidate on its plain-success/failure legs.
	stagedRetained := ""
	var fullIdentity installedDestIdentity
	defer func() {
		if stagedRetained != fullPath {
			removeScratchIfStillOurs(d.fs, fullPath, fullIdentity, "downloaded")
		}
	}()

	fullResult, err := d.download(ctx, posterURL, fullPath, MediaTypePoster, overwriteExisting, nil, ledger)
	fullResult.LocalPath = ""
	if err != nil || !fullResult.Downloaded {
		fullResult.Downloaded = false
		fullResult.Replaced = false
		fullResult.Duration = time.Since(startTime)
		return fullResult, err
	}
	// Wave-67 (codex P2, PR#215): the full download's producer record rides ON
	// its result — the install's post-publish-verified destination identity,
	// captured before the producer returned. No caller-side re-lookup of the
	// mutable fullPath: a swap between producer-return and such a capture used
	// to have the probe authenticate the substitute. The completed-despite-
	// error (wave-41) leg files no record — an unknown identity keeps the
	// wave-53 fail-closed posture both here and at the bind below.
	fullIdentity = fullResult.producerIdentity

	cropPath := uniqueTempPath(destPath, "crop.tmp")
	var cropIdentity installedDestIdentity
	defer func() {
		if stagedRetained != cropPath {
			removeScratchIfStillOurs(d.fs, cropPath, cropIdentity, "cropped")
		}
	}()

	candidate := fullPath
	cropped := false
	if geometryUsable {
		var cropOK bool
		// Wave-67 (codex P2, PR#215): the crop producer hands its write leg's
		// OWN post-write identity record back with its result — the record the
		// install-time provenance bind authenticates against (and the
		// scratch-defer binding), taken inside the producer before ANY
		// install-time lookup of the name. A fallback (undecodable / aspect
		// drift / empty rect) files no record and the name's wave-65 unknown
		// posture (retain, never unlink on doubt) applies.
		cropOK, cropIdentity = d.cropDownloadedPoster(fullPath, cropPath, bounds)
		if cropOK {
			candidate = cropPath
			cropped = true
		}
	}
	if !cropped && movie.Poster.ShouldCropPoster {
		cropInfo, cropErr := imageutil.CropPosterFromCover(d.fs, fullPath, cropPath, d.config.MaxPosterHeight)
		if cropErr != nil {
			fullResult.Error = fmt.Errorf("failed to crop poster: %w", cropErr)
			fullResult.Downloaded = false
			fullResult.Replaced = false
			fullResult.LocalPath = ""
			fullResult.Duration = time.Since(startTime)
			return fullResult, fullResult.Error
		}
		candidate = cropPath
		// Wave-67: same producer-returned record for the auto-crop leg.
		cropIdentity = installedIdentityFromFileInfo(cropInfo)
	}

	// Wave-47 (codex P2, PR#215 finding F1-media) as deepened by wave-48
	// (codex P2, PR#215 finding 6): the candidate NAME is bound to the exact
	// object downloadPoster just produced end to end. The crop writers hand
	// back no handle, so the candidate is re-opened O_RDONLY no-follow
	// immediately after the crop/write completes and THAT fd rides as the
	// install provenance (bindCandidateProvenance — identity frozen from its
	// own fstat; a failed open/fstat degrades to the wave-47 post-write
	// no-follow capture, never a failure). installOverwriting owns the handle
	// through every publish (the wave-29/30 bound-publish family closes it at
	// publish adjacency and re-proves the landed destination), so a substitute
	// rotated onto the candidate name inside the crop/write→install window is
	// refused — before any bytes-at-dest mutation — instead of being published
	// and confirmed as ours. Wave-53 (codex P2, PR#215 finding 2): the
	// non-overwrite rename promote below now shares the SAME candidate-provenance
	// binding — the bound publish re-proves the candidate name at publish
	// adjacency, so a substitute rotated onto it inside the crop/write→promote
	// window is refused too (the pre-shape legacy leg published by name
	// unprovenanced). Wave-53 (codex P3, PR#215 finding 3): when both the
	// path identity capture and the no-follow re-open fail, bindCandidateProvenance
	// returns the typed errCandidateProvenanceUnprobeable refusal — fail CLOSED
	// on either leg (never publish unauthenticated; nothing recorded or touched,
	// candidate preserved for manual cleanup).
	// Wave-66 (codex P2, PR#215 — bind the candidate to the PRODUCER'S
	// identity): the candidate's producer record — wave-67's full-download
	// record filed on the install's result, or the crop producers' returned
	// post-write FileInfo — rides into BOTH the overwrite-mode bind below and
	// the non-overwrite promote's bind
	// (promotePosterCandidateNoReplace). bindCandidateProvenance compares its
	// install-time Lstat AND the re-opened fd's fstat against THAT record, so
	// a substitute rotated onto the candidate name between the producer write
	// and the bind no longer authenticates against itself: the bind refuses
	// typed (errStagedInputSubstituted), the substitute is preserved
	// byte-intact, and the install/refusal posture below is unchanged.
	producerIdentity := fullIdentity
	if candidate == cropPath {
		producerIdentity = cropIdentity
	}
	var provenance stagedInstallProvenance
	if overwriteExisting {
		var provErr error
		provenance, provErr = bindCandidateProvenanceFn(d.fs, candidate, producerIdentity)
		if provErr != nil {
			if errors.Is(provErr, errStagedInputSubstituted) {
				// Wave-66: the name provably stopped naming the producer-written
				// object between the crop/download and the bind — the same
				// retained-substitute posture as the install-time substitution
				// refusal below, reached before installOverwriting ever ran.
				logging.Warnf("downloadPoster: install of %s refused — candidate name %s no longer names the crop/write-produced object (foreign substitution between the crop/write and the install-time bind); substitute preserved, destination untouched, manual cleanup advised", destPath, candidate)
			} else {
				logging.Warnf("downloadPoster: install of %s refused — candidate %s could not be proven (path identity capture and no-follow re-open both failed); refusing to publish unauthenticated, destination untouched, candidate preserved for manual cleanup", destPath, candidate)
			}
			stagedRetained = candidate
			fullResult.Error = provErr
			fullResult.Downloaded = false
			fullResult.Replaced = false
			fullResult.LocalPath = ""
			fullResult.Duration = time.Since(startTime)
			return fullResult, fullResult.Error
		}
	}

	if overwriteExisting {
		skipped, replaced, instErr := d.installOverwriting(ctx, candidate, destPath, ledger, provenance)
		switch {
		case instErr != nil:
			// Wave-47 (codex P2, PR#215 finding F1-media): the candidate name
			// provably stopped naming the crop/write-produced object inside the
			// capture→install window — the staged occupant is now FOREIGN bytes.
			// The install published nothing on the create path (the destination
			// was never touched) and already ran the set-aside restore + journal
			// retraction on the replace path, so the only remaining duty is
			// retaining the candidate name: stagedRetained gates BOTH deferred
			// scratch unlinks off it (the wave-42 retained-candidate discipline —
			// the possibly-foreign substitute stays byte-intact for manual
			// inspection, warn-logged, matching download()'s wave-45 refusal
			// posture in http.go) while the non-candidate scratch still reaps.
			if errors.Is(instErr, errStagedInputSubstituted) {
				logging.Warnf("downloadPoster: install of %s refused — candidate name %s no longer names the crop/write-produced object (foreign substitution between crop and install); substitute preserved, destination untouched, manual cleanup advised", destPath, candidate)
				stagedRetained = candidate
				fullResult.Error = instErr
				fullResult.Downloaded = false
				fullResult.Replaced = false
				fullResult.LocalPath = ""
				fullResult.Duration = time.Since(startTime)
				return fullResult, fullResult.Error
			}
			// Wave-42 (codex P2, PR#215): an install error carrying
			// fsutil.ErrPublishCompleted proves the destination WAS published
			// with the candidate bytes — the POSIX hard-link fallback's staged
			// cleanup could not re-prove the candidate name
			// (fsutil.ErrPublishNoReplaceStagedUnverified: it may now address a
			// FOREIGN occupant fsutil deliberately left byte-intact) or its
			// unlink failed with the destination rollback failing too (wave-20).
			// This is a completed download, never a failure: record it exactly
			// like the success leg below (dest enters CreatedPaths through
			// Downloaded && !Replaced, so a later revert leaves the new media
			// behind) and NEVER remove the candidate name — unlinking there
			// could destroy foreign bytes. The retained staged name is
			// warn-logged for manual cleanup, matching download()'s wave-41
			// posture in http.go. Every other error keeps the prior failure leg
			// (both scratch names reaped by the deferred cleanups).
			if fsutil.PublishCompleted(instErr) {
				logging.Warnf("downloadPoster: install of %s completed despite the returned error (%v) — staged name %s could not be re-proven (possibly foreign) and is left in place; manual cleanup advised", destPath, instErr, candidate)
				stagedRetained = candidate
				fullResult.Downloaded = true
				fullResult.Replaced = replaced
				d.finalizePosterResult(fullResult, destPath)
				fullResult.Duration = time.Since(startTime)
				return fullResult, nil
			}
			fullResult.Error = instErr
			fullResult.Downloaded = false
			fullResult.Replaced = false
			fullResult.LocalPath = ""
			fullResult.Duration = time.Since(startTime)
			return fullResult, fullResult.Error
		case skipped:
			// Ledger-less destructive overwrite refused: keep existing artwork,
			// report skip (reuse the existing path for downstream state).
			fullResult.Skipped = true
			fullResult.Downloaded = false
			fullResult.LocalPath = destPath
			fullResult.Duration = time.Since(startTime)
			return fullResult, nil
		default:
			fullResult.Downloaded = true
			fullResult.Replaced = replaced
			d.finalizePosterResult(fullResult, destPath)
			fullResult.Duration = time.Since(startTime)
			return fullResult, nil
		}
	} else {
		// overwriteExisting=false — the overwrite leg above terminated already.
		// Wave-51 (codex P2 parity for the legacy promote): the non-overwrite
		// promote must NEVER replace an occupied destination — 'existing
		// artwork is never replaced outside overwrite mode' covers a racer that
		// claimed destPath inside the download→promote window too. The
		// pre-shape plain Rename CLOBBERED that racer on POSIX (replace
		// semantics: its bytes destroyed, no backup, no ledger entry) while
		// Windows's MoveFileW refused — the wave-15 classifier window on the
		// legacy leg, plus a POSIX/Windows parity break. The promote now rides
		// the shared no-replace primitive: a collision keeps the racer's bytes
		// byte-intact and lands exactly the pre-download existing-artwork
		// outcome, and the publish-completed class (the POSIX hard-link
		// fallback's staged-residue legs) is honored like the wave-42 install
		// path — the destination provably carries the candidate bytes and the
		// possibly-foreign candidate name is retained for manual cleanup.
		// Wave-53 (codex P2, PR#215 finding 2): the candidate is now bound to its
		// validated-handle provenance BEFORE the promote publish through
		// promotePosterCandidateNoReplace (the same bindCandidateProvenance +
		// bound-publish discipline the overwrite install uses). A substitute
		// rotated onto the candidate name inside the crop/write→promote window
		// is refused (errStagedInputSubstituted) instead of being published
		// unprovenanced — the legacy leg's last unprovenanced publish surface is
		// closed. The both-fail refusal (finding 3) fails closed there too.
		outcome, promoteErr := promotePosterCandidateNoReplace(d.fs, candidate, destPath, producerIdentity)
		switch outcome {
		case promotePosterCandidateCollision:
			// fullResult carries the FULL download's bookkeeping: reset it to
			// the pre-download existing-artwork outcome so the racer's
			// destination NEVER enters CreatedPaths (a later revert would
			// delete those foreign bytes) — exactly the early-classification
			// leg's shape above.
			fullResult.Downloaded = false
			fullResult.Replaced = false
			if info, serr := d.fs.Stat(destPath); serr == nil {
				fullResult.LocalPath = destPath
				fullResult.Size = info.Size()
			}
			fullResult.Duration = time.Since(startTime)
			return fullResult, nil
		case promotePosterCandidateCompleted:
			stagedRetained = candidate
			fullResult.Downloaded = true
			fullResult.Replaced = false
			d.finalizePosterResult(fullResult, destPath)
			fullResult.Duration = time.Since(startTime)
			return fullResult, nil
		case promotePosterCandidateRetained:
			// Substitution (errStagedInputSubstituted) or both-fail refusal
			// (errCandidateProvenanceUnprobeable): the candidate name is
			// possibly foreign — preserve it byte-intact for manual cleanup.
			stagedRetained = candidate
			fullResult.Error = promoteErr
			fullResult.Downloaded = false
			fullResult.Replaced = false
			fullResult.LocalPath = ""
			fullResult.Duration = time.Since(startTime)
			return fullResult, fullResult.Error
		case promotePosterCandidateFailed:
			// A plain publish failure — the candidate is provably ours, so the
			// deferred cleanup reaps both scratch names; surface the error.
			fullResult.Downloaded = false
			fullResult.Replaced = false
			fullResult.LocalPath = ""
			fullResult.Size = 0
			fullResult.Error = promoteErr
			fullResult.Duration = time.Since(startTime)
			return fullResult, fullResult.Error
		default: // promotePosterCandidateSucceeded
			fullResult.Downloaded = true
			fullResult.Replaced = existed
			d.finalizePosterResult(fullResult, destPath)
			fullResult.Duration = time.Since(startTime)
			return fullResult, nil
		}
	}
	// Both mode legs above return from every switch arm (each switch has a
	// default), so this if/else is a terminating statement: no fall-through
	// tail follows. The pre-reshape tail duplicated the promote's succeeded
	// arm (Downloaded / Replaced=existed / finalizePosterResult / return
	// nil) but was unreachable — the switch returns on all five outcomes —
	// and its five dead statements failed the 100% patch-coverage gate.
}

// removeScratchIfStillOurs is downloadPoster's wave-65 identity-bound scratch
// cleanup (codex P2, PR#215 finding F1), mirroring http.go's wave-62
// failed-install cleanup: remove the scratch name ONLY when it still provably
// names the object the caller captured at the write instant (dev/inode when
// exposed, then size + mtime). A foreign write rotated onto the name inside
// the crop→cleanup window is preserved byte-intact for manual cleanup
// instead of being destroyed by a pathname Remove. An unproven/unknown
// identity (the write never completed, or the capture failed) retains too —
// never unlink on doubt. A name that already vanished (ENOENT) is a silent
// no-op.
func removeScratchIfStillOurs(fs afero.Fs, path string, id installedDestIdentity, what string) {
	if destStillHoldsInstalledObject(fs, path, id) {
		_ = fs.Remove(path)
		return
	}
	if _, lerr := lstatBackupCandidate(fs, path); !os.IsNotExist(lerr) {
		logging.Warnf("downloadPoster: scratch name %s left in place — it no longer provably names the %s object (foreign substitution or indeterminate); preserved byte-intact for manual cleanup", path, what)
	}
}

// finalizePosterResult points result at the promoted poster, or clears the
// location fields so a caller can never see a dangling (removed) temp path.
func (d *Downloader) finalizePosterResult(result *DownloadResult, destPath string) {
	result.LocalPath = ""
	result.Size = 0
	if info, err := d.fs.Stat(destPath); err == nil {
		result.LocalPath = destPath
		result.Size = info.Size()
	}
}

// cropDownloadedPoster applies the normalized review-page geometry to an
// already-downloaded full source image and writes the cropped poster to dst.
// Returns false when the geometry does not apply to this image (undecodable,
// aspect drift, empty rect); the caller then falls back to the pre-geometry
// behavior with the temp file still in place.
// Wave-67 (codex P2, PR#215): on success the producer's own post-write
// identity record rides back with the bool — CropPosterWithBounds'
// producer-side capture, never a caller-side re-lookup.
func (d *Downloader) cropDownloadedPoster(tempPath, dst string, bounds *models.CropBounds) (bool, installedDestIdentity) {
	w, h, derr := imageutil.ImageDimensions(d.fs, tempPath)
	if derr != nil || w <= 0 || h <= 0 {
		logging.Warnf("downloadPoster: cannot decode downloaded source for manual crop: %v", derr)
		return false, installedDestIdentity{}
	}
	// P4 source identity guard: aspect alone cannot distinguish a same-sized
	// image whose pixels were replaced at the same URL. Legacy envelopes have
	// no fingerprint and retain the pre-P4 aspect-only floor.
	if bounds.SourceFingerprint != "" {
		identity, ierr := assetidentity.Measure(d.fs, tempPath)
		if ierr != nil {
			logging.Warnf("downloadPoster: cannot fingerprint downloaded source for manual crop: %v", ierr)
			return false, installedDestIdentity{}
		}
		if !strings.EqualFold(identity.Fingerprint, bounds.SourceFingerprint) {
			logging.Warnf("downloadPoster: manual crop fingerprint mismatch (crop %s, downloaded %s); falling back", bounds.SourceFingerprint, identity.Fingerprint)
			return false, installedDestIdentity{}
		}
	}

	// Aspect guard: the geometry was normalized against the review-time
	// source; if the downloaded image no longer matches that aspect, the
	// geometry targets a different image — refuse and fall back.
	if bounds.SourceAspect > 0 {
		got := float64(w) / float64(h)
		diff := math.Abs(got - bounds.SourceAspect)
		if diff > 0.01*bounds.SourceAspect {
			logging.Warnf("downloadPoster: manual crop aspect mismatch (crop %.4f, downloaded %.4f); falling back", bounds.SourceAspect, got)
			return false, installedDestIdentity{}
		}
	}

	// Valid() guarantees unit-square containment, so no clamping is needed:
	// rounding stays within [0,w]×[0,h] (the 1e-9 tolerance cannot edge over a
	// pixel boundary) — only degenerate rounding needs a guard.
	fw, fh := float64(w), float64(h)
	left := int(math.Round(bounds.X * fw))
	top := int(math.Round(bounds.Y * fh))
	right := int(math.Round((bounds.X + bounds.Width) * fw))
	bottom := int(math.Round((bounds.Y + bounds.Height) * fh))
	if right <= left || bottom <= top {
		logging.Warnf("downloadPoster: manual crop geometry collapses to empty rect; falling back")
		return false, installedDestIdentity{}
	}

	cropInfo, cropErr := imageutil.CropPosterWithBounds(d.fs, tempPath, dst, left, top, right, bottom, d.config.MaxPosterHeight)
	if cropErr != nil {
		logging.Warnf("downloadPoster: manual crop failed: %v", cropErr)
		return false, installedDestIdentity{}
	}
	return true, installedIdentityFromFileInfo(cropInfo)
}

// downloadExtrafanart downloads screenshots to the extrafanart subdirectory.
// Extrafanart is used by media centers like Kodi/Plex for background images.
// Note: In the original Javinizer, screenshots and extrafanart are the same thing.
func (d *Downloader) downloadExtrafanart(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo, enabled bool, options ...any) ([]DownloadResult, error) {
	overwriteExisting, dedup := resolveDownloadOptions(options)
	if !enabled || len(movie.Screenshots) == 0 {
		return []DownloadResult{}, nil
	}

	extrafanartDir := filepath.Join(destDir, d.config.ScreenshotFolder)
	tmplCtx := d.buildTemplateContext(movie, multipart)
	screenshotNames := d.pathResolver.ResolveScreenshotNames(movie, true, tmplCtx)
	results := make([]DownloadResult, 0, len(movie.Screenshots))

	for i, url := range movie.Screenshots {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		if i >= len(screenshotNames) {
			break
		}
		destPath := filepath.Join(extrafanartDir, screenshotNames[i])
		result, err := d.download(ctx, url, destPath, MediaTypeExtrafanart, overwriteExisting, dedup, resolveDownloadLedger(options))
		if err != nil {
			result.Error = err
		}
		results = append(results, *result)
	}

	return results, nil
}

func (d *Downloader) downloadTrailer(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo, options ...any) (*DownloadResult, error) {
	overwriteExisting, dedup := resolveDownloadOptions(options)
	if !d.config.DownloadTrailer || movie.TrailerURL == "" {
		return &DownloadResult{Type: MediaTypeTrailer, Downloaded: false}, nil
	}

	tmplCtx := d.buildTemplateContext(movie, multipart)
	destPath := d.pathResolver.ResolveTrailerPath(movie, true, tmplCtx, destDir)

	op := &retryableOperation{
		initialDelay: 100 * time.Millisecond,
		maxDelay:     10 * time.Second,
	}

	var lastResult *DownloadResult
	retryErr := op.ExecuteWithRetry(ctx, func() error {
		res, err := d.download(ctx, movie.TrailerURL, destPath, MediaTypeTrailer, overwriteExisting, dedup, resolveDownloadLedger(options))
		if err == nil {
			lastResult = res
		}
		return err
	}, 2, redactURL(movie.TrailerURL))

	if retryErr != nil {
		return &DownloadResult{
			URL:       movie.TrailerURL,
			LocalPath: destPath,
			Type:      MediaTypeTrailer,
			Error:     retryErr,
		}, retryErr
	}

	return lastResult, nil
}

func (d *Downloader) downloadActressImages(ctx context.Context, movie *models.Movie, destDir string, options ...any) ([]DownloadResult, error) {
	overwriteExisting, dedup := resolveDownloadOptions(options)
	if !d.config.DownloadActress || len(movie.Actresses) == 0 {
		return []DownloadResult{}, nil
	}

	actressDir := filepath.Join(destDir, d.config.ActressFolder)
	results := make([]DownloadResult, 0)

	for _, actress := range movie.Actresses {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		if actress.ThumbURL == "" {
			continue
		}

		formattedName := models.FormatActressName(actress, models.FormatActressNameOptions{
			JapaneseNames:      d.config.ActorJapaneseNames,
			FirstNameOrder:     d.config.ActorFirstNameOrder,
			UnknownActress:     d.config.UnknownActressText,
			UnknownActressMode: d.config.UnknownActressMode,
		})
		if formattedName == "" {
			continue
		}

		actressMovie := &models.Movie{ID: movie.ID}
		filename := d.generateActressFilename(actressMovie, formattedName, d.config.ActressFormat)
		if filename == "" {
			name := template.SanitizeFilename(formattedName)
			filename = fmt.Sprintf("%s.jpg", name)
		}
		destPath := filepath.Join(actressDir, filename)

		result, err := d.download(ctx, actress.ThumbURL, destPath, MediaTypeActress, overwriteExisting, dedup, resolveDownloadLedger(options))
		if err != nil {
			result.Error = err
		}
		results = append(results, *result)
	}

	return results, nil
}

func (d *Downloader) downloadAllWithExtrafanart(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo, extrafanartEnabled bool, options ...any) ([]DownloadResult, error) {
	overwriteExisting, dedup := resolveDownloadOptions(options)
	results := make([]DownloadResult, 0)
	criticalAttempted := 0
	criticalSucceeded := 0

	coverResult, _ := d.downloadCover(ctx, movie, destDir, multipart, overwriteExisting, dedup, resolveDownloadLedger(options))

	if coverResult != nil {
		if coverResult.Error != nil {
			logging.Warnf("downloadAll: cover download failed for %s: %v", movie.ID, coverResult.Error)
		}
		if coverResult.Type == MediaTypeCover && d.config.DownloadCover && movie.Poster.CoverURL != "" && !coverResult.Skipped {
			criticalAttempted++
			if coverResult.Error == nil && coverResult.LocalPath != "" {
				criticalSucceeded++
			}
		}
		results = append(results, *coverResult)
	}

	owner := resolveDownloadOwnerOptions(options)
	posterResult, _ := d.downloadPoster(ctx, movie, destDir, multipart, overwriteExisting, dedup, resolveDownloadLedger(options), owner)
	if posterResult != nil {
		if posterResult.Error != nil {
			logging.Warnf("downloadAll: poster download failed for %s: %v", movie.ID, posterResult.Error)
		}
		if posterResult.Type == MediaTypePoster && d.config.DownloadPoster && !posterResult.Skipped {
			posterURL := movie.Poster.PosterURL
			if posterURL == "" {
				posterURL = movie.Poster.CoverURL
			}
			if posterURL != "" {
				criticalAttempted++
				if posterResult.Error == nil && posterResult.LocalPath != "" {
					criticalSucceeded++
				}
			}
		}
		results = append(results, *posterResult)
	}

	extrafanart, _ := d.downloadExtrafanart(ctx, movie, destDir, multipart, extrafanartEnabled, overwriteExisting, dedup, resolveDownloadLedger(options))
	for i := range extrafanart {
		if extrafanart[i].Error != nil {
			logging.Warnf("downloadAll: extrafanart[%d] download failed for %s: %v", i, movie.ID, extrafanart[i].Error)
		}
	}
	results = append(results, extrafanart...)

	if trailerResult, _ := d.downloadTrailer(ctx, movie, destDir, multipart, overwriteExisting, dedup, resolveDownloadLedger(options)); trailerResult != nil {
		if trailerResult.Error != nil {
			logging.Warnf("downloadAll: trailer download failed for %s: %v", movie.ID, trailerResult.Error)
		}
		results = append(results, *trailerResult)
	}

	partNumber := 0
	if multipart != nil {
		partNumber = multipart.PartNumber
	}
	if partNumber == 0 || partNumber == 1 || overwriteExisting {
		actresses, err := d.downloadActressImages(ctx, movie, destDir, overwriteExisting, dedup, resolveDownloadLedger(options))
		if err != nil {
			logging.Warnf("downloadAll: actress image download aborted for %s: %v", movie.ID, err)
		}
		for i := range actresses {
			if actresses[i].Error != nil {
				logging.Warnf("downloadAll: actress image download failed for %s: %v", movie.ID, actresses[i].Error)
			}
		}
		results = append(results, actresses...)
	}

	if criticalAttempted > 0 && criticalSucceeded == 0 {
		return results, &DownloadPartialError{Attempted: criticalAttempted, Succeeded: criticalSucceeded}
	}

	return results, nil
}
