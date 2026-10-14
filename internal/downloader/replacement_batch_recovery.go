package downloader

import (
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

// RecoverySnapshot describes remaining ownership after Rollback. Armed but
// uninstalled paths and proven foreign successors contribute no recovery act.
// An unavailable lookup retains the old, bound proof, never a new occupant.
func (b *ReplacementBatch) RecoverySnapshot() *models.PublicationRecovery {
	recovery := &models.PublicationRecovery{Managed: append([]string(nil), b.released...)}
	for _, leg := range append(append([]*replacementBatchLeg(nil), b.legs...), b.unarmedRecovery...) {
		recovery.Managed = append(recovery.Managed, leg.destination)
		recovery.Unresolved = recovery.Unresolved || leg.replaced
		if !leg.installed {
			continue
		}
		if _, err := fsutil.ObserveVerifiedInstall(b.fs, leg.destination, leg.installedID); fsutil.PublishSuccessorUnproven(err) {
			continue
		}
		recovery.Unresolved = true
		proof := models.DeleteEntry{Path: leg.destination, RecoveryUnproven: true}
		if target := leg.installedID.LinkTarget(); target != "" {
			// Link payload alone cannot distinguish a same-target successor.
			proof = models.DeleteEntry{Path: leg.destination, RecoveryUnproven: true}
		} else if info := leg.installedID.FileInfo(); info != nil {
			if dev, ino, strong := leg.installedID.ObjectIdentity(); strong {
				proof = models.DeleteEntry{Path: leg.destination, IdentityPinned: true, IdentityStrong: true, IdentityDev: dev, IdentityIno: ino, IdentitySize: info.Size(), IdentityModUnix: info.ModTime().Unix()}
			}
		}
		proof.RecoveryPublication = true
		if leg.rollbackOrigin != "" {
			recovery.Moves = append(recovery.Moves, models.FileMove{OriginalPath: leg.rollbackOrigin, NewPath: leg.destination, Proof: &proof})
		} else if !leg.replaced {
			recovery.Deletes = append(recovery.Deletes, proof)
		}
	}
	return recovery
}
