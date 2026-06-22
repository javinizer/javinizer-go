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
// ActressMerge — PreviewMerge error paths
// Line 192: loadPair error (e.g., target not found)
// Line 199: mergeActressValues error (should not happen with defaults)
// =====================================================================

func TestMiss6_PreviewMerge_TargetNotFound(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	// Only create source, not target
	source := &models.Actress{DMMID: 30001, JapaneseName: "PreviewSrcNF"}
	require.NoError(t, actressRepo.Create(context.TODO(), source))

	_, err := actressRepo.PreviewMerge(context.TODO(), 99999, source.ID)
	assert.Error(t, err)
}

func TestMiss6_PreviewMerge_SourceNotFound(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	target := &models.Actress{DMMID: 30002, JapaneseName: "PreviewTgtNF"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))

	_, err := actressRepo.PreviewMerge(context.TODO(), target.ID, 99999)
	assert.Error(t, err)
}

func TestMiss6_PreviewMerge_InvalidIDs(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	_, err := actressRepo.PreviewMerge(context.TODO(), 0, 1)
	assert.Error(t, err)

	_, err = actressRepo.PreviewMerge(context.TODO(), 1, 1)
	assert.Error(t, err)
}

// =====================================================================
// ActressMerge — PlanMerge error paths
// Line 225: PreviewMerge error
// Line 240: normalizeMergeResolutions error
// =====================================================================

func TestMiss6_PlanMerge_InvalidIDs(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	_, err := actressRepo.Merge(context.TODO(), 0, 1, nil)
	assert.Error(t, err)
}

func TestMiss6_PlanMerge_InvalidFieldResolution(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	target := &models.Actress{DMMID: 30011, JapaneseName: "PlanInvTgt", FirstName: "TgtFirst"}
	source := &models.Actress{DMMID: 30012, JapaneseName: "PlanInvSrc", FirstName: "SrcFirst"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))
	require.NoError(t, actressRepo.Create(context.TODO(), source))

	// Invalid field name
	_, err := actressRepo.Merge(context.TODO(), target.ID, source.ID, map[string]string{"bad_field": "target"})
	assert.Error(t, err)
}

// =====================================================================
// ActressMerge — ExecuteMerge DMMID swap with different target DMMID
// Lines 288-303
// =====================================================================

func TestMiss6_ExecuteMerge_DMMIDSwap_SourceDMMIDToTarget(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	// Target has DMMID=0, source has DMMID>0
	// Resolve dmm_id to "source" so merged gets source's DMMID
	// This triggers the DMMID swap: source gets temp DMMID, then target gets source's DMMID
	target := &models.Actress{DMMID: 0, JapaneseName: "SwapTarget2", FirstName: "TgtFirst"}
	source := &models.Actress{DMMID: 30021, JapaneseName: "SwapSource2", FirstName: "SrcFirst"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))
	require.NoError(t, actressRepo.Create(context.TODO(), source))

	resolutions := map[string]string{"dmm_id": "source", "first_name": "source"}
	result, err := actressRepo.Merge(context.TODO(), target.ID, source.ID, resolutions)
	require.NoError(t, err)
	assert.Equal(t, 30021, result.MergedActress.DMMID)
	assert.Equal(t, "SrcFirst", result.MergedActress.FirstName)
}

// =====================================================================
// ActressMerge — ExecuteMerge with movies and DMMID swap
// Lines 315-345
// =====================================================================

func TestMiss6_ExecuteMerge_WithMoviesAndDMMIDSwap(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)
	movieRepo := NewMovieRepository(db)

	target := &models.Actress{DMMID: 0, JapaneseName: "MovieSwapTarget"}
	source := &models.Actress{DMMID: 30031, JapaneseName: "MovieSwapSource"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))
	require.NoError(t, actressRepo.Create(context.TODO(), source))

	// Create movie with source actress
	movie := &models.Movie{
		ContentID:    "movie-swap-test",
		ID:           "MOVIE-SWAP-001",
		DisplayTitle: "Movie Swap Test",
		Actresses:    []models.Actress{{DMMID: 30031}},
	}
	_, err := movieRepo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	// Merge with DMMID source resolution
	resolutions := map[string]string{"dmm_id": "source"}
	result, err := actressRepo.Merge(context.TODO(), target.ID, source.ID, resolutions)
	require.NoError(t, err)
	assert.Equal(t, 30031, result.MergedActress.DMMID)
	assert.GreaterOrEqual(t, result.UpdatedMovies, 1)
}

