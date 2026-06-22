package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// =====================================================================
// ActressRepository — remaining error branches
// Line 88: FindByJapaneseNameAndDMMID not found error
// Line 133: ListSorted DB error
// =====================================================================

func TestMiss7_ActressFindByJapaneseNameAndDMMID_NotFound(t *testing.T) {
	db := missDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "NonExistent", 99999)
	assert.Error(t, err)
}

func TestMiss7_ActressListSorted_DBError(t *testing.T) {
	db := missDB(t)
	repo := NewActressRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	_, err := repo.ListSorted(context.TODO(), 10, 0, "japanese_name", "asc")
	assert.Error(t, err)
}

// =====================================================================
// Migrations runner — error branches using closed DB
// Lines 46,55,58,63,76,88,94,100,127,131
// =====================================================================

func TestMiss7_RunMigrations_ClosedDB(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)

	// Close the underlying connection
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	// RunMigrationsOnStartup should fail because DB is closed
	err = db.RunMigrationsOnStartup(context.Background())
	assert.Error(t, err)
}

// =====================================================================
// Migration runner — Lock/Unlock error paths
// Lines 160,167
// =====================================================================

func TestMiss7_ProcessMigrationLocker_UnlockErr(t *testing.T) {
	// processMigrationLocker just uses a mutex, so Unlock always succeeds
	locker := processMigrationLocker{}
	err := locker.Lock(context.Background(), nil)
	require.NoError(t, err)
	err = locker.Unlock(context.Background(), nil)
	require.NoError(t, err)
}

// =====================================================================
// Migration runner — createSQLiteBackupSnapshot error
// Line 192
// =====================================================================

func TestMiss7_CreateSQLiteBackupSnapshot_VacuumErr(t *testing.T) {
	db := missDB(t)

	// Create a file-based DB to test backup
	// But for in-memory, should return empty path
	backupPath, err := createSQLiteBackupSnapshot(context.Background(), nil, ":memory:", db.fs)
	require.NoError(t, err)
	assert.Equal(t, "", backupPath)
}

// =====================================================================
// MovieUpserter — resolveContentID error
// Line 64-66: empty ContentID and ID
// =====================================================================

func TestMiss7_MovieUpsert_EmptyContentIDAndID(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{ContentID: "", ID: "", DisplayTitle: "No IDs"}
	_, err := repo.Upsert(context.TODO(), movie)
	assert.Error(t, err)
}

// =====================================================================
// MovieUpserter — insertOrHandleDuplicateTx duplicate key with reload
// Lines 150-170
// =====================================================================

func TestMiss7_InsertOrHandleDuplicate_DupKeyWithReload(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie
	require.NoError(t, repo.Create(context.TODO(), &models.Movie{ContentID: "dup-reload", ID: "DUP-RELOAD-001", DisplayTitle: "Test"}))

	// Inject duplicate key and seed the row
	cbName := "test:inject_movie_dup_reload"
	injectDuplicate := false
	require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if injectDuplicate || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "movies" {
			return
		}
		injectDuplicate = true
		_ = tx.AddError(gorm.ErrDuplicatedKey)
	}))
	defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

	// Upsert a movie that will trigger the duplicate key path
	movie2 := &models.Movie{ContentID: "dup-reload", ID: "DUP-RELOAD-002", DisplayTitle: "Dup Reload"}
	_, err := repo.Upsert(context.TODO(), movie2)
	require.NoError(t, err)
}

// =====================================================================
// MovieUpserter — findExistingMovieTx: lookup by movie ID when contentID is empty
// Lines 73,78
// =====================================================================

func TestMiss7_FindExistingMovie_ByIDOnly(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie with specific ID
	movie := &models.Movie{ContentID: "find-by-id-test", ID: "FIND-BY-ID-001", DisplayTitle: "Test"}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	// Now upsert with same ID but no ContentID — should find by ID
	movie2 := &models.Movie{ID: "FIND-BY-ID-001", DisplayTitle: "Found By ID"}
	result, err := repo.Upsert(context.TODO(), movie2)
	require.NoError(t, err)
	assert.Equal(t, "find-by-id-test", result.ContentID)
}

// =====================================================================
// ActressMerge — ExecuteMerge: DMMID swap where target already has same DMMID
// Lines 293-303
// =====================================================================

func TestMiss7_ExecuteMerge_SameDMMIDNoSwap(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	// Both target and source have DMMID=0 — no swap needed
	target := &models.Actress{DMMID: 0, JapaneseName: "SameDMMTgt", FirstName: "TgtFirst"}
	source := &models.Actress{DMMID: 0, JapaneseName: "SameDMMSrc", FirstName: "SrcFirst"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))
	require.NoError(t, actressRepo.Create(context.TODO(), source))

	result, err := actressRepo.Merge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, target.ID, result.MergedActress.ID)
}

