package nfo

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMergeMovieMetadata_FloatMergeArrays tests mergeFloatField with MergeArrays strategy
func TestMergeMovieMetadata_FloatMergeArrays(t *testing.T) {
	releaseDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	scraped := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "Scraped",
		RatingScore: 8.5,
		ReleaseDate: &releaseDate,
	}

	nfo := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "NFO",
		RatingScore: 7.0,
	}

	// With MergeArrays strategy, scalar fields like RatingScore fall back to PreferScraper
	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	require.NoError(t, err)
	require.NotNil(t, result)

	// RatingScore should prefer scraper with MergeArrays (falls back to PreferScraper for scalars)
	assert.Equal(t, 8.5, result.Merged.RatingScore)
	assert.Equal(t, "scraper", result.Provenance["RatingScore"].Source)
}

// TestMergeMovieMetadata_BoolMergeArrays tests mergeBoolField with MergeArrays strategy
func TestMergeMovieMetadata_BoolMergeArrays(t *testing.T) {
	scraped := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "Scraped",
		ShouldCropPoster: true,
	}

	nfo := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "NFO",
		ShouldCropPoster: false,
	}

	// With MergeArrays strategy, bool fields like ShouldCropPoster fall back to PreferScraper
	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	require.NoError(t, err)
	require.NotNil(t, result)

	// ShouldCropPoster should prefer scraper with MergeArrays (falls back to PreferScraper for scalars)
	// Note: false is treated as empty for bool fields, so scraper (true) is used
	assert.True(t, result.Merged.ShouldCropPoster)
	assert.Equal(t, "scraper", result.Provenance["ShouldCropPoster"].Source)
}

// TestMergeFloatField_PreserveExisting tests mergeFloatField with PreserveExisting strategy
func TestMergeFloatField_PreserveExisting(t *testing.T) {
	scraped := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "Scraped",
		RatingScore: 8.5,
	}

	nfo := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "NFO",
		RatingScore: 7.0,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreserveExisting, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// PreserveExisting should prefer NFO value when both have data
	assert.Equal(t, 7.0, result.Merged.RatingScore)
	assert.Equal(t, "nfo", result.Provenance["RatingScore"].Source)
}

// TestMergeBoolField_PreserveExisting tests mergeBoolField with PreserveExisting strategy
func TestMergeBoolField_PreserveExisting(t *testing.T) {
	scraped := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "Scraped",
		ShouldCropPoster: true,
	}

	nfo := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "NFO",
		ShouldCropPoster: true, // Both true to test conflict resolution (false = empty)
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreserveExisting, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// PreserveExisting should prefer NFO value when both have data
	assert.True(t, result.Merged.ShouldCropPoster)
	assert.Equal(t, "nfo", result.Provenance["ShouldCropPoster"].Source)
}

// TestMergeFloatField_FillMissingOnly tests mergeFloatField with FillMissingOnly strategy
func TestMergeFloatField_FillMissingOnly(t *testing.T) {
	scraped := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "Scraped",
		RatingScore: 8.5,
	}

	nfo := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "NFO",
		RatingScore: 7.0,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, FillMissingOnly, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// FillMissingOnly should prefer NFO value when both have data
	assert.Equal(t, 7.0, result.Merged.RatingScore)
	assert.Equal(t, "nfo", result.Provenance["RatingScore"].Source)
}

// TestMergeBoolField_FillMissingOnly tests mergeBoolField with FillMissingOnly strategy
func TestMergeBoolField_FillMissingOnly(t *testing.T) {
	scraped := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "Scraped",
		ShouldCropPoster: true,
	}

	nfo := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "NFO",
		ShouldCropPoster: true, // Both true to test conflict resolution
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, FillMissingOnly, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// FillMissingOnly should prefer NFO value when both have data
	assert.True(t, result.Merged.ShouldCropPoster)
	assert.Equal(t, "nfo", result.Provenance["ShouldCropPoster"].Source)
}

// TestMergeFloatField_ScraperOnly tests mergeFloatField when NFO is empty
func TestMergeFloatField_ScraperOnly(t *testing.T) {
	scraped := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "Scraped",
		RatingScore: 8.5,
	}

	nfo := &models.Movie{
		ContentID: "IPX-123",
		ID:        "IPX-123",
		Title:     "NFO",
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should use scraper value when NFO is zero
	assert.Equal(t, 8.5, result.Merged.RatingScore)
	assert.Equal(t, "scraper", result.Provenance["RatingScore"].Source)
}