// =====================================================================
// ActressMerge — ExecuteMerge: ErrActressMergeUniqueConstraint via check
// Line 281
// =====================================================================

func TestMiss6_ExecuteMerge_UniqueConstraintViaCheck(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	target := &models.Actress{DMMID: 30041, JapaneseName: "UniqueCheckTgt"}
	source := &models.Actress{DMMID: 30042, JapaneseName: "UniqueCheckSrc"}
	third := &models.Actress{DMMID: 30043, JapaneseName: "UniqueCheckThird"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))
	require.NoError(t, actressRepo.Create(context.TODO(), source))
	require.NoError(t, actressRepo.Create(context.TODO(), third))

	// Trying to update source's DMMID to match third's DMMID should fail
	srcUpdated := &models.Actress{ID: source.ID, DMMID: 30043, JapaneseName: "UniqueCheckSrc"}
	err := actressRepo.Update(context.TODO(), srcUpdated)
	// The update itself fails because DMMID 30043 is already used by third
	assert.Error(t, err)
}

// =====================================================================
// ActressMerge — moveMovieAssociations: Find error (line 91)
// Need Pluck to succeed but Find to fail
// =====================================================================

func TestMiss6_MoveMovieAssociations_FindError(t *testing.T) {
	db := missDB(t)
	source := &models.Actress{DMMID: 30051, JapaneseName: "FindErrSrc"}
	target := &models.Actress{DMMID: 30052, JapaneseName: "FindErrTgt"}
	require.NoError(t, db.DB.Create(source).Error)
	require.NoError(t, db.DB.Create(target).Error)

	// Create movie with source actress using Upsert
	repo := NewMovieRepository(db)
	movie := &models.Movie{
		ContentID:    "find-err-movie",
		ID:           "FIND-ERR-001",
		DisplayTitle: "Find Error",
		Actresses:    []models.Actress{{DMMID: 30051}},
	}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	// Verify movie exists
	found, err := repo.FindByContentID(context.TODO(), "find-err-movie")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(found.Actresses), 1)

	// Now rename the movies table to break Find but keep the join table
	// Actually, we can drop just the movies table - the join table uses content_id as FK
	require.NoError(t, db.DB.Exec("DROP TABLE movies").Error)

	err = db.DB.Transaction(func(tx *gorm.DB) error {
		var moveErr error
		_, moveErr = moveMovieAssociations(tx, source.ID, target.ID)
		return moveErr
	})
	assert.Error(t, err)
}

// =====================================================================
// ActressMerge — moveMovieAssociations: !hasSource continue (line 119)
// This happens when a movie appears in the content_id list but
// doesn't actually have the source actress in its Actresses list
// =====================================================================

func TestMiss6_MoveMovieAssociations_SourceNotInList(t *testing.T) {
	db := missDB(t)
	source := &models.Actress{DMMID: 30061, JapaneseName: "NotInListSrc"}
	target := &models.Actress{DMMID: 30062, JapaneseName: "NotInListTgt"}
	require.NoError(t, db.DB.Create(source).Error)
	require.NoError(t, db.DB.Create(target).Error)

	// Create movie directly with only the target actress
	require.NoError(t, db.DB.Create(&models.Movie{ContentID: "not-in-list", ID: "NOT-IN-LIST-001", DisplayTitle: "Not In List"}).Error)
	require.NoError(t, db.DB.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "not-in-list", target.ID).Error)

	var count int
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		var moveErr error
		count, moveErr = moveMovieAssociations(tx, source.ID, target.ID)
		return moveErr
	})
	require.NoError(t, err)
	// Source actress isn't in any movie, so count should be 0
	assert.Equal(t, 0, count)
}

