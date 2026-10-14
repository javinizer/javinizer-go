package database

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestDurableJournalRepositoryRejectsUnmetBoundaryBeforeMerge(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "synthetic.sqlite"), LogLevel: "silent"})
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.AutoMigrate(&models.Job{}, &models.BatchFileOperation{}))
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	repo := NewBatchFileOperationRepository(db)
	op := &models.BatchFileOperation{BatchJobID: "durability-refusal", RevertStatus: models.RevertStatusApplied, GeneratedFiles: `{"roots":["untouched"]}`}
	require.NoError(t, repo.Create(context.Background(), op))
	require.NoError(t, db.Exec("PRAGMA journal_mode=DELETE").Error)
	var before int
	require.NoError(t, db.Raw("PRAGMA synchronous").Scan(&before).Error)
	called := false
	err = repo.UpdateJournalInTx(WithDurableJournal(context.Background()), op.ID, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		called = true
		return models.GeneratedFilesJSON{}, true, nil
	})
	require.ErrorContains(t, err, "requires SQLite WAL")
	require.False(t, called)
	current, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, op.GeneratedFiles, current.GeneratedFiles)
	var after int
	require.NoError(t, db.Raw("PRAGMA synchronous").Scan(&after).Error)
	require.Equal(t, before, after)
}
