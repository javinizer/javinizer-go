package database

// POSTER-WRITE-HARDENING wave-15 (codex follow-up, P1) — "preserve a
// concurrently reverted status": UpdateNonJournalFields used to write
// revert_status + reverted_at unconditionally from the caller's STALE
// in-memory row, so an UpdateRevertStatus(Reverted) committed by another
// process between the caller's read and this write was silently clobbered
// back to Applied/Failed — a reverted operation looked live again.
//
// The update is now two statements inside one BEGIN IMMEDIATE transaction:
// the non-status columns land unconditionally, while revert_status +
// reverted_at persist only WHERE the stored row is not already reverted. A
// suppressed status write (stale completion losing the race) commits the
// non-status columns and reports ErrOperationRowReverted. These tests pin the
// guard on sqlite with a deterministic stale snapshot.

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// TestW15UpdateNonJournalFields_ConcurrentRevertSurvivesStaleCompletion is
// the finding's race, replayed deterministically: the caller's snapshot is
// hydrated while the row is Applied, another writer commits Reverted, and the
// completion's column update lands carrying the stale Applied status. The
// stored status STAYS reverted (and keeps its original reverted_at stamp)
// while the non-status columns still persist.
func TestW15UpdateNonJournalFields_ConcurrentRevertSurvivesStaleCompletion(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	op := &models.BatchFileOperation{
		BatchJobID:    "job-w15-stale",
		MovieID:       "W15-STALE",
		OriginalPath:  "/src/W15-STALE.mkv",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	// The completion's hydrated snapshot — read while the row is still
	// Applied, BEFORE the concurrent revert commits.
	stale, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusApplied, stale.RevertStatus)

	// The concurrent writer reverts the operation first.
	require.NoError(t, repo.UpdateRevertStatus(ctx, op.ID, models.RevertStatusReverted))
	revertedRow, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.NotNil(t, revertedRow.RevertedAt, "UpdateRevertStatus stamps reverted_at")

	// The stale completion publishes: Applied status, mutated non-status
	// columns, and a junk journal snapshot that must never land either.
	stale.NewPath = "/dst/lib/W15-STALE.mkv"
	stale.OriginalDirPath = "/dst/lib"
	staleYear := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	stale.RevertedAt = &staleYear // a visible marker that must never persist
	stale.GeneratedFiles = models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{"/dst/stale-junk"}})

	err = repo.UpdateNonJournalFields(ctx, stale)
	require.ErrorIs(t, err, ErrOperationRowReverted,
		"a completion whose status columns were suppressed loses the race loudly")

	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus,
		"the concurrent revert is never clobbered back to a live status")
	require.NotNil(t, row.RevertedAt)
	require.Equal(t, revertedRow.RevertedAt.UTC().Year(), row.RevertedAt.UTC().Year())
	require.NotEqual(t, 2001, row.RevertedAt.UTC().Year(), "the stale reverted_at stamp never lands")
	require.WithinDuration(t, revertedRow.RevertedAt.UTC(), row.RevertedAt.UTC(), 2*time.Second,
		"the original revert stamp is preserved")
	require.Equal(t, "/dst/lib/W15-STALE.mkv", row.NewPath, "non-status columns still persist on a lost race")
	require.Equal(t, "/dst/lib", row.OriginalDirPath)
	require.Equal(t, "", row.GeneratedFiles, "the stale journal snapshot never lands (journal stays with UpdateJournalInTx)")
}

// TestW15UpdateNonJournalFields_NormalCompletionStillFlipsStatus pins the
// non-raced path: stored Applied + caller's completion status persists
// exactly as the wave-10 write did.
func TestW15UpdateNonJournalFields_NormalCompletionStillFlipsStatus(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	op := &models.BatchFileOperation{
		BatchJobID:    "job-w15-normal",
		MovieID:       "W15-NORMAL",
		OriginalPath:  "/src/W15-NORMAL.mkv",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	row.RevertStatus = models.RevertStatusFailed // CompleteFailed's mark
	row.NewPath = "/dst/lib/W15-NORMAL.mkv"
	row.RevertedAt = nil

	require.NoError(t, repo.UpdateNonJournalFields(ctx, row))

	got, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusFailed, got.RevertStatus,
		"a non-raced completion status persists exactly as before")
	require.Nil(t, got.RevertedAt)
	require.Equal(t, "/dst/lib/W15-NORMAL.mkv", got.NewPath)
}

// TestW15UpdateNonJournalFields_RevertedCallerAgainstRevertedRowIsIdempotent:
// a caller CARRYING Reverted against an already-reverted row is an idempotent
// no-op — no race error, the earlier revert stamp is preserved.
func TestW15UpdateNonJournalFields_RevertedCallerAgainstRevertedRowIsIdempotent(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	op := &models.BatchFileOperation{
		BatchJobID:    "job-w15-idem",
		MovieID:       "W15-IDEM",
		OriginalPath:  "/src/W15-IDEM.mkv",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))
	require.NoError(t, repo.UpdateRevertStatus(ctx, op.ID, models.RevertStatusReverted))
	revertedRow, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.NotNil(t, revertedRow.RevertedAt)
	originalStamp := revertedRow.RevertedAt.UTC()

	revertedRow.NewPath = "/dst/lib/W15-IDEM.mkv"
	revertedRow.RevertedAt = nil // even a nulled stamp must not erase the original
	require.NoError(t, repo.UpdateNonJournalFields(ctx, revertedRow),
		"an idempotent reverted write is not a lost race")

	got, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusReverted, got.RevertStatus)
	require.NotNil(t, got.RevertedAt, "the original revert stamp survives the idempotent write")
	require.WithinDuration(t, originalStamp, got.RevertedAt.UTC(), 2*time.Second)
	require.Equal(t, "/dst/lib/W15-IDEM.mkv", got.NewPath, "non-status columns persist")
}

// TestW15UpdateNonJournalFields_MissingRowStaysANoOp pins the wave-10
// missing-row contract through the new guarded statements: zero rows affected
// without an error (unlike Save, which would upsert-resurrect the row).
func TestW15UpdateNonJournalFields_MissingRowStaysANoOp(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	require.NoError(t, repo.UpdateNonJournalFields(context.Background(), &models.BatchFileOperation{
		ID:           424243,
		RevertStatus: models.RevertStatusApplied,
		NewPath:      "/nowhere",
	}))
}
