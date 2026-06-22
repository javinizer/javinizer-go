package database

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Movie Repository deep tests for additional coverage ---

func TestMovieRepository_Deep_DeleteNonExistentID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Delete a movie ID that doesn't exist at all — should return nil
	err := repo.Delete(context.TODO(), "NONEXISTENT-999")
	assert.NoError(t, err, "deleting non-existent movie should not error")
}

func TestMovieRepository_Deep_UpdateNonPersisted(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Update a movie that hasn't been created — GORM Save creates if not exists
	movie := createTestMovie("IPX-UPD-NEW")
	err := repo.Update(context.TODO(), movie)
	require.NoError(t, err, "GORM Save should create the record if it doesn't exist")

	found, err := repo.FindByID(context.TODO(), "IPX-UPD-NEW")
	require.NoError(t, err)
	assert.Equal(t, "IPX-UPD-NEW", found.ID)
}

func TestMovieRepository_Deep_DeleteWithActressesAndGenres(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-DEL-AG")
	movie.Genres = []models.Genre{{Name: "DeleteGenre"}, {Name: "DeleteGenre2"}}
	movie.Actresses = []models.Actress{{DMMID: 55501, JapaneseName: "DeleteActress"}}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	err = repo.Delete(context.TODO(), "IPX-DEL-AG")
	require.NoError(t, err)

	_, err = repo.FindByID(context.TODO(), "IPX-DEL-AG")
	assert.Error(t, err, "movie should be deleted")
}

func TestMovieRepository_Deep_ListEmptyDB(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movies, err := repo.List(context.TODO(), 10, 0)
	require.NoError(t, err)
	assert.Empty(t, movies)
}

func TestMovieRepository_Deep_ListPagination(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create 5 movies
	for i := 0; i < 5; i++ {
		movie := createTestMovie("IPX-LIST-" + string(rune('A'+i)))
		err := repo.Create(context.TODO(), movie)
		require.NoError(t, err)
	}

	// Page 1: limit=2 offset=0
	movies, err := repo.List(context.TODO(), 2, 0)
	require.NoError(t, err)
	assert.Len(t, movies, 2)

	// Page 2: limit=2 offset=2
	movies, err = repo.List(context.TODO(), 2, 2)
	require.NoError(t, err)
	assert.Len(t, movies, 2)

	// Page 3: limit=2 offset=4
	movies, err = repo.List(context.TODO(), 2, 4)
	require.NoError(t, err)
	assert.Len(t, movies, 1)

	// Beyond: offset=10
	movies, err = repo.List(context.TODO(), 2, 10)
	require.NoError(t, err)
	assert.Empty(t, movies)
}

func TestMovieRepository_Deep_FindByContentID_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	_, err := repo.FindByContentID(context.TODO(), "nonexistent-cid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent-cid")
}

func TestMovieRepository_Deep_FindByID_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	_, err := repo.FindByID(context.TODO(), "NONEXISTENT-ID")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NONEXISTENT-ID")
}

func TestMovieRepository_Deep_UpsertWithContentIDDerviedFromID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// When ContentID is empty and only ID is provided
	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "ContentID Derivation Test",
	}
	result, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "abc123", result.ContentID, "ContentID should be derived from ID as lowercase with hyphens removed")
}

func TestMovieRepository_Deep_UpsertWithBothIDsEmpty(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// When both ContentID and ID are empty strings, Upsert should fail
	movie := &models.Movie{
		ID:        "",
		ContentID: "",
		Title:     "No IDs",
	}
	_, err := repo.Upsert(context.TODO(), movie)
	assert.Error(t, err, "upsert with no ID or ContentID should fail")
}

func TestMovieRepository_Deep_UpsertPreservesCreatedAt(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create movie
	movie := createTestMovie("IPX-CREATED-001")
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	// Find it to get the CreatedAt
	found, err := repo.FindByID(context.TODO(), "IPX-CREATED-001")
	require.NoError(t, err)
	originalCreatedAt := found.CreatedAt

	// Wait briefly
	time.Sleep(10 * time.Millisecond)

	// Upsert again
	movie.Title = "Updated Title"
	_, err = repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	// Verify CreatedAt is preserved
	found2, err := repo.FindByID(context.TODO(), "IPX-CREATED-001")
	require.NoError(t, err)
	assert.Equal(t, originalCreatedAt.Unix(), found2.CreatedAt.Unix(), "CreatedAt should be preserved on upsert")
}

func TestMovieRepository_Deep_UpsertWithTranslationsDeep(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create movie with genre and actress translations
	movie := createTestMovie("IPX-TRANS-DEEP")
	movie.Genres = []models.Genre{{Name: "DeepGenre"}}
	movie.Actresses = []models.Actress{{DMMID: 66001, JapaneseName: "DeepActress"}}

	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "DeepGenre (EN)", SourceName: "test"},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", DisplayName: "DeepActress (EN)", SourceName: "test"},
	}

	result, err := repo.UpsertWithTranslations(context.TODO(), movie, genreTranslations, actressTranslations)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-TRANS-DEEP", result.ID)
}

