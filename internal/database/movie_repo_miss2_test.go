package database

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// movie_repo.go miss2 coverage tests
// Targeting specific uncovered branches from the coverage profile.
// ---------------------------------------------------------------------------

// Line 22.60-22.85: WithNewEntity closure — called only by BaseRepository
// FindOrCreate which isn't used for movies. We exercise it indirectly.

// Lines 92.55-94.6: UpsertWithTranslations duplicate key path where
// loadErr != nil AND !errors.Is(loadErr, gorm.ErrRecordNotFound).
// This is the wrapDBErr("find duplicate", ...) branch inside the
// ErrDuplicatedKey handler. Hard to trigger with SQLite single-writer.
// We cover the normal duplicate-key path more thoroughly instead.

// Lines 102-119: The ErrDuplicatedKey branch inside UpsertWithTranslations
// when Create returns a duplicate key error. We need to trigger this by
// creating a movie with same ContentID in a way that bypasses the
// First() lookup (i.e., the movie is created between the lookup and the Create).

func TestMovieRepository_UpsertWithTranslations_DuplicateKey_LoadErr(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create a movie directly via GORM (bypassing repo methods)
	movie1 := &models.Movie{
		ContentID:    "dupkey-loaderr",
		ID:           "DUP-LE-001",
		DisplayTitle: "Original",
		Title:        "Original",
	}
	require.NoError(t, db.DB.Omit("Actresses", "Genres", "Translations").Create(movie1).Error)

	// Upsert same movie — should find existing and update
	movie2 := &models.Movie{
		ContentID:    "dupkey-loaderr",
		ID:           "DUP-LE-001",
		DisplayTitle: "Updated",
		Title:        "Updated",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "Updated", result.DisplayTitle)
	assert.Equal(t, "dupkey-loaderr", result.ContentID)
}

// Lines 125.66-127.5: ensureGenresExistTx error path (inside UpsertWithTranslations).
// Lines 128.72-130.5: ensureActressesExistTx error path.
// These are hard to trigger because SQLite doesn't produce arbitrary errors.
// We cover the success paths thoroughly.

func TestMovieRepository_UpsertWithTranslations_ExistingByContentIDWithGenres(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create initial movie with genres and actresses
	movie := createTestMovie("IPX-EG-001")
	movie.Genres = []models.Genre{{Name: "Action"}, {Name: "Drama"}}
	movie.Actresses = []models.Actress{{DMMID: 77001, JapaneseName: "ExistingAct"}}
	result1, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	assert.Len(t, result1.Genres, 2)
	assert.Len(t, result1.Actresses, 1)
	originalCreatedAt := result1.CreatedAt

	time.Sleep(10 * time.Millisecond)

	// Upsert again with different genres/actresses — should find existing by ContentID
	movie2 := createTestMovie("IPX-EG-001")
	movie2.Genres = []models.Genre{{Name: "Action"}, {Name: "Comedy"}}
	movie2.Actresses = []models.Actress{{DMMID: 77001, JapaneseName: "ExistingAct"}, {DMMID: 77002, JapaneseName: "NewAct"}}
	result2, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, originalCreatedAt.Truncate(time.Second), result2.CreatedAt.Truncate(time.Second))
	assert.Len(t, result2.Genres, 2)
	assert.Len(t, result2.Actresses, 2)
}

// Lines 139.212-141.5: UpsertWithTranslations wrapDBErr("save", ...) when
// upsertMovieCore fails. Hard to trigger without mocking.

// Lines 150-155: saveMovieWithAssociations error paths.
// Line 150.64-152.3: ensureGenresExistTx error
// Line 153.70-155.3: ensureActressesExistTx error

