package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMovieTranslationRepositoryUpsertTx_DuplicateCreateRace(t *testing.T) {
	db := newDatabaseTestDB(t)
	movieRepo := NewMovieRepository(db)
	translationRepo := NewMovieTranslationRepository(db)

	movie := createTestMovie("IPX-RACE-TX-001")
	require.NoError(t, movieRepo.Create(movie))

	cbName := "test:inject_translation_duplicate"
	inserted := false
	require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if inserted || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "movie_translations" {
			return
		}
		dest, ok := tx.Statement.Dest.(*models.MovieTranslation)
		if !ok {
			return
		}
		inserted = true
		if err := db.DB.Exec(
			"INSERT INTO movie_translations (movie_id, language, title, created_at, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
			dest.MovieID,
			dest.Language,
			"seeded",
		).Error; err != nil {
			_ = tx.AddError(err)
			return
		}
		_ = tx.AddError(gorm.ErrDuplicatedKey)
	}))
	defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

	translation := &models.MovieTranslation{
		MovieID:     movie.ContentID,
		Language:    "en",
		Title:       "Updated Title",
		Description: "Updated Description",
	}
	require.NoError(t, translationRepo.UpsertTx(db.DB, translation))

	stored, err := translationRepo.FindByMovieAndLanguage(movie.ContentID, "en")
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", stored.Title)
	assert.Equal(t, "Updated Description", stored.Description)
}

func TestMovieRepositoryUpsert_MissingIdentifiers(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	_, err := repo.Upsert(&models.Movie{Title: "No IDs"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content_id is required")
}

func TestMovieRepositoryUpsert_FallbackByDisplayID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	existing := createTestMovie("LEG-001")
	existing.ContentID = "legacy_content"
	existing.Title = "Legacy Title"
	require.NoError(t, repo.Create(existing))

	incoming := &models.Movie{
		ID:        "LEG-001",
		ContentID: "",
		Title:     "Updated via ID fallback",
	}
	_, err := repo.Upsert(incoming)

	loaded, err := repo.FindByContentID("legacy_content")
	require.NoError(t, err)
	assert.Equal(t, "Updated via ID fallback", loaded.Title)
	assert.Equal(t, "legacy_content", loaded.ContentID)
}

func TestMovieRepositoryEnsureGenresExistTx_DBError(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	err := db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec("DROP TABLE genres").Error)
		return repo.ensureGenresExistTx(tx, []models.Genre{{Name: "Any"}})
	})
	require.Error(t, err)
}

func TestMovieRepositoryEnsureActressesExistTx_DBError(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	err := db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec("DROP TABLE actresses").Error)
		return repo.ensureActressesExistTx(tx, []models.Actress{{DMMID: 1, JapaneseName: "X"}})
	})
	require.Error(t, err)
}

