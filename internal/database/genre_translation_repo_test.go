package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenreTranslationRepository(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	// Create a test genre first (translations reference genres)
	genreRepo := newGenreRepository(db)
	genre, err := genreRepo.FindOrCreate(context.TODO(), "Action")
	require.NoError(t, err)

	repo := newGenreTranslationRepository(db)

	t.Run("Upsert creates new translation", func(t *testing.T) {
		translation := &models.GenreTranslation{
			GenreID:    genre.ID,
			Language:   "en",
			Name:       "Action",
			SourceName: "test",
		}

		err := repo.Upsert(context.TODO(), translation)
		require.NoError(t, err)
		assert.NotZero(t, translation.ID)
	})

	t.Run("Upsert updates existing translation", func(t *testing.T) {
		translation := &models.GenreTranslation{
			GenreID:  genre.ID,
			Language: "zh",
			Name:     "动作",
		}

		// First upsert (create)
		err := repo.Upsert(context.TODO(), translation)
		require.NoError(t, err)
		originalID := translation.ID

		// Second upsert (update)
		translation.Name = "动作片"
		err = repo.Upsert(context.TODO(), translation)
		require.NoError(t, err)

		// Verify ID remains the same (dedup by genre_id+language)
		assert.Equal(t, originalID, translation.ID)

		// Verify update
		found, err := repo.FindByGenreAndLanguage(context.TODO(), genre.ID, "zh")
		require.NoError(t, err)
		assert.Equal(t, "动作片", found.Name)
	})

	t.Run("FindByGenreAndLanguage", func(t *testing.T) {
		translation := &models.GenreTranslation{
			GenreID:  genre.ID,
			Language: "ja",
			Name:     "アクション",
		}

		err := repo.Upsert(context.TODO(), translation)
		require.NoError(t, err)

		found, err := repo.FindByGenreAndLanguage(context.TODO(), genre.ID, "ja")
		require.NoError(t, err)
		assert.Equal(t, genre.ID, found.GenreID)
		assert.Equal(t, "ja", found.Language)
		assert.Equal(t, "アクション", found.Name)
	})

	t.Run("FindByGenreAndLanguage not found", func(t *testing.T) {
		_, err := repo.FindByGenreAndLanguage(context.TODO(), genre.ID, "fr")
		assert.Error(t, err)
	})

	t.Run("FindAllByGenre", func(t *testing.T) {
		// Create a new genre with multiple translations
		genre2, err := genreRepo.FindOrCreate(context.TODO(), "Comedy")
		require.NoError(t, err)

		translations := []*models.GenreTranslation{
			{GenreID: genre2.ID, Language: "en", Name: "Comedy"},
			{GenreID: genre2.ID, Language: "zh", Name: "喜剧"},
			{GenreID: genre2.ID, Language: "ko", Name: "코미디"},
		}

		for _, trans := range translations {
			err := repo.Upsert(context.TODO(), trans)
			require.NoError(t, err)
		}

		// Find all translations for this genre
		results, err := repo.FindAllByGenre(context.TODO(), genre2.ID)
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

	t.Run("FindAllByGenre with no translations", func(t *testing.T) {
		genre3, err := genreRepo.FindOrCreate(context.TODO(), "Horror")
		require.NoError(t, err)

		results, err := repo.FindAllByGenre(context.TODO(), genre3.ID)
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("Delete translation", func(t *testing.T) {
		genre4, err := genreRepo.FindOrCreate(context.TODO(), "Drama")
		require.NoError(t, err)

		translation := &models.GenreTranslation{
			GenreID:  genre4.ID,
			Language: "de",
			Name:     "Drama",
		}

		err = repo.Upsert(context.TODO(), translation)
		require.NoError(t, err)

		// Delete
		err = repo.Delete(context.TODO(), genre4.ID, "de")
		require.NoError(t, err)

		// Verify deletion
		_, err = repo.FindByGenreAndLanguage(context.TODO(), genre4.ID, "de")
		assert.Error(t, err)
	})

	t.Run("Delete non-existent translation", func(t *testing.T) {
		err := repo.Delete(context.TODO(), 9999, "xx")
		assert.NoError(t, err, "Deleting non-existent translation should not error")
	})

	t.Run("Dedup by genre_id and language", func(t *testing.T) {
		genre5, err := genreRepo.FindOrCreate(context.TODO(), "Thriller")
		require.NoError(t, err)

		// Upsert same genre+language twice
		trans1 := &models.GenreTranslation{
			GenreID:    genre5.ID,
			Language:   "es",
			Name:       "Suspense",
			SourceName: "scraper_a",
		}
		err = repo.Upsert(context.TODO(), trans1)
		require.NoError(t, err)

		trans2 := &models.GenreTranslation{
			GenreID:    genre5.ID,
			Language:   "es",
			Name:       "Thriller",
			SourceName: "scraper_b",
		}
		err = repo.Upsert(context.TODO(), trans2)
		require.NoError(t, err)

		// Should be one row — the second upsert updated the existing one
		results, err := repo.FindAllByGenre(context.TODO(), genre5.ID)
		require.NoError(t, err)
		esResults := make([]models.GenreTranslation, 0)
		for _, r := range results {
			if r.Language == "es" {
				esResults = append(esResults, r)
			}
		}
		assert.Len(t, esResults, 1, "same genre_id+language should result in one row")
		assert.Equal(t, "Thriller", esResults[0].Name, "second upsert should overwrite")
		assert.Equal(t, "scraper_b", esResults[0].SourceName, "source_name should be updated")
	})

	t.Run("Multiple genres with same language", func(t *testing.T) {
		genreA, err := genreRepo.FindOrCreate(context.TODO(), "Sci-Fi")
		require.NoError(t, err)
		genreB, err := genreRepo.FindOrCreate(context.TODO(), "Romance")
		require.NoError(t, err)

		trans1 := &models.GenreTranslation{
			GenreID:  genreA.ID,
			Language: "en",
			Name:     "Science Fiction",
		}
		trans2 := &models.GenreTranslation{
			GenreID:  genreB.ID,
			Language: "en",
			Name:     "Romance",
		}

		err = repo.Upsert(context.TODO(), trans1)
		require.NoError(t, err)
		err = repo.Upsert(context.TODO(), trans2)
		require.NoError(t, err)

		// Verify each genre has its own translation
		found1, err := repo.FindByGenreAndLanguage(context.TODO(), genreA.ID, "en")
		require.NoError(t, err)
		assert.Equal(t, "Science Fiction", found1.Name)

		found2, err := repo.FindByGenreAndLanguage(context.TODO(), genreB.ID, "en")
		require.NoError(t, err)
		assert.Equal(t, "Romance", found2.Name)
	})

	t.Run("FindAllByGenre with nonexistent genre", func(t *testing.T) {
		results, err := repo.FindAllByGenre(context.TODO(), 99999)
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("FindByGenreAndLanguage with nonexistent genre", func(t *testing.T) {
		_, err := repo.FindByGenreAndLanguage(context.TODO(), 99999, "en")
		assert.Error(t, err)
	})
}
