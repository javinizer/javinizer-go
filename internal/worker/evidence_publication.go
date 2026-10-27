package worker

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// publishInitialScrapeResult preserves the default-off publication path. An
// opted-in result and its report must cross one store-lock boundary. Failed
// atomic publication may publish ordinary metadata, but never a split report.
func publishInitialScrapeResult(updater resultstore.ResultUpdater, filePath string, result *resultstore.MovieResult, prov *resultstore.ProvenanceData) {
	if prov != nil && prov.OfflineEvidence != nil {
		err := updater.AtomicUpdateFileResultWithProvenance(filePath, func(current *resultstore.MovieResult, currentProv *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
			owned := result.Clone()
			if current.ResultID != "" {
				owned.ResultID = current.ResultID
			}
			return owned, resultstore.ScrapeReplacementProvenance(currentProv, prov), nil
		})
		if err == nil {
			return
		}
		prov = prov.Clone()
		prov.OfflineEvidence = nil
	}
	updater.UpdateFileResult(filePath, result)
	if prov != nil {
		updater.SetProvenance(filePath, prov)
	}
}

// Clearing a historical report also requires a coherent publication; a legacy
// store restored from a newer envelope must not retain stale opted-out evidence.
func requiresAtomicEvidence(rm resultstore.ResultMapAccessor, filePath string, prov *resultstore.ProvenanceData) bool {
	if prov != nil && prov.OfflineEvidence != nil {
		return true
	}
	old := rm.SnapshotData().Provenance[filePath]
	return old != nil && old.OfflineEvidence != nil
}

func markUnpersistedEvidenceCancelled(updater resultstore.ResultUpdater, path string, cause error) {
	_ = updater.AtomicUpdateFileResultWithProvenance(path, func(current *resultstore.MovieResult, prov *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
		if current.Persisted {
			return current, prov, nil
		}
		current.Status = models.JobStatusCancelled
		current.Error = cause.Error()
		if prov != nil {
			prov.OfflineEvidence = nil
		}
		return current, prov, nil
	})
}
