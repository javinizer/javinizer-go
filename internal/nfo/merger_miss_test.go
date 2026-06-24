package nfo

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- merger.go miss lines: critical field both-empty fallback, PreferScraper
// strict mode with empty scraper, PreferNFO strict mode with empty NFO,
// unknown merge strategy, mergeScalarField conflicts, mergeActresses with
// MergeArrays, makeProvenanceMap with timestamps, countNonEmptyFields,
// isFieldEmpty for all fields ---

func TestMergeStringField_CriticalFieldBothEmptyFallback(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("ID", "", "", PreferNFO, fm)
	assert.Contains(t, result, "Unknown", "Critical field should use fallback when both empty")
	assert.Equal(t, 1, stats.EmptyFields)
}

func TestMergeStringField_CriticalFieldContentIDBothEmpty(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("ContentID", "", "", PreferScraper, fm)
	assert.Contains(t, result, "Unknown")
}

func TestMergeStringField_CriticalFieldTitleBothEmpty(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Title", "", "", MergeArrays, fm)
	assert.Contains(t, result, "Unknown")
}

func TestMergeStringField_CriticalFieldScraperEmptyNFOHasValue(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("ID", "", "NFO-123", PreferScraper, fm)
	assert.Equal(t, "NFO-123", result, "Critical field should fall back to NFO when scraper is empty")
	assert.Equal(t, 1, stats.FromNFO)
}

func TestMergeStringField_CriticalFieldScraperEmptyNFOHasValue_NonPreferNFO(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	// With MergeArrays strategy, critical field should still fall back to NFO
	result := mergeStringField("ContentID", "", "nfo-cid", MergeArrays, fm)
	assert.Equal(t, "nfo-cid", result)
}

func TestMergeStringField_PreferScraperStrictEmptyScraper(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	// Non-critical field: PreferScraper with empty scraper should use empty string
	result := mergeStringField("Maker", "", "NFO-Maker", PreferScraper, fm)
	assert.Equal(t, "", result, "PreferScraper strict mode should use empty scraper value")
	assert.Equal(t, 1, stats.FromScraper)
}

func TestMergeStringField_PreferNFOStrictEmptyNFO(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	// Non-critical field: PreferNFO with empty NFO should use empty string
	result := mergeStringField("Maker", "Scraper-Maker", "", PreferNFO, fm)
	assert.Equal(t, "", result, "PreferNFO strict mode should use empty NFO value")
	assert.Equal(t, 1, stats.FromNFO)
}

func TestMergeStringField_BothEmpty(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Maker", "", "", PreferNFO, fm)
	assert.Equal(t, "", result)
	assert.Equal(t, 1, stats.EmptyFields)
}

func TestMergeStringField_ScraperEmptyFallsBackToNFO(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Label", "", "NFO-Label", MergeArrays, fm)
	assert.Equal(t, "NFO-Label", result)
	assert.Equal(t, 1, stats.FromNFO)
}

func TestMergeStringField_NFOEmptyUsesScraper(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Label", "Scraper-Label", "", MergeArrays, fm)
	assert.Equal(t, "Scraper-Label", result)
	assert.Equal(t, 1, stats.FromScraper)
}

func TestMergeStringField_BothHaveDataPreserveExisting(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Director", "S-Director", "N-Director", PreserveExisting, fm)
	assert.Equal(t, "N-Director", result, "PreserveExisting should prefer NFO value")
	assert.Equal(t, 1, stats.FromNFO)
	assert.Equal(t, 1, stats.ConflictsResolved)
}

func TestMergeStringField_BothHaveDataFillMissingOnly(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Director", "S-Director", "N-Director", FillMissingOnly, fm)
	assert.Equal(t, "N-Director", result, "FillMissingOnly should prefer NFO value when both have data")
	assert.Equal(t, 1, stats.FromNFO)
}

func TestMergeStringField_BothHaveDataMergeArraysFallsBackToScraper(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Director", "S-Director", "N-Director", MergeArrays, fm)
	assert.Equal(t, "S-Director", result, "MergeArrays falls back to PreferScraper for strings")
	assert.Equal(t, 1, stats.FromScraper)
}

func TestMergeStringField_UnknownStrategy(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeStringField("Director", "S-Dir", "N-Dir", MergeStrategy("unknown-strategy"), fm)
	assert.Equal(t, "S-Dir", result, "Unknown strategy should default to scraper value")
	assert.Equal(t, 1, stats.FromScraper)
}

func TestMergeScalarField_BothEmpty(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 0, 0, PreferNFO, fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 0, result)
	assert.Equal(t, 1, stats.EmptyFields)
}

func TestMergeScalarField_ScraperEmpty(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 0, 120, PreferNFO, fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 120, result)
	assert.Equal(t, 1, stats.FromNFO)
}