func TestMovieRepositoryEnsureActressesExistTx_DuplicateCreateRetries(t *testing.T) {
	t.Run("retry by dmm_id", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		cbName := "test:inject_actress_duplicate_dmm"
		inserted := false
		require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
			if inserted || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "actresses" {
				return
			}
			dest, ok := tx.Statement.Dest.(*models.Actress)
			if !ok {
				return
			}
			inserted = true
			if err := db.DB.Exec(
				"INSERT INTO actresses (dmm_id, japanese_name, first_name, last_name, thumb_url, created_at, updated_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
				dest.DMMID,
				dest.JapaneseName,
				dest.FirstName,
				dest.LastName,
				"",
			).Error; err != nil {
				_ = tx.AddError(err)
				return
			}
			_ = tx.AddError(gorm.ErrDuplicatedKey)
		}))
		defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

		actresses := []models.Actress{{DMMID: 777001, JapaneseName: "Retry DMM", ThumbURL: "https://example.com/dmm.jpg"}}
		err := repo.ensureActressesExistTx(db.DB, actresses)
		require.NoError(t, err)
		assert.NotZero(t, actresses[0].ID)

		var stored models.Actress
		require.NoError(t, db.DB.First(&stored, actresses[0].ID).Error)
		assert.Equal(t, "https://example.com/dmm.jpg", stored.ThumbURL)
	})

	t.Run("retry by japanese_name", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		cbName := "test:inject_actress_duplicate_jp"
		inserted := false
		require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
			if inserted || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "actresses" {
				return
			}
			dest, ok := tx.Statement.Dest.(*models.Actress)
			if !ok {
				return
			}
			inserted = true
			if err := db.DB.Exec(
				"INSERT INTO actresses (dmm_id, japanese_name, first_name, last_name, thumb_url, created_at, updated_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
				0,
				dest.JapaneseName,
				"",
				"",
				"",
			).Error; err != nil {
				_ = tx.AddError(err)
				return
			}
			_ = tx.AddError(gorm.ErrDuplicatedKey)
		}))
		defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

		actresses := []models.Actress{{JapaneseName: "Retry JP", ThumbURL: "https://example.com/jp.jpg"}}
		err := repo.ensureActressesExistTx(db.DB, actresses)
		require.NoError(t, err)
		assert.NotZero(t, actresses[0].ID)

		var stored models.Actress
		require.NoError(t, db.DB.First(&stored, actresses[0].ID).Error)
		assert.Equal(t, "https://example.com/jp.jpg", stored.ThumbURL)
	})

	t.Run("retry by first and last name", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		cbName := "test:inject_actress_duplicate_name_pair"
		inserted := false
		require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
			if inserted || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "actresses" {
				return
			}
			dest, ok := tx.Statement.Dest.(*models.Actress)
			if !ok {
				return
			}
			inserted = true
			if err := db.DB.Exec(
				"INSERT INTO actresses (dmm_id, japanese_name, first_name, last_name, thumb_url, created_at, updated_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
				0,
				"",
				dest.FirstName,
				dest.LastName,
				"",
			).Error; err != nil {
				_ = tx.AddError(err)
				return
			}
			_ = tx.AddError(gorm.ErrDuplicatedKey)
		}))
		defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

		actresses := []models.Actress{{FirstName: "Retry", LastName: "Pair", ThumbURL: "https://example.com/pair.jpg"}}
		err := repo.ensureActressesExistTx(db.DB, actresses)
		require.NoError(t, err)
		assert.NotZero(t, actresses[0].ID)

		var stored models.Actress
		require.NoError(t, db.DB.First(&stored, actresses[0].ID).Error)
		assert.Equal(t, "https://example.com/pair.jpg", stored.ThumbURL)
	})
}

func TestMovieRepositoryUpsert_OrphanedTranslationsDoNotBlockActressCreation(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	contentID := "orphan-trans-test"
	require.NoError(t, db.DB.Exec(
		"INSERT INTO movie_translations (movie_id, language, title, created_at, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
		contentID, "en", "Orphaned Title",
	).Error)

	movie := &models.Movie{
		ContentID: contentID,
		ID:        "ORPHAN-001",
		Title:     "Movie With Orphaned Translation",
		Actresses: []models.Actress{
			{DMMID: 88888, JapaneseName: "OrphanTest Actress", FirstName: "Orphan", LastName: "Test"},
		},
		Genres: []models.Genre{
			{Name: "OrphanGenre"},
		},
		Translations: []models.MovieTranslation{
			{Language: "en", Title: "Real English Title"},
			{Language: "zh", Title: "Chinese Title"},
		},
	}
	result, err := repo.Upsert(movie)
	require.NoError(t, err)

	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "OrphanTest Actress", result.Actresses[0].JapaneseName)
	assert.Len(t, result.Genres, 1)
	assert.Equal(t, "OrphanGenre", result.Genres[0].Name)
	assert.Len(t, result.Translations, 2)

	langTitles := make(map[string]string)
	for _, tr := range result.Translations {
		langTitles[tr.Language] = tr.Title
	}
	assert.Equal(t, "Real English Title", langTitles["en"], "orphaned translation should be upserted-over")
	assert.Equal(t, "Chinese Title", langTitles["zh"])

	found, err := repo.FindByContentID(contentID)
	require.NoError(t, err)
	assert.Len(t, found.Actresses, 1)
	assert.Equal(t, 88888, found.Actresses[0].DMMID)
	assert.Len(t, found.Translations, 2)

	actressRepo := NewActressRepository(db)
	actress, err := actressRepo.FindByDMMID(88888)
	require.NoError(t, err)
	assert.Equal(t, "OrphanTest Actress", actress.JapaneseName)
}

