package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// --- Miss-line coverage tests for movie_repo.go ---

// movieEntityID (line 22): ContentID branch
func TestMovieEntityID_ContentID(t *testing.T) {
	m := &models.Movie{ContentID: "abc123", ID: "ABC-123"}
	assert.Equal(t, "abc123", movieEntityID(m))
}

// movieEntityID (line 22): fallback to ID when ContentID is empty
func TestMovieEntityID_FallbackToID(t *testing.T) {
	m := &models.Movie{ContentID: "", ID: "ABC-123"}
	assert.Equal(t, "ABC-123", movieEntityID(m))
}

// Update (lines 92-94): Save via GORM
func TestMovieRepository_Update_Save(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("UPD-SAVE-001")
	err := repo.Create(context.TODO(), movie)
	require.NoError(t, err)

	movie.DisplayTitle = "Updated Title"
	err = repo.Update(context.TODO(), movie)
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), "UPD-SAVE-001")
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", found.DisplayTitle)
}

// UpsertWithTranslations (lines 102-200): new movie with ContentID auto-generated from ID
func TestMovieRepository_UpsertWithTranslations_NewMovie_AutoContentID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Movie with no ContentID — should auto-generate from ID
	movie := &models.Movie{
		ID:           "IPX-100",
		DisplayTitle: "Auto ContentID Test",
		Title:        "Auto ContentID Test",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "ipx100", result.ContentID)
}

// UpsertWithTranslations: content_id required error when both empty
func TestMovieRepository_UpsertWithTranslations_NoContentID_NoID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Movie with no ContentID and no ID — should error
	movie := &models.Movie{
		DisplayTitle: "No IDs",
		Title:        "No IDs",
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content_id is required")
}

// UpsertWithTranslations: existing movie found by ContentID, updates CreatedAt
func TestMovieRepository_UpsertWithTranslations_ExistingByContentID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create initial movie
	movie := createTestMovie("IPX-200")
	result1, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	originalCreatedAt := result1.CreatedAt

	// Small sleep to ensure CreatedAt would differ if overwritten
	time.Sleep(10 * time.Millisecond)

	// Upsert same movie — should preserve CreatedAt
	movie2 := createTestMovie("IPX-200")
	movie2.DisplayTitle = "Updated Title"
	result2, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", result2.DisplayTitle)
	assert.Equal(t, originalCreatedAt.Truncate(time.Second), result2.CreatedAt.Truncate(time.Second))
}

// UpsertWithTranslations: existing movie found by ID (not ContentID)
func TestMovieRepository_UpsertWithTranslations_ExistingByID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create movie with ContentID
	movie := &models.Movie{
		ContentID:    "customcid",
		ID:           "CUSTOM-001",
		DisplayTitle: "Original",
		Title:        "Original",
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	// Upsert with matching ID but different ContentID — should find by ID and adopt ContentID
	movie2 := &models.Movie{
		ID:           "CUSTOM-001",
		DisplayTitle: "Updated by ID lookup",
		Title:        "Updated by ID lookup",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "Updated by ID lookup", result.DisplayTitle)
	assert.Equal(t, "customcid", result.ContentID)
}

// UpsertWithTranslations: new movie with genres and actresses (ensureGenresExistTx, ensureActressesExistTx)
func TestMovieRepository_UpsertWithTranslations_NewMovieWithAssociations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-300")
	movie.Genres = []models.Genre{{Name: "Action"}, {Name: "Thriller"}}
	movie.Actresses = []models.Actress{{DMMID: 9001, JapaneseName: "TestActress"}}

	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	assert.Len(t, result.Genres, 2)
	assert.Len(t, result.Actresses, 1)
}

// UpsertWithTranslations: with genre and actress translations
func TestMovieRepository_UpsertWithTranslations_WithTranslations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-400")
	movie.Genres = []models.Genre{{Name: "Drama"}, {Name: "Romance"}}
	movie.Actresses = []models.Actress{{DMMID: 9002, JapaneseName: "TransActress", FirstName: "First", LastName: "Last"}}
	movie.Translations = []models.MovieTranslation{
		{Language: "en", Title: "English Title"},
	}

	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "Drama EN", SourceName: "test"},
		{GenreIndex: 1, Language: "en", Name: "Romance EN", SourceName: "test"},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", FirstName: "FirstEN", LastName: "LastEN", SourceName: "test"},
	}

	result, err := repo.UpsertWithTranslations(context.TODO(), movie, genreTranslations, actressTranslations)
	require.NoError(t, err)
	assert.Equal(t, "English Title", result.Translations[0].Title)
}

