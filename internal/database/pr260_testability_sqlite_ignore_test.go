package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// SQLite RAISE(IGNORE) silently skips an UPDATE without a driver error; the
// repository must treat that real RowsAffected=0 as non-publication, not success.
func TestPR260SQLiteIgnoredUpdatesRemainFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name, trigger              string
		artifact, dirty, published bool
		want                       error
	}{
		{"apply lock", "BEFORE UPDATE OF render_generation ON movies", false, true, false, ErrNotFound},
		{"artifact lock", "BEFORE UPDATE OF render_generation ON movies", true, true, false, ErrNotFound},
		{"admission", "BEFORE UPDATE OF render_dirty ON movies", true, false, false, ErrApplyPublicationStale},
		{"clean", "BEFORE UPDATE OF render_dirty ON movies", true, true, true, ErrApplyPublicationStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupBaseRepoTestDB(t)
			seedArtifactPublicationMovie(t, db, "ignored-movie", 4, tc.dirty)
			before := loadArtifactPublicationMovie(t, db, "ignored-movie")
			require.NoError(t, db.Exec("CREATE TRIGGER pr260_ignore "+tc.trigger+" BEGIN SELECT RAISE(IGNORE); END").Error)
			t.Cleanup(func() { require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS pr260_ignore").Error) })
			called := false
			publish := func(*models.Movie) error { called = true; return nil }
			repo := NewMovieRepository(db)
			var err error
			if tc.artifact {
				err = repo.WithApplyArtifactPublicationFence(context.Background(), "ignored-movie", 4, publish)
			} else {
				err = repo.WithApplyPublicationFence(context.Background(), "ignored-movie", 4, publish)
			}
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.published, called)
			saved := loadArtifactPublicationMovie(t, db, "ignored-movie")
			require.Equal(t, before.RenderGeneration, saved.RenderGeneration)
			require.Equal(t, before.RenderDirty, saved.RenderDirty)
		})
	}
}

func TestPR260SQLiteIgnoredCreditSuppressionDoesNotMutateAuthority(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	before := loadArtifactPublicationMovie(t, db, credit.MovieContentID)
	require.NoError(t, db.Exec("CREATE TRIGGER pr260_ignore_credit BEFORE UPDATE OF suppressed ON movie_credits BEGIN SELECT RAISE(IGNORE); END").Error)
	t.Cleanup(func() { require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS pr260_ignore_credit").Error) })
	err := service.SetCreditSuppressed(context.Background(), credit.ID, true)
	require.ErrorIs(t, err, ErrNotFound)
	var saved models.MovieCredit
	require.NoError(t, db.First(&saved, credit.ID).Error)
	require.False(t, saved.Suppressed)
	require.Equal(t, credit.ActressID, saved.ActressID)
	var stored models.CreditCollision
	require.NoError(t, db.First(&stored, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, stored.Status)
	after := loadArtifactPublicationMovie(t, db, credit.MovieContentID)
	require.Equal(t, before.RenderGeneration, after.RenderGeneration)
	require.Equal(t, before.RenderDirty, after.RenderDirty)
}
