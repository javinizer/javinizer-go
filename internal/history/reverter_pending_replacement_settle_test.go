package history

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P1 (reverter settle): a forced overwrite that crashed between
// BeforePublish (old destination set aside) and the source publish hydrates as
// a pending move whose replay exclusion skips exactly the stranded backup.
// Settling that shape no-op without restoring first strands the user's prior
// bytes behind a terminal status. The settle must restore the excluded primary
// replacement BEFORE the no-op verdict; the complementary no-replacement
// settle (TestRevertFile_UnexecutedMoveIntentSettlesNoOp) stays unchanged.

// pendingReplacementOperation builds that crash shape: the move row carries
// only its pending MoveBack intent (NewPath blank — completion never
// persisted), the journaled ReplacementEntry holds the user's pre-overwrite
// bytes at the production-shaped backup name, the destination is absent
// (nothing was ever published), and the source stands untouched.
func pendingReplacementOperation(t *testing.T, fs afero.Fs, repo *p3OpRepo, withBackup bool) (*models.BatchFileOperation, string, string) {
	t.Helper()
	src := "/src/ABC-123.mp4"
	dst := "/dst/lib/ABC-123.mp4"
	backup := dst + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/src", config.DirPerm))
	require.NoError(t, fs.MkdirAll("/dst/lib", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, src, []byte("video"), config.FilePerm))
	if withBackup {
		require.NoError(t, afero.WriteFile(fs, backup, []byte("prior-dest-bytes"), config.FilePerm))
	}
	gf := models.GeneratedFilesJSON{
		MoveBack:     []models.FileMove{{OriginalPath: src, NewPath: dst}},
		Replacements: []models.ReplacementEntry{{Destination: dst, Backup: backup, DestSeq: 1}},
	}
	op := &models.BatchFileOperation{
		BatchJobID:     "pending-replacement",
		MovieID:        "ABC-123",
		OriginalPath:   src,
		NewPath:        "",
		OperationType:  models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(gf),
		RevertStatus:   models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(t.Context(), op))
	return op, dst, backup
}

func TestRevertFile_UnexecutedMoveIntentRestoresPrimaryReplacementBeforeNoOp(t *testing.T) {
	fs, repo := afero.NewMemMapFs(), newP3OpRepo()
	op, dst, backup := pendingReplacementOperation(t, fs, repo, true)

	result, err := NewReverter(fs, repo).revertFile(t.Context(), op)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeSkipped, result.Outcome, "the unexecuted move still settles as the no-op verdict")
	assert.Equal(t, models.RevertReasonAnchorMissing, result.Reason)

	assert.Equal(t, "prior-dest-bytes", p3ReadFile(t, fs, dst), "the forced-overwrite backup lands back at the destination before the settle")
	if exists, _ := afero.Exists(fs, backup); exists {
		t.Fatalf("the consumed backup must not remain at %s", backup)
	}
	assert.Equal(t, "video", p3ReadFile(t, fs, op.OriginalPath), "the never-consumed source is retained untouched")

	stored, findErr := repo.FindByID(t.Context(), op.ID)
	require.NoError(t, findErr)
	assert.Equal(t, models.RevertStatusNoOp, stored.RevertStatus, "the row settles terminally")
	gf, parseErr := models.ParseGeneratedFiles(stored.GeneratedFiles)
	require.NoError(t, parseErr)
	assert.Empty(t, gf.Replacements, "the restore consumed the journal entry")

	_, err = NewReverter(fs, repo).revertFile(t.Context(), op)
	assert.ErrorIs(t, err, ErrBatchAlreadyReverted, "a repeat revert is idempotent against the settled row")
	assert.Equal(t, "prior-dest-bytes", p3ReadFile(t, fs, dst), "the repeat attempt leaves the restored bytes untouched")
}

// The restore failure leg: a journaled backup that cannot be read rejects the
// settle, keeps the row Applied for retry, and never writes the no-op verdict.
func TestRevertFile_UnexecutedMoveIntentRetainsRowWhenPrimaryRestoreFails(t *testing.T) {
	fs, repo := afero.NewMemMapFs(), newP3OpRepo()
	op, dst, backup := pendingReplacementOperation(t, fs, repo, false)

	result, err := NewReverter(fs, repo).revertFile(t.Context(), op)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	assert.Equal(t, models.RevertReasonUnexpectedPathState, result.Reason)
	assert.Contains(t, result.Error, backup)

	if exists, _ := afero.Exists(fs, dst); exists {
		t.Fatal("a failed restore must leave the destination absent")
	}
	stored, findErr := repo.FindByID(t.Context(), op.ID)
	require.NoError(t, findErr)
	assert.Equal(t, models.RevertStatusApplied, stored.RevertStatus, "the rejection keeps the row retryable — it must NOT settle no-op")
	gf, parseErr := models.ParseGeneratedFiles(stored.GeneratedFiles)
	require.NoError(t, parseErr)
	assert.Len(t, gf.Replacements, 1, "the journal entry stays armed for the retry")
}

// The destination-locked leg: a live busy marker refuses the restore, retains
// the backup and the Applied status, and a retry after release completes the
// restore + no-op settle.
func TestRevertFile_UnexecutedMoveIntentPrimaryRestoreRetainsWhileDestinationBusy(t *testing.T) {
	fs, repo := afero.NewMemMapFs(), newP3OpRepo()
	op, dst, backup := pendingReplacementOperation(t, fs, repo, true)

	release, busyErr := fsutil.AcquireReplacementBusy(fs, dst)
	require.NoError(t, busyErr)

	result, err := NewReverter(fs, repo).revertFile(t.Context(), op)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	assert.Contains(t, result.Error, "busy")
	assert.Equal(t, "prior-dest-bytes", p3ReadFile(t, fs, backup), "the locked destination keeps its armed backup")
	if exists, _ := afero.Exists(fs, dst); exists {
		t.Fatal("the busy destination stays absent")
	}
	stored, findErr := repo.FindByID(t.Context(), op.ID)
	require.NoError(t, findErr)
	assert.Equal(t, models.RevertStatusApplied, stored.RevertStatus, "busy retains the row for retry")

	release()

	retry, err := NewReverter(fs, repo).revertFile(t.Context(), op)
	require.NoError(t, err)
	require.NotNil(t, retry)
	assert.Equal(t, models.RevertOutcomeSkipped, retry.Outcome)
	assert.Equal(t, "prior-dest-bytes", p3ReadFile(t, fs, dst), "the retry restores the stranded bytes once the destination frees")
	stored, findErr = repo.FindByID(t.Context(), op.ID)
	require.NoError(t, findErr)
	assert.Equal(t, models.RevertStatusNoOp, stored.RevertStatus)
}
