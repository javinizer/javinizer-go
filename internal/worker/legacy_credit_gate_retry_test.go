package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestLegacyCreditGateTransientFailureThenSuccess(t *testing.T) {
	for _, message := range []string{"apply blocked by an open credit collision", "apply blocked because credit collision status is unavailable"} {
		for _, reload := range []bool{false, true} {
			t.Run(message+"/"+map[bool]string{false: "same-session", true: "persisted-reload"}[reload], func(t *testing.T) {
				baseline := legacyGateFailureResult()
				baseline.Error = message
				path := baseline.FileMatchInfo.Path
				now := time.Now()
				row, err := jobpersist.Encode(jobpersist.Snapshot{ID: "legacy-two-attempt", Status: models.JobStatusCompleted, TotalFiles: 1, Files: []string{path}, CompletedAt: &now, Results: map[string]*resultstore.MovieResult{path: baseline}})
				require.NoError(t, err)
				job := NewInMemoryJobStore().reconstructBatchJob(row)
				job.Controller().SetWorkflow(&stubApplyWorkflow{applyErr: errors.New("synthetic disk full")})
				cfg := ApplyPhaseConfig{Destination: t.TempDir(), RetryFilePaths: []string{path}}
				require.NoError(t, job.Controller().StartApply(t.Context(), cfg))
				require.NoError(t, job.Controller().Wait())
				first := job.GetStatus().Results[path]
				require.Equal(t, models.JobStatusFailed, first.Status)
				require.Equal(t, "synthetic disk full", first.Error)
				// Discharge the recognized gate code before replacing its only
				// identifying message with this unrelated apply failure.
				require.Empty(t, first.ErrorCode)
				require.Equal(t, models.JobStatusCompleted, job.GetStatus().Status)
				if reload {
					row, err = jobpersist.Encode(jobpersist.Snapshot{ID: "legacy-two-attempt-reloaded", Status: job.GetStatus().Status, TotalFiles: 1, Files: []string{path}, CompletedAt: &now, Results: map[string]*resultstore.MovieResult{path: first}})
					require.NoError(t, err)
					job = NewInMemoryJobStore().reconstructBatchJob(row)
					reloaded := job.GetStatus().Results[path]
					require.Equal(t, first.Status, reloaded.Status)
					require.Equal(t, first.Error, reloaded.Error)
					require.Empty(t, reloaded.ErrorCode)
				}
				job.Controller().SetWorkflow(&stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: baseline.Movie.Clone()}})
				require.NoError(t, job.Controller().StartApply(t.Context(), cfg))
				require.NoError(t, job.Controller().Wait())
				final := job.GetStatus().Results[path]
				require.Equal(t, models.JobStatusCompleted, final.Status)
				require.Empty(t, final.Error)
				require.Empty(t, final.ErrorCode)
				require.Equal(t, "blocked", baseline.ErrorCode)
				require.Equal(t, message, baseline.Error)
			})
		}
	}
}

func TestLegacyCreditGateFailureReplacementPreservesOutcome(t *testing.T) {
	for _, message := range []string{"apply blocked by an open credit collision", "apply blocked because credit collision status is unavailable"} {
		for _, tc := range []struct {
			name   string
			err    error
			status models.JobStatus
			code   string
			text   string
		}{
			{"disk-full", errors.New("synthetic disk full"), models.JobStatusFailed, "", "synthetic disk full"},
			{"deadline", context.DeadlineExceeded, models.JobStatusFailed, "", "apply timed out after 0s"},
			{"cancelled", context.Canceled, models.JobStatusCancelled, "", "organization canceled"},
			{"new-poster-refusal", downloader.ErrPosterRecropRequired, models.JobStatusFailed, downloader.PosterRecropRequiredCode, downloader.ErrPosterRecropRequired.Error()},
		} {
			t.Run(message+"/"+tc.name, func(t *testing.T) {
				baseline := legacyGateFailureResult()
				baseline.Error = message
				path := baseline.FileMatchInfo.Path
				store := resultstore.New(1, []string{path})
				store.UpdateFileResult(path, baseline.Clone())
				wf := &stubApplyWorkflow{applyErr: tc.err}
				inputs := makeApplyInputs(wf)
				inputs.Updater = store
				inputs.Results = map[string]*resultstore.MovieResult{path: baseline.Clone()}
				NewApplyPhase().Run(t.Context(), inputs, ApplyPhaseConfig{Destination: t.TempDir(), RetryFilePaths: []string{path}})
				after, err := store.GetMovieResult(path)
				require.NoError(t, err)
				require.Equal(t, 1, wf.getApplyCalled())
				require.Equal(t, tc.status, after.Status)
				require.Equal(t, tc.text, after.Error)
				require.Equal(t, tc.code, after.ErrorCode)
				row, err := jobpersist.Encode(jobpersist.Snapshot{ID: "legacy-replacement", Results: map[string]*resultstore.MovieResult{path: after}})
				require.NoError(t, err)
				reloaded := NewInMemoryJobStore().reconstructBatchJob(row).GetStatus().Results[path]
				require.Equal(t, after.Status, reloaded.Status)
				require.Equal(t, after.Error, reloaded.Error)
				require.Equal(t, after.ErrorCode, reloaded.ErrorCode)
			})
		}
	}
}

