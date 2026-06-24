package nfo

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Partial line coverage for mergeStringField ---

// TestMergeStringField_BothEmpty covers both-empty branch
func TestMergeStringField_BothEmpty_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Director: ""}
	nfo := &models.Movie{ID: "T1", Director: ""}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, "", result.Merged.Director)
}

// TestMergeStringField_ScraperEmpty_NonCritical covers scraped empty for non-critical field
func TestMergeStringField_ScraperEmpty_NonCritical_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Director: ""}
	nfo := &models.Movie{ID: "T1", Director: "Director A"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, "Director A", result.Merged.Director)
}

// TestMergeStringField_ScraperEmpty_PreferScraper_StrictMode covers PreferScraper strict mode with empty scraper
func TestMergeStringField_ScraperEmpty_PreferScraper_StrictMode_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Director: ""}
	nfo := &models.Movie{ID: "T1", Director: "Director A"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	require.NoError(t, err)
	// PreferScraper strict: uses empty scraper value
	assert.Equal(t, "", result.Merged.Director)
}

// TestMergeStringField_NFOEmpty_PreferNFO_StrictMode covers PreferNFO strict mode with empty NFO
func TestMergeStringField_NFOEmpty_PreferNFO_StrictMode_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Director: "Director A"}
	nfo := &models.Movie{ID: "T1", Director: ""}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	// PreferNFO strict: uses empty NFO value
	assert.Equal(t, "", result.Merged.Director)
}

// TestMergeStringField_BothHaveData_PreferScraper covers PreferScraper conflict resolution
func TestMergeStringField_BothHaveData_PreferScraper_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Director: "Scraped Dir"}
	nfo := &models.Movie{ID: "T1", Director: "NFO Dir"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	require.NoError(t, err)
	assert.Equal(t, "Scraped Dir", result.Merged.Director)
}

// TestMergeStringField_BothHaveData_PreferNFO covers PreferNFO conflict resolution
func TestMergeStringField_BothHaveData_PreferNFO_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Director: "Scraped Dir"}
	nfo := &models.Movie{ID: "T1", Director: "NFO Dir"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, "NFO Dir", result.Merged.Director)
}

// TestMergeStringField_BothHaveData_PreserveExisting covers PreserveExisting strategy
func TestMergeStringField_BothHaveData_PreserveExisting_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Director: "Scraped Dir"}
	nfo := &models.Movie{ID: "T1", Director: "NFO Dir"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreserveExisting, true)
	require.NoError(t, err)
	assert.Equal(t, "NFO Dir", result.Merged.Director)
}

// TestMergeStringField_BothHaveData_FillMissingOnly covers FillMissingOnly strategy
func TestMergeStringField_BothHaveData_FillMissingOnly_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Director: "Scraped Dir"}
	nfo := &models.Movie{ID: "T1", Director: "NFO Dir"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, FillMissingOnly, true)
	require.NoError(t, err)
	assert.Equal(t, "NFO Dir", result.Merged.Director)
}

// TestMergeStringField_BothHaveData_MergeArrays covers MergeArrays strategy for strings
func TestMergeStringField_BothHaveData_MergeArrays_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Director: "Scraped Dir"}
	nfo := &models.Movie{ID: "T1", Director: "NFO Dir"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, MergeArrays, true)
	require.NoError(t, err)
	assert.Equal(t, "Scraped Dir", result.Merged.Director)
}

// TestMergeStringField_CriticalField_ScraperEmpty_NonPreferNFO covers critical field fallback with non-PreferNFO strategy
func TestMergeStringField_CriticalField_ScraperEmpty_NonPreferNFO_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "", Title: ""}
	nfo := &models.Movie{ID: "", Title: "NFO Title"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	require.NoError(t, err)
	// Title is critical: scraper empty, uses NFO value
	assert.Equal(t, "NFO Title", result.Merged.Title)
}

// --- Partial line coverage for mergeScalarField ---

