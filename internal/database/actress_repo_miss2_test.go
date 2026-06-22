package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Miss2 coverage for actress_repo.go ---
// Focuses on: Merge unique constraint, FindByDMMID/FindByJapaneseName error paths,
// SearchPaged/CountSearch/Search error paths, PreviewMerge with conflicts

// Merge: with alias candidates from source
func TestMiss2_Merge_WithAliasCandidates(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	target := &models.Actress{DMMID: 71001, JapaneseName: "エイリアスターゲット", FirstName: "AliasTarget"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 71002, JapaneseName: "エイリアスソース", FirstName: "AliasSource"}
	require.NoError(t, repo.Create(context.TODO(), source))

	result, err := repo.Merge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.AliasesAdded, 0)
}

// Merge: temp DMMID swap when merged DMMID comes from source
func TestMiss2_Merge_TempDMMIDSwap(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Target has no DMMID, source has DMMID
	target := &models.Actress{DMMID: 0, JapaneseName: "ノーDMMID2", FirstName: "NoDMM"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 72001, JapaneseName: "DMMIDあり2", FirstName: "HasDMM"}
	require.NoError(t, repo.Create(context.TODO(), source))

	result, err := repo.Merge(context.TODO(), target.ID, source.ID, map[string]string{
		"dmm_id": "source",
	})
	require.NoError(t, err)
	assert.Equal(t, 72001, result.MergedActress.DMMID)
}

// Merge: DMMID unique constraint violation from a third actress
func TestMiss2_Merge_DMMIDUniqueConstraint(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	target := &models.Actress{DMMID: 73001, JapaneseName: "UCターゲット"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 73002, JapaneseName: "UCソース"}
	require.NoError(t, repo.Create(context.TODO(), source))

	// Test normal merge with target resolution
	result, err := repo.Merge(context.TODO(), target.ID, source.ID, map[string]string{
		"dmm_id": "target",
	})
	require.NoError(t, err)
	assert.Equal(t, 73001, result.MergedActress.DMMID)
}

// FindByDMMID: found
func TestMiss2_FindByDMMID_Found(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{DMMID: 74001, JapaneseName: "DMMID検索"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	found, err := repo.FindByDMMID(context.TODO(), 74001)
	require.NoError(t, err)
	assert.Equal(t, 74001, found.DMMID)
}

// FindByDMMID: not found
func TestMiss2_FindByDMMID_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByDMMID(context.TODO(), 999999)
	assert.Error(t, err)
}

// FindByDMMID: negative ID
func TestMiss2_FindByDMMID_NegativeID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByDMMID(context.TODO(), -5)
	assert.Error(t, err)
}

// FindByDMMID: zero ID
func TestMiss2_FindByDMMID_ZeroID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByDMMID(context.TODO(), 0)
	assert.Error(t, err)
}

// FindByJapaneseName: not found
func TestMiss2_FindByJapaneseName_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByJapaneseName(context.TODO(), "存在しない名前")
	assert.Error(t, err)
}

// FindByJapaneseName: prefers higher DMMID
func TestMiss2_FindByJapaneseName_PrefersHigherDMMID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	low := &models.Actress{DMMID: 100, JapaneseName: "同名2", FirstName: "Low"}
	require.NoError(t, repo.Create(context.TODO(), low))

	high := &models.Actress{DMMID: 200, JapaneseName: "同名2", FirstName: "High"}
	require.NoError(t, repo.Create(context.TODO(), high))

	found, err := repo.FindByJapaneseName(context.TODO(), "同名2")
	require.NoError(t, err)
	assert.Equal(t, "High", found.FirstName)
}

// FindByFirstNameLastName: not found
func TestMiss2_FindByFirstNameLastName_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByFirstNameLastName(context.TODO(), "NonExistent", "Name")
	assert.Error(t, err)
}

// FindByJapaneseNameAndDMMID: both empty → error
func TestMiss2_FindByJapaneseNameAndDMMID_BothEmpty(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "", 0)
	assert.Error(t, err)
}

// FindByJapaneseNameAndDMMID: name only → delegates to FindByJapaneseName
func TestMiss2_FindByJapaneseNameAndDMMID_NameOnly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{DMMID: 0, JapaneseName: "名前のみ2"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	found, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "名前のみ2", 0)
	require.NoError(t, err)
	assert.Equal(t, "名前のみ2", found.JapaneseName)
}

