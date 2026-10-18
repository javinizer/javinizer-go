package database

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func columnGuardDBs(t *testing.T) (*DB, *BatchFileOperationRepository, *BatchFileOperationRepository) {
	t.Helper()
	cfg := &Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "column-guard.sqlite"), LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	other, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })
	return db, NewBatchFileOperationRepository(db), NewBatchFileOperationRepository(other)
}

// Pin native SQLite hooks to the pool's one connection. Update hooks count
// actual row writes even if the transaction later rolls back, so a zero count
// proves refusal happens before metadata or status SQL executes.
func columnGuardHooks(t *testing.T, db *DB, denyJournalRead bool) *atomic.Int32 {
	t.Helper()
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	var writes atomic.Int32
	configure := func(install bool) {
		conn, err := sqlDB.Conn(context.Background())
		require.NoError(t, err)
		require.NoError(t, conn.Raw(func(raw any) error {
			native := raw.(*sqlite3.SQLiteConn)
			if !install {
				native.RegisterUpdateHook(nil)
				native.RegisterAuthorizer(nil)
				return nil
			}
			native.RegisterUpdateHook(func(_ int, _ string, table string, _ int64) {
				if table == "batch_file_operations" {
					writes.Add(1)
				}
			})
			if denyJournalRead {
				native.RegisterAuthorizer(func(action int, table, column, _ string) int {
					if action == sqlite3.SQLITE_READ && table == "batch_file_operations" && column == "generated_files" {
						return sqlite3.SQLITE_DENY
					}
					return sqlite3.SQLITE_OK
				})
			}
			return nil
		}))
		require.NoError(t, conn.Close())
	}
	configure(true)
	t.Cleanup(func() { configure(false) })
	return &writes
}

func TestOperationColumnGuardFreshRefusalPrecedesAllWrites(t *testing.T) {
	db, repo, other := columnGuardDBs(t)
	op := seedJournalTxRow(t, repo, models.GeneratedFilesJSON{Roots: []string{"/old"}})
	stale, err := repo.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	// Another connection commits both state dimensions after the caller read.
	require.NoError(t, other.UpdateJournalInTx(t.Context(), op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		ledger, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		ledger.UndoStarted = true
		return ledger, true, err
	}))
	require.NoError(t, other.UpdateRevertStatus(t.Context(), op.ID, models.RevertStatusFailed))
	before, err := other.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	stale.BatchJobID = "stale-job"
	stale.MovieID = "STALE-MOVIE"
	stale.OriginalPath = "/stale/source"
	stale.NewPath = "/stale/destination"
	stale.OperationType = models.OperationTypeCopy
	stale.NFOSnapshot = "stale nfo snapshot"
	stale.NFOPath = "/stale/nfo"
	stale.InPlaceRenamed = true
	stale.OriginalDirPath = "/stale/directory"
	stale.CreatedAt = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	stale.RevertedAt = &stale.CreatedAt
	refused := errors.New("column completion refused after Undo admission")
	var saw *models.BatchFileOperation
	ctx := WithOperationColumnGuard(t.Context(), func(current *models.BatchFileOperation) error {
		snapshot := *current
		saw = &snapshot
		return refused
	})
	writes := columnGuardHooks(t, db, false)
	err = repo.UpdateNonJournalFields(ctx, stale)
	require.ErrorIs(t, err, refused)
	require.Equal(t, &models.BatchFileOperation{ID: op.ID, GeneratedFiles: before.GeneratedFiles, RevertStatus: models.RevertStatusFailed}, saw)
	require.Zero(t, writes.Load(), "refusal must precede even a rolled-back metadata/status write")
	after, err := other.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	require.Equal(t, before, after, "every persisted field and timestamp remains unchanged")
	// The failed transaction released its lock and the guard is context-local.
	accepted := *before
	accepted.NewPath = "/accepted/destination"
	require.NoError(t, repo.UpdateNonJournalFields(t.Context(), &accepted))
	require.Equal(t, int32(2), writes.Load())
}

func TestOperationColumnGuardSuccessPreservesJournal(t *testing.T) {
	db, repo, other := columnGuardDBs(t)
	op := seedJournalTxRow(t, repo, models.GeneratedFilesJSON{Roots: []string{"/committed"}})
	before, err := other.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	caller := *before
	caller.GeneratedFiles = "stale caller journal must not persist"
	caller.NewPath = "/accepted/output"
	caller.NFOPath = "/accepted/output.nfo"
	caller.RevertStatus = models.RevertStatusFailed
	calls := 0
	ctx := WithOperationColumnGuard(t.Context(), func(current *models.BatchFileOperation) error {
		calls++
		require.Equal(t, &models.BatchFileOperation{ID: op.ID, GeneratedFiles: before.GeneratedFiles, RevertStatus: before.RevertStatus}, current)
		return nil
	})
	writes := columnGuardHooks(t, db, false)
	require.NoError(t, repo.UpdateNonJournalFields(ctx, &caller))
	require.Equal(t, 1, calls)
	require.Equal(t, int32(2), writes.Load())
	after, err := other.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	require.Equal(t, caller.NewPath, after.NewPath)
	require.Equal(t, caller.NFOPath, after.NFOPath)
	require.Equal(t, models.RevertStatusFailed, after.RevertStatus)
	require.Equal(t, before.GeneratedFiles, after.GeneratedFiles)
}

func TestOperationColumnGuardMissingRowRemainsNoOp(t *testing.T) {
	db, repo, other := columnGuardDBs(t)
	called := false
	ctx := WithOperationColumnGuard(t.Context(), func(*models.BatchFileOperation) error {
		called = true
		return errors.New("must not validate a nonexistent row")
	})
	writes := columnGuardHooks(t, db, false)
	require.NoError(t, repo.UpdateNonJournalFields(ctx, &models.BatchFileOperation{ID: 999999, NewPath: "/never-created", RevertStatus: models.RevertStatusApplied}))
	require.False(t, called)
	require.Zero(t, writes.Load())
	_, err := other.FindByID(t.Context(), 999999)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestOperationColumnGuardReadFailurePrecedesAllWrites(t *testing.T) {
	db, repo, other := columnGuardDBs(t)
	op := seedJournalTxRow(t, repo, models.GeneratedFilesJSON{Roots: []string{"/committed"}})
	before, err := other.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	caller := *before
	caller.NewPath = "/must-not-persist"
	caller.RevertStatus = models.RevertStatusFailed
	called := false
	ctx := WithOperationColumnGuard(t.Context(), func(*models.BatchFileOperation) error {
		called = true
		return nil
	})
	writes := columnGuardHooks(t, db, true)
	err = repo.UpdateNonJournalFields(ctx, &caller)
	require.Error(t, err)
	require.ErrorContains(t, err, "generated_files")
	require.ErrorContains(t, err, "update non-journal fields")
	require.False(t, called)
	require.Zero(t, writes.Load())
	after, err := other.FindByID(t.Context(), op.ID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	// A nil guard introduces no fresh-journal read and keeps old behavior.
	require.NoError(t, repo.UpdateNonJournalFields(WithOperationColumnGuard(t.Context(), nil), &caller))
	require.Equal(t, int32(2), writes.Load())
}
