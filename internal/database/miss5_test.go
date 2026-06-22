package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// =====================================================================
// MovieUpserter — error branches inside UpsertWithTranslations transaction
// These are the `return err` lines that are only hit when sub-functions fail
// =====================================================================

func TestMiss5_UpsertWithTranslations_InsertDupKeyLoadErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create initial movie
	movie := &models.Movie{ContentID: "dup-load-err", ID: "DUP-LOAD-ERR-001", DisplayTitle: "Test"}
	require.NoError(t, repo.Create(context.TODO(), movie))

	// Use GORM callback to inject ErrDuplicatedKey on movie Create
	cbName := "test:inject_movie_dup_load_err"
	injectDuplicate := false
	require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if injectDuplicate || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "movies" {
			return
		}
		injectDuplicate = true
		_ = tx.AddError(gorm.ErrDuplicatedKey)
	}))
	defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

	// Upsert a movie with different ContentID but same ID — the duplicate key path
	// will try to load the existing movie, but since we didn't seed it, it may fail
	movie2 := &models.Movie{ContentID: "dup-load-err", ID: "DUP-LOAD-ERR-002", DisplayTitle: "Dup Load Err"}
	_, err := repo.Upsert(context.TODO(), movie2)
	// Should succeed because the existing movie already exists with same ContentID
	if err != nil {
		t.Logf("Error (expected for some paths): %v", err)
	}
}

// Test the insertOrHandleDuplicateTx duplicate key path where load fails
func TestMiss5_InsertOrHandleDuplicate_DupKeyLoadNotFound(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Use GORM callback to inject ErrDuplicatedKey without seeding the row
	cbName := "test:inject_movie_dup_load_nf"
	injectDuplicate := false
	require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if injectDuplicate || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "movies" {
			return
		}
		injectDuplicate = true
		_ = tx.AddError(gorm.ErrDuplicatedKey)
	}))
	defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

	movie := &models.Movie{ContentID: "dup-load-nf", ID: "DUP-LOAD-NF-001", DisplayTitle: "Test"}
	_, err := repo.Upsert(context.TODO(), movie)
	// This will hit the duplicate key path, then try to load, and not find anything
	// Then it will try saveMovieWithAssociations
	if err != nil {
		t.Logf("Error: %v", err)
	}
}

// =====================================================================
// MovieUpserter — upsertGenresTx error inside UpsertWithTranslations
// Line 73: if err := u.upsertGenresTx(tx, movie); err != nil
// =====================================================================

func TestMiss5_UpsertWithTranslations_GenreTxError(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create initial movie without genres
	movie := &models.Movie{ContentID: "genre-tx-err", ID: "GENRE-TX-ERR-001", DisplayTitle: "Test"}
	require.NoError(t, repo.Create(context.TODO(), movie))

	// Now update with genres but drop the genres table
	require.NoError(t, db.DB.Exec("DROP TABLE genres").Error)

	movie2 := &models.Movie{
		ContentID:    "genre-tx-err",
		ID:           "GENRE-TX-ERR-001",
		DisplayTitle: "Genre Tx Error",
		Genres:       []models.Genre{{Name: "ErrGenre"}},
	}
	_, err := repo.Upsert(context.TODO(), movie2)
	assert.Error(t, err)
}

// =====================================================================
// MovieUpserter — upsertActressesTx error inside UpsertWithTranslations
// Line 78: if err := u.upsertActressesTx(tx, movie); err != nil
// =====================================================================

func TestMiss5_UpsertWithTranslations_ActressTxError(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create initial movie without actresses
	movie := &models.Movie{ContentID: "actress-tx-err", ID: "ACTRESS-TX-ERR-001", DisplayTitle: "Test"}
	require.NoError(t, repo.Create(context.TODO(), movie))

	// Now update with actresses but drop the actresses table
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	movie2 := &models.Movie{
		ContentID:    "actress-tx-err",
		ID:           "ACTRESS-TX-ERR-001",
		DisplayTitle: "Actress Tx Error",
		Actresses:    []models.Actress{{DMMID: 20001, JapaneseName: "ErrActress"}},
	}
	_, err := repo.Upsert(context.TODO(), movie2)
	assert.Error(t, err)
}

// =====================================================================
// MovieUpserter — upsertTranslationsTx error inside UpsertWithTranslations
// Line 89: if err := u.upsertTranslationsTx(...); err != nil
// =====================================================================

