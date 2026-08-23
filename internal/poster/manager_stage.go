package poster

// P2 (apply-writeback-coherence): staged preview install + asset
// snapshot/restore compensation.

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
)

// stagedNameCounter disambiguates staged names allocated within the same
// clock tick (single-process deployment per design D15).
var stagedNameCounter atomic.Uint64

// uniqueStagedSibling returns a collision-free sibling path for staged
// bytes/backups: <dest>.<unixnano>-<counter>.<suffix> — mirroring the
// downloaders' uniqueTempPath convention, re-landed fresh per the quarry
// discipline.
func uniqueStagedSibling(destPath, suffix string) string {
	return fmt.Sprintf("%s.%d-%d.%s", destPath, time.Now().UnixNano(), stagedNameCounter.Add(1), suffix)
}

// installStagedPreview installs staged bytes onto finalPath so the canonical
// path is NEVER absent mid-replace (codex P2): the previous preview is COPIED
// aside (not moved), and the staged bytes land via fsutil.ReplaceFile, which
// is rename-based on POSIX and MoveFileEx-based on Windows — both atomic
// replaces. A lock-free reader (serveTempPoster) sees old bytes until the
// swap, then new bytes; failure to replace leaves old bytes untouched.
func (pm *PosterManager) installStagedPreview(finalPath, stagedPath string) error {
	backupPath := ""
	if _, err := pm.fs.Stat(finalPath); err == nil {
		backupPath = uniqueStagedSibling(finalPath, "bak")
		if err := fsutil.CopyFileFs(pm.fs, finalPath, backupPath); err != nil {
			return fmt.Errorf("failed to back up previous preview: %w", err)
		}
	} else if !os.IsNotExist(err) {
		// codex P2: fail closed on an undecidable stat — never replace blind.
		return fmt.Errorf("failed to probe preview %s before install: %w", finalPath, err)
	}
	if err := fsutil.ReplaceFile(pm.fs, stagedPath, finalPath); err != nil {
		// Nothing moved over finalPath (atomic replace either lands or doesn't
		// — the prior bytes are still there); the backup copy is pure litter.
		if backupPath != "" {
			if rmErr := pm.fs.Remove(backupPath); rmErr != nil {
				logging.Warnf("failed install backup sweep %s: %v", backupPath, rmErr)
			}
		}
		return fmt.Errorf("failed to install staged preview: %w", err)
	}
	if backupPath != "" {
		if err := pm.fs.Remove(backupPath); err != nil {
			logging.Warnf("poster preview backup sweep failed for %s: %v", backupPath, err)
		}
	}
	return nil
}

// PosterAssetSnapshot captures the byte state of a (jobID, posterID) asset
// pair as of SnapshotAssets, for compensation restore on commit failure.
type PosterAssetSnapshot struct {
	jobID     string
	posterID  string
	fullBytes []byte // nil ⇒ full leg absent at snapshot time
	cropBytes []byte // nil ⇒ cropped leg absent at snapshot time
}

// SnapshotAssets captures the current <posterID>-full.jpg and <posterID>.jpg
// bytes (or their absence) for later RestoreAssets compensation.
func (pm *PosterManager) SnapshotAssets(jobID, posterID string) (*PosterAssetSnapshot, error) {
	if err := ValidateJobID(jobID); err != nil {
		return nil, err
	}
	if err := validatePosterID(posterID); err != nil {
		return nil, err
	}
	dir := filepath.Join(pm.tempDir, "posters", jobID)
	snap := &PosterAssetSnapshot{jobID: jobID, posterID: posterID}
	if b, err := afero.ReadFile(pm.fs, filepath.Join(dir, posterID+"-full.jpg")); err == nil {
		snap.fullBytes = b
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("snapshot assets: %w", err)
	}
	if b, err := afero.ReadFile(pm.fs, filepath.Join(dir, posterID+".jpg")); err == nil {
		snap.cropBytes = b
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("snapshot assets: %w", err)
	}
	return snap, nil
}

// RestoreAssets returns the (jobID, posterID) pair to its snapshot state:
// captured legs are restored byte-for-byte; legs CREATED after the snapshot
// are removed. Restores install via the same staged machinery so a
// mid-restore reader never sees partial bytes.
func (pm *PosterManager) RestoreAssets(snap *PosterAssetSnapshot) error {
	if snap == nil {
		return nil
	}
	dir := filepath.Join(pm.tempDir, "posters", snap.jobID)
	if err := pm.restoreLeg(dir, snap.posterID+"-full.jpg", snap.fullBytes); err != nil {
		return err
	}
	return pm.restoreLeg(dir, snap.posterID+".jpg", snap.cropBytes)
}

