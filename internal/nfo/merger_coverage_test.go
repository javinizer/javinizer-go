package nfo

import (
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeActressSlices_ReverseMatchByRomanizedName(t *testing.T) {
	// Scraped has JapaneseName but no romanized; NFO has romanized but no JapaneseName
	// Phase 2 reverse match should find the connection
	scraped := []models.Actress{
		{JapaneseName: "波多野結衣", DMMID: 50, ThumbURL: "http://thumb.jpg"},
	}
	nfo := []models.Actress{
		{FirstName: "Hatano", LastName: "Yui", JapaneseName: "波多野結衣"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	assert.Equal(t, 50, result[0].DMMID)
	assert.Equal(t, "http://thumb.jpg", result[0].ThumbURL)
}

func TestMergeActressSlices_PreferNFOFillsNames(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", DMMID: 10, FirstName: "SFirst", LastName: "SLast"},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", FirstName: "NFirst", LastName: "NLast"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	require.Len(t, result, 1)
	assert.Equal(t, "NFirst", result[0].FirstName)
	assert.Equal(t, "NLast", result[0].LastName)
	assert.Equal(t, 10, result[0].DMMID)
}

func TestMergeActressSlices_PreferScraperFillsMissing(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", DMMID: 10, FirstName: "SFirst", LastName: "SLast"},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", FirstName: "NFirst", LastName: "NLast"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	// Scraper values should be kept since they exist
	assert.Equal(t, "SFirst", result[0].FirstName)
	assert.Equal(t, "SLast", result[0].LastName)
}

func TestMergeActressSlices_ThumbURLFromNFO(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", DMMID: 10, ThumbURL: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", ThumbURL: "http://nfo-thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	assert.Equal(t, "http://nfo-thumb.jpg", result[0].ThumbURL)
}

func TestMergeActressSlices_ThumbURLFromScraperPreserved(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", DMMID: 10, ThumbURL: "http://scraper-thumb.jpg"},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", ThumbURL: "http://nfo-thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1)
	// Scraper thumb should be preserved when it exists
	assert.Equal(t, "http://scraper-thumb.jpg", result[0].ThumbURL)
}

func TestMergeActressSlices_MultipleMatchesByRomanizedName(t *testing.T) {
	scraped := []models.Actress{
		{FirstName: "Yui", LastName: "Hatano", DMMID: 100},
	}
	nfo := []models.Actress{
		{FirstName: "Hatano", LastName: "Yui"}, // Reversed order
	}

	result := mergeActressSlices(scraped, nfo, false)
	require.Len(t, result, 1, "should match by reversed romanized name")
	assert.Equal(t, 100, result[0].DMMID)
}

func TestMergeActressSlices_UnmatchedNFOActress(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "Actress1", DMMID: 1},
	}
	nfo := []models.Actress{
		{JapaneseName: "Actress2", FirstName: "Unique"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2)
}

func TestMergeSlice_PreferNFO(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []string{"a"}
	nfo := []string{"b"}
	result := mergeSlice("test", scraped, nfo, PreferNFO, fm, func(s string) string { return s })
	assert.Equal(t, []string{"b"}, result)
}

func TestMergeSlice_PreserveExisting(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []string{"a"}
	nfo := []string{"b"}
	result := mergeSlice("test", scraped, nfo, PreserveExisting, fm, func(s string) string { return s })
	assert.Equal(t, []string{"b"}, result)
}

func TestMergeSlice_FillMissingOnly(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []string{"a"}
	nfo := []string{"b"}
	result := mergeSlice("test", scraped, nfo, FillMissingOnly, fm, func(s string) string { return s })
	assert.Equal(t, []string{"b"}, result)
}

func TestMergeSlice_PreferScraper(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []string{"a"}
	nfo := []string{"b"}
	result := mergeSlice("test", scraped, nfo, PreferScraper, fm, func(s string) string { return s })
	assert.Equal(t, []string{"a"}, result)
}

func TestMergeSlice_BothEmpty(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeSlice("test", nil, nil, MergeArrays, fm, func(s string) string { return s })
	assert.Nil(t, result)
	assert.Equal(t, 1, stats.EmptyFields)
}

func TestMergeSlice_ScraperOnly(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeSlice("test", []string{"x"}, nil, MergeArrays, fm, func(s string) string { return s })
	assert.Equal(t, []string{"x"}, result)
	assert.Equal(t, 1, stats.FromScraper)
}

func TestMergeSlice_NFOOnly(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeSlice("test", nil, []string{"y"}, MergeArrays, fm, func(s string) string { return s })
	assert.Equal(t, []string{"y"}, result)
	assert.Equal(t, 1, stats.FromNFO)
}

func TestMergeSlice_MergeArraysWithDedupKey(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []string{"A", "B"}
	nfo := []string{"a", "C"} // "a" should dedup with "A" via case-insensitive key
	result := mergeSlice("test", scraped, nfo, MergeArrays, fm, func(s string) string { return strings.ToLower(s) })
	assert.Equal(t, []string{"A", "B", "C"}, result)
}