// Upsert: delegates to UpsertWithTranslations
func TestMovieRepository_Upsert_Delegates(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-UPSERT-001")
	result, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)
	assert.Equal(t, "IPX-UPSERT-001", result.ID)
}

// saveMovieWithAssociations (lines 267-287): exercised via duplicate key path
func TestMovieRepository_SaveMovieWithAssociations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-SAVE-ASSOC")
	movie.Genres = []models.Genre{{Name: "SaveGenre"}}
	movie.Actresses = []models.Actress{{DMMID: 9100, JapaneseName: "SaveActress"}}

	// Create first
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	// Now test saveMovieWithAssociations directly via duplicate-key path
	// by creating a second movie with same ContentID
	movie2 := createTestMovie("IPX-SAVE-ASSOC")
	movie2.Genres = []models.Genre{{Name: "SaveGenre"}, {Name: "NewGenre"}}
	movie2.Actresses = []models.Actress{{DMMID: 9100, JapaneseName: "SaveActress"}, {DMMID: 9101, JapaneseName: "NewActress"}}

	result, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Len(t, result.Genres, 2)
	assert.Len(t, result.Actresses, 2)
}

// ensureGenresExistTx: race retry create path (new genre that doesn't exist in batch lookup)
func TestMovieRepository_EnsureGenresExistTx_NewGenre(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	genres := []models.Genre{{Name: "BrandNewGenre"}}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureGenresExistTx(tx, genres)
	})
	require.NoError(t, err)
	assert.NotZero(t, genres[0].ID)
}

// ensureGenresExistTx: existing genre found in batch lookup
func TestMovieRepository_EnsureGenresExistTx_ExistingGenre(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Pre-create genre
	existing := models.Genre{Name: "ExistingGenre"}
	require.NoError(t, db.DB.Create(&existing).Error)

	genres := []models.Genre{{Name: "ExistingGenre"}, {Name: "AnotherNewGenre"}}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureGenresExistTx(tx, genres)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, genres[0].ID)
	assert.NotZero(t, genres[1].ID)
}

// mergeActressData (lines 348-370): all update paths
func TestMovieRepository_MergeActressData_AllPaths(t *testing.T) {
	repo := &MovieRepository{}

	t.Run("ThumbURL update", func(t *testing.T) {
		existing := &models.Actress{}
		new := models.Actress{ThumbURL: "http://example.com/thumb.jpg"}
		assert.True(t, repo.upserter.mergeActressData(existing, new))
		assert.Equal(t, "http://example.com/thumb.jpg", existing.ThumbURL)
	})

	t.Run("FirstName update", func(t *testing.T) {
		existing := &models.Actress{}
		new := models.Actress{FirstName: "Yui"}
		assert.True(t, repo.upserter.mergeActressData(existing, new))
		assert.Equal(t, "Yui", existing.FirstName)
	})

	t.Run("LastName update", func(t *testing.T) {
		existing := &models.Actress{}
		new := models.Actress{LastName: "Hatano"}
		assert.True(t, repo.upserter.mergeActressData(existing, new))
		assert.Equal(t, "Hatano", existing.LastName)
	})

	t.Run("no update needed", func(t *testing.T) {
		existing := &models.Actress{ThumbURL: "existing", FirstName: "A", LastName: "B"}
		new := models.Actress{ThumbURL: "new", FirstName: "C", LastName: "D"}
		assert.False(t, repo.upserter.mergeActressData(existing, new))
		assert.Equal(t, "existing", existing.ThumbURL) // not overwritten
	})

	t.Run("partial update - only empty fields", func(t *testing.T) {
		existing := &models.Actress{FirstName: "Existing"}
		new := models.Actress{ThumbURL: "http://new.com/thumb.jpg", LastName: "New"}
		assert.True(t, repo.upserter.mergeActressData(existing, new))
		assert.Equal(t, "http://new.com/thumb.jpg", existing.ThumbURL)
		assert.Equal(t, "Existing", existing.FirstName) // not empty, not overwritten
		assert.Equal(t, "New", existing.LastName)
	})
}