// TestMergeScalarField_BothEmpty covers both empty branch
func TestMergeScalarField_BothEmpty_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Runtime: 0}
	nfo := &models.Movie{ID: "T1", Runtime: 0}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Merged.Runtime)
}

// TestMergeScalarField_ScraperEmpty covers scraped empty branch
func TestMergeScalarField_ScraperEmpty_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Runtime: 0}
	nfo := &models.Movie{ID: "T1", Runtime: 120}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, 120, result.Merged.Runtime)
}

// TestMergeScalarField_NFOEmpty covers NFO empty branch
func TestMergeScalarField_NFOEmpty_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Runtime: 90}
	nfo := &models.Movie{ID: "T1", Runtime: 0}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, 90, result.Merged.Runtime)
}

// TestMergeScalarField_BothHaveData_PreferNFO covers PreferNFO conflict
func TestMergeScalarField_BothHaveData_PreferNFO_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Runtime: 90}
	nfo := &models.Movie{ID: "T1", Runtime: 120}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, 120, result.Merged.Runtime)
}

// TestMergeScalarField_BothHaveData_PreferScraper covers PreferScraper conflict
func TestMergeScalarField_BothHaveData_PreferScraper_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Runtime: 90}
	nfo := &models.Movie{ID: "T1", Runtime: 120}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	require.NoError(t, err)
	assert.Equal(t, 90, result.Merged.Runtime)
}

// TestMergeScalarField_BothHaveData_MergeArrays covers MergeArrays conflict
func TestMergeScalarField_BothHaveData_MergeArrays_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Runtime: 90}
	nfo := &models.Movie{ID: "T1", Runtime: 120}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, MergeArrays, true)
	require.NoError(t, err)
	assert.Equal(t, 90, result.Merged.Runtime)
}

// TestMergeScalarField_BothHaveData_DefaultStrategy covers unknown/default strategy
func TestMergeScalarField_BothHaveData_DefaultStrategy_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Runtime: 90}
	nfo := &models.Movie{ID: "T1", Runtime: 120}

	// Use a strategy that isn't one of the named ones - but MergeStrategy is a string type
	// so we can't easily create an "unknown" one. Instead, test PreserveExisting
	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreserveExisting, true)
	require.NoError(t, err)
	assert.Equal(t, 120, result.Merged.Runtime)
}

// TestMergeScalarField_BothHaveData_FillMissingOnly covers FillMissingOnly
func TestMergeScalarField_BothHaveData_FillMissingOnly_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Runtime: 90}
	nfo := &models.Movie{ID: "T1", Runtime: 120}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, FillMissingOnly, true)
	require.NoError(t, err)
	assert.Equal(t, 120, result.Merged.Runtime)
}

// --- Partial line coverage for mergeActresses ---

// TestMergeActresses_BothEmpty covers both empty
func TestMergeActresses_BothEmpty_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Actresses: nil}
	nfo := &models.Movie{ID: "T1", Actresses: nil}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Nil(t, result.Merged.Actresses)
}

// TestMergeActresses_ScraperEmpty covers scraped empty
func TestMergeActresses_ScraperEmpty_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Actresses: nil}
	nfo := &models.Movie{ID: "T1", Actresses: []models.Actress{
		{FirstName: "NFO", LastName: "Actress"},
	}}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	require.Len(t, result.Merged.Actresses, 1)
	assert.Equal(t, "NFO", result.Merged.Actresses[0].FirstName)
}

// TestMergeActresses_NFOEmpty covers NFO empty
func TestMergeActresses_NFOEmpty_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Actresses: []models.Actress{
		{FirstName: "Scraper", LastName: "Actress"},
	}}
	nfo := &models.Movie{ID: "T1", Actresses: nil}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	require.Len(t, result.Merged.Actresses, 1)
	assert.Equal(t, "Scraper", result.Merged.Actresses[0].FirstName)
}