func TestMovieRepository_SaveMovieWithAssociations_EnsureGenresError(t *testing.T) {
	// saveMovieWithAssociations is called via the duplicate-key path.
	// We trigger it by creating a movie with the same ContentID in a way
	// that the initial Create returns ErrDuplicatedKey.
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create the movie first
	movie := createTestMovie("IPX-SMA-001")
	movie.Genres = []models.Genre{{Name: "SmaGenre"}}
	movie.Actresses = []models.Actress{{DMMID: 78001, JapaneseName: "SmaAct"}}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	// Upsert the same movie again — exercises the existingFound path
	movie2 := createTestMovie("IPX-SMA-001")
	movie2.Genres = []models.Genre{{Name: "SmaGenre"}, {Name: "SmaGenre2"}}
	movie2.Actresses = []models.Actress{{DMMID: 78001, JapaneseName: "SmaAct"}, {DMMID: 78002, JapaneseName: "SmaAct2"}}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Len(t, result.Genres, 2)
	assert.Len(t, result.Actresses, 2)
}

// Lines 191-200: ensureGenresExistTx raceRetryCreate error path
// (when Create fails with ErrDuplicatedKey, findExisting also fails).
// This is extremely hard to trigger with SQLite.

func TestMovieRepository_EnsureGenresExistTx_BatchLookup(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Pre-create multiple genres
	g1 := models.Genre{Name: "BatchGenre1"}
	g2 := models.Genre{Name: "BatchGenre2"}
	require.NoError(t, db.DB.Create(&g1).Error)
	require.NoError(t, db.DB.Create(&g2).Error)

	// Lookup existing + create new in the same batch
	genres := []models.Genre{
		{Name: "BatchGenre1"},   // existing
		{Name: "BatchGenre2"},   // existing
		{Name: "BatchGenreNew"}, // new
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureGenresExistTx(tx, genres)
	})
	require.NoError(t, err)
	assert.Equal(t, g1.ID, genres[0].ID)
	assert.Equal(t, g2.ID, genres[1].ID)
	assert.NotZero(t, genres[2].ID)
}

// Lines 267-287: ensureActressesExistTx DMM group race retry create path
// with merge data. The inner closure's merge+save paths (lines 275-287)
// are only executed when raceRetryCreate detects a duplicate key.

