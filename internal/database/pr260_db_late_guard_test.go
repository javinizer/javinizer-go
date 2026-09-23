package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPR260ApplyPublicationTranslationLoadFailureNeverPublishes(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	seedArtifactPublicationMovie(t, db, "apply-translation", 3, true)
	require.NoError(t, db.Create(&models.MovieTranslation{MovieID: "apply-translation", Language: "en"}).Error)
	require.NoError(t, db.Exec("DROP TABLE movie_translations").Error)
	called := false
	err := NewMovieRepository(db).WithApplyPublicationFence(context.Background(), "apply-translation", 3, func(*models.Movie) error { called = true; return nil })
	require.ErrorContains(t, err, "movie_translations")
	require.False(t, called)
	saved := loadArtifactPublicationMovie(t, db, "apply-translation")
	require.True(t, saved.RenderDirty)
	require.EqualValues(t, 3, saved.RenderGeneration)
}

func TestPR260RelinkMergeDeleteFailureRetainsPinnedSource(t *testing.T) {
	db, service, source, collision := collisionFixture(t)
	target := models.Actress{FirstName: "Reported", LastName: "Person", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	survivor := models.MovieCredit{MovieContentID: source.MovieContentID, ActressID: target.ID}
	require.NoError(t, db.Create(&survivor).Error)
	require.NoError(t, db.Model(&source).Updates(map[string]interface{}{"order_pinned": true, "order_index": 9}).Error)
	require.NoError(t, db.Model(&collision).Update("user_pinned", true).Error)
	before := loadArtifactPublicationMovie(t, db, source.MovieContentID)
	require.NoError(t, db.Exec("CREATE TRIGGER pr260_delete_fault BEFORE DELETE ON movie_credits BEGIN SELECT RAISE(ABORT, 'credit delete fault'); END").Error)
	t.Cleanup(func() { require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS pr260_delete_fault").Error) })
	_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionReassign, target.ID)
	require.ErrorContains(t, err, "credit delete fault")
	var original, kept models.MovieCredit
	require.NoError(t, db.First(&original, source.ID).Error)
	require.NoError(t, db.First(&kept, survivor.ID).Error)
	require.Equal(t, source.ActressID, original.ActressID)
	require.True(t, original.OrderPinned)
	require.Equal(t, 9, original.OrderIndex)
	require.False(t, kept.OrderPinned)
	var pinned models.CreditCollision
	require.NoError(t, db.First(&pinned, collision.ID).Error)
	require.Equal(t, source.ID, pinned.CreditID)
	require.True(t, pinned.UserPinned)
	require.Equal(t, models.CollisionStatusOpen, pinned.Status)
	after := loadArtifactPublicationMovie(t, db, source.MovieContentID)
	require.Equal(t, before.RenderGeneration, after.RenderGeneration)
	require.Equal(t, before.RenderDirty, after.RenderDirty)
}
