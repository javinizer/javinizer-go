package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMovieTagRepository_AddTag(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())

	repo := NewMovieTagRepository(db)

	// Test adding tag
	err = repo.AddTag("IPX-535", "Favorite")
	assert.NoError(t, err)

	// Test duplicate (should error due to UNIQUE constraint)
	err = repo.AddTag("IPX-535", "Favorite")
	assert.Error(t, err, "Adding duplicate tag should fail")

	// Test different tag same movie
	err = repo.AddTag("IPX-535", "Watched")
	assert.NoError(t, err)

	// Test same tag different movie
	err = repo.AddTag("ABC-123", "Favorite")
	assert.NoError(t, err)
}

func TestMovieTagRepository_GetTagsForMovie(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())

	repo := NewMovieTagRepository(db)

	// Add tags
	_ = repo.AddTag("IPX-535", "Favorite")
	_ = repo.AddTag("IPX-535", "Watched")
	_ = repo.AddTag("IPX-535", "Uncensored")

	// Get tags
	tags, err := repo.GetTagsForMovie("IPX-535")
	require.NoError(t, err)
	assert.Len(t, tags, 3)
	assert.Contains(t, tags, "Favorite")
	assert.Contains(t, tags, "Watched")
	assert.Contains(t, tags, "Uncensored")

	// Test movie with no tags
	tags, err = repo.GetTagsForMovie("NONEXISTENT")
	require.NoError(t, err)
	assert.Len(t, tags, 0)
}

func TestMovieTagRepository_RemoveTag(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())

	repo := NewMovieTagRepository(db)

	// Add and remove tag
	_ = repo.AddTag("IPX-535", "Favorite")
	_ = repo.AddTag("IPX-535", "Watched")

	err = repo.RemoveTag("IPX-535", "Favorite")
	assert.NoError(t, err)

	tags, _ := repo.GetTagsForMovie("IPX-535")
	assert.Len(t, tags, 1)
	assert.Contains(t, tags, "Watched")
	assert.NotContains(t, tags, "Favorite")

	// Remove non-existent tag (should not error)
	err = repo.RemoveTag("IPX-535", "NonExistent")
	assert.NoError(t, err)
}

func TestMovieTagRepository_RemoveAllTags(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())

	repo := NewMovieTagRepository(db)

	// Add multiple tags
	_ = repo.AddTag("IPX-535", "Favorite")
	_ = repo.AddTag("IPX-535", "Watched")
	_ = repo.AddTag("IPX-535", "Uncensored")

	// Remove all tags
	err = repo.RemoveAllTags("IPX-535")
	assert.NoError(t, err)

	tags, _ := repo.GetTagsForMovie("IPX-535")
	assert.Len(t, tags, 0)

	// Remove from movie with no tags (should not error)
	err = repo.RemoveAllTags("NONEXISTENT")
	assert.NoError(t, err)
}

func TestMovieTagRepository_GetMoviesWithTag(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())

	repo := NewMovieTagRepository(db)

	// Add same tag to multiple movies
	_ = repo.AddTag("IPX-535", "Favorite")
	_ = repo.AddTag("ABC-123", "Favorite")
	_ = repo.AddTag("XYZ-789", "Favorite")
	_ = repo.AddTag("ABC-123", "Watched")

	// Search
	movies, err := repo.GetMoviesWithTag("Favorite")
	require.NoError(t, err)
	assert.Len(t, movies, 3)
	assert.Contains(t, movies, "IPX-535")
	assert.Contains(t, movies, "ABC-123")
	assert.Contains(t, movies, "XYZ-789")

	// Search for tag with one movie
	movies, err = repo.GetMoviesWithTag("Watched")
	require.NoError(t, err)
	assert.Len(t, movies, 1)
	assert.Contains(t, movies, "ABC-123")

	// Search for non-existent tag
	movies, err = repo.GetMoviesWithTag("NonExistent")
	require.NoError(t, err)
	assert.Len(t, movies, 0)
}

func TestMovieTagRepository_ListAll(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())

	repo := NewMovieTagRepository(db)

	// Add tags for multiple movies
	_ = repo.AddTag("IPX-535", "Favorite")
	_ = repo.AddTag("IPX-535", "Watched")
	_ = repo.AddTag("ABC-123", "Favorite")
	_ = repo.AddTag("ABC-123", "Uncensored")

	// List all
	allTags, err := repo.ListAll()
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
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())

	repo := NewMovieTagRepository(db)

	// Add duplicate tags across movies
	_ = repo.AddTag("IPX-535", "Favorite")
	_ = repo.AddTag("ABC-123", "Favorite")
	_ = repo.AddTag("IPX-535", "Watched")
	_ = repo.AddTag("XYZ-789", "Uncensored")

	// Get unique tags
	tags, err := repo.GetUniqueTagsList()
	require.NoError(t, err)
	assert.Len(t, tags, 3) // Favorite, Watched, Uncensored
	assert.Contains(t, tags, "Favorite")
	assert.Contains(t, tags, "Watched")
	assert.Contains(t, tags, "Uncensored")

	// Test empty database
	_ = repo.RemoveAllTags("IPX-535")
	_ = repo.RemoveAllTags("ABC-123")
	_ = repo.RemoveAllTags("XYZ-789")

	tags, err = repo.GetUniqueTagsList()
	require.NoError(t, err)
	assert.Len(t, tags, 0)
}

func TestMovieTagRepository_CaseSensitivity(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())

	repo := NewMovieTagRepository(db)

	// Add tags with different case
	err = repo.AddTag("IPX-535", "Favorite")
	assert.NoError(t, err)

	err = repo.AddTag("IPX-535", "favorite")
	assert.NoError(t, err, "Tags should be case-sensitive")

	err = repo.AddTag("IPX-535", "FAVORITE")
	assert.NoError(t, err, "Tags should be case-sensitive")

	// All three should exist
	tags, _ := repo.GetTagsForMovie("IPX-535")
	assert.Len(t, tags, 3)
	assert.Contains(t, tags, "Favorite")
	assert.Contains(t, tags, "favorite")
	assert.Contains(t, tags, "FAVORITE")
}

func TestMovieTagRepository_TagsWithSpaces(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())

	repo := NewMovieTagRepository(db)

	// Add tags with spaces
	err = repo.AddTag("IPX-535", "Best of 2023")
	assert.NoError(t, err)

	err = repo.AddTag("IPX-535", "Collection: Summer Movies")
	assert.NoError(t, err)

	tags, _ := repo.GetTagsForMovie("IPX-535")
	assert.Len(t, tags, 2)
	assert.Contains(t, tags, "Best of 2023")
	assert.Contains(t, tags, "Collection: Summer Movies")

	// Search should work with spaces
	movies, _ := repo.GetMoviesWithTag("Best of 2023")
	assert.Len(t, movies, 1)
	assert.Contains(t, movies, "IPX-535")
}
