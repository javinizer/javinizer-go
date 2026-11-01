package history_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// t.Chdir forbids a parallel test/ancestor and restores the original cwd on
// every exit path. Only an empty directory created by this fixture is removed;
// all database and anchor I/O stays on absolute paths while cwd is unavailable.
func TestRevertRelativeLegacyPathsWithDeletedWorkingDirectoryCanRetry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not permit removing the active working directory")
	}
	ctx := t.Context()
	root := t.TempDir()
	cwd := filepath.Join(root, "owned-working-directory")
	require.NoError(t, os.Mkdir(cwd, 0700))
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "history.sqlite"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	repo := database.NewBatchFileOperationRepository(db)
	installed := filepath.Join(root, "library", "movie", "installed.mp4")
	updateAnchor := filepath.Join(root, "resident", "movie.mp4")
	foreign := filepath.Join(root, "resident", "unrelated.nfo")
	files := map[string]string{installed: "moved primary bytes", updateAnchor: "resident primary bytes", foreign: "foreign NFO bytes"}
	identities := make(map[string]os.FileInfo)
	for path, data := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte(data), 0600))
		handle, openErr := os.Open(path)
		require.NoError(t, openErr)
		identities[path], err = handle.Stat()
		require.NoError(t, err)
		require.NoError(t, handle.Close())
	}
	snapshot := "<movie><title>original retained NFO</title></movie>"
	move := models.BatchFileOperation{BatchJobID: "deleted-cwd", MovieID: "CWD-MOVE", OriginalPath: filepath.Join("incoming", "original.mp4"), NewPath: installed, OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied}
	update := models.BatchFileOperation{BatchJobID: "deleted-cwd", MovieID: "CWD-UPDATE", OriginalPath: updateAnchor, NewPath: updateAnchor, NFOPath: filepath.Join("metadata", "original.nfo"), NFOSnapshot: snapshot, OperationType: models.OperationTypeUpdate, RevertStatus: models.RevertStatusApplied}
	require.NoError(t, repo.Create(ctx, &move))
	require.NoError(t, repo.Create(ctx, &update))
	t.Chdir(cwd)
	require.NoError(t, os.Remove(cwd), "remove only the newly-created empty cwd, never an ancestor or data directory")
	_, err = os.Getwd()
	require.Error(t, err, "the real environment must lack a resolvable cwd")
	result, err := history.NewReverter(afero.NewOsFs(), repo).RevertBatch(ctx, "deleted-cwd")
	require.NoError(t, err)
	require.Equal(t, 2, result.Total)
	require.Equal(t, 2, result.Failed)
	require.Zero(t, result.Succeeded)
	require.Zero(t, result.Skipped)
	expected := map[uint]struct {
		reason  models.RevertReasonEnum
		message string
	}{move.ID: {models.RevertReasonUnexpectedPathState, "failed to canonicalize directory path"}, update.ID: {models.RevertReasonNFORestoreFailed, "failed to canonicalize NFO path"}}
	require.Len(t, result.Outcomes, 2)
	for _, outcome := range result.Outcomes {
		want, ok := expected[outcome.OperationID]
		require.True(t, ok)
		require.Equal(t, models.RevertOutcomeFailed, outcome.Outcome)
		require.Equal(t, want.reason, outcome.Reason)
		require.Contains(t, outcome.Error, want.message)
	}
	for path, want := range files {
		data, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.Equal(t, want, string(data))
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		require.True(t, os.SameFile(identities[path], info))
	}
	for _, original := range []models.BatchFileOperation{move, update} {
		failed, readErr := repo.FindByID(ctx, original.ID)
		require.NoError(t, readErr)
		require.Equal(t, models.RevertStatusFailed, failed.RevertStatus)
		require.Equal(t, original.OriginalPath, failed.OriginalPath)
		require.Equal(t, original.NewPath, failed.NewPath)
		require.Equal(t, original.NFOPath, failed.NFOPath)
		require.Equal(t, original.NFOSnapshot, failed.NFOSnapshot)
		ledger, parseErr := models.ParseGeneratedFiles(failed.GeneratedFiles)
		require.NoError(t, parseErr)
		require.True(t, ledger.UndoStarted, "failed Undo remains admitted and cannot reopen publication")
	}
	require.NoDirExists(t, cwd)
	// Re-establish the same intended base, then retry through the public API.
	require.NoError(t, os.Mkdir(cwd, 0700))
	require.NoError(t, os.Chdir(cwd))
	retry, err := history.NewReverter(afero.NewOsFs(), repo).RevertBatch(ctx, "deleted-cwd")
	require.NoError(t, err)
	require.Equal(t, 2, retry.Total)
	require.Equal(t, 2, retry.Succeeded)
	require.Zero(t, retry.Failed)
	require.Zero(t, retry.Skipped)
	restored := filepath.Join(cwd, move.OriginalPath)
	data, err := os.ReadFile(restored)
	require.NoError(t, err)
	require.Equal(t, files[installed], string(data))
	require.NoFileExists(t, installed)
	restoredInfo, err := os.Stat(restored)
	require.NoError(t, err)
	require.True(t, os.SameFile(identities[installed], restoredInfo))
	data, err = os.ReadFile(filepath.Join(cwd, update.NFOPath))
	require.NoError(t, err)
	require.Equal(t, snapshot, string(data))
	for _, path := range []string{updateAnchor, foreign} {
		data, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.Equal(t, files[path], string(data))
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		require.True(t, os.SameFile(identities[path], info))
	}
	for _, id := range []uint{move.ID, update.ID} {
		row, readErr := repo.FindByID(ctx, id)
		require.NoError(t, readErr)
		require.Equal(t, models.RevertStatusReverted, row.RevertStatus)
		require.NotNil(t, row.RevertedAt)
	}
}
