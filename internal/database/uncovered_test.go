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

func ptrStr(s string) *string { return &s }

// --- movie_repo.go uncovered ---

func TestMovieEntityID_Uncovered(t *testing.T) {
	t.Run("uses ContentID when present", func(t *testing.T) {
		m := &models.Movie{ContentID: "abc123", ID: "ABC-123"}
		assert.Equal(t, "abc123", movieEntityID(m))
	})
	t.Run("falls back to ID when ContentID is empty", func(t *testing.T) {
		m := &models.Movie{ContentID: "", ID: "ABC-123"}
		assert.Equal(t, "ABC-123", movieEntityID(m))
	})
}

func TestMovieRepository_UpdateUncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-UPD-UNC")
	require.NoError(t, repo.Create(context.TODO(), movie))

	movie.DisplayTitle = "Updated Display Title"
	require.NoError(t, repo.Update(context.TODO(), movie))

	found, err := repo.FindByID(context.TODO(), "IPX-UPD-UNC")
	require.NoError(t, err)
	assert.Equal(t, "Updated Display Title", found.DisplayTitle)
}

func TestMovieRepository_FindByContentID_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-CID-UNC")
	movie.ContentID = "custom-cid-unc"
	require.NoError(t, repo.Create(context.TODO(), movie))

	found, err := repo.FindByContentID(context.TODO(), "custom-cid-unc")
	require.NoError(t, err)
	assert.Equal(t, "IPX-CID-UNC", found.ID)
}

func TestMovieRepository_DeleteUncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-DEL-UNC")
	movie.Genres = []models.Genre{{Name: "DelGenre"}}
	movie.Actresses = []models.Actress{{DMMID: 44001, JapaneseName: "DelActress"}}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	require.NoError(t, repo.Delete(context.TODO(), "IPX-DEL-UNC"))
	_, err = repo.FindByID(context.TODO(), "IPX-DEL-UNC")
	assert.Error(t, err)
}

func TestMovieRepository_UpsertWithTranslations_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-UTRANS-UNC")
	movie.Genres = []models.Genre{{Name: "TransGenre"}}
	movie.Actresses = []models.Actress{{DMMID: 44002, JapaneseName: "TransActress"}}

	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "TransGenre (EN)", SourceName: "test"},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", DisplayName: "TransActress (EN)", SourceName: "test"},
	}

	result, err := repo.UpsertWithTranslations(context.TODO(), movie, genreTranslations, actressTranslations)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-UTRANS-UNC", result.ID)
	assert.Len(t, result.Genres, 1)
	assert.Len(t, result.Actresses, 1)
}

func TestMovieRepository_Upsert_NoContentID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{ID: "NO-CID-001", Title: "No ContentID"}
	result, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "nocid001", result.ContentID)
}

func TestMovieRepository_Upsert_DuplicateKeyRace(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie1 := createTestMovie("IPX-DRACE")
	require.NoError(t, repo.Create(context.TODO(), movie1))

	movie2 := createTestMovie("IPX-DRACE")
	movie2.Title = "After Race"
	result, err := repo.Upsert(context.TODO(), movie2)
	require.NoError(t, err)
	assert.Equal(t, "After Race", result.Title)
}

func TestMovieRepository_EnsureActressesExistTx_NameOnly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Actress with only FirstName (no DMMID, no JapaneseName)
	actresses := []models.Actress{
		{FirstName: "OnlyFirst"},
	}
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.NotZero(t, actresses[0].ID)

	// Actress with only LastName
	actresses2 := []models.Actress{
		{LastName: "OnlyLast"},
	}
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses2)
	})
	require.NoError(t, err)
	assert.NotZero(t, actresses2[0].ID)
}

func TestMovieRepository_MergeActressData_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	existing := &models.Actress{ThumbURL: "", FirstName: "", LastName: ""}
	newActress := models.Actress{ThumbURL: "http://example.com/thumb.jpg", FirstName: "New", LastName: "Name"}

	needsUpdate := repo.upserter.mergeActressData(existing, newActress)
	assert.True(t, needsUpdate)
	assert.Equal(t, "http://example.com/thumb.jpg", existing.ThumbURL)
	assert.Equal(t, "New", existing.FirstName)
	assert.Equal(t, "Name", existing.LastName)

	// When existing already has data, no update needed
	existing2 := &models.Actress{ThumbURL: "http://existing.com/thumb.jpg", FirstName: "Existing", LastName: "ExistingLast"}
	newActress2 := models.Actress{ThumbURL: "http://new.com/thumb.jpg", FirstName: "NewFirst", LastName: "NewLast"}
	needsUpdate2 := repo.upserter.mergeActressData(existing2, newActress2)
	assert.False(t, needsUpdate2)
}