func TestMergeScalarField_NFOEmpty(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 90, 0, PreferNFO, fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 90, result)
	assert.Equal(t, 1, stats.FromScraper)
}

func TestMergeScalarField_ConflictPreferNFO(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 90, 120, PreferNFO, fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 120, result, "PreferNFO should use NFO value on conflict")
	assert.Equal(t, 1, stats.ConflictsResolved)
	assert.Equal(t, 1, stats.FromNFO)
}

func TestMergeScalarField_ConflictPreferScraper(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 90, 120, PreferScraper, fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 90, result)
	assert.Equal(t, 1, stats.FromScraper)
}

func TestMergeScalarField_ConflictMergeArrays(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 90, 120, MergeArrays, fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 90, result, "MergeArrays should prefer scraper for scalars")
}

func TestMergeScalarField_ConflictPreserveExisting(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 90, 120, PreserveExisting, fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 120, result, "PreserveExisting should prefer NFO for scalars")
}

func TestMergeScalarField_ConflictFillMissingOnly(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 90, 120, FillMissingOnly, fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 120, result, "FillMissingOnly should prefer NFO when both have data")
}

func TestMergeScalarField_ConflictUnknownStrategy(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("Runtime", 90, 120, MergeStrategy("weird"), fm, func(v int) bool { return v == 0 })
	assert.Equal(t, 90, result, "Unknown strategy should default to scraper")
}

func TestMergeScalarField_FloatConflict(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("RatingScore", 7.5, 8.0, PreferNFO, fm, func(v float64) bool { return v == 0 })
	assert.Equal(t, 8.0, result)
}

func TestMergeScalarField_BoolConflict(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	result := mergeScalarField("ShouldCropPoster", true, false, PreferNFO, fm, func(v bool) bool { return false })
	assert.False(t, result, "PreferNFO should use NFO value for bool conflict")
}

func TestMergeScalarField_TimePtrConflict(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)
	earlier := now.Add(-24 * time.Hour)

	result := mergeScalarField("ReleaseDate", &now, &earlier, PreferNFO, fm, func(v *time.Time) bool { return v == nil || v.IsZero() })
	assert.Equal(t, earlier, *result, "PreferNFO should use NFO value for time pointer conflict")
}

func TestMergeScalarField_TimePtrNilScraper(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)
	earlier := now.Add(-24 * time.Hour)

	result := mergeScalarField("ReleaseDate", nil, &earlier, PreferScraper, fm, func(v *time.Time) bool { return v == nil || v.IsZero() })
	assert.Equal(t, earlier, *result, "Should fall back to NFO when scraper time is nil")
}

func TestMergeActresses_MergeArrays(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []models.Actress{
		{JapaneseName: "actress1", DMMID: 10},
		{JapaneseName: "actress2", DMMID: 20},
	}
	nfo := []models.Actress{
		{JapaneseName: "actress1", FirstName: "NFirst"},
		{JapaneseName: "actress3"},
	}

	result := mergeActresses("Actresses", scraped, nfo, MergeArrays, fm)
	assert.GreaterOrEqual(t, len(result), 2, "MergeArrays should merge and deduplicate actresses")
	assert.Equal(t, 1, stats.MergedArrays)
}

func TestMergeActresses_PreferScraper(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []models.Actress{{JapaneseName: "A", DMMID: 1}}
	nfo := []models.Actress{{JapaneseName: "A", FirstName: "NFirst"}}

	result := mergeActresses("Actresses", scraped, nfo, PreferScraper, fm)
	require.Len(t, result, 1)
	assert.Equal(t, 1, result[0].DMMID)
	assert.Equal(t, 1, stats.FromScraper)
	assert.Equal(t, 1, stats.ConflictsResolved)
}

func TestMergeActresses_PreserveExisting(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []models.Actress{{JapaneseName: "A", DMMID: 1}}
	nfo := []models.Actress{{JapaneseName: "A", FirstName: "NFirst"}}

	result := mergeActresses("Actresses", scraped, nfo, PreserveExisting, fm)
	require.Len(t, result, 1)
	assert.Equal(t, "NFirst", result[0].FirstName, "PreserveExisting should prefer NFO actress names")
	assert.Equal(t, 1, result[0].DMMID, "DMMID should always be preserved from scraped")
}

func TestMergeActresses_FillMissingOnly(t *testing.T) {
	stats := &MergeStats{}
	provenance := make(map[string]DataSource)
	now := time.Now()
	fm := newFieldMerger(stats, provenance, now, now)

	scraped := []models.Actress{{JapaneseName: "A", DMMID: 1}}
	nfo := []models.Actress{{JapaneseName: "A", FirstName: "NFirst"}}

	result := mergeActresses("Actresses", scraped, nfo, FillMissingOnly, fm)
	require.Len(t, result, 1)
	assert.Equal(t, "NFirst", result[0].FirstName)
}