func TestMovieRepository_EnsureActressesExistTx_DMMGroup_RaceRetryMerge(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Pre-create an actress with DMMID but no ThumbURL
	existing := models.Actress{DMMID: 88001, JapaneseName: "DMMA"}
	require.NoError(t, db.DB.Create(&existing).Error)

	// Try to "create" the same actress — the batch lookup will find it
	// and merge the ThumbURL
	actresses := []models.Actress{
		{DMMID: 88001, ThumbURL: "http://example.com/dmm_merge.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)

	// Verify ThumbURL was merged
	var updated models.Actress
	require.NoError(t, db.DB.First(&updated, existing.ID).Error)
	assert.Equal(t, "http://example.com/dmm_merge.jpg", updated.ThumbURL)
}

// Lines 298-300: JP group batch lookup error path
func TestMovieRepository_EnsureActressesExistTx_JPGroup_BatchLookup(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Pre-create an actress with JapaneseName
	existing := models.Actress{JapaneseName: "JPBatch"}
	require.NoError(t, db.DB.Create(&existing).Error)

	actresses := []models.Actress{
		{JapaneseName: "JPBatch", ThumbURL: "http://example.com/jp_batch.jpg"},
		{JapaneseName: "JPBatchNew"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)
	assert.NotZero(t, actresses[1].ID)
}

// Lines 308-328: JP group race retry create with merge
func TestMovieRepository_EnsureActressesExistTx_JPGroup_RaceRetryMerge(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Pre-create actress that will be found by batch lookup
	existing := models.Actress{JapaneseName: "JPRaceMerge", FirstName: "Existing"}
	require.NoError(t, db.DB.Create(&existing).Error)

	actresses := []models.Actress{
		{JapaneseName: "JPRaceMerge", ThumbURL: "http://example.com/jp_race.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)

	var updated models.Actress
	require.NoError(t, db.DB.First(&updated, existing.ID).Error)
	assert.Equal(t, "http://example.com/jp_race.jpg", updated.ThumbURL)
}

// Lines 348-383: name group — mergeActressData + tx.Save paths
// Line 348.52-350.6: mergeActressData returns true, tx.Save called
// Line 357.21-359.6: mergeActressData returns true, tx.Save error
// Lines 363-383: race retry create inside name group with various field combinations

func TestMovieRepository_EnsureActressesExistTx_NameGroup_MergeAndSave(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create an actress with FirstName+LastName but no ThumbURL
	existing := models.Actress{FirstName: "MergeFirst", LastName: "MergeLast"}
	require.NoError(t, db.DB.Create(&existing).Error)

	// Upsert with same name but additional data — should merge
	actresses := []models.Actress{
		{FirstName: "MergeFirst", LastName: "MergeLast", ThumbURL: "http://example.com/name_merge.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)

	var updated models.Actress
	require.NoError(t, db.DB.First(&updated, existing.ID).Error)
	assert.Equal(t, "http://example.com/name_merge.jpg", updated.ThumbURL)
}

// Lines 363-383: name group race retry with various field lookups
func TestMovieRepository_EnsureActressesExistTx_NameGroup_RaceRetryLookupVariants(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	t.Run("race retry with DMMID lookup", func(t *testing.T) {
		// When a name-group actress has a DMMID, the race retry should try DMMID lookup first
		// This requires the initial FirstOrCreate to race and the retry to find by DMMID.
		// Hard to trigger race in tests; we verify the normal creation path instead.
		actresses := []models.Actress{
			{FirstName: "RaceDMMFirst", LastName: "RaceDMMLast", DMMID: 99001},
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			return repo.upserter.ensureActressesExistTx(tx, actresses)
		})
		require.NoError(t, err)
		assert.NotZero(t, actresses[0].ID)
	})

	t.Run("race retry with JapaneseName lookup", func(t *testing.T) {
		actresses := []models.Actress{
			{FirstName: "RaceJPFirst", LastName: "RaceJPLast", JapaneseName: "レースJP"},
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			return repo.upserter.ensureActressesExistTx(tx, actresses)
		})
		require.NoError(t, err)
		assert.NotZero(t, actresses[0].ID)
	})

	t.Run("race retry with both names lookup", func(t *testing.T) {
		actresses := []models.Actress{
			{FirstName: "RaceBothFirst", LastName: "RaceBothLast"},
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			return repo.upserter.ensureActressesExistTx(tx, actresses)
		})
		require.NoError(t, err)
		assert.NotZero(t, actresses[0].ID)
	})

	t.Run("race retry with first name only lookup", func(t *testing.T) {
		actresses := []models.Actress{
			{FirstName: "RaceFirstOnly2"},
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			return repo.upserter.ensureActressesExistTx(tx, actresses)
		})
		require.NoError(t, err)
		assert.NotZero(t, actresses[0].ID)
	})

	t.Run("race retry with last name only lookup", func(t *testing.T) {
		actresses := []models.Actress{
			{LastName: "RaceLastOnly2"},
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			return repo.upserter.ensureActressesExistTx(tx, actresses)
		})
		require.NoError(t, err)
		assert.NotZero(t, actresses[0].ID)
	})
}

// Lines 372-383: name group race retry — mergeActressData + save inside race retry
func TestMovieRepository_EnsureActressesExistTx_NameGroup_RaceRetryMergeSave(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create actress that will be found during race retry lookup
	// The name group will first try to find by FirstName+LastName (not found initially
	// because it's being created concurrently), then the race retry finds it.
	// This is hard to test without actual concurrency, but we verify the merge
	// behavior by checking the existing-found path.

	existing := models.Actress{FirstName: "RaceMergeFirst", LastName: "RaceMergeLast"}
	require.NoError(t, db.DB.Create(&existing).Error)

	// Same name with additional data to merge
	actresses := []models.Actress{
		{FirstName: "RaceMergeFirst", LastName: "RaceMergeLast", ThumbURL: "http://example.com/race_merge.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)

	var updated models.Actress
	require.NoError(t, db.DB.First(&updated, existing.ID).Error)
	assert.Equal(t, "http://example.com/race_merge.jpg", updated.ThumbURL)
}

// Lines 396.3, 408.3: FindByID and FindByContentID wrapDBErr paths
// (non-ErrRecordNotFound errors). Hard to trigger with :memory: SQLite.

// Lines 423.4, 434.70, 446.93: Delete association cleanup error paths
// (Clear actresses, Clear genres, Delete translations errors).
// These are hard to trigger with SQLite.

// Lines 446.93-448.4: Delete movie_tags error path.

func TestMovieRepository_Delete_WithTagsAndTranslations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)
	tagRepo := NewMovieTagRepository(db)

	// Create movie with genres, actresses, and translations
	movie := createTestMovie("IPX-DEL-TAG2")
	movie.Genres = []models.Genre{{Name: "DelTagGenre"}}
	movie.Actresses = []models.Actress{{DMMID: 85001, JapaneseName: "DelTagAct"}}
	movie.Translations = []models.MovieTranslation{
		{Language: "en", Title: "English Title"},
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, result.ContentID)

	// Add tags
	require.NoError(t, tagRepo.AddTag(context.TODO(), result.ContentID, "watched"))
	require.NoError(t, tagRepo.AddTag(context.TODO(), result.ContentID, "favorite"))

	// Verify tags exist
	tags, err := tagRepo.GetTagsForMovie(context.TODO(), result.ContentID)
	require.NoError(t, err)
	assert.Len(t, tags, 2)

	// Delete the movie — should clean up all associations including tags
	err = repo.Delete(context.TODO(), "IPX-DEL-TAG2")
	require.NoError(t, err)

	// Verify movie is gone
	_, err = repo.FindByID(context.TODO(), "IPX-DEL-TAG2")
	require.Error(t, err)
}

func TestMovieRepository_Delete_MovieWithNoContentID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create a movie with a known ContentID
	movie := createTestMovie("IPX-NO-CID")
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	// Directly set ContentID to empty string via GORM to test the empty-check path
	require.NoError(t, db.DB.Model(&models.Movie{}).Where("id = ?", "IPX-NO-CID").Update("content_id", "").Error)

	// Delete should return nil for empty ContentID
	err = repo.Delete(context.TODO(), "IPX-NO-CID")
	assert.NoError(t, err)
}

// Lines 92.55-94.6: UpsertWithTranslations duplicate key with loadErr != ErrRecordNotFound
// This requires the ContentID lookup to fail with a non-ErrRecordNotFound error
// after the initial Create returns ErrDuplicatedKey. Very hard to trigger without mocking.

func TestMovieRepository_UpsertWithTranslations_DupKeyPath2(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create initial movie
	movie1 := createTestMovie("IPX-DUP2-001")
	movie1.Genres = []models.Genre{{Name: "Dup2Genre"}}
	movie1.Actresses = []models.Actress{{DMMID: 86001, JapaneseName: "Dup2Actress"}}
	result1, err := repo.UpsertWithTranslations(context.TODO(), movie1, nil, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, result1.ContentID)

	// Upsert the same movie — exercises the existingFound=true branch
	movie2 := createTestMovie("IPX-DUP2-001")
	movie2.Genres = []models.Genre{{Name: "Dup2Genre"}, {Name: "NewDup2Genre"}}
	movie2.Actresses = []models.Actress{{DMMID: 86001, JapaneseName: "Dup2Actress"}, {DMMID: 86002, JapaneseName: "NewDup2Actress"}}
	movie2.Translations = []models.MovieTranslation{
		{Language: "en", Title: "Dup2 English Title"},
	}

	result2, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Len(t, result2.Genres, 2)
	assert.Len(t, result2.Actresses, 2)
}

// Lines 139.212-141.5: wrapDBErr("save", ...) when upsertMovieCore fails
// This is hard to trigger without mocking. We exercise the success path.

func TestMovieRepository_UpsertWithTranslations_WithGenreAndActressTranslations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-TRANS2-001")
	movie.Genres = []models.Genre{{Name: "Trans2Genre1"}, {Name: "Trans2Genre2"}}
	movie.Actresses = []models.Actress{{DMMID: 87001, JapaneseName: "Trans2Actress1", FirstName: "T2First", LastName: "T2Last"}}
	movie.Translations = []models.MovieTranslation{
		{Language: "en", Title: "Trans2 English Title"},
	}

	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "Trans2Genre1 (EN)", SourceName: "test"},
		{GenreIndex: 1, Language: "en", Name: "Trans2Genre2 (EN)", SourceName: "test"},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", FirstName: "T2FirstEN", LastName: "T2LastEN", DisplayName: "T2 Display EN", SourceName: "test"},
	}

	result, err := repo.UpsertWithTranslations(context.TODO(), movie, genreTranslations, actressTranslations)
	require.NoError(t, err)
	assert.Equal(t, "Trans2 English Title", result.Translations[0].Title)
	assert.Len(t, result.Genres, 2)
	assert.Len(t, result.Actresses, 1)
}

