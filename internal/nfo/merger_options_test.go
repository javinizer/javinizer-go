package nfo

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMergeMovieMetadataWithOptions_AllCombinations tests all 4 strategy combinations
func TestMergeMovieMetadataWithOptions_AllCombinations(t *testing.T) {
	// Setup test data
	now := time.Now()

	scraped := &models.Movie{
		ID:          "IPX-123",
		Title:       "Scraped Title",
		Description: "Scraped Description",
		Maker:       "Scraped Studio",
		Label:       "Scraped Label",
		Series:      "Scraped Series",
		Runtime:     120,
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"},
			{FirstName: "Akari", LastName: "Asagiri"},
		},
		Genres: []models.Genre{
			{Name: "Drama"},
			{Name: "Romance"},
		},
		Screenshots: []string{"scraped1.jpg", "scraped2.jpg"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	nfoData := &models.Movie{
		ID:          "IPX-123",
		Title:       "NFO Title",
		Description: "NFO Description",
		Maker:       "NFO Studio",
		Label:       "NFO Label",
		Series:      "NFO Series",
		Runtime:     100,
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"},  // Same as scraped - will dedupe
			{FirstName: "Mika", LastName: "Sumire"}, // Unique to NFO
		},
		Genres: []models.Genre{
			{Name: "Drama"},  // Same as scraped - will dedupe
			{Name: "Comedy"}, // Unique to NFO
		},
		Screenshots: []string{"nfo1.jpg", "nfo3.jpg"},
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}

	tests := []struct {
		name            string
		scalarStrategy  MergeStrategy
		mergeArrays     bool
		expectedTitle   string
		expectedMaker   string
		actressCount    int
		genreCount      int
		screenshotCount int
		description     string
	}{
		{
			name:            "PreferNFO + Merge Arrays",
			scalarStrategy:  PreferNFO,
			mergeArrays:     true,
			expectedTitle:   "NFO Title",
			expectedMaker:   "NFO Studio",
			actressCount:    3, // Yui Hatano (dedupe), Mika Sumire (NFO), Akari Asagiri (scraped)
			genreCount:      3, // Drama (dedupe), Comedy (NFO), Romance (scraped)
			screenshotCount: 4, // All combined
			description:     "Keep NFO scalar fields, combine array fields",
		},
		{
			name:            "PreferNFO + Replace Arrays",
			scalarStrategy:  PreferNFO,
			mergeArrays:     false,
			expectedTitle:   "NFO Title",
			expectedMaker:   "NFO Studio",
			actressCount:    2, // Only NFO actresses
			genreCount:      2, // Only NFO genres
			screenshotCount: 2, // Only NFO screenshots
			description:     "Keep NFO scalar fields, use only NFO arrays",
		},
		{
			name:            "PreferScraper + Merge Arrays",
			scalarStrategy:  PreferScraper,
			mergeArrays:     true,
			expectedTitle:   "Scraped Title",
			expectedMaker:   "Scraped Studio",
			actressCount:    3, // Yui Hatano (dedupe), Akari Asagiri (scraped), Mika Sumire (NFO)
			genreCount:      3, // Drama (dedupe), Romance (scraped), Comedy (NFO)
			screenshotCount: 4, // All combined
			description:     "Use scraped scalar fields, combine array fields",
		},
		{
			name:            "PreferScraper + Replace Arrays",
			scalarStrategy:  PreferScraper,
			mergeArrays:     false,
			expectedTitle:   "Scraped Title",
			expectedMaker:   "Scraped Studio",
			actressCount:    2, // Only scraped actresses
			genreCount:      2, // Only scraped genres
			screenshotCount: 2, // Only scraped screenshots
			description:     "Use scraped scalar fields and arrays (full replace)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeMovieMetadataWithOptions(scraped, nfoData, tt.scalarStrategy, tt.mergeArrays)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, result.Merged)

			// Check scalar fields
			assert.Equal(t, tt.expectedTitle, result.Merged.Title, "Title should match expected for %s", tt.description)
			assert.Equal(t, tt.expectedMaker, result.Merged.Maker, "Maker should match expected for %s", tt.description)

			// Check array fields
			assert.Len(t, result.Merged.Actresses, tt.actressCount, "Actress count should match for %s", tt.description)
			assert.Len(t, result.Merged.Genres, tt.genreCount, "Genre count should match for %s", tt.description)
			assert.Len(t, result.Merged.Screenshots, tt.screenshotCount, "Screenshot count should match for %s", tt.description)

			// Verify stats make sense
			assert.Greater(t, result.Stats.TotalFields, 0, "Should have counted fields")
			if tt.scalarStrategy == PreferNFO {
				assert.Greater(t, result.Stats.FromNFO, 0, "Should have fields from NFO")
			} else {
				assert.Greater(t, result.Stats.FromScraper, 0, "Should have fields from scraper")
			}
		})
	}
}

