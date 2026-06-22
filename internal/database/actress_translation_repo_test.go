package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActressTranslationRepository(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	// Create a test actress first (translations reference actresses)
	actressRepo := NewActressRepository(db)
	actress := &models.Actress{
		DMMID:        10001,
		JapaneseName: "テスト女優",
		FirstName:    "Test",
		LastName:     "Actress",
	}
	err = actressRepo.Create(context.TODO(), actress)
	require.NoError(t, err)

	repo := newActressTranslationRepository(db)

	t.Run("Upsert creates new translation", func(t *testing.T) {
		translation := &models.ActressTranslation{
			ActressID:   actress.ID,
			Language:    "en",
			FirstName:   "Test",
			LastName:    "Actress",
			DisplayName: "Test Actress",
			SourceName:  "test",
		}

		err := repo.Upsert(context.TODO(), translation)
		require.NoError(t, err)
		assert.NotZero(t, translation.ID)
	})

	t.Run("Upsert updates existing translation", func(t *testing.T) {
		translation := &models.ActressTranslation{
			ActressID:   actress.ID,
			Language:    "zh",
			FirstName:   "测试",
			LastName:    "女优",
			DisplayName: "测试女优",
		}

		// First upsert (create)
		err := repo.Upsert(context.TODO(), translation)
		require.NoError(t, err)
		originalID := translation.ID

		// Second upsert (update)
		translation.DisplayName = "更新女优"
		err = repo.Upsert(context.TODO(), translation)
		require.NoError(t, err)

		// Verify ID remains the same (dedup by actress_id+language)
		assert.Equal(t, originalID, translation.ID)

		// Verify update
		found, err := repo.FindByActressAndLanguage(context.TODO(), actress.ID, "zh")
		require.NoError(t, err)
		assert.Equal(t, "更新女优", found.DisplayName)
	})

	t.Run("FindByActressAndLanguage", func(t *testing.T) {
		translation := &models.ActressTranslation{
			ActressID:   actress.ID,
			Language:    "ja",
			FirstName:   "テスト",
			LastName:    "女優",
			DisplayName: "テスト女優",
		}

		err := repo.Upsert(context.TODO(), translation)
		require.NoError(t, err)

		found, err := repo.FindByActressAndLanguage(context.TODO(), actress.ID, "ja")
		require.NoError(t, err)
		assert.Equal(t, actress.ID, found.ActressID)
		assert.Equal(t, "ja", found.Language)
		assert.Equal(t, "テスト女優", found.DisplayName)
	})

	t.Run("FindByActressAndLanguage not found", func(t *testing.T) {
		_, err := repo.FindByActressAndLanguage(context.TODO(), actress.ID, "fr")
		assert.Error(t, err)
	})

	t.Run("FindAllByActress", func(t *testing.T) {
		// Create a new actress with multiple translations
		actress2 := &models.Actress{
			DMMID:        10002,
			JapaneseName: "第二女優",
		}
		err := actressRepo.Create(context.TODO(), actress2)
		require.NoError(t, err)

		translations := []*models.ActressTranslation{
			{ActressID: actress2.ID, Language: "en", DisplayName: "Actress Two"},
			{ActressID: actress2.ID, Language: "zh", DisplayName: "第二女优"},
			{ActressID: actress2.ID, Language: "ko", DisplayName: "배우 둘"},
		}

		for _, trans := range translations {
			err := repo.Upsert(context.TODO(), trans)
			require.NoError(t, err)
		}

		// Find all translations for this actress
		results, err := repo.FindAllByActress(context.TODO(), actress2.ID)
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

	t.Run("FindAllByActress with no translations", func(t *testing.T) {
		actress3 := &models.Actress{
			DMMID:        10003,
			JapaneseName: "第三女優",
		}
		err := actressRepo.Create(context.TODO(), actress3)
		require.NoError(t, err)

		results, err := repo.FindAllByActress(context.TODO(), actress3.ID)
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("Delete translation", func(t *testing.T) {
		actress4 := &models.Actress{
			DMMID:        10004,
			JapaneseName: "第四女優",
		}
		err := actressRepo.Create(context.TODO(), actress4)
		require.NoError(t, err)

		translation := &models.ActressTranslation{
			ActressID:   actress4.ID,
			Language:    "de",
			DisplayName: "Schauspielerin Vier",
		}

		err = repo.Upsert(context.TODO(), translation)
		require.NoError(t, err)

		// Delete
		err = repo.Delete(context.TODO(), actress4.ID, "de")
		require.NoError(t, err)

		// Verify deletion
		_, err = repo.FindByActressAndLanguage(context.TODO(), actress4.ID, "de")
		assert.Error(t, err)
	})

	t.Run("Delete non-existent translation", func(t *testing.T) {
		err := repo.Delete(context.TODO(), 9999, "xx")
		assert.NoError(t, err, "Deleting non-existent translation should not error")
	})

	t.Run("Dedup by actress_id and language", func(t *testing.T) {
		actress5 := &models.Actress{
			DMMID:        10005,
			JapaneseName: "第五女優",
		}
		err := actressRepo.Create(context.TODO(), actress5)
		require.NoError(t, err)

		// Upsert same actress+language twice
		trans1 := &models.ActressTranslation{
			ActressID:   actress5.ID,
			Language:    "es",
			DisplayName: "Actriz Cinco",
			SourceName:  "scraper_a",
		}
		err = repo.Upsert(context.TODO(), trans1)
		require.NoError(t, err)

		trans2 := &models.ActressTranslation{
			ActressID:   actress5.ID,
			Language:    "es",
			DisplayName: "Quinta Actriz",
			SourceName:  "scraper_b",
		}
		err = repo.Upsert(context.TODO(), trans2)
		require.NoError(t, err)

		// Should be one row — the second upsert updated the existing one
		results, err := repo.FindAllByActress(context.TODO(), actress5.ID)
		require.NoError(t, err)
		esResults := make([]models.ActressTranslation, 0)
		for _, r := range results {
			if r.Language == "es" {
				esResults = append(esResults, r)
			}
		}
		assert.Len(t, esResults, 1, "same actress_id+language should result in one row")
		assert.Equal(t, "Quinta Actriz", esResults[0].DisplayName, "second upsert should overwrite")
		assert.Equal(t, "scraper_b", esResults[0].SourceName, "source_name should be updated")
	})

	t.Run("Multiple actresses with same language", func(t *testing.T) {
		actressA := &models.Actress{
			DMMID:        10006,
			JapaneseName: "女優A",
		}
		actressB := &models.Actress{
			DMMID:        10007,
			JapaneseName: "女優B",
		}
		err := actressRepo.Create(context.TODO(), actressA)
		require.NoError(t, err)
		err = actressRepo.Create(context.TODO(), actressB)
		require.NoError(t, err)

		trans1 := &models.ActressTranslation{
			ActressID:   actressA.ID,
			Language:    "en",
			DisplayName: "Actress A",
		}
		trans2 := &models.ActressTranslation{
			ActressID:   actressB.ID,
			Language:    "en",
			DisplayName: "Actress B",
		}

		err = repo.Upsert(context.TODO(), trans1)
		require.NoError(t, err)
		err = repo.Upsert(context.TODO(), trans2)
		require.NoError(t, err)

		// Verify each actress has its own translation
		found1, err := repo.FindByActressAndLanguage(context.TODO(), actressA.ID, "en")
		require.NoError(t, err)
		assert.Equal(t, "Actress A", found1.DisplayName)

		found2, err := repo.FindByActressAndLanguage(context.TODO(), actressB.ID, "en")
		require.NoError(t, err)
		assert.Equal(t, "Actress B", found2.DisplayName)
	})

	t.Run("Translation with all fields populated", func(t *testing.T) {
		actress6 := &models.Actress{
			DMMID:        10008,
			JapaneseName: "完全女優",
		}
		err := actressRepo.Create(context.TODO(), actress6)
		require.NoError(t, err)

		translation := &models.ActressTranslation{
			ActressID:    actress6.ID,
			Language:     "es",
			FirstName:    "Nombre",
			LastName:     "Apellido",
			JapaneseName: "完全女優",
			DisplayName:  "Nombre Apellido",
			SourceName:   "test_scraper",
		}

		err = repo.Upsert(context.TODO(), translation)
		require.NoError(t, err)

		// Verify all fields
		found, err := repo.FindByActressAndLanguage(context.TODO(), actress6.ID, "es")
		require.NoError(t, err)
		assert.Equal(t, "Nombre", found.FirstName)
		assert.Equal(t, "Apellido", found.LastName)
		assert.Equal(t, "完全女優", found.JapaneseName)
		assert.Equal(t, "Nombre Apellido", found.DisplayName)
		assert.Equal(t, "test_scraper", found.SourceName)
	})

	t.Run("FindAllByActress with nonexistent actress", func(t *testing.T) {
		results, err := repo.FindAllByActress(context.TODO(), 99999)
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("FindByActressAndLanguage with nonexistent actress", func(t *testing.T) {
		_, err := repo.FindByActressAndLanguage(context.TODO(), 99999, "en")
		assert.Error(t, err)
	})
}