func TestMergeMovieMetadataWithOptions_FullMergeWithTimestamps(t *testing.T) {
	pastTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	futureTime := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	scraped := &models.Movie{
		ID:          "IPX-001",
		ContentID:   "sc-cid",
		Title:       "Scraper Title",
		Maker:       "SMaker",
		ReleaseYear: 2023,
		Runtime:     90,
		UpdatedAt:   pastTime,
		CreatedAt:   pastTime,
	}
	nfo := &models.Movie{
		ID:          "IPX-001",
		ContentID:   "nfo-cid",
		Title:       "NFO Title",
		Maker:       "NMaker",
		ReleaseYear: 2024,
		Runtime:     120,
		UpdatedAt:   futureTime,
		CreatedAt:   futureTime,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, true)
	require.NoError(t, err)
	assert.Equal(t, "Scraper Title", result.Merged.Title)
	assert.Equal(t, "SMaker", result.Merged.Maker)
	assert.Equal(t, 90, result.Merged.Runtime)
	assert.Equal(t, 2023, result.Merged.ReleaseYear)

	// CreatedAt should use the newer one (NFO)
	assert.Equal(t, futureTime, result.Merged.CreatedAt)

	// UpdatedAt should be set to now
	assert.False(t, result.Merged.UpdatedAt.IsZero())
}