// restoreLeg restores one asset to content, or removes it when content is
// nil (the leg did not exist at snapshot time).
func (pm *PosterManager) restoreLeg(dir, name string, content []byte) error {
	target := filepath.Join(dir, name)
	if content == nil {
		if err := pm.fs.Remove(target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("restore assets: remove created leg %s: %w", name, err)
		}
		return nil
	}
	staged := uniqueStagedSibling(target, "tmp")
	if err := afero.WriteFile(pm.fs, staged, content, 0o644); err != nil {
		return fmt.Errorf("restore assets: stage leg %s: %w", name, err)
	}
	if err := pm.installStagedPreview(target, staged); err != nil {
		_ = pm.fs.Remove(staged)
		return fmt.Errorf("restore assets: install leg %s: %w", name, err)
	}
	return nil
}

// --- P2 staged download/promote seam ---

// StagePosterRequest carries one poster download to be staged OUTSIDE the
// caller's lock window (the network leg must never hold an edit key).
type StagePosterRequest struct {
	JobID     string
	PosterID  string // canonical target identity
	URL       string
	UserAgent string
	Referer   string
}

// StagedPoster is the opaque handle StagePosterDownload returns: bytes live
// under a unique staged identity and only PromoteStagedPoster moves them to
// the canonical names. All identity is internal — a caller can never reach
// the filesystem except through PromoteStagedPoster (fs-only).
type StagedPoster struct {
	jobID             string
	stagedID          string
	targetID          string
	croppedURLStaged  string
	sourceWidth       int
	sourceHeight      int
	sourceRevision    uint64
	sourceFingerprint string
}

// NewStagedPosterHandleForTest builds a handle directly — usable only from
// *_test.go callers (other packages, e.g. worker fakes, cannot construct the
// opaque type otherwise).
func NewStagedPosterHandleForTest(jobID, stagedID, targetID, stagedCropURL string) *StagedPoster {
	return &StagedPoster{jobID: jobID, stagedID: stagedID, targetID: targetID, croppedURLStaged: stagedCropURL}
}

// SourceDimensions returns the measured size of the staged source image.
func (s *StagedPoster) SourceDimensions() (int, int) {
	if s == nil {
		return 0, 0
	}
	return s.sourceWidth, s.sourceHeight
}

// StagePosterDownload downloads the poster to a UNIQUE staged identity
// (network, unlocked); nothing touches the canonical pair. The stage
// inherits DownloadFromURL's content validation and preview cropping;
// promote swaps the staged names for the canonical ones atomically.
func (pm *PosterManager) StagePosterDownload(ctx context.Context, req StagePosterRequest) (*StagedPoster, error) {
	if err := ValidateJobID(req.JobID); err != nil {
		return nil, err
	}
	if err := validatePosterID(req.PosterID); err != nil {
		return nil, err
	}
	stagedID := fmt.Sprintf("%s.stage-%d-%d", req.PosterID, time.Now().UnixNano(), stagedNameCounter.Add(1))
	res, err := pm.DownloadFromURL(ctx, req.JobID, stagedID, req.URL, req.UserAgent, req.Referer)
	if err != nil {
		return nil, err
	}
	return &StagedPoster{
		jobID:             req.JobID,
		stagedID:          stagedID,
		targetID:          req.PosterID,
		croppedURLStaged:  res.CroppedURL,
		sourceWidth:       res.SourceWidth,
		sourceHeight:      res.SourceHeight,
		sourceRevision:    res.SourceRevision,
		sourceFingerprint: res.SourceFingerprint,
	}, nil
}

// promoteLegMove tracks one staged→canonical leg through the promote dance.
type promoteLegMove struct{ stagedPath, finalPath, backupPath string }

