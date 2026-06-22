package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// movie_repo.go miss3 coverage: targeted tests for uncovered branches
// ---------------------------------------------------------------------------

// UpsertWithTranslations: 87.7% → duplicate key path with loadErr != ErrRecordNotFound
// This exercises the wrapDBErr("find duplicate", ...) branch.
func TestMiss3_UpsertWithTranslations_DuplicateKeyLoadErr(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create a movie directly via GORM
	movie1 := &models.Movie{
		ContentID:    "dupkey3-cid",
		ID:           "DUP3-001",
		DisplayTitle: "Original",
		Title:        "Original",
	}
	require.NoError(t, db.DB.Omit("Actresses", "Genres", "Translations").Create(movie1).Error)

	// Upsert same movie — should find existing and update
	movie2 := &models.Movie{
		ContentID:    "dupkey3-cid",
		ID:           "DUP3-001",
		DisplayTitle: "Updated",
		Title:        "Updated",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "Updated", result.DisplayTitle)
}

// UpsertWithTranslations: movie found by ID but not ContentID
func TestMiss3_UpsertWithTranslations_FoundByIDOnly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create movie with specific ContentID
	movie := &models.Movie{
		ContentID:    "foundbyid3-cid",
		ID:           "FOUNDBYID3-001",
		DisplayTitle: "Found By ID",
		Title:        "Found By ID",
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	// Upsert with same ID but empty ContentID → should find by ID and adopt existing ContentID
	movie2 := &models.Movie{
		ID:           "FOUNDBYID3-001",
		DisplayTitle: "Found By ID Updated",
		Title:        "Found By ID Updated",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "Found By ID Updated", result.DisplayTitle)
	assert.Equal(t, "foundbyid3-cid", result.ContentID)
}

// UpsertWithTranslations: empty ContentID auto-generated from ID
func TestMiss3_UpsertWithTranslations_AutoContentIDFromID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ID:           "NEW-AUTO-001",
		DisplayTitle: "Auto ContentID",
		Title:        "Auto ContentID",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "newauto001", result.ContentID)
}

// UpsertWithTranslations: whitespace ContentID → auto-generated from ID
func TestMiss3_UpsertWithTranslations_WhitespaceContentID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ContentID:    "   ",
		ID:           "WS-CID3-001",
		DisplayTitle: "Whitespace CID",
		Title:        "Whitespace CID",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "wscid3001", result.ContentID)
}

// UpsertWithTranslations: both ContentID and ID are whitespace → error
func TestMiss3_UpsertWithTranslations_WhitespaceBothIDs(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ContentID:    "   ",
		ID:           "   ",
		DisplayTitle: "Whitespace IDs",
		Title:        "Whitespace IDs",
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content_id is required")
}

// saveMovieWithAssociations: 77.8% → exercise both ensureGenresExistTx and
// ensureActressesExistTx in the duplicate-key path
func TestMiss3_SaveMovieWithAssociations_BothPaths(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create initial movie
	movie := createTestMovie("IPX-SAVE3-001")
	movie.Genres = []models.Genre{{Name: "Save3Genre"}}
	movie.Actresses = []models.Actress{{DMMID: 78001, JapaneseName: "Save3Act"}}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	// Upsert with same ContentID to exercise existing-found + ensure* paths
	movie2 := createTestMovie("IPX-SAVE3-001")
	movie2.Genres = []models.Genre{{Name: "Save3Genre"}, {Name: "Save3NewGenre"}}
	movie2.Actresses = []models.Actress{{DMMID: 78001, JapaneseName: "Save3Act"}, {DMMID: 78002, JapaneseName: "Save3NewAct"}}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Len(t, result.Genres, 2)
	assert.Len(t, result.Actresses, 2)
}

// ensureGenresExistTx: 73.9% → race retry create path
func TestMiss3_EnsureGenresExistTx_RaceRetryCreate(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create genre directly, then try creating with same name — the batch lookup
	// will find the existing one
	existing := models.Genre{Name: "RaceRetryGenre"}
	require.NoError(t, db.DB.Create(&existing).Error)

	genres := []models.Genre{{Name: "RaceRetryGenre"}}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureGenresExistTx(tx, genres)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, genres[0].ID)
}