// ensureActressesExistTx: DMM group with existing actress and merge
func TestMovieRepository_EnsureActressesExistTx_DMMGroup_Merge(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Pre-create actress with DMMID but no ThumbURL
	existing := models.Actress{DMMID: 8001, JapaneseName: "DMMA"}
	require.NoError(t, db.DB.Create(&existing).Error)

	actresses := []models.Actress{
		{DMMID: 8001, ThumbURL: "http://example.com/merged_thumb.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)

	// Verify ThumbURL was merged
	var updated models.Actress
	require.NoError(t, db.DB.First(&updated, existing.ID).Error)
	assert.Equal(t, "http://example.com/merged_thumb.jpg", updated.ThumbURL)
}

// ensureActressesExistTx: DMM group with new actress (race retry create)
func TestMovieRepository_EnsureActressesExistTx_DMMGroup_New(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	actresses := []models.Actress{
		{DMMID: 8002, JapaneseName: "DMMNew"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.NotZero(t, actresses[0].ID)
}

// ensureActressesExistTx: JP group with existing actress and merge
func TestMovieRepository_EnsureActressesExistTx_JPGroup_Merge(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	existing := models.Actress{JapaneseName: "JPMergeTarget"}
	require.NoError(t, db.DB.Create(&existing).Error)

	actresses := []models.Actress{
		{JapaneseName: "JPMergeTarget", ThumbURL: "http://example.com/jp_thumb.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)

	var updated models.Actress
	require.NoError(t, db.DB.First(&updated, existing.ID).Error)
	assert.Equal(t, "http://example.com/jp_thumb.jpg", updated.ThumbURL)
}

// ensureActressesExistTx: JP group with new actress (race retry create)
func TestMovieRepository_EnsureActressesExistTx_JPGroup_New(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	actresses := []models.Actress{
		{JapaneseName: "JPNewActress"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.NotZero(t, actresses[0].ID)
}

// ensureActressesExistTx: name group — both first+last
func TestMovieRepository_EnsureActressesExistTx_NameGroup_BothNames(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	existing := models.Actress{FirstName: "Yui", LastName: "Hatano"}
	require.NoError(t, db.DB.Create(&existing).Error)

	actresses := []models.Actress{
		{FirstName: "Yui", LastName: "Hatano", ThumbURL: "http://example.com/name_thumb.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)
}

// ensureActressesExistTx: name group — only first name
func TestMovieRepository_EnsureActressesExistTx_NameGroup_FirstOnly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	existing := models.Actress{FirstName: "UniqueFirst"}
	require.NoError(t, db.DB.Create(&existing).Error)

	actresses := []models.Actress{
		{FirstName: "UniqueFirst", ThumbURL: "http://example.com/first_thumb.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)
}

// ensureActressesExistTx: name group — only last name
func TestMovieRepository_EnsureActressesExistTx_NameGroup_LastOnly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	existing := models.Actress{LastName: "UniqueLast"}
	require.NoError(t, db.DB.Create(&existing).Error)

	actresses := []models.Actress{
		{LastName: "UniqueLast", ThumbURL: "http://example.com/last_thumb.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)
}

// ensureActressesExistTx: name group — new actress (not found, race retry create)
func TestMovieRepository_EnsureActressesExistTx_NameGroup_New(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	actresses := []models.Actress{
		{FirstName: "BrandNewFirst", LastName: "BrandNewLast"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.NotZero(t, actresses[0].ID)
}

// ensureActressesExistTx: name group — new actress, only first name
func TestMovieRepository_EnsureActressesExistTx_NameGroup_NewFirstOnly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	actresses := []models.Actress{
		{FirstName: "NewFirstOnly"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.NotZero(t, actresses[0].ID)
}

// ensureActressesExistTx: name group — new actress, only last name
func TestMovieRepository_EnsureActressesExistTx_NameGroup_NewLastOnly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	actresses := []models.Actress{
		{LastName: "NewLastOnly"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.NotZero(t, actresses[0].ID)
}

// FindByContentID (lines 408-423)
func TestMovieRepositoryMiss_FindByContentID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-FIND-CID")
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	contentID := strings.ToLower(strings.ReplaceAll("IPX-FIND-CID", "-", ""))
	found, err := repo.FindByContentID(context.TODO(), contentID)
	require.NoError(t, err)
	assert.Equal(t, "IPX-FIND-CID", found.ID)
}

// FindByContentID: not found
func TestMovieRepositoryMiss_FindByContentID_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	_, err := repo.FindByContentID(context.TODO(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// FindByID: not found
func TestMovieRepositoryMiss_FindByID_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	_, err := repo.FindByID(context.TODO(), "NONEXISTENT-999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// Delete (lines 423-488): full delete with associations
func TestMovieRepository_Delete_WithTranslationsAndTags(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-DEL-FULL")
	movie.Genres = []models.Genre{{Name: "DelGenre"}}
	movie.Actresses = []models.Actress{{DMMID: 9200, JapaneseName: "DelActress"}}
	movie.Translations = []models.MovieTranslation{
		{Language: "en", Title: "English"},
	}

	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	err = repo.Delete(context.TODO(), "IPX-DEL-FULL")
	require.NoError(t, err)

	_, err = repo.FindByID(context.TODO(), "IPX-DEL-FULL")
	require.Error(t, err)
}

// Delete: movie with empty ContentID (should return nil)
func TestMovieRepository_Delete_EmptyContentID(t *testing.T) {
	db := newDatabaseTestDB(t)

	// Create a movie directly with GORM to have empty ContentID
	movie := models.Movie{
		ContentID:    "del-empty-cid",
		ID:           "DEL-EMPTY",
		DisplayTitle: "Empty CID Test",
	}
	require.NoError(t, db.DB.Omit("Actresses", "Genres", "Translations").Create(&movie).Error)

	// Now clear ContentID to test the empty check
	require.NoError(t, db.DB.Model(&models.Movie{}).Where("content_id = ?", "del-empty-cid").Update("content_id", "").Error)

	repo := NewMovieRepository(db)
	err := repo.Delete(context.TODO(), "DEL-EMPTY")
	assert.NoError(t, err) // should return nil for empty ContentID
}

// Delete: non-existent ID returns nil
func TestMovieRepository_Delete_NonExistentReturnsNil(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	err := repo.Delete(context.TODO(), "GONE-999")
	assert.NoError(t, err)
}

// List (lines 446-448)
func TestMovieRepositoryMiss_List(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create several movies
	for _, id := range []string{"IPX-LIST-001", "IPX-LIST-002", "IPX-LIST-003"} {
		movie := createTestMovie(id)
		err := repo.Create(context.TODO(), movie)
		require.NoError(t, err)
	}

	movies, err := repo.List(context.TODO(), 2, 0)
	require.NoError(t, err)
	assert.Len(t, movies, 2)

	movies, err = repo.List(context.TODO(), 2, 2)
	require.NoError(t, err)
	assert.Len(t, movies, 1)
}

// UpsertWithTranslations: duplicate key path where ContentID create returns ErrDuplicatedKey
func TestMovieRepository_UpsertWithTranslations_DuplicateKeyPath(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create initial movie
	movie1 := createTestMovie("IPX-DUPKEY")
	movie1.Genres = []models.Genre{{Name: "DupGenre"}}
	movie1.Actresses = []models.Actress{{DMMID: 9300, JapaneseName: "DupActress"}}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie1, nil, nil)
	require.NoError(t, err)

	// Upsert with same ContentID — exercises the existing-found branch
	movie2 := createTestMovie("IPX-DUPKEY")
	movie2.Genres = []models.Genre{{Name: "DupGenre"}, {Name: "NewDupGenre"}}
	movie2.Actresses = []models.Actress{{DMMID: 9300, JapaneseName: "DupActress"}, {DMMID: 9301, JapaneseName: "NewDupActress"}}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Len(t, result.Genres, 2)
	assert.Len(t, result.Actresses, 2)
}

// ensureActressesExistTx: name group — new actress found via race retry, merge data
func TestMovieRepository_EnsureActressesExistTx_NameGroup_RaceRetryWithMerge(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// We need to test the race retry create path where:
	// 1. Name group actress not found initially
	// 2. Create fails with duplicate key
	// 3. FindExisting finds the actress and merges data

	// This is hard to trigger directly with SQLite, but we can test the
	// normal path where the actress is not found and is successfully created
	actresses := []models.Actress{
		{FirstName: "RaceNewFirst", LastName: "RaceNewLast", ThumbURL: "http://example.com/race.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.NotZero(t, actresses[0].ID)
}

// UpsertWithTranslations: movie with ID-only lookup (no ContentID, match by ID)
func TestMovieRepository_UpsertWithTranslations_IDOnlyLookup(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create a movie that has a ContentID different from auto-generated
	movie := &models.Movie{
		ContentID:    "specialcid123",
		ID:           "SPECIAL-001",
		DisplayTitle: "Original Special",
		Title:        "Original Special",
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	// Now upsert with ID matching but no ContentID set — should find by ID and adopt ContentID
	movie2 := &models.Movie{
		ID:           "SPECIAL-001",
		DisplayTitle: "Updated Special",
		Title:        "Updated Special",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "Updated Special", result.DisplayTitle)
	assert.Equal(t, "specialcid123", result.ContentID)
}