// TestMergeActresses_BothHaveData_PreferScraper covers PreferScraper for actresses
func TestMergeActresses_BothHaveData_PreferScraper_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Title: "T", Actresses: []models.Actress{
		{FirstName: "Scraper", LastName: "A"},
	}}
	nfo := &models.Movie{ID: "T1", Title: "T", Actresses: []models.Actress{
		{FirstName: "NFO", LastName: "B"},
	}}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	require.NoError(t, err)
	// PreferScraper: use scraped actresses
	assert.GreaterOrEqual(t, len(result.Merged.Actresses), 1)
	assert.Equal(t, "Scraper", result.Merged.Actresses[0].FirstName)
}

// TestMergeActresses_BothHaveData_MergeArrays covers MergeArrays strategy for actresses
func TestMergeActresses_BothHaveData_MergeArrays_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Actresses: []models.Actress{
		{FirstName: "Scraper", LastName: "A", DMMID: 10},
	}}
	nfo := &models.Movie{ID: "T1", Actresses: []models.Actress{
		{FirstName: "NFO", LastName: "B"},
	}}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, MergeArrays, true)
	require.NoError(t, err)
	// Both actresses should be present when mergeArrays=true
	assert.GreaterOrEqual(t, len(result.Merged.Actresses), 2)
}

// TestMergeActresses_BothHaveData_PreferNFO covers PreferNFO strategy for actresses
func TestMergeActresses_BothHaveData_PreferNFO_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Actresses: []models.Actress{
		{FirstName: "Scraper", LastName: "A", DMMID: 10},
	}}
	nfo := &models.Movie{ID: "T1", Actresses: []models.Actress{
		{FirstName: "NFO", LastName: "B"},
	}}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Merged.Actresses), 1)
}

// --- Partial line coverage for mergeActressSlices ---

// TestMergeActressSlices_PreferNFO_JapaneseNameMerge covers preferNFO JapaneseName merge
func TestMergeActressSlices_PreferNFO_JapaneseNameMerge_Partial(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", DMMID: 10, FirstName: "", LastName: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", FirstName: "NFirst", LastName: "NLast"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	require.Len(t, result, 1)
	assert.Equal(t, "NFirst", result[0].FirstName)
	assert.Equal(t, "NLast", result[0].LastName)
	assert.Equal(t, 10, result[0].DMMID)
	// NFO JapaneseName should overwrite since preferNFO=true and it's not empty
	assert.Equal(t, "テスト", result[0].JapaneseName)
}

// TestMergeActressSlices_PreferScraper_JapaneseNameMerge covers preferScraper JapaneseName merge
func TestMergeActressSlices_PreferScraper_JapaneseNameMerge_Partial(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", DMMID: 10, FirstName: "SFirst", LastName: "SLast"},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", FirstName: "NFirst", LastName: "NLast"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	// Scraper values should be kept since they're not empty
	assert.Equal(t, "SFirst", result[0].FirstName)
	assert.Equal(t, "SLast", result[0].LastName)
	assert.Equal(t, 10, result[0].DMMID)
}

// TestMergeActressSlices_ScraperEmptyFirstName_NFillsIn covers !preferNFO with empty scraper field
func TestMergeActressSlices_ScraperEmptyFirstName_NFillsIn_Partial(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", DMMID: 10, FirstName: "", LastName: "SLast"},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", FirstName: "NFirst", LastName: "NLast"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	// Scraper FirstName is empty, so NFO fills in
	assert.Equal(t, "NFirst", result[0].FirstName)
	// Scraper LastName is non-empty, so keeps scraper value
	assert.Equal(t, "SLast", result[0].LastName)
}

// TestMergeActressSlices_ScraperEmptyJapaneseName_NFillsIn covers !preferNFO with empty scraper JapaneseName
// The actresses must have matching names for mergeActressSlices to pair them.
func TestMergeActressSlices_ScraperEmptyJapaneseName_NFillsIn_Partial(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "", DMMID: 10, FirstName: "SFirst", LastName: "SLast"},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", FirstName: "SFirst", LastName: "SLast"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	// Scraper JapaneseName is empty, so NFO fills in
	assert.Equal(t, "テスト", result[0].JapaneseName)
}

