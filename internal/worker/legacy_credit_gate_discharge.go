package worker

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// This is diagnostic recognition only; it grants no job admission or recovery.
func isTerminalLegacyCreditGateFailure(result *resultstore.MovieResult) bool {
	if result == nil || result.Status != models.JobStatusFailed || result.ErrorCode != "blocked" || result.Movie == nil ||
		result.Movie.ID == "" || result.Movie.ContentID == "" || result.FileMatchInfo.Path == "" ||
		result.EndedAt == nil || result.EndedAt.IsZero() {
		return false
	}
	return result.Error == "apply blocked by an open credit collision" || result.Error == "apply blocked because credit collision status is unavailable"
}
