package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMovieTranslationRepository(t *testing.T) {
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

	// Create a test movie first (translations reference movies)
	movieRepo := NewMovieRepository(db)
	movie := createTestMovie("IPX-TRANS-001")
	err = movieRepo.Create(movie)
	require.NoError(t, err)

	repo := NewMovieTranslationRepository(db)

	t.Run("Upsert creates new translation", func(t *testing.T) {
		translation := &models.MovieTranslation{
			MovieID:     "IPX-TRANS-001",
			Language:    "en",
			Title:       "English Title",
			Description: "English description",
			SourceName:  "test",
		}

		err := repo.Upsert(translation)
		require.NoError(t, err)
		assert.NotZero(t, translation.ID)
	})

	t.Run("Upsert updates existing translation", func(t *testing.T) {
		translation := &models.MovieTranslation{
			MovieID:     "IPX-TRANS-001",
			Language:    "zh",
			Title:       "Chinese Title",
			Description: "Chinese description",
		}

		// First upsert (create)
		err := repo.Upsert(translation)
		require.NoError(t, err)
		originalID := translation.ID

		// Second upsert (update)
		translation.Title = "Updated Chinese Title"
		translation.Description = "Updated Chinese description"
		err = repo.Upsert(translation)
		require.NoError(t, err)

		// Verify ID remains the same
		assert.Equal(t, originalID, translation.ID)

		// Verify update
		found, err := repo.FindByMovieAndLanguage("IPX-TRANS-001", "zh")
		require.NoError(t, err)
		assert.Equal(t, "Updated Chinese Title", found.Title)
		assert.Equal(t, "Updated Chinese description", found.Description)
	})

	t.Run("FindByMovieAndLanguage", func(t *testing.T) {
		translation := &models.MovieTranslation{
			MovieID:     "IPX-TRANS-001",
			Language:    "ja",
			Title:       "日本語タイトル",
			Description: "日本語の説明",
		}

		err := repo.Upsert(translation)
		require.NoError(t, err)

		found, err := repo.FindByMovieAndLanguage("IPX-TRANS-001", "ja")
		require.NoError(t, err)
		assert.Equal(t, "IPX-TRANS-001", found.MovieID)
		assert.Equal(t, "ja", found.Language)
		assert.Equal(t, "日本語タイトル", found.Title)
	})

	t.Run("FindByMovieAndLanguage not found", func(t *testing.T) {
		_, err := repo.FindByMovieAndLanguage("IPX-TRANS-001", "fr")
		assert.Error(t, err)
	})

	t.Run("FindAllByMovie", func(t *testing.T) {
		// Create a new movie with multiple translations
		movie2 := createTestMovie("IPX-TRANS-002")
		err := movieRepo.Create(movie2)
		require.NoError(t, err)

		translations := []*models.MovieTranslation{
			{MovieID: "IPX-TRANS-002", Language: "en", Title: "English"},
			{MovieID: "IPX-TRANS-002", Language: "zh", Title: "Chinese"},
			{MovieID: "IPX-TRANS-002", Language: "ko", Title: "Korean"},
		}

		for _, trans := range translations {
			err := repo.Upsert(trans)
			require.NoError(t, err)
		}

		// Find all translations for this movie
		results, err := repo.FindAllByMovie("IPX-TRANS-002")
		require.NoError(t, err)
		assert.Len(t, results, 3)

		// Verify languages
		languages := make(map[string]bool)
		for _, r := range results {
			languages[r.Language] = true
		}
		assert.True(t, languages["en"])
		assert.True(t, languages["zh"])
		assert.True(t, languages["ko"])
	})

	t.Run("FindAllByMovie with no translations", func(t *testing.T) {
		movie3 := createTestMovie("IPX-TRANS-003")
		err := movieRepo.Create(movie3)
		require.NoError(t, err)

		results, err := repo.FindAllByMovie("IPX-TRANS-003")
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("Delete translation", func(t *testing.T) {
		movie4 := createTestMovie("IPX-TRANS-004")
		err := movieRepo.Create(movie4)
		require.NoError(t, err)

		translation := &models.MovieTranslation{
			MovieID:  "IPX-TRANS-004",
			Language: "de",
			Title:    "German Title",
		}

		err = repo.Upsert(translation)
		require.NoError(t, err)

		// Delete
		err = repo.Delete("IPX-TRANS-004", "de")
		require.NoError(t, err)

		// Verify deletion
		_, err = repo.FindByMovieAndLanguage("IPX-TRANS-004", "de")
		assert.Error(t, err)
	})

	t.Run("Delete non-existent translation", func(t *testing.T) {
		err := repo.Delete("NONEXISTENT", "xx")
		assert.NoError(t, err, "Deleting non-existent translation should not error")
	})

	t.Run("Multiple movies with same language", func(t *testing.T) {
		// Create two movies with English translations
		movie5 := createTestMovie("IPX-TRANS-005")
		movie6 := createTestMovie("IPX-TRANS-006")
		err := movieRepo.Create(movie5)
		require.NoError(t, err)
		err = movieRepo.Create(movie6)
		require.NoError(t, err)

		trans1 := &models.MovieTranslation{
			MovieID:  "IPX-TRANS-005",
			Language: "en",
			Title:    "Movie 5 English",
		}
		trans2 := &models.MovieTranslation{
			MovieID:  "IPX-TRANS-006",
			Language: "en",
			Title:    "Movie 6 English",
		}

		err = repo.Upsert(trans1)
		require.NoError(t, err)
		err = repo.Upsert(trans2)
		require.NoError(t, err)

		// Verify each movie has its own translation
		found1, err := repo.FindByMovieAndLanguage("IPX-TRANS-005", "en")
		require.NoError(t, err)
		assert.Equal(t, "Movie 5 English", found1.Title)

		found2, err := repo.FindByMovieAndLanguage("IPX-TRANS-006", "en")
		require.NoError(t, err)
		assert.Equal(t, "Movie 6 English", found2.Title)
	})

	t.Run("Translation with all fields populated", func(t *testing.T) {
		movie7 := createTestMovie("IPX-TRANS-007")
		err := movieRepo.Create(movie7)
		require.NoError(t, err)

		translation := &models.MovieTranslation{
			MovieID:       "IPX-TRANS-007",
			Language:      "es",
			Title:         "Spanish Title",
			OriginalTitle: "Original Spanish Title",
			Description:   "Spanish description",
			Director:      "Spanish Director",
			Maker:         "Spanish Studio",
			Label:         "Spanish Label",
			Series:        "Spanish Series",
			SourceName:    "test_scraper",
		}

		err = repo.Upsert(translation)
		require.NoError(t, err)

		// Verify all fields
		found, err := repo.FindByMovieAndLanguage("IPX-TRANS-007", "es")
		require.NoError(t, err)
		assert.Equal(t, "Spanish Title", found.Title)
		assert.Equal(t, "Original Spanish Title", found.OriginalTitle)
		assert.Equal(t, "Spanish description", found.Description)
		assert.Equal(t, "Spanish Director", found.Director)
		assert.Equal(t, "Spanish Studio", found.Maker)
		assert.Equal(t, "Spanish Label", found.Label)
		assert.Equal(t, "Spanish Series", found.Series)
		assert.Equal(t, "test_scraper", found.SourceName)
	})
}
