package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMovieTagRepository_AddTag(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewMovieTagRepository(db)

	// Test adding tag
	err = repo.AddTag(context.TODO(), "IPX-535", "Favorite")
	assert.NoError(t, err)

	// Test duplicate (should error due to UNIQUE constraint)
	err = repo.AddTag(context.TODO(), "IPX-535", "Favorite")
	assert.Error(t, err, "Adding duplicate tag should fail")

	// Test different tag same movie
	err = repo.AddTag(context.TODO(), "IPX-535", "Watched")
	assert.NoError(t, err)

	// Test same tag different movie
	err = repo.AddTag(context.TODO(), "ABC-123", "Favorite")
	assert.NoError(t, err)
}

func TestMovieTagRepository_GetTagsForMovie(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewMovieTagRepository(db)

	// Add tags
	_ = repo.AddTag(context.TODO(), "IPX-535", "Favorite")
	_ = repo.AddTag(context.TODO(), "IPX-535", "Watched")
	_ = repo.AddTag(context.TODO(), "IPX-535", "Uncensored")

	// Get tags
	tags, err := repo.GetTagsForMovie(context.TODO(), "IPX-535")
	require.NoError(t, err)
	assert.Len(t, tags, 3)
	assert.Contains(t, tags, "Favorite")
	assert.Contains(t, tags, "Watched")
	assert.Contains(t, tags, "Uncensored")

	// Test movie with no tags
	tags, err = repo.GetTagsForMovie(context.TODO(), "NONEXISTENT")
	require.NoError(t, err)
	assert.Len(t, tags, 0)
}

func TestMovieTagRepository_RemoveTag(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewMovieTagRepository(db)

	// Add and remove tag
	_ = repo.AddTag(context.TODO(), "IPX-535", "Favorite")
	_ = repo.AddTag(context.TODO(), "IPX-535", "Watched")

	err = repo.RemoveTag(context.TODO(), "IPX-535", "Favorite")
	assert.NoError(t, err)

	tags, _ := repo.GetTagsForMovie(context.TODO(), "IPX-535")
	assert.Len(t, tags, 1)
	assert.Contains(t, tags, "Watched")
	assert.NotContains(t, tags, "Favorite")

	// Remove non-existent tag (should not error)
	err = repo.RemoveTag(context.TODO(), "IPX-535", "NonExistent")
	assert.NoError(t, err)
}

func TestMovieTagRepository_RemoveAllTags(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewMovieTagRepository(db)

	// Add multiple tags
	_ = repo.AddTag(context.TODO(), "IPX-535", "Favorite")
	_ = repo.AddTag(context.TODO(), "IPX-535", "Watched")
	_ = repo.AddTag(context.TODO(), "IPX-535", "Uncensored")

	// Remove all tags
	err = repo.RemoveAllTags(context.TODO(), "IPX-535")
	assert.NoError(t, err)

	tags, _ := repo.GetTagsForMovie(context.TODO(), "IPX-535")
	assert.Len(t, tags, 0)

	// Remove from movie with no tags (should not error)
	err = repo.RemoveAllTags(context.TODO(), "NONEXISTENT")
	assert.NoError(t, err)
}

func TestMovieTagRepository_GetMoviesWithTag(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewMovieTagRepository(db)

	// Add same tag to multiple movies
	_ = repo.AddTag(context.TODO(), "IPX-535", "Favorite")
	_ = repo.AddTag(context.TODO(), "ABC-123", "Favorite")
	_ = repo.AddTag(context.TODO(), "XYZ-789", "Favorite")
	_ = repo.AddTag(context.TODO(), "ABC-123", "Watched")

	// Search
	movies, err := repo.GetMoviesWithTag(context.TODO(), "Favorite")
	require.NoError(t, err)
	assert.Len(t, movies, 3)
	assert.Contains(t, movies, "IPX-535")
	assert.Contains(t, movies, "ABC-123")
	assert.Contains(t, movies, "XYZ-789")

	// Search for tag with one movie
	movies, err = repo.GetMoviesWithTag(context.TODO(), "Watched")
	require.NoError(t, err)
	assert.Len(t, movies, 1)
	assert.Contains(t, movies, "ABC-123")

	// Search for non-existent tag
	movies, err = repo.GetMoviesWithTag(context.TODO(), "NonExistent")
	require.NoError(t, err)
	assert.Len(t, movies, 0)
}

