package database

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPR260ArtifactFenceRefusesInvalidAndMissingMoviesWithoutCallback(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	fencer := artifactPublicationFencer(t, db)
	called := false
	callback := func(*models.Movie) error { called = true; return nil }
	for _, tc := range []struct {
		name, id string
		fn       func(*models.Movie) error
		want     error
	}{
		{"empty id", " ", callback, nil},
		{"nil callback", "missing", nil, nil},
		{"missing movie", "missing", callback, ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := fencer.WithApplyArtifactPublicationFence(context.Background(), tc.id, 1, tc.fn)
			require.Error(t, err)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			}
			require.False(t, called)
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, fencer.WithApplyArtifactPublicationFence(ctx, "missing", 1, callback), context.Canceled)
	require.False(t, called)
}

func TestPR260ArtifactFenceRejectsCallbackGenerationChangeAndKeepsDirty(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	const id = "artifact-mutated-generation"
	seedArtifactPublicationMovie(t, db, id, 21, false)
	err := artifactPublicationFencer(t, db).WithApplyArtifactPublicationFence(context.Background(), id, 21, func(movie *models.Movie) error {
		movie.RenderGeneration++
		return nil
	})
	require.ErrorIs(t, err, ErrApplyPublicationStale)
	persisted := loadArtifactPublicationMovie(t, db, id)
	require.True(t, persisted.RenderDirty)
	require.Equal(t, int64(21), persisted.RenderGeneration)
}

func TestPR260ResultFenceCallbackErrorPreservesDirtyAndRollback(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	const id = "result-callback-rollback"
	seedArtifactPublicationMovie(t, db, id, 23, true)
	repo := NewMovieRepository(db)
	sentinel := errors.New("publication failed")
	err := repo.WithApplyPublicationFence(context.Background(), id, 23, func(movie *models.Movie) error {
		require.Equal(t, int64(23), movie.RenderGeneration)
		movie.Title = "uncommitted"
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	persisted := loadArtifactPublicationMovie(t, db, id)
	require.True(t, persisted.RenderDirty)
	require.Equal(t, "Artifact fence movie", persisted.Title)
}
