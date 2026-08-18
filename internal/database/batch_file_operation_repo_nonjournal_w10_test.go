package database

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING wave-10 (codex follow-up, P1): completion's
// non-journal column write after the journal transaction must NEVER touch
// generated_files — a concurrent UpdateJournalInTx append/consume committed
// between the journal tx and the column write would otherwise be erased or
// resurrected by the follow-up Save. UpdateNonJournalFields is the scoped
// replacement; these tests pin its column envelope on sqlite.

// TestW10UpdateNonJournalFields_PersistsAllNonJournalColumns verifies every
// non-journal column persists — including ZERO values (map-based update must
// behave like Save, not like a plain struct Updates that skips zero fields).
func TestW10UpdateNonJournalFields_PersistsAllNonJournalColumns(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	// Wave-15 note: the stored row must NOT be reverted here — the guarded
	// status columns (wave-15: never clobber a stored reverted status) would
	// suppress exactly the transition this envelope test exercises. The
	// applied→failed transition keeps full Save-parity coverage; the
	// suppressed reverted-row transition lives in the w15 tests.
	original := &models.BatchFileOperation{
		BatchJobID:      "job-w10",
		MovieID:         "W10-001",
		OriginalPath:    "/src/W10-001.mkv",
		NewPath:         "/dst/old/W10-001.mkv",
		OperationType:   models.OperationTypeMove,
		NFOSnapshot:     "<nfo>old</nfo>",
		NFOPath:         "/dst/old/W10-001.nfo",
		GeneratedFiles:  models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{"/dst/old"}}),
		RevertStatus:    models.RevertStatusApplied,
		RevertedAt:      nil,
		InPlaceRenamed:  true,
		OriginalDirPath: "/dst/old",
	}
	require.NoError(t, repo.Create(ctx, original))

	mutated := *original
	mutated.BatchJobID = "job-w10-next"
	mutated.MovieID = "W10-002"
	mutated.OriginalPath = "/src/W10-002.mkv"
	mutated.NewPath = "" // zero value must persist
	mutated.OperationType = models.OperationTypeHardlink
	mutated.NFOSnapshot = "" // zero value must persist
	mutated.NFOPath = "/dst/new/W10-002.nfo"
	mutated.RevertStatus = models.RevertStatusFailed
	mutated.RevertedAt = nil // zero value must persist
	mutated.InPlaceRenamed = false
	mutated.OriginalDirPath = "/dst/new"

	require.NoError(t, repo.UpdateNonJournalFields(ctx, &mutated))

	row, err := repo.FindByID(ctx, mutated.ID)
	require.NoError(t, err)
	require.Equal(t, "job-w10-next", row.BatchJobID)
	require.Equal(t, "W10-002", row.MovieID)
	require.Equal(t, "/src/W10-002.mkv", row.OriginalPath)
	require.Equal(t, "", row.NewPath, "zero-value new_path persisted")
	require.Equal(t, models.OperationTypeHardlink, row.OperationType)
	require.Equal(t, "", row.NFOSnapshot, "zero-value nfo_snapshot persisted")
	require.Equal(t, "/dst/new/W10-002.nfo", row.NFOPath)
	require.Equal(t, models.RevertStatusFailed, row.RevertStatus)
	require.Nil(t, row.RevertedAt, "nil reverted_at persisted")
	require.False(t, row.InPlaceRenamed, "zero-value in_place_renamed persisted")
	require.Equal(t, "/dst/new", row.OriginalDirPath)
	require.WithinDuration(t, original.CreatedAt, row.CreatedAt, 2*time.Second, "created_at carried from the record")
	require.False(t, row.UpdatedAt.Before(original.UpdatedAt), "updated_at stamped forward like Save")
}

// TestW10UpdateNonJournalFields_NeverWritesGeneratedFiles is the core
// invariant: a stale generated_files snapshot carried on the record NEVER
// overwrites the committed journal.
func TestW10UpdateNonJournalFields_NeverWritesGeneratedFiles(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	committedJournal := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{Destination: "/dst/a.jpg", Backup: "/dst/a.jpg.dlbak.1", DestSeq: 1}},
	})
	op := seedJournalTxRow(t, repo, models.GeneratedFilesJSON{})
	require.NoError(t, repo.UpdateJournalInTx(ctx, op.ID, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, perr := models.ParseGeneratedFiles(committedJournal)
		require.NoError(t, perr)
		return gf, true, nil
	}))

	// The caller's snapshot carries DIFFERENT (stale) journal bytes — the
	// scoped update must leave the committed column byte-identical.
	op.GeneratedFiles = models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{"/dst/stale"}})
	op.NewPath = "/dst/lib/W10.mkv"
	require.NoError(t, repo.UpdateNonJournalFields(ctx, op))

	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, committedJournal, row.GeneratedFiles, "generated_files untouched by the non-journal write")
	require.Equal(t, "/dst/lib/W10.mkv", row.NewPath, "non-journal columns still persisted")
}

