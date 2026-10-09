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
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// doctorPublishCompletedSubtitle rewrites every executed subtitle row into the
// finding's exact shape (codex P2, PRRT_kwDORn9KaM6m5-ms): the real copy
// publish DID install the bytes at the destination, but the outcome reports
// Copied=false and carries an ErrPublishCompleted error because the
// post-publish leg refused after the install.
func doctorPublishCompletedSubtitle(result *organizer.OrganizeResult) {
	for i := range result.Subtitles {
		result.Subtitles[i].Copied = false
		result.Subtitles[i].Error = fmt.Errorf("subtitle published but source cleanup refused — both copies retained (%w)", fsutil.ErrPublishCompleted)
	}
}

// A publish-completed subtitle error proves the sidecar WAS installed: its
// target must join the confirmed-copy set, so the armed batch leg confirms
// (never releases), the durable hash pin survives the delete-intent
// reconciliation, and the apply still reports the subtitle error nonfatally.
func TestDeferredCopyPublishCompletedSidecarKeepsPin(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "copy-publish-completed-keep")
	dest := filepath.Join(root, "library")
	real := copyIntentOrganizer(base)
	fault := &pr260PublicationFaultOrganizer{Organizer: real, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		doctorPublishCompletedSubtitle(result)
	}}
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: fault, revertLog: ledger}
	cmd := copyIntentCommand(&models.Movie{ContentID: "copy-publish-completed-keep"}, match, dest)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, finalPlan := copyIntentPlans(t, real, stage, match, source, dest)
	subMoves := real.PlanSubtitleMoves(finalPlan)
	require.Len(t, subMoves, 1)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	require.NoError(t, stage.publish(context.Background(), orch, state, nil), "a publish-completed subtitle error stays nonfatal")

	assert.Equal(t, int32(1), atomic.LoadInt32(&ledger.deleteReconciles), "one settle transaction after the primary confirm")
	assert.Equal(t, []string{subMoves[0].NewPath}, ledger.deleteKeepCaptured,
		"the publish-completed sidecar keeps its durable pin — installed bytes stay revert-cleanable")
	require.Len(t, state.organizeResult.Subtitles, 1)
	require.Error(t, state.organizeResult.Subtitles[0].Error, "the apply result still reports the subtitle error")
	assert.True(t, fsutil.PublishCompleted(state.organizeResult.Subtitles[0].Error))
	assert.False(t, state.organizeResult.Subtitles[0].Copied, "the row keeps the organizer's ambiguity shape")
	require.FileExists(t, subMoves[0].NewPath, "the sidecar bytes ARE installed")
	require.FileExists(t, finalPlan.TargetPath)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

// The disjoint classification side: a Copied=false subtitle whose error is a
// PLAIN failure (nothing provably installed) keeps the unconfirmed treatment —
// only the publish-completed class joins the confirmed-copy set.
func TestDeferredCopyPlainSubtitleErrorStaysUnconfirmed(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "copy-subtitle-plain-error")
	dest := filepath.Join(root, "library")
	real := copyIntentOrganizer(base)
	fault := &pr260PublicationFaultOrganizer{Organizer: real, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		for i := range result.Subtitles {
			result.Subtitles[i].Copied = false
			result.Subtitles[i].Error = errors.New("subtitle publish refused pre-install")
		}
	}}
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: fault, revertLog: ledger}
	cmd := copyIntentCommand(&models.Movie{ContentID: "copy-subtitle-plain-error"}, match, dest)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, _ := copyIntentPlans(t, real, stage, match, source, dest)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	require.NoError(t, stage.publish(context.Background(), orch, state, nil), "subtitle errors stay nonfatal")
	assert.Equal(t, int32(1), atomic.LoadInt32(&ledger.deleteReconciles))
	assert.Empty(t, ledger.deleteKeepCaptured, "only the publish-completed class joins the confirmed-copy set")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}

// End-to-end mirror of TestDeferredCopySkippedSidecarPinRetractedBeforeRevert:
// the publish-completed seat's durable pin is NOT retracted — it survives the
// completion merge still hash-pinned (the error-carrying row never enters the
// plain Delete generated-files payload), and a later revert reaps exactly the
// installed sidecar while retaining the primary and every source.
func TestDeferredCopyPublishCompletedSidecarPinSurvivesRevert(t *testing.T) {
	env := setupCopyIntentE2E(t, "copy-publish-completed-revert")
	probePlan, planErr := env.org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, ForceUpdate: true, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, planErr)
	subMoves := env.org.PlanSubtitleMoves(probePlan)
	require.Len(t, subMoves, 1)
	subTarget := subMoves[0].NewPath

	env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: env.org, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		doctorPublishCompletedSubtitle(result)
	}}
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err, "a publish-completed subtitle error stays nonfatal end-to-end")
	require.NotNil(t, result.OrganizeResult)
	video := result.OrganizeResult.NewPath
	require.FileExists(t, video)

	reported := false
	for _, sr := range result.OrganizeResult.Subtitles {
		if filepath.Clean(sr.NewPath) == filepath.Clean(subTarget) {
			require.Error(t, sr.Error, "the apply result still reports the subtitle error")
			assert.True(t, fsutil.PublishCompleted(sr.Error))
			assert.False(t, sr.Copied, "the row keeps the organizer's ambiguity shape: unreported as copied")
			reported = true
		}
	}
	require.True(t, reported, "the subtitle outcome rode the apply result")

	installed, readErr := afero.ReadFile(env.fs, subTarget)
	require.NoError(t, readErr, "the sidecar bytes ARE installed at the destination")
	assert.Equal(t, "subtitle", string(installed))

	ledger := p3Ledger(t, env.repo, result.OperationID)
	pinned := false
	for _, pd := range ledger.PlannedDeletes {
		require.NotEqual(t, video, pd.Path, "the graduated primary pin was consumed")
		if pd.Path == subTarget {
			pinned = true
		}
	}
	assert.True(t, pinned, "the publish-completed sidecar's durable pin survived reconcile and completion")
	assert.NotContains(t, ledger.Delete, subTarget,
		"the error-carrying row stays out of plain Delete; the hash pin is its ledger record")

	res, revErr := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)
	subGone, statErr := afero.Exists(env.fs, subTarget)
	require.NoError(t, statErr)
	assert.False(t, subGone, "revert reaps the installed sidecar through the retained pin")
	require.FileExists(t, video, "the installed copy primary is retained")
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
}
