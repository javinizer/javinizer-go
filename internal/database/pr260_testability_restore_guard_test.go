package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The restore helper also rejects incomplete identities when called independently
// of SetCreditSuppressed's preloaded-credit path.
func TestPR260RestoreHelperRejectsMissingIdentityWithoutWrites(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, true))
	before := loadArtifactPublicationMovie(t, db, credit.MovieContentID)
	for _, tc := range []struct {
		name   string
		credit *models.MovieCredit
	}{
		{"nil credit", nil},
		{"nil actress", &models.MovieCredit{ID: credit.ID, MovieContentID: credit.MovieContentID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := db.Transaction(func(tx *gorm.DB) error {
				return restoreSuppressedCreditCollisionsTx(tx, tc.credit)
			})
			require.ErrorContains(t, err, "credit identity is missing")
			var saved models.MovieCredit
			require.NoError(t, db.First(&saved, credit.ID).Error)
			require.True(t, saved.Suppressed)
			require.Equal(t, credit.ActressID, saved.ActressID)
			var stored models.CreditCollision
			require.NoError(t, db.First(&stored, collision.ID).Error)
			require.Equal(t, models.CollisionStatusResolved, stored.Status)
			require.Equal(t, models.CollisionResolutionBySuppression, stored.Resolution)
			after := loadArtifactPublicationMovie(t, db, credit.MovieContentID)
			require.Equal(t, before.RenderGeneration, after.RenderGeneration)
			require.Equal(t, before.RenderDirty, after.RenderDirty)
			var ids []uint
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", credit.MovieContentID).Pluck("actress_id", &ids).Error)
			require.NotContains(t, ids, credit.ActressID)
		})
	}
}
