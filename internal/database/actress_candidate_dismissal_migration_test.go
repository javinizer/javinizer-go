package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCandidateDismissalMigrationUpgradeRollbackAndCascade(t *testing.T) {
	db := newCreditTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, sqlDB)
	_, err = provider.DownTo(t.Context(), 27)
	require.NoError(t, err)
	candidate := models.Actress{FirstName: "Existing candidate", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&candidate).Error)
	_, err = provider.Up(t.Context())
	require.NoError(t, err)
	repo := NewActressRepository(db)
	count, err := repo.CountCandidates(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	count, err = repo.CountDismissedCandidates(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
	require.NoError(t, repo.SetCandidateDismissed(t.Context(), candidate.ID, true))
	_, err = provider.DownTo(t.Context(), 27)
	require.NoError(t, err)
	var stored models.Actress
	require.NoError(t, db.First(&stored, candidate.ID).Error)
	require.Equal(t, candidate, stored, "rolling back queue state must not alter identities")
	_, err = provider.Up(t.Context())
	require.NoError(t, err)
	count, err = repo.CountDismissedCandidates(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
	require.NoError(t, db.Exec("PRAGMA foreign_keys = ON").Error)
	require.NoError(t, repo.SetCandidateDismissed(t.Context(), candidate.ID, true))
	require.NoError(t, db.Delete(&models.Actress{}, candidate.ID).Error)
	require.NoError(t, db.Table("actress_candidate_dismissals").Count(&count).Error)
	require.Zero(t, count, "raw identity deletion must cascade while foreign keys are enabled")
	require.Error(t, db.Exec("INSERT INTO actress_candidate_dismissals(actress_id, dismissed_at) VALUES (?, CURRENT_TIMESTAMP)", candidate.ID).Error)
}