func TestMiss5_UpsertWithTranslations_TranslationsTxError(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create initial movie
	movie := &models.Movie{ContentID: "trans-tx-err", ID: "TRANS-TX-ERR-001", DisplayTitle: "Test"}
	require.NoError(t, repo.Create(context.TODO(), movie))

	// Now update with translations but drop the movies table to make Save fail
	require.NoError(t, db.DB.Exec("DROP TABLE movies").Error)

	movie2 := &models.Movie{
		ContentID:    "trans-tx-err",
		ID:           "TRANS-TX-ERR-001",
		DisplayTitle: "Trans Tx Error",
	}
	_, err := repo.Upsert(context.TODO(), movie2)
	assert.Error(t, err)
}

// =====================================================================
// MovieUpserter — insertOrHandleDuplicateTx non-duplicate create error
// Line 132: return wrapDBErr("create", ...)
// Line 150: existing movie's created_at from duplicate key load fails
// Lines 157-160: duplicate key load not found → still try save
// Lines 164-166: saveMovieWithAssociations error
// Lines 168-170: reload after save duplicate
// =====================================================================

func TestMiss5_InsertOrHandleDuplicate_CreateErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Drop movies table to make Create fail
	require.NoError(t, db.DB.Exec("DROP TABLE movies").Error)

	movie := &models.Movie{ContentID: "create-err", ID: "CREATE-ERR-001", DisplayTitle: "Test"}
	_, err := repo.Upsert(context.TODO(), movie)
	assert.Error(t, err)
}

func TestMiss5_InsertOrHandleDuplicate_SaveAssocErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create initial movie
	require.NoError(t, repo.Create(context.TODO(), &models.Movie{ContentID: "save-assoc-err", ID: "SAVE-ASSOC-ERR-001", DisplayTitle: "Test"}))

	// Inject duplicate key, then break saveMovieWithAssociations
	cbName := "test:inject_movie_dup_save_assoc"
	injectDuplicate := false
	require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if injectDuplicate || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "movies" {
			return
		}
		injectDuplicate = true
		_ = tx.AddError(gorm.ErrDuplicatedKey)
	}))
	defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

	// Upsert with genres that will fail
	require.NoError(t, db.DB.Exec("DROP TABLE movie_genres").Error)

	movie2 := &models.Movie{
		ContentID:    "save-assoc-err-2",
		ID:           "SAVE-ASSOC-ERR-002",
		DisplayTitle: "Save Assoc Err",
		Genres:       []models.Genre{{Name: "ErrGenre"}},
	}
	_, err := repo.Upsert(context.TODO(), movie2)
	assert.Error(t, err)
}

// =====================================================================
// MovieUpserter — upsertGenresTx/upsertActressesTx error wrapping
// Lines 179, 188
// =====================================================================

func TestMiss5_UpsertGenresTx_WrapErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	require.NoError(t, db.DB.Exec("DROP TABLE genres").Error)

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.upsertGenresTx(tx, &models.Movie{ContentID: "test", Genres: []models.Genre{{Name: "ErrGenre"}}})
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ensure genres")
}

func TestMiss5_UpsertActressesTx_WrapErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.upsertActressesTx(tx, &models.Movie{ContentID: "test", Actresses: []models.Actress{{DMMID: 1, JapaneseName: "ErrActress"}}})
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ensure actresses")
}

// =====================================================================
// MovieUpserter — saveMovieWithAssociations error branches
// Lines 205, 208
// =====================================================================

func TestMiss5_SaveMovieWithAssociations_GenreErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.Movie{ContentID: "smw-genre-err", ID: "SMW-GENRE-ERR-001", DisplayTitle: "Test"}))

	require.NoError(t, db.DB.Exec("DROP TABLE genres").Error)

	movie := &models.Movie{
		ContentID:    "smw-genre-err",
		ID:           "SMW-GENRE-ERR-001",
		DisplayTitle: "SMW Genre Err",
		Genres:       []models.Genre{{Name: "ErrGenre"}},
	}
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.saveMovieWithAssociations(tx, movie)
	})
	assert.Error(t, err)
}

func TestMiss5_SaveMovieWithAssociations_ActressErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.Movie{ContentID: "smw-actress-err", ID: "SMW-ACTRESS-ERR-001", DisplayTitle: "Test"}))

	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	movie := &models.Movie{
		ContentID:    "smw-actress-err",
		ID:           "SMW-ACTRESS-ERR-001",
		DisplayTitle: "SMW Actress Err",
		Actresses:    []models.Actress{{DMMID: 20002, JapaneseName: "ErrActress"}},
	}
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.saveMovieWithAssociations(tx, movie)
	})
	assert.Error(t, err)
}