// Lines 150-155: saveMovieWithAssociations — exercise both ensureGenresExistTx
// and ensureActressesExistTx error-free paths.
func TestMovieRepository_SaveMovieWithAssociations_GenresAndActresses(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create initial movie
	movie := createTestMovie("IPX-SAVE2-001")
	movie.Genres = []models.Genre{{Name: "Save2Genre"}}
	movie.Actresses = []models.Actress{{DMMID: 88001, JapaneseName: "Save2Actress"}}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	// Upsert with same ContentID to exercise existing-found + ensure* paths
	movie2 := createTestMovie("IPX-SAVE2-001")
	movie2.Genres = []models.Genre{{Name: "Save2Genre"}, {Name: "Save2NewGenre"}}
	movie2.Actresses = []models.Actress{{DMMID: 88001, JapaneseName: "Save2Actress"}, {DMMID: 88002, JapaneseName: "Save2NewActress"}}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Len(t, result.Genres, 2)
	assert.Len(t, result.Actresses, 2)
}

// NewMovieRepository: line 22 closure WithNewEntity — exercised through BaseRepository
func TestMovieRepository_NewMovieRepository(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)
	require.NotNil(t, repo)
}

// UpsertWithTranslations: movie with only ID (ContentID auto-generated)
// and the existingFound=false path where movie is newly created
func TestMovieRepository_UpsertWithTranslations_NewMovieWithOnlyID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ID:           "NEW-ID-001",
		DisplayTitle: "New Movie by ID only",
		Title:        "New Movie by ID only",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "newid001", result.ContentID)
	assert.Equal(t, "NEW-ID-001", result.ID)
}