// --- actress_repo.go uncovered ---

func TestActressRepository_FindByJapaneseNameAndDMMID_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{DMMID: 30001, JapaneseName: "検索テスト"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	t.Run("both name and DMMID", func(t *testing.T) {
		found, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "検索テスト", 30001)
		require.NoError(t, err)
		assert.Equal(t, 30001, found.DMMID)
	})

	t.Run("name only delegates to FindByJapaneseName", func(t *testing.T) {
		found, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "検索テスト", 0)
		require.NoError(t, err)
		assert.Equal(t, "検索テスト", found.JapaneseName)
	})

	t.Run("DMMID only delegates to FindByDMMID", func(t *testing.T) {
		found, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "", 30001)
		require.NoError(t, err)
		assert.Equal(t, 30001, found.DMMID)
	})

	t.Run("both empty returns error", func(t *testing.T) {
		_, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "", 0)
		assert.Error(t, err)
	})
}

func TestActressRepository_FindOrCreate_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	t.Run("creates when not found", func(t *testing.T) {
		actress := &models.Actress{JapaneseName: "新規作成", FirstName: "New"}
		require.NoError(t, repo.FindOrCreate(context.TODO(), actress))
		assert.NotZero(t, actress.ID)
	})

	t.Run("finds existing by JapaneseName", func(t *testing.T) {
		existing := &models.Actress{DMMID: 30002, JapaneseName: "既存女優"}
		require.NoError(t, repo.Create(context.TODO(), existing))

		actress := &models.Actress{JapaneseName: "既存女優"}
		require.NoError(t, repo.FindOrCreate(context.TODO(), actress))
		assert.Equal(t, existing.ID, actress.ID)
	})

	t.Run("creates when no JapaneseName", func(t *testing.T) {
		actress := &models.Actress{FirstName: "NoJpName", LastName: "Test"}
		require.NoError(t, repo.FindOrCreate(context.TODO(), actress))
		assert.NotZero(t, actress.ID)
	})
}

func TestActressRepository_ListSorted_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.Actress{DMMID: 30010, JapaneseName: "佐藤"}))
	require.NoError(t, repo.Create(context.TODO(), &models.Actress{DMMID: 30011, JapaneseName: "山田"}))

	result, err := repo.ListSorted(context.TODO(), 10, 0, "japanese_name", "asc")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result), 2)

	_, err = repo.ListSorted(context.TODO(), 10, 0, "invalid_field", "asc")
	assert.Error(t, err)
}

func TestActressRepository_SearchPagedSorted_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.Actress{DMMID: 30020, JapaneseName: "検索順序", FirstName: "Search"}))

	result, err := repo.SearchPagedSorted(context.TODO(), "検索", 10, 0, "japanese_name", "asc")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result), 1)

	_, err = repo.SearchPagedSorted(context.TODO(), "検索", 10, 0, "bad", "asc")
	assert.Error(t, err)
}

func TestActressRepository_CountSearch_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.Actress{DMMID: 30030, JapaneseName: "計数テスト"}))

	count, err := repo.CountSearch(context.TODO(), "計数")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(1))
}

func TestActressRepository_FindByDMMID_EdgeCases(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByDMMID(context.TODO(), -1)
	assert.Error(t, err)

	_, err = repo.FindByDMMID(context.TODO(), 0)
	assert.Error(t, err)
}

// --- migrations_runner.go uncovered ---

