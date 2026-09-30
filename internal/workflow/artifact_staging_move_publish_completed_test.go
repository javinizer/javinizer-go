package workflow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Move-lane mirror of artifact_staging_copy_publish_completed_test.go (codex
// P1, PRRT_kwDORn9KaM6nfbS3): in deferred move mode a subtitle publication can
// succeed and only the claimed-source cleanup refuse afterwards, surfacing
// ErrPublishCompleted. The destination then holds an INSTALLED output while the
// row reads as not-moved — without compensation the apply commits an untracked
// sidecar and drops the intent that could move it back.

// doctorMovePublishCompletedSubtitle rewrites every executed subtitle row into
// the finding's exact shape: the real move publish DID install the bytes at the
// destination and consumed the source off its path, but the claimed-source
// cleanup refused after the install, so the outcome reports Moved=false and
// carries an ErrPublishCompleted error.
func doctorMovePublishCompletedSubtitle(result *organizer.OrganizeResult) {
	for i := range result.Subtitles {
		result.Subtitles[i].Moved = false
		result.Subtitles[i].Error = fmt.Errorf("subtitle published but source cleanup refused — both copies retained (%w)", fsutil.ErrPublishCompleted)
	}
}

func movePublishCompletedOrganizer(base afero.Fs) *organizer.Organizer {
	return organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
}

func movePublishCompletedCommand(movie *models.Movie, match models.FileMatchInfo, dest string) ApplyCmd {
	cmd := pr260ArtifactFailureCommand(movie, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	return cmd
}

func movePublishCompletedProbe(t *testing.T, real *organizer.Organizer, stage *artifactStage, match models.FileMatchInfo, dest string) (*organizer.OrganizePlan, string) {
	t.Helper()
	probePlan, err := real.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: stage.original.Movie, DestDir: dest, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, err)
	subMoves := real.PlanSubtitleMoves(probePlan)
	require.Len(t, subMoves, 1)
	return probePlan, subMoves[0].NewPath
}

func moveIntentRecorded(moves []models.FileMove, originalPath, newPath string) bool {
	for _, mv := range moves {
		if filepath.Clean(mv.OriginalPath) == filepath.Clean(originalPath) && filepath.Clean(mv.NewPath) == filepath.Clean(newPath) {
			return true
		}
	}
	return false
}

// A publish-completed subtitle move error proves the sidecar WAS installed and
// its source consumed off the path: the pre-execution pending intent must
// survive the outcome reconciliation, so a crash or a later revert can still
// compensate the destination instead of committing an untracked file.
func TestDeferredMovePublishCompletedSubtitleKeepsMoveIntent(t *testing.T) {
	base, root, source, subtitle, _, unrelated, match := pr260FencedFiles(t, "move-publish-completed-keep")
	dest := filepath.Join(root, "library")
	real := movePublishCompletedOrganizer(base)
	fault := &pr260PublicationFaultOrganizer{Organizer: real, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		doctorMovePublishCompletedSubtitle(result)
	}}
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: fault, revertLog: ledger}
	cmd := movePublishCompletedCommand(&models.Movie{ContentID: "move-publish-completed-keep"}, match, dest)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	probePlan, subTarget := movePublishCompletedProbe(t, real, stage, match, dest)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: probePlan.TargetPath, FolderPath: probePlan.TargetDir}}

	require.NoError(t, stage.publish(context.Background(), orch, state, nil), "a publish-completed subtitle move error stays nonfatal")

	require.True(t, moveIntentRecorded(ledger.movesCaptured, subtitle, subTarget), "the pending subtitle move intent journaled pre-execution")
	require.Equal(t, int32(1), atomic.LoadInt32(&ledger.reconciles), "one outcome reconciliation after the direct move")
	assert.True(t, moveIntentRecorded(ledger.keepCaptured, subtitle, subTarget),
		"the publish-completed subtitle move keeps its durable intent — an installed output is never reconciled away")
	require.Len(t, state.organizeResult.Subtitles, 1)
	require.Error(t, state.organizeResult.Subtitles[0].Error, "the apply result still reports the subtitle error")
	assert.True(t, fsutil.PublishCompleted(state.organizeResult.Subtitles[0].Error))
	assert.False(t, state.organizeResult.Subtitles[0].Moved, "the row keeps the organizer's ambiguity shape")
	installed, readErr := afero.ReadFile(base, subTarget)
	require.NoError(t, readErr, "the sidecar bytes ARE installed at the destination")
	assert.Equal(t, "subtitle", string(installed))
	require.FileExists(t, probePlan.TargetPath)
	subGone, statErr := afero.Exists(base, subtitle)
	require.NoError(t, statErr)
	assert.False(t, subGone, "the move consumed the subtitle source off its path")
	pr260AssertRemoved(t, base, source)
	unrelatedBytes, readErr := afero.ReadFile(base, unrelated)
	require.NoError(t, readErr)
	assert.Equal(t, "unrelated", string(unrelatedBytes))
}

