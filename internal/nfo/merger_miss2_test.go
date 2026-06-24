package nfo

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mergeActressSlices: Phase 2 reverse matching (scraped JapaneseName -> NFO JapaneseName) ---

func TestMergeActressSlices_Miss2_ReverseMatchJapaneseToJapanese(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "田中麻里", FirstName: "", LastName: "", ThumbURL: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "田中麻里", FirstName: "NFOFirst", LastName: "NFOLast", ThumbURL: "https://example.com/thumb.jpg"},
	}

	// Phase 1: NFO's JapaneseName matches scraped's JapaneseName
	result := mergeActressSlices(scraped, nfo, false)
	assert.NotEmpty(t, result)
	for _, a := range result {
		if a.JapaneseName == "田中麻里" {
			// With preferNFO=false, NFO names fill in empty scraped fields
			assert.Equal(t, "NFOFirst", a.FirstName)
			assert.Equal(t, "https://example.com/thumb.jpg", a.ThumbURL)
			break
		}
	}
}

// --- mergeActressSlices: with preferNFO ---

func TestMergeActressSlices_Miss2_PreferNFO(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "田中麻里", FirstName: "Mari", LastName: "ScraperLast"},
	}
	nfo := []models.Actress{
		{JapaneseName: "田中麻里", FirstName: "NFOFirst", LastName: "NFOLast"},
	}

	result := mergeActressSlices(scraped, nfo, true)
	assert.NotEmpty(t, result)
	for _, a := range result {
		if a.JapaneseName == "田中麻里" {
			// With preferNFO, NFO name fields should overwrite
			assert.Equal(t, "NFOFirst", a.FirstName)
			assert.Equal(t, "NFOLast", a.LastName)
			break
		}
	}
}

// --- mergeActressSlices: ThumbURL fill from NFO ---

func TestMergeActressSlices_Miss2_ThumbURLFromNFO(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "田中麻里", ThumbURL: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "田中麻里", ThumbURL: "https://example.com/thumb.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.NotEmpty(t, result)
	// ThumbURL should be filled from NFO
	found := false
	for _, a := range result {
		if a.JapaneseName == "田中麻里" {
			found = true
			assert.Equal(t, "https://example.com/thumb.jpg", a.ThumbURL)
			break
		}
	}
	assert.True(t, found)
}

// --- mergeActressSlices: ThumbURL fill from NFO (same JapaneseName) ---

func TestMergeActressSlices_Miss2_ThumbURLFromNFOReverseMatch(t *testing.T) {
	scraped := []models.Actress{
		{JapaneseName: "田中麻里", ThumbURL: ""},
	}
	nfo := []models.Actress{
		{JapaneseName: "田中麻里", ThumbURL: "https://example.com/thumb2.jpg"},
	}

	result := mergeActressSlices(scraped, nfo, false)
	assert.NotEmpty(t, result)
	for _, a := range result {
		if a.JapaneseName == "田中麻里" {
			assert.Equal(t, "https://example.com/thumb2.jpg", a.ThumbURL)
			break
		}
	}
}

// --- mergeSlice: MergeArrays strategy with dedup ---

func TestMergeSlice_Miss2_MergeArraysWithDedup(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []string{"item1", "item2", "item3"}
	nfo := []string{"item2", "item3", "item4"}

	result := mergeSlice("test", scraped, nfo, MergeArrays, fm, func(s string) string {
		return s
	})

	assert.NotEmpty(t, result)
	assert.Equal(t, 1, stats.MergedArrays)
	// Should deduplicate
	seen := map[string]bool{}
	for _, item := range result {
		assert.False(t, seen[item], "duplicate found: %s", item)
		seen[item] = true
	}
	assert.Len(t, result, 4) // item1, item2, item3, item4
}

// --- mergeSlice: MergeArrays with empty dedupKey ---

func TestMergeSlice_Miss2_MergeArraysEmptyDedupKey(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []string{"item1"}
	nfo := []string{"item2"}

	result := mergeSlice("test", scraped, nfo, MergeArrays, fm, func(s string) string {
		return "" // Empty key = no dedup
	})

	assert.Len(t, result, 2)
}

// --- mergeSlice: PreferScraper strategy (default branch) ---

func TestMergeSlice_Miss2_PreferScraper(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []string{"item1"}
	nfo := []string{"item2"}

	result := mergeSlice("test", scraped, nfo, PreferScraper, fm, func(s string) string {
		return s
	})

	assert.Equal(t, []string{"item1"}, result)
	assert.Equal(t, 1, stats.FromScraper)
}

