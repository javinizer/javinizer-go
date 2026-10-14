package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P1 (PRRT_kwDORn9KaM6m3ujI) crash-replay E2E through the production
// journal writers and the production reverter. The sibling publish pin and
// the pending move intent are journaled exactly the way the deferred sibling
// block writes them, then the process "exits" BEFORE the source removal is
// confirmed and foreign bytes land on the source path. While the intent is
// unconfirmed the row retains BOTH the pin and the arm; recovery suppresses
// the rename-back AND stays the pinned delete (codex P1,
// PRRT_kwDORn9KaM6m5kmF) — this shape cannot be told apart from a consumed
// source that a foreign process recreated, so the surviving source is never
// overwritten and the pinned published copy, potentially the last remaining
// copy of the moved sibling, is retained alongside it.
func TestPendingSiblingMoveIntentCrashReplayKeepsSourceAndPinnedCopy(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "pending-sibling-crash", "")
	fs, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "pending-sibling-crash")
	dest := filepath.Join(root, "library")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "pending-sibling-crash", fs, nil, nil, nil)
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest, Organize: OrganizeOptions{MoveFiles: true}})
	require.NoError(t, err)

	finalDir := filepath.Join(dest, "movie")
	videoTarget := filepath.Join(finalDir, filepath.Base(source))
	siblingTarget := filepath.Join(finalDir, stagedArtifactSiblingName(filepath.Base(source), filepath.Base(source), filepath.Base(multipart)))
	require.NoError(t, fs.MkdirAll(finalDir, 0o755))

	// The video and sibling copies are already published; the video source
	// was consumed — only the sibling removal never ran.
	require.NoError(t, afero.WriteFile(fs, videoTarget, []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(fs, siblingTarget, []byte("part two"), 0o644))
	require.NoError(t, fs.Remove(source))

	// The publish-block pin, then the removal-loop pending intent — through
	// the real recorder, in the real order.
	digest, digestErr := artifactDigest(fs, siblingTarget)
	require.NoError(t, digestErr)
	require.NoError(t, log.RecordDeleteIntent(context.Background(), opID, []models.DeleteEntry{{Path: siblingTarget, SHA256: digest}}))
	require.NoError(t, log.RecordMoveIntent(context.Background(), opID, source, videoTarget))
	require.NoError(t, log.RecordMoveIntent(context.Background(), opID, multipart, siblingTarget))

	// The exit lands here, with the completion columns stamped as the partial
	// inverse persisted them — and the row still carries pin AND pending arm.
	require.NoError(t, db.Model(&models.BatchFileOperation{}).Where("id = ?", mustParseOpID(t, opID)).Update("new_path", videoTarget).Error)
	row, rowErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, rowErr)
	journal, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	armed := false
	for _, fm := range journal.MoveBack {
		if fm.OriginalPath == multipart && fm.NewPath == siblingTarget {
			armed = true
		}
	}
	require.True(t, armed, "the pending sibling intent is journaled")
	pinned := false
	for _, pd := range journal.PlannedDeletes {
		if pd.Path == siblingTarget {
			pinned = true
		}
	}
	require.True(t, pinned, "the pending intent RETAINS the sibling target's pin until consumption is confirmed")

	// Before recovery runs, foreign bytes land on the never-consumed source.
	require.NoError(t, afero.WriteFile(fs, multipart, []byte("user re-edit"), 0o644))

	res, revErr := history.NewReverter(fs, repo).RevertBatch(t.Context(), "pending-sibling-crash")
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)

	restored, readErr := afero.ReadFile(fs, multipart)
	require.NoError(t, readErr, "the surviving source was never renamed over")
	assert.Equal(t, "user re-edit", string(restored), "pre-recovery source edits survive the revert")
	siblingCopy, siblingErr := afero.ReadFile(fs, siblingTarget)
	require.NoError(t, siblingErr, "the hash-pinned published copy is retained — neither deleted nor moved back over the occupied source")
	assert.Equal(t, "part two", string(siblingCopy))
	videoBack, videoErr := afero.ReadFile(fs, source)
	require.NoError(t, videoErr, "the consumed primary still renames back onto its source")
	assert.Equal(t, "video", string(videoBack))

	persisted, findErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, findErr)
	assert.Equal(t, models.RevertStatusReverted, persisted.RevertStatus)
	for _, path := range []string{subtitle, unrelated} {
		exists, existsErr := afero.Exists(fs, path)
		require.NoError(t, existsErr)
		require.True(t, exists, path)
	}
}