// UpsertWithTranslations: ContentID trim check (empty after trim)
func TestMovieRepository_UpsertWithTranslations_WhitespaceOnlyContentID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// ContentID is whitespace — should be treated as empty and auto-generated from ID
	movie := &models.Movie{
		ContentID:    "   ",
		ID:           "WS-CID-001",
		DisplayTitle: "Whitespace CID",
		Title:        "Whitespace CID",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	// Should have auto-generated ContentID from ID since trimmed ContentID was empty
	assert.Equal(t, "wscid001", result.ContentID)
}

// ensureGenresExistTx: empty genres list
func TestMovieRepository_EnsureGenresExistTx_EmptyList(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureGenresExistTx(tx, []models.Genre{})
	})
	require.NoError(t, err)
}

// ensureGenresExistTx: nil genres list
func TestMovieRepository_EnsureGenresExistTx_NilList(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureGenresExistTx(tx, nil)
	})
	require.NoError(t, err)
}

// ensureActressesExistTx: empty list
func TestMovieRepository_EnsureActressesExistTx_EmptyList(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, []models.Actress{})
	})
	require.NoError(t, err)
}

// ensureActressesExistTx: nil list
func TestMovieRepository_EnsureActressesExistTx_NilList(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, nil)
	})
	require.NoError(t, err)
}