func TestMovieTagRepository_ListAll(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewMovieTagRepository(db)

	// Add tags for multiple movies
	_ = repo.AddTag(context.TODO(), "IPX-535", "Favorite")
	_ = repo.AddTag(context.TODO(), "IPX-535", "Watched")
	_ = repo.AddTag(context.TODO(), "ABC-123", "Favorite")
	_ = repo.AddTag(context.TODO(), "ABC-123", "Uncensored")

	// List all
	allTags, err := repo.ListAll(context.TODO())
	require.NoError(t, err)
	assert.Len(t, allTags, 2) // Two movies

	// Check IPX-535 tags
	assert.Len(t, allTags["IPX-535"], 2)
	assert.Contains(t, allTags["IPX-535"], "Favorite")
	assert.Contains(t, allTags["IPX-535"], "Watched")

	// Check ABC-123 tags
	assert.Len(t, allTags["ABC-123"], 2)
	assert.Contains(t, allTags["ABC-123"], "Favorite")
	assert.Contains(t, allTags["ABC-123"], "Uncensored")
}

func TestMovieTagRepository_GetUniqueTagsList(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewMovieTagRepository(db)

	// Add duplicate tags across movies
	_ = repo.AddTag(context.TODO(), "IPX-535", "Favorite")
	_ = repo.AddTag(context.TODO(), "ABC-123", "Favorite")
	_ = repo.AddTag(context.TODO(), "IPX-535", "Watched")
	_ = repo.AddTag(context.TODO(), "XYZ-789", "Uncensored")

	// Get unique tags
	tags, err := repo.GetUniqueTagsList(context.TODO())
	require.NoError(t, err)
	assert.Len(t, tags, 3) // Favorite, Watched, Uncensored
	assert.Contains(t, tags, "Favorite")
	assert.Contains(t, tags, "Watched")
	assert.Contains(t, tags, "Uncensored")

	// Test empty database
	_ = repo.RemoveAllTags(context.TODO(), "IPX-535")
	_ = repo.RemoveAllTags(context.TODO(), "ABC-123")
	_ = repo.RemoveAllTags(context.TODO(), "XYZ-789")

	tags, err = repo.GetUniqueTagsList(context.TODO())
	require.NoError(t, err)
	assert.Len(t, tags, 0)
}

func TestMovieTagRepository_CaseSensitivity(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewMovieTagRepository(db)

	// Add tags with different case
	err = repo.AddTag(context.TODO(), "IPX-535", "Favorite")
	assert.NoError(t, err)

	err = repo.AddTag(context.TODO(), "IPX-535", "favorite")
	assert.NoError(t, err, "Tags should be case-sensitive")

	err = repo.AddTag(context.TODO(), "IPX-535", "FAVORITE")
	assert.NoError(t, err, "Tags should be case-sensitive")

	// All three should exist
	tags, _ := repo.GetTagsForMovie(context.TODO(), "IPX-535")
	assert.Len(t, tags, 3)
	assert.Contains(t, tags, "Favorite")
	assert.Contains(t, tags, "favorite")
	assert.Contains(t, tags, "FAVORITE")
}

func TestMovieTagRepository_TagsWithSpaces(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewMovieTagRepository(db)

	// Add tags with spaces
	err = repo.AddTag(context.TODO(), "IPX-535", "Best of 2023")
	assert.NoError(t, err)

	err = repo.AddTag(context.TODO(), "IPX-535", "Collection: Summer Movies")
	assert.NoError(t, err)

	tags, _ := repo.GetTagsForMovie(context.TODO(), "IPX-535")
	assert.Len(t, tags, 2)
	assert.Contains(t, tags, "Best of 2023")
	assert.Contains(t, tags, "Collection: Summer Movies")

	// Search should work with spaces
	movies, _ := repo.GetMoviesWithTag(context.TODO(), "Best of 2023")
	assert.Len(t, movies, 1)
	assert.Contains(t, movies, "IPX-535")
}
