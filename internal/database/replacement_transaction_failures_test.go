package database

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestArtifactPublicationFinalizationRechecksAuthority(t *testing.T) {
	for _, mode := range []string{"movie removed", "generation changed", "collision opened", "collision lookup failed", "context canceled", "final context canceled"} {
		t.Run(mode, func(t *testing.T) {
			db := setupBaseRepoTestDB(t)
			seedArtifactPublicationMovie(t, db, "final-authority", 7, false)
			actress := models.Actress{FirstName: "Final", Verified: true}
			require.NoError(t, db.Create(&actress).Error)
			credit := models.MovieCredit{MovieContentID: "final-authority", ActressID: actress.ID, CreditedName: "Final"}
			require.NoError(t, db.Create(&credit).Error)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			published := 0
			if mode == "final context canceled" {
				const hook = "cancel_final_publication_collision_count"
				require.NoError(t, db.Callback().Query().After("gorm:query").Register(hook, func(tx *gorm.DB) {
					if published > 0 && tx.Statement != nil && tx.Statement.Table == "credit_collisions" {
						cancel()
					}
				}))
				t.Cleanup(func() { _ = db.Callback().Query().Remove(hook) })
			}
			err := NewMovieRepository(db).WithApplyArtifactPublicationFence(ctx, "final-authority", 7, func(*models.Movie) error {
				published++
				var mutation error
				switch mode {
				case "movie removed":
					mutation = db.Exec("DELETE FROM movies WHERE content_id = ?", "final-authority").Error
				case "generation changed":
					mutation = db.Exec("UPDATE movies SET render_generation = 8 WHERE content_id = ?", "final-authority").Error
				case "collision opened":
					mutation = db.Create(&models.CreditCollision{CreditID: credit.ID, MovieContentID: "final-authority", Status: models.CollisionStatusOpen, Field: models.CreditFieldCreditedName}).Error
				case "collision lookup failed":
					mutation = db.Exec("DROP TABLE credit_collisions").Error
				case "context canceled":
					cancel()
				}
				return mutation
			})
			require.Equal(t, 1, published)
			switch mode {
			case "movie removed":
				require.ErrorIs(t, err, ErrNotFound)
			case "generation changed":
				require.ErrorIs(t, err, ErrApplyPublicationStale)
			case "collision opened":
				require.ErrorIs(t, err, ErrApplyArtifactPublicationBlocked)
			case "collision lookup failed":
				require.ErrorContains(t, err, "credit_collisions")
			case "context canceled", "final context canceled":
				require.True(t, errors.Is(err, context.Canceled), "%v", err)
			}
		})
	}
}

func TestCandidateBackfillQuarantinesLegacyDuplicatePositiveIdentity(t *testing.T) {
	db := newCreditTestDB(t)
	require.NoError(t, db.Exec("DROP INDEX idx_actresses_dmm_id_positive").Error)
	for _, name := range []string{"Shared", "Ｓｈａｒｅｄ"} {
		require.NoError(t, db.Exec("INSERT INTO actresses (dmm_id, japanese_name, name_key, origin, verified, ambiguity_quarantined) VALUES (?, ?, ?, ?, ?, ?)", 90901, name, nil, ActressOriginScrape, false, false).Error)
	}

	require.NoError(t, backfillActressCandidateNameKeys(t.Context(), db.DB))
	var candidates []models.Actress
	require.NoError(t, db.Where("dmm_id = ?", 90901).Order("id").Find(&candidates).Error)
	require.Len(t, candidates, 2)
	for i := range candidates {
		require.True(t, candidates[i].AmbiguityQuarantined)
		require.Empty(t, candidates[i].NameKey)
	}
}

func TestCandidateBackfillRollsBackWhenLegacyQuarantineCannotBeCleared(t *testing.T) {
	db := newCreditTestDB(t)
	candidate := models.Actress{JapaneseName: "Recovered", Origin: ActressOriginScrape, NameKey: "legacy", AmbiguityQuarantined: true}
	require.NoError(t, db.Create(&candidate).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_candidate_unquarantine BEFORE UPDATE OF ambiguity_quarantined ON actresses WHEN NEW.ambiguity_quarantined = 0 BEGIN SELECT RAISE(ABORT, 'unquarantine refused'); END").Error)

	err := backfillActressCandidateNameKeys(t.Context(), db.DB)
	require.ErrorContains(t, err, "unquarantine refused")
	var stored models.Actress
	require.NoError(t, db.First(&stored, candidate.ID).Error)
	require.True(t, stored.AmbiguityQuarantined)
	require.Equal(t, "legacy", stored.NameKey, "transaction rollback must retain the prior key")
}