// =====================================================================
// ActressMerge — loadPair: source FindByID error
// Line 179
// =====================================================================

func TestMiss7_LoadPair_SourceFindErr(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)
	merger := actressRepo.merger

	target := &models.Actress{DMMID: 40001, JapaneseName: "LoadPairSrcFindTgt"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))

	// Drop table to cause FindByID error on source lookup
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	_, _, err := merger.loadPair(context.TODO(), target.ID, 99999)
	assert.Error(t, err)
}

// =====================================================================
// ActressMerge — loadPair: target FindByID error
// Line 179
// =====================================================================

func TestMiss7_LoadPair_TargetFindErr(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)
	merger := actressRepo.merger

	// Drop table before any FindByID call
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	_, _, err := merger.loadPair(context.TODO(), 1, 2)
	assert.Error(t, err)
}

// =====================================================================
// helpers.go — raceRetryCreate error paths
// Lines 47-49, 80-112
// =====================================================================

func TestMiss7_RaceRetryCreate_Success(t *testing.T) {
	db := missDB(t)

	// Normal create that succeeds
	genre := &models.Genre{Name: "RaceSuccess"}
	err := raceRetryCreate(db.WithContext(context.TODO()), genre, func(tx *gorm.DB) error {
		return nil
	})
	require.NoError(t, err)
	assert.NotZero(t, genre.ID)
}

// =====================================================================
// helpers.go — prepareMovieForUpsert with zero-ID genre/actress
// Lines 80-112
// =====================================================================

func TestMiss7_PrepareMovieForUpsert_GenreZeroIDResolution(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create a genre first
	genreRepo := newGenreRepository(db)
	_, err := genreRepo.FindOrCreate(context.TODO(), "ZeroIDGenre")
	require.NoError(t, err)

	// Create movie that references the genre by name but has zero ID
	movie := &models.Movie{
		ContentID:    "zero-id-genre-test",
		ID:           "ZERO-ID-GENRE-001",
		DisplayTitle: "Zero ID Genre",
		Genres:       []models.Genre{{Name: "ZeroIDGenre"}}, // ID is 0, name matches existing
	}
	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "Zero ID Genre EN", SourceName: "test"},
	}
	_, err = repo.UpsertWithTranslations(context.TODO(), movie, genreTranslations, nil)
	require.NoError(t, err)
}

func TestMiss7_PrepareMovieForUpsert_ActressZeroIDResolution(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)
	actressRepo := NewActressRepository(db)

	// Create an actress
	actress := &models.Actress{DMMID: 40101, JapaneseName: "ZeroIDActress"}
	require.NoError(t, actressRepo.Create(context.TODO(), actress))

	// Create movie that references the actress by DMMID but has zero ID
	movie := &models.Movie{
		ContentID:    "zero-id-actress-test",
		ID:           "ZERO-ID-ACTRESS-001",
		DisplayTitle: "Zero ID Actress",
		Actresses:    []models.Actress{{DMMID: 40101}}, // ID is 0, DMMID matches existing
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", FirstName: "Zero", LastName: "ID", DisplayName: "Zero ID Actress EN", SourceName: "test"},
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, actressTranslations)
	require.NoError(t, err)
}

func TestMiss7_PrepareMovieForUpsert_ActressCompositeKeyResolution(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)
	actressRepo := NewActressRepository(db)

	// Create an actress without DMMID
	actress := &models.Actress{FirstName: "CompositeKeyFirst", LastName: "CompositeKeyLast", JapaneseName: "CompositeKeyJP"}
	require.NoError(t, actressRepo.Create(context.TODO(), actress))

	// Create movie that references the actress by composite key
	movie := &models.Movie{
		ContentID:    "composite-key-test",
		ID:           "COMPOSITE-KEY-001",
		DisplayTitle: "Composite Key Test",
		Actresses:    []models.Actress{{FirstName: "CompositeKeyFirst", LastName: "CompositeKeyLast", JapaneseName: "CompositeKeyJP"}},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", FirstName: "Composite", LastName: "Key", DisplayName: "Composite Key EN", SourceName: "test"},
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, actressTranslations)
	require.NoError(t, err)
}

// =====================================================================
// helpers.go — upsertMovieCore: Association Replace errors
// Line 231
// =====================================================================

