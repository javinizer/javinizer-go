package database

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPR260PublicationMissingMovieNeverCallsPublisher(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := NewMovieRepository(db)
	for _, artifact := range []bool{false, true} {
		called := false
		publish := func(*models.Movie) error { called = true; return nil }
		var err error
		if artifact {
			err = repo.WithApplyArtifactPublicationFence(context.Background(), "deleted-after-get", 1, publish)
		} else {
			err = repo.WithApplyPublicationFence(context.Background(), "deleted-after-get", 1, publish)
		}
		require.ErrorIs(t, err, ErrNotFound)
		require.False(t, called)
	}
}

func TestPR260PublicationWriterFailuresKeepMovieAndSkipCallback(t *testing.T) {
	for _, tc := range []struct {
		name, trigger   string
		artifact, dirty bool
	}{
		{"apply-lock", "BEFORE UPDATE OF render_generation ON movies", false, true},
		{"artifact-lock", "BEFORE UPDATE OF render_generation ON movies", true, true},
		{"admission", "BEFORE UPDATE OF render_dirty ON movies", true, false},
		{"clean", "BEFORE UPDATE OF render_dirty ON movies", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupBaseRepoTestDB(t)
			seedArtifactPublicationMovie(t, db, "fault-movie", 4, tc.dirty)
			require.NoError(t, db.Exec("CREATE TRIGGER fail_publication "+tc.trigger+" BEGIN SELECT RAISE(ABORT, 'writer fault'); END").Error)
			t.Cleanup(func() { require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS fail_publication").Error) })
			called := false
			publish := func(*models.Movie) error { called = true; return nil }
			repo := NewMovieRepository(db)
			var err error
			if tc.artifact {
				err = repo.WithApplyArtifactPublicationFence(context.Background(), "fault-movie", 4, publish)
			} else {
				err = repo.WithApplyPublicationFence(context.Background(), "fault-movie", 4, publish)
			}
			require.ErrorContains(t, err, "writer fault")
			require.Equal(t, tc.name == "clean", called)
			saved := loadArtifactPublicationMovie(t, db, "fault-movie")
			require.EqualValues(t, 4, saved.RenderGeneration)
			require.Equal(t, tc.dirty, saved.RenderDirty)
		})
	}
}

func TestPR260PublicationLoadFailureNeverCallsPublisher(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{true: "artifact", false: "apply"}[artifact], func(t *testing.T) {
			db := setupBaseRepoTestDB(t)
			seedArtifactPublicationMovie(t, db, "load-fault", 2, true)
			require.NoError(t, db.Exec("DROP TABLE movie_credits").Error)
			called := false
			publish := func(*models.Movie) error { called = true; return nil }
			repo := NewMovieRepository(db)
			var err error
			if artifact {
				err = repo.WithApplyArtifactPublicationFence(context.Background(), "load-fault", 2, publish)
			} else {
				err = repo.WithApplyPublicationFence(context.Background(), "load-fault", 2, publish)
			}
			require.ErrorContains(t, err, "movie_credits")
			require.False(t, called)
			saved := loadArtifactPublicationMovie(t, db, "load-fault")
			require.True(t, saved.RenderDirty)
			require.EqualValues(t, 2, saved.RenderGeneration)
		})
	}
}

func TestPR260PublicationCallbackGenerationMutationRollsBack(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	seedArtifactPublicationMovie(t, db, "mutation", 3, true)
	repo := NewMovieRepository(db)
	called := 0
	err := repo.WithApplyArtifactPublicationFence(context.Background(), "mutation", 3, func(movie *models.Movie) error { called++; movie.RenderGeneration++; return nil })
	require.ErrorIs(t, err, ErrApplyPublicationStale)
	require.Equal(t, 1, called)
	saved := loadArtifactPublicationMovie(t, db, "mutation")
	require.EqualValues(t, 3, saved.RenderGeneration)
	require.True(t, saved.RenderDirty)
	// No writer remains held by the failed publication transaction.
	require.ErrorContains(t, repo.WithApplyArtifactPublicationFence(context.Background(), "mutation", 3, func(*models.Movie) error { return errors.New("retry stopped") }), "retry stopped")
}

func TestPR260PublicationAdmissionGuards(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := NewMovieRepository(db)
	for _, artifact := range []bool{false, true} {
		called := false
		publish := func(*models.Movie) error { called = true; return nil }
		var err error
		if artifact {
			err = repo.WithApplyArtifactPublicationFence(context.Background(), " ", 1, publish)
			require.ErrorContains(t, err, "empty content id")
			err = repo.WithApplyArtifactPublicationFence(context.Background(), "guard", 1, nil)
			require.ErrorContains(t, err, "nil publisher")
		} else {
			err = repo.WithApplyPublicationFence(context.Background(), " ", 1, publish)
			require.ErrorContains(t, err, "empty content id")
			err = repo.WithApplyPublicationFence(context.Background(), "guard", 1, nil)
			require.ErrorContains(t, err, "nil publisher")
		}
		require.False(t, called)
	}
	seedArtifactPublicationMovie(t, db, "guard", 7, false)
	called := false
	err := repo.WithApplyPublicationFence(context.Background(), "guard", 6, func(*models.Movie) error { called = true; return nil })
	require.ErrorIs(t, err, ErrApplyPublicationStale)
	require.False(t, called)
	require.False(t, loadArtifactPublicationMovie(t, db, "guard").RenderDirty)
	require.NoError(t, db.Create(&models.CreditCollision{MovieContentID: "guard", Status: models.CollisionStatusOpen, Field: models.CreditFieldCreditedName}).Error)
	err = repo.WithApplyArtifactPublicationFence(context.Background(), "guard", 7, func(*models.Movie) error { called = true; return nil })
	require.ErrorIs(t, err, ErrApplyArtifactPublicationBlocked)
	require.False(t, called)
	require.False(t, loadArtifactPublicationMovie(t, db, "guard").RenderDirty)
}

func TestPR260PublicationAdmissionCollisionQueryError(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	seedArtifactPublicationMovie(t, db, "collision-query", 4, false)
	require.NoError(t, db.Exec("DROP TABLE credit_collisions").Error)
	called := false
	err := NewMovieRepository(db).WithApplyArtifactPublicationFence(context.Background(), "collision-query", 4, func(*models.Movie) error { called = true; return nil })
	require.ErrorContains(t, err, "credit_collisions")
	require.False(t, called)
	saved := loadArtifactPublicationMovie(t, db, "collision-query")
	require.False(t, saved.RenderDirty)
	require.EqualValues(t, 4, saved.RenderGeneration)
}

func TestPR260PublicationPreloadFailureRollsBackAdmission(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	seedArtifactPublicationMovie(t, db, "preload", 5, false)
	// A present association makes the preload query authoritative, not an empty shortcut.
	require.NoError(t, db.Create(&models.MovieTranslation{MovieID: "preload", Language: "en"}).Error)
	require.NoError(t, db.Exec("DROP TABLE movie_translations").Error)
	called := false
	err := NewMovieRepository(db).WithApplyArtifactPublicationFence(context.Background(), "preload", 5, func(*models.Movie) error { called = true; return nil })
	require.ErrorContains(t, err, "movie_translations")
	require.False(t, called)
	saved := loadArtifactPublicationMovie(t, db, "preload")
	require.False(t, saved.RenderDirty)
	require.EqualValues(t, 5, saved.RenderGeneration)
}