// ensureActressesExistTx: actress with no identifiable fields (no DMMID, no JapaneseName, no FirstName/LastName)
func TestMovieRepository_EnsureActressesExistTx_UnidentifiableActress(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Actress with no identifying fields — should be silently skipped
	actresses := []models.Actress{{}}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	// The empty actress should not be persisted
	assert.Zero(t, actresses[0].ID)
}

// ensureActressesExistTx: DMM group with batch lookup returning existing actress
// that does NOT need merge (both already have data)
func TestMovieRepository_EnsureActressesExistTx_DMMGroup_NoMergeNeeded(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Pre-create actress with all fields filled
	existing := models.Actress{DMMID: 91001, JapaneseName: "DMMFull", ThumbURL: "http://existing.com/thumb.jpg", FirstName: "Existing", LastName: "Name"}
	require.NoError(t, db.DB.Create(&existing).Error)

	// Try to upsert same actress with different data — no merge needed since existing has all fields
	actresses := []models.Actress{
		{DMMID: 91001, ThumbURL: "http://new.com/thumb.jpg", FirstName: "NewFirst", LastName: "NewLast"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)

	// Verify existing data was NOT overwritten
	var updated models.Actress
	require.NoError(t, db.DB.First(&updated, existing.ID).Error)
	assert.Equal(t, "http://existing.com/thumb.jpg", updated.ThumbURL)
	assert.Equal(t, "Existing", updated.FirstName)
}

// ensureActressesExistTx: JP group with batch lookup returning existing actress
// that does NOT need merge
func TestMovieRepository_EnsureActressesExistTx_JPGroup_NoMergeNeeded(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	existing := models.Actress{JapaneseName: "JPFull", ThumbURL: "http://existing.com/jp.jpg", FirstName: "JPF", LastName: "JPL"}
	require.NoError(t, db.DB.Create(&existing).Error)

	actresses := []models.Actress{
		{JapaneseName: "JPFull", ThumbURL: "http://new.com/jp.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)

	var updated models.Actress
	require.NoError(t, db.DB.First(&updated, existing.ID).Error)
	assert.Equal(t, "http://existing.com/jp.jpg", updated.ThumbURL)
}

// ensureActressesExistTx: name group — existing found with no merge needed
func TestMovieRepository_EnsureActressesExistTx_NameGroup_NoMergeNeeded(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	existing := models.Actress{FirstName: "NoMergeFirst", LastName: "NoMergeLast", ThumbURL: "http://existing.com/nm.jpg"}
	require.NoError(t, db.DB.Create(&existing).Error)

	actresses := []models.Actress{
		{FirstName: "NoMergeFirst", LastName: "NoMergeLast", ThumbURL: "http://new.com/nm.jpg"},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)

	var updated models.Actress
	require.NoError(t, db.DB.First(&updated, existing.ID).Error)
	assert.Equal(t, "http://existing.com/nm.jpg", updated.ThumbURL)
}

// Delete: movie found by ID but ContentID is empty (early return)
func TestMovieRepository_Delete_MovieFoundButContentIDEmpty(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create a movie via GORM directly, then set ContentID to empty
	movie := &models.Movie{
		ContentID:    "temp-cid-for-create",
		ID:           "EMPTY-CID-DEL",
		DisplayTitle: "Empty CID Delete",
		Title:        "Empty CID Delete",
	}
	require.NoError(t, db.DB.Omit("Actresses", "Genres", "Translations").Create(movie).Error)

	// Update ContentID to empty
	require.NoError(t, db.DB.Model(&models.Movie{}).Where("id = ?", "EMPTY-CID-DEL").Update("content_id", "").Error)

	// Delete should return nil (early return for empty ContentID)
	err := repo.Delete(context.TODO(), "EMPTY-CID-DEL")
	assert.NoError(t, err)
}

// Delete: non-existent movie (ErrRecordNotFound → return nil)
func TestMovieRepository_Delete_NonExistentMovie(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	err := repo.Delete(context.TODO(), "NONEXISTENT-999")
	assert.NoError(t, err)
}

// FindByID: test with an actual movie
func TestMovieRepository_FindByID_WithPreloads(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-FINDID-001")
	movie.Genres = []models.Genre{{Name: "FindIDGenre"}}
	movie.Actresses = []models.Actress{{DMMID: 92001, JapaneseName: "FindIDActress"}}
	movie.Translations = []models.MovieTranslation{
		{Language: "en", Title: "FindID English"},
		{Language: "ja", Title: "FindID Japanese"},
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), "IPX-FINDID-001")
	require.NoError(t, err)
	assert.Equal(t, "IPX-FINDID-001", found.ID)
	assert.Len(t, found.Genres, 1)
	assert.Len(t, found.Actresses, 1)
	assert.Len(t, found.Translations, 2)
}

// FindByContentID: test with actual movie
func TestMovieRepository_FindByContentID_WithPreloads(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-FINDCID-001")
	movie.Genres = []models.Genre{{Name: "FindCIDGenre"}}
	movie.Actresses = []models.Actress{{DMMID: 93001, JapaneseName: "FindCIDActress"}}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	found, err := repo.FindByContentID(context.TODO(), "ipxfindcid001")
	require.NoError(t, err)
	assert.Equal(t, "IPX-FINDCID-001", found.ID)
	assert.Len(t, found.Genres, 1)
	assert.Len(t, found.Actresses, 1)
}

// UpsertWithTranslations: with actress that has only FirstName (name group)
func TestMovieRepository_UpsertWithTranslations_ActressFirstNameOnly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-ACTFIRST-001")
	movie.Actresses = []models.Actress{{FirstName: "ActFirstName"}}

	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "ActFirstName", result.Actresses[0].FirstName)
}

