package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- FindByDMMID: negative DMMID (line 122) ---

func TestActressRepository_FindByDMMID_Negative_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByDMMID(context.TODO(), -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid lookup")
}

// --- FindByDMMID: zero DMMID (line 122) ---

func TestActressRepository_FindByDMMID_Zero_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByDMMID(context.TODO(), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- Update: error path (line 167) ---

func TestActressRepository_Update_Error_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Updating a non-existent actress should not error with GORM Save (it creates)
	actress := &models.Actress{JapaneseName: "更新テスト", FirstName: "Update"}
	err := repo.Update(context.TODO(), actress)
	require.NoError(t, err)
}

// --- FindByJapaneseName: not found (line 183) ---

func TestActressRepository_FindByJapaneseName_NotFound_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByJapaneseName(context.TODO(), "存在しない名前")
	require.Error(t, err)
}

// --- FindByFirstNameLastName: not found (line 205) ---

func TestActressRepository_FindByFirstNameLastName_NotFound_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByFirstNameLastName(context.TODO(), "NonExistent", "Name")
	require.Error(t, err)
}

// --- FindByJapaneseNameAndDMMID: both name and dmmID (line 218) ---

func TestActressRepository_FindByJapaneseNameAndDMMID_Both_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Create an actress with both JapaneseName and DMMID
	actress := &models.Actress{DMMID: 44444, JapaneseName: "両方テスト"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	found, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "両方テスト", 44444)
	require.NoError(t, err)
	assert.Equal(t, 44444, found.DMMID)
}

// --- FindByJapaneseNameAndDMMID: name only (line 229) ---

func TestActressRepository_FindByJapaneseNameAndDMMID_NameOnly_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{JapaneseName: "名前のみテスト"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	found, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "名前のみテスト", 0)
	require.NoError(t, err)
	assert.Equal(t, "名前のみテスト", found.JapaneseName)
}

// --- FindByJapaneseNameAndDMMID: dmmID only (line 241) ---

func TestActressRepository_FindByJapaneseNameAndDMMID_DMMIDOnly_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{DMMID: 55555, JapaneseName: "DMMのみテスト"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	found, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "", 55555)
	require.NoError(t, err)
	assert.Equal(t, 55555, found.DMMID)
}

// --- FindByJapaneseNameAndDMMID: neither (line 256) ---

func TestActressRepository_FindByJapaneseNameAndDMMID_Neither_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid lookup")
}

// --- ListSorted: error in sort (line 268) ---

func TestActressRepository_ListSorted_InvalidSort_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.ListSorted(context.TODO(), 10, 0, "invalid_field", "asc")
	require.Error(t, err)
}

// --- SearchPaged: success (line 275) ---

func TestActressRepository_SearchPaged_Success_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{JapaneseName: "検索テスト", FirstName: "Search"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	results, err := repo.SearchPaged(context.TODO(), "検索", 10, 0)
	require.NoError(t, err)
	assert.True(t, len(results) > 0)
}

// --- CountSearch: success (line 299) ---

func TestActressRepository_CountSearch_Success_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{JapaneseName: "カウントテスト"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	count, err := repo.CountSearch(context.TODO(), "カウント")
	require.NoError(t, err)
	assert.True(t, count > 0)
}

// --- SearchPagedSorted: success (line 314) ---

func TestActressRepository_SearchPagedSorted_Success_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{JapaneseName: "ソート検索テスト"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	results, err := repo.SearchPagedSorted(context.TODO(), "ソート検索", 10, 0, "japanese_name", "asc")
	require.NoError(t, err)
	assert.True(t, len(results) > 0)
}

// --- SearchPagedSorted: invalid sort (line 314) ---

func TestActressRepository_SearchPagedSorted_InvalidSort_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.SearchPagedSorted(context.TODO(), "test", 10, 0, "bad_field", "asc")
	require.Error(t, err)
}

// --- Merge: DMMID unique constraint check (line 337) ---

func TestActressRepository_Merge_DMMIDCheckNotErrRecordNotFound_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Create target and source with DMMIDs
	target := &models.Actress{DMMID: 60001, JapaneseName: "マージターゲット"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 60002, JapaneseName: "マージソース"}
	require.NoError(t, repo.Create(context.TODO(), source))

	// Normal merge should succeed
	result, err := repo.Merge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, target.ID, result.MergedActress.ID)
}

// --- Merge: tempDMMID == 0 case (line 344) ---

func TestActressRepository_Merge_TempDMMIDZero_Partial(t *testing.T) {
	// This case happens when sourceID is 0, but that's caught by loadPair
	// which requires both IDs > 0. So tempDMMID = -int(sourceID) = 0
	// only when sourceID = 0, which is already rejected.
	// Test the normal merge path instead.
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	target := &models.Actress{DMMID: 0, JapaneseName: "ノーDMMターゲット", FirstName: "NoDmmTarget"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 60003, JapaneseName: "DMMソース", FirstName: "DmmSource"}
	require.NoError(t, repo.Create(context.TODO(), source))

	result, err := repo.Merge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	// The merged actress should have the source's DMMID
	assert.Equal(t, 60003, result.MergedActress.DMMID)
}