// TestMergeActressSlices_ScraperEmptyThumbURL_NFillsIn covers ThumbURL fill from NFO
func TestMergeActressSlices_ScraperEmptyThumbURL_NFillsIn_Partial(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", DMMID: 10, ThumbURL: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", ThumbURL: "http://thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	assert.Equal(t, "http://thumb.jpg", result[0].ThumbURL)
}

// TestMergeActressSlices_ReverseMatch_PreferNFOJapaneseName covers reverse match with preferNFO
// Scraped and NFO must share a common identifier for matching.
func TestMergeActressSlices_ReverseMatch_PreferNFOJapaneseName_Partial(t *testing.T) {
	// Both have JapaneseName for matching
	scraped := []models.Actress{
		{JapaneseName: "波多野結衣", DMMID: 50, FirstName: "Hatano", LastName: "Yui"},
	}
	nfo := []models.Actress{
		{JapaneseName: "波多野結衣", FirstName: "NFirst", LastName: "NLast"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	require.Len(t, result, 1)
	// preferNFO=true: NFO names should overwrite
	assert.Equal(t, "NFirst", result[0].FirstName)
	assert.Equal(t, "NLast", result[0].LastName)
	assert.Equal(t, 50, result[0].DMMID)
}

// TestMergeActressSlices_ReverseMatch_PreferScraperEmptyNames covers reverse match with empty scraper names
// Both actresses must share a common identifier for matching.
func TestMergeActressSlices_ReverseMatch_PreferScraperEmptyNames_Partial(t *testing.T) {
	// Both have JapaneseName for matching
	scraped := []models.Actress{
		{JapaneseName: "波多野結衣", DMMID: 50, FirstName: "", LastName: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "波多野結衣", FirstName: "Hatano", LastName: "Yui"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	// preferNFO=false: scraper FirstName empty, NFO fills in
	assert.Equal(t, "Hatano", result[0].FirstName)
	assert.Equal(t, "Yui", result[0].LastName)
	assert.Equal(t, 50, result[0].DMMID)
}

// TestMergeActressSlices_UnmatchedActresses_BothSides covers actresses that don't match
func TestMergeActressSlices_UnmatchedActresses_BothSides_Partial(t *testing.T) {
	scraped := []models.Actress{
		{FirstName: "Unique", LastName: "Scraper"},
	}
	nfo := []models.Actress{
		{FirstName: "Unique", LastName: "NFO"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	// Both are unique, should have 2 entries
	assert.Len(t, result, 2)
}

// --- Partial line coverage for mergeSlice ---

// TestMergeSlice_BothEmpty covers both empty
func TestMergeSlice_BothEmpty_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Screenshots: nil}
	nfo := &models.Movie{ID: "T1", Screenshots: nil}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Nil(t, result.Merged.Screenshots)
}

// TestMergeSlice_ScraperEmpty covers scraped empty
func TestMergeSlice_ScraperEmpty_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Screenshots: nil}
	nfo := &models.Movie{ID: "T1", Screenshots: []string{"nfo1.jpg"}}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"nfo1.jpg"}, result.Merged.Screenshots)
}

// TestMergeSlice_NFOEmpty covers NFO empty
func TestMergeSlice_NFOEmpty_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Screenshots: []string{"scr1.jpg"}}
	nfo := &models.Movie{ID: "T1", Screenshots: nil}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"scr1.jpg"}, result.Merged.Screenshots)
}

