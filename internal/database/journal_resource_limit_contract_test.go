package database

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// This tests configured SQLite resource exhaustion, not the application's default limit.
func TestJournalResourceLimitPreservesLedgerAndRecovers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := newDatabaseTestDB(t)
	pool, err := db.DB.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	pool.SetConnMaxLifetime(0)
	pool.SetConnMaxIdleTime(0)
	repo := NewBatchFileOperationRepository(db)
	jobs := NewJobRepository(db)
	require.NoError(t, jobs.Create(ctx, &models.Job{ID: "job-limit", Status: models.JobStatusCompleted}))
	ledger := models.GeneratedFilesJSON{}
	for i := 0; i < 256; i++ {
		path := fmt.Sprintf("/owned/%s/art-%03d.jpg", strings.Repeat("a", 128), i)
		ledger.Replacements = append(ledger.Replacements, models.ReplacementEntry{Destination: path, Backup: path + ".dlbak.1", DestSeq: 1, Installed: true})
	}
	row := &models.BatchFileOperation{BatchJobID: "job-limit", MovieID: "LIMIT-001", OriginalPath: "/owned/source.mkv", NewPath: "/owned/dest.mkv", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, GeneratedFiles: models.MarshalLedgerJSON(ledger)}
	require.NoError(t, repo.Create(ctx, row))
	before, err := repo.FindByID(ctx, row.ID)
	require.NoError(t, err)
	const limit = 32768
	require.Greater(t, len(before.GeneratedFiles), limit)
	var driverConn *sqlite3.SQLiteConn
	original := 0
	setLimit := func(value int, initial bool) {
		t.Helper()
		connectionCtx, connectionCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer connectionCancel()
		conn, err := pool.Conn(connectionCtx)
		require.NoError(t, err)
		defer conn.Close()
		require.NoError(t, conn.Raw(func(raw any) error {
			native, ok := raw.(*sqlite3.SQLiteConn)
			require.True(t, ok)
			if initial {
				driverConn = native
				original = native.GetLimit(sqlite3.SQLITE_LIMIT_LENGTH)
			} else {
				require.Same(t, driverConn, native, "the configured connection must survive the failure")
			}
			native.SetLimit(sqlite3.SQLITE_LIMIT_LENGTH, value)
			require.Equal(t, value, native.GetLimit(sqlite3.SQLITE_LIMIT_LENGTH))
			return nil
		}))
	}
	// Register restoration before changing the owned connection limit; the cleanup
	// runs before the database cleanup even if any following assertion aborts.
	t.Cleanup(func() {
		if driverConn != nil {
			setLimit(original, false)
		}
	})
	setLimit(limit, true)
	called := false
	err = repo.UpdateJournalInTx(ctx, row.ID, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		called = true
		return models.GeneratedFilesJSON{}, true, nil
	})
	require.Error(t, err)
	require.False(t, called)
	var sqliteErr sqlite3.Error
	require.ErrorAs(t, err, &sqliteErr)
	require.Equal(t, sqlite3.ErrTooBig, sqliteErr.Code)
	require.Contains(t, err.Error(), fmt.Sprintf("update journal batch file operation %d", row.ID))
	t.Logf("configured projection failure: %v (SQLite code %d); original ledger bytes=%d", err, sqliteErr.Code, len(before.GeneratedFiles))
	setLimit(original, false)
	after, err := repo.FindByID(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, before, after, "failed read must preserve exact ledger bytes and every operation field")
	err = repo.UpdateJournalInTx(ctx, row.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		called = true
		require.Equal(t, before.GeneratedFiles, current.GeneratedFiles)
		require.Equal(t, before.RevertStatus, current.RevertStatus)
		next, parseErr := models.ParseGeneratedFiles(current.GeneratedFiles)
		require.NoError(t, parseErr)
		next.Replacements = append(next.Replacements, models.ReplacementEntry{Destination: "/owned/new.jpg", Backup: "/owned/new.jpg.dlbak.1", DestSeq: 1})
		return next, true, nil
	})
	require.NoError(t, err)
	require.True(t, called)
	recovered, err := repo.FindByID(ctx, row.ID)
	require.NoError(t, err)
	got, err := models.ParseGeneratedFiles(recovered.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, got.Replacements, len(ledger.Replacements)+1)
	require.Equal(t, ledger.Replacements, got.Replacements[:len(ledger.Replacements)])
	require.Equal(t, "/owned/new.jpg", got.Replacements[len(ledger.Replacements)].Destination)
	// Only the ledger and its update timestamp are allowed to change.
	recovered.GeneratedFiles = before.GeneratedFiles
	recovered.UpdatedAt = before.UpdatedAt
	require.Equal(t, before, recovered)
}
