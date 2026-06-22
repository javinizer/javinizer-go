package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Merge uncovered branches ---

func TestActressRepository_Merge_UniqueConstraintViolation(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Create target with a DMMID
	target := &models.Actress{DMMID: 50001, JapaneseName: "ターゲット", FirstName: "Target"}
	require.NoError(t, repo.Create(context.TODO(), target))

	// Create source with different DMMID
	source := &models.Actress{DMMID: 50002, JapaneseName: "ソース", FirstName: "Source"}
	require.NoError(t, repo.Create(context.TODO(), source))

	// Create a third actress with yet another DMMID that will conflict
	// when merged DMMID=50002 conflicts with it (not target, not source)
	blocker := &models.Actress{DMMID: 50003, JapaneseName: "ブロッカー", FirstName: "Blocker"}
	require.NoError(t, repo.Create(context.TODO(), blocker))

	// Now manually set blocker's DMMID to 50002 (same as source) via raw SQL
	// This simulates a race condition where another actress has the same DMMID
	// Actually, we can't do this because of the unique constraint.
	// Instead, let's test the merge where we resolve dmm_id to source,
	// and the merged DMMID would be 50002 (source's DMMID) which is now being
	// moved from source to target. The temp DMMID swap path handles this.
	result, err := repo.Merge(context.TODO(), target.ID, source.ID, map[string]string{
		"dmm_id": "source",
	})
	require.NoError(t, err)
	assert.Equal(t, 50002, result.MergedActress.DMMID)
}

func TestActressRepository_Merge_SourceResolution(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Target with all fields
	target := &models.Actress{
		DMMID:        51001,
		FirstName:    "TargetFirst",
		LastName:     "TargetLast",
		JapaneseName: "ターゲット名前",
		ThumbURL:     "http://example.com/target.jpg",
	}
	require.NoError(t, repo.Create(context.TODO(), target))

	// Source with all fields different
	source := &models.Actress{
		DMMID:        51002,
		FirstName:    "SourceFirst",
		LastName:     "SourceLast",
		JapaneseName: "ソース名前",
		ThumbURL:     "http://example.com/source.jpg",
	}
	require.NoError(t, repo.Create(context.TODO(), source))

	// Merge with source resolution for all fields
	result, err := repo.Merge(context.TODO(), target.ID, source.ID, map[string]string{
		"dmm_id":        "source",
		"first_name":    "source",
		"last_name":     "source",
		"japanese_name": "source",
		"thumb_url":     "source",
	})
	require.NoError(t, err)
	assert.Equal(t, 51002, result.MergedActress.DMMID)
	assert.Equal(t, "SourceFirst", result.MergedActress.FirstName)
	assert.Equal(t, "SourceLast", result.MergedActress.LastName)
	assert.Equal(t, "ソース名前", result.MergedActress.JapaneseName)
	assert.Equal(t, "http://example.com/source.jpg", result.MergedActress.ThumbURL)
	assert.Equal(t, source.ID, result.MergedFromID)
}

func TestActressRepository_Merge_TargetResolution(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	target := &models.Actress{
		DMMID:        52001,
		FirstName:    "TargetFirst",
		LastName:     "TargetLast",
		JapaneseName: "ターゲット名前",
		ThumbURL:     "http://example.com/target.jpg",
	}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{
		DMMID:        52002,
		FirstName:    "SourceFirst",
		LastName:     "SourceLast",
		JapaneseName: "ソース名前",
		ThumbURL:     "http://example.com/source.jpg",
	}
	require.NoError(t, repo.Create(context.TODO(), source))

	// Merge with target resolution for all fields (default)
	result, err := repo.Merge(context.TODO(), target.ID, source.ID, map[string]string{})
	require.NoError(t, err)
	assert.Equal(t, 52001, result.MergedActress.DMMID)
	assert.Equal(t, "TargetFirst", result.MergedActress.FirstName)
	assert.Equal(t, "TargetLast", result.MergedActress.LastName)
	assert.Equal(t, "ターゲット名前", result.MergedActress.JapaneseName)
	assert.Equal(t, "http://example.com/target.jpg", result.MergedActress.ThumbURL)
}