// TestMergeSlice_BothHaveData_MergeArrays covers MergeArrays dedup
func TestMergeSlice_BothHaveData_MergeArrays_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Screenshots: []string{"scr1.jpg", "shared.jpg"}}
	nfo := &models.Movie{ID: "T1", Screenshots: []string{"nfo1.jpg", "shared.jpg"}}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, MergeArrays, true)
	require.NoError(t, err)
	// Should deduplicate "shared.jpg"
	assert.Contains(t, result.Merged.Screenshots, "scr1.jpg")
	assert.Contains(t, result.Merged.Screenshots, "nfo1.jpg")
	// "shared.jpg" should appear once
	count := 0
	for _, s := range result.Merged.Screenshots {
		if s == "shared.jpg" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

// TestMergeSlice_BothHaveData_PreferScraper covers PreferScraper for slices
// When mergeArrays=false, arrays use the scalarStrategy
func TestMergeSlice_BothHaveData_PreferScraper_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Title: "T", Screenshots: []string{"scr1.jpg"}}
	nfo := &models.Movie{ID: "T1", Title: "T", Screenshots: []string{"nfo1.jpg"}}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"scr1.jpg"}, result.Merged.Screenshots)
}

// --- Partial line coverage for mergeGenres ---

// TestMergeGenres_BothHaveData_MergeArrays covers MergeArrays for genres
func TestMergeGenres_BothHaveData_MergeArrays_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Genres: []models.Genre{{Name: "Action"}, {Name: "Drama"}}}
	nfo := &models.Movie{ID: "T1", Genres: []models.Genre{{Name: "Comedy"}, {Name: "drama"}}}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, MergeArrays, true)
	require.NoError(t, err)
	// "drama" and "Drama" should be deduplicated (case-insensitive)
	names := make(map[string]bool)
	for _, g := range result.Merged.Genres {
		names[g.Name] = true
	}
	assert.True(t, names["Action"] || names["action"])
	assert.True(t, names["Comedy"] || names["comedy"])
}

// --- Partial line coverage for MergeMovieMetadataWithOptions ---

// TestMergeMovieMetadataWithOptions_OnlyScraped covers scraped-only merge
func TestMergeMovieMetadataWithOptions_OnlyScraped_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Title: "Scraped Title"}

	result, err := MergeMovieMetadataWithOptions(scraped, nil, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, "T1", result.Merged.ID)
	assert.Equal(t, "Scraped Title", result.Merged.Title)
}

// TestMergeMovieMetadataWithOptions_OnlyNFO covers NFO-only merge
func TestMergeMovieMetadataWithOptions_OnlyNFO_Partial(t *testing.T) {
	nfo := &models.Movie{ID: "T1", Title: "NFO Title"}

	result, err := MergeMovieMetadataWithOptions(nil, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, "T1", result.Merged.ID)
	assert.Equal(t, "NFO Title", result.Merged.Title)
}

// TestMergeMovieMetadataWithOptions_BothNil covers both nil error
func TestMergeMovieMetadataWithOptions_BothNil_Partial(t *testing.T) {
	_, err := MergeMovieMetadataWithOptions(nil, nil, PreferNFO, true)
	assert.Error(t, err)
}

// TestMergeMovieMetadataWithOptions_ScraperTimestamps covers scraped timestamp fallback
func TestMergeMovieMetadataWithOptions_ScraperTimestamps_Partial(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)

	scraped := &models.Movie{ID: "T1", CreatedAt: earlier}
	nfo := &models.Movie{ID: "T1", CreatedAt: now}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	// NFO CreatedAt is later, should be used
	assert.Equal(t, now, result.Merged.CreatedAt)
}

// TestMergeMovieMetadataWithOptions_ScraperNoTimestamps covers scraped with zero timestamps
func TestMergeMovieMetadataWithOptions_ScraperNoTimestamps_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1"}
	nfo := &models.Movie{ID: "T1"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, "T1", result.Merged.ID)
}

// TestMergeMovieMetadataWithOptions_MergeArraysFlagFalse covers mergeArrays=false using scalarStrategy
func TestMergeMovieMetadataWithOptions_MergeArraysFlagFalse_Partial(t *testing.T) {
	scraped := &models.Movie{
		ID:          "T1",
		Actresses:   []models.Actress{{FirstName: "A"}},
		Genres:      []models.Genre{{Name: "Action"}},
		Screenshots: []string{"scr1.jpg"},
	}
	nfo := &models.Movie{
		ID:          "T1",
		Actresses:   []models.Actress{{FirstName: "B"}},
		Genres:      []models.Genre{{Name: "Comedy"}},
		Screenshots: []string{"nfo1.jpg"},
	}

	// mergeArrays=false: arrays use scalarStrategy (PreferScraper)
	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	require.NoError(t, err)
	assert.Equal(t, "A", result.Merged.Actresses[0].FirstName)
	assert.Equal(t, "Action", result.Merged.Genres[0].Name)
	assert.Equal(t, []string{"scr1.jpg"}, result.Merged.Screenshots)
}

