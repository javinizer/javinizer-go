package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
)

// TestApply_UnauthorizedDuplicateConflict_FinalizesNoOpRow is the codex PR
// #241 batch-2 F2 regression, end-to-end: a ForceUpdate=false intra-batch
// duplicate REJECTION fails the organize step with no OrganizeResult
// (validation conflict — nothing ever executed, nothing mutated) AFTER Begin
// created the revert row. Pre-fix that row lingered failed-with-empty-NewPath
// and every batch revert probed its "" anchor as anchor_missing, so the batch
// could never report fully reverted. The conflict-rejection row now finalizes
// completed-noop — exactly the dup-skip r13 terminal: excluded from revert
// selection across consumers — while the winner's real row unwinds normally.
func TestApply_UnauthorizedDuplicateConflict_FinalizesNoOpRow(t *testing.T) {
	ctx := context.Background()
	repo, fs, _, orchFor, cmdForForce := newW241PrePublishRig(t)

	// Same shared-destination rig, but UNAUTHORIZED (ForceUpdate=false): the
	// loser's observe lands the ConflictDuplicate on its plan and validation
	// rejects it through the ordinary failure pipeline (nil result + error).
	cmdFor := func(movieID, src, name string) ApplyCmd {
		cmd := cmdForForce(movieID, src, name)
		cmd.Organize.ForceUpdate = false
		return cmd
	}

	winnerRes, winnerErr := orchFor().Execute(ctx, cmdFor("ABC-100", "/in/A.mkv", "A.mkv"))
	require.NoError(t, winnerErr)
	require.NotNil(t, winnerRes.OrganizeResult)
	require.True(t, winnerRes.OrganizeResult.Moved)
	assert.Equal(t, []byte("a-bytes"), mustReadW241(t, fs, w241Target))

	loserRes, loserErr := orchFor().Execute(ctx, cmdFor("ABC-200", "/in/B.mkv", "B.mkv"))
	require.Error(t, loserErr, "the unauthorized duplicate is rejected, not skipped")
	require.NotNil(t, loserRes)
	assert.Equal(t, "organize", loserRes.FailedStep)
	assert.Contains(t, loserErr.Error(), "organization validation failed")
	assert.True(t, loserRes.PrePublication,
		"a conflict rejection mutates nothing — the pre-publication marker reaches the caller")
	assert.Nil(t, loserRes.OrganizeResult, "plan rejections keep the established (nil, err) organizer contract")

	// Journal truth: the rejected row is completed-noop with NO anchorless
	// shape — no failed-with-\"\" row left to probe anchor_missing forever.
	rowL := w241Row(t, repo, loserRes.OperationID)
	assert.Equal(t, models.RevertStatusNoOp, rowL.RevertStatus,
		"codex batch-2 F2: conflict-rejection rows get the dup-skip r13 terminal treatment")
	assert.Empty(t, rowL.NewPath)
	assert.False(t, rowL.InPlaceRenamed)
	assert.Empty(t, rowL.NFOPath)
	rowW := w241Row(t, repo, winnerRes.OperationID)
	assert.Equal(t, models.RevertStatusApplied, rowW.RevertStatus)
	assert.Equal(t, w241Target, filepath.ToSlash(rowW.NewPath))

	// RevertBatch reaches fully-reverted math: only the winner's row is
	// processable, it unwinds, and no anchor-missing skip holds the job open.
	reverter := history.NewReverter(fs, repo)
	rb, err := reverter.RevertBatch(ctx, "job-w241")
	require.NoError(t, err)
	assert.Equal(t, 1, rb.Total, "the noop rejection row never enters revert selection")
	assert.Equal(t, 1, rb.Succeeded)
	assert.Zero(t, rb.Skipped, "no anchor_missing outcome — the batch reports fully reverted")
	assert.Zero(t, rb.Failed)
	assert.Equal(t, []byte("a-bytes"), mustReadW241(t, fs, "/in/A.mkv"), "the winner's move unwound")
	assert.Equal(t, []byte("b-bytes"), mustReadW241(t, fs, "/in/B.mkv"),
		"the rejected loser's untouched source is never dislodged")
	exists, err := afero.Exists(fs, filepath.FromSlash(w241Target))
	require.NoError(t, err)
	assert.False(t, exists, "the shared destination is vacated by the winner's revert alone")

	require.Equal(t, models.RevertStatusNoOp, w241Row(t, repo, loserRes.OperationID).RevertStatus,
		"the noop terminal is stable across the winner's revert")
	_, err = reverter.RevertBatch(ctx, "job-w241")
	require.ErrorIs(t, err, history.ErrBatchAlreadyReverted,
		"noop + reverted is the batch's terminal ledger state")
}