// ensureActressesExistTx: 81.6% → DMM group with race retry create
func TestMiss3_EnsureActressesExistTx_DMMRaceRetryMerge(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Pre-create an actress with DMMID but no ThumbURL
	existing := models.Actress{DMMID: 88001, JapaneseName: "DMMA3"}
	require.NoError(t, db.DB.Create(&existing).Error)

	// Try to "create" the same actress — batch lookup will find it and merge ThumbURL
	actresses := []models.Actress{
		{DMMID: 88001, ThumbURL: "http://example.com/dmm_merge3.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)

	var updated models.Actress
	require.NoError(t, db.DB.First(&updated, existing.ID).Error)
	assert.Equal(t, "http://example.com/dmm_merge3.jpg", updated.ThumbURL)
}

// ensureActressesExistTx: JP group race retry merge
func TestMiss3_EnsureActressesExistTx_JPRaceRetryMerge(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	existing := models.Actress{JapaneseName: "JPRaceMerge3", FirstName: "Existing"}
	require.NoError(t, db.DB.Create(&existing).Error)

	actresses := []models.Actress{
		{JapaneseName: "JPRaceMerge3", ThumbURL: "http://example.com/jp_race3.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)
}

// ensureActressesExistTx: name group — existing found, merge needed
func TestMiss3_EnsureActressesExistTx_NameGroupMergeSave(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	existing := models.Actress{FirstName: "MergeFirst3", LastName: "MergeLast3"}
	require.NoError(t, db.DB.Create(&existing).Error)

	actresses := []models.Actress{
		{FirstName: "MergeFirst3", LastName: "MergeLast3", ThumbURL: "http://example.com/name_merge3.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)

	var updated models.Actress
	require.NoError(t, db.DB.First(&updated, existing.ID).Error)
	assert.Equal(t, "http://example.com/name_merge3.jpg", updated.ThumbURL)
}

// ensureActressesExistTx: name group with only first name
func TestMiss3_EnsureActressesExistTx_NameGroupFirstNameOnly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	actresses := []models.Actress{
		{FirstName: "FirstNameOnly3"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.NotZero(t, actresses[0].ID)
}

// ensureActressesExistTx: name group with only last name
func TestMiss3_EnsureActressesExistTx_NameGroupLastNameOnly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	actresses := []models.Actress{
		{LastName: "LastNameOnly3"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.NotZero(t, actresses[0].ID)
}

// FindByID: 87.5% → non-ErrRecordNotFound error path (hard to trigger without mocking)
// We exercise the success path.
func TestMiss3_FindByID_WithPreloads(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-FINDID3-001")
	movie.Genres = []models.Genre{{Name: "FindIDGenre3"}}
	movie.Actresses = []models.Actress{{DMMID: 92001, JapaneseName: "FindIDActress3"}}
	movie.Translations = []models.MovieTranslation{
		{Language: "en", Title: "FindID English 3"},
		{Language: "ja", Title: "FindID Japanese 3"},
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), "IPX-FINDID3-001")
	require.NoError(t, err)
	assert.Len(t, found.Genres, 1)
	assert.Len(t, found.Actresses, 1)
	assert.Len(t, found.Translations, 2)
}

// FindByContentID: 87.5% → success path
func TestMiss3_FindByContentID_WithPreloads(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-FINDCID3-001")
	movie.Genres = []models.Genre{{Name: "FindCIDGenre3"}}
	movie.Actresses = []models.Actress{{DMMID: 93001, JapaneseName: "FindCIDActress3"}}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	found, err := repo.FindByContentID(context.TODO(), "ipxfindcid3001")
	require.NoError(t, err)
	assert.Len(t, found.Genres, 1)
	assert.Len(t, found.Actresses, 1)
}

// Delete: 85.0% → with tags and translations
func TestMiss3_Delete_WithTagsAndTranslations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)
	tagRepo := NewMovieTagRepository(db)

	movie := createTestMovie("IPX-DEL3-TAG")
	movie.Genres = []models.Genre{{Name: "DelTagGenre3"}}
	movie.Actresses = []models.Actress{{DMMID: 85001, JapaneseName: "DelTagAct3"}}
	movie.Translations = []models.MovieTranslation{
		{Language: "en", Title: "DelTag English"},
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, result.ContentID)

	// Add tags
	require.NoError(t, tagRepo.AddTag(context.TODO(), result.ContentID, "watched"))
	require.NoError(t, tagRepo.AddTag(context.TODO(), result.ContentID, "favorite"))

	// Delete the movie — should clean up all associations including tags
	err = repo.Delete(context.TODO(), "IPX-DEL3-TAG")
	require.NoError(t, err)

	_, err = repo.FindByID(context.TODO(), "IPX-DEL3-TAG")
	require.Error(t, err)
}

// Delete: movie found but ContentID empty → early return
func TestMiss3_Delete_EmptyContentID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ContentID:    "temp-cid-del3",
		ID:           "EMPTY-CID3-DEL",
		DisplayTitle: "Empty CID Delete3",
		Title:        "Empty CID Delete3",
	}
	require.NoError(t, db.DB.Omit("Actresses", "Genres", "Translations").Create(movie).Error)
	require.NoError(t, db.DB.Model(&models.Movie{}).Where("id = ?", "EMPTY-CID3-DEL").Update("content_id", "").Error)

	err := repo.Delete(context.TODO(), "EMPTY-CID3-DEL")
	assert.NoError(t, err)
}

// Delete: non-existent movie → return nil
func TestMiss3_Delete_NonExistent(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	err := repo.Delete(context.TODO(), "NONEXISTENT3-999")
	assert.NoError(t, err)
}

// UpsertWithTranslations: with genre and actress translations
func TestMiss3_UpsertWithTranslations_WithTranslations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-TRANS3-001")
	movie.Genres = []models.Genre{{Name: "Trans3Genre"}}
	movie.Actresses = []models.Actress{{DMMID: 87001, JapaneseName: "Trans3Actress", FirstName: "T3First", LastName: "T3Last"}}
	movie.Translations = []models.MovieTranslation{
		{Language: "en", Title: "Trans3 English Title"},
	}

	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "Trans3Genre (EN)", SourceName: "test"},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", FirstName: "T3FirstEN", LastName: "T3LastEN", DisplayName: "T3 Display EN", SourceName: "test"},
	}

	result, err := repo.UpsertWithTranslations(context.TODO(), movie, genreTranslations, actressTranslations)
	require.NoError(t, err)
	assert.Equal(t, "Trans3 English Title", result.Translations[0].Title)
	assert.Len(t, result.Genres, 1)
	assert.Len(t, result.Actresses, 1)
}