// TestW10UpdateNonJournalFields_CommitsBetweenJournalTxAndColumnWrite is the
// repo-level ordering pin for the wave-10 race: journal tx commits → a
// third-party UpdateJournalInTx append commits → the completion's scoped
// column write lands carrying its PRE-append snapshot. Under the wave-9 full
// Save the foreign entry was erased; now it survives.
func TestW10UpdateNonJournalFields_CommitsBetweenJournalTxAndColumnWrite(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	op := seedJournalTxRow(t, repo, models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{Destination: "/dst/own.jpg", Backup: "/dst/own.jpg.dlbak.1", DestSeq: 1}},
	})

	// 1. Journal transaction commits (e.g. the completion merge).
	var txSnapshot string
	require.NoError(t, repo.UpdateJournalInTx(ctx, op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, perr := models.ParseGeneratedFiles(current.GeneratedFiles)
		if perr != nil {
			return models.GeneratedFilesJSON{}, false, perr
		}
		gf.Roots = append(gf.Roots, "/dst/leaf")
		txSnapshot = models.MarshalLedgerJSON(gf)
		return gf, true, nil
	}))

	// 2. A third-party append commits BETWEEN the tx and the column update.
	require.NoError(t, repo.UpdateJournalInTx(ctx, op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, perr := models.ParseGeneratedFiles(current.GeneratedFiles)
		if perr != nil {
			return models.GeneratedFilesJSON{}, false, perr
		}
		gf.Replacements = append(gf.Replacements, models.ReplacementEntry{Destination: "/dst/foreign.jpg", Backup: "/dst/foreign.jpg.dlbak.9", DestSeq: 9})
		return gf, true, nil
	}))

	// 3. The completion's column update lands LAST, carrying the tx-commit
	// snapshot (which predates the foreign append).
	op.GeneratedFiles = txSnapshot
	op.NewPath = "/dst/lib/movie.mkv"
	require.NoError(t, repo.UpdateNonJournalFields(ctx, op))

	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, "/dst/lib/movie.mkv", row.NewPath, "non-journal columns persisted")
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Contains(t, gf.Roots, "/dst/leaf", "the tx-committed merge survives")
	require.Len(t, gf.Replacements, 2, "the foreign append is NOT erased by the trailing column write")
	require.Equal(t, "/dst/foreign.jpg", gf.Replacements[1].Destination)
}

// TestW10UpdateNonJournalFields_NilRecordRefused pins the defensive leg.
func TestW10UpdateNonJournalFields_NilRecordRefused(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	err := repo.UpdateNonJournalFields(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be nil")
}

// TestW10UpdateNonJournalFields_MissingRowIsANoOp documents the contract for
// a vanished row: Updates affects zero rows without erroring (unlike Save,
// which would upsert and resurrect a deleted record).
func TestW10UpdateNonJournalFields_MissingRowIsANoOp(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	require.NoError(t, repo.UpdateNonJournalFields(context.Background(), &models.BatchFileOperation{ID: 424242, NewPath: "/nowhere"}))
}

// TestW10UpdateNonJournalFields_DBErrorSurfaces drives the wrapDBErr leg.
func TestW10UpdateNonJournalFields_DBErrorSurfaces(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE batch_file_operations").Error)
	err := repo.UpdateNonJournalFields(context.Background(), &models.BatchFileOperation{ID: 1, NewPath: "/x"})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNotFound)
	require.Contains(t, err.Error(), "update non-journal fields")
	require.Contains(t, err.Error(), "batch file operation 1")
}

// TestW10BatchFileOperationRepositorySatisfiesInterface keeps the concrete
// repository's interface conformance (incl. the wave-10 additive method)
// compile-pinned.
func TestW10BatchFileOperationRepositorySatisfiesInterface(t *testing.T) {
	db := newDatabaseTestDB(t)
	var iface BatchFileOperationRepositoryInterface = NewBatchFileOperationRepository(db)
	require.NotNil(t, iface)
}
