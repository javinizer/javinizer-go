package database

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestDismissedCandidatesDefaultPageAndCanceledRead(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	candidates := make([]models.Actress, 51)
	for i := range candidates {
		candidates[i] = models.Actress{FirstName: fmt.Sprintf("Candidate %d", i), Origin: ActressOriginScrape, UpdatedAt: stamp}
	}
	require.NoError(t, db.Create(&candidates).Error)
	for _, candidate := range candidates {
		require.NoError(t, repo.SetCandidateDismissed(t.Context(), candidate.ID, true))
	}
	for _, limit := range []int{0, -1} {
		page, err := repo.ListDismissedCandidates(t.Context(), limit, 0)
		require.NoError(t, err)
		require.Len(t, page, 50)
		for i := range page {
			require.Equal(t, candidates[i].ID, page[i].ID)
		}
		tail, err := repo.ListDismissedCandidates(t.Context(), limit, 50)
		require.NoError(t, err)
		require.Len(t, tail, 1)
		require.Equal(t, candidates[50].ID, tail[0].ID)
	}
	before := previewDatabaseState(t, db)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	page, err := repo.ListDismissedCandidates(ctx, 10, 0)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, page, "failed reads must not expose partial results")
	count, err := repo.CountDismissedCandidates(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, count)
	require.Equal(t, before, previewDatabaseState(t, db))
	count, err = repo.CountDismissedCandidates(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 51, count)
	page, err = repo.ListDismissedCandidates(t.Context(), 1, 0)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, candidates[0].ID, page[0].ID)
}

func TestCandidateDismissalCleanupFailureRollsBackDeleteAndMerge(t *testing.T) {
	for _, operation := range []string{"delete", "merge"} {
		t.Run(operation, func(t *testing.T) {
			db := newRollbackContractDB(t)
			repo := NewActressRepository(db)
			source := models.Actress{FirstName: "Dismissed source", Origin: ActressOriginScrape}
			target := models.Actress{FirstName: "Verified target", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&source).Error)
			require.NoError(t, repo.Create(t.Context(), &target))
			movie := models.Movie{ContentID: "dismissal-rollback", ID: "DISMISSAL-ROLLBACK", Actresses: []models.Actress{source}, RenderGeneration: 7}
			require.NoError(t, db.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: source.ID, CreditedName: "Source credit", OverrideName: "Local credit", UserOverride: true, Origin: string(models.CreditOriginUser)}
			require.NoError(t, db.Create(&credit).Error)
			require.NoError(t, db.Create(&models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusOpen, ReportedValue: "Source evidence"}).Error)
			require.NoError(t, db.Create(&models.ActressTranslation{ActressID: source.ID, Language: "en", DisplayName: "Source translation"}).Error)
			require.NoError(t, repo.SetCandidateDismissed(t.Context(), source.ID, true))
			before := previewDatabaseState(t, db)
			require.NoError(t, db.Exec(fmt.Sprintf(`CREATE TEMP TRIGGER reject_dismissal_cleanup BEFORE DELETE ON actress_candidate_dismissals
WHEN OLD.actress_id = %d BEGIN SELECT RAISE(ABORT, 'dismissal cleanup rejected'); END`, source.ID)).Error)
			t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS reject_dismissal_cleanup").Error })
			run := func() error {
				if operation == "delete" {
					return repo.Delete(t.Context(), source.ID)
				}
				result, err := repo.Merge(t.Context(), target.ID, source.ID, nil)
				if err != nil {
					require.Nil(t, result)
				}
				return err
			}
			requireTriggerFailure(t, run(), "dismissal cleanup rejected")
			require.Equal(t, before, previewDatabaseState(t, db), "cleanup failure must roll back identities, credits, collisions, translations, projections and render state")
			require.NoError(t, db.Exec("DROP TRIGGER reject_dismissal_cleanup").Error)
			require.NoError(t, run())
			_, err := repo.FindByID(t.Context(), source.ID)
			require.ErrorIs(t, err, ErrNotFound)
			count, err := repo.CountDismissedCandidates(t.Context())
			require.NoError(t, err)
			require.Zero(t, count)
			var remaining []models.MovieCredit
			require.NoError(t, db.Find(&remaining).Error)
			if operation == "delete" {
				require.Empty(t, remaining)
			} else {
				require.Len(t, remaining, 1)
				require.Equal(t, target.ID, remaining[0].ActressID)
				require.Equal(t, credit.OverrideName, remaining[0].OverrideName)
			}
		})
	}
}