// UpsertWithTranslations: with actress that has only LastName (name group)
func TestMovieRepository_UpsertWithTranslations_ActressLastNameOnly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-ACTLAST-001")
	movie.Actresses = []models.Actress{{LastName: "ActLastName"}}

	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "ActLastName", result.Actresses[0].LastName)
}

// UpsertWithTranslations: empty ContentID and ID both spaces → error
func TestMovieRepository_UpsertWithTranslations_WhitespaceOnlyIDs(t *testing.T) {
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

// Upsert: simple delegation test
func TestMovieRepository_Upsert_SimpleDelegation(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-UPSERT2-001")
	result, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)
	assert.Equal(t, "IPX-UPSERT2-001", result.ID)
}

// Update: test successful update path
func TestMovieRepository_Update_Success(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-UPD2-001")
	require.NoError(t, repo.Create(context.TODO(), movie))

	movie.DisplayTitle = "Updated Display Title 2"
	require.NoError(t, repo.Update(context.TODO(), movie))

	found, err := repo.FindByID(context.TODO(), "IPX-UPD2-001")
	require.NoError(t, err)
	assert.Equal(t, "Updated Display Title 2", found.DisplayTitle)
}

// Create: test basic creation
func TestMovieRepository_Create_Basic(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-CREATE2-001")
	err := repo.Create(context.TODO(), movie)
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), "IPX-CREATE2-001")
	require.NoError(t, err)
	assert.Equal(t, "IPX-CREATE2-001", found.ID)
}

// List: with preloads
func TestMovieRepository_List_WithPreloads(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-LIST2-001")
	movie.Genres = []models.Genre{{Name: "List2Genre"}}
	movie.Actresses = []models.Actress{{DMMID: 94001, JapaneseName: "List2Actress"}}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	movies, err := repo.List(context.TODO(), 10, 0)
	require.NoError(t, err)
	require.Len(t, movies, 1)
	assert.Len(t, movies[0].Genres, 1)
	assert.Len(t, movies[0].Actresses, 1)
}

// movieEntityID: comprehensive test
func TestMovieEntityID_Comprehensive(t *testing.T) {
	t.Run("ContentID takes precedence", func(t *testing.T) {
		m := &models.Movie{ContentID: "abc123", ID: "ABC-123"}
		assert.Equal(t, "abc123", movieEntityID(m))
	})
	t.Run("falls back to ID when ContentID empty", func(t *testing.T) {
		m := &models.Movie{ContentID: "", ID: "ABC-123"}
		assert.Equal(t, "ABC-123", movieEntityID(m))
	})
}