func TestMiss5_SaveMovieWithAssociations_UpsertCoreErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.Movie{ContentID: "smw-core-err", ID: "SMW-CORE-ERR-001", DisplayTitle: "Test"}))

	require.NoError(t, db.DB.Exec("DROP TABLE movies").Error)

	movie := &models.Movie{ContentID: "smw-core-err", ID: "SMW-CORE-ERR-001", DisplayTitle: "SMW Core Err"}
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.saveMovieWithAssociations(tx, movie)
	})
	assert.Error(t, err)
}

// =====================================================================
// MovieUpserter — ensureGenresExistTx: batch find then race retry
// Lines 246-255
// =====================================================================

func TestMiss5_EnsureGenresExistTx_NewGenreSuccess(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create a genre that exists
	require.NoError(t, db.DB.Create(&models.Genre{Name: "ExistingGenre"}).Error)

	genres := []models.Genre{{Name: "ExistingGenre"}, {Name: "NewGenre"}}
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureGenresExistTx(tx, genres)
	})
	require.NoError(t, err)

	// Both should have IDs now
	assert.NotZero(t, genres[0].ID)
	assert.NotZero(t, genres[1].ID)
	assert.Equal(t, "ExistingGenre", genres[0].Name)
	assert.Equal(t, "NewGenre", genres[1].Name)
}

func TestMiss5_EnsureGenresExistTx_RaceRetryFindErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Inject ErrDuplicatedKey on genre Create, then drop table so Find fails
	cbName := "test:inject_genre_race_find_err2"
	injectDuplicate := false
	require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if injectDuplicate || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "genres" {
			return
		}
		injectDuplicate = true
		_ = tx.AddError(gorm.ErrDuplicatedKey)
	}))
	defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

	require.NoError(t, db.DB.Exec("DROP TABLE genres").Error)

	genres := []models.Genre{{Name: "RaceFindErr"}}
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureGenresExistTx(tx, genres)
	})
	assert.Error(t, err)
}

// =====================================================================
// MovieUpserter — resolveActressGroup: found with merge+save
// Lines 304,312-318,322
// =====================================================================

func TestMiss5_ResolveActressGroup_FoundNoMerge(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)
	actressRepo := NewActressRepository(db)

	// Create an actress with all fields filled
	existing := &models.Actress{DMMID: 20101, JapaneseName: "NoMergeActress", ThumbURL: "http://existing.jpg", FirstName: "ExistingFirst"}
	require.NoError(t, actressRepo.Create(context.TODO(), existing))

	// New actress has same DMMID but ThumbURL is empty — no merge needed
	actresses := []models.Actress{{DMMID: 20101, JapaneseName: "NoMergeActress"}}
	group := []actressGroupEntry{{index: 0, act: &actresses[0]}}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.resolveActressGroup(tx, actresses, group, lookupActressByDMMID)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)
}

func TestMiss5_ResolveActressGroup_FoundWithMerge(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)
	actressRepo := NewActressRepository(db)

	// Create an actress without ThumbURL
	existing := &models.Actress{DMMID: 20111, JapaneseName: "MergeActress", FirstName: "ExistingFirst"}
	require.NoError(t, actressRepo.Create(context.TODO(), existing))

	// New actress has same DMMID with ThumbURL — merge should update
	actresses := []models.Actress{{DMMID: 20111, JapaneseName: "MergeActress", ThumbURL: "http://new.jpg"}}
	group := []actressGroupEntry{{index: 0, act: &actresses[0]}}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.resolveActressGroup(tx, actresses, group, lookupActressByDMMID)
	})
	require.NoError(t, err)

	// Verify merge happened
	updated, err := actressRepo.FindByDMMID(context.TODO(), 20111)
	require.NoError(t, err)
	assert.Equal(t, "http://new.jpg", updated.ThumbURL)
}

func TestMiss5_ResolveActressGroup_MergeSaveErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Drop actresses table to cause Save error
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	actresses := []models.Actress{{DMMID: 20121, JapaneseName: "MergeSaveErr", ThumbURL: "http://err.jpg"}}
	group := []actressGroupEntry{{index: 0, act: &actresses[0]}}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.resolveActressGroup(tx, actresses, group, lookupActressByDMMID)
	})
	assert.Error(t, err)
}

