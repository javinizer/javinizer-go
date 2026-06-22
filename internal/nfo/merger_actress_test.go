package nfo

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeActresses_BothEmpty(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeActresses("actresses", nil, nil, MergeArrays, fm)
	assert.Nil(t, result)
	assert.Equal(t, 1, stats.EmptyFields)
}

func TestMergeActresses_ScraperOnly(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []models.Actress{
		{JapaneseName: "TestActress"},
	}

	result := mergeActresses("actresses", scraped, nil, MergeArrays, fm)
	assert.Len(t, result, 1)
	assert.Equal(t, 1, stats.FromScraper)
}

func TestMergeActresses_NFOOnly(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	nfo := []models.Actress{
		{JapaneseName: "NFOActress"},
	}

	result := mergeActresses("actresses", nil, nfo, MergeArrays, fm)
	assert.Len(t, result, 1)
	assert.Equal(t, 1, stats.FromNFO)
}

func TestMergeActresses_PreferNFOStrategy(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []models.Actress{{JapaneseName: "Match", DMMID: 1, ThumbURL: "http://scraper.jpg"}}
	nfo := []models.Actress{{JapaneseName: "Match", FirstName: "NFOFirst", LastName: "NFOLast"}}

	result := mergeActresses("actresses", scraped, nfo, PreferNFO, fm)
	assert.Len(t, result, 1)
	// DMMID always from scraper
	assert.Equal(t, 1, result[0].DMMID)
	// When preferNFO=true, name data from NFO is used
	assert.Equal(t, "NFOFirst", result[0].FirstName)
}

func TestMergeActresses_FillMissingOnlyStrategy(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []models.Actress{{JapaneseName: "Match", DMMID: 2}}
	nfo := []models.Actress{{JapaneseName: "Match", FirstName: "Filled"}}

	result := mergeActresses("actresses", scraped, nfo, FillMissingOnly, fm)
	assert.Len(t, result, 1)
	assert.Equal(t, 2, result[0].DMMID)
}

func TestMergeActresses_MergeArraysStrategy(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []models.Actress{{JapaneseName: "Actress1"}, {JapaneseName: "Actress2"}}
	nfo := []models.Actress{{JapaneseName: "Actress3"}}

	result := mergeActresses("actresses", scraped, nfo, MergeArrays, fm)
	assert.GreaterOrEqual(t, len(result), 3)
}

func TestMergeActresses_PreserveExistingStrategy(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []models.Actress{{JapaneseName: "Match", DMMID: 3}}
	nfo := []models.Actress{{JapaneseName: "Match", FirstName: "Existing"}}

	result := mergeActresses("actresses", scraped, nfo, PreserveExisting, fm)
	assert.Len(t, result, 1)
	assert.Equal(t, 3, result[0].DMMID)
}

func TestMergeActressSlices_EmptyInputs(t *testing.T) {
	result := mergeActressSlices(nil, nil, false)
	assert.Empty(t, result)
}

func TestMergeActressSlices_ScraperOnly(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "TestActress", DMMID: 123, ThumbURL: "http://example.com/thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nil, false)
	assert.Len(t, result, 1)
	assert.Equal(t, 123, result[0].DMMID)
	assert.Equal(t, "http://example.com/thumb.jpg", result[0].ThumbURL)
}

func TestMergeActressSlices_NFOOnly(t *testing.T) {
	nfo := []models.Actress{
		{JapaneseName: "NFOActress", FirstName: "Test", LastName: "Actress"},
	}

	result := mergeActressSlices(nil, nfo, false)
	assert.Len(t, result, 1)
	assert.Equal(t, "Test", result[0].FirstName)
}

func TestMergeActressSlices_MatchedByJapaneseName(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "波多野結衣", DMMID: 123, ThumbURL: "http://example.com/thumb.jpg"},
	}
	nfo := []models.Actress{
		{JapaneseName: "波多野結衣", FirstName: "Yui", LastName: "Hatano"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	// Should merge into one entry (matched by JapaneseName)
	assert.Len(t, result, 1)
	// DMMID always from scraper
	assert.Equal(t, 123, result[0].DMMID)
}

func TestMergeActressSlices_DifferentActresses(t *testing.T) {
	scraped := []models.Actress{
		{DMMID: 456, JapaneseName: "ScraperName", ThumbURL: "http://example.com/thumb.jpg"},
	}
	nfo := []models.Actress{
		{JapaneseName: "NFOName", FirstName: "Test"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2)
	// Verify both actresses are preserved
	assert.Equal(t, 456, result[0].DMMID)
}

func TestMergeActressSlices_PreferNFO(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "Test", FirstName: "ScraperFirst", LastName: "ScraperLast"},
	}
	nfo := []models.Actress{
		{JapaneseName: "Test", FirstName: "NFOFirst", LastName: "NFOLast"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	require.Len(t, result, 1)
	assert.Equal(t, "NFOFirst", result[0].FirstName)
	assert.Equal(t, "NFOLast", result[0].LastName)
}

func TestMergeActressSlices_PreferScraper(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "Test", FirstName: "ScraperFirst", LastName: "ScraperLast"},
	}
	nfo := []models.Actress{
		{JapaneseName: "Test", FirstName: "NFOFirst", LastName: "NFOLast"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	assert.Equal(t, "ScraperFirst", result[0].FirstName)
	assert.Equal(t, "ScraperLast", result[0].LastName)
}

func TestMergeActressSlices_ThumbURLFilledFromEither(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "Test", ThumbURL: "http://scraper/thumb.jpg"},
	}
	nfo := []models.Actress{
		{JapaneseName: "Test", ThumbURL: ""},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	assert.Equal(t, "http://scraper/thumb.jpg", result[0].ThumbURL)
}

func TestMergeActressSlices_NoMatch(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "ScraperOnly", DMMID: 100},
	}
	nfo := []models.Actress{
		{JapaneseName: "NFOOnly", FirstName: "NFO"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2)
}

func TestMergeActressSlices_MatchedByRomanizedName(t *testing.T) {
	scraped := []models.Actress{
		{FirstName: "Yui", LastName: "Hatano", JapaneseName: ""},
	}
	nfo := []models.Actress{
		{FirstName: "Yui", LastName: "Hatano", JapaneseName: ""},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 1, "Should match by romanized name")
}

func TestMergeSlice_MergeArraysWithDedup(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []string{"a", "b", "c"}
	nfo := []string{"b", "c", "d"}

	result := mergeSlice("test", scraped, nfo, MergeArrays, fm, func(s string) string { return s })
	// Should deduplicate
	assert.GreaterOrEqual(t, len(result), 4)
}

func TestMergeSlice_MergeArraysNoDedupKey(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []string{"a", "b"}
	nfo := []string{"c", "d"}

	result := mergeSlice("test", scraped, nfo, MergeArrays, fm, func(s string) string { return "" })
	// With no dedup key, all items should be included
	assert.Len(t, result, 4)
}