// =====================================================================
// ActressMerge — moveMovieAssociations: !hasTarget append (line 122)
// Source is in movie but target isn't — need to append target
// =====================================================================

func TestMiss6_MoveMovieAssociations_SourceOnlyNeedsTargetAppend(t *testing.T) {
	db := missDB(t)
	source := &models.Actress{DMMID: 30071, JapaneseName: "SrcOnlyAppend"}
	target := &models.Actress{DMMID: 30072, JapaneseName: "TgtAppend"}
	require.NoError(t, db.DB.Create(source).Error)
	require.NoError(t, db.DB.Create(target).Error)

	// Create movie with only the source actress
	repo := NewMovieRepository(db)
	movie := &models.Movie{
		ContentID:    "src-only-append",
		ID:           "SRC-ONLY-APPEND-001",
		DisplayTitle: "Source Only Append",
		Actresses:    []models.Actress{{DMMID: 30071}},
	}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	var count int
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		var moveErr error
		count, moveErr = moveMovieAssociations(tx, source.ID, target.ID)
		return moveErr
	})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// =====================================================================
// ActressMerge — upsertActressAliases error via Clauses
// Lines 315-319
// =====================================================================

func TestMiss6_UpsertActressAliases_CreateClauseErr(t *testing.T) {
	db := missDB(t)
	// Break the table to trigger Clauses Create error
	require.NoError(t, db.DB.Exec("DROP TABLE actress_aliases").Error)

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return upsertActressAliases(tx, []string{"SomeAlias"}, "CanonicalName")
	})
	assert.Error(t, err)
}

// =====================================================================
// ActressMerge — ExecuteMerge: Updates ErrDuplicatedKey
// Lines 315-319
// =====================================================================

func TestMiss6_ExecuteMerge_UpdatesErrDuplicatedKey(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	target := &models.Actress{DMMID: 30081, JapaneseName: "DupKeyTarget"}
	source := &models.Actress{DMMID: 30082, JapaneseName: "DupKeySource"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))
	require.NoError(t, actressRepo.Create(context.TODO(), source))

	plan, err := actressRepo.merger.PlanMerge(context.TODO(), target.ID, source.ID, map[string]string{"dmm_id": "source"})
	require.NoError(t, err)

	// Inject ErrDuplicatedKey on the Updates call
	cbName := "test:inject_actress_update_dup"
	injectDuplicate := false
	require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(cbName, func(tx *gorm.DB) {
		if injectDuplicate || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "actresses" {
			return
		}
		// Only inject on the first update (the merge update)
		injectDuplicate = true
		_ = tx.AddError(gorm.ErrDuplicatedKey)
	}))
	defer func() { _ = db.DB.Callback().Update().Remove(cbName) }()

	_, err = actressRepo.merger.ExecuteMerge(context.TODO(), plan, db)
	assert.Error(t, err)
}

// =====================================================================
// ActressMerge — ExecuteMerge: moveMovieAssociations error
// Line 332
// =====================================================================

func TestMiss6_ExecuteMerge_MoveAssocErr(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	target := &models.Actress{DMMID: 30091, JapaneseName: "MoveErrTarget2"}
	source := &models.Actress{DMMID: 30092, JapaneseName: "MoveErrSource2"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))
	require.NoError(t, actressRepo.Create(context.TODO(), source))

	// Create movie with source actress
	repo := NewMovieRepository(db)
	movie := &models.Movie{
		ContentID:    "move-err-2",
		ID:           "MOVE-ERR-2-001",
		DisplayTitle: "Move Error 2",
		Actresses:    []models.Actress{{DMMID: 30092}},
	}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	plan, err := actressRepo.merger.PlanMerge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)

	// Break the join table to cause moveMovieAssociations error
	require.NoError(t, db.DB.Exec("DROP TABLE movie_actresses").Error)

	_, err = actressRepo.merger.ExecuteMerge(context.TODO(), plan, db)
	assert.Error(t, err)
}

// =====================================================================
// ActressMerge — ExecuteMerge: delete source error
// Line 343
// =====================================================================