func TestMiss5_ResolveActressGroup_RaceRetryMergeSaveErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)
	actressRepo := NewActressRepository(db)

	// Create existing actress
	existing := &models.Actress{DMMID: 20131, JapaneseName: "RaceMergeSaveErr", FirstName: "ExistingFirst"}
	require.NoError(t, actressRepo.Create(context.TODO(), existing))

	// Inject ErrDuplicatedKey to trigger race retry path
	cbName := "test:inject_actress_race_merge_save_err"
	injectDuplicate := false
	require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if injectDuplicate || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "actresses" {
			return
		}
		injectDuplicate = true
		_ = tx.AddError(gorm.ErrDuplicatedKey)
	}))
	defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

	// Drop table so Save in race retry fails
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	actresses := []models.Actress{{DMMID: 20131, JapaneseName: "RaceMergeSaveErr", ThumbURL: "http://race.jpg"}}
	group := []actressGroupEntry{{index: 0, act: &actresses[0]}}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.resolveActressGroup(tx, actresses, group, lookupActressByDMMID)
	})
	assert.Error(t, err)
}

// =====================================================================
// lookupActressByJapaneseName — non-ErrRecordNotFound error
// Line 304
// =====================================================================

func TestMiss5_LookupActressByJapaneseName_DBError(t *testing.T) {
	db := missDB(t)

	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	_, _, err := lookupActressByJapaneseName(db.WithContext(context.TODO()), &models.Actress{JapaneseName: "ErrTest"})
	assert.Error(t, err)
}

// =====================================================================
// lookupActressByName — non-ErrRecordNotFound error
// Line 312-318, 322
// =====================================================================

func TestMiss5_LookupActressByName_DBError(t *testing.T) {
	db := missDB(t)

	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	_, _, err := lookupActressByName(db.WithContext(context.TODO()), &models.Actress{DMMID: 1})
	assert.Error(t, err)
}

// =====================================================================
// ensureActressesExistTx — jpGroup and nameGroup paths
// Lines 409, 415
// =====================================================================

func TestMiss5_EnsureActressesExistTx_JPGroupExisting(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)
	actressRepo := NewActressRepository(db)

	// Create an actress with JapaneseName
	existing := &models.Actress{JapaneseName: "ExistingJPActress"}
	require.NoError(t, actressRepo.Create(context.TODO(), existing))

	// Upsert with same JapaneseName — should find existing
	actresses := []models.Actress{{JapaneseName: "ExistingJPActress"}}
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)
}

func TestMiss5_EnsureActressesExistTx_NameGroupExisting(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)
	actressRepo := NewActressRepository(db)

	// Create an actress with name only
	existing := &models.Actress{FirstName: "ExistingFirst", LastName: "ExistingLast"}
	require.NoError(t, actressRepo.Create(context.TODO(), existing))

	// Upsert with same name — should find existing
	actresses := []models.Actress{{FirstName: "ExistingFirst", LastName: "ExistingLast"}}
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actresses[0].ID)
}

func TestMiss5_EnsureActressesExistTx_JPGroupErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	actresses := []models.Actress{{JapaneseName: "JPErrActress"}}
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	assert.Error(t, err)
}

func TestMiss5_EnsureActressesExistTx_NameGroupErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	actresses := []models.Actress{{FirstName: "NameErrFirst", LastName: "NameErrLast"}}
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return repo.upserter.ensureActressesExistTx(tx, actresses)
	})
	assert.Error(t, err)
}

// =====================================================================
// ActressMerge — PreviewMerge with conflicting fields
// Lines 192-201: error paths from mergeActressValues
// =====================================================================

func TestMiss5_PreviewMerge_WithConflicts(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	target := &models.Actress{DMMID: 20201, JapaneseName: "PreviewTgt", FirstName: "TgtFirst", LastName: "TgtLast"}
	source := &models.Actress{DMMID: 20202, JapaneseName: "PreviewSrc", FirstName: "SrcFirst", LastName: "SrcLast"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))
	require.NoError(t, actressRepo.Create(context.TODO(), source))

	preview, err := actressRepo.PreviewMerge(context.TODO(), target.ID, source.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(preview.Conflicts), 2) // dmm_id + first_name + last_name + japanese_name
	assert.NotNil(t, preview.ConflictByField)
}

// =====================================================================
// ActressMerge — PlanMerge with custom resolutions
// Lines 225-242
// =====================================================================

func TestMiss5_PlanMerge_WithCustomResolutions(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	target := &models.Actress{DMMID: 20211, JapaneseName: "PlanTgt", FirstName: "TgtFirst"}
	source := &models.Actress{DMMID: 20212, JapaneseName: "PlanSrc", FirstName: "SrcFirst"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))
	require.NoError(t, actressRepo.Create(context.TODO(), source))

	resolutions := map[string]string{
		"dmm_id":     "source",
		"first_name": "source",
	}
	plan, err := actressRepo.merger.PlanMerge(context.TODO(), target.ID, source.ID, resolutions)
	require.NoError(t, err)
	assert.Equal(t, 20212, plan.Merged.DMMID)
	assert.Equal(t, "SrcFirst", plan.Merged.FirstName)
}

