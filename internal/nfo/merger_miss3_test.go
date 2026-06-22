package nfo

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Miss3 coverage for merger.go ---
// Focuses on: mergeActressSlices Phase 2 reverse matching,
// mergeStringField critical field paths, mergeScalarField with both values

// mergeActressSlices: Phase 2 reverse match — scraped has JapaneseName but NFO only has romanized
func TestMiss3_MergeActressSlices_ReverseMatchJapaneseToRomanized(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "田中麻里", FirstName: "", LastName: ""},
	}
	nfo := []models.Actress{
		{FirstName: "Mari", LastName: "Tanaka", ThumbURL: "https://example.com/thumb.jpg"},
	}

	// Phase 1: NFO's romanized name doesn't match scraped's JapaneseName
	// Phase 2: Scraped's JapaneseName doesn't match NFO's romanized name either
	// Both are unmatched, both should appear in output
	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2)
}

// mergeActressSlices: Phase 2 reverse match by JapaneseName
func TestMiss3_MergeActressSlices_ReverseMatchByJPName(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "佐藤花子", FirstName: "Sato", LastName: "Hanako"},
	}
	nfo := []models.Actress{
		{JapaneseName: "佐藤花子", FirstName: "NFOFirst", LastName: "NFOLast", ThumbURL: "https://example.com/thumb3.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 1)
	assert.Equal(t, "佐藤花子", result[0].JapaneseName)
	// With preferNFO=false, scraped JapaneseName is kept, NFO fills empty fields
	assert.Equal(t, "Sato", result[0].FirstName)
	assert.Equal(t, "https://example.com/thumb3.jpg", result[0].ThumbURL)
}

// mergeActressSlices: preferNFO=true overwrites names
func TestMiss3_MergeActressSlices_PreferNFOOverwritesNames(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "田中麻里", FirstName: "Mari", LastName: "ScraperLast"},
	}
	nfo := []models.Actress{
		{JapaneseName: "田中麻里", FirstName: "NFOFirst", LastName: "NFOLast"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	assert.Len(t, result, 1)
	// With preferNFO, NFO name fields should overwrite
	assert.Equal(t, "NFOFirst", result[0].FirstName)
	assert.Equal(t, "NFOLast", result[0].LastName)
}

// mergeActressSlices: unmatched scraped and NFO entries (no match at all)
func TestMiss3_MergeActressSlices_NoMatch(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "山田太郎", FirstName: "Taro"},
	}
	nfo := []models.Actress{
		{JapaneseName: "鈴木一郎", FirstName: "Ichiro"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2) // Both should appear
}

// mergeActressSlices: empty slices
func TestMiss3_MergeActressSlices_EmptyScraped(t *testing.T) {
	result := mergeActressSlices(nil, []models.Actress{{FirstName: "A"}}, false)
	assert.Len(t, result, 1)
}

func TestMiss3_MergeActressSlices_EmptyNFO(t *testing.T) {
	result := mergeActressSlices([]models.Actress{{FirstName: "A"}}, nil, false)
	assert.Len(t, result, 1)
}

func TestMiss3_MergeActressSlices_BothEmpty(t *testing.T) {
	result := mergeActressSlices(nil, nil, false)
	assert.Empty(t, result) // Both nil → empty result (not nil, since entries loop produces empty slice)
}

// mergeActressSlices: matched actresses with preferNFO=false (fill empty)
func TestMiss3_MergeActressSlices_MatchedFillEmpty(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", FirstName: "", LastName: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", FirstName: "NFOFirst", LastName: "NFOLast"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 1)
	// With preferNFO=false, NFO names fill empty scraped fields
	assert.Equal(t, "NFOFirst", result[0].FirstName)
	assert.Equal(t, "NFOLast", result[0].LastName)
}

// mergeStringField: critical field both empty → fallback
func TestMiss3_MergeStringField_CriticalBothEmpty(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("ID", "", "", PreferScraper, fm)
	assert.Contains(t, result, "Unknown")
	assert.Equal(t, 1, stats.EmptyFields)
}

// mergeStringField: critical field empty in scraper, filled in NFO
func TestMiss3_MergeStringField_CriticalEmptyInScraper(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("ID", "", "NFO-ID", PreferScraper, fm)
	// Even with PreferScraper, critical field should fall back to NFO
	assert.Equal(t, "NFO-ID", result)
	assert.Equal(t, 1, stats.FromNFO)
}

// mergeStringField: PreferScraper with empty scraper value (strict mode)
func TestMiss3_MergeStringField_PreferScraperEmptyStrict(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Maker", "", "NFO Maker", PreferScraper, fm)
	// With PreferScraper, empty scraper value is used (strict mode)
	assert.Equal(t, "", result)
	assert.Equal(t, 1, stats.FromScraper)
}