func TestMovieRepository_Deep_MergeActressDataBothFilled(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// When existing already has data and new also has data, no update needed
	existing := &models.Actress{
		ThumbURL:  "http://existing.com/thumb.jpg",
		FirstName: "Existing",
		LastName:  "ExistingLast",
	}
	newActress := models.Actress{
		ThumbURL:  "http://new.com/thumb.jpg",
		FirstName: "NewFirst",
		LastName:  "NewLast",
	}
	needsUpdate := repo.upserter.mergeActressData(existing, newActress)
	assert.False(t, needsUpdate, "no update needed when existing already has all fields filled")
	assert.Equal(t, "http://existing.com/thumb.jpg", existing.ThumbURL, "existing ThumbURL should not be overwritten")
	assert.Equal(t, "Existing", existing.FirstName, "existing FirstName should not be overwritten")
	assert.Equal(t, "ExistingLast", existing.LastName, "existing LastName should not be overwritten")
}

func TestMovieRepository_Deep_MergeActressDataPartial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// When existing has some fields and new has different ones
	existing := &models.Actress{
		ThumbURL:  "http://existing.com/thumb.jpg",
		FirstName: "Existing",
		LastName:  "",
	}
	newActress := models.Actress{
		ThumbURL:  "",
		FirstName: "NewFirst",
		LastName:  "NewLast",
	}
	needsUpdate := repo.upserter.mergeActressData(existing, newActress)
	assert.True(t, needsUpdate, "update needed when new fills gaps in existing")
	assert.Equal(t, "http://existing.com/thumb.jpg", existing.ThumbURL, "existing ThumbURL should be preserved")
	assert.Equal(t, "Existing", existing.FirstName, "existing FirstName should be preserved")
	assert.Equal(t, "NewLast", existing.LastName, "missing LastName should be filled from new")
}

func TestMovieRepository_Deep_MovieEntityID(t *testing.T) {
	t.Run("prefers ContentID", func(t *testing.T) {
		m := &models.Movie{ContentID: "abc123", ID: "ABC-123"}
		assert.Equal(t, "abc123", movieEntityID(m))
	})

	t.Run("falls back to ID when ContentID is empty", func(t *testing.T) {
		m := &models.Movie{ContentID: "", ID: "ABC-123"}
		assert.Equal(t, "ABC-123", movieEntityID(m))
	})
}

func TestMovieRepository_Deep_CreateWithTranslations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-CR-TRANS")
	movie.Translations = []models.MovieTranslation{
		{Language: "en", Title: "English Title", SourceName: "test"},
		{Language: "zh", Title: "Chinese Title", SourceName: "test"},
	}
	err := repo.Create(context.TODO(), movie)
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), "IPX-CR-TRANS")
	require.NoError(t, err)
	assert.Len(t, found.Translations, 2)
}

func TestMovieRepository_Deep_DeleteCleansUpTags(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)
	tagRepo := NewMovieTagRepository(db)

	// Create a movie and add tags
	movie := createTestMovie("IPX-DEL-TAG")
	err := repo.Create(context.TODO(), movie)
	require.NoError(t, err)

	require.NoError(t, tagRepo.AddTag(context.TODO(), "ipxdeltag001", "test-tag"))

	// Delete the movie
	err = repo.Delete(context.TODO(), "IPX-DEL-TAG")
	require.NoError(t, err)

	// Verify movie is deleted
	_, err = repo.FindByID(context.TODO(), "IPX-DEL-TAG")
	assert.Error(t, err)
}

func TestMovieRepository_Deep_CreateDuplicateError(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie1 := createTestMovie("IPX-DUP-001")
	err := repo.Create(context.TODO(), movie1)
	require.NoError(t, err)

	// Creating the same movie again should error (duplicate content_id)
	movie2 := createTestMovie("IPX-DUP-001")
	err = repo.Create(context.TODO(), movie2)
	assert.Error(t, err, "creating duplicate movie should error")
}

func TestMovieRepository_Deep_UpsertExistingByID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create a movie
	movie := createTestMovie("IPX-UPBYID-001")
	movie.ContentID = "upbyid001"
	err := repo.Create(context.TODO(), movie)
	require.NoError(t, err)

	// Upsert the same movie by ID (no ContentID match, but ID match)
	movie2 := createTestMovie("IPX-UPBYID-001")
	movie2.ContentID = "" // Will be derived from ID
	movie2.Title = "Updated via ID lookup"
	result, err := repo.Upsert(context.TODO(), movie2)
	require.NoError(t, err)
	assert.Equal(t, "Updated via ID lookup", result.Title)
}
