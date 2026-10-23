package jobpersist

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// Matches binds a failure to one admitted identity and revision, never a newer edit.
func (failure ApplyRetryFailure) Matches(result *resultstore.MovieResult) bool {
	return result != nil && failure.ResultID != "" && result.ResultID == failure.ResultID && result.Revision == failure.Revision
}

// ProjectApplyRetryFailures overlays retry status on a disposable read snapshot.
// Never call this on the domain snapshot used for persistence or witness recovery.
// The return value is the number of completed rows projected as failed.
func ProjectApplyRetryFailures(results map[string]*resultstore.MovieResult, excluded map[string]bool, failures map[string]ApplyRetryFailure) int {
	delta := 0
	for path, failure := range failures {
		result := results[path]
		if !failure.Matches(result) || result.Movie == nil || excluded[path] {
			continue
		}
		if result.Status != models.JobStatusCompleted && result.Status != models.JobStatusFailed {
			continue
		}
		if result.Status == models.JobStatusCompleted {
			delta++
		}
		result.Status = models.JobStatusFailed
		result.Error = failure.Error
		result.ErrorCode = "APPLY_RECEIPT_PERSIST_FAILED"
		result.VideoOutcome = ""
		result.VideoCollision = nil
	}
	return delta
}