// mergeStringField: PreferNFO with empty NFO value (strict mode)
func TestMiss3_MergeStringField_PreferNFOEmptyStrict(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Maker", "Scraper Maker", "", PreferNFO, fm)
	// With PreferNFO, empty NFO value is used (strict mode)
	assert.Equal(t, "", result)
	assert.Equal(t, 1, stats.FromNFO)
}

// mergeStringField: PreserveExisting strategy with both values
func TestMiss3_MergeStringField_PreserveExistingBoth(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Maker", "Scraper Maker", "NFO Maker", PreserveExisting, fm) // when both have data
	assert.Equal(t, "NFO Maker", result)
	assert.Equal(t, 1, stats.FromNFO)
	assert.Equal(t, 1, stats.ConflictsResolved)
}

// mergeStringField: FillMissingOnly strategy with both values
func TestMiss3_MergeStringField_FillMissingOnlyBoth(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Maker", "Scraper Maker", "NFO Maker", FillMissingOnly, fm)
	// FillMissingOnly prefers NFO when both have data
	assert.Equal(t, "NFO Maker", result)
}

// mergeStringField: MergeArrays strategy for string falls back to scraper
func TestMiss3_MergeStringField_MergeArraysFallback(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Maker", "Scraper Maker", "NFO Maker", MergeArrays, fm)
	// MergeArrays for scalars should prefer scraper
	assert.Equal(t, "Scraper Maker", result)
	assert.Equal(t, 1, stats.FromScraper)
}

// mergeStringField: unknown strategy → default to scraper
func TestMiss3_MergeStringField_UnknownStrategy(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Maker", "Scraper Maker", "NFO Maker", MergeStrategy("unknown"), fm)
	assert.Equal(t, "Scraper Maker", result)
}

// mergeScalarField: both values present with PreferNFO
func TestMiss3_MergeScalarField_BothPresentPreferNFO(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 120, 90, PreferNFO, fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 90, result)
	assert.Equal(t, 1, stats.FromNFO)
	assert.Equal(t, 1, stats.ConflictsResolved)
}

// mergeScalarField: both values present with PreferScraper
func TestMiss3_MergeScalarField_BothPresentPreferScraper(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 120, 90, PreferScraper, fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 120, result)
	assert.Equal(t, 1, stats.FromScraper)
}

// mergeScalarField: both values present with MergeArrays
func TestMiss3_MergeScalarField_BothPresentMergeArrays(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 120, 90, MergeArrays, fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 120, result) // MergeArrays for scalars falls back to scraper
}

// mergeScalarField: both values present with unknown strategy
func TestMiss3_MergeScalarField_BothPresentUnknown(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 120, 90, MergeStrategy("unknown"), fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 120, result) // Unknown defaults to scraper
}

// mergeScalarField: scraped empty, nfo filled
func TestMiss3_MergeScalarField_ScrapedEmpty(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 0, 90, PreferScraper, fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 90, result) // Falls back to NFO
	assert.Equal(t, 1, stats.FromNFO)
}

// mergeScalarField: both empty
func TestMiss3_MergeScalarField_BothEmpty(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 0, 0, PreferScraper, fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 0, result)
	assert.Equal(t, 1, stats.EmptyFields)
}

// MergeMovieMetadataWithOptions: both nil returns error
func TestMiss3_MergeMovieMetadata_BothNil(t *testing.T) {
	_, err := MergeMovieMetadataWithOptions(nil, nil, PreferScraper, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both scraped and nfo are nil")
}

// MergeMovieMetadataWithOptions: scraped nil → return nfo
func TestMiss3_MergeMovieMetadata_ScrapedNil(t *testing.T) {
	nfo := &models.Movie{ID: "N-001", Title: "NFO Title"}
	result, err := MergeMovieMetadataWithOptions(nil, nfo, PreferScraper, true)
	require.NoError(t, err)
	assert.Equal(t, "N-001", result.Merged.ID)
}

// MergeMovieMetadataWithOptions: nfo nil → return scraped
func TestMiss3_MergeMovieMetadata_NFONil(t *testing.T) {
	scraped := &models.Movie{ID: "S-001", Title: "Scraper Title"}
	result, err := MergeMovieMetadataWithOptions(scraped, nil, PreferScraper, true)
	require.NoError(t, err)
	assert.Equal(t, "S-001", result.Merged.ID)
}

// countNonEmptyFields: nil movie
func TestMiss3_CountNonEmptyFields_NilMovie(t *testing.T) {
	assert.Equal(t, 0, countNonEmptyFields(nil))
}

// makeProvenanceMap: nil movie
func TestMiss3_MakeProvenanceMap_NilMovie(t *testing.T) {
	result := makeProvenanceMap(nil, "scraper")
	assert.Empty(t, result)
}