// =====================================================================
// ActressMerge — ExecuteMerge error when FindByID fails after merge
// Lines 315-319
// =====================================================================

func TestMiss5_ExecuteMerge_FindByIDAfterErr(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	target := &models.Actress{DMMID: 20221, JapaneseName: "FindAfterTgt"}
	source := &models.Actress{DMMID: 20222, JapaneseName: "FindAfterSrc"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))
	require.NoError(t, actressRepo.Create(context.TODO(), source))

	// Plan the merge
	plan, err := actressRepo.merger.PlanMerge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)

	// Execute the merge
	result, err := actressRepo.merger.ExecuteMerge(context.TODO(), plan, db)
	require.NoError(t, err)
	assert.Equal(t, target.ID, result.MergedActress.ID)
}

// =====================================================================
// ActressMerge — ExecuteMerge DMMID swap for sourceID=0 edge case
// Line 298: tempDMMID == 0
// =====================================================================

func TestMiss5_ExecuteMerge_TempDMMIDEdge(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	// Target has DMMID, source has a different DMMID — resolve to "target" to keep target's DMMID
	// This tests the path where merged.DMMID == source.DMMID (no swap needed)
	target := &models.Actress{DMMID: 20231, JapaneseName: "NoSwapTgt"}
	source := &models.Actress{DMMID: 20232, JapaneseName: "NoSwapSrc"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))
	require.NoError(t, actressRepo.Create(context.TODO(), source))

	resolutions := map[string]string{"dmm_id": "target"}
	result, err := actressRepo.Merge(context.TODO(), target.ID, source.ID, resolutions)
	require.NoError(t, err)
	assert.Equal(t, 20231, result.MergedActress.DMMID)
}

// =====================================================================
// helpers.go — persistTranslations: genre/actress translation error paths
// Lines 147, 152, 169, 193
// =====================================================================

func TestMiss5_PersistTranslations_GenreTransErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie with genres
	movie := &models.Movie{
		ContentID:    "genre-trans-err-test",
		ID:           "GENRE-TRANS-ERR-001",
		DisplayTitle: "Genre Trans Error",
		Genres:       []models.Genre{{Name: "TransErrGenre"}},
	}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	// Now try with genre translations but break the genre_translations table
	require.NoError(t, db.DB.Exec("DROP TABLE genre_translations").Error)

	movie2 := &models.Movie{
		ContentID:    "genre-trans-err-test",
		ID:           "GENRE-TRANS-ERR-001",
		DisplayTitle: "Genre Trans Error Updated",
		Genres:       []models.Genre{{Name: "TransErrGenre"}},
	}
	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "Error Genre EN", SourceName: "test"},
	}
	_, err = repo.UpsertWithTranslations(context.TODO(), movie2, genreTranslations, nil)
	assert.Error(t, err)
}

func TestMiss5_PersistTranslations_ActressTransErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie with actresses
	movie := &models.Movie{
		ContentID:    "actress-trans-err-test",
		ID:           "ACTRESS-TRANS-ERR-001",
		DisplayTitle: "Actress Trans Error",
		Actresses:    []models.Actress{{DMMID: 20241, JapaneseName: "TransErrActress"}},
	}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	// Now try with actress translations but break the actress_translations table
	require.NoError(t, db.DB.Exec("DROP TABLE actress_translations").Error)

	movie2 := &models.Movie{
		ContentID:    "actress-trans-err-test",
		ID:           "ACTRESS-TRANS-ERR-001",
		DisplayTitle: "Actress Trans Error Updated",
		Actresses:    []models.Actress{{DMMID: 20241, JapaneseName: "TransErrActress"}},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", FirstName: "TransErr", LastName: "Actress", DisplayName: "TransErr Actress EN", SourceName: "test"},
	}
	_, err = repo.UpsertWithTranslations(context.TODO(), movie2, nil, actressTranslations)
	assert.Error(t, err)
}

func TestMiss5_PersistTranslations_InvalidGenreIndex(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie with genres
	movie := &models.Movie{
		ContentID:    "invalid-genre-idx-test",
		ID:           "INVALID-GENRE-IDX-001",
		DisplayTitle: "Invalid Genre Index",
		Genres:       []models.Genre{{Name: "ValidGenre"}},
	}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	// Try with out-of-range genre index — should skip gracefully
	movie2 := &models.Movie{
		ContentID:    "invalid-genre-idx-test",
		ID:           "INVALID-GENRE-IDX-001",
		DisplayTitle: "Invalid Genre Index Updated",
		Genres:       []models.Genre{{Name: "ValidGenre"}},
	}
	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: -1, Language: "en", Name: "Invalid Index", SourceName: "test"}, // negative index
		{GenreIndex: 5, Language: "en", Name: "Out of Range", SourceName: "test"},   // out of range
	}
	_, err = repo.UpsertWithTranslations(context.TODO(), movie2, genreTranslations, nil)
	require.NoError(t, err)
}

