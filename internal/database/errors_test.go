package database

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestErrNotFound(t *testing.T) {
	t.Run("sentinel is self-equal", func(t *testing.T) {
		assert.True(t, errors.Is(ErrNotFound, ErrNotFound))
	})

	t.Run("wrapped sentinel is found by errors.Is", func(t *testing.T) {
		wrapped := fmt.Errorf("find movie by id abc: %w", ErrNotFound)
		assert.True(t, errors.Is(wrapped, ErrNotFound))
	})
}

func TestIsNotFound(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"ErrNotFound sentinel", ErrNotFound, true},
		{"wrapped ErrNotFound", fmt.Errorf("context: %w", ErrNotFound), true},
		{"gorm ErrRecordNotFound", gorm.ErrRecordNotFound, true},
		{"wrapped gorm ErrRecordNotFound", fmt.Errorf("context: %w", gorm.ErrRecordNotFound), true},
		{"unrelated error", errors.New("something else"), false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, IsNotFound(tc.err))
		})
	}
}

func TestFindByID_ErrNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: dbPath},
	}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	repo := NewMovieRepository(db)

	t.Run("returns ErrNotFound for missing movie", func(t *testing.T) {
		_, err := repo.FindByID("NONEXISTENT-999")
		assert.Error(t, err)
		assert.True(t, IsNotFound(err), "error should be ErrNotFound, got: %v", err)
		assert.True(t, errors.Is(err, ErrNotFound), "errors.Is should match ErrNotFound")
	})
}

func TestFindByContentID_ErrNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: dbPath},
	}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	repo := NewMovieRepository(db)

	t.Run("returns ErrNotFound for missing movie", func(t *testing.T) {
		_, err := repo.FindByContentID("nonexistent999")
		assert.Error(t, err)
		assert.True(t, IsNotFound(err), "error should be ErrNotFound, got: %v", err)
		assert.True(t, errors.Is(err, ErrNotFound), "errors.Is should match ErrNotFound")
	})

	t.Run("existing movie returns no error", func(t *testing.T) {
		movie := &models.Movie{
			ID:        "FINDTEST-001",
			ContentID: "findtest001",
			Title:     "Test Movie",
		}
		require.NoError(t, repo.Create(movie))

		found, err := repo.FindByContentID("findtest001")
		assert.NoError(t, err)
		assert.Equal(t, "findtest001", found.ContentID)
	})
}