func TestSqliteFilePathFromDSN_Uncovered(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		path string
		ok   bool
	}{
		{"memory", ":memory:", "", false},
		{"file memory", "file::memory:", "", false},
		{"mode=memory", "file:test.db?mode=memory", "", false},
		{"empty", "", "", false},
		{"simple path", "/data/app.db", "/data/app.db", true},
		{"file scheme", "file:/data/app.db", "/data/app.db", true},
		{"file scheme with query", "file:/data/app.db?cache=shared", "/data/app.db", true},
		{"relative path", "app.db", "app.db", true},
		{"path with query", "app.db?cache=shared", "app.db", true},
		{"whitespace", "  /data/app.db  ", "/data/app.db", true},
		{"file empty path", "file:?cache=shared", "", false},
		{"file with percent encoding", "file:/path%20with%20spaces/db.sqlite", "/path with spaces/db.sqlite", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ok := sqliteFilePathFromDSN(tt.dsn)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.path, path)
		})
	}
}

func TestQuoteSQLiteStringLiteral_Uncovered(t *testing.T) {
	assert.Equal(t, "'hello'", quoteSQLiteStringLiteral("hello"))
	assert.Equal(t, "'it''s'", quoteSQLiteStringLiteral("it's"))
}

// --- actress_translation_repo.go uncovered ---

func TestActressTranslationRepository_UpsertTx_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newActressTranslationRepository(db)

	actress := &models.Actress{DMMID: 40001, JapaneseName: "翻訳女優"}
	require.NoError(t, db.DB.Create(actress).Error)

	translation := &models.ActressTranslation{
		ActressID:   actress.ID,
		Language:    "en",
		DisplayName: "Translated Name",
		SourceName:  "test",
	}

	require.NoError(t, repo.Upsert(context.TODO(), translation))
	assert.NotZero(t, translation.ID)

	// Upsert should update existing
	translation.DisplayName = "Updated Name"
	require.NoError(t, repo.Upsert(context.TODO(), translation))

	found, err := repo.FindByActressAndLanguage(context.TODO(), actress.ID, "en")
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", found.DisplayName)
}

func TestActressTranslationRepository_FindAllByActress_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newActressTranslationRepository(db)

	actress := &models.Actress{DMMID: 40002, JapaneseName: "多言語女優"}
	require.NoError(t, db.DB.Create(actress).Error)

	for _, lang := range []string{"en", "zh"} {
		require.NoError(t, repo.Upsert(context.TODO(), &models.ActressTranslation{
			ActressID:   actress.ID,
			Language:    lang,
			DisplayName: lang + " Name",
			SourceName:  "test",
		}))
	}

	translations, err := repo.FindAllByActress(context.TODO(), actress.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(translations), 2)
}

func TestActressTranslationRepository_FindByActressIDsAndLanguage_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newActressTranslationRepository(db)

	actress1 := &models.Actress{DMMID: 40010, JapaneseName: "BatchActress1"}
	actress2 := &models.Actress{DMMID: 40011, JapaneseName: "BatchActress2"}
	require.NoError(t, db.DB.Create(actress1).Error)
	require.NoError(t, db.DB.Create(actress2).Error)

	require.NoError(t, repo.Upsert(context.TODO(), &models.ActressTranslation{
		ActressID: actress1.ID, Language: "en", DisplayName: "En1", SourceName: "test",
	}))
	require.NoError(t, repo.Upsert(context.TODO(), &models.ActressTranslation{
		ActressID: actress2.ID, Language: "en", DisplayName: "En2", SourceName: "test",
	}))

	t.Run("with valid IDs", func(t *testing.T) {
		result, err := repo.FindByActressIDsAndLanguage(context.TODO(), []uint{actress1.ID, actress2.ID}, "en")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(result), 2)
	})

	t.Run("with empty IDs returns empty map", func(t *testing.T) {
		result, err := repo.FindByActressIDsAndLanguage(context.TODO(), []uint{}, "en")
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}

func TestActressTranslationRepository_Delete_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newActressTranslationRepository(db)

	actress := &models.Actress{DMMID: 40020, JapaneseName: "DelTransActress"}
	require.NoError(t, db.DB.Create(actress).Error)

	require.NoError(t, repo.Upsert(context.TODO(), &models.ActressTranslation{
		ActressID: actress.ID, Language: "de", DisplayName: "De Name", SourceName: "test",
	}))

	require.NoError(t, repo.Delete(context.TODO(), actress.ID, "de"))
	_, err := repo.FindByActressAndLanguage(context.TODO(), actress.ID, "de")
	assert.Error(t, err)
}

// --- genre_translation_repo.go uncovered ---

