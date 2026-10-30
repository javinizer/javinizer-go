package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestBaseRepositoryDefaultFactoryOwnsGenreLifecycle(t *testing.T) {
	db := newDatabaseTestDB(t)
	// Omitting WithNewEntity is a supported constructor default. These genres
	// have no movie associations, so deletion exercises only generic CRUD.
	repo := NewBaseRepository[models.Genre, uint](db, "genre", func(g models.Genre) string { return g.Name })
	ctx := context.Background()
	count, err := repo.Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	first := models.Genre{Name: "default-factory-first"}
	second := models.Genre{Name: "default-factory-survivor"}
	require.NoError(t, repo.Create(ctx, &first))
	require.NoError(t, repo.Create(ctx, &second))
	require.NotZero(t, first.ID)
	require.NotEqual(t, first.ID, second.ID)
	count, err = repo.Count(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	require.NoError(t, repo.Delete(ctx, first.ID))
	count, err = repo.Count(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	missing, err := repo.FindByID(ctx, first.ID)
	require.ErrorIs(t, err, ErrNotFound)
	require.Nil(t, missing)
	survivor, err := repo.FindByID(ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, second, *survivor)
}