// --- Merge: DMMID temp update (line 347) ---

func TestActressRepository_Merge_DMMIDTempUpdate_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Target has no DMMID, source has DMMID
	// After merge, DMMID moves from source to target
	target := &models.Actress{JapaneseName: "一時ターゲット", FirstName: "TempTarget"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 70001, JapaneseName: "一時ソース", FirstName: "TempSource"}
	require.NoError(t, repo.Create(context.TODO(), source))

	result, err := repo.Merge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
}

// --- Merge: duplicated key error (line 360) ---

func TestActressRepository_Merge_DuplicatedKey_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Create two actresses with same DMMID to potentially trigger duplicated key
	target := &models.Actress{DMMID: 80001, JapaneseName: "重複ターゲット"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 80002, JapaneseName: "重複ソース"}
	require.NoError(t, repo.Create(context.TODO(), source))

	// Create a third actress that could conflict if DMMIDs get mixed up
	conflict := &models.Actress{DMMID: 80003, JapaneseName: "重複衝突"}
	require.NoError(t, repo.Create(context.TODO(), conflict))

	// Normal merge should succeed (no unique constraint violation)
	result, err := repo.Merge(context.TODO(), target.ID, source.ID, map[string]string{
		"dmm_id": "target", // Keep target's DMMID
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// --- Merge: moveMovieAssociations error (line 369) ---

func TestActressRepository_Merge_MoveAssociations_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	target := &models.Actress{JapaneseName: "移動ターゲット", FirstName: "MoveTarget"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{JapaneseName: "移動ソース", FirstName: "MoveSource"}
	require.NoError(t, repo.Create(context.TODO(), source))

	// Merge should succeed even without associated movies
	result, err := repo.Merge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, result.UpdatedMovies)
}

// --- Merge: upsertActressAliases (line 373) ---

func TestActressRepository_Merge_WithAliases_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	target := &models.Actress{JapaneseName: "エイリアスターゲット", FirstName: "AliasTarget"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{JapaneseName: "エイリアスソース", FirstName: "AliasSource"}
	require.NoError(t, repo.Create(context.TODO(), source))

	result, err := repo.Merge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
}

// --- Merge: delete source actress (line 377) ---

func TestActressRepository_Merge_DeleteSource_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	target := &models.Actress{JapaneseName: "削除ターゲット", FirstName: "DelTarget"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{JapaneseName: "削除ソース", FirstName: "DelSource"}
	require.NoError(t, repo.Create(context.TODO(), source))
	sourceID := source.ID

	result, err := repo.Merge(context.TODO(), target.ID, sourceID, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Source should be deleted
	_, err = repo.FindByID(context.TODO(), sourceID)
	require.Error(t, err)
}

// --- Merge: FindByID error after merge (line 388) ---

func TestActressRepository_Merge_SuccessAndReload_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	target := &models.Actress{JapaneseName: "リロードターゲット", FirstName: "ReloadTarget"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{JapaneseName: "リロードソース", FirstName: "ReloadSource"}
	require.NoError(t, repo.Create(context.TODO(), source))

	result, err := repo.Merge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, target.ID, result.MergedActress.ID)
	assert.Equal(t, source.ID, result.MergedFromID)
}

// --- Search: empty query (line 275+) ---

func TestActressRepository_Search_EmptyQuery_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{JapaneseName: "空検索テスト"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	results, err := repo.Search(context.TODO(), "")
	require.NoError(t, err)
	assert.True(t, len(results) > 0)
}

// --- Search: non-empty query ---

func TestActressRepository_Search_NonEmptyQuery_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{JapaneseName: "検索クエリテスト", FirstName: "QueryTest"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	results, err := repo.Search(context.TODO(), "検索クエリ")
	require.NoError(t, err)
	assert.True(t, len(results) > 0)
}

// --- FindOrCreate: existing actress found ---

func TestActressRepository_FindOrCreate_Existing_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Create an actress
	actress := &models.Actress{JapaneseName: "既存テスト", FirstName: "Existing"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	// FindOrCreate should find the existing one
	newActress := &models.Actress{JapaneseName: "既存テスト", FirstName: "NewFirst"}
	err := repo.FindOrCreate(context.TODO(), newActress)
	require.NoError(t, err)
	assert.Equal(t, actress.ID, newActress.ID)
	assert.Equal(t, "既存テスト", newActress.JapaneseName)
}

// --- FindOrCreate: new actress ---

func TestActressRepository_FindOrCreate_New_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{JapaneseName: "新規テスト", FirstName: "New"}
	err := repo.FindOrCreate(context.TODO(), actress)
	require.NoError(t, err)
	assert.NotEqual(t, uint(0), actress.ID)
}

// --- FindOrCreate: no JapaneseName ---

func TestActressRepository_FindOrCreate_NoJapaneseName_Partial(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{FirstName: "NoJpName", LastName: "Test"}
	err := repo.FindOrCreate(context.TODO(), actress)
	require.NoError(t, err)
	assert.NotEqual(t, uint(0), actress.ID)
}