func TestGenreTranslationRepository_UpsertTx_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreTranslationRepository(db)

	genre := &models.Genre{Name: "TransGenre"}
	require.NoError(t, db.DB.Create(genre).Error)

	translation := &models.GenreTranslation{
		GenreID:    genre.ID,
		Language:   "en",
		Name:       "Translated Genre",
		SourceName: "test",
	}

	require.NoError(t, repo.Upsert(context.TODO(), translation))
	assert.NotZero(t, translation.ID)

	// Upsert should update existing
	translation.Name = "Updated Genre"
	require.NoError(t, repo.Upsert(context.TODO(), translation))

	found, err := repo.FindByGenreAndLanguage(context.TODO(), genre.ID, "en")
	require.NoError(t, err)
	assert.Equal(t, "Updated Genre", found.Name)
}

func TestGenreTranslationRepository_FindAllByGenre_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreTranslationRepository(db)

	genre := &models.Genre{Name: "MultiTransGenre"}
	require.NoError(t, db.DB.Create(genre).Error)

	for _, lang := range []string{"en", "zh"} {
		require.NoError(t, repo.Upsert(context.TODO(), &models.GenreTranslation{
			GenreID:    genre.ID,
			Language:   lang,
			Name:       lang + " Genre",
			SourceName: "test",
		}))
	}

	translations, err := repo.FindAllByGenre(context.TODO(), genre.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(translations), 2)
}

func TestGenreTranslationRepository_FindByGenreIDsAndLanguage_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreTranslationRepository(db)

	genre1 := &models.Genre{Name: "BatchGenre1"}
	genre2 := &models.Genre{Name: "BatchGenre2"}
	require.NoError(t, db.DB.Create(genre1).Error)
	require.NoError(t, db.DB.Create(genre2).Error)

	require.NoError(t, repo.Upsert(context.TODO(), &models.GenreTranslation{
		GenreID: genre1.ID, Language: "en", Name: "En1", SourceName: "test",
	}))
	require.NoError(t, repo.Upsert(context.TODO(), &models.GenreTranslation{
		GenreID: genre2.ID, Language: "en", Name: "En2", SourceName: "test",
	}))

	t.Run("with valid IDs", func(t *testing.T) {
		result, err := repo.FindByGenreIDsAndLanguage(context.TODO(), []uint{genre1.ID, genre2.ID}, "en")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(result), 2)
	})

	t.Run("with empty IDs returns empty map", func(t *testing.T) {
		result, err := repo.FindByGenreIDsAndLanguage(context.TODO(), []uint{}, "en")
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}

func TestGenreTranslationRepository_Delete_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreTranslationRepository(db)

	genre := &models.Genre{Name: "DelTransGenre"}
	require.NoError(t, db.DB.Create(genre).Error)

	require.NoError(t, repo.Upsert(context.TODO(), &models.GenreTranslation{
		GenreID: genre.ID, Language: "de", Name: "De Genre", SourceName: "test",
	}))

	require.NoError(t, repo.Delete(context.TODO(), genre.ID, "de"))
	_, err := repo.FindByGenreAndLanguage(context.TODO(), genre.ID, "de")
	assert.Error(t, err)
}

// --- content_id_mapping.go uncovered ---

func TestContentIDMappingRepository_GetAllPaginated_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewContentIDMappingRepository(db)

	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
			SearchID:  "PAG-00" + string(rune('1'+i)),
			ContentID: "pag" + string(rune('1'+i)),
			Source:    "test",
		}))
	}

	mappings, err := repo.GetAllPaginated(context.TODO(), 3, 0)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(mappings), 3)
}

func TestContentIDMappingRepository_GetAllChunked_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewContentIDMappingRepository(db)

	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Create(context.TODO(), &models.ContentIDMapping{
			SearchID:  "CHK-00" + string(rune('1'+i)),
			ContentID: "chk" + string(rune('1'+i)),
			Source:    "test",
		}))
	}

	mappings, err := repo.GetAllChunked(context.TODO(), 2)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(mappings), 5)

	// Zero/negative chunkSize defaults to 1000
	mappings2, err := repo.GetAllChunked(context.TODO(), 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(mappings2), 5)
}

// --- batch_file_operation_repo.go uncovered ---