func TestMergeMovieMetadataWithOptions_WithZeroTimestamps(t *testing.T) {
	scraped := &models.Movie{
		ID:        "IPX-001",
		Title:     "Test",
		UpdatedAt: time.Time{}, // Zero
		CreatedAt: time.Time{}, // Zero
	}
	nfo := &models.Movie{
		ID:        "IPX-001",
		Title:     "NFO Test",
		UpdatedAt: time.Time{},
		CreatedAt: time.Time{},
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestMakeProvenanceMap_NilMovie(t *testing.T) {
	result := makeProvenanceMap(nil, "scraper")
	assert.Empty(t, result)
}

func TestMakeProvenanceMap_WithTimestamps(t *testing.T) {
	now := time.Now()
	movie := &models.Movie{
		ID:        "IPX-001",
		Title:     "Test",
		UpdatedAt: now,
	}

	result := makeProvenanceMap(movie, "scraper")
	assert.Contains(t, result, "ID")
	assert.Contains(t, result, "Title")
	assert.Equal(t, "scraper", result["ID"].Source)
	require.NotNil(t, result["ID"].LastUpdated)
	assert.Equal(t, now, *result["ID"].LastUpdated)
}

func TestMakeProvenanceMap_WithCreatedAtFallback(t *testing.T) {
	created := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:        "IPX-001",
		Title:     "Test",
		UpdatedAt: time.Time{}, // Zero → fallback to CreatedAt
		CreatedAt: created,
	}

	result := makeProvenanceMap(movie, "nfo")
	assert.Contains(t, result, "ID")
	require.NotNil(t, result["ID"].LastUpdated)
	assert.Equal(t, created, *result["ID"].LastUpdated)
}

func TestCountNonEmptyFields_Nil(t *testing.T) {
	assert.Equal(t, 0, countNonEmptyFields(nil))
}

func TestCountNonEmptyFields_WithFields(t *testing.T) {
	movie := &models.Movie{
		ID:    "IPX-001",
		Title: "Test",
	}
	count := countNonEmptyFields(movie)
	assert.GreaterOrEqual(t, count, 2)
}

func TestIsFieldEmpty_AllFields(t *testing.T) {
	movie := &models.Movie{
		ID:               "IPX-001",
		ContentID:        "cid",
		DisplayTitle:     "Display",
		Title:            "Title",
		OriginalTitle:    "OrigTitle",
		Description:      "Desc",
		ReleaseYear:      2023,
		Runtime:          90,
		Director:         "Dir",
		Maker:            "Maker",
		Label:            "Label",
		Series:           "Series",
		RatingScore:      7.5,
		RatingVotes:      100,
		TrailerURL:       "http://trailer",
		OriginalFileName: "file.mp4",
		SourceName:       "r18dev",
		SourceURL:        "http://source",
		Poster:           models.PosterState{ShouldCropPoster: true},
		Actresses:        []models.Actress{{JapaneseName: "A"}},
		Genres:           []models.Genre{{Name: "Action"}},
		Screenshots:      []string{"http://ss.jpg"},
		Translations:     []models.MovieTranslation{{Language: "en"}},
	}

	assert.False(t, isFieldEmptySpec("ID", movie))
	assert.False(t, isFieldEmptySpec("ContentID", movie))
	assert.False(t, isFieldEmptySpec("DisplayTitle", movie))
	assert.False(t, isFieldEmptySpec("Title", movie))
	assert.False(t, isFieldEmptySpec("OriginalTitle", movie))
	assert.False(t, isFieldEmptySpec("Description", movie))
	assert.False(t, isFieldEmptySpec("ReleaseYear", movie))
	assert.False(t, isFieldEmptySpec("Runtime", movie))
	assert.False(t, isFieldEmptySpec("Director", movie))
	assert.False(t, isFieldEmptySpec("Maker", movie))
	assert.False(t, isFieldEmptySpec("Label", movie))
	assert.False(t, isFieldEmptySpec("Series", movie))
	assert.False(t, isFieldEmptySpec("RatingScore", movie))
	assert.False(t, isFieldEmptySpec("RatingVotes", movie))
	assert.False(t, isFieldEmptySpec("TrailerURL", movie))
	assert.False(t, isFieldEmptySpec("OriginalFileName", movie))
	assert.False(t, isFieldEmptySpec("Actresses", movie))
	assert.False(t, isFieldEmptySpec("Genres", movie))
	assert.False(t, isFieldEmptySpec("Screenshots", movie))
	assert.False(t, isFieldEmptySpec("Translations", movie))
	assert.False(t, isFieldEmptySpec("SourceName", movie))
	assert.False(t, isFieldEmptySpec("SourceURL", movie))
	// ShouldCropPoster=true is a meaningful (non-empty) value: !true == false
	assert.False(t, isFieldEmptySpec("ShouldCropPoster", movie))
	// Unknown field should be empty
	assert.True(t, isFieldEmptySpec("UnknownField", movie))
}

func TestIsFieldEmpty_PosterFields(t *testing.T) {
	movie := &models.Movie{
		Poster: models.PosterState{
			PosterURL:                "http://poster.jpg",
			CoverURL:                 "http://cover.jpg",
			CroppedPosterURL:         "http://crop.jpg",
			OriginalPosterURL:        "http://orig-poster.jpg",
			OriginalCroppedPosterURL: "http://orig-crop.jpg",
		},
	}

	assert.False(t, isFieldEmptySpec("PosterURL", movie))
	assert.False(t, isFieldEmptySpec("CoverURL", movie))
	assert.False(t, isFieldEmptySpec("CroppedPosterURL", movie))
	assert.False(t, isFieldEmptySpec("OriginalPosterURL", movie))
	assert.False(t, isFieldEmptySpec("OriginalCroppedPosterURL", movie))
}

func TestIsFieldEmpty_ReleaseDate(t *testing.T) {
	now := time.Now()
	movie := &models.Movie{ReleaseDate: &now}
	assert.False(t, isFieldEmptySpec("ReleaseDate", movie))

	movieNil := &models.Movie{ReleaseDate: nil}
	assert.True(t, isFieldEmptySpec("ReleaseDate", movieNil))
}

func TestIsFieldEmpty_EmptySlices(t *testing.T) {
	movie := &models.Movie{}
	assert.True(t, isFieldEmptySpec("Actresses", movie))
	assert.True(t, isFieldEmptySpec("Genres", movie))
	assert.True(t, isFieldEmptySpec("Screenshots", movie))
	assert.True(t, isFieldEmptySpec("Translations", movie))
}

func TestIsFieldEmpty_OriginalShouldCropPoster(t *testing.T) {
	t.Run("nil pointer is empty", func(t *testing.T) {
		movie := &models.Movie{}
		assert.True(t, isFieldEmptySpec("OriginalShouldCropPoster", movie))
	})

	t.Run("false pointer is empty", func(t *testing.T) {
		val := false
		movie := &models.Movie{Poster: models.PosterState{OriginalShouldCropPoster: &val}}
		assert.True(t, isFieldEmptySpec("OriginalShouldCropPoster", movie))
	})

	t.Run("true pointer is not empty", func(t *testing.T) {
		val := true
		movie := &models.Movie{Poster: models.PosterState{OriginalShouldCropPoster: &val}}
		assert.False(t, isFieldEmptySpec("OriginalShouldCropPoster", movie))
	})
}

func TestMergeMovieMetadataWithOptions_CroppedPosterURLAlwaysFromScraper(t *testing.T) {
	now := time.Now()
	scraped := &models.Movie{
		ID: "IPX-001", Title: "T",
		Poster:    models.PosterState{CroppedPosterURL: "http://sc-crop.jpg"},
		UpdatedAt: now,
	}
	nfo := &models.Movie{
		ID: "IPX-001", Title: "T",
		Poster:    models.PosterState{CroppedPosterURL: "http://nfo-crop.jpg"},
		UpdatedAt: now,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	require.NoError(t, err)
	assert.Equal(t, "http://sc-crop.jpg", result.Merged.Poster.CroppedPosterURL,
		"CroppedPosterURL should always come from scraper (not stored in NFO)")
}
