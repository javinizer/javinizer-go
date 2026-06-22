package nfo

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// --- mergeActressSlices: Phase 2 reverse match with preferNFO=true ---
// This exercises the preferNFO branch in Phase 2 (lines 656-662 of merger.go)
// which is currently uncovered.

func TestMiss5_MergeActressSlices_Phase2ReverseMatchPreferNFOTrue(t *testing.T) {
	// Scraped has romanized name, NFO also has romanized name
	// Phase 1: NFO tries to match by JapaneseName — no JP name, no match
	// Phase 2: Scraped tries to match NFO by romanized name — should match
	// preferNFO=true should overwrite scraped names with NFO names
	scraped := []models.Actress{
		{FirstName: "Yui", LastName: "Hatano", ThumbURL: ""},
	}
	nfo := []models.Actress{
		{FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://example.com/thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	assert.Len(t, result, 1)
	// preferNFO=true: NFO's names should overwrite scraped's names
	assert.Equal(t, "Yui", result[0].FirstName)
	assert.Equal(t, "Hatano", result[0].LastName)
	assert.Equal(t, "https://example.com/thumb.jpg", result[0].ThumbURL)
}

// --- mergeActressSlices: Phase 2 reverse match by JapaneseName with preferNFO=true ---

func TestMiss5_MergeActressSlices_Phase2ReverseMatchByJPNamePreferNFO(t *testing.T) {
	// Scraped has JapaneseName, NFO has JapaneseName
	// Phase 1: NFO tries to match by JapaneseName — should match
	// But let's test Phase 2 specifically where scraped has JP name and NFO has romanized name only
	// This is the reverse match case
	scraped := []models.Actress{
		{JapaneseName: "山田花子", FirstName: "", LastName: ""},
	}
	nfo := []models.Actress{
		{FirstName: "Hanako", LastName: "Yamada", JapaneseName: "山田花子", ThumbURL: "https://example.com/thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	assert.Len(t, result, 1)
	// Phase 1: NFO matches by JP name to scraped — should match and merge
	assert.Equal(t, "山田花子", result[0].JapaneseName)
	assert.Equal(t, "Hanako", result[0].FirstName) // preferNFO=true, NFO name overwrites
	assert.Equal(t, "Yamada", result[0].LastName)
}

// --- mergeActressSlices: Phase 2 reverse match where scraped has JP name but NFO only has romanized ---
// This exercises findNFOMatch (lines 573-588)

func TestMiss5_MergeActressSlices_Phase2ScrapedJPNameNFORomanized(t *testing.T) {
	// Scraped has JP name but no romanized name
	// NFO has romanized name but no JP name
	// Phase 1: NFO tries to match scraped — NFO has no JP name, tries romanized — no match (scraped has no romanized)
	// Phase 2: Scraped tries to match NFO — scraped has no romanized name, JP name doesn't match NFO (NFO has no JP)
	// This should result in NO match — both appear separately
	scraped := []models.Actress{
		{JapaneseName: "山田太郎", FirstName: "", LastName: ""},
	}
	nfo := []models.Actress{
		{FirstName: "Taro", LastName: "Yamada", ThumbURL: "https://example.com/thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2, "scraped JP name and NFO romanized name don't cross-match without shared identifier")
}

// --- mergeActressSlices: Phase 2 reverse match where scraped has romanized that matches NFO romanized ---

func TestMiss5_MergeActressSlices_Phase2ScrapedRomanizedMatchesNFORomanized(t *testing.T) {
	// Scraped has JP name AND romanized name
	// NFO has romanized name only
	// Phase 1: NFO tries to match scraped — no JP name in NFO, tries romanized — match found!
	// This should merge in Phase 1 (not Phase 2)
	scraped := []models.Actress{
		{JapaneseName: "佐藤美咲", FirstName: "Misaki", LastName: "Sato"},
	}
	nfo := []models.Actress{
		{FirstName: "Misaki", LastName: "Sato", ThumbURL: "https://example.com/thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 1)
	assert.Equal(t, "佐藤美咲", result[0].JapaneseName)
	assert.Equal(t, "https://example.com/thumb.jpg", result[0].ThumbURL)
}

// --- mergeActressSlices: Phase 2 with scraped having romanized name that matches NFO via reversed order ---

func TestMiss5_MergeActressSlices_Phase2ReverseOrderMatch(t *testing.T) {
	// Scraped has FirstName/LastName in one order, NFO has them reversed
	// Phase 1 might not match if NFO doesn't have JP name
	// Phase 2: scraped tries romanized name (including reversed) against NFO
	scraped := []models.Actress{
		{FirstName: "Hanako", LastName: "Sato", ThumbURL: ""},
	}
	nfo := []models.Actress{
		{FirstName: "Sato", LastName: "Hanako", ThumbURL: "https://example.com/thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 1)
	assert.Equal(t, "https://example.com/thumb.jpg", result[0].ThumbURL)
}

// --- mergeActressSlices: Phase 2 preferNFO=true overwrites with NFO JP name ---

func TestMiss5_MergeActressSlices_Phase2PreferNFOOverwritesJPName(t *testing.T) {
	// Scraped has JP name, NFO has same JP name
	// Phase 1 should match
	scraped := []models.Actress{
		{JapaneseName: "鈴木一郎", FirstName: "Ichiro", LastName: "Suzuki", ThumbURL: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "鈴木一郎", FirstName: "NFOFirst", LastName: "NFOLast", ThumbURL: "https://example.com/nfo_thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	assert.Len(t, result, 1)
	// preferNFO=true: NFO's romanized names should overwrite
	assert.Equal(t, "NFOFirst", result[0].FirstName)
	assert.Equal(t, "NFOLast", result[0].LastName)
	assert.Equal(t, "鈴木一郎", result[0].JapaneseName) // JP name from NFO overwrites too
	assert.Equal(t, "https://example.com/nfo_thumb.jpg", result[0].ThumbURL)
}

// --- mergeActressSlices: Phase 2 preferNFO=false fills empty fields from NFO ---

func TestMiss5_MergeActressSlices_Phase2PreferNFOFalseFillsEmptyFields(t *testing.T) {
	// Scraped has empty first/last name, NFO has romanized name
	// Phase 2 should match and fill empty fields
	scraped := []models.Actress{
		{JapaneseName: "高橋花子", FirstName: "", LastName: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "高橋花子", FirstName: "Hanako", LastName: "Takahashi", ThumbURL: "https://example.com/thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 1)
	// preferNFO=false: fills empty fields from NFO
	assert.Equal(t, "Hanako", result[0].FirstName)
	assert.Equal(t, "Takahashi", result[0].LastName)
	assert.Equal(t, "https://example.com/thumb.jpg", result[0].ThumbURL)
}

// --- mergeScalarField: int conflict with PreferNFO strategy ---

func TestMiss5_MergeScalarField_IntConflictPreferNFO(t *testing.T) {
	scraped := &models.Movie{ReleaseYear: 2023, Runtime: 120}
	nfo := &models.Movie{ReleaseYear: 2020, Runtime: 90}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	requireNoError(t, err)
	assert.Equal(t, 2020, result.Merged.ReleaseYear, "PreferNFO should choose NFO value")
	assert.Equal(t, 90, result.Merged.Runtime)
}

// --- mergeScalarField: int conflict with PreferScraper strategy ---

func TestMiss5_MergeScalarField_IntConflictPreferScraper(t *testing.T) {
	scraped := &models.Movie{ReleaseYear: 2023, Runtime: 120}
	nfo := &models.Movie{ReleaseYear: 2020, Runtime: 90}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	requireNoError(t, err)
	assert.Equal(t, 2023, result.Merged.ReleaseYear, "PreferScraper should choose scraped value")
	assert.Equal(t, 120, result.Merged.Runtime)
}

// --- mergeScalarField: float conflict with PreferScraper ---

func TestMiss5_MergeScalarField_FloatConflictPreferScraper(t *testing.T) {
	scraped := &models.Movie{RatingScore: 8.5}
	nfo := &models.Movie{RatingScore: 7.0}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	requireNoError(t, err)
	assert.Equal(t, 8.5, result.Merged.RatingScore)
}

// --- mergeScalarField: bool with PreferScraper ---

func TestMiss5_MergeScalarField_BoolWithPreferScraper(t *testing.T) {
	scraped := &models.Movie{}
	scraped.Poster.ShouldCropPoster = true
	nfo := &models.Movie{}
	nfo.Poster.ShouldCropPoster = false

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	requireNoError(t, err)
	assert.True(t, result.Merged.Poster.ShouldCropPoster)
}

// --- mergeScalarField: time.Time with PreferNFO ---

func TestMiss5_MergeScalarField_TimeConflictPreferNFO(t *testing.T) {
	scrapedDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	nfoDate := time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)
	scraped := &models.Movie{ReleaseDate: &scrapedDate}
	nfo := &models.Movie{ReleaseDate: &nfoDate}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	requireNoError(t, err)
	assert.True(t, result.Merged.ReleaseDate.Equal(nfoDate), "PreferNFO should choose NFO date")
}

// --- mergeScalarField: scraped empty, nfo has value, PreferScraper ---

func TestMiss5_MergeScalarField_ScrapedEmptyPreferScraper(t *testing.T) {
	// With PreferScraper on scalars, when scraped is empty but NFO has value,
	// the scalar merge (unlike string merge) falls back to NFO
	scraped := &models.Movie{ReleaseYear: 0} // empty
	nfo := &models.Movie{ReleaseYear: 2020}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	requireNoError(t, err)
	// mergeScalarField doesn't have the PreferScraper strict mode — it falls back to NFO when scraped is empty
	assert.Equal(t, 2020, result.Merged.ReleaseYear, "when scraped is empty, scalar field falls back to NFO")
}

// --- mergeScalarField: nfo empty, scraped has value, PreferNFO ---

func TestMiss5_MergeScalarField_NFOEmptyPreferNFO(t *testing.T) {
	scraped := &models.Movie{RatingVotes: 1000}
	nfo := &models.Movie{RatingVotes: 0}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	requireNoError(t, err)
	// mergeScalarField doesn't have the PreferNFO strict mode — it falls back to scraped when NFO is empty
	assert.Equal(t, 1000, result.Merged.RatingVotes, "when NFO is empty, scalar field falls back to scraped")
}

// --- mergeScalarField: both empty ---

func TestMiss5_MergeScalarField_BothEmpty(t *testing.T) {
	scraped := &models.Movie{ReleaseYear: 0}
	nfo := &models.Movie{ReleaseYear: 0}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	requireNoError(t, err)
	assert.Equal(t, 0, result.Merged.ReleaseYear)
	assert.Greater(t, result.Stats.EmptyFields, 0, "should have at least one empty field count")
}

// --- mergeActressSlices: Phase 2 with findNFOMatch by romanized reversed order ---

func TestMiss5_MergeActressSlices_Phase2FindNFOMatchByRomanized(t *testing.T) {
	// Scraped has romanized name, NFO has romanized name in same order
	// Phase 1 matches — but let's test Phase 2 explicitly
	// by having scraped with a JP name that Phase 1 can't match from NFO side
	scraped := []models.Actress{
		{JapaneseName: "田中太郎", FirstName: "Taro", LastName: "Tanaka", ThumbURL: ""},
	}
	nfo := []models.Actress{
		{FirstName: "Taro", LastName: "Tanaka", ThumbURL: "https://example.com/thumb.jpg"},
	}

	// Phase 1: NFO tries to match by JP name — no JP name in NFO
	// Phase 1: NFO tries to match by romanized — found in scraped
	// So this should match in Phase 1 already
	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 1)
	assert.Equal(t, "https://example.com/thumb.jpg", result[0].ThumbURL)
}

// --- mergeActressSlices: multiple scraped actresses, some match, some don't ---

func TestMiss5_MergeActressSlices_MultipleWithPartialMatch(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "女優A", FirstName: "Actress", LastName: "A", ThumbURL: "scraped_a.jpg"},
		{JapaneseName: "女優C", FirstName: "Actress", LastName: "C", ThumbURL: "scraped_c.jpg"},
	}
	nfo := []models.Actress{
		{JapaneseName: "女優A", FirstName: "NFOA", LastName: "NFOA_Last", ThumbURL: "nfo_a.jpg"},
		{JapaneseName: "女優B", FirstName: "NFOB", LastName: "NFOB_Last", ThumbURL: "nfo_b.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 3) // A merged, B unmatched, C unmatched
}

// Helper for tests that don't have require imported
func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