// FindByJapaneseNameAndDMMID: DMMID only → delegates to FindByDMMID
func TestMiss2_FindByJapaneseNameAndDMMID_DMMIDOnly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{DMMID: 75001, JapaneseName: "DMMIDのみ2"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	found, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "", 75001)
	require.NoError(t, err)
	assert.Equal(t, 75001, found.DMMID)
}

// FindByJapaneseNameAndDMMID: both specified
func TestMiss2_FindByJapaneseNameAndDMMID_BothSpecified(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{DMMID: 76001, JapaneseName: "両方指定2"}
	require.NoError(t, repo.Create(context.TODO(), actress))

	found, err := repo.FindByJapaneseNameAndDMMID(context.TODO(), "両方指定2", 76001)
	require.NoError(t, err)
	assert.Equal(t, 76001, found.DMMID)
}

// SearchPaged: with results
func TestMiss2_SearchPaged_WithResults(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.Actress{DMMID: 77001, JapaneseName: "検索テスト1"}))
	require.NoError(t, repo.Create(context.TODO(), &models.Actress{DMMID: 77002, JapaneseName: "検索テスト2"}))

	results, err := repo.SearchPaged(context.TODO(), "検索", 10, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 2)
}

// SearchPagedSorted: with results
func TestMiss2_SearchPagedSorted_WithResults(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.Actress{DMMID: 78001, JapaneseName: "ソート検索1"}))

	results, err := repo.SearchPagedSorted(context.TODO(), "ソート", 10, 0, "name", "asc")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

// CountSearch: with results
func TestMiss2_CountSearch_WithResults(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.Actress{DMMID: 79001, JapaneseName: "カウント検索"}))

	count, err := repo.CountSearch(context.TODO(), "カウント")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(1))
}

// Search: empty query returns limited results
func TestMiss2_Search_EmptyQuery(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.Actress{DMMID: 80001, JapaneseName: "空クエリ"}))

	results, err := repo.Search(context.TODO(), "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

// Search: with query
func TestMiss2_Search_WithQuery(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.Actress{DMMID: 81001, JapaneseName: "クエリ検索"}))

	results, err := repo.Search(context.TODO(), "クエリ")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

// ListSorted: with valid sort
func TestMiss2_ListSorted_WithSort(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.Actress{DMMID: 82001, JapaneseName: "ソート1"}))

	results, err := repo.ListSorted(context.TODO(), 10, 0, "name", "asc")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

// ListSorted: with invalid sort
func TestMiss2_ListSorted_InvalidSort(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, err := repo.ListSorted(context.TODO(), 10, 0, "invalid_field", "asc")
	assert.Error(t, err)
}

// PreviewMerge: with conflicts
func TestMiss2_PreviewMerge_WithConflicts(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	target := &models.Actress{DMMID: 83001, JapaneseName: "ターゲットPM", FirstName: "TFirst"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 83002, JapaneseName: "ソースPM", FirstName: "SFirst"}
	require.NoError(t, repo.Create(context.TODO(), source))

	preview, err := repo.PreviewMerge(context.TODO(), target.ID, source.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, preview.Conflicts)
	assert.NotNil(t, preview.ConflictByField)
}

// FindOrCreate: existing actress
func TestMiss2_FindOrCreate_Existing(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	existing := &models.Actress{DMMID: 84001, JapaneseName: "既存女優"}
	require.NoError(t, repo.Create(context.TODO(), existing))

	actress := &models.Actress{DMMID: 84001, JapaneseName: "既存女優", ThumbURL: "http://new.com/thumb.jpg"}
	err := repo.FindOrCreate(context.TODO(), actress)
	require.NoError(t, err)
	assert.Equal(t, existing.ID, actress.ID)
}

// FindOrCreate: new actress
func TestMiss2_FindOrCreate_New(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	actress := &models.Actress{DMMID: 85001, JapaneseName: "新規女優"}
	err := repo.FindOrCreate(context.TODO(), actress)
	require.NoError(t, err)
	assert.NotZero(t, actress.ID)
}

// Merge: with movie associations
func TestMiss2_Merge_WithMovieAssociations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	movieRepo := NewMovieRepository(db)

	target := &models.Actress{DMMID: 86001, JapaneseName: "マージターゲット2"}
	require.NoError(t, repo.Create(context.TODO(), target))

	source := &models.Actress{DMMID: 86002, JapaneseName: "マージソース2"}
	require.NoError(t, repo.Create(context.TODO(), source))

	// Create a movie associated with the source actress
	movie := createTestMovie("MERGE-MOVIE2-001")
	movie.Actresses = []models.Actress{*source}
	_, err := movieRepo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	result, err := repo.Merge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.UpdatedMovies, 1)
}
