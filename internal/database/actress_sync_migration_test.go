package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfirmedNameSyncMigrationPreservesHistoryButRevokesOnRollback(t *testing.T) {
	db, repo, snapshot, review, now, _ := durableNameFixture(t)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "migration-receipt", review, snapshot, now)
	require.NoError(t, err)
	lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "old-worker", now, time.Minute)
	require.NoError(t, err)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, sqlDB)
	version, err := provider.GetDBVersion(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 27, version)
	_, err = provider.DownTo(t.Context(), 25)
	require.NoError(t, err)
	var triggers int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'actress_sync_%'").Scan(&triggers).Error)
	require.Zero(t, triggers)
	var kept ActressSyncTask
	require.NoError(t, db.First(&kept, job.Tasks[0].ID).Error)
	require.Equal(t, "invalidated", kept.Status)
	require.Equal(t, "migration_rollback", kept.Reason)
	_, err = provider.Up(t.Context())
	require.NoError(t, err)
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'actress_sync_%'").Scan(&triggers).Error)
	require.EqualValues(t, 4, triggers)
	_, err = repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now)
	require.ErrorIs(t, err, ErrActressSyncLease)
	after, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, job.RequestHash, after.RequestHash)
	require.Equal(t, "invalidated", after.Tasks[0].Status)
	var protected bool
	require.NoError(t, db.Raw("SELECT protected FROM actress_sync_name_guards WHERE actress_id = ?", review.Proposals[0].ActressID).Scan(&protected).Error)
	require.True(t, protected)
	_, err = repo.SubmitConfirmedOfflineNameSync(t.Context(), "cannot-regrant-after-rollback", review, snapshot, now)
	require.ErrorIs(t, err, ErrActressSyncProtected)
}