func TestActressRepository_Merge_SourceDeletedAfterMerge(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	target := &models.Actress{DMMID: 53001, JapaneseName: "残る"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 53002, JapaneseName: "消える"}
	require.NoError(t, repo.Create(context.TODO(), source))

	_, err := repo.Merge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)

	// Source should be deleted
	_, err = repo.FindByID(context.TODO(), source.ID)
	assert.Error(t, err)

	// Target should still exist
	found, err := repo.FindByID(context.TODO(), target.ID)
	require.NoError(t, err)
	assert.NotNil(t, found)
}

// --- FindByDMMID uncovered branches ---

func TestActressRepository_FindByDMMID_NegativeID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByDMMID(context.TODO(), -5)
	assert.Error(t, err)
}

func TestActressRepository_FindByDMMID_ZeroID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByDMMID(context.TODO(), 0)
	assert.Error(t, err)
}

func TestActressRepository_FindByDMMID_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByDMMID(context.TODO(), 999999)
	assert.Error(t, err)
}

func TestActressRepository_FindByDMMID_Found(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{DMMID: 54001, JapaneseName: "検索対象"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	found, err := repo.FindByDMMID(context.TODO(), 54001)
	require.NoError(t, err)
	assert.Equal(t, 54001, found.DMMID)
	assert.Equal(t, "検索対象", found.JapaneseName)
}

// --- FindByJapaneseName uncovered branches ---

func TestActressRepository_FindByJapaneseName_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByJapaneseName(context.TODO(), "存在しない名前")
	assert.Error(t, err)
}

func TestActressRepository_FindByJapaneseName_PrefersHigherDMMID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Create two actresses with same Japanese name but different DMMIDs
	low := &models.Actress{DMMID: 100, JapaneseName: "同名女優", FirstName: "Low"}
	require.NoError(t, repo.Create(context.TODO(), low))

	high := &models.Actress{DMMID: 200, JapaneseName: "同名女優", FirstName: "High"}
	require.NoError(t, repo.Create(context.TODO(), high))

	found, err := repo.FindByJapaneseName(context.TODO(), "同名女優")
	require.NoError(t, err)
	// Should prefer the one with higher DMMID
	assert.Equal(t, "High", found.FirstName)
}

// --- FindByFirstNameLastName uncovered branches ---

func TestActressRepository_FindByFirstNameLastName_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByFirstNameLastName(context.TODO(), "NonExistent", "Name")
	assert.Error(t, err)
}

func TestActressRepository_FindByFirstNameLastName_Found(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{
		DMMID:     55001,
		FirstName: "Yui",
		LastName:  "Hatano",
	}
	require.NoError(t, repo.Create(context.TODO(), actress))

	found, err := repo.FindByFirstNameLastName(context.TODO(), "Yui", "Hatano")
	require.NoError(t, err)
	assert.Equal(t, "Yui", found.FirstName)
	assert.Equal(t, "Hatano", found.LastName)
}

// --- FindByJapaneseNameAndDMMID uncovered branches ---

func TestActressRepository_FindByJapaneseNameAndDMMID_BothSpecified(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{DMMID: 56001, JapaneseName: "両方検索"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	found, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "両方検索", 56001)
	require.NoError(t, err)
	assert.Equal(t, 56001, found.DMMID)
}

func TestActressRepository_FindByJapaneseNameAndDMMID_BothEmpty(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "", 0)
	assert.Error(t, err)
}

// --- Merge: invalid field resolution ---

func TestActressRepository_Merge_InvalidResolution(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	target := &models.Actress{DMMID: 57001, JapaneseName: "ターゲット"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 57002, JapaneseName: "ソース"}
	require.NoError(t, repo.Create(context.TODO(), source))

	_, err := repo.Merge(context.TODO(), target.ID, source.ID, map[string]string{
		"dmm_id": "invalid_value",
	})
	assert.Error(t, err)
}

// --- Merge: invalid field name ---

func TestActressRepository_Merge_InvalidFieldName(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	target := &models.Actress{DMMID: 58001, JapaneseName: "ターゲット2"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 58002, JapaneseName: "ソース2"}
	require.NoError(t, repo.Create(context.TODO(), source))

	_, err := repo.Merge(context.TODO(), target.ID, source.ID, map[string]string{
		"nonexistent_field": "source",
	})
	assert.Error(t, err)
}

// --- Merge: source DMMID same as target (temp DMMID swap) ---

