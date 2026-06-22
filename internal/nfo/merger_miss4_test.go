package nfo

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// --- mergeActressSlices: NFO has romanized name, scraped has JapaneseName, reverse match by romanized name ---

func TestMiss4_MergeActressSlices_ReverseMatchRomanizedToJapanese(t *testing.T) {
	// Scraped has romanized name, NFO has romanized name too
	scraped := []models.Actress{
		{FirstName: "Yui", LastName: "Hatano"},
	}
	nfo := []models.Actress{
		{FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://example.com/thumb.jpg"},
	}

	// Phase 1: NFO's romanized name matches scraped's romanized name
	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 1)
	assert.Equal(t, "https://example.com/thumb.jpg", result[0].ThumbURL)
}

// --- mergeActressSlices: preferNFO=true with matched actresses ---

func TestMiss4_MergeActressSlices_PreferNFONamesOverwrite(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "波多野結衣", FirstName: "Yui", LastName: "Hatano"},
	}
	nfo := []models.Actress{
		{JapaneseName: "波多野結衣", FirstName: "NFOFirst", LastName: "NFOLast"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	assert.Len(t, result, 1)
	// preferNFO=true should overwrite with NFO's name data
	assert.Equal(t, "NFOFirst", result[0].FirstName)
	assert.Equal(t, "NFOLast", result[0].LastName)
	// DMMID and JapaneseName from scraped should be preserved
	assert.Equal(t, "波多野結衣", result[0].JapaneseName)
}

// --- mergeActressSlices: preferNFO=false fills empty fields from NFO ---

func TestMiss4_MergeActressSlices_PreferNFOFalseFillsEmpty(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "佐藤花子", FirstName: "", LastName: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "佐藤花子", FirstName: "Hanako", LastName: "Sato"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 1)
	// preferNFO=false should fill empty fields from NFO
	assert.Equal(t, "Hanako", result[0].FirstName)
	assert.Equal(t, "Sato", result[0].LastName)
}

// --- mergeActressSlices: NFO actress with ThumbURL fills scraped empty ThumbURL ---

func TestMiss4_MergeActressSlices_ThumbURLFillsFromNFO(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", ThumbURL: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", ThumbURL: "https://example.com/thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 1)
	assert.Equal(t, "https://example.com/thumb.jpg", result[0].ThumbURL)
}

// --- mergeActressSlices: unmatched actresses from both sources appear in result ---

func TestMiss4_MergeActressSlices_UnmatchedActressesBothAppear(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "女優A", FirstName: "Actress", LastName: "A"},
	}
	nfo := []models.Actress{
		{JapaneseName: "女優B", FirstName: "Actress", LastName: "B"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 2) // Both appear since they don't match
}

// --- mergeActressSlices: matched by reversed romanized name order ---

func TestMiss4_MergeActressSlices_MatchByReversedRomanizedName(t *testing.T) {
	// Japanese names often have reversed first/last order between sources
	scraped := []models.Actress{
		{FirstName: "Hanako", LastName: "Sato"},
	}
	nfo := []models.Actress{
		{FirstName: "Sato", LastName: "Hanako", ThumbURL: "https://example.com/thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 1)
	// Should match because reversed order is checked
	assert.Equal(t, "https://example.com/thumb.jpg", result[0].ThumbURL)
}

// --- mergeActressSlices: empty slices ---

func TestMiss4_MergeActressSlices_EmptySlices(t *testing.T) {
	result := mergeActressSlices(nil, nil, false)
	assert.Empty(t, result)

	result = mergeActressSlices([]models.Actress{}, nil, false)
	assert.Empty(t, result)

	result = mergeActressSlices(nil, []models.Actress{}, false)
	assert.Empty(t, result)
}

// --- mergeActressSlices: Phase 2 reverse match - scraped has both JP and romanized, NFO only has romanized ---

func TestMiss4_MergeActressSlices_Phase2ReverseMatchJpAndRomanized(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "山田太郎", FirstName: "Taro", LastName: "Yamada"},
	}
	nfo := []models.Actress{
		{FirstName: "Taro", LastName: "Yamada", ThumbURL: "https://example.com/thumb.jpg"},
	}

	// Phase 1: NFO tries to match scraped by JapaneseName - NFO has no JP name, no match
	// Phase 2: Scraped tries to match NFO by romanized name - should match
	result := mergeActressSlices(scraped, nfo, false)
	assert.Len(t, result, 1)
	assert.Equal(t, "山田太郎", result[0].JapaneseName)
	assert.Equal(t, "https://example.com/thumb.jpg", result[0].ThumbURL)
}

// --- mergeActresses: PreferScraper strategy returns scraped ---

func TestMiss4_MergeActresses_PreferScraperReturnsScraped(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "女優A"},
	}
	nfo := []models.Actress{
		{JapaneseName: "女優A", ThumbURL: "https://example.com/thumb.jpg"},
	}

	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)
	result := mergeActresses("Actresses", scraped, nfo, PreferScraper, fm)
	assert.Len(t, result, 1)
	// PreferScraper returns scraped directly (no merge)
	assert.Equal(t, "", result[0].ThumbURL)
}

// --- mergeActresses: PreserveExisting strategy merges like PreferNFO ---

func TestMiss4_MergeActresses_PreserveExistingMergesLikeNFO(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", FirstName: "Test", ThumbURL: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", FirstName: "NFOFirst", ThumbURL: "https://example.com/thumb.jpg"},
	}

	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)
	result := mergeActresses("Actresses", scraped, nfo, PreserveExisting, fm)
	assert.Len(t, result, 1)
	assert.Equal(t, "https://example.com/thumb.jpg", result[0].ThumbURL)
}

// --- mergeActresses: FillMissingOnly strategy merges like PreferNFO ---

func TestMiss4_MergeActresses_FillMissingOnlyMergesLikeNFO(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", FirstName: "Test", ThumbURL: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", ThumbURL: "https://example.com/thumb.jpg"},
	}

	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)
	result := mergeActresses("Actresses", scraped, nfo, FillMissingOnly, fm)
	assert.Len(t, result, 1)
	assert.Equal(t, "https://example.com/thumb.jpg", result[0].ThumbURL)
}

// --- mergeActresses: MergeArrays strategy ---

func TestMiss4_MergeActresses_MergeArraysStrategy(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "テスト", FirstName: "Test"},
	}
	nfo := []models.Actress{
		{JapaneseName: "テスト", ThumbURL: "https://example.com/thumb.jpg"},
	}

	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)
	result := mergeActresses("Actresses", scraped, nfo, MergeArrays, fm)
	assert.Len(t, result, 1)
	assert.Equal(t, "https://example.com/thumb.jpg", result[0].ThumbURL)
	assert.Equal(t, 1, stats.MergedArrays)
}