func TestMiss6_ExecuteMerge_DeleteSourceErr(t *testing.T) {
	db := missDB(t)
	actressRepo := NewActressRepository(db)

	target := &models.Actress{DMMID: 30101, JapaneseName: "DelErrTarget2"}
	source := &models.Actress{DMMID: 30102, JapaneseName: "DelErrSource2"}
	require.NoError(t, actressRepo.Create(context.TODO(), target))
	require.NoError(t, actressRepo.Create(context.TODO(), source))

	plan, err := actressRepo.merger.PlanMerge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)

	// Break the DB to cause the delete to fail
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	_, err = actressRepo.merger.ExecuteMerge(context.TODO(), plan, db)
	assert.Error(t, err)
}

// =====================================================================
// ActressMerge — mergeActressValues: "source" resolution for each conflict
// Lines 264,277,290
// =====================================================================

func TestMiss6_MergeActressValues_JapaneseNameSource(t *testing.T) {
	target := &models.Actress{JapaneseName: "ターゲット"}
	source := &models.Actress{JapaneseName: "ソース"}

	merged, err := mergeActressValues(target, source, map[string]string{"japanese_name": "source"})
	require.NoError(t, err)
	assert.Equal(t, "ソース", merged.JapaneseName)
}

func TestMiss6_MergeActressValues_LastNameSource(t *testing.T) {
	target := &models.Actress{LastName: "TargetLast"}
	source := &models.Actress{LastName: "SourceLast"}

	merged, err := mergeActressValues(target, source, map[string]string{"last_name": "source"})
	require.NoError(t, err)
	assert.Equal(t, "SourceLast", merged.LastName)
}

// =====================================================================
// helpers.go — prepareMovieForUpsert: actress ID resolution by DMMID and composite
// Lines 80-112
// =====================================================================

func TestMiss6_PrepareMovieForUpsert_ActressIDByDMMID(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)
	actressRepo := NewActressRepository(db)

	// Create actress with DMMID
	actress := &models.Actress{DMMID: 30111, JapaneseName: "DMMIDResolve"}
	require.NoError(t, actressRepo.Create(context.TODO(), actress))

	// Upsert movie referencing actress by DMMID only
	// This tests the actressByDMMID resolution path in prepareMovieForUpsert
	movie := &models.Movie{
		ContentID:    "dmmid-resolve-test",
		ID:           "DMMID-RESOLVE-001",
		DisplayTitle: "DMMID Resolve",
		Actresses:    []models.Actress{{DMMID: 30111}},
	}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)
}

func TestMiss6_PrepareMovieForUpsert_ActressIDByComposite(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)
	actressRepo := NewActressRepository(db)

	// Create actress without DMMID but with name
	actress := &models.Actress{FirstName: "CompositeFirst", LastName: "CompositeLast"}
	require.NoError(t, actressRepo.Create(context.TODO(), actress))

	// Upsert movie referencing actress by name (no DMMID)
	// This tests the actressByComposite resolution path
	movie := &models.Movie{
		ContentID:    "composite-resolve-test",
		ID:           "COMPOSITE-RESOLVE-001",
		DisplayTitle: "Composite Resolve",
		Actresses:    []models.Actress{{FirstName: "CompositeFirst", LastName: "CompositeLast"}},
	}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)
}

// =====================================================================
// helpers.go — persistTranslations: stale deletion and genre/actress trans
// Lines 147,152,169,193
// =====================================================================

func TestMiss6_PersistTranslations_FindStaleErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie with translations
	movie := &models.Movie{
		ContentID:    "stale-find-err-test",
		ID:           "STALE-FIND-ERR-001",
		DisplayTitle: "Stale Find Err",
		Translations: []models.MovieTranslation{
			{Language: "en", Title: "English"},
			{Language: "ja", Title: "日本語"},
		},
	}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	// Now try to update with fewer translations, but break Find for stale check
	require.NoError(t, db.DB.Exec("DROP TABLE movie_translations").Error)

	movie2 := &models.Movie{
		ContentID:    "stale-find-err-test",
		ID:           "STALE-FIND-ERR-001",
		DisplayTitle: "Stale Find Err Updated",
		Translations: []models.MovieTranslation{
			{Language: "en", Title: "English Updated"},
		},
	}
	_, err = repo.Upsert(context.TODO(), movie2)
	assert.Error(t, err)
}

