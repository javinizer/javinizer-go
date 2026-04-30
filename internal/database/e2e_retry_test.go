package database

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDBForE2E(t *testing.T, dbPath string) *DB {
	t.Helper()
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  dbPath,
		},
		Logging: config.LoggingConfig{Level: "error"},
	}
	db, err := New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate())
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestIsLocked_DetectsErrBusy(t *testing.T) {
	err := &sqlite3.Error{Code: sqlite3.ErrBusy}
	assert.True(t, isLocked(err))
}

func TestIsLocked_DetectsErrLocked(t *testing.T) {
	err := &sqlite3.Error{Code: sqlite3.ErrLocked}
	assert.True(t, isLocked(err))
}

func TestIsLocked_DetectsWrappedErrBusy(t *testing.T) {
	wrapped := fmt.Errorf("wrapped: %w", &sqlite3.Error{Code: sqlite3.ErrBusy})
	assert.True(t, isLocked(wrapped))
}

func TestIsLocked_DetectsDoubleWrappedErrLocked(t *testing.T) {
	inner := &sqlite3.Error{Code: sqlite3.ErrLocked}
	outer1 := fmt.Errorf("level1: %w", inner)
	outer2 := fmt.Errorf("level2: %w", outer1)
	assert.True(t, isLocked(outer2))
}

func TestIsLocked_IgnoresNonLockErrors(t *testing.T) {
	assert.False(t, isLocked(nil))
	assert.False(t, isLocked(&sqlite3.Error{Code: sqlite3.ErrNotFound}))
	assert.False(t, isLocked(fmt.Errorf("plain error")))
}

func TestIsLocked_StringFallbackDatabaseIsLocked(t *testing.T) {
	assert.True(t, isLocked(fmt.Errorf("create movie IPX-001: database is locked")))
	assert.True(t, isLocked(fmt.Errorf("save movie: database table is locked")))
	assert.False(t, isLocked(fmt.Errorf("create movie: connection refused")))
	assert.False(t, isLocked(fmt.Errorf("random database error")))
}

func TestRetryOnLocked_RetriesOnStringMatchLockError(t *testing.T) {
	var attempts int
	err := retryOnLocked(func() error {
		attempts++
		if attempts < 2 {
			return fmt.Errorf("create movie X: database is locked")
		}
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 2, attempts)
}

func TestRetryOnLocked_NoRetryOnSuccess(t *testing.T) {
	var attempts int
	err := retryOnLocked(func() error {
		attempts++
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, attempts)
}

func TestRetryOnLocked_RetriesOnLockAndSucceeds(t *testing.T) {
	var attempts int
	lockErr := &sqlite3.Error{Code: sqlite3.ErrBusy}
	err := retryOnLocked(func() error {
		attempts++
		if attempts < 3 {
			return lockErr
		}
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 3, attempts)
}

func TestRetryOnLocked_ExhaustsRetries(t *testing.T) {
	var attempts int
	lockErr := &sqlite3.Error{Code: sqlite3.ErrBusy}
	err := retryOnLocked(func() error {
		attempts++
		return lockErr
	})
	assert.Error(t, err)
	assert.Equal(t, defaultLockRetries, attempts)
}

func TestRetryOnLocked_ReturnsImmediatelyOnNonLockError(t *testing.T) {
	var attempts int
	otherErr := &sqlite3.Error{Code: sqlite3.ErrNotFound}
	err := retryOnLocked(func() error {
		attempts++
		return otherErr
	})
	assert.Error(t, err)
	assert.Equal(t, 1, attempts)
}

func TestUpsert_StateIntegrityAfterSuccessiveUpserts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping state integrity E2E")
	}

	tmpDir := t.TempDir()
	db := newDBForE2E(t, filepath.Join(tmpDir, "test.db"))
	repo := NewMovieRepository(db)

	ids := []string{"E2E-100", "E2E-200", "E2E-300"}
	for i := 0; i < 30; i++ {
		id := ids[i%len(ids)]
		genre := fmt.Sprintf("Genre%02d", i%10)

		movie, err := repo.Upsert(&models.Movie{
			ID:     id,
			Title:  "E2E State Test",
			Genres: []models.Genre{{Name: genre}},
			Translations: []models.MovieTranslation{
				{Language: "en", Title: fmt.Sprintf("Iteration %d", i)},
			},
		})
		require.NoError(t, err, "iteration %d should succeed", i)
		require.NotNil(t, movie)

		assert.Equal(t, strings.ToLower(strings.ReplaceAll(id, "-", "")), movie.ContentID, "iteration %d: ContentID mismatch", i)
		assert.Equal(t, genre, movie.Genres[0].Name, "iteration %d: Genre mismatch", i)
	}
}