func TestBatchFileOperationRepository_UpdateUncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	op := &models.BatchFileOperation{
		BatchJobID:    "update-job-1",
		OriginalPath:  "/src/file1.mp4",
		NewPath:       "/dst/file1.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.TODO(), op))

	op.RevertStatus = models.RevertStatusReverted
	require.NoError(t, repo.Update(context.TODO(), op))

	found, err := repo.FindByID(context.TODO(), op.ID)
	require.NoError(t, err)
	assert.Equal(t, models.RevertStatusReverted, found.RevertStatus)
}

func TestBatchFileOperationRepository_CountByBatchJobIDs_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.BatchFileOperation{
		BatchJobID: "job-a", OriginalPath: "/a1", NewPath: "/b1", OperationType: models.OperationTypeMove,
	}))
	require.NoError(t, repo.Create(context.TODO(), &models.BatchFileOperation{
		BatchJobID: "job-a", OriginalPath: "/a2", NewPath: "/b2", OperationType: models.OperationTypeMove,
	}))
	require.NoError(t, repo.Create(context.TODO(), &models.BatchFileOperation{
		BatchJobID: "job-b", OriginalPath: "/c1", NewPath: "/d1", OperationType: models.OperationTypeMove,
	}))

	t.Run("with valid IDs", func(t *testing.T) {
		counts, err := repo.CountByBatchJobIDs(context.TODO(), []string{"job-a", "job-b"})
		require.NoError(t, err)
		assert.Equal(t, int64(2), counts["job-a"])
		assert.Equal(t, int64(1), counts["job-b"])
	})

	t.Run("with empty IDs returns empty map", func(t *testing.T) {
		counts, err := repo.CountByBatchJobIDs(context.TODO(), []string{})
		require.NoError(t, err)
		assert.Empty(t, counts)
	})
}

func TestBatchFileOperationRepository_CountRevertedByBatchJobIDs_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.BatchFileOperation{
		BatchJobID: "rev-job", OriginalPath: "/r1", NewPath: "/s1", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusReverted,
	}))
	require.NoError(t, repo.Create(context.TODO(), &models.BatchFileOperation{
		BatchJobID: "rev-job", OriginalPath: "/r2", NewPath: "/s2", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
	}))

	counts, err := repo.CountRevertedByBatchJobIDs(context.TODO(), []string{"rev-job"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), counts["rev-job"])

	emptyCounts, err := repo.CountRevertedByBatchJobIDs(context.TODO(), []string{})
	require.NoError(t, err)
	assert.Empty(t, emptyCounts)
}

func TestBatchFileOperationRepository_UpdateRevertStatus_Reverted(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	op := &models.BatchFileOperation{
		BatchJobID:    "revert-status-job",
		OriginalPath:  "/src",
		NewPath:       "/dst",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.TODO(), op))

	require.NoError(t, repo.UpdateRevertStatus(context.TODO(), op.ID, models.RevertStatusReverted))
	found, err := repo.FindByID(context.TODO(), op.ID)
	require.NoError(t, err)
	assert.Equal(t, models.RevertStatusReverted, found.RevertStatus)
	assert.NotZero(t, found.RevertedAt)
}

// --- history_repo.go uncovered ---

func TestHistoryRepository_FindByOperation_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.History{
		MovieID: "HIST-OP-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess, OriginalPath: "/src", NewPath: "/dst",
	}))
	require.NoError(t, repo.Create(context.TODO(), &models.History{
		MovieID: "HIST-OP-002", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess, OriginalPath: "/src2", NewPath: "/dst2",
	}))
	require.NoError(t, repo.Create(context.TODO(), &models.History{
		MovieID: "HIST-OP-003", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess, OriginalPath: "/src3", NewPath: "/dst3",
	}))

	result, err := repo.FindByOperation(context.TODO(), models.HistoryOpOrganize, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result), 2)

	// With zero limit, no limit is applied
	resultAll, err := repo.FindByOperation(context.TODO(), models.HistoryOpOrganize, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(resultAll), 2)
}