// The in-process arm side: when a later leg fails after the publish-completed
// subtitle installed, the rollback machinery must move the destination back
// onto its (vacant) source instead of stranding the install untracked.
func TestDeferredMovePublishCompletedSubtitleRollbackRestoresSource(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "move-publish-completed-rollback")
	dest := filepath.Join(root, "library")
	real := movePublishCompletedOrganizer(base)
	fault := &pr260PublicationFaultOrganizer{Organizer: real, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		doctorMovePublishCompletedSubtitle(result)
	}}
	ledger := &completeCallFaultLog{reconcileErr: errors.New("reconcile down")}
	orch := &applyOrchImpl{fs: base, organizer: fault, revertLog: ledger}
	cmd := movePublishCompletedCommand(&models.Movie{ContentID: "move-publish-completed-rollback"}, match, dest)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	probePlan, subTarget := movePublishCompletedProbe(t, real, stage, match, dest)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: probePlan.TargetPath, FolderPath: probePlan.TargetDir}}

	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorContains(t, publishErr, "reconcile move intents")

	subBack, readErr := afero.ReadFile(base, subtitle)
	require.NoError(t, readErr, "the armed subtitle inverse rolled the installed destination back onto its vacant source")
	assert.Equal(t, "subtitle", string(subBack))
	destGone, statErr := afero.Exists(base, subTarget)
	require.NoError(t, statErr)
	assert.False(t, destGone, "the install is compensated, never stranded untracked at the destination")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	pr260AssertNoFinals(t, base, dest)
}

// End-to-end mirror of TestDeferredCopyPublishCompletedSidecarPinSurvivesRevert:
// the publish-completed seat's move intent is NOT reconciled away — it rides the
// completion merge as a MoveBack arm, and a later revert renames the installed
// sidecar back onto its vacant (consumed) source rather than leaving it
// untracked at the destination.
func TestDeferredMovePublishCompletedSubtitleMoveBackSurvivesRevert(t *testing.T) {
	env := setupCopyIntentE2E(t, "move-publish-completed-revert")
	env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: env.org, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		doctorMovePublishCompletedSubtitle(result)
	}}
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err, "a publish-completed subtitle move error stays nonfatal end-to-end")
	require.NotNil(t, result.OrganizeResult)
	video := result.OrganizeResult.NewPath
	require.FileExists(t, video)

	subTarget := ""
	for _, sr := range result.OrganizeResult.Subtitles {
		if filepath.Ext(sr.NewPath) == ".srt" {
			require.Error(t, sr.Error, "the apply result still reports the subtitle error")
			assert.True(t, fsutil.PublishCompleted(sr.Error))
			assert.False(t, sr.Moved, "the row keeps the organizer's ambiguity shape: unreported as moved")
			subTarget = sr.NewPath
		}
	}
	require.NotEmpty(t, subTarget, "the subtitle outcome rode the apply result")
	installed, readErr := afero.ReadFile(env.fs, subTarget)
	require.NoError(t, readErr, "the sidecar bytes ARE installed at the destination")
	assert.Equal(t, "subtitle", string(installed))

	ledger := p3Ledger(t, env.repo, result.OperationID)
	assert.True(t, moveIntentRecorded(ledger.MoveBack, env.source, video), "the primary move intent survived completion")
	require.True(t, moveIntentRecorded(ledger.MoveBack, env.subtitle, subTarget),
		"the publish-completed subtitle intent survived reconcile and completion as a MoveBack arm")

	res, revErr := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)
	subBack, readErr := afero.ReadFile(env.fs, env.subtitle)
	require.NoError(t, readErr, "revert renamed the published sidecar back onto its vacant source")
	assert.Equal(t, "subtitle", string(subBack))
	destGone, statErr := afero.Exists(env.fs, subTarget)
	require.NoError(t, statErr)
	assert.False(t, destGone, "no untracked sidecar remains at the destination")
	videoBack, readErr := afero.ReadFile(env.fs, env.source)
	require.NoError(t, readErr, "the consumed primary still renames back onto its source")
	assert.Equal(t, "video", string(videoBack))
	unrelatedBytes, readErr := afero.ReadFile(env.fs, env.unrelated)
	require.NoError(t, readErr)
	assert.Equal(t, "unrelated", string(unrelatedBytes))
}
