package database

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMovieRepoV5(t *testing.T) *MovieRepository {
	t.Helper()
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"}
	db, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	if err := db.RunMigrationsOnStartup(context.Background()); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return NewMovieRepository(db)
}

func TestMovieRepo_V5_MovieEntityID_ContentID(t *testing.T) {
	movie := &models.Movie{ID: "ABC-123", ContentID: "abc123"}
	id := movieEntityID(movie)
	assert.Equal(t, "abc123", id)
}

func TestMovieRepo_V5_MovieEntityID_NoContentID(t *testing.T) {
	movie := &models.Movie{ID: "ABC-123"}
	id := movieEntityID(movie)
	assert.Equal(t, "ABC-123", id)
}

func TestMovieRepo_V5_CreateAndFind(t *testing.T) {
	repo := setupMovieRepoV5(t)
	ctx := context.Background()

	movie := &models.Movie{
		ID:          "ABC-123",
		ContentID:   "abc123",
		Title:       "Test Movie",
		Maker:       "Test Maker",
		Description: "A test movie",
	}

	err := repo.Create(ctx, movie)
	require.NoError(t, err)

	found, err := repo.FindByID(ctx, "ABC-123")
	require.NoError(t, err)
	assert.Equal(t, "Test Movie", found.Title)
	assert.Equal(t, "Test Maker", found.Maker)
}

func TestMovieRepo_V5_FindByContentID(t *testing.T) {
	repo := setupMovieRepoV5(t)
	ctx := context.Background()

	movie := &models.Movie{
		ID:        "ABC-123",
		ContentID: "abc123",
		Title:     "Test Movie",
	}

	err := repo.Create(ctx, movie)
	require.NoError(t, err)

	found, err := repo.FindByContentID(ctx, "abc123")
	require.NoError(t, err)
	assert.Equal(t, "ABC-123", found.ID)
}

func TestMovieRepo_V5_FindByContentID_NotFound(t *testing.T) {
	repo := setupMovieRepoV5(t)
	ctx := context.Background()

	_, err := repo.FindByContentID(ctx, "nonexistent")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMovieRepo_V5_Delete(t *testing.T) {
	repo := setupMovieRepoV5(t)
	ctx := context.Background()

	movie := &models.Movie{
		ID:        "ABC-123",
		ContentID: "abc123",
		Title:     "Test Movie",
	}

	err := repo.Create(ctx, movie)
	require.NoError(t, err)

	err = repo.Delete(ctx, "ABC-123")
	require.NoError(t, err)

	_, err = repo.FindByID(ctx, "ABC-123")
	assert.Error(t, err)
}

func TestMovieRepo_V5_DeleteNonExistent(t *testing.T) {
	repo := setupMovieRepoV5(t)
	ctx := context.Background()

	err := repo.Delete(ctx, "nonexistent")
	assert.NoError(t, err) // Delete is idempotent
}

func TestMovieRepo_V5_Update(t *testing.T) {
	repo := setupMovieRepoV5(t)
	ctx := context.Background()

	movie := &models.Movie{
		ID:        "ABC-123",
		ContentID: "abc123",
		Title:     "Original Title",
	}

	err := repo.Create(ctx, movie)
	require.NoError(t, err)

	movie.Title = "Updated Title"
	err = repo.Update(ctx, movie)
	require.NoError(t, err)

	found, err := repo.FindByID(ctx, "ABC-123")
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", found.Title)
}

func TestMovieRepo_V5_ListPagination(t *testing.T) {
	repo := setupMovieRepoV5(t)
	ctx := context.Background()

	// Create multiple movies
	for i := 0; i < 5; i++ {
		movie := &models.Movie{
			ID:        fmt.Sprintf("MOV-%03d", i),
			ContentID: fmt.Sprintf("mov%03d", i),
			Title:     fmt.Sprintf("Movie %d", i),
		}
		err := repo.Create(ctx, movie)
		require.NoError(t, err)
	}

	// Get first page
	movies, err := repo.List(ctx, 2, 0)
	require.NoError(t, err)
	assert.Len(t, movies, 2)

	// Get second page
	movies, err = repo.List(ctx, 2, 2)
	require.NoError(t, err)
	assert.Len(t, movies, 2)
}

func TestMovieRepo_V5_ListEmptyDB(t *testing.T) {
	repo := setupMovieRepoV5(t)
	ctx := context.Background()

	movies, err := repo.List(ctx, 10, 0)
	require.NoError(t, err)
	assert.Empty(t, movies)
}

func TestMovieRepo_V5_Upsert_NewMovie(t *testing.T) {
	repo := setupMovieRepoV5(t)
	ctx := context.Background()

	movie := &models.Movie{
		ID:        "ABC-123",
		ContentID: "abc123",
		Title:     "Test Movie",
	}

	result, err := repo.Upsert(ctx, movie)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "ABC-123", result.ID)
}

func TestMovieRepo_V5_Upsert_ExistingMovie(t *testing.T) {
	repo := setupMovieRepoV5(t)
	ctx := context.Background()

	movie := &models.Movie{
		ID:        "ABC-123",
		ContentID: "abc123",
		Title:     "Original",
	}

	_, err := repo.Upsert(ctx, movie)
	require.NoError(t, err)

	movie.Title = "Updated"
	result, err := repo.Upsert(ctx, movie)
	require.NoError(t, err)
	assert.Equal(t, "Updated", result.Title)
}

func TestMovieRepo_V5_FilterIdentifiableActresses(t *testing.T) {
	actresses := []models.Actress{
		{JapaneseName: "Test1", DMMID: 100},
		{}, // Empty actress should be filtered
		{JapaneseName: "Test2"},
	}

	filtered := filterIdentifiableActresses(actresses)
	assert.Len(t, filtered, 2)
}

func TestNewMovieRepository_V5(t *testing.T) {
	repo := setupMovieRepoV5(t)
	assert.NotNil(t, repo)
}

func TestMovieRepo_V5_UpsertWithGenres(t *testing.T) {
	repo := setupMovieRepoV5(t)
	ctx := context.Background()

	movie := &models.Movie{
		ID:        "ABC-123",
		ContentID: "abc123",
		Title:     "Test Movie",
		Genres: []models.Genre{
			{Name: "Action"},
			{Name: "Drama"},
		},
	}

	result, err := repo.Upsert(ctx, movie)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Genres, 2)
}

func TestMovieRepo_V5_UpsertWithActresses(t *testing.T) {
	repo := setupMovieRepoV5(t)
	ctx := context.Background()

	movie := &models.Movie{
		ID:        "ABC-123",
		ContentID: "abc123",
		Title:     "Test Movie",
		Actresses: []models.Actress{
			{JapaneseName: "Test Actress", DMMID: 100},
		},
	}

	result, err := repo.Upsert(ctx, movie)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Actresses, 1)
}
