package history

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestRevertBatchNFORestoreFailureRetainsVideoAndCanRetry(t *testing.T) {
	ctx := context.Background()
	base := afero.NewMemMapFs()
	dir := filepath.Join(t.TempDir(), "library")
	require.NoError(t, base.MkdirAll(dir, 0755))
	videoPath := filepath.Join(dir, "RESTORE-001.mp4")
	nfoPath := filepath.Join(dir, "RESTORE-001.nfo")
	videoBytes := []byte("resident video: must never be moved or deleted by metadata Undo")
	currentNFO := []byte("<movie><title>Updated metadata</title></movie>")
	snapshot := "<movie>\r\n<title>Original &amp; retained</title>\r\n</movie>\r\n"
	require.NoError(t, afero.WriteFile(base, videoPath, videoBytes, 0644))
	require.NoError(t, afero.WriteFile(base, nfoPath, currentNFO, 0644))
	repo := &nfoRestoreStatusRecordingRepo{p3OpRepo: newP3OpRepo()}
	retainUndo := true
	op := &models.BatchFileOperation{
		BatchJobID:     "nfo-restore-retry",
		MovieID:        "RESTORE-001",
		OriginalPath:   videoPath,
		NewPath:        videoPath,
		OperationType:  models.OperationTypeUpdate,
		NFOSnapshot:    snapshot,
		NFOPath:        nfoPath,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{UndoRetention: &retainUndo, Roots: []string{dir}}),
		RevertStatus:   models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))
	fs := &nfoRestoreWriteFailureFS{Fs: base, denied: nfoPath, reject: true}
	reverter := NewReverter(fs, repo)

	result, err := reverter.RevertBatch(ctx, op.BatchJobID)
	require.NoError(t, err, "the batch reports a per-file restore failure rather than a batch lookup error")
	require.NotNil(t, result)
	require.Equal(t, 1, result.Total)
	require.Zero(t, result.Succeeded)
	require.Zero(t, result.Skipped)
	require.Equal(t, 1, result.Failed)
	require.Len(t, result.Outcomes, 1)
	require.Equal(t, op.ID, result.Outcomes[0].OperationID)
	require.Equal(t, models.RevertOutcomeFailed, result.Outcomes[0].Outcome)
	require.Equal(t, models.RevertReasonNFORestoreFailed, result.Outcomes[0].Reason)
	require.Contains(t, result.Outcomes[0].Error, "failed to restore NFO")
	require.Equal(t, 1, fs.writeAttempts)
	require.Equal(t, []models.RevertStatusEnum{models.RevertStatusFailed}, repo.statuses, "a failed restore must never mark the operation reverted")
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusFailed, row.RevertStatus)
	require.Equal(t, snapshot, row.NFOSnapshot)
	require.Equal(t, nfoPath, row.NFOPath)
	ledger, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.True(t, ledger.UndoStarted, "the failed attempt must keep publication fenced while Undo remains retryable")
	for path, want := range map[string][]byte{videoPath: videoBytes, nfoPath: currentNFO} {
		got, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}

	// Once the write refusal is removed, the same public batch operation must
	// consume the retained snapshot and only then become terminally reverted.
	fs.reject = false
	result, err = reverter.RevertBatch(ctx, op.BatchJobID)
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Equal(t, 1, result.Succeeded)
	require.Zero(t, result.Failed)
	require.Zero(t, result.Skipped)
	require.Len(t, result.Outcomes, 1)
	require.Equal(t, models.RevertOutcomeReverted, result.Outcomes[0].Outcome)
	require.Empty(t, result.Outcomes[0].Reason)
	require.Empty(t, result.Outcomes[0].Error)
	require.Equal(t, 2, fs.writeAttempts)
	require.Equal(t, []models.RevertStatusEnum{models.RevertStatusFailed, models.RevertStatusReverted}, repo.statuses)
	row, err = repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus)
	for path, want := range map[string][]byte{videoPath: videoBytes, nfoPath: []byte(snapshot)} {
		got, err := afero.ReadFile(base, path)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

type nfoRestoreWriteFailureFS struct {
	afero.Fs
	denied        string
	reject        bool
	writeAttempts int
}

func (fs *nfoRestoreWriteFailureFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if filepath.Clean(name) == fs.denied && flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		fs.writeAttempts++
		if fs.reject {
			return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrPermission}
		}
	}
	return fs.Fs.OpenFile(name, flag, perm)
}

type nfoRestoreStatusRecordingRepo struct {
	*p3OpRepo
	statuses []models.RevertStatusEnum
}

func (repo *nfoRestoreStatusRecordingRepo) UpdateRevertStatus(ctx context.Context, id uint, status models.RevertStatusEnum) error {
	repo.statuses = append(repo.statuses, status)
	return repo.p3OpRepo.UpdateRevertStatus(ctx, id, status)
}