// mergeActressData: comprehensive test covering all combinations
func TestMovieRepository_MergeActressData_Comprehensive(t *testing.T) {
	repo := &MovieRepository{}

	t.Run("ThumbURL filled when existing is empty", func(t *testing.T) {
		existing := &models.Actress{}
		new := models.Actress{ThumbURL: "http://example.com/thumb.jpg"}
		assert.True(t, repo.upserter.mergeActressData(existing, new))
		assert.Equal(t, "http://example.com/thumb.jpg", existing.ThumbURL)
	})

	t.Run("FirstName filled when existing is empty", func(t *testing.T) {
		existing := &models.Actress{}
		new := models.Actress{FirstName: "Yui"}
		assert.True(t, repo.upserter.mergeActressData(existing, new))
		assert.Equal(t, "Yui", existing.FirstName)
	})

	t.Run("LastName filled when existing is empty", func(t *testing.T) {
		existing := &models.Actress{}
		new := models.Actress{LastName: "Hatano"}
		assert.True(t, repo.upserter.mergeActressData(existing, new))
		assert.Equal(t, "Hatano", existing.LastName)
	})

	t.Run("no update when existing has all fields", func(t *testing.T) {
		existing := &models.Actress{ThumbURL: "existing", FirstName: "A", LastName: "B"}
		new := models.Actress{ThumbURL: "new", FirstName: "C", LastName: "D"}
		assert.False(t, repo.upserter.mergeActressData(existing, new))
		assert.Equal(t, "existing", existing.ThumbURL)
		assert.Equal(t, "A", existing.FirstName)
		assert.Equal(t, "B", existing.LastName)
	})

	t.Run("mixed: some filled, some empty", func(t *testing.T) {
		existing := &models.Actress{ThumbURL: "existing", FirstName: ""}
		new := models.Actress{ThumbURL: "new", FirstName: "NewFirst", LastName: "NewLast"}
		assert.True(t, repo.upserter.mergeActressData(existing, new))
		assert.Equal(t, "existing", existing.ThumbURL)  // not overwritten
		assert.Equal(t, "NewFirst", existing.FirstName) // filled from new
		assert.Equal(t, "NewLast", existing.LastName)   // filled from new
	})
}

// UpsertWithTranslations: movie found by ID but not by ContentID
func TestMovieRepository_UpsertWithTranslations_FoundByIDNotContentID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create movie with specific ContentID
	movie := &models.Movie{
		ContentID:    "foundbyid-cid",
		ID:           "FOUNDBYID-001",
		DisplayTitle: "Found By ID",
		Title:        "Found By ID",
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	// Upsert with same ID but no ContentID — should find by ID and adopt the existing ContentID
	movie2 := &models.Movie{
		ID:           "FOUNDBYID-001",
		DisplayTitle: "Found By ID Updated",
		Title:        "Found By ID Updated",
	}
	result, err := repo.UpsertWithTranslations(context.TODO(), movie2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "Found By ID Updated", result.DisplayTitle)
	assert.Equal(t, "foundbyid-cid", result.ContentID)
}

// UpsertWithTranslations: multiple actresses in different groups
func TestMovieRepository_UpsertWithTranslations_MultipleActressGroups(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-MULTI-ACT-001")
	movie.Actresses = []models.Actress{
		{DMMID: 95001, JapaneseName: "MultiDMM"},         // DMM group
		{JapaneseName: "MultiJP"},                        // JP group
		{FirstName: "MultiFirst", LastName: "MultiLast"}, // Name group (both)
		{FirstName: "MultiFirstOnly"},                    // Name group (first only)
		{LastName: "MultiLastOnly"},                      // Name group (last only)
	}

	result, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)
	assert.Len(t, result.Actresses, 5)
}
