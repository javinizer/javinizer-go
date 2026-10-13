package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P1 (PRRT_kwDORn9KaM6m0-JL): a deferred move that published a generic
// sibling could persist the sibling target in BOTH PlannedDeletes (the
// pre-copy hash pin) and MoveBack (the armed inverse). A revert processed
// PlannedDeletes first, deleted the still-pinned destination, and could then
// no longer restore its removed source. The journal now promotes the pin when
// the arm lands, and the executor supersedes any surviving conflicting pair:
// a destination named by a MoveBack entry is owned by the rename-back leg, so
// the pinned delete must never fire first.
func TestRevertMoveRowMoveBackSupersedesPinnedDelete(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	srcDir := "/src-w161"
	dstDir := "/dst-w161/lib/W161-001"
	siblingSource := srcDir + "/W161-001-cd2.mp4"
	siblingTarget := dstDir + "/W161-001-cd2.mp4"
	stray := dstDir + "/W161-001-stray.nfo"
	require.NoError(t, fs.MkdirAll(dstDir, 0o777))
	require.NoError(t, fs.MkdirAll(srcDir, 0o777))
	require.NoError(t, afero.WriteFile(fs, dstDir+"/W161-001.mkv", []byte("video"), 0o666))
	require.NoError(t, afero.WriteFile(fs, siblingTarget, []byte("part two"), 0o666))
	require.NoError(t, afero.WriteFile(fs, stray, []byte("stray"), 0o666))

	partSum := sha256.Sum256([]byte("part two"))
	straySum := sha256.Sum256([]byte("stray"))
	op := &models.BatchFileOperation{
		BatchJobID:    "job-w161-promote",
		MovieID:       "W161-001",
		OriginalPath:  srcDir + "/W161-001.mkv",
		NewPath:       dstDir + "/W161-001.mkv",
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			PlannedDeletes: []models.DeleteEntry{
				{Path: siblingTarget, SHA256: hex.EncodeToString(partSum[:])},
				{Path: stray, SHA256: hex.EncodeToString(straySum[:])},
			},
			MoveBack: []models.FileMove{{OriginalPath: siblingSource, NewPath: siblingTarget}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	res, err := NewReverter(fs, repo).RevertBatch(ctx, "job-w161-promote")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded)

	restored, readErr := afero.ReadFile(fs, siblingSource)
	require.NoError(t, readErr, "the armed move-back restores the sibling onto its source")
	assert.Equal(t, []byte("part two"), restored)
	if _, statErr := fs.Stat(siblingTarget); !os.IsNotExist(statErr) {
		t.Fatalf("the destination moved back instead of being deleted-then-lost: %v", statErr)
	}
	if _, statErr := fs.Stat(stray); !os.IsNotExist(statErr) {
		t.Fatalf("an un-armed pinned delete still hash-fires: %v", statErr)
	}
	video, readErr := afero.ReadFile(fs, srcDir+"/W161-001.mkv")
	require.NoError(t, readErr, "the primary move reverts through its column arm")
	assert.Equal(t, []byte("video"), video)

	row, findErr := repo.FindByID(ctx, op.ID)
	require.NoError(t, findErr)
	assert.Equal(t, models.RevertStatusReverted, row.RevertStatus)
}
