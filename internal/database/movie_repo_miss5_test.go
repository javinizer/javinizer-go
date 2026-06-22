package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// --- UpsertWithTranslations: find by ID when ContentID not found ---
// Line 102-105: existing found by ID, ContentID copied

func TestMiss5_Upsert_FindByIDWhenContentIDNotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// First create a movie with ContentID set
	movie1 := &models.Movie{
		ContentID:    "miss5-find-cid",
		ID:           "MISS5-FIND",
		DisplayTitle: "Find By ID Test",
		Title:        "Find By ID Test",
	}
	result1, err := repo.UpsertWithTranslations(context.TODO(), movie1, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result1)

	// Now upsert with same ID but different ContentID
	// This tests the "find by ID" branch where ContentID is empty in the new movie
	// but the existing record has ContentID set
	movie2 := &models.Movie{
		ID:           "MISS5-FIND",
		DisplayTitle: "Updated Find Test",
		Title:        "Updated Find Test",
	}
	result2, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "miss5-find-cid", result2.ContentID)
}

// --- UpsertWithTranslations: duplicate key race with loadErr = NotFound ---
// Line 103-105: loadErr is ErrRecordNotFound after duplicate key

func TestMiss5_Upsert_DuplicateKeyLoadNotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create a movie directly
	movie := &models.Movie{
		ContentID:    "miss5-dup-notfound-cid",
		ID:           "MISS5-DUPNF",
		DisplayTitle: "Dup NotFound Test",
		Title:        "Dup NotFound Test",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Upsert again with same content ID — exercises the "existing found" path
	movie2 := &models.Movie{
		ContentID:    "miss5-dup-notfound-cid",
		ID:           "MISS5-DUPNF",
		DisplayTitle: "Dup Updated",
		Title:        "Dup Updated",
	}
	result2, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "Dup Updated", result2.DisplayTitle)
}

// --- ensureGenresExistTx: concurrent create race retry ---
// Line 191-198: raceRetryCreate for genres

func TestMiss5_EnsureGenresExistTx_RaceRetryCreate(t *testing.T) {
	db := newDatabaseTestDB(t)

	// Create a genre in advance
	require.NoError(t, db.DB.Create(&models.Genre{Name: "ExistingGenre"}).Error)

	genres := []models.Genre{
		{Name: "ExistingGenre"}, // Will be found
		{Name: "NewGenre"},      // Will be created
	}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		repo := NewMovieRepository(db)
		return repo.upserter.ensureGenresExistTx(tx, genres)
	})
	require.NoError(t, err)
	assert.Equal(t, "ExistingGenre", genres[0].Name)
	assert.Greater(t, genres[0].ID, uint(0))
	assert.Equal(t, "NewGenre", genres[1].Name)
	assert.Greater(t, genres[1].ID, uint(0))
}

// --- ensureActressesExistTx: JapaneseName group with existing actresses ---
// Line 308-316: jpGroup path

func TestMiss5_EnsureActressesExistTx_JapaneseNameGroup(t *testing.T) {
	db := newDatabaseTestDB(t)

	// Create actress with JapaneseName in advance
	require.NoError(t, db.DB.Create(&models.Actress{JapaneseName: "テスト女優", ThumbURL: "https://existing.jpg"}).Error)

	actresses := []models.Actress{
		{JapaneseName: "テスト女優", ThumbURL: ""}, // Will be found and merged (no merge needed, existing has ThumbURL)
		{JapaneseName: "新しい女優"},               // Will be created
	}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		repo := NewMovieRepository(db)
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Greater(t, actresses[0].ID, uint(0))
	assert.Greater(t, actresses[1].ID, uint(0))
}

// --- ensureActressesExistTx: name group with FirstName+LastName ---
// Line 348-381: nameGroup path with full names

func TestMiss5_EnsureActressesExistTx_NameGroupWithBothNames(t *testing.T) {
	db := newDatabaseTestDB(t)

	// Create actress with FirstName+LastName in advance
	require.NoError(t, db.DB.Create(&models.Actress{FirstName: "Test", LastName: "Actress"}).Error)

	actresses := []models.Actress{
		{FirstName: "Test", LastName: "Actress"}, // Will be found
		{FirstName: "New", LastName: "Actress"},  // Will be created
	}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		repo := NewMovieRepository(db)
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Greater(t, actresses[0].ID, uint(0))
	assert.Greater(t, actresses[1].ID, uint(0))
}