// TestMergeMovieMetadataWithOptions_NilInputs tests edge cases with nil inputs
func TestMergeMovieMetadataWithOptions_NilInputs(t *testing.T) {
	movie := &models.Movie{
		ID:    "IPX-123",
		Title: "Test Movie",
	}

	tests := []struct {
		name        string
		scraped     *models.Movie
		nfo         *models.Movie
		expectError bool
		description string
	}{
		{
			name:        "Both nil",
			scraped:     nil,
			nfo:         nil,
			expectError: true,
			description: "Should error when both inputs are nil",
		},
		{
			name:        "Scraped only",
			scraped:     movie,
			nfo:         nil,
			expectError: false,
			description: "Should use scraped data when NFO is nil",
		},
		{
			name:        "NFO only",
			scraped:     nil,
			nfo:         movie,
			expectError: false,
			description: "Should use NFO data when scraped is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeMovieMetadataWithOptions(tt.scraped, tt.nfo, PreferNFO, true)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.NotNil(t, result, tt.description)
				assert.NotNil(t, result.Merged, tt.description)
			}
		})
	}
}

// TestMergeMovieMetadataWithOptions_EmptyFields tests handling of empty fields
func TestMergeMovieMetadataWithOptions_EmptyFields(t *testing.T) {
	scraped := &models.Movie{
		ID:          "IPX-123",
		Title:       "Scraped Title",
		Description: "", // Empty in scraped
		Maker:       "Scraped Studio",
	}

	nfoData := &models.Movie{
		ID:          "IPX-123",
		Title:       "", // Empty in NFO
		Description: "NFO Description",
		Maker:       "NFO Studio",
	}

	// Test PreferNFO strategy (strict mode: uses empty NFO values, no fallback)
	result, err := MergeMovieMetadataWithOptions(scraped, nfoData, PreferNFO, false)
	require.NoError(t, err)

	// With strict PreferNFO: empty NFO title is used (no fallback to scraped)
	// Note: Critical field protection only applies when the PREFERRED source is empty AND the other source is also empty
	// Here, NFO (preferred) is empty, but scraper is non-empty, so strict mode uses empty NFO value
	assert.Equal(t, "", result.Merged.Title, "Strict PreferNFO uses empty NFO value (no fallback to scraper)")
	// When preferring NFO, non-empty NFO description should be used
	assert.Equal(t, "NFO Description", result.Merged.Description, "Should use NFO when it's not empty")
	// When preferring NFO, non-empty NFO maker should be used
	assert.Equal(t, "NFO Studio", result.Merged.Maker, "Should use NFO when it's not empty")

	// Test PreferScraper strategy (strict mode: uses empty scraper values, no fallback)
	result2, err := MergeMovieMetadataWithOptions(scraped, nfoData, PreferScraper, false)
	require.NoError(t, err)

	// When preferring scraper, non-empty scraped title should be used
	assert.Equal(t, "Scraped Title", result2.Merged.Title, "Should use scraped when it's not empty")
	// With strict PreferScraper: empty scraped description is used (no fallback to NFO)
	assert.Equal(t, "", result2.Merged.Description, "Strict PreferScraper uses empty scraper value")
	// When preferring scraper, non-empty scraped maker should be used
	assert.Equal(t, "Scraped Studio", result2.Merged.Maker, "Should use scraped when it's not empty")
}

// TestMergeMovieMetadataWithOptions_ArrayDeduplication tests that merged arrays don't have duplicates
func TestMergeMovieMetadataWithOptions_ArrayDeduplication(t *testing.T) {
	scraped := &models.Movie{
		ID: "IPX-123",
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"},
			{FirstName: "Akari", LastName: "Asagiri"},
		},
		Genres: []models.Genre{
			{Name: "Drama"},
			{Name: "Romance"},
		},
	}

	nfoData := &models.Movie{
		ID: "IPX-123",
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"}, // Duplicate
			{FirstName: "Mika", LastName: "Sumire"},
		},
		Genres: []models.Genre{
			{Name: "Drama"}, // Duplicate
			{Name: "Comedy"},
		},
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfoData, PreferNFO, true)
	require.NoError(t, err)

	// Check that arrays are merged without duplicates
	assert.Len(t, result.Merged.Actresses, 3, "Should have 3 unique actresses (Yui Hatano appears once)")
	assert.Len(t, result.Merged.Genres, 3, "Should have 3 unique genres (Drama appears once)")

	// Verify the specific actresses
	actressNames := make(map[string]bool)
	for _, actress := range result.Merged.Actresses {
		key := actress.FirstName + " " + actress.LastName
		actressNames[key] = true
	}
	assert.True(t, actressNames["Yui Hatano"], "Should have Yui Hatano")
	assert.True(t, actressNames["Akari Asagiri"], "Should have Akari Asagiri")
	assert.True(t, actressNames["Mika Sumire"], "Should have Mika Sumire")

	// Verify the specific genres
	genreNames := make(map[string]bool)
	for _, genre := range result.Merged.Genres {
		genreNames[genre.Name] = true
	}
	assert.True(t, genreNames["Drama"], "Should have Drama")
	assert.True(t, genreNames["Romance"], "Should have Romance")
	assert.True(t, genreNames["Comedy"], "Should have Comedy")
}