// PromoteStagedPoster installs the staged pair under the canonical names,
// PAIR-ATOMIC: the previous canonical legs are moved aside FIRST, staged
// renames follow, and ANY failure restores the whole previous pair so the
// two legs never split across generations. FS-only — call it INSIDE the
// caller's lock window immediately before the state commit.
func (pm *PosterManager) PromoteStagedPoster(staged *StagedPoster) (*cropResult, error) {
	if staged == nil {
		return nil, fmt.Errorf("promote staged poster: nil stage")
	}
	tempPosterDir := filepath.Join(pm.tempDir, "posters", staged.jobID)
	var legs []promoteLegMove
	for _, suffix := range []string{"-full.jpg", ".jpg"} {
		l := promoteLegMove{
			stagedPath: filepath.Join(tempPosterDir, staged.stagedID+suffix),
			finalPath:  filepath.Join(tempPosterDir, staged.targetID+suffix),
		}
		if _, err := pm.fs.Stat(l.stagedPath); err != nil {
			if os.IsNotExist(err) {
				// codex P2 round 7: a successful stage ALWAYS produced both legs
				// (crop failures fall back to a copy). A missing leg therefore
				// means the stage was disturbed (temp sweep mid-op); promoting a
				// remainder would commit a dangling crop URL or mix generations.
				return nil, fmt.Errorf("promote staged poster: incomplete stage, staged leg absent: %s", l.stagedPath)
			}
			return nil, fmt.Errorf("promote staging stat %s: %w", l.stagedPath, err)
		}
		legs = append(legs, l)
	}
	// Phase 1: COPY the previous canonical bytes aside (never move). The
	// canonical leg stays present throughout the promote — a lock-free reader
	// on /temp/posters can never observe a 404 window (codex P2 round 4).
	for i := range legs {
		if _, err := pm.fs.Stat(legs[i].finalPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			pm.sweepPromotedBackups(legs[:i]) // earlier copies are pure litter
			return nil, fmt.Errorf("promote canonical stat %s: %w", legs[i].finalPath, err)
		}
		legs[i].backupPath = uniqueStagedSibling(legs[i].finalPath, "bak")
		if err := fsutil.CopyFileFs(pm.fs, legs[i].finalPath, legs[i].backupPath); err != nil {
			pm.sweepPromotedBackups(legs[:i])
			return nil, fmt.Errorf("promote staged poster: back up %s: %w", legs[i].finalPath, err)
		}
	}
	// Phase 2: staged bytes land via atomic REPLACE (the canonical entry
	// never disappears) — POSIX rename replaces; MoveFileEx on Windows.
	for i := range legs {
		if err := fsutil.ReplaceFile(pm.fs, legs[i].stagedPath, legs[i].finalPath); err != nil {
			// Restored legs return to their pre-promotion bytes; legs never
			// displaced just shed the backup copies.
			pm.restorePromotedBackups(legs[:i])
			pm.sweepPromotedBackups(legs[i:])
			return nil, fmt.Errorf("promote staged poster: %w", err)
		}
	}
	// Success: drop the per-op backups best-effort.
	pm.sweepPromotedBackups(legs)

	res := &cropResult{SourceFull: true}
	for _, l := range legs {
		if strings.HasSuffix(l.finalPath, "-full.jpg") {
			res.FullPath = l.finalPath
			res.SourceWidth = staged.sourceWidth
			res.SourceHeight = staged.sourceHeight
			res.SourceRevision = staged.sourceRevision
			res.SourceFingerprint = staged.sourceFingerprint
		} else {
			res.CroppedPath = l.finalPath
		}
	}
	// The staged crop URL carries the staged identity — swap it for the
	// canonical one the committed row must reference (never the staged name).
	res.CroppedURL = strings.Replace(staged.croppedURLStaged, url.PathEscape(staged.stagedID)+".jpg", url.PathEscape(staged.targetID)+".jpg", 1)
	return res, nil
}

// restorePromotedBackups best-effort returns every leg's previous bytes from
// its backup after a failed promote (clears any partial new installs first).
func (pm *PosterManager) restorePromotedBackups(legs []promoteLegMove) {
	for _, l := range legs {
		if l.backupPath == "" {
			// Nothing was backed up for this leg — remove a possibly installed new final.
			_ = pm.fs.Remove(l.finalPath)
			continue
		}
		// Remove the partial new install first so the restore rename lands even
		// on strict filesystems, then restore the backup in its place.
		_ = pm.fs.Remove(l.finalPath)
		if err := pm.fs.Rename(l.backupPath, l.finalPath); err != nil {
			logging.Warnf("poster promote restore %s failed (%v) — bytes survive at %s", l.finalPath, err, l.backupPath)
		}
	}
}

// sweepPromotedBackups deletes the per-op backup COPIES of legs whose
// canonical bytes were never displaced (phase-1 copies) or that already
// landed successfully. Warn-only on wedge (inspect-friendly).
func (pm *PosterManager) sweepPromotedBackups(legs []promoteLegMove) {
	for _, l := range legs {
		if l.backupPath != "" {
			if err := pm.fs.Remove(l.backupPath); err != nil {
				logging.Warnf("poster promote backup sweep %s: %v", l.backupPath, err)
			}
		}
	}
}

// DiscardStagedPoster removes any staged residue best-effort (e.g. when the
// fenced commit declines after staging succeeded).
func (pm *PosterManager) DiscardStagedPoster(staged *StagedPoster) {
	if staged == nil {
		return
	}
	tempPosterDir := filepath.Join(pm.tempDir, "posters", staged.jobID)
	for _, suffix := range []string{"-full.jpg", ".jpg"} {
		if err := pm.fs.Remove(filepath.Join(tempPosterDir, staged.stagedID+suffix)); err != nil && !os.IsNotExist(err) {
			logging.Warnf("staged poster discard %s-%s: %v", staged.stagedID, suffix, err)
		}
	}
}