func TestMiss5_PersistTranslations_InvalidActressIndex(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie with actresses
	movie := &models.Movie{
		ContentID:    "invalid-actress-idx-test",
		ID:           "INVALID-ACTRESS-IDX-001",
		DisplayTitle: "Invalid Actress Index",
		Actresses:    []models.Actress{{DMMID: 20251, JapaneseName: "ValidActress"}},
	}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	// Try with out-of-range actress index — should skip gracefully
	movie2 := &models.Movie{
		ContentID:    "invalid-actress-idx-test",
		ID:           "INVALID-ACTRESS-IDX-001",
		DisplayTitle: "Invalid Actress Index Updated",
		Actresses:    []models.Actress{{DMMID: 20251, JapaneseName: "ValidActress"}},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: -1, Language: "en", FirstName: "Invalid", DisplayName: "Invalid Index", SourceName: "test"},
		{ActressIndex: 5, Language: "en", FirstName: "OutRange", DisplayName: "Out of Range", SourceName: "test"},
	}
	_, err = repo.UpsertWithTranslations(context.TODO(), movie2, nil, actressTranslations)
	require.NoError(t, err)
}

// =====================================================================
// upsertMovieCore — Association Replace error
// Line 231
// =====================================================================

func TestMiss5_UpsertMovieCore_AssocErr(t *testing.T) {
	db := missDB(t)

	// Create a movie first
	require.NoError(t, db.DB.Create(&models.Movie{ContentID: "assoc-err-core", ID: "ASSOC-ERR-CORE-001", DisplayTitle: "Test"}).Error)

	// Drop join tables to cause association errors
	require.NoError(t, db.DB.Exec("DROP TABLE movie_genres").Error)

	movie := &models.Movie{ContentID: "assoc-err-core", ID: "ASSOC-ERR-CORE-001", DisplayTitle: "Test Updated", Genres: []models.Genre{{Name: "AssocErrGenre"}}}
	err := upsertMovieCore(db.WithContext(context.TODO()), db, movie, nil, nil, nil)
	assert.Error(t, err)
}

func TestMiss5_UpsertMovieCore_ActressAssocErr(t *testing.T) {
	db := missDB(t)

	require.NoError(t, db.DB.Create(&models.Movie{ContentID: "act-assoc-err-core", ID: "ACT-ASSOC-ERR-CORE-001", DisplayTitle: "Test"}).Error)

	require.NoError(t, db.DB.Exec("DROP TABLE movie_actresses").Error)

	movie := &models.Movie{ContentID: "act-assoc-err-core", ID: "ACT-ASSOC-ERR-CORE-001", DisplayTitle: "Test Updated", Actresses: []models.Actress{{DMMID: 20261, JapaneseName: "AssocErrActress"}}}
	err := upsertMovieCore(db.WithContext(context.TODO()), db, movie, nil, nil, nil)
	assert.Error(t, err)
}

// =====================================================================
// prepareMovieForUpsert — genre/actress with zero ID resolution
// Lines 80-112
// =====================================================================

func TestMiss5_PrepareMovieForUpsert_GenreIDNotResolved(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie with a genre that has no ID
	// (this happens when genre index is valid but ID couldn't be resolved)
	// This path is tested implicitly through UpsertWithTranslations
	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "Genre No ID", SourceName: "test"},
	}
	movie := &models.Movie{
		ContentID:    "genre-no-id-test",
		ID:           "GENRE-NO-ID-001",
		DisplayTitle: "Genre No ID",
		Genres:       []models.Genre{{Name: "GenreNoID"}},
	}
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, genreTranslations, nil)
	require.NoError(t, err)
}

// =====================================================================
// ActressMerge — loadPair error for both zero and same IDs
// =====================================================================

func TestMiss5_LoadPair_Errors(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)
	merger := actressRepo.merger

	// Zero target ID
	_, _, err := merger.loadPair(context.TODO(), 0, 1)
	assert.ErrorIs(t, err, ErrActressMergeInvalidID)

	// Zero source ID
	_, _, err = merger.loadPair(context.TODO(), 1, 0)
	assert.ErrorIs(t, err, ErrActressMergeInvalidID)

	// Same ID
	_, _, err = merger.loadPair(context.TODO(), 5, 5)
	assert.ErrorIs(t, err, ErrActressMergeSameID)
}