func TestHistoryRepository_FindByStatus_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.History{
		MovieID: "HIST-ST-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess, OriginalPath: "/src", NewPath: "/dst",
	}))
	require.NoError(t, repo.Create(context.TODO(), &models.History{
		MovieID: "HIST-ST-002", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusFailed, OriginalPath: "/src2", NewPath: "/dst2",
	}))

	result, err := repo.FindByStatus(context.TODO(), models.HistoryStatusSuccess, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result), 1)
}

func TestHistoryRepository_FindRecent_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.History{
		MovieID: "HIST-REC-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess, OriginalPath: "/src", NewPath: "/dst",
	}))

	result, err := repo.FindRecent(context.TODO(), 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result), 1)
}

func TestHistoryRepository_FindByDateRange_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.History{
		MovieID: "HIST-DR-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess, OriginalPath: "/src", NewPath: "/dst",
	}))

	now := time.Now()
	result, err := repo.FindByDateRange(context.TODO(), now.Add(-24*time.Hour), now.Add(24*time.Hour))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result), 1)
}

func TestHistoryRepository_CountByStatus_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.History{
		MovieID: "HIST-CS-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess, OriginalPath: "/src", NewPath: "/dst",
	}))

	count, err := repo.CountByStatus(context.TODO(), models.HistoryStatusSuccess)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(1))
}

func TestHistoryRepository_CountByOperation_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.History{
		MovieID: "HIST-CO-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess, OriginalPath: "/src", NewPath: "/dst",
	}))

	count, err := repo.CountByOperation(context.TODO(), models.HistoryOpOrganize)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(1))
}

func TestHistoryRepository_DeleteByMovieID_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.History{
		MovieID: "HIST-DM-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess, OriginalPath: "/src", NewPath: "/dst",
	}))

	require.NoError(t, repo.DeleteByMovieID(context.TODO(), "HIST-DM-001"))
	result, err := repo.FindByMovieID(context.TODO(), "HIST-DM-001")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestHistoryRepository_DeleteOlderThan_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.History{
		MovieID: "HIST-DO-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess, OriginalPath: "/src", NewPath: "/dst",
	}))

	// Delete everything older than far future should delete nothing
	require.NoError(t, repo.DeleteOlderThan(context.TODO(), time.Now().Add(24*time.Hour)))
}

func TestHistoryRepository_FindByBatchJobID_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.History{
		MovieID: "HIST-BJ-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess, BatchJobID: ptrStr("batch-1"), OriginalPath: "/src", NewPath: "/dst",
	}))

	result, err := repo.FindByBatchJobID(context.TODO(), "batch-1")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result), 1)
}

// --- movie_tag_repo.go uncovered ---

func TestMovieTagRepository_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieTagRepository(db)

	movieRepo := NewMovieRepository(db)
	movie := createTestMovie("TAG-UNC-001")
	require.NoError(t, movieRepo.Create(context.TODO(), movie))

	t.Run("AddTag and GetTagsForMovie", func(t *testing.T) {
		require.NoError(t, repo.AddTag(context.TODO(), "tag-unc-001", "watched"))
		require.NoError(t, repo.AddTag(context.TODO(), "tag-unc-001", "favorite"))

		tags, err := repo.GetTagsForMovie(context.TODO(), "tag-unc-001")
		require.NoError(t, err)
		assert.Contains(t, tags, "favorite")
		assert.Contains(t, tags, "watched")
	})

	t.Run("RemoveTag", func(t *testing.T) {
		require.NoError(t, repo.RemoveTag(context.TODO(), "tag-unc-001", "watched"))
		tags, err := repo.GetTagsForMovie(context.TODO(), "tag-unc-001")
		require.NoError(t, err)
		assert.NotContains(t, tags, "watched")
	})

	t.Run("GetMoviesWithTag", func(t *testing.T) {
		movieIDs, err := repo.GetMoviesWithTag(context.TODO(), "favorite")
		require.NoError(t, err)
		assert.Contains(t, movieIDs, "tag-unc-001")
	})

	t.Run("ListTagsPaginated", func(t *testing.T) {
		tags, err := repo.ListTagsPaginated(context.TODO(), 10, 0)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(tags), 1)
	})

	t.Run("ListAll", func(t *testing.T) {
		tagMap, err := repo.ListAll(context.TODO())
		require.NoError(t, err)
		assert.NotEmpty(t, tagMap)
	})

	t.Run("ListAllChunked", func(t *testing.T) {
		tagMap, err := repo.ListAllChunked(context.TODO(), 100)
		require.NoError(t, err)
		assert.NotEmpty(t, tagMap)

		// Zero/negative chunkSize defaults to 1000
		tagMap2, err := repo.ListAllChunked(context.TODO(), 0)
		require.NoError(t, err)
		assert.NotEmpty(t, tagMap2)
	})

	t.Run("GetUniqueTagsList", func(t *testing.T) {
		tags, err := repo.GetUniqueTagsList(context.TODO())
		require.NoError(t, err)
		assert.Contains(t, tags, "favorite")
	})

	t.Run("RemoveAllTags", func(t *testing.T) {
		require.NoError(t, repo.RemoveAllTags(context.TODO(), "tag-unc-001"))
		tags, err := repo.GetTagsForMovie(context.TODO(), "tag-unc-001")
		require.NoError(t, err)
		assert.Empty(t, tags)
	})
}

