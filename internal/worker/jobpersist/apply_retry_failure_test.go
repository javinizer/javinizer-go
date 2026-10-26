package jobpersist

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestApplyRetryFailureProjectionGuardsAndRoundTrip(t *testing.T) {
	for _, variant := range []string{"matching", "already-failed", "blank-id", "missing-row", "new-revision", "new-identity", "other-path", "excluded", "no-movie", "running"} {
		t.Run(variant, func(t *testing.T) {
			row := &resultstore.MovieResult{ResultID: "r1", Revision: 7, RecoveryWarning: "previous apply warning", Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "ABC-123"}}
			failures := map[string]ApplyRetryFailure{"a": {ResultID: "r1", Revision: 7, Error: "fenced"}}
			excluded := map[string]bool{}
			switch variant {
			case "already-failed":
				row.Status = models.JobStatusFailed
			case "blank-id":
				failures["a"] = ApplyRetryFailure{Revision: 7}
			case "missing-row":
				row = nil
			case "new-revision":
				row.Revision++
			case "new-identity":
				row.ResultID = "r2"
			case "other-path":
				failures["b"] = failures["a"]
				delete(failures, "a")
			case "excluded":
				excluded["a"] = true
			case "no-movie":
				row.Movie = nil
			case "running":
				row.Status = models.JobStatusRunning
			}
			raw, err := Encode(Snapshot{ID: "retry-ledger", Results: map[string]*resultstore.MovieResult{"a": row}, ApplyRetryFailures: failures, Excluded: excluded})
			require.NoError(t, err)
			restored, errs := Decode(raw)
			require.Empty(t, errs)
			require.Equal(t, failures, restored.ApplyRetryFailures)
			var before *resultstore.MovieResult
			if restored.Results["a"] != nil {
				before = restored.Results["a"].Clone()
			}
			delta := ProjectApplyRetryFailures(restored.Results, restored.Excluded, restored.ApplyRetryFailures)
			if variant == "matching" || variant == "already-failed" {
				if variant == "matching" {
					require.Equal(t, 1, delta)
				} else {
					require.Zero(t, delta)
				}
				require.Empty(t, restored.Results["a"].RecoveryWarning)
				require.Equal(t, models.JobStatusFailed, restored.Results["a"].Status)
				require.Equal(t, uint64(7), restored.Results["a"].Revision)
			} else {
				require.Zero(t, delta)
				require.Equal(t, before, restored.Results["a"])
			}
		})
	}
}