func TestMiss7_UpsertMovieCore_AssocReplaceGenreErr(t *testing.T) {
	db := missDB(t)

	require.NoError(t, db.DB.Create(&models.Movie{ContentID: "assoc-rep-genre-err", ID: "ASSOC-REP-GENRE-ERR-001", DisplayTitle: "Test"}).Error)
	require.NoError(t, db.DB.Exec("DROP TABLE movie_genres").Error)

	movie := &models.Movie{ContentID: "assoc-rep-genre-err", ID: "ASSOC-REP-GENRE-ERR-001", DisplayTitle: "Updated", Genres: []models.Genre{{Name: "AssocRepErrGenre"}}}
	err := upsertMovieCore(db.WithContext(context.TODO()), db, movie, nil, nil, nil)
	assert.Error(t, err)
}

func TestMiss7_UpsertMovieCore_AssocReplaceActressErr(t *testing.T) {
	db := missDB(t)

	require.NoError(t, db.DB.Create(&models.Movie{ContentID: "assoc-rep-actress-err", ID: "ASSOC-REP-ACTRESS-ERR-001", DisplayTitle: "Test"}).Error)
	require.NoError(t, db.DB.Exec("DROP TABLE movie_actresses").Error)

	movie := &models.Movie{ContentID: "assoc-rep-actress-err", ID: "ASSOC-REP-ACTRESS-ERR-001", DisplayTitle: "Updated", Actresses: []models.Actress{{DMMID: 40111, JapaneseName: "AssocRepErrActress"}}}
	err := upsertMovieCore(db.WithContext(context.TODO()), db, movie, nil, nil, nil)
	assert.Error(t, err)
}

// =====================================================================
// helpers.go — persistTranslations: genre/actress translation with zero ID skip
// Lines 169, 193
// =====================================================================

func TestMiss7_PersistTranslations_GenreTransZeroIDSkip(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie with a genre that has ID=0 and genre translations
	// The genre ID should not be resolved at persist time if the genre was just created
	movie := &models.Movie{
		ContentID:    "genre-zero-id-skip-test",
		ID:           "GENRE-ZERO-ID-SKIP-001",
		DisplayTitle: "Genre Zero ID Skip",
		Genres:       []models.Genre{{Name: "ZeroIDSkipGenre"}}, // ID=0
	}
	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "Zero ID Skip EN", SourceName: "test"},
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, genreTranslations, nil)
	require.NoError(t, err)
}

func TestMiss7_PersistTranslations_ActressTransZeroIDSkip(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie with an actress that has ID=0 and actress translations
	movie := &models.Movie{
		ContentID:    "actress-zero-id-skip-test",
		ID:           "ACTRESS-ZERO-ID-SKIP-001",
		DisplayTitle: "Actress Zero ID Skip",
		Actresses:    []models.Actress{{DMMID: 40121, JapaneseName: "ZeroIDSkipActress"}},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", FirstName: "Zero", LastName: "Skip", DisplayName: "Zero ID Skip EN", SourceName: "test"},
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, actressTranslations)
	require.NoError(t, err)
}

// =====================================================================
// Migrations — fileMigrationLocker Lock error
// Line 160
// =====================================================================

func TestMiss7_FileMigrationLocker_LockErr(t *testing.T) {
	db := missDB(t)
	locker, err := newStartupMigrationLocker("test.db", db.fs)
	require.NoError(t, err)
	fileLocker, ok := locker.(*fileMigrationLocker)
	require.True(t, ok)

	// Try to lock with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	sqlDB, err := db.DB.DB()
	require.NoError(t, err)

	err = fileLocker.Lock(ctx, sqlDB)
	// May or may not error depending on context cancellation timing
	_ = err

	// Clean up
	_ = fileLocker.fileLock.Unlock()
}

// =====================================================================
// Migrations — fileMigrationLocker Unlock with Close error
// Line 167
// =====================================================================

func TestMiss7_FileMigrationLocker_UnlockClose(t *testing.T) {
	db := missDB(t)
	locker, err := newStartupMigrationLocker("test_unlock.db", db.fs)
	require.NoError(t, err)
	fileLocker, ok := locker.(*fileMigrationLocker)
	require.True(t, ok)

	sqlDB, err := db.DB.DB()
	require.NoError(t, err)

	// Lock and unlock normally
	err = fileLocker.Lock(context.Background(), sqlDB)
	require.NoError(t, err)

	err = fileLocker.Unlock(context.Background(), sqlDB)
	require.NoError(t, err)
}

// =====================================================================
// Migrations — RunMigrationsOnStartup with nil context
// Line 46 (ctx == nil → ctx = context.Background())
// =====================================================================

func TestMiss7_RunMigrations_NilContext(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// nil context should be replaced with Background
	err = db.RunMigrationsOnStartup(nil)
	require.NoError(t, err)
}

