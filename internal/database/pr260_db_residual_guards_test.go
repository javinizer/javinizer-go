package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPR260ApplyPublicationCreditLookupFailureSkipsPublisher(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	seedArtifactPublicationMovie(t, db, "credit-lookup", 2, true)
	require.NoError(t, db.Exec("DROP TABLE movie_credits").Error)
	called := false
	err := NewMovieRepository(db).WithApplyPublicationFence(context.Background(), "credit-lookup", 2, func(*models.Movie) error { called = true; return nil })
	require.ErrorContains(t, err, "movie_credits")
	require.False(t, called)
	saved := loadArtifactPublicationMovie(t, db, "credit-lookup")
	require.True(t, saved.RenderDirty)
	require.EqualValues(t, 2, saved.RenderGeneration)
}

func TestPR260RelinkDirectDirtyWriteFailureRollsBackIdentity(t *testing.T) {
	db, service, source, collision := collisionFixture(t)
	target := models.Actress{FirstName: "Reported", LastName: "Person", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", source.MovieContentID, source.ActressID).Error)
	require.NoError(t, db.Model(&collision).Update("user_pinned", true).Error)
	before := loadArtifactPublicationMovie(t, db, source.MovieContentID)
	require.NoError(t, db.Exec("CREATE TRIGGER pr260_relink_dirty_fault BEFORE UPDATE OF render_generation ON movies BEGIN SELECT RAISE(ABORT, 'relink dirty fault'); END").Error)
	t.Cleanup(func() { require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS pr260_relink_dirty_fault").Error) })
	_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionReassign, target.ID)
	require.ErrorContains(t, err, "relink dirty fault")
	var kept models.MovieCredit
	require.NoError(t, db.First(&kept, source.ID).Error)
	require.Equal(t, source.ActressID, kept.ActressID)
	var pinned models.CreditCollision
	require.NoError(t, db.First(&pinned, collision.ID).Error)
	require.Equal(t, source.ID, pinned.CreditID)
	require.True(t, pinned.UserPinned)
	require.Equal(t, models.CollisionStatusOpen, pinned.Status)
	var ids []uint
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", source.MovieContentID).Pluck("actress_id", &ids).Error)
	require.Equal(t, []uint{source.ActressID}, ids)
	after := loadArtifactPublicationMovie(t, db, source.MovieContentID)
	require.Equal(t, before.RenderGeneration, after.RenderGeneration)
	require.Equal(t, before.RenderDirty, after.RenderDirty)
}