// TestMergeBoolField_ScraperOnly tests mergeBoolField when NFO is empty
func TestMergeBoolField_ScraperOnly(t *testing.T) {
	scraped := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "Scraped",
		ShouldCropPoster: true,
	}

	nfo := &models.Movie{
		ContentID: "IPX-123",
		ID:        "IPX-123",
		Title:     "NFO",
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should use scraper value when NFO is zero (false = empty)
	assert.True(t, result.Merged.ShouldCropPoster)
	assert.Equal(t, "scraper", result.Provenance["ShouldCropPoster"].Source)
}

// TestMergeFloatField_NFOOnly tests mergeFloatField when scraper is empty
func TestMergeFloatField_NFOOnly(t *testing.T) {
	scraped := &models.Movie{
		ContentID: "IPX-123",
		ID:        "IPX-123",
		Title:     "Scraped",
	}

	nfo := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "NFO",
		RatingScore: 7.0,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should use NFO value when scraper is zero
	assert.Equal(t, 7.0, result.Merged.RatingScore)
	assert.Equal(t, "nfo", result.Provenance["RatingScore"].Source)
}

// TestMergeBoolField_NFOOnly tests mergeBoolField when scraper is empty
func TestMergeBoolField_NFOOnly(t *testing.T) {
	scraped := &models.Movie{
		ContentID: "IPX-123",
		ID:        "IPX-123",
		Title:     "Scraped",
	}

	nfo := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "NFO",
		ShouldCropPoster: true,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should use NFO value when scraper is empty
	assert.True(t, result.Merged.ShouldCropPoster)
	assert.Equal(t, "nfo", result.Provenance["ShouldCropPoster"].Source)
}

// TestMergeFloatField_BothEmpty tests mergeFloatField when both are zero
func TestMergeFloatField_BothEmpty(t *testing.T) {
	scraped := &models.Movie{
		ContentID: "IPX-123",
		ID:        "IPX-123",
		Title:     "Scraped",
	}

	nfo := &models.Movie{
		ContentID: "IPX-123",
		ID:        "IPX-123",
		Title:     "NFO",
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Both zero values, should be empty
	assert.Equal(t, 0.0, result.Merged.RatingScore)
	assert.Equal(t, "empty", result.Provenance["RatingScore"].Source)
	assert.Greater(t, result.Stats.EmptyFields, 0)
}

// TestMergeBoolField_BothEmpty tests mergeBoolField when both are false
func TestMergeBoolField_BothEmpty(t *testing.T) {
	scraped := &models.Movie{
		ContentID: "IPX-123",
		ID:        "IPX-123",
		Title:     "Scraped",
	}

	nfo := &models.Movie{
		ContentID: "IPX-123",
		ID:        "IPX-123",
		Title:     "NFO",
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Both false values (empty), should be empty
	assert.Equal(t, false, result.Merged.ShouldCropPoster)
	assert.Equal(t, "empty", result.Provenance["ShouldCropPoster"].Source)
	assert.Greater(t, result.Stats.EmptyFields, 0)
}

// TestMergeFloatField_PreferScraper_EmptyNFO tests prefer-scraper with empty NFO
func TestMergeFloatField_PreferScraper_EmptyNFO(t *testing.T) {
	scraped := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "Scraped",
		RatingScore: 8.5,
	}

	nfo := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "NFO",
		RatingScore: 0, // Empty
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// PreferScraper uses scraper when NFO is empty
	assert.Equal(t, 8.5, result.Merged.RatingScore)
	assert.Equal(t, "scraper", result.Provenance["RatingScore"].Source)
}

// TestMergeBoolField_PreferScraper_EmptyNFO tests prefer-scraper with empty NFO
func TestMergeBoolField_PreferScraper_EmptyNFO(t *testing.T) {
	scraped := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "Scraped",
		ShouldCropPoster: true,
	}

	nfo := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "NFO",
		ShouldCropPoster: false, // Empty (false = empty for bool)
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// PreferScraper uses scraper when NFO is empty
	assert.True(t, result.Merged.ShouldCropPoster)
	assert.Equal(t, "scraper", result.Provenance["ShouldCropPoster"].Source)
}

// TestMergeFloatField_PreferNFO_EmptyScraper tests prefer-nfo with empty scraper
func TestMergeFloatField_PreferNFO_EmptyScraper(t *testing.T) {
	scraped := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "Scraped",
		RatingScore: 0, // Empty
	}

	nfo := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "NFO",
		RatingScore: 7.0,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// PreferNFO uses NFO when scraper is empty
	assert.Equal(t, 7.0, result.Merged.RatingScore)
	assert.Equal(t, "nfo", result.Provenance["RatingScore"].Source)
}