// TestMergeMovieMetadataWithOptions_CroppedPosterURL_AlwaysFromScraper covers CroppedPosterURL
func TestMergeMovieMetadataWithOptions_CroppedPosterURL_AlwaysFromScraper_Partial(t *testing.T) {
	scraped := &models.Movie{
		ID: "T1",
		Poster: models.PosterState{
			CroppedPosterURL: "http://cropped.jpg",
		},
	}
	nfo := &models.Movie{
		ID: "T1",
		Poster: models.PosterState{
			CroppedPosterURL: "http://nfo-cropped.jpg",
		},
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, "http://cropped.jpg", result.Merged.Poster.CroppedPosterURL)
}

// TestMergeMovieMetadataWithOptions_ReleaseDateMerge covers ReleaseDate merge
func TestMergeMovieMetadataWithOptions_ReleaseDateMerge_Partial(t *testing.T) {
	now := time.Now()
	scraped := &models.Movie{ID: "T1", ReleaseDate: nil}
	nfo := &models.Movie{ID: "T1", ReleaseDate: &now}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.NotNil(t, result.Merged.ReleaseDate)
}

// TestMergeMovieMetadataWithOptions_RatingMerge covers RatingScore/RatingVotes merge
func TestMergeMovieMetadataWithOptions_RatingMerge_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", RatingScore: 8.5, RatingVotes: 100}
	nfo := &models.Movie{ID: "T1", RatingScore: 0, RatingVotes: 0}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	assert.Equal(t, float64(8.5), result.Merged.RatingScore)
	assert.Equal(t, 100, result.Merged.RatingVotes)
}

// TestMergeMovieMetadataWithOptions_ShouldCropPosterMerge covers bool field merge
func TestMergeMovieMetadataWithOptions_ShouldCropPosterMerge_Partial(t *testing.T) {
	scraped := &models.Movie{ID: "T1", Poster: models.PosterState{ShouldCropPoster: true}}
	nfo := &models.Movie{ID: "T1", Poster: models.PosterState{ShouldCropPoster: false}}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	require.NoError(t, err)
	assert.True(t, result.Merged.Poster.ShouldCropPoster)
}

// --- Partial line coverage for isFieldEmpty and countNonEmptyFields ---

// TestIsFieldEmpty_AllFields covers all field name cases
func TestIsFieldEmpty_AllFields_Partial(t *testing.T) {
	m := &models.Movie{
		ID:        "T1",
		ContentID: "c1",
		Title:     "Title",
		Translations: []models.MovieTranslation{
			{Language: "en", Title: "T"},
		},
	}

	// Non-empty fields
	assert.False(t, isFieldEmptySpec("ID", m))
	assert.False(t, isFieldEmptySpec("ContentID", m))
	assert.False(t, isFieldEmptySpec("Title", m))
	assert.False(t, isFieldEmptySpec("Translations", m))

	// Empty fields
	assert.True(t, isFieldEmptySpec("DisplayTitle", m))
	assert.True(t, isFieldEmptySpec("OriginalTitle", m))
	assert.True(t, isFieldEmptySpec("Description", m))
	assert.True(t, isFieldEmptySpec("ReleaseYear", m))
	assert.True(t, isFieldEmptySpec("Runtime", m))
	assert.True(t, isFieldEmptySpec("Director", m))
	assert.True(t, isFieldEmptySpec("Maker", m))
	assert.True(t, isFieldEmptySpec("Label", m))
	assert.True(t, isFieldEmptySpec("Series", m))
	assert.True(t, isFieldEmptySpec("RatingScore", m))
	assert.True(t, isFieldEmptySpec("RatingVotes", m))
	assert.True(t, isFieldEmptySpec("PosterURL", m))
	assert.True(t, isFieldEmptySpec("CoverURL", m))
	assert.True(t, isFieldEmptySpec("CroppedPosterURL", m))
	assert.True(t, isFieldEmptySpec("ShouldCropPoster", m)) // false (default) → !false == true (empty)
	assert.True(t, isFieldEmptySpec("OriginalPosterURL", m))
	assert.True(t, isFieldEmptySpec("OriginalCroppedPosterURL", m))
	assert.True(t, isFieldEmptySpec("OriginalShouldCropPoster", m))
	assert.True(t, isFieldEmptySpec("TrailerURL", m))
	assert.True(t, isFieldEmptySpec("OriginalFileName", m))
	assert.True(t, isFieldEmptySpec("Actresses", m))
	assert.True(t, isFieldEmptySpec("Genres", m))
	assert.True(t, isFieldEmptySpec("Screenshots", m))
	assert.True(t, isFieldEmptySpec("SourceName", m))
	assert.True(t, isFieldEmptySpec("SourceURL", m))
	assert.True(t, isFieldEmptySpec("UnknownField", m)) // default
}