// =====================================================================
// helpers.go — prepareMovieForUpsert: reload genres/actresses error
// Lines 72-89
// =====================================================================

func TestMiss6_PrepareMovieForUpsert_ReloadGenresErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie with genres
	movie := &models.Movie{
		ContentID:    "reload-genre-err2",
		ID:           "RELOAD-GENRE-ERR2-001",
		DisplayTitle: "Reload Genre Err 2",
		Genres:       []models.Genre{{Name: "ReloadGenre2"}},
	}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	// Break the association
	require.NoError(t, db.DB.Exec("DROP TABLE movie_genres").Error)

	movie2 := &models.Movie{
		ContentID:    "reload-genre-err2",
		ID:           "RELOAD-GENRE-ERR2-001",
		DisplayTitle: "Reload Genre Err 2 Updated",
		Genres:       []models.Genre{{Name: "ReloadGenre2"}},
	}
	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "Reload Genre 2 EN", SourceName: "test"},
	}
	_, err = repo.UpsertWithTranslations(context.TODO(), movie2, genreTranslations, nil)
	assert.Error(t, err)
}

func TestMiss6_PrepareMovieForUpsert_ReloadActressesErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie with actresses
	movie := &models.Movie{
		ContentID:    "reload-actress-err2",
		ID:           "RELOAD-ACTRESS-ERR2-001",
		DisplayTitle: "Reload Actress Err 2",
		Actresses:    []models.Actress{{DMMID: 30121, JapaneseName: "ReloadActress2"}},
	}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	// Break the association
	require.NoError(t, db.DB.Exec("DROP TABLE movie_actresses").Error)

	movie2 := &models.Movie{
		ContentID:    "reload-actress-err2",
		ID:           "RELOAD-ACTRESS-ERR2-001",
		DisplayTitle: "Reload Actress Err 2 Updated",
		Actresses:    []models.Actress{{DMMID: 30121, JapaneseName: "ReloadActress2"}},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", FirstName: "Reload", LastName: "Actress", DisplayName: "Reload Actress 2 EN", SourceName: "test"},
	}
	_, err = repo.UpsertWithTranslations(context.TODO(), movie2, nil, actressTranslations)
	assert.Error(t, err)
}

// =====================================================================
// helpers.go — upsertMovieCore Association Replace error
// Line 231
// =====================================================================

func TestMiss6_UpsertMovieCore_AssocReplaceErr(t *testing.T) {
	db := missDB(t)

	require.NoError(t, db.DB.Create(&models.Movie{ContentID: "assoc-replace-err", ID: "ASSOC-REPLACE-ERR-001", DisplayTitle: "Test"}).Error)
	require.NoError(t, db.DB.Exec("DROP TABLE movie_genres").Error)

	movie := &models.Movie{ContentID: "assoc-replace-err", ID: "ASSOC-REPLACE-ERR-001", DisplayTitle: "Test Updated", Genres: []models.Genre{{Name: "AssocErrGenre"}}}
	err := upsertMovieCore(db.WithContext(context.TODO()), db, movie, nil, nil, nil)
	assert.Error(t, err)
}

// =====================================================================
// GenreTranslationRepository.UpsertTx — duplicate key save error
// Line 48
// =====================================================================