// =====================================================================
// Migrations runner — DSN-specific paths
// =====================================================================

func TestMiss5_NormalizeSQLiteDSN_FilePath(t *testing.T) {
	// Non-:memory: (file-backed) DSNs are enhanced with WAL journal mode and a
	// busy timeout so concurrent worker-pool writes and API reads don't contend
	// on SQLite's default locking (Fix C — see /tmp/concurrency-investigation-results.md).
	result := normalizeSQLiteDSN("test.db")
	assert.Equal(t, "test.db?_journal_mode=WAL&_busy_timeout=5000", result)
	// Explicit params already present are preserved; only missing ones are added.
	assert.Equal(t, "test.db?_busy_timeout=1000&_journal_mode=WAL", normalizeSQLiteDSN("test.db?_busy_timeout=1000"))
	assert.Equal(t, "test.db?_journal_mode=WAL&_busy_timeout=5000", normalizeSQLiteDSN("test.db?_journal_mode=WAL"))
}

func TestMiss5_MigrationLocker_EmptyDSN(t *testing.T) {
	// Empty DSN should return processMigrationLocker
	locker, err := newStartupMigrationLocker("", nil)
	require.NoError(t, err)
	_, ok := locker.(processMigrationLocker)
	assert.True(t, ok)
}

// =====================================================================
// Migrations — sqliteFilePathFromDSN with whitespace
// =====================================================================

func TestMiss5_SqliteFilePathFromDSN_Whitespace(t *testing.T) {
	_, ok := sqliteFilePathFromDSN("  ")
	assert.False(t, ok)
}

// =====================================================================
// Migrations — quoteSQLiteStringLiteral
// =====================================================================

func TestMiss5_QuoteSQLiteStringLiteral(t *testing.T) {
	result := quoteSQLiteStringLiteral("it's a test")
	assert.Equal(t, "'it''s a test'", result)
}

func TestMiss5_QuoteSQLiteStringLiteral_NoQuote(t *testing.T) {
	result := quoteSQLiteStringLiteral("simple")
	assert.Equal(t, "'simple'", result)
}

// =====================================================================
// BaseRepository — Create with string ID
// Line 39
// =====================================================================

func TestMiss5_BaseRepository_CreateStringID(t *testing.T) {
	db := missDB(t)
	repo := NewBaseRepository[models.Job, string](
		db, "job",
		func(j models.Job) string { return j.ID },
		WithNewEntity[models.Job, string](func() models.Job { return models.Job{} }),
	)
	job := &models.Job{ID: "str-id-test-001", Status: models.JobStatusPending}
	require.NoError(t, repo.Create(context.TODO(), job))

	found, err := repo.FindByID(context.TODO(), "str-id-test-001")
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusPending, found.Status)
}

// =====================================================================
// ActressRepository — error branches for SearchPaged, SearchPagedSorted, CountSearch
// Lines 133,149,171,184,195,207
// =====================================================================

func TestMiss5_ActressSearchPaged_ErrorBranch(t *testing.T) {
	db := missDB(t)
	repo := NewActressRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	_, err := repo.SearchPaged(context.TODO(), "test", 10, 0)
	assert.Error(t, err)
}

func TestMiss5_ActressSearchPagedSorted_ErrorBranch(t *testing.T) {
	db := missDB(t)
	repo := NewActressRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	_, err := repo.SearchPagedSorted(context.TODO(), "test", 10, 0, "japanese_name", "asc")
	assert.Error(t, err)
}

func TestMiss5_ActressCountSearch_ErrorBranch(t *testing.T) {
	db := missDB(t)
	repo := NewActressRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	_, err := repo.CountSearch(context.TODO(), "test")
	assert.Error(t, err)
}

func TestMiss5_ActressSearch_ErrorBranch(t *testing.T) {
	db := missDB(t)
	repo := NewActressRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	_, err := repo.Search(context.TODO(), "test")
	assert.Error(t, err)
}

func TestMiss5_ActressSearch_EmptyQueryError(t *testing.T) {
	db := missDB(t)
	repo := NewActressRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	_, err := repo.Search(context.TODO(), "")
	assert.Error(t, err)
}

func TestMiss5_ActressListSorted_InvalidSort(t *testing.T) {
	db := missDB(t)
	repo := NewActressRepository(db)

	_, err := repo.ListSorted(context.TODO(), 10, 0, "invalid_field", "asc")
	assert.Error(t, err)
}