// TestCountNonEmptyFields_NilMovie covers nil input
func TestCountNonEmptyFields_NilMovie_Partial(t *testing.T) {
	assert.Equal(t, 0, countNonEmptyFields(nil))
}

// TestCountNonEmptyFields_PartiallyFilled covers some fields filled
func TestCountNonEmptyFields_PartiallyFilled_Partial(t *testing.T) {
	m := &models.Movie{ID: "T1", Title: "Title", Runtime: 120}
	count := countNonEmptyFields(m)
	assert.GreaterOrEqual(t, count, 3)
}

// --- Partial line coverage for makeProvenanceMap ---

// TestMakeProvenanceMap_NilMovie covers nil input
func TestMakeProvenanceMap_NilMovie_Partial(t *testing.T) {
	result := makeProvenanceMap(nil, "scraper")
	assert.Empty(t, result)
}

// TestMakeProvenanceMap_WithTimestamps covers timestamp attachment
func TestMakeProvenanceMap_WithTimestamps_Partial(t *testing.T) {
	now := time.Now()
	m := &models.Movie{ID: "T1", Title: "Title", UpdatedAt: now}

	result := makeProvenanceMap(m, "scraper")
	assert.Contains(t, result, "ID")
	assert.Contains(t, result, "Title")
	if ds, ok := result["ID"]; ok {
		assert.Equal(t, "scraper", ds.Source)
		assert.NotNil(t, ds.LastUpdated)
	}
}

// TestMakeProvenanceMap_NoTimestampsUsesCreatedAt covers fallback to CreatedAt
func TestMakeProvenanceMap_NoTimestampsUsesCreatedAt_Partial(t *testing.T) {
	created := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	m := &models.Movie{ID: "T1", Title: "Title", CreatedAt: created}

	result := makeProvenanceMap(m, "nfo")
	assert.Contains(t, result, "ID")
	if ds, ok := result["ID"]; ok {
		assert.Equal(t, "nfo", ds.Source)
		assert.NotNil(t, ds.LastUpdated)
	}
}

// --- Partial line coverage for ParseScalarStrategy ---

// TestParseScalarStrategy_UnknownStrategy covers unknown input
func TestParseScalarStrategy_UnknownStrategy_Partial(t *testing.T) {
	result, err := ParseScalarStrategy("unknown-strategy")
	assert.Equal(t, MergeStrategy(""), result) // zero-value on error
	assert.Error(t, err)
}

// --- Partial line coverage for ParseArrayStrategy ---

// TestParseArrayStrategy_UnknownStrategy covers unknown input
func TestParseArrayStrategy_UnknownStrategy_Partial(t *testing.T) {
	result, err := ParseArrayStrategy("unknown")
	assert.False(t, result) // zero-value on error
	assert.Error(t, err)
}