// TestMergeBoolField_PreferNFO_EmptyScraper tests prefer-nfo with empty scraper
func TestMergeBoolField_PreferNFO_EmptyScraper(t *testing.T) {
	scraped := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "Scraped",
		ShouldCropPoster: false, // Empty
	}

	nfo := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "NFO",
		ShouldCropPoster: true,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// PreferNFO uses NFO when scraper is empty
	assert.True(t, result.Merged.ShouldCropPoster)
	assert.Equal(t, "nfo", result.Provenance["ShouldCropPoster"].Source)
}

// TestMergeFloatField_StrictPreferScraper_BothHaveData tests strict prefer-scraper with conflicts
func TestMergeFloatField_StrictPreferScraper_BothHaveData(t *testing.T) {
	scraped := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "Scraped",
		RatingScore: 8.5,
	}

	nfo := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "NFO",
		RatingScore: 7.0,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Strict PreferScraper always uses scraper value, even when NFO also has data
	assert.Equal(t, 8.5, result.Merged.RatingScore)
	assert.Equal(t, "scraper", result.Provenance["RatingScore"].Source)
	assert.Greater(t, result.Stats.ConflictsResolved, 0)
}

// TestMergeBoolField_StrictPreferScraper_BothHaveData tests strict prefer-scraper with conflicts
func TestMergeBoolField_StrictPreferScraper_BothHaveData(t *testing.T) {
	scraped := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "Scraped",
		ShouldCropPoster: true,
	}

	nfo := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "NFO",
		ShouldCropPoster: true, // Both true to test conflict resolution (false = empty)
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Strict PreferScraper always uses scraper value
	assert.True(t, result.Merged.ShouldCropPoster)
	assert.Equal(t, "scraper", result.Provenance["ShouldCropPoster"].Source)
	assert.Greater(t, result.Stats.ConflictsResolved, 0)
}

// TestMergeFloatField_StrictPreferNFO_BothHaveData tests strict prefer-nfo with conflicts
func TestMergeFloatField_StrictPreferNFO_BothHaveData(t *testing.T) {
	scraped := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "Scraped",
		RatingScore: 8.5,
	}

	nfo := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "NFO",
		RatingScore: 7.0,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Strict PreferNFO always uses NFO value
	assert.Equal(t, 7.0, result.Merged.RatingScore)
	assert.Equal(t, "nfo", result.Provenance["RatingScore"].Source)
	assert.Greater(t, result.Stats.ConflictsResolved, 0)
}

// TestMergeBoolField_StrictPreferNFO_BothHaveData tests strict prefer-nfo with conflicts
func TestMergeBoolField_StrictPreferNFO_BothHaveData(t *testing.T) {
	scraped := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "Scraped",
		ShouldCropPoster: true,
	}

	nfo := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "NFO",
		ShouldCropPoster: true, // Both true to test conflict resolution (false = empty)
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Strict PreferNFO always uses NFO value
	assert.True(t, result.Merged.ShouldCropPoster)
	assert.Equal(t, "nfo", result.Provenance["ShouldCropPoster"].Source)
	assert.Greater(t, result.Stats.ConflictsResolved, 0)
}

// TestMergeFloatField_Timestamps tests provenance timestamps
func TestMergeFloatField_Timestamps(t *testing.T) {
	scrapedTS := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	nfoTS := time.Date(2024, 1, 10, 10, 0, 0, 0, time.UTC)

	scraped := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "Scraped",
		RatingScore: 8.5,
		UpdatedAt:   scrapedTS,
	}

	nfo := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "NFO",
		RatingScore: 7.0,
		UpdatedAt:   nfoTS,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check that provenance has LastUpdated timestamp
	assert.NotNil(t, result.Provenance["RatingScore"].LastUpdated)
	assert.Equal(t, scrapedTS, *result.Provenance["RatingScore"].LastUpdated)
}

// TestMergeBoolField_Timestamps tests provenance timestamps
func TestMergeBoolField_Timestamps(t *testing.T) {
	scrapedTS := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	scraped := &models.Movie{
		ContentID:        "IPX-123",
		ID:               "IPX-123",
		Title:            "Scraped",
		ShouldCropPoster: true,
		UpdatedAt:        scrapedTS,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nil, PreferScraper, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check that provenance has LastUpdated timestamp
	assert.NotNil(t, result.Provenance["ShouldCropPoster"].LastUpdated)
	assert.Equal(t, scrapedTS, *result.Provenance["ShouldCropPoster"].LastUpdated)
}
