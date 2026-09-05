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
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// dupSkipResult builds the Authorized-duplicate-skip ApplyResult shape for a
// loser whose (display-only) NewPath names the batch winner's destination.
func dupSkipResult(movieID, src, target string) *ApplyResult {
	return &ApplyResult{
		Movie: &models.Movie{ID: movieID},
		OrganizeResult: &organizer.OrganizeResult{
			OriginalPath:     src,
			NewPath:          target,
			FolderPath:       filepath.Dir(target),
			Moved:            false,
			DuplicateSkipped: true,
			Warnings:         []string{"duplicate destination within batch: " + target + " already claimed (overwrite authorized)"},
		},
	}
}

// TestRevertBatch_AllDupSkips_CompletesWithoutAnchorSpam pins codex P2 (PR
// #241 F2) for the degenerate batch: EVERY row is an authorized duplicate
// skip, finalized completed-noop at Complete/CompleteFailed. Reverting such a
// batch must be trivially success-shaped — zero processable rows, zero
// outcomes, and NOT ONE anchor_missing skip against an empty NewPath — while
// the legacy failure would have probed anchor "" per row forever.
func TestRevertBatch_AllDupSkips_CompletesWithoutAnchorSpam(t *testing.T) {
	_, repo, fs, rl := newW241Harness(t)
	ctx := context.Background()

	opL1 := w241Begin(t, rl, "ABC-200", "/in/B.mkv")
	opL2 := w241Begin(t, rl, "ABC-300", "/in/C.mkv")
	require.NoError(t, rl.Complete(ctx, opL1, dupSkipResult("ABC-200", "/in/B.mkv", w241Target)))
	require.NoError(t, rl.CompleteFailed(ctx, opL2, dupSkipResult("ABC-300", "/in/C.mkv", w241Target)))

	for _, op := range []OperationID{opL1, opL2} {
		row := w241Row(t, repo, op)
		require.Equal(t, models.RevertStatusNoOp, row.RevertStatus,
			"every dup-skip leg finalizes completed-noop, not applied-with-empty-NewPath")
		require.Empty(t, row.NewPath)
	}

	reverter := history.NewReverter(fs, repo)
	rb, err := reverter.RevertBatch(ctx, "job-w241")
	require.NoError(t, err, "an all-noop batch completes trivially instead of probing empty anchors")
	assert.Zero(t, rb.Total, "noop rows never enter the revert selection")
	assert.Zero(t, rb.Succeeded)
	assert.Zero(t, rb.Skipped, "no anchor_missing skip outcomes may be emitted")
	assert.Zero(t, rb.Failed)
	assert.Empty(t, rb.Outcomes)
}

// TestRevertBatch_MixedBatchFullyReverts pins codex P2 (PR #241 F2) for the
// mixed batch: the authorized dup-skip loser's noop row is excluded from the
// selection, the winner's REAL row unwinds normally, and the batch reports
// fully reverted — no lingering applied-without-anchor row holding the batch
// open.
func TestRevertBatch_MixedBatchFullyReverts(t *testing.T) {
	_, repo, fs, rl := newW241Harness(t)
	ctx := context.Background()

	require.NoError(t, fs.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/in/B.mkv", []byte("b-bytes"), 0o644))
	org := organizer.NewOrganizer(fs, &organizer.Config{
		FolderFormat:  "shared",
		FileFormat:    "shared",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)
	tracker := organizer.NewDuplicateTracker(false)
	tracker.PrimeBatch([]organizer.DuplicatePriming{
		{SourcePath: "/in/A.mkv", TargetPath: w241Target, WillMove: true},
		{SourcePath: "/in/B.mkv", TargetPath: w241Target, WillMove: true},
	})
	cmd := func(movieID, src, name string) organizer.OrganizeCmd {
		return organizer.OrganizeCmd{
			Match:            models.FileMatchInfo{MovieID: movieID, Path: src, Name: name, Extension: ".mkv"},
			Movie:            &models.Movie{ID: movieID},
			DestDir:          "/dest",
			MoveFiles:        true,
			ForceUpdate:      true,
			DuplicateTracker: tracker,
		}
	}

	resW, err := org.Organize(ctx, cmd("ABC-100", "/in/A.mkv", "A.mkv"))
	require.NoError(t, err)
	require.True(t, resW.Moved)
	resL, err := org.Organize(ctx, cmd("ABC-200", "/in/B.mkv", "B.mkv"))
	require.NoError(t, err)
	require.True(t, resL.DuplicateSkipped)

	opW := w241Begin(t, rl, "ABC-100", "/in/A.mkv")
	opL := w241Begin(t, rl, "ABC-200", "/in/B.mkv")
	require.NoError(t, rl.Complete(ctx, opW, &ApplyResult{Movie: &models.Movie{ID: "ABC-100"}, OrganizeResult: resW}))
	require.NoError(t, rl.Complete(ctx, opL, &ApplyResult{Movie: &models.Movie{ID: "ABC-200"}, OrganizeResult: resL}))

	reverter := history.NewReverter(fs, repo)
	rb, err := reverter.RevertBatch(ctx, "job-w241")
	require.NoError(t, err)
	assert.Equal(t, 1, rb.Total, "only the winner's real row is processable — the noop loser is excluded")
	assert.Equal(t, 1, rb.Succeeded, "the mixed batch fully reverts")
	assert.Zero(t, rb.Skipped, "no anchor_missing outcome from the completed-noop loser row")
	assert.Zero(t, rb.Failed)

	restored, err := afero.ReadFile(fs, "/in/A.mkv")
	require.NoError(t, err)
	assert.Equal(t, []byte("a-bytes"), restored, "the winner's move unwound")
	loserSrc, err := afero.ReadFile(fs, "/in/B.mkv")
	require.NoError(t, err)
	assert.Equal(t, []byte("b-bytes"), loserSrc, "the skipped loser's untouched source survives the batch revert")

	// Terminal-state ledger: winner reverted, loser noop — nothing left
	// 'applied', so every later batch revert reports already-reverted.
	require.Equal(t, models.RevertStatusReverted, w241Row(t, repo, opW).RevertStatus)
	require.Equal(t, models.RevertStatusNoOp, w241Row(t, repo, opL).RevertStatus)
	_, err = reverter.RevertBatch(ctx, "job-w241")
	require.ErrorIs(t, err, history.ErrBatchAlreadyReverted)
}