func TestLegacyCreditGateRetryPreservesOtherDiagnosticCodes(t *testing.T) {
	for _, outcome := range []struct {
		name string
		err  error
	}{{"success", nil}, {"disk-full", errors.New("synthetic disk full")}} {
		for _, tc := range []struct {
			name     string
			baseline string
			liveCode string
			liveText string
		}{
			{name: "unrelated-provider-baseline", baseline: "provider blocked request", liveCode: "blocked"},
			{name: "unrecognized-prior-disk-full", baseline: "synthetic disk full", liveCode: "blocked"},
			{name: "new-generic-block", liveCode: "blocked", liveText: "new provider blocked request"},
			{name: "new-poster-code", liveCode: downloader.PosterRecropRequiredCode, liveText: "new poster failure"},
			{name: "new-gate-code", liveCode: models.FileErrorCodeCreditStatusUnavailable, liveText: "new gate lookup failure"},
			{name: "cleared-code", liveCode: "", liveText: "user cleared diagnostic"},
		} {
			t.Run(outcome.name+"/"+tc.name, func(t *testing.T) {
				baseline := legacyGateFailureResult()
				if tc.baseline != "" {
					baseline.Error = tc.baseline
				}
				path := baseline.FileMatchInfo.Path
				store := resultstore.New(1, []string{path})
				store.UpdateFileResult(path, baseline.Clone())
				wf := &stubApplyWorkflow{applyErr: outcome.err, applyResult: &workflow.ApplyResult{Movie: baseline.Movie.Clone()}}
				inputs := makeApplyInputs(wf)
				inputs.Updater = store
				inputs.Results = map[string]*resultstore.MovieResult{path: baseline.Clone()}
				cfg := ApplyPhaseConfig{Destination: t.TempDir(), RetryFilePaths: []string{path}}
				if tc.liveText != "" {
					cfg.PostApplyFunc = func(_ context.Context, _ *ApplyFileContext, _ *ApplyFileResult) {
						require.NoError(t, store.AtomicUpdateFileResult(path, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
							current.ErrorCode = tc.liveCode
							current.Error = tc.liveText
							return current, nil
						}))
					}
				}
				NewApplyPhase().Run(t.Context(), inputs, cfg)
				after, err := store.GetMovieResult(path)
				require.NoError(t, err)
				require.Equal(t, 1, wf.getApplyCalled())
				require.Equal(t, tc.liveCode, after.ErrorCode)
				if outcome.err == nil {
					require.Equal(t, models.JobStatusCompleted, after.Status)
					require.Empty(t, after.Error)
				} else {
					require.Equal(t, models.JobStatusFailed, after.Status)
					require.Equal(t, outcome.err.Error(), after.Error)
				}
			})
		}
	}
}

func TestLegacyCreditGateDischargeRequiresUnchangedLiveDiagnostic(t *testing.T) {
	for _, status := range []models.JobStatus{models.JobStatusCompleted, models.JobStatusCancelled, models.JobStatusRunning} {
		t.Run(string(status), func(t *testing.T) {
			baseline := legacyGateFailureResult()
			current := baseline.Clone()
			current.Status = status
			clearPriorCreditGateError(current, baseline)
			require.Equal(t, "blocked", current.ErrorCode)
			require.Equal(t, baseline.Error, current.Error)
			require.Equal(t, status, current.Status)
		})
	}
	baseline := legacyGateFailureResult()
	current := baseline.Clone()
	clearPriorCreditGateError(current, baseline)
	require.Empty(t, current.ErrorCode)
	require.Equal(t, baseline.Error, current.Error)
	require.Equal(t, baseline.Status, current.Status)
	require.Equal(t, "blocked", baseline.ErrorCode)
}
