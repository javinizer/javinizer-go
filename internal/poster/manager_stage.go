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

// installStagedPreview moves the current preview aside into a per-operation
// backup name and renames the staged bytes into place — readers always see
// either the old or the new bytes, never a partial write. Any failure
// restores the previous preview best-effort and reports the error; no staged
// or backup residue survives, success or failure.
func (pm *PosterManager) installStagedPreview(finalPath, stagedPath string) error {
	backupPath := ""
	if _, err := pm.fs.Stat(finalPath); err == nil {
		backupPath = uniqueStagedSibling(finalPath, "bak")
		if err := pm.fs.Rename(finalPath, backupPath); err != nil {
			return fmt.Errorf("failed to set aside previous preview: %w", err)
		}
	}
	if err := pm.fs.Rename(stagedPath, finalPath); err != nil {
		if backupPath != "" {
			// Losing the just-staged bytes is preferable to leaving the
			// destination empty — old content beats none.
			if rErr := pm.fs.Rename(backupPath, finalPath); rErr != nil {
				return fmt.Errorf("failed to install staged preview: %w (restore of previous preview also failed: %v)", err, rErr)
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
	jobID            string
	stagedID         string
	targetID         string
	croppedURLStaged string
	sourceWidth      int
	sourceHeight     int
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
		jobID:            req.JobID,
		stagedID:         stagedID,
		targetID:         req.PosterID,
		croppedURLStaged: res.CroppedURL,
		sourceWidth:      res.SourceWidth,
		sourceHeight:     res.SourceHeight,
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
				continue // leg never staged (e.g. crop failed with no fallback)
			}
			return nil, fmt.Errorf("promote staging stat %s: %w", l.stagedPath, err)
		}
		legs = append(legs, l)
	}

	// Phase 1: aside the previous canonical legs (staged bytes stay put).
	for i := range legs {
		if _, err := pm.fs.Stat(legs[i].finalPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("promote canonical stat %s: %w", legs[i].finalPath, err)
		}
		legs[i].backupPath = uniqueStagedSibling(legs[i].finalPath, "bak")
		if err := pm.fs.Rename(legs[i].finalPath, legs[i].backupPath); err != nil {
			pm.restorePromotedBackups(legs[:i])
			return nil, fmt.Errorf("promote staged poster: set aside %s: %w", legs[i].finalPath, err)
		}
	}
	// Phase 2: move staged into canonical (rename-only).
	for i := range legs {
		if err := pm.fs.Rename(legs[i].stagedPath, legs[i].finalPath); err != nil {
			pm.restorePromotedBackups(legs)
			return nil, fmt.Errorf("promote staged poster: %w", err)
		}
	}
	// Success: drop the per-op backups best-effort.
	for _, l := range legs {
		if l.backupPath != "" {
			if err := pm.fs.Remove(l.backupPath); err != nil {
				logging.Warnf("poster promote backup sweep %s: %v", l.backupPath, err)
			}
		}
	}

	res := &cropResult{SourceFull: true}
	for _, l := range legs {
		if strings.HasSuffix(l.finalPath, "-full.jpg") {
			res.FullPath = l.finalPath
			res.SourceWidth = staged.sourceWidth
			res.SourceHeight = staged.sourceHeight
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
			// Nothing was aside for this leg — remove a possibly installed new final.
			_ = pm.fs.Remove(l.finalPath)
			continue
		}
		// Prefer the bytes' identity over the address: delete a partial new
		// final first so the restore rename lands even on strict filesystems.
		_ = pm.fs.Remove(l.finalPath)
		if err := pm.fs.Rename(l.backupPath, l.finalPath); err != nil {
			logging.Warnf("poster promote restore %s failed (%v) — bytes survive at %s", l.finalPath, err, l.backupPath)
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
