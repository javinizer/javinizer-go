package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// --- UpsertWithTranslations: movie with ContentID empty, ID provided generates ContentID ---

func TestMiss4_UpsertWithTranslations_EmptyContentIDGeneratesFromID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ID:           "ABP-420",
		DisplayTitle: "Test Movie",
		Title:        "Test Movie",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "abp420", result.ContentID)
	assert.Equal(t, "ABP-420", result.ID)
}

// --- UpsertWithTranslations: movie with both ContentID and ID empty returns error ---

func TestMiss4_UpsertWithTranslations_BothIDsEmpty(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		DisplayTitle: "Test Movie",
		Title:        "Test Movie",
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content_id is required")
}

// --- UpsertWithTranslations: movie with genres and actresses ---

func TestMiss4_UpsertWithTranslations_WithGenresAndActresses(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ContentID:    "miss4-genre-cid",
		ID:           "MISS4-001",
		DisplayTitle: "Genre Test",
		Title:        "Genre Test",
		Genres: []models.Genre{
			{Name: "Action"},
			{Name: "Drama"},
		},
		Actresses: []models.Actress{
			{JapaneseName: "テスト女優"},
		},
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	assert.Len(t, result.Genres, 2)
	assert.Len(t, result.Actresses, 1)
}

// --- UpsertWithTranslations: duplicate movie updates existing ---

func TestMiss4_UpsertWithTranslations_DuplicateUpdatesExisting(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie1 := &models.Movie{
		ContentID:    "miss4-dup-cid",
		ID:           "MISS4-002",
		DisplayTitle: "Original Title",
		Title:        "Original Title",
	}
	result1, err := repo.UpsertWithTranslations(context.TODO(), movie1, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "Original Title", result1.DisplayTitle)

	movie2 := &models.Movie{
		ContentID:    "miss4-dup-cid",
		ID:           "MISS4-002",
		DisplayTitle: "Updated Title",
		Title:        "Updated Title",
	}
	result2, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", result2.DisplayTitle)
}

// --- UpsertWithTranslations: with translations ---

func TestMiss4_UpsertWithTranslations_WithTranslations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ContentID:    "miss4-trans-cid",
		ID:           "MISS4-003",
		DisplayTitle: "Translation Test",
		Title:        "Translation Test",
	}

	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "Action"},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", FirstName: "Test"},
	}

	result, err := repo.UpsertWithTranslations(context.TODO(), movie, genreTranslations, actressTranslations)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// --- ensureActressesExistTx: actress with only FirstName ---

func TestMiss4_EnsureActressesExistTx_FirstNameOnly(t *testing.T) {
	db := newDatabaseTestDB(t)

	actresses := []models.Actress{
		{FirstName: "TestFirst"},
	}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		repo := NewMovieRepository(db)
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, "TestFirst", actresses[0].FirstName)
}

// --- ensureActressesExistTx: actress with only LastName ---

func TestMiss4_EnsureActressesExistTx_LastNameOnly(t *testing.T) {
	db := newDatabaseTestDB(t)

	actresses := []models.Actress{
		{LastName: "TestLast"},
	}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		repo := NewMovieRepository(db)
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, "TestLast", actresses[0].LastName)
}

// --- ensureActressesExistTx: existing actress with DMMID gets merged ---

func TestMiss4_EnsureActressesExistTx_ExistingDMMIDMerged(t *testing.T) {
	db := newDatabaseTestDB(t)

	// Create actress first
	existing := models.Actress{
		DMMID:        5555,
		JapaneseName: "既存女優",
		ThumbURL:     "",
	}
	require.NoError(t, db.DB.Create(&existing).Error)

	// Now upsert with same DMMID but different fields
	actresses := []models.Actress{
		{DMMID: 5555, ThumbURL: "https://example.com/thumb.jpg"},
	}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		repo := NewMovieRepository(db)
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/thumb.jpg", actresses[0].ThumbURL)
}

// --- ensureGenresExistTx: empty slice returns nil ---

func TestMiss4_EnsureGenresExistTx_EmptySlice(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	err := repo.upserter.ensureGenresExistTx(db.DB, nil)
	assert.NoError(t, err)

	err = repo.upserter.ensureGenresExistTx(db.DB, []models.Genre{})
	assert.NoError(t, err)
}

// --- FindByID: not found returns error ---

func TestMiss4_FindByID_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	_, err := repo.FindByID(context.TODO(), "NONEXISTENT-999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "find movie by id")
}

// --- FindByContentID: not found returns error ---

func TestMiss4_FindByContentID_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	_, err := repo.FindByContentID(context.TODO(), "nonexistent999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "find movie")
}

// --- Delete: non-existent movie returns nil ---

func TestMiss4_Delete_NonExistent(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	err := repo.Delete(context.TODO(), "NONEXISTENT-999")
	assert.NoError(t, err)
}

// --- Delete: existing movie with associations ---

func TestMiss4_Delete_WithAssociations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ContentID:    "miss4-del-cid",
		ID:           "MISS4-DEL",
		DisplayTitle: "Delete Test",
		Title:        "Delete Test",
		Genres: []models.Genre{
			{Name: "DeletableGenre"},
		},
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	err = repo.Delete(context.TODO(), "MISS4-DEL")
	assert.NoError(t, err)

	_, err = repo.FindByContentID(context.TODO(), "miss4-del-cid")
	require.Error(t, err) // Should not be found
}