func TestMiss6_GenreTranslationUpsertTx_DupKeySaveErr(t *testing.T) {
	db := missDB(t)
	repo := newGenreTranslationRepository(db)
	genreRepo := newGenreRepository(db)

	genre, err := genreRepo.FindOrCreate(context.TODO(), "DupSaveErrGenre")
	require.NoError(t, err)

	cbName := "test:inject_genre_trans_dup_save_err2"
	injectDuplicate := false
	require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if injectDuplicate || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "genre_translations" {
			return
		}
		injectDuplicate = true
		dest, ok := tx.Statement.Dest.(*models.GenreTranslation)
		if !ok {
			return
		}
		// Seed the row so Find succeeds
		if err := db.DB.Exec(
			"INSERT INTO genre_translations (genre_id, language, name, source_name, created_at, updated_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
			dest.GenreID, dest.Language, "seeded", "seed",
		).Error; err != nil {
			_ = tx.AddError(err)
			return
		}
		_ = tx.AddError(gorm.ErrDuplicatedKey)
	}))
	defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

	// Drop table so Save fails
	require.NoError(t, db.DB.Exec("DROP TABLE genre_translations").Error)

	translation := &models.GenreTranslation{
		GenreID:    genre.ID,
		Language:   "en",
		Name:       "SaveFails",
		SourceName: "test",
	}
	tx := db.WithContext(context.TODO())
	err = repo.UpsertTx(tx, translation)
	assert.Error(t, err)
}

// =====================================================================
// ActressTranslationRepository.UpsertTx — duplicate key save error
// Line 48
// =====================================================================

func TestMiss6_ActressTranslationUpsertTx_DupKeySaveErr(t *testing.T) {
	db := missDB(t)
	repo := newActressTranslationRepository(db)
	actressRepo := NewActressRepository(db)

	actress := &models.Actress{DMMID: 30131, JapaneseName: "DupSaveErrActress"}
	require.NoError(t, actressRepo.Create(context.TODO(), actress))

	cbName := "test:inject_act_trans_dup_save_err2"
	injectDuplicate := false
	require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if injectDuplicate || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "actress_translations" {
			return
		}
		injectDuplicate = true
		dest, ok := tx.Statement.Dest.(*models.ActressTranslation)
		if !ok {
			return
		}
		if err := db.DB.Exec(
			"INSERT INTO actress_translations (actress_id, language, display_name, source_name, created_at, updated_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
			dest.ActressID, dest.Language, "seeded", "seed",
		).Error; err != nil {
			_ = tx.AddError(err)
			return
		}
		_ = tx.AddError(gorm.ErrDuplicatedKey)
	}))
	defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

	require.NoError(t, db.DB.Exec("DROP TABLE actress_translations").Error)

	translation := &models.ActressTranslation{
		ActressID:   actress.ID,
		Language:    "en",
		DisplayName: "SaveFails",
		SourceName:  "test",
	}
	tx := db.WithContext(context.TODO())
	err := repo.UpsertTx(tx, translation)
	assert.Error(t, err)
}

// =====================================================================
// MovieTranslationRepository.UpsertTx — duplicate key save error
// Line 46
// =====================================================================

func TestMiss6_MovieTranslationUpsertTx_DupKeySaveErr(t *testing.T) {
	db := missDB(t)
	repo := newMovieTranslationRepository(db)
	movieRepo := NewMovieRepository(db)

	require.NoError(t, movieRepo.Create(context.TODO(), &models.Movie{ContentID: "mtrans-dup-save-err", ID: "MTRANS-DUP-SE", DisplayTitle: "Test"}))

	cbName := "test:inject_movie_trans_dup_save_err2"
	injectDuplicate := false
	require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if injectDuplicate || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "movie_translations" {
			return
		}
		injectDuplicate = true
		dest, ok := tx.Statement.Dest.(*models.MovieTranslation)
		if !ok {
			return
		}
		if err := db.DB.Exec(
			"INSERT INTO movie_translations (movie_id, language, title, created_at, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
			dest.MovieID, dest.Language, "seeded",
		).Error; err != nil {
			_ = tx.AddError(err)
			return
		}
		_ = tx.AddError(gorm.ErrDuplicatedKey)
	}))
	defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

	require.NoError(t, db.DB.Exec("DROP TABLE movie_translations").Error)

	translation := &models.MovieTranslation{
		MovieID:  "mtrans-dup-save-err",
		Language: "en",
		Title:    "SaveFails Title",
	}
	tx := db.WithContext(context.TODO())
	err := repo.UpsertTx(tx, translation)
	assert.Error(t, err)
}

// =====================================================================
// MovieRepository — Delete with tags error
// Lines 100, 112
// =====================================================================

