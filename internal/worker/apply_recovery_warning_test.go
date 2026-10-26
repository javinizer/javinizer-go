package worker

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/javinizer/javinizer-go/internal/database"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestCommittedRecoveryWarningDoesNotOfferApplyRetry(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "cancelled-after-commit"}[cancelled], func(t *testing.T) {
			const path = "/source/TEST-001.mp4"
			const warning = "Output committed; transaction backup cleanup pending. Do not reapply."
			movie := &models.Movie{ID: "TEST-001", ContentID: "test001"}
			result := &workflow.ApplyResult{Movie: movie, PublicationFinalized: true, RecoveryWarning: warning, VideoOutcome: "replaced"}
			inputs := makeApplyInputs(&stubApplyWorkflow{applyResult: result})
			fenceCalled := false
			inputs.MovieRepo = &cleanupFenceRepository{fence: func(writeCtx context.Context, _ string, _ int64, publish func(*models.Movie) error) error {
				fenceCalled = true
				require.NoError(t, writeCtx.Err(), "known committed warning writeback survives request cancellation")
				_, bounded := writeCtx.Deadline()
				require.True(t, bounded)
				return publish(movie)
			}}
			updater := inputs.Updater.(*stubUpdater)
			updater.UpdateFileResult(path, &resultstore.MovieResult{Movie: movie, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID}})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if cancelled {
				cancel()
			}
			organized, failed, warned := 0, 0, 0
			cfg := ApplyPhaseConfig{OnFileOrganized: func(string) { organized++ }, OnFileRecoveryWarning: func(_ string, got string) { require.Equal(t, warning, got); warned++ }, OnFileFailed: func(string, string) { failed++ }}
			afc := &ApplyFileContext{FilePath: path, Movie: movie, Match: models.FileMatchInfo{Path: path, MovieID: movie.ID}}
			outcome := interpretApplyResult(path, movie, time.Now(), time.Minute, inputs, cfg, ctx, afc, result, nil)
			require.True(t, fenceCalled)
			require.True(t, outcome.Success)
			require.False(t, outcome.Failed)
			require.False(t, outcome.Cancelled)
			require.Zero(t, organized)
			require.Equal(t, 1, warned)
			require.Zero(t, failed)
			stored := updater.getResult(path)
			require.Equal(t, warning, stored.RecoveryWarning)
			require.Equal(t, "replaced", stored.VideoOutcome)
			raw, err := json.Marshal(stored)
			require.NoError(t, err)
			var restored resultstore.MovieResult
			require.NoError(t, json.Unmarshal(raw, &restored))
			require.Equal(t, warning, restored.RecoveryWarning)
			events := inputs.Broadcaster.(*stubBroadcaster).events
			require.NotEmpty(t, events)
			require.Equal(t, StepComplete, events[len(events)-1].Step)
			require.Contains(t, events[len(events)-1].Message, warning)
		})
	}
}

func TestApplyPhaseRecoveryWarningCompletesOnce(t *testing.T) {
	const path = "/source/TEST-002.mp4"
	const warning = "Files updated; backup cleanup pending."
	movie := &models.Movie{ID: "TEST-002"}
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: movie, PublicationFinalized: true, RecoveryWarning: warning}}
	inputs := makeApplyInputs(wf)
	row := &resultstore.MovieResult{Movie: movie, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID}}
	inputs.Results[path] = row
	inputs.Updater.(*stubUpdater).UpdateFileResult(path, row)
	organized, failed, warned := 0, 0, 0
	NewApplyPhase().Run(t.Context(), inputs, ApplyPhaseConfig{Destination: "/output", OnFileOrganized: func(string) { organized++ }, OnFileRecoveryWarning: func(_ string, got string) { require.Equal(t, warning, got); warned++ }, OnFileFailed: func(string, string) { failed++ }})
	require.Equal(t, 1, wf.getApplyCalled())
	require.Zero(t, organized)
	require.Equal(t, 1, warned)
	require.Zero(t, failed)
	require.Equal(t, warning, inputs.Updater.(*stubUpdater).getResult(path).RecoveryWarning)
	require.True(t, inputs.Lifecycle.(*stubLifecycle).organized)
}

