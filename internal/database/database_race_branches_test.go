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