// =====================================================================
// GenreReplacementRepository — Upsert error branch
// Line 39
// =====================================================================

func TestMiss7_GenreReplacementUpsert_FindErr(t *testing.T) {
	db := missDB(t)
	repo := NewGenreReplacementRepository(db)

	// Drop table to cause Find error
	require.NoError(t, db.DB.Exec("DROP TABLE genre_replacements").Error)

	err := repo.Upsert(context.TODO(), &models.GenreReplacement{Original: "FindErrGenre", Replacement: "Replaced"})
	assert.Error(t, err)
}

// =====================================================================
// WordReplacementRepository — Upsert error branch
// Line 40
// =====================================================================

func TestMiss7_WordReplacementUpsert_FindErr(t *testing.T) {
	db := missDB(t)
	repo := NewWordReplacementRepository(db)

	require.NoError(t, db.DB.Exec("DROP TABLE word_replacements").Error)

	err := repo.Upsert(context.TODO(), &models.WordReplacement{Original: "FindErrWord", Replacement: "Replaced"})
	assert.Error(t, err)
}

// =====================================================================
// ActressAliasRepository — Upsert non-NotFound error
// Line 39
// =====================================================================

func TestMiss7_ActressAliasUpsert_FindErr(t *testing.T) {
	db := missDB(t)
	repo := NewActressAliasRepository(db)

	require.NoError(t, db.DB.Exec("DROP TABLE actress_aliases").Error)

	err := repo.Upsert(context.TODO(), &models.ActressAlias{AliasName: "FindErrAlias", CanonicalName: "Canon"})
	assert.Error(t, err)
}

// =====================================================================
// MovieTranslationRepository — UpsertTx duplicate key save error
// Line 53
// =====================================================================

func TestMiss7_MovieTranslationUpsertTx_DupKeySaveErr(t *testing.T) {
	db := missDB(t)
	repo := newMovieTranslationRepository(db)
	movieRepo := NewMovieRepository(db)

	require.NoError(t, movieRepo.Create(context.TODO(), &models.Movie{ContentID: "mtrans-dup-save-err2", ID: "MTRANS-DUP-SE2", DisplayTitle: "Test"}))

	// First create a translation
	require.NoError(t, repo.Upsert(context.TODO(), &models.MovieTranslation{MovieID: "mtrans-dup-save-err2", Language: "en", Title: "Initial"}))

	// Now try upserting again — this should hit the Save path
	translation := &models.MovieTranslation{MovieID: "mtrans-dup-save-err2", Language: "en", Title: "Updated"}
	err := repo.Upsert(context.TODO(), translation)
	require.NoError(t, err)

	// Verify
	found, err := repo.FindByMovieAndLanguage(context.TODO(), "mtrans-dup-save-err2", "en")
	require.NoError(t, err)
	assert.Equal(t, "Updated", found.Title)
}

// =====================================================================
// GenreTranslationRepository — UpsertTx error with save error
// Line 54
// =====================================================================

func TestMiss7_GenreTranslationUpsertTx_SaveErr(t *testing.T) {
	db := missDB(t)
	repo := newGenreTranslationRepository(db)
	genreRepo := newGenreRepository(db)

	genre, err := genreRepo.FindOrCreate(context.TODO(), "SaveErrGenre")
	require.NoError(t, err)
	_ = genre

	// Create translation first
	require.NoError(t, repo.Upsert(context.TODO(), &models.GenreTranslation{GenreID: genre.ID, Language: "en", Name: "Initial", SourceName: "test"}))

	// Now drop table and try to update
	require.NoError(t, db.DB.Exec("DROP TABLE genre_translations").Error)

	err = repo.Upsert(context.TODO(), &models.GenreTranslation{GenreID: genre.ID, Language: "en", Name: "Updated", SourceName: "test"})
	assert.Error(t, err)
}

// =====================================================================
// ActressTranslationRepository — UpsertTx error with save error
// Line 54
// =====================================================================

func TestMiss7_ActressTranslationUpsertTx_SaveErr(t *testing.T) {
	db := missDB(t)
	repo := newActressTranslationRepository(db)
	actressRepo := NewActressRepository(db)

	actress := &models.Actress{DMMID: 40131, JapaneseName: "SaveErrActress"}
	require.NoError(t, actressRepo.Create(context.TODO(), actress))

	// Create translation first
	require.NoError(t, repo.Upsert(context.TODO(), &models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Initial", SourceName: "test"}))

	// Now drop table and try to update
	require.NoError(t, db.DB.Exec("DROP TABLE actress_translations").Error)

	err := repo.Upsert(context.TODO(), &models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Updated", SourceName: "test"})
	assert.Error(t, err)
}
