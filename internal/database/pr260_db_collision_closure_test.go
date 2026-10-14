package database

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPR260SuppressionAndRestoreFaultsRollbackOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, trigger string
		restore       bool
	}{
		{"suppress-collision", "BEFORE UPDATE OF status ON credit_collisions", false},
		{"suppress-projection", "BEFORE DELETE ON movie_actresses", false},
		{"restore-collision", "BEFORE UPDATE OF status ON credit_collisions", true},
		{"restore-projection", "BEFORE INSERT ON movie_actresses", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, service, credit, collision := collisionFixture(t)
			require.NoError(t, db.Model(&models.CreditCollision{}).Where("id = ?", collision.ID).Update("user_pinned", true).Error)
			require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", credit.MovieContentID, credit.ActressID).Error)
			if tc.restore {
				require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, true))
			}
			var creditBefore models.MovieCredit
			require.NoError(t, db.First(&creditBefore, credit.ID).Error)
			before := loadArtifactPublicationMovie(t, db, credit.MovieContentID)
			var projectionBefore []uint
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", credit.MovieContentID).Pluck("actress_id", &projectionBefore).Error)
			require.NoError(t, db.Exec("CREATE TRIGGER fail_suppression "+tc.trigger+" BEGIN SELECT RAISE(ABORT, 'suppression fault'); END").Error)
			t.Cleanup(func() { require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS fail_suppression").Error) })
			err := service.SetCreditSuppressed(context.Background(), credit.ID, !tc.restore)
			require.ErrorContains(t, err, "suppression fault")
			var savedCredit models.MovieCredit
			require.NoError(t, db.First(&savedCredit, credit.ID).Error)
			require.Equal(t, tc.restore, savedCredit.Suppressed)
			require.Equal(t, credit.ActressID, savedCredit.ActressID)
			require.Equal(t, credit.CreditedName, savedCredit.CreditedName)
			require.Equal(t, creditBefore.Origin, savedCredit.Origin)
			var savedCollision models.CreditCollision
			require.NoError(t, db.First(&savedCollision, collision.ID).Error)
			require.True(t, savedCollision.UserPinned)
			if tc.restore {
				require.Equal(t, models.CollisionStatusResolved, savedCollision.Status)
				require.Equal(t, models.CollisionResolutionBySuppression, savedCollision.Resolution)
			} else {
				require.Equal(t, models.CollisionStatusOpen, savedCollision.Status)
				require.Empty(t, savedCollision.Resolution)
			}
			var projectionAfter []uint
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", credit.MovieContentID).Pluck("actress_id", &projectionAfter).Error)
			require.Equal(t, projectionBefore, projectionAfter)
			after := loadArtifactPublicationMovie(t, db, credit.MovieContentID)
			require.Equal(t, before.RenderGeneration, after.RenderGeneration)
			require.Equal(t, before.RenderDirty, after.RenderDirty)
		})
	}
}

func TestPR260RestoreCollisionQueryFailureRollsBack(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, true))
	before := loadArtifactPublicationMovie(t, db, credit.MovieContentID)
	const callback = "pr260_restore_collision_query_fault"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "credit_collisions" {
			tx.AddError(errors.New("collision query fault"))
		}
	}))
	t.Cleanup(func() { db.Callback().Query().Remove(callback) })
	err := service.SetCreditSuppressed(context.Background(), credit.ID, false)
	require.ErrorContains(t, err, "collision query fault")
	var saved models.MovieCredit
	require.NoError(t, db.First(&saved, credit.ID).Error)
	require.True(t, saved.Suppressed)
	require.Equal(t, credit.ActressID, saved.ActressID)
	var stored models.CreditCollision
	// Remove the injected callback before querying the collision control.
	db.Callback().Query().Remove(callback)
	require.NoError(t, db.First(&stored, collision.ID).Error)
	require.Equal(t, models.CollisionStatusResolved, stored.Status)
	require.Equal(t, models.CollisionResolutionBySuppression, stored.Resolution)
	after := loadArtifactPublicationMovie(t, db, credit.MovieContentID)
	require.Equal(t, before.RenderGeneration, after.RenderGeneration)
}