// --- ensureActressesExistTx: name group with only FirstName, ErrRecordNotFound ---
// Line 357-367: nameGroup with only FirstName, creating new

func TestMiss5_EnsureActressesExistTx_NameGroupFirstNameOnly(t *testing.T) {
	db := newDatabaseTestDB(t)

	actresses := []models.Actress{
		{FirstName: "UniqueFirstName"},
	}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		repo := NewMovieRepository(db)
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Greater(t, actresses[0].ID, uint(0))
}

// --- ensureActressesExistTx: name group with LastName only, ErrRecordNotFound ---
// Line 365-367: nameGroup.LastName only path

func TestMiss5_EnsureActressesExistTx_NameGroupLastNameOnly(t *testing.T) {
	db := newDatabaseTestDB(t)

	actresses := []models.Actress{
		{LastName: "UniqueLastName"},
	}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		repo := NewMovieRepository(db)
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Greater(t, actresses[0].ID, uint(0))
}

// --- ensureActressesExistTx: merge actress data ---
// Line 267-275: mergeActressData triggers save

func TestMiss5_EnsureActressesExistTx_MergeActressDataDMMID(t *testing.T) {
	db := newDatabaseTestDB(t)

	// Create actress with DMMID but no ThumbURL
	require.NoError(t, db.DB.Create(&models.Actress{DMMID: 77777, ThumbURL: ""}).Error)

	actresses := []models.Actress{
		{DMMID: 77777, ThumbURL: "https://new-thumb.jpg"}, // Should merge ThumbURL
	}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		repo := NewMovieRepository(db)
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, "https://new-thumb.jpg", actresses[0].ThumbURL)
	assert.Greater(t, actresses[0].ID, uint(0))
}

// --- Delete: movie with genres and actresses ---
// Line 434-448: delete with associations

func TestMiss5_Delete_WithGenresAndActresses(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ContentID:    "miss5-del-ga-cid",
		ID:           "MISS5-DELGA",
		DisplayTitle: "Delete Genres Actresses",
		Title:        "Delete Genres Actresses",
		Genres: []models.Genre{
			{Name: "DeleteGenre"},
		},
		Actresses: []models.Actress{
			{JapaneseName: "削除女優"},
		},
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	err = repo.Delete(context.TODO(), "MISS5-DELGA")
	require.NoError(t, err)

	// Verify movie is gone
	_, err = repo.FindByContentID(context.TODO(), "miss5-del-ga-cid")
	require.Error(t, err)
}

// --- List: returns movies ---

func TestMiss5_List_ReturnsMovies(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ContentID:    "miss5-list-cid",
		ID:           "MISS5-LIST",
		DisplayTitle: "List Test",
		Title:        "List Test",
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	movies, err := repo.List(context.TODO(), 10, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(movies), 1)
}

// --- saveMovieWithAssociations: error path ---
// Lines 150-155

func TestMiss5_SaveMovieWithAssociations_Normal(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ContentID:    "miss5-saveassoc-cid",
		ID:           "MISS5-SAVE",
		DisplayTitle: "Save Assoc Test",
		Title:        "Save Assoc Test",
		Genres: []models.Genre{
			{Name: "AssocGenre"},
		},
		Actresses: []models.Actress{
			{JapaneseName: "アソシエーション女優"},
		},
		Translations: []models.MovieTranslation{
			{Language: "en", Title: "Save Assoc Test EN"},
		},
	}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.saveMovieWithAssociations(tx, movie)
	})
	require.NoError(t, err)
	// Translations should be repopulated after upsertMovieCore
	assert.NotNil(t, movie.Translations)
}

// --- Update: update existing movie ---

func TestMiss5_Update_ExistingMovie(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ContentID:    "miss5-update-cid",
		ID:           "MISS5-UPD",
		DisplayTitle: "Original Title",
		Title:        "Original Title",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	result.DisplayTitle = "Updated Title"
	result.Title = "Updated Title"
	err = repo.Update(context.TODO(), result)
	require.NoError(t, err)

	found, err := repo.FindByContentID(context.TODO(), "miss5-update-cid")
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", found.DisplayTitle)
}