func TestMovieRepositoryUpsert_OrphanedTranslationsOnExistingMovieUpdate(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	contentID := "orphan-update-test"
	movie := createTestMovie("ORPHAN-UPD-001")
	movie.ContentID = contentID
	movie.Actresses = []models.Actress{
		{DMMID: 77711, JapaneseName: "Original Actress"},
	}
	err := repo.Create(movie)
	require.NoError(t, err)

	require.NoError(t, db.DB.Exec(
		"INSERT INTO movie_translations (movie_id, language, title, created_at, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
		contentID, "en", "Orphaned Title",
	).Error)
	require.NoError(t, db.DB.Exec(
		"INSERT INTO movie_translations (movie_id, language, title, created_at, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
		contentID, "ja", "Orphaned Japanese Title",
	).Error)

	updated := &models.Movie{
		ContentID: contentID,
		ID:        "ORPHAN-UPD-001",
		Title:     "Updated Movie Title",
		Actresses: []models.Actress{
			{DMMID: 77712, JapaneseName: "New Actress", FirstName: "New", LastName: "Actress"},
		},
		Genres: []models.Genre{
			{Name: "UpdatedGenre"},
		},
		Translations: []models.MovieTranslation{
			{Language: "en", Title: "Replaced English Title"},
			{Language: "zh", Title: "New Chinese Title"},
		},
	}
	result, err := repo.Upsert(updated)
	require.NoError(t, err)

	assert.Equal(t, "Updated Movie Title", result.Title)
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "New Actress", result.Actresses[0].JapaneseName)
	assert.Len(t, result.Genres, 1)
	assert.Len(t, result.Translations, 2)

	langTitles := make(map[string]string)
	for _, tr := range result.Translations {
		langTitles[tr.Language] = tr.Title
	}
	assert.Equal(t, "Replaced English Title", langTitles["en"], "orphaned translation should be upserted-over")
	assert.Equal(t, "New Chinese Title", langTitles["zh"])
	assert.NotContains(t, langTitles, "ja", "stale translation should be deleted")

	found, err := repo.FindByContentID(contentID)
	require.NoError(t, err)
	assert.Len(t, found.Actresses, 1)
	assert.Equal(t, 77712, found.Actresses[0].DMMID)

	var transCount int64
	require.NoError(t, db.DB.Model(&models.MovieTranslation{}).Where("movie_id = ?", contentID).Count(&transCount).Error)
	assert.Equal(t, int64(2), transCount)
}

func TestMovieRepositoryUpsert_DuplicateCreateRaceFallbackUpdatePath(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	cbName := "test:inject_movie_duplicate_create"
	inserted := false
	require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if inserted || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "movies" {
			return
		}
		dest, ok := tx.Statement.Dest.(*models.Movie)
		if !ok {
			return
		}
		inserted = true
		if err := tx.Exec(
			"INSERT INTO movies (content_id, id, title, created_at, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
			dest.ContentID,
			dest.ID,
			"Seeded Row",
		).Error; err != nil {
			_ = tx.AddError(err)
			return
		}
		_ = tx.AddError(gorm.ErrDuplicatedKey)
	}))
	defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

	movie := &models.Movie{
		ContentID: "racefallback001",
		ID:        "RACE-FALLBACK-001",
		Title:     "Updated by fallback",
		Genres: []models.Genre{
			{Name: "RaceGenre"},
		},
		Actresses: []models.Actress{
			{DMMID: 991001, JapaneseName: "Race Actress"},
		},
		Translations: []models.MovieTranslation{
			{Language: "en", Title: "Race Title EN"},
		},
	}
	_, err := repo.Upsert(movie)
	require.NoError(t, err)

	found, err := repo.FindByContentID(movie.ContentID)
	require.NoError(t, err)
	assert.Equal(t, "Updated by fallback", found.Title)
	assert.Len(t, found.Genres, 1)
	assert.Len(t, found.Actresses, 1)
	assert.Len(t, found.Translations, 1)
	assert.Equal(t, "en", found.Translations[0].Language)
}