func TestUnfinalizedRecoveryWarningOnlyWriteAndRefusal(t *testing.T) {
	for _, refusal := range []string{"", "database", "stale", "witness"} {
		t.Run(refusal, func(t *testing.T) {
			const path = "/source/TEST-003.mp4"
			const warning = "Files updated, but journal finalization is pending; do not reapply."
			baseline := &models.Movie{ID: "TEST-003", ContentID: "test003", Title: "Before"}
			changed := baseline.Clone()
			changed.Title = "After"
			result := &workflow.ApplyResult{Movie: changed, PublicationFinalized: false, RecoveryWarning: warning, VideoOutcome: "replaced"}
			inputs := makeApplyInputs(&stubApplyWorkflow{applyResult: result})
			updater := inputs.Updater.(*stubUpdater)
			original := &resultstore.MovieResult{Movie: baseline, Status: models.JobStatusFailed, Error: "old apply failure", VideoOutcome: "old", FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: baseline.ID}}
			updater.UpdateFileResult(path, original)
			fenceCalls := 0
			inputs.MovieRepo = &cleanupFenceRepository{fence: func(ctx context.Context, _ string, _ int64, publish func(*models.Movie) error) error {
				fenceCalls++
				require.NoError(t, ctx.Err())
				_, bounded := ctx.Deadline()
				require.True(t, bounded)
				if refusal == "database" {
					return errors.New("database refused warning write")
				}
				if refusal == "stale" {
					return database.ErrApplyPublicationStale
				}
				return publish(baseline)
			}}
			if refusal == "witness" {
				inputs.PromoteWitnessFn = func(string) bool { return true }
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			warned, clean, failed := 0, 0, 0
			cfg := ApplyPhaseConfig{OnFileRecoveryWarning: func(_ string, got string) { require.Equal(t, warning, got); warned++ }, OnFileOrganized: func(string) { clean++ }, OnFileFailed: func(string, string) { failed++ }}
			afc := &ApplyFileContext{FilePath: path, Movie: baseline, Match: original.FileMatchInfo}
			outcome := interpretApplyResult(path, baseline, time.Now(), time.Minute, inputs, cfg, ctx, afc, result, nil)
			require.True(t, outcome.Success)
			require.False(t, result.PublicationFinalized)
			require.Equal(t, 1, warned)
			require.Zero(t, clean)
			require.Zero(t, failed)
			if refusal == "witness" {
				require.Zero(t, fenceCalls)
			} else {
				require.Equal(t, 1, fenceCalls)
			}
			stored := updater.getResult(path)
			require.Equal(t, "Before", stored.Movie.Title)
			require.Equal(t, models.JobStatusFailed, stored.Status)
			require.Equal(t, "old apply failure", stored.Error)
			require.Equal(t, "old", stored.VideoOutcome)
			if refusal == "" {
				require.Equal(t, warning, stored.RecoveryWarning)
			} else {
				require.Empty(t, stored.RecoveryWarning)
			}
			events := inputs.Broadcaster.(*stubBroadcaster).events
			require.Contains(t, events[len(events)-1].Message, warning)
		})
	}
}

func TestActualApplyFailureClearsPreviousRecoveryWarning(t *testing.T) {
	for _, collision := range []bool{false, true} {
		const path = "/source/TEST-004.mp4"
		movie := &models.Movie{ID: "TEST-004"}
		inputs := makeApplyInputs(nil)
		updater := inputs.Updater.(*stubUpdater)
		row := &resultstore.MovieResult{Movie: movie, RecoveryWarning: "old warning", Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID}}
		updater.UpdateFileResult(path, row)
		failed := 0
		cfg := ApplyPhaseConfig{OnFileFailed: func(string, string) { failed++ }}
		if collision {
			markCollisionGateFailure(inputs, cfg, path, row, "new collision", "VIDEO_COLLISION_STALE")
		} else {
			afc := &ApplyFileContext{FilePath: path, Movie: movie, Match: row.FileMatchInfo}
			interpretApplyResult(path, movie, time.Now(), time.Minute, inputs, cfg, t.Context(), afc, nil, errors.New("new failure"))
		}
		require.Equal(t, 1, failed)
		require.Empty(t, updater.getResult(path).RecoveryWarning)
		require.Equal(t, models.JobStatusFailed, updater.getResult(path).Status)
	}
}
