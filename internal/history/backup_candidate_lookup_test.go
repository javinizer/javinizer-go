package history

import (
	"context"
	"errors"
	"github.com/javinizer/javinizer-go/internal/database"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type backupCandidateRepo struct {
	*p3OpRepo
	lookup    func(context.Context, string) ([]models.BatchFileOperation, error)
	fullScans int
	queries   int
}

func (r *backupCandidateRepo) FindReplacementBackupCandidates(ctx context.Context, backup string) ([]models.BatchFileOperation, error) {
	r.queries++
	return r.lookup(ctx, backup)
}
func (r *backupCandidateRepo) FindOperationsWithLedger(ctx context.Context) ([]models.BatchFileOperation, error) {
	r.fullScans++
	return r.p3OpRepo.FindOperationsWithLedger(ctx)
}

func TestSuccessfulCleanupUsesFreshBackupCandidates(t *testing.T) {
	ctx := context.Background()
	fs, base, row, ledger := successCleanupFixture(t, 2)
	repo := &backupCandidateRepo{p3OpRepo: base}
	repo.lookup = func(ctx context.Context, backup string) ([]models.BatchFileOperation, error) {
		if repo.queries == 2 {
			// A reference published between replacements must not be hidden by a
			// per-operation cached reference set. A different destination still owns it.
			shared := ledger.Replacements[1]
			shared.Destination = filepath.Join(t.TempDir(), "OTHER.nfo")
			shared.Backup = filepath.Dir(backup) + string(filepath.Separator) + "." + string(filepath.Separator) + filepath.Base(backup)
			published := make(chan error, 1)
			go func() {
				published <- base.Create(ctx, &models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{shared}})})
			}()
			require.NoError(t, <-published)
		}
		return base.FindOperationsWithLedger(ctx)
	}
	err := NewReplacementSweeper(fs, repo).CleanupSuccessfulOperation(ctx, row.ID)
	require.ErrorContains(t, err, "retains backup cleanup intent")
	require.Equal(t, 2, repo.queries)
	require.Zero(t, repo.fullScans, "candidate-capable repository must not hydrate full historical operations")
	exists, err := fs.Stat(ledger.Replacements[0].Backup)
	require.Nil(t, exists)
	require.Error(t, err)
	require.Equal(t, "old synthetic metadata 1", string(mustRead2(t, fs, ledger.Replacements[1].Backup)))
	current, err := base.FindByID(ctx, row.ID)
	require.NoError(t, err)
	remaining, err := models.ParseGeneratedFiles(current.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, remaining.Replacements, 1)
}

func TestSuccessfulCleanupCandidateLookupFailureRetainsBackup(t *testing.T) {
	for _, cause := range []error{errors.New("synthetic candidate read failed"), context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
			fs, base, row, ledger := successCleanupFixture(t, 1)
			repo := &backupCandidateRepo{p3OpRepo: base, lookup: func(context.Context, string) ([]models.BatchFileOperation, error) { return nil, cause }}
			err := NewReplacementSweeper(fs, repo).CleanupSuccessfulOperation(context.Background(), row.ID)
			require.ErrorIs(t, err, cause)
			require.Zero(t, repo.fullScans)
			require.Equal(t, "old synthetic metadata 0", string(mustRead2(t, fs, ledger.Replacements[0].Backup)))
			current, err := base.FindByID(context.Background(), row.ID)
			require.NoError(t, err)
			remaining, err := models.ParseGeneratedFiles(current.GeneratedFiles)
			require.NoError(t, err)
			require.Equal(t, ledger.Replacements, remaining.Replacements)
		})
	}
}

func TestSuccessfulCleanupProjectedMalformedReferenceSafety(t *testing.T) {
	for _, kind := range []string{"unrelated", "relevant", "escaped-relevant"} {
		t.Run(kind, func(t *testing.T) {
			fs, _, row, ledger := successCleanupFixture(t, 1)
			db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			repo := database.NewBatchFileOperationRepository(db)
			row.ID = 0
			require.NoError(t, repo.Create(t.Context(), row))
			backup := ledger.Replacements[0].Backup
			if kind == "unrelated" {
				backup = "/unrelated"
			}
			raw := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{Backup: backup}}})
			raw = strings.TrimSuffix(raw, "}") + `,"undo_started":"bad"}`
			if kind == "escaped-relevant" {
				raw = strings.ReplaceAll(raw, p3HexA, `\u0030`+p3HexA[1:])
			}
			require.NoError(t, repo.Create(t.Context(), &models.BatchFileOperation{BatchJobID: "synthetic-corrupt", GeneratedFiles: raw}))
			err = NewReplacementSweeper(fs, repo).CleanupSuccessfulOperation(t.Context(), row.ID)
			if kind == "unrelated" {
				require.NoError(t, err)
				_, err = fs.Stat(ledger.Replacements[0].Backup)
				require.Error(t, err)
			} else {
				require.ErrorContains(t, err, "cannot prove backup")
				require.Equal(t, "old synthetic metadata 0", string(mustRead2(t, fs, ledger.Replacements[0].Backup)))
				current, readErr := repo.FindByID(t.Context(), row.ID)
				require.NoError(t, readErr)
				remaining, parseErr := models.ParseGeneratedFiles(current.GeneratedFiles)
				require.NoError(t, parseErr)
				require.Equal(t, ledger.Replacements, remaining.Replacements)
			}
		})
	}
}
