package batch

import (
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRecoveryWarningAPIAndProgress(t *testing.T) {
	const warning = "Output committed; backup cleanup pending. Do not reapply."
	mr := &resultstore.MovieResult{RecoveryWarning: warning, Status: models.JobStatusCompleted}
	require.Equal(t, warning, movieResultToResponse(mr, nil).RecoveryWarning)
	require.Equal(t, warning, movieResultToSlimResponse(mr, nil).RecoveryWarning)
	for _, update := range []bool{false, true} {
		job := &stubControlledJob{status: &worker.BatchJobStatus{Results: map[string]*resultstore.MovieResult{"/f.mp4": mr}}}
		var got *websocket.ProgressMessage
		makeOrganizeFileOrganizedBroadcaster(job, update, func(msg *websocket.ProgressMessage) { got = msg })("/f.mp4")
		require.NotNil(t, got)
		require.Equal(t, warning, got.RecoveryWarning)
		require.NotEqual(t, websocket.ProgressStatus("failed"), got.Status)
		emitter := &ctxEnforcingEmitter{}
		deps := &core.APIDeps{EventEmitter: emitter}
		gen := uint64(1)
		hook := makeOrganizePostApplyAuditHook(deps, job, &gen)
		if update {
			hook = makeUpdatePostApplyAuditHook(deps, job, &gen)
		}
		hook(preCanceledCtx(), &worker.ApplyFileContext{FilePath: "/f.mp4", Movie: &models.Movie{ID: "TEST-001"}}, &worker.ApplyFileResult{Result: &workflow.ApplyResult{RecoveryWarning: warning, PublicationFinalized: true}})
		require.Empty(t, emitter.drops)
		found := false
		for _, event := range emitter.calls {
			require.NotEqual(t, models.SeverityError, event.severity)
			if event.severity == models.SeverityWarn {
				require.Contains(t, event.message, warning)
				found = true
			}
		}
		require.True(t, found)
	}
}

func TestRecoveryWarningProgressSurvivesRefusedPersistence(t *testing.T) {
	const warning = "Files updated; journal finalization is pending. Do not reapply."
	for _, update := range []bool{false, true} {
		job := &stubControlledJob{status: &worker.BatchJobStatus{Results: map[string]*resultstore.MovieResult{"/f.mp4": {Status: models.JobStatusFailed, Error: "old failure"}}}}
		var got *websocket.ProgressMessage
		var cfg worker.ApplyPhaseConfig
		ref := wireApplyProgressCallbacks(&cfg, job, update, func(msg *websocket.ProgressMessage) { got = msg })
		*ref = 23
		require.NotNil(t, cfg.OnFileRecoveryWarning)
		cfg.OnFileRecoveryWarning("/f.mp4", warning)
		require.NotNil(t, got)
		require.Equal(t, warning, got.RecoveryWarning)
		require.Equal(t, uint64(23), got.ApplyGeneration)
		require.Empty(t, got.Error)
		require.NotEqual(t, websocket.ProgressStatus("failed"), got.Status)
	}
}