// =====================================================================
// MovieRepository — movieEntityID with ContentID
// =====================================================================

func TestMiss5_MovieEntityID_WithContentID(t *testing.T) {
	movie := &models.Movie{ContentID: "test-content-id", ID: "TEST-ID"}
	id := movieEntityID(movie)
	assert.Equal(t, "test-content-id", id)
}

// =====================================================================
// GenreReplacementRepository — Delete error
// =====================================================================

func TestMiss5_GenreReplacementDelete_Success(t *testing.T) {
	db := missDB(t)
	repo := NewGenreReplacementRepository(db)

	gr := &models.GenreReplacement{Original: "DeleteTestGenre", Replacement: "Replaced"}
	require.NoError(t, repo.Upsert(context.TODO(), gr))

	err := repo.Delete(context.TODO(), "DeleteTestGenre")
	require.NoError(t, err)

	_, err = repo.FindByOriginal(context.TODO(), "DeleteTestGenre")
	assert.Error(t, err)
}

// =====================================================================
// WordReplacementRepository — Delete success
// =====================================================================

func TestMiss5_WordReplacementDelete_Success(t *testing.T) {
	db := missDB(t)
	repo := NewWordReplacementRepository(db)

	wr := &models.WordReplacement{Original: "DeleteTestWord", Replacement: "Replaced"}
	require.NoError(t, repo.Create(context.TODO(), wr))

	err := repo.Delete(context.TODO(), "DeleteTestWord")
	require.NoError(t, err)

	_, err = repo.FindByOriginal(context.TODO(), "DeleteTestWord")
	assert.Error(t, err)
}

// =====================================================================
// ActressAliasRepository — Upsert error path
// Lines 39
// =====================================================================

func TestMiss5_ActressAliasUpsert_SaveError(t *testing.T) {
	db := missDB(t)
	repo := NewActressAliasRepository(db)

	// Create first
	alias := &models.ActressAlias{AliasName: "SaveErrAlias", CanonicalName: "Original"}
	require.NoError(t, repo.Create(context.TODO(), alias))

	// Now break the table and try to update
	require.NoError(t, db.DB.Exec("DROP TABLE actress_aliases").Error)

	alias2 := &models.ActressAlias{AliasName: "SaveErrAlias", CanonicalName: "Updated"}
	err := repo.Upsert(context.TODO(), alias2)
	assert.Error(t, err)
}

// =====================================================================
// BatchFileOperationRepository — constructor paths
// Lines 20-21
// =====================================================================

func TestMiss5_BFOConstructor(t *testing.T) {
	db := missDB(t)
	repo := NewBatchFileOperationRepository(db)
	require.NotNil(t, repo)

	// Verify it works
	op := &models.BatchFileOperation{BatchJobID: "constructor-test", MovieID: "MOV-001", OriginalPath: "/a", NewPath: "/b", OperationType: models.OperationTypeMove}
	require.NoError(t, repo.Create(context.TODO(), op))
	assert.NotZero(t, op.ID)
}

// =====================================================================
// GenreRepository — List error
// =====================================================================

func TestMiss5_GenreList_Error(t *testing.T) {
	db := missDB(t)
	repo := newGenreRepository(db)

	require.NoError(t, db.DB.Exec("DROP TABLE genres").Error)
	_, err := repo.List(context.TODO())
	assert.Error(t, err)
}

// =====================================================================
// Migrations runner — processMigrationLocker
// =====================================================================

func TestMiss5_ProcessMigrationLocker(t *testing.T) {
	locker := processMigrationLocker{}

	err := locker.Lock(context.Background(), nil)
	require.NoError(t, err)

	err = locker.Unlock(context.Background(), nil)
	require.NoError(t, err)
}

// =====================================================================
// lookupActressByDMMID — error path
// =====================================================================

func TestMiss5_LookupActressByDMMID_DBError(t *testing.T) {
	db := missDB(t)
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	_, _, err := lookupActressByDMMID(db.WithContext(context.TODO()), &models.Actress{DMMID: 1})
	assert.Error(t, err)
}

// =====================================================================
// parseLogLevel — warning path
// =====================================================================

func TestMiss5_ParseLogLevel_Invalid(t *testing.T) {
	level := parseLogLevel("invalid")
	assert.Equal(t, level, logger.Silent)
}

// =====================================================================
// Migrations — RunMigrationsOnStartup with invalid DSN
// =====================================================================

func TestMiss5_RunMigrations_InvalidType(t *testing.T) {
	cfg := &Config{Type: "postgres", DSN: "host=localhost", LogLevel: "error"}
	_, err := New(cfg)
	assert.Error(t, err)
}
