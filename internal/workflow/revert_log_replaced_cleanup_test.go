package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestCompletionDeletionRequiresCreationOwnership(t *testing.T) {
	root := t.TempDir()
	replaced := filepath.Join(root, "movie.nfo")
	created := filepath.Join(root, "created.nfo")
	pending := filepath.Join(root, "pending.nfo")
	fresh := filepath.Join(root, "fresh.nfo")
	// A syntactically different spelling of the same path must not turn the
	// restored replacement into a newly owned file.
	equivalent := root + string(filepath.Separator) + "." + string(filepath.Separator) + "movie.nfo"
	prior := models.GeneratedFilesJSON{
		Replacements:   []models.ReplacementEntry{{Destination: replaced, Backup: replaced + ".backup"}, {Destination: created, Backup: created + ".backup"}, {Destination: pending, Backup: pending + ".backup"}},
		Delete:         []string{created},
		PlannedDeletes: []models.DeleteEntry{{Path: pending, SHA256: "owned-payload"}},
	}
	for _, promote := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending-intent", true: "completion"}[promote], func(t *testing.T) {
			freshLedger := models.GeneratedFilesJSON{Delete: []string{replaced, equivalent, created, pending, fresh}}
			merged := mergeReplacementLedgerIntents(models.MarshalLedgerJSON(prior), models.MarshalLedgerJSON(freshLedger), promote)
			got, err := models.ParseGeneratedFiles(merged)
			require.NoError(t, err)
			require.Equal(t, []string{created, pending, fresh}, got.Delete)
			require.Equal(t, prior.Replacements, got.Replacements)
			// Both CompleteFailed and retries may restate the result. Neither may
			// manufacture ownership after the first durable merge excluded it.
			again := mergeReplacementLedgerIntents(merged, models.MarshalLedgerJSON(freshLedger), promote)
			got, err = models.ParseGeneratedFiles(again)
			require.NoError(t, err)
			require.Equal(t, []string{created, pending, fresh}, got.Delete)
		})
	}
}

// A failed transaction must leave the original replacement journal intact;
// retries must recompute ownership from that durable row, not from a failed
// completion's in-memory projection.
type replacementCleanupFailRepo struct {
	database.BatchFileOperationRepositoryInterface
	failure error
}

func (r *replacementCleanupFailRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	return r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, func(row *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		next, persist, err := fn(row)
		if err == nil && r.failure != nil {
			return models.GeneratedFilesJSON{}, false, r.failure
		}
		return next, persist, err
	})
}

func TestReplacementCleanupCompletionTransactionRetry(t *testing.T) {
	for _, failure := range []error{errors.New("commit refused"), context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			ctx := context.Background()
			db := newInMemoryDB(t)
			repo := database.NewBatchFileOperationRepository(db)
			root := t.TempDir()
			dest := filepath.Join(root, "movie.nfo")
			fresh := filepath.Join(root, "fresh.nfo")
			prior := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{Destination: dest, Backup: dest + ".backup", DestSeq: 1}}})
			row := &models.BatchFileOperation{BatchJobID: "replacement-retry", MovieID: "ABC-123", OriginalPath: filepath.Join(root, "video.mp4"), OperationType: models.OperationTypeUpdate, RevertStatus: models.RevertStatusApplied, GeneratedFiles: prior}
			require.NoError(t, repo.Create(ctx, row))
			broken := &replacementCleanupFailRepo{BatchFileOperationRepositoryInterface: repo, failure: failure}
			log := NewDBRevertLog(broken, nil, "replacement-retry", afero.NewOsFs(), nil, nil, nil).(*dbRevertLog)
			outcome := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Delete: []string{dest, fresh}})
			_, err := log.mergeJournalInTx(ctx, row.ID, "1", "test", outcome, "")
			require.ErrorIs(t, err, failure)
			saved, err := repo.FindByID(ctx, row.ID)
			require.NoError(t, err)
			require.Equal(t, prior, saved.GeneratedFiles)
			broken.failure = nil
			_, err = log.mergeJournalInTx(ctx, row.ID, "1", "test", outcome, "")
			require.NoError(t, err)
			saved, err = repo.FindByID(ctx, row.ID)
			require.NoError(t, err)
			gf, err := models.ParseGeneratedFiles(saved.GeneratedFiles)
			require.NoError(t, err)
			require.Equal(t, []string{fresh}, gf.Delete)
			require.Len(t, gf.Replacements, 1)
		})
	}
}
