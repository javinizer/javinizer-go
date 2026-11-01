package history_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// A repository implementation can retain the caller's candidate slice. This
// synchronous callback changes a later snapshot, not a persisted ledger, while
// the first candidate's real query is in progress. There is no concurrent writer.
type pruneSnapshotRepository struct {
	database.BatchFileOperationRepositoryInterface
	candidates []models.BatchFileOperation
	calls      int
}

func (r *pruneSnapshotRepository) FindOperationsWithLedger(ctx context.Context) ([]models.BatchFileOperation, error) {
	rows, err := r.BatchFileOperationRepositoryInterface.FindOperationsWithLedger(ctx)
	if err == nil {
		r.calls++
		if r.calls == 1 {
			r.candidates[1].GeneratedFiles = `{"replacements":broken`
		}
	}
	return rows, err
}

func TestPruneRejectsLaterCandidateChangedBySynchronousRepository(t *testing.T) {
	ctx := t.Context()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "history.sqlite"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	repo := database.NewBatchFileOperationRepository(db)
	root := t.TempDir()
	candidates := make([]models.BatchFileOperation, 2)
	destinations := make([]string, 2)
	backups := make([]string, 2)
	for i := range candidates {
		destinations[i] = filepath.Join(root, fmt.Sprintf("poster-%d.jpg", i))
		backups[i] = destinations[i] + ".dlbak.0123456789abcdef"
		require.NoError(t, os.WriteFile(destinations[i], []byte("organized"), 0600))
		previous := []byte(fmt.Sprintf("owned previous poster %d", i))
		require.NoError(t, os.WriteFile(backups[i], previous, 0600))
		info, statErr := os.Stat(backups[i])
		require.NoError(t, statErr)
		digest := sha256.Sum256(previous)
		candidates[i] = models.BatchFileOperation{BatchJobID: "snapshot-prune", MovieID: fmt.Sprintf("SNAPSHOT-%d", i), OriginalPath: destinations[i], NewPath: destinations[i], OperationType: models.OperationTypeUpdate, RevertStatus: models.RevertStatusApplied,
			GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{root}, Replacements: []models.ReplacementEntry{{Destination: destinations[i], Backup: backups[i], DestSeq: int64(i + 1), Installed: true, BackupSize: info.Size(), BackupModUnix: info.ModTime().Unix(), BackupSHA256: hex.EncodeToString(digest[:])}}})}
		require.NoError(t, repo.Create(ctx, &candidates[i]))
	}
	beforeSecond, err := repo.FindByID(ctx, candidates[1].ID)
	require.NoError(t, err)
	secondInfo, err := os.Stat(backups[1])
	require.NoError(t, err)
	adapter := &pruneSnapshotRepository{BatchFileOperationRepositoryInterface: repo, candidates: candidates}
	err = history.NewReplacementSweeper(afero.NewOsFs(), adapter).PruneOperationBackups(ctx, candidates)
	var syntaxErr *json.SyntaxError
	require.ErrorAs(t, err, &syntaxErr)
	require.ErrorContains(t, err, fmt.Sprintf("operation %d ledger parse", candidates[1].ID))
	require.Equal(t, 1, adapter.calls, "the malformed later snapshot must not reach live-ledger cleanup admission")
	require.NoFileExists(t, backups[0], "earlier admitted cleanup must finish despite later snapshot rejection")
	first, err := repo.FindByID(ctx, candidates[0].ID)
	require.NoError(t, err)
	ledger, err := models.ParseGeneratedFiles(first.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, ledger.Replacements, "successful earlier cleanup must retract its durable ownership")
	afterSecond, err := repo.FindByID(ctx, candidates[1].ID)
	require.NoError(t, err)
	require.Equal(t, beforeSecond, afterSecond, "rejected snapshot must not mutate the durable row")
	secondAfter, err := os.Stat(backups[1])
	require.NoError(t, err)
	require.True(t, os.SameFile(secondInfo, secondAfter))
	bytes, err := os.ReadFile(backups[1])
	require.NoError(t, err)
	require.Equal(t, "owned previous poster 1", string(bytes))
	for _, dest := range destinations {
		bytes, readErr := os.ReadFile(dest)
		require.NoError(t, readErr)
		require.Equal(t, "organized", string(bytes))
		release, claimErr := fsutil.AcquireReplacementBusy(afero.NewOsFs(), dest)
		require.NoError(t, claimErr)
		require.NotNil(t, release)
		release()
	}
}
