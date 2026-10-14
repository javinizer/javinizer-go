package workflow

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"path/filepath"
)

// Failed completion is authoritative for batch-managed paths. Ordinary
// completion may already have promoted pins into plain Delete/MoveBack entries;
// remove those claims and retain only rollback's carried ownership proofs.
func reconcilePublicationRecovery(ledger models.GeneratedFilesJSON, recovery *models.PublicationRecovery) models.GeneratedFilesJSON {
	managed := map[string]bool{}
	for _, path := range recovery.Managed {
		managed[filepath.Clean(path)] = true
	}
	deletes := ledger.Delete[:0]
	for _, path := range ledger.Delete {
		if !managed[filepath.Clean(path)] {
			deletes = append(deletes, path)
		}
	}
	ledger.Delete = deletes
	pins := ledger.PlannedDeletes[:0]
	for _, entry := range ledger.PlannedDeletes {
		if !managed[filepath.Clean(entry.Path)] {
			pins = append(pins, entry)
		}
	}
	ledger.PlannedDeletes = append(pins, recovery.Deletes...)
	moves := ledger.MoveBack[:0]
	for _, entry := range ledger.MoveBack {
		if !managed[filepath.Clean(entry.NewPath)] {
			moves = append(moves, entry)
		}
	}
	ledger.MoveBack = append(moves, recovery.Moves...)
	return ledger
}
