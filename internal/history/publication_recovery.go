package history

import (
	"fmt"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"os"
)

// Failed-publication move inverses carry the original install's eager identity.
// Re-prove that object inside the move, never relocate a later name occupant.
func recoverPublicationMove(fs afero.Fs, move models.FileMove) error {
	if move.Proof == nil {
		return fsutil.MoveFileNoReplace(fs, move.NewPath, move.OriginalPath)
	}
	entry := move.Proof
	if !entry.IdentityStrong {
		return fmt.Errorf("publication move has no strong install identity: %s", move.NewPath)
	}
	if entry.IdentityMode == nil || *entry.IdentityMode > 0777 {
		return fmt.Errorf("publication move has no valid installed permission mode: %s", move.NewPath)
	}
	mode := os.FileMode(*entry.IdentityMode)
	proof := func(path string, info os.FileInfo) error {
		dev, ino, ok := fsutil.BoundObjectIdentity(fs, path, info)
		if !ok || !info.Mode().IsRegular() || dev != entry.IdentityDev || ino != entry.IdentityIno || info.Size() != entry.IdentitySize || info.ModTime().Unix() != entry.IdentityModUnix || info.Mode().Perm() != mode {
			return fmt.Errorf("publication move identity changed: %s", path)
		}
		return nil
	}
	_, err := fsutil.MoveFileNoReplaceVerifiedMode(fs, move.NewPath, move.OriginalPath, proof, mode)
	return err
}

// Check only new failed-publication proofs. Unobservable or still-owned output
// cannot be reported as recovered; a positively different occupant is excluded.
func checkPublicationRecovery(fs afero.Fs, ledger models.GeneratedFilesJSON, afterCleanup bool) error {
	entries := append([]models.DeleteEntry(nil), ledger.PlannedDeletes...)
	for _, move := range ledger.MoveBack {
		if move.Proof != nil {
			entries = append(entries, *move.Proof)
		}
	}
	for _, entry := range entries {
		if !entry.RecoveryPublication && !entry.RecoveryUnproven {
			continue
		}
		info, err := lstatRestoreSource(fs, entry.Path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || entry.RecoveryUnproven {
			return fmt.Errorf("publication requires manual recovery without observable install identity: %s", entry.Path)
		}
		same := false
		if entry.LinkTarget != "" {
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			reader, ok := fs.(afero.LinkReader)
			if !ok {
				return fmt.Errorf("publication link identity unavailable: %s", entry.Path)
			}
			target, err := reader.ReadlinkIfPossible(entry.Path)
			if err != nil {
				return fmt.Errorf("publication link identity unavailable: %s: %w", entry.Path, err)
			}
			same = target == entry.LinkTarget
		} else {
			if !info.Mode().IsRegular() {
				continue
			}
			dev, ino, ok := fsutil.BoundObjectIdentity(fs, entry.Path, info)
			if !ok || !entry.IdentityStrong {
				return fmt.Errorf("publication identity unavailable: %s", entry.Path)
			}
			same = dev == entry.IdentityDev && ino == entry.IdentityIno && info.Size() == entry.IdentitySize && info.ModTime().Unix() == entry.IdentityModUnix
		}
		if afterCleanup && same {
			return fmt.Errorf("publication cleanup remains incomplete: %s", entry.Path)
		}
	}
	return nil
}