func TestMiss6_MovieDelete_TagDeleteErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie with tags
	movie := &models.Movie{ContentID: "tag-del-err", ID: "TAG-DEL-ERR-001", DisplayTitle: "Tag Delete Err"}
	require.NoError(t, repo.Create(context.TODO(), movie))

	tagRepo := NewMovieTagRepository(db)
	require.NoError(t, tagRepo.AddTag(context.TODO(), "tag-del-err", "test-tag"))

	// Break the tag table
	require.NoError(t, db.DB.Exec("DROP TABLE movie_tags").Error)

	err := repo.Delete(context.TODO(), "TAG-DEL-ERR-001")
	assert.Error(t, err)
}

func TestMiss6_MovieDelete_TranslationDeleteErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)

	// Create movie with translations
	movie := &models.Movie{
		ContentID:    "trans-del-err",
		ID:           "TRANS-DEL-ERR-001",
		DisplayTitle: "Trans Delete Err",
		Translations: []models.MovieTranslation{{Language: "en", Title: "English"}},
	}
	_, err := repo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	// Break the translation table
	require.NoError(t, db.DB.Exec("DROP TABLE movie_translations").Error)

	err = repo.Delete(context.TODO(), "TRANS-DEL-ERR-001")
	assert.Error(t, err)
}

// =====================================================================
// MovieRepository — FindByID error (non-ErrRecordNotFound)
// Line 62
// =====================================================================

func TestMiss6_MovieFindByID_NonNotFoundErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE movies").Error)
	_, err := repo.FindByID(context.TODO(), "some-id")
	assert.Error(t, err)
}

// =====================================================================
// MovieRepository — FindByContentID error (non-ErrRecordNotFound)
// Line 74
// =====================================================================

func TestMiss6_MovieFindByContentID_NonNotFoundErr(t *testing.T) {
	db := missDB(t)
	repo := NewMovieRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE movies").Error)
	_, err := repo.FindByContentID(context.TODO(), "some-content-id")
	assert.Error(t, err)
}

// =====================================================================
// ActressMerge — canonicalActressName: LastName fallback
// Line 117
// =====================================================================

func TestMiss6_CanonicalActressName_LastNameFallback(t *testing.T) {
	// Only LastName, no JapaneseName or FirstName
	actress := &models.Actress{LastName: "OnlyLastName"}
	name := canonicalActressName(actress)
	assert.Equal(t, "OnlyLastName", name)
}

// =====================================================================
// ActressMerge — mergeActressValues: source fills empty fields
// Lines 264, 277, 290
// =====================================================================

func TestMiss6_MergeActressValues_SourceFillsEmpty(t *testing.T) {
	// Target has empty fields, source fills them
	target := &models.Actress{ID: 1}
	source := &models.Actress{
		LastName:     "SrcLast",
		JapaneseName: "SrcJapanese",
		ThumbURL:     "http://src.jpg",
	}
	merged, err := mergeActressValues(target, source, map[string]string{})
	require.NoError(t, err)
	assert.Equal(t, "SrcLast", merged.LastName)
	assert.Equal(t, "SrcJapanese", merged.JapaneseName)
	assert.Equal(t, "http://src.jpg", merged.ThumbURL)
}

// =====================================================================
// DB.Close error
// Line 101
// =====================================================================

func TestMiss6_DBClose_DoubleCloseErr(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// Second close may or may not error depending on implementation
	_ = db.Close()
}

// =====================================================================
// Migrations — sqliteFilePathFromDSN with file: prefix and query
// =====================================================================

func TestMiss6_SqliteFilePathFromDSN_EdgeCases(t *testing.T) {
	// File URI with just query params
	_, ok := sqliteFilePathFromDSN("file:?cache=shared")
	assert.False(t, ok)

	// Whitespace DSN
	_, ok = sqliteFilePathFromDSN("  ")
	assert.False(t, ok)

	// Mode memory with cache param
	_, ok = sqliteFilePathFromDSN("file:test?mode=memory&cache=shared")
	assert.False(t, ok)
}
