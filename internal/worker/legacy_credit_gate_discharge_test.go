package worker

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestLegacyCreditGateDiagnosticDischargedAfterSuccessfulRetry(t *testing.T) {
	for _, message := range []string{"apply blocked by an open credit collision", "apply blocked because credit collision status is unavailable"} {
		t.Run(message, func(t *testing.T) {
			baseline := legacyGateFailureResult()
			baseline.Error = message
			path := baseline.FileMatchInfo.Path
			store := resultstore.New(1, []string{path})
			store.UpdateFileResult(path, baseline.Clone())
			wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: baseline.Movie.Clone()}}
			inputs := makeApplyInputs(wf)
			inputs.Updater = store
			inputs.Results = map[string]*resultstore.MovieResult{path: baseline.Clone()}
			NewApplyPhase().Run(t.Context(), inputs, ApplyPhaseConfig{Destination: t.TempDir(), RetryFilePaths: []string{path}})
			after, err := store.GetMovieResult(path)
			require.NoError(t, err)
			require.Equal(t, 1, wf.getApplyCalled())
			require.Equal(t, models.JobStatusCompleted, after.Status)
			require.Empty(t, after.Error)
			require.Empty(t, after.ErrorCode)
			require.Equal(t, "blocked", baseline.ErrorCode, "frozen baseline must not be rewritten")
		})
	}
}

func TestLegacyCreditGateDischargePreservesUnrelatedOrNewDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*resultstore.MovieResult, *resultstore.MovieResult)
	}{
		{"provider-block", func(b, c *resultstore.MovieResult) { b.Error = "provider blocked request" }},
		{"prefixed-message", func(b, c *resultstore.MovieResult) { b.Error = "provider: " + b.Error }},
		{"suffixed-message", func(b, c *resultstore.MovieResult) { b.Error += " (other failure)" }},
		{"missing-movie", func(b, c *resultstore.MovieResult) { b.Movie = nil }},
		{"missing-end", func(b, c *resultstore.MovieResult) { b.EndedAt = nil }},
		{"baseline-not-failed", func(b, c *resultstore.MovieResult) { b.Status = models.JobStatusCompleted }},
		{"new-poster-code", func(b, c *resultstore.MovieResult) { c.ErrorCode = downloader.PosterRecropRequiredCode }},
		{"new-gate-code", func(b, c *resultstore.MovieResult) { c.ErrorCode = models.FileErrorCodeCreditStatusUnavailable }},
		{"new-failure", func(b, c *resultstore.MovieResult) { c.Status = models.JobStatusFailed; c.Error = "disk full" }},
		{"cancelled", func(b, c *resultstore.MovieResult) { c.Status = models.JobStatusCancelled }},
		{"remaining-error", func(b, c *resultstore.MovieResult) { c.Error = "new error" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := legacyGateFailureResult()
			c := b.Clone()
			c.Status = models.JobStatusCompleted
			c.Error = ""
			tc.mutate(b, c)
			want := c.ErrorCode
			clearPriorCreditGateError(c, b)
			require.Equal(t, want, c.ErrorCode)
		})
	}
}

func legacyGateFailureResult() *resultstore.MovieResult {
	now := time.Now()
	return &resultstore.MovieResult{FileMatchInfo: models.FileMatchInfo{Path: "/synthetic/SYNTH-001.mkv", MovieID: "SYNTH-001"}, Movie: &models.Movie{ID: "SYNTH-001", ContentID: "synthetic-001"}, Status: models.JobStatusFailed, ErrorCode: "blocked", Error: "apply blocked by an open credit collision", EndedAt: &now}
}

func TestLegacyDischargeDoesNotAdmitFailedOrCancelledJobs(t *testing.T) {
	for _, status := range []models.JobStatus{models.JobStatusFailed, models.JobStatusCancelled, models.JobStatusRunning} {
		t.Run(string(status), func(t *testing.T) {
			result := legacyGateFailureResult()
			now := time.Now()
			snapshot := jobpersist.Snapshot{ID: "legacy-discharge-admission", Status: status, TotalFiles: 1, Files: []string{result.FileMatchInfo.Path}, CompletedAt: &now, Results: map[string]*resultstore.MovieResult{result.FileMatchInfo.Path: result}}
			row, err := jobpersist.Encode(snapshot)
			require.NoError(t, err)
			job := NewInMemoryJobStore().reconstructBatchJob(row)
			wf := &stubApplyWorkflow{}
			job.Controller().SetWorkflow(wf)
			require.Equal(t, status, job.GetStatus().Status)
			require.ErrorContains(t, job.Controller().StartApply(t.Context(), ApplyPhaseConfig{RetryFilePaths: snapshot.Files}), "expected status completed")
			require.Zero(t, wf.getApplyCalled())
			require.Equal(t, "blocked", job.GetStatus().Results[result.FileMatchInfo.Path].ErrorCode)
		})
	}
}
