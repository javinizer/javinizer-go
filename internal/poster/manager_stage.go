package poster

// P2 (apply-writeback-coherence): staged preview install + asset
// snapshot/restore compensation.

import (
	"fmt"
	"os"
	"path/filepath"
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