// TestMergeMovieMetadataWithOptions_ActressDMMIDDeduplication tests that actresses are deduplicated by DMMID first
func TestMergeMovieMetadataWithOptions_ActressDMMIDDeduplication(t *testing.T) {
	scraped := &models.Movie{
		ID: "ABP-960",
		Actresses: []models.Actress{
			{FirstName: "Remu", LastName: "Suzumori", JapaneseName: "涼森れむ", DMMID: 1051912},
		},
	}

	nfoData := &models.Movie{
		ID: "ABP-960",
		Actresses: []models.Actress{
			{FirstName: "Suzumori", LastName: "Remu", JapaneseName: "涼森れむ", DMMID: 1051912}, // Same DMMID
		},
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfoData, PreferNFO, true)
	require.NoError(t, err)

	// Should deduplicate based on DMMID (most reliable)
	assert.Len(t, result.Merged.Actresses, 1, "Should have 1 unique actress (same DMMID)")
	assert.Equal(t, 1051912, result.Merged.Actresses[0].DMMID, "Should preserve DMMID")
}

// TestMergeMovieMetadataWithOptions_ActressJapaneseNameDeduplication tests that actresses with different name orders
// but same JapaneseName are properly deduplicated (when DMMID is not available)
func TestMergeMovieMetadataWithOptions_ActressJapaneseNameDeduplication(t *testing.T) {
	scraped := &models.Movie{
		ID: "TEST-001",
		Actresses: []models.Actress{
			{FirstName: "Remu", LastName: "Suzumori", JapaneseName: "涼森れむ"}, // No DMMID
		},
	}

	nfoData := &models.Movie{
		ID: "TEST-001",
		Actresses: []models.Actress{
			{FirstName: "Suzumori", LastName: "Remu", JapaneseName: "涼森れむ"}, // Same person, different name order, no DMMID
		},
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfoData, PreferNFO, true)
	require.NoError(t, err)

	// Should deduplicate based on JapaneseName (fallback when no DMMID)
	assert.Len(t, result.Merged.Actresses, 1, "Should have 1 unique actress (same JapaneseName)")
	assert.Equal(t, "涼森れむ", result.Merged.Actresses[0].JapaneseName, "Should preserve JapaneseName")
}

// TestMergeMovieMetadataWithOptions_ActressRomanizedNameDeduplication tests romanized name deduplication
// when neither DMMID nor JapaneseName are available
func TestMergeMovieMetadataWithOptions_ActressRomanizedNameDeduplication(t *testing.T) {
	scraped := &models.Movie{
		ID: "TEST-002",
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"}, // No DMMID, no JapaneseName
		},
	}

	nfoData := &models.Movie{
		ID: "TEST-002",
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"}, // Exact match
		},
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfoData, PreferNFO, true)
	require.NoError(t, err)

	// Should deduplicate based on romanized names (last resort)
	assert.Len(t, result.Merged.Actresses, 1, "Should have 1 unique actress (same romanized name)")
	assert.Equal(t, "Yui", result.Merged.Actresses[0].FirstName)
	assert.Equal(t, "Hatano", result.Merged.Actresses[0].LastName)
}

// TestParseScalarStrategy tests the scalar strategy parsing
func TestParseScalarStrategy(t *testing.T) {
	tests := []struct {
		input    string
		expected MergeStrategy
	}{
		{"prefer-scraper", PreferScraper},
		{"prefer-nfo", PreferNFO},
		{"PREFER-NFO", PreferNFO}, // Should handle case
		{"invalid", PreferNFO},    // Default to PreferNFO
		{"", PreferNFO},           // Default to PreferNFO
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseScalarStrategy(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseArrayStrategy tests the array strategy parsing
func TestParseArrayStrategy(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"merge", true},
		{"replace", false},
		{"MERGE", true},    // Should handle case
		{"REPLACE", false}, // Should handle case
		{"invalid", true},  // Default to merge (true)
		{"", true},         // Default to merge (true)
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseArrayStrategy(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