// --- mergeSlice: both empty returns nil ---

func TestMergeSlice_Miss2_BothEmpty(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeSlice("test", []string{}, []string{}, MergeArrays, fm, func(s string) string {
		return s
	})

	assert.Nil(t, result)
	assert.Equal(t, 1, stats.EmptyFields)
}

// --- MergeMovieMetadataWithOptions: critical field both empty ---

func TestMergeMovieMetadataWithOptions_Miss2_CriticalFieldBothEmpty(t *testing.T) {
	scraped := &models.Movie{ID: "", ContentID: "test", Title: "test"}
	nfo := &models.Movie{ID: "", ContentID: "nfo", Title: "nfo"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	require.NoError(t, err)
	require.NotNil(t, result)
	// ID was empty in both sources, should get the "[Unknown ID]" fallback
	assert.Contains(t, result.Merged.ID, "Unknown")
}

// --- MergeMovieMetadataWithOptions: critical field empty in scraper, filled in NFO ---

func TestMergeMovieMetadataWithOptions_Miss2_CriticalFieldEmptyInScraper(t *testing.T) {
	scraped := &models.Movie{ID: "", ContentID: "test", Title: "test"}
	nfo := &models.Movie{ID: "NFO-ID", ContentID: "nfo", Title: "nfo"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	require.NoError(t, err)
	require.NotNil(t, result)
	// Even with PreferScraper, critical field ID should fall back to NFO
	assert.Equal(t, "NFO-ID", result.Merged.ID)
}

// --- MergeMovieMetadataWithOptions: PreferScraper with empty scraper field ---

func TestMergeMovieMetadataWithOptions_Miss2_PreferScraperEmptyField(t *testing.T) {
	scraped := &models.Movie{ID: "S-001", ContentID: "test", Title: "", Maker: ""}
	nfo := &models.Movie{ID: "N-001", ContentID: "nfo", Title: "NFO Title", Maker: "NFO Maker"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	require.NoError(t, err)
	require.NotNil(t, result)
	// With PreferScraper, empty scraper fields should remain empty (strict mode)
	assert.Equal(t, "", result.Merged.Maker)
}

// --- MergeMovieMetadataWithOptions: PreferNFO with empty NFO field ---

func TestMergeMovieMetadataWithOptions_Miss2_PreferNFOEmptyField(t *testing.T) {
	scraped := &models.Movie{ID: "S-001", ContentID: "test", Title: "Scraper Title", Maker: "Scraper Maker"}
	nfo := &models.Movie{ID: "N-001", ContentID: "nfo", Title: "", Maker: ""}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, true)
	require.NoError(t, err)
	require.NotNil(t, result)
	// With PreferNFO, empty NFO fields should remain empty (strict mode)
	assert.Equal(t, "", result.Merged.Maker)
}

// --- MergeMovieMetadataWithOptions: MergeArrays strategy for scalar fields ---

func TestMergeMovieMetadataWithOptions_Miss2_MergeArraysScalar(t *testing.T) {
	scraped := &models.Movie{ID: "S-001", ContentID: "test", Title: "Scraper Title", Maker: "Scraper Maker"}
	nfo := &models.Movie{ID: "N-001", ContentID: "nfo", Title: "NFO Title", Maker: "NFO Maker"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, MergeArrays, true)
	require.NoError(t, err)
	require.NotNil(t, result)
	// MergeArrays for scalars should prefer scraper
	assert.Equal(t, "Scraper Title", result.Merged.Title)
}

// --- MergeMovieMetadataWithOptions: unknown strategy falls through to scraper ---

func TestMergeMovieMetadataWithOptions_Miss2_UnknownStrategy(t *testing.T) {
	scraped := &models.Movie{ID: "S-001", ContentID: "test", Title: "Scraper Title", Maker: "Scraper Maker"}
	nfo := &models.Movie{ID: "N-001", ContentID: "nfo", Title: "NFO Title", Maker: "NFO Maker"}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, MergeStrategy("unknown"), true)
	require.NoError(t, err)
	require.NotNil(t, result)
	// Unknown strategy should fall through to scraper
	assert.Equal(t, "Scraper Title", result.Merged.Title)
}

// --- MergeMovieMetadataWithOptions: timestamps ---

func TestMergeMovieMetadataWithOptions_Miss2_Timestamps(t *testing.T) {
	now := time.Now()
	scraped := &models.Movie{
		ID:        "S-001",
		ContentID: "test",
		Title:     "Title",
		CreatedAt: now.Add(-24 * time.Hour),
		UpdatedAt: now,
	}
	nfo := &models.Movie{
		ID:        "N-001",
		ContentID: "nfo",
		Title:     "NFO Title",
		CreatedAt: now.Add(-48 * time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	require.NoError(t, err)
	require.NotNil(t, result)
	// CreatedAt should use the older one
	assert.True(t, result.Merged.CreatedAt.Before(now.Add(-24*time.Hour)) || result.Merged.CreatedAt.Equal(now.Add(-24*time.Hour)))
}

// --- countNonEmptyFields: nil movie ---

func TestCountNonEmptyFields_Miss2_NilMovie(t *testing.T) {
	assert.Equal(t, 0, countNonEmptyFields(nil))
}

// --- makeProvenanceMap: nil movie ---

func TestMakeProvenanceMap_Miss2_NilMovie(t *testing.T) {
	result := makeProvenanceMap(nil, "scraper")
	assert.Empty(t, result)
}

// --- isFieldEmpty: various fields ---

func TestIsFieldEmpty_Miss2_VariousFields(t *testing.T) {
	movie := &models.Movie{
		ID:               "TEST-001",
		ContentID:        "test001",
		DisplayTitle:     "Display",
		Title:            "Title",
		OriginalTitle:    "Original",
		Description:      "Desc",
		ReleaseYear:      2024,
		Runtime:          120,
		Director:         "Director",
		Maker:            "Maker",
		Label:            "Label",
		Series:           "Series",
		RatingScore:      7.5,
		RatingVotes:      100,
		TrailerURL:       "https://example.com/trailer.mp4",
		OriginalFileName: "test.mp4",
		SourceName:       "r18dev",
		SourceURL:        "https://example.com",
		Poster:           models.PosterState{ShouldCropPoster: true},
	}

	// These should NOT be empty
	assert.False(t, isFieldEmptySpec("ID", movie))
	assert.False(t, isFieldEmptySpec("ContentID", movie))
	assert.False(t, isFieldEmptySpec("DisplayTitle", movie))
	assert.False(t, isFieldEmptySpec("Title", movie))
	assert.False(t, isFieldEmptySpec("ReleaseYear", movie))
	assert.False(t, isFieldEmptySpec("Runtime", movie))
	assert.False(t, isFieldEmptySpec("RatingScore", movie))
	assert.False(t, isFieldEmptySpec("RatingVotes", movie))
	assert.False(t, isFieldEmptySpec("ShouldCropPoster", movie)) // true → !true == false (not empty)

	// Default case (unknown field)
	assert.True(t, isFieldEmptySpec("UnknownField", movie))
}

// --- isFieldEmpty: poster fields ---

func TestIsFieldEmpty_Miss2_PosterFields(t *testing.T) {
	movie := &models.Movie{
		Poster: models.PosterState{
			PosterURL:                "https://example.com/poster.jpg",
			CoverURL:                 "https://example.com/cover.jpg",
			CroppedPosterURL:         "https://example.com/cropped.jpg",
			OriginalPosterURL:        "https://example.com/original.jpg",
			OriginalCroppedPosterURL: "https://example.com/original-cropped.jpg",
			OriginalShouldCropPoster: boolPtr(true),
		},
	}

	assert.False(t, isFieldEmptySpec("PosterURL", movie))
	assert.False(t, isFieldEmptySpec("CoverURL", movie))
	assert.False(t, isFieldEmptySpec("CroppedPosterURL", movie))
	assert.False(t, isFieldEmptySpec("OriginalPosterURL", movie))
	assert.False(t, isFieldEmptySpec("OriginalCroppedPosterURL", movie))
	assert.False(t, isFieldEmptySpec("OriginalShouldCropPoster", movie))
}

// --- isFieldEmpty: array fields ---

func TestIsFieldEmpty_Miss2_ArrayFields(t *testing.T) {
	movie := &models.Movie{
		Actresses:    []models.Actress{{FirstName: "Jane"}},
		Genres:       []models.Genre{{Name: "Action"}},
		Screenshots:  []string{"screenshot1.jpg"},
		Translations: []models.MovieTranslation{{Language: "en"}},
	}

	assert.False(t, isFieldEmptySpec("Actresses", movie))
	assert.False(t, isFieldEmptySpec("Genres", movie))
	assert.False(t, isFieldEmptySpec("Screenshots", movie))
	assert.False(t, isFieldEmptySpec("Translations", movie))
}

func boolPtr(b bool) *bool {
	return &b
}