// TestParseArrayStrategy_Replace covers "replace"
func TestParseArrayStrategy_Replace_Partial(t *testing.T) {
	result, err := ParseArrayStrategy("replace")
	assert.False(t, result)
	assert.NoError(t, err)
}

// TestParseArrayStrategy_Empty covers empty string default
func TestParseArrayStrategy_Empty_Partial(t *testing.T) {
	result, err := ParseArrayStrategy("")
	assert.True(t, result) // default is merge
	assert.NoError(t, err)
}

// --- Partial line coverage for ApplyPreset ---

// TestApplyPreset_Invalid covers invalid preset
func TestApplyPreset_Invalid_Partial(t *testing.T) {
	s, a, err := ApplyPreset("invalid", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid preset")
	// Returns original strategies
	assert.Equal(t, "", s)
	assert.Equal(t, "", a)
}

// TestApplyPreset_Empty covers empty preset (no-op)
func TestApplyPreset_Empty_Partial(t *testing.T) {
	s, a, err := ApplyPreset("", "prefer-scraper", "merge")
	assert.NoError(t, err)
	assert.Equal(t, "prefer-scraper", s)
	assert.Equal(t, "merge", a)
}

// TestApplyPreset_Conservative covers conservative preset
func TestApplyPreset_Conservative_Partial(t *testing.T) {
	s, a, err := ApplyPreset("conservative", "", "")
	assert.NoError(t, err)
	assert.Equal(t, "preserve-existing", s)
	assert.Equal(t, "merge", a)
}

// TestApplyPreset_GapFill covers gap-fill preset
func TestApplyPreset_GapFill_Partial(t *testing.T) {
	s, a, err := ApplyPreset("gap-fill", "", "")
	assert.NoError(t, err)
	assert.Equal(t, "fill-missing-only", s)
	assert.Equal(t, "merge", a)
}

// TestApplyPreset_Aggressive covers aggressive preset
func TestApplyPreset_Aggressive_Partial(t *testing.T) {
	s, a, err := ApplyPreset("aggressive", "", "")
	assert.NoError(t, err)
	assert.Equal(t, "prefer-scraper", s)
	assert.Equal(t, "replace", a)
}

// --- Partial line coverage for ApplyPresetTyped ---

// TestApplyPresetTyped_Invalid covers invalid preset
func TestApplyPresetTyped_Invalid_Partial(t *testing.T) {
	s, a, err := ApplyPresetTyped("invalid", PreferNFO, true)
	assert.Error(t, err)
	assert.Equal(t, PreferNFO, s)
	assert.True(t, a)
}

// TestApplyPresetTyped_Empty covers empty preset
func TestApplyPresetTyped_Empty_Partial(t *testing.T) {
	s, a, err := ApplyPresetTyped("", PreferScraper, false)
	assert.NoError(t, err)
	assert.Equal(t, PreferScraper, s)
	assert.False(t, a)
}

// TestApplyPresetTyped_Conservative covers conservative typed
func TestApplyPresetTyped_Conservative_Partial(t *testing.T) {
	s, a, err := ApplyPresetTyped("conservative", PreferScraper, false)
	assert.NoError(t, err)
	assert.Equal(t, PreserveExisting, s)
	assert.True(t, a)
}

// TestApplyPresetTyped_GapFill covers gap-fill typed
func TestApplyPresetTyped_GapFill_Partial(t *testing.T) {
	s, a, err := ApplyPresetTyped("gap-fill", PreferScraper, false)
	assert.NoError(t, err)
	assert.Equal(t, FillMissingOnly, s)
	assert.True(t, a)
}

// TestApplyPresetTyped_Aggressive covers aggressive typed
func TestApplyPresetTyped_Aggressive_Partial(t *testing.T) {
	s, a, err := ApplyPresetTyped("aggressive", PreferNFO, true)
	assert.NoError(t, err)
	assert.Equal(t, PreferScraper, s)
	assert.False(t, a)
}