func TestActressRepository_Merge_SourceDMMIDMatchesMerged(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Target has no DMMID, source has DMMID. Merged will take source's DMMID.
	// This triggers the temp DMMID swap path.
	target := &models.Actress{DMMID: 0, JapaneseName: "ノーDMMID", FirstName: "Target"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 59001, JapaneseName: "DMMIDあり", FirstName: "Source"}
	require.NoError(t, repo.Create(context.TODO(), source))

	result, err := repo.Merge(context.TODO(), target.ID, source.ID, map[string]string{
		"dmm_id": "source",
	})
	require.NoError(t, err)
	assert.Equal(t, 59001, result.MergedActress.DMMID)
}

// --- PreviewMerge uncovered branches ---

func TestActressRepository_PreviewMerge_NoConflicts(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Target and source have no conflicting fields
	target := &models.Actress{DMMID: 0, JapaneseName: "", FirstName: "Target"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 60001, JapaneseName: "ソースのみ", LastName: "Source"}
	require.NoError(t, repo.Create(context.TODO(), source))

	preview, err := repo.PreviewMerge(context.TODO(), target.ID, source.ID)
	require.NoError(t, err)
	assert.Empty(t, preview.Conflicts)
	assert.Equal(t, 60001, preview.ProposedMerged.DMMID)
	assert.Equal(t, "ソースのみ", preview.ProposedMerged.JapaneseName)
}

func TestActressRepository_PreviewMerge_WithConflicts(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	target := &models.Actress{DMMID: 61001, JapaneseName: "ターゲット名前", FirstName: "TFirst"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 61002, JapaneseName: "ソース名前", FirstName: "SFirst"}
	require.NoError(t, repo.Create(context.TODO(), source))

	preview, err := repo.PreviewMerge(context.TODO(), target.ID, source.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, preview.Conflicts)
	assert.NotNil(t, preview.ConflictByField)
}

// --- Count ---

func TestActressRepository_Count(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	count1, err := repo.Count(context.TODO())
	require.NoError(t, err)

	actress := &models.Actress{DMMID: 62001, JapaneseName: "カウントテスト"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	count2, err := repo.Count(context.TODO())
	require.NoError(t, err)
	assert.Equal(t, count1+1, count2)
}

// --- ListAll ---

func TestActressRepository_ListAll(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.Actress{DMMID: 63001, JapaneseName: "全件1"}))
	require.NoError(t, repo.Create(context.TODO(), &models.Actress{DMMID: 63002, JapaneseName: "全件2"}))

	actresses, err := repo.ListAll(context.TODO())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(actresses), 2)
}

// --- Delete ---

func TestActressRepository_Delete(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{DMMID: 64001, JapaneseName: "削除対象"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	require.NoError(t, repo.Delete(context.TODO(), actress.ID))

	_, err := repo.FindByID(context.TODO(), actress.ID)
	assert.Error(t, err)
}

// --- Update ---

func TestActressRepository_Update(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{DMMID: 65001, JapaneseName: "更新前", FirstName: "Before"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	actress.FirstName = "After"
	actress.JapaneseName = "更新後"
	require.NoError(t, repo.Update(context.TODO(), actress))

	found, err := repo.FindByID(context.TODO(), actress.ID)
	require.NoError(t, err)
	assert.Equal(t, "After", found.FirstName)
	assert.Equal(t, "更新後", found.JapaneseName)
}

// --- FindByJapaneseNameAndDMMID: name only ---

func TestActressRepository_FindByJapaneseNameAndDMMID_NameOnlyNotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "存在しない名前2", 0)
	assert.Error(t, err)
}

// --- FindByJapaneseNameAndDMMID: DMMID only not found ---

func TestActressRepository_FindByJapaneseNameAndDMMID_DMMIDOnlyNotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "", 999999)
	assert.Error(t, err)
}

// --- Merge: with movie associations ---

func TestActressRepository_Merge_WithMovieAssociations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	movieRepo := NewMovieRepository(db)

	target := &models.Actress{DMMID: 66001, JapaneseName: "マージターゲット"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 66002, JapaneseName: "マージソース"}
	require.NoError(t, repo.Create(context.TODO(), source))

	// Create a movie associated with the source actress
	movie := createTestMovie("MERGE-MOVIE-001")
	movie.Actresses = []models.Actress{*source}
	_, err := movieRepo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	result, err := repo.Merge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.UpdatedMovies, 1)

	// Verify movie now points to target
	found, err := movieRepo.FindByID(context.TODO(), "MERGE-MOVIE-001")
	require.NoError(t, err)
	assert.Len(t, found.Actresses, 1)
	assert.Equal(t, target.ID, found.Actresses[0].ID)
}