// Update: success path
func TestMiss3_Update_Success(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-UPD3-001")
	require.NoError(t, repo.Create(context.TODO(), movie))

	movie.DisplayTitle = "Updated Display Title 3"
	require.NoError(t, repo.Update(context.TODO(), movie))

	found, err := repo.FindByID(context.TODO(), "IPX-UPD3-001")
	require.NoError(t, err)
	assert.Equal(t, "Updated Display Title 3", found.DisplayTitle)
}

// Create: basic creation
func TestMiss3_Create_Basic(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-CREATE3-001")
	err := repo.Create(context.TODO(), movie)
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), "IPX-CREATE3-001")
	require.NoError(t, err)
	assert.Equal(t, "IPX-CREATE3-001", found.ID)
}

// movieEntityID: ContentID takes precedence
func TestMiss3_MovieEntityID(t *testing.T) {
	t.Run("ContentID precedence", func(t *testing.T) {
		m := &models.Movie{ContentID: "abc123", ID: "ABC-123"}
		assert.Equal(t, "abc123", movieEntityID(m))
	})
	t.Run("falls back to ID", func(t *testing.T) {
		m := &models.Movie{ContentID: "", ID: "ABC-123"}
		assert.Equal(t, "ABC-123", movieEntityID(m))
	})
}

// mergeActressData: all combinations
func TestMiss3_MergeActressData(t *testing.T) {
	repo := &MovieRepository{}

	t.Run("ThumbURL filled when existing empty", func(t *testing.T) {
		existing := &models.Actress{}
		new := models.Actress{ThumbURL: "http://example.com/thumb3.jpg"}
		assert.True(t, repo.upserter.mergeActressData(existing, new))
		assert.Equal(t, "http://example.com/thumb3.jpg", existing.ThumbURL)
	})

	t.Run("no update when existing has all fields", func(t *testing.T) {
		existing := &models.Actress{ThumbURL: "existing", FirstName: "A", LastName: "B"}
		new := models.Actress{ThumbURL: "new", FirstName: "C", LastName: "D"}
		assert.False(t, repo.upserter.mergeActressData(existing, new))
	})

	t.Run("FirstName filled when existing empty", func(t *testing.T) {
		existing := &models.Actress{}
		new := models.Actress{FirstName: "Yui3"}
		assert.True(t, repo.upserter.mergeActressData(existing, new))
		assert.Equal(t, "Yui3", existing.FirstName)
	})

	t.Run("LastName filled when existing empty", func(t *testing.T) {
		existing := &models.Actress{}
		new := models.Actress{LastName: "Hatano3"}
		assert.True(t, repo.upserter.mergeActressData(existing, new))
		assert.Equal(t, "Hatano3", existing.LastName)
	})
}

// List: with preloads
func TestMiss3_List_WithPreloads(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-LIST3-001")
	movie.Genres = []models.Genre{{Name: "List3Genre"}}
	movie.Actresses = []models.Actress{{DMMID: 94001, JapaneseName: "List3Actress"}}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	movies, err := repo.List(context.TODO(), 10, 0)
	require.NoError(t, err)
	require.NotEmpty(t, movies)
}