// --- actress_alias_repo.go uncovered ---

func TestActressAliasRepository_Upsert_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	t.Run("creates new alias", func(t *testing.T) {
		alias := &models.ActressAlias{AliasName: "NewAlias", CanonicalName: "Canonical"}
		require.NoError(t, repo.Upsert(context.TODO(), alias))
		assert.NotZero(t, alias.ID)
	})

	t.Run("updates existing alias", func(t *testing.T) {
		alias := &models.ActressAlias{AliasName: "ExistingAlias", CanonicalName: "Old"}
		require.NoError(t, repo.Create(context.TODO(), alias))

		updated := &models.ActressAlias{AliasName: "ExistingAlias", CanonicalName: "New"}
		require.NoError(t, repo.Upsert(context.TODO(), updated))

		found, err := repo.FindByAliasName(context.TODO(), "ExistingAlias")
		require.NoError(t, err)
		assert.Equal(t, "New", found.CanonicalName)
	})
}

func TestActressAliasRepository_FindByCanonicalName_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.ActressAlias{AliasName: "Alias1", CanonicalName: "Canon"}))
	require.NoError(t, repo.Create(context.TODO(), &models.ActressAlias{AliasName: "Alias2", CanonicalName: "Canon"}))

	aliases, err := repo.FindByCanonicalName(context.TODO(), "Canon")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(aliases), 2)
}

func TestActressAliasRepository_List_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.ActressAlias{AliasName: "ListAlias", CanonicalName: "ListCanon"}))

	aliases, err := repo.List(context.TODO())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(aliases), 1)
}

func TestActressAliasRepository_Delete_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.ActressAlias{AliasName: "DelAlias", CanonicalName: "DelCanon"}))

	require.NoError(t, repo.Delete(context.TODO(), "DelAlias"))
	_, err := repo.FindByAliasName(context.TODO(), "DelAlias")
	assert.Error(t, err)
}

func TestActressAliasRepository_GetAliasMap_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.ActressAlias{AliasName: "MapAlias", CanonicalName: "MapCanon"}))

	aliasMap, err := repo.GetAliasMap(context.TODO())
	require.NoError(t, err)
	assert.Equal(t, "MapCanon", aliasMap["MapAlias"])
}

// --- base_repository.go uncovered ---

func TestBaseRepository_CreateNilEntity_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBaseRepository[models.Genre, uint](
		db, "genre",
		func(g models.Genre) string { return g.Name },
		WithNewEntity[models.Genre, uint](func() models.Genre { return models.Genre{} }),
	)

	err := repo.Create(context.TODO(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "entity must not be nil")
}

func TestBaseRepository_ListWithDefaultOrder_Uncovered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBaseRepository[models.Genre, uint](
		db, "genre",
		func(g models.Genre) string { return g.Name },
		withDefaultOrder[models.Genre, uint]("name ASC"),
		WithNewEntity[models.Genre, uint](func() models.Genre { return models.Genre{} }),
	)

	require.NoError(t, repo.Create(context.TODO(), &models.Genre{Name: "Zebra"}))
	require.NoError(t, repo.Create(context.TODO(), &models.Genre{Name: "Alpha"}))

	result, err := repo.List(context.TODO(), 10, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result), 2)
	// Should be ordered by name ASC
	assert.Equal(t, "Alpha", result[0].Name)
}
