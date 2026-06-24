package nfo

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestMergeMovieMetadataWithOptions_V5_AllStrategiesSliceMerge(t *testing.T) {
	scraped := &models.Movie{
		ID:          "ABC-123",
		ContentID:   "abc123",
		Title:       "Scraped Title",
		Description: "Scraped desc",
		Maker:       "Scraped Maker",
		Actresses: []models.Actress{
			{JapaneseName: "Actress1", DMMID: 100},
		},
		Genres: []models.Genre{
			{Name: "Genre1"},
		},
		Screenshots: []string{"http://shot1.jpg"},
	}

	nfo := &models.Movie{
		ID:          "ABC-123",
		ContentID:   "abc123",
		Title:       "NFO Title",
		Description: "NFO desc",
		Maker:       "NFO Maker",
		Actresses: []models.Actress{
			{JapaneseName: "Actress2"},
		},
		Genres: []models.Genre{
			{Name: "Genre2"},
		},
		Screenshots: []string{"http://shot2.jpg"},
	}

	for _, strategy := range []MergeStrategy{PreferScraper, PreferNFO, MergeArrays, PreserveExisting, FillMissingOnly} {
		t.Run(strategy.String(), func(t *testing.T) {
			result, err := MergeMovieMetadataWithOptions(scraped, nfo, strategy, true)
			if err != nil {
				t.Fatalf("merge failed: %v", err)
			}
			if result.Merged == nil {
				t.Fatal("merged should not be nil")
			}
			// Critical fields should never be empty
			if result.Merged.ID == "" {
				t.Error("ID should not be empty")
			}
			if result.Merged.Title == "" {
				t.Error("Title should not be empty")
			}
		})
	}
}

func TestMergeMovieMetadataWithOptions_V5_MergeArraysDeduplication(t *testing.T) {
	scraped := &models.Movie{
		ID:    "ABC-123",
		Title: "Test",
		Genres: []models.Genre{
			{Name: "Action"},
			{Name: "Drama"},
		},
		Screenshots: []string{"http://shot1.jpg", "http://shot2.jpg"},
	}

	nfo := &models.Movie{
		ID:    "ABC-123",
		Title: "Test",
		Genres: []models.Genre{
			{Name: "action"}, // same name, different case
			{Name: "Comedy"},
		},
		Screenshots: []string{"http://shot1.jpg/", "http://shot3.jpg"}, // shot1 with trailing slash should dedup
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, MergeArrays, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// Genres should be deduplicated (Action == action)
	genreNames := make(map[string]bool)
	for _, g := range result.Merged.Genres {
		genreNames[g.Name] = true
	}
	if len(result.Merged.Genres) != 3 {
		t.Errorf("expected 3 genres after dedup, got %d: %v", len(result.Merged.Genres), result.Merged.Genres)
	}

	// Screenshots should dedup by normalized URL
	if len(result.Merged.Screenshots) != 3 {
		t.Errorf("expected 3 screenshots after dedup, got %d", len(result.Merged.Screenshots))
	}
}

func TestMergeMovieMetadataWithOptions_V5_ScalarFieldMergeArraysStrategy(t *testing.T) {
	scraped := &models.Movie{
		ID:          "ABC-123",
		Title:       "Scraped",
		ReleaseYear: 2024,
		Runtime:     120,
	}

	nfo := &models.Movie{
		ID:          "ABC-123",
		Title:       "NFO",
		ReleaseYear: 2023,
		Runtime:     90,
	}

	// MergeArrays for scalar fields should prefer scraper
	result, err := MergeMovieMetadataWithOptions(scraped, nfo, MergeArrays, false)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if result.Merged.ReleaseYear != 2024 {
		t.Errorf("expected ReleaseYear=2024 (scraper), got %d", result.Merged.ReleaseYear)
	}
}

func TestMergeMovieMetadataWithOptions_V5_PreserveExistingStrategy(t *testing.T) {
	scraped := &models.Movie{
		ID:          "ABC-123",
		Title:       "Scraped Title",
		Description: "Scraped desc",
		Maker:       "Scraped Maker",
	}

	nfo := &models.Movie{
		ID:          "ABC-123",
		Title:       "NFO Title",
		Description: "NFO desc",
		Maker:       "NFO Maker",
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreserveExisting, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// PreserveExisting should always prefer NFO when both have data
	if result.Merged.Title != "NFO Title" {
		t.Errorf("expected NFO Title, got %q", result.Merged.Title)
	}
	if result.Merged.Description != "NFO desc" {
		t.Errorf("expected NFO desc, got %q", result.Merged.Description)
	}
	if result.Merged.Maker != "NFO Maker" {
		t.Errorf("expected NFO Maker, got %q", result.Merged.Maker)
	}
}

func TestMergeMovieMetadataWithOptions_V5_FillMissingOnlyStrategy(t *testing.T) {
	scraped := &models.Movie{
		ID:          "ABC-123",
		Title:       "Scraped Title",
		Description: "", // empty - should be filled from NFO
		Maker:       "Scraped Maker",
	}

	nfo := &models.Movie{
		ID:          "ABC-123",
		Title:       "NFO Title",
		Description: "NFO desc",
		Maker:       "NFO Maker",
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, FillMissingOnly, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// FillMissingOnly should prefer NFO when both have data (like PreserveExisting)
	if result.Merged.Title != "NFO Title" {
		t.Errorf("expected NFO Title, got %q", result.Merged.Title)
	}
	// Empty scraped field should fall back to NFO
	if result.Merged.Description != "NFO desc" {
		t.Errorf("expected NFO desc to fill empty, got %q", result.Merged.Description)
	}
}

func TestMergeMovieMetadataWithOptions_V5_PreferScraperStrict(t *testing.T) {
	scraped := &models.Movie{
		ID:    "ABC-123",
		Title: "Scraped Title",
		// Description is empty - PreferScraper should use empty value
	}

	nfo := &models.Movie{
		ID:          "ABC-123",
		Title:       "NFO Title",
		Description: "NFO desc",
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// PreferScraper should use scraper value even when empty
	if result.Merged.Description != "" {
		t.Errorf("PreferScraper should use empty scraper value, got %q", result.Merged.Description)
	}
}

func TestMergeMovieMetadataWithOptions_V5_PreferNFOStrict(t *testing.T) {
	scraped := &models.Movie{
		ID:          "ABC-123",
		Title:       "Scraped Title",
		Description: "Scraped desc",
	}

	nfo := &models.Movie{
		ID:    "ABC-123",
		Title: "NFO Title",
		// Description is empty - PreferNFO should use empty value
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// PreferNFO should use NFO value even when empty
	if result.Merged.Description != "" {
		t.Errorf("PreferNFO should use empty NFO value, got %q", result.Merged.Description)
	}
}

func TestMergeMovieMetadataWithOptions_V5_CriticalFieldFallback(t *testing.T) {
	scraped := &models.Movie{
		// ID and Title empty - critical field
	}

	nfo := &models.Movie{
		ID:    "ABC-123",
		Title: "NFO Title",
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// Critical field should fall back to NFO even with PreferScraper
	if result.Merged.ID != "ABC-123" {
		t.Errorf("critical field ID should fall back to NFO, got %q", result.Merged.ID)
	}
	if result.Merged.Title != "NFO Title" {
		t.Errorf("critical field Title should fall back to NFO, got %q", result.Merged.Title)
	}
}

func TestMergeMovieMetadataWithOptions_V5_BothCriticalFieldsEmpty(t *testing.T) {
	scraped := &models.Movie{}
	nfo := &models.Movie{}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// Both empty - should use fallback "[Unknown ...]"
	if result.Merged.ID != "[Unknown ID]" {
		t.Errorf("expected [Unknown ID] fallback, got %q", result.Merged.ID)
	}
	if result.Merged.Title != "[Unknown Title]" {
		t.Errorf("expected [Unknown Title] fallback, got %q", result.Merged.Title)
	}
}

func TestMergeMovieMetadataWithOptions_V5_ActressCrossSourceMatching(t *testing.T) {
	scraped := &models.Movie{
		ID:    "ABC-123",
		Title: "Test",
		Actresses: []models.Actress{
			{JapaneseName: "田中", DMMID: 100, ThumbURL: "http://thumb.jpg"},
		},
	}

	nfo := &models.Movie{
		ID:    "ABC-123",
		Title: "Test",
		Actresses: []models.Actress{
			{JapaneseName: "田中", FirstName: "Tanaka"}, // same JapaneseName
		},
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, MergeArrays, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// Should dedup by JapaneseName match
	if len(result.Merged.Actresses) != 1 {
		t.Errorf("expected 1 actress after dedup, got %d", len(result.Merged.Actresses))
	}
	// DMMID should be preserved from scraper
	if result.Merged.Actresses[0].DMMID != 100 {
		t.Errorf("expected DMMID=100, got %d", result.Merged.Actresses[0].DMMID)
	}
}

func TestMergeMovieMetadataWithOptions_V5_Timestamps(t *testing.T) {
	now := time.Now()
	past := now.Add(-24 * time.Hour)

	scraped := &models.Movie{
		ID:        "ABC-123",
		Title:     "Scraped",
		CreatedAt: past,
		UpdatedAt: now,
	}

	nfo := &models.Movie{
		ID:        "ABC-123",
		Title:     "NFO",
		CreatedAt: now,
		UpdatedAt: past,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// CreatedAt should use the more recent (nfo.Now) timestamp per the merge logic
	// which prefers nfo.CreatedAt when it's after scraped.CreatedAt
	if !result.Merged.CreatedAt.Equal(now) {
		t.Errorf("expected CreatedAt=%v, got %v", now, result.Merged.CreatedAt)
	}
	// UpdatedAt should be set to now
	if result.Merged.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}

func TestMergeMovieMetadataWithOptions_V5_PosterFields(t *testing.T) {
	scraped := &models.Movie{
		ID:    "ABC-123",
		Title: "Test",
		Poster: models.PosterState{
			PosterURL:        "http://scraped-poster.jpg",
			CoverURL:         "http://scraped-cover.jpg",
			CroppedPosterURL: "http://scraped-cropped.jpg",
		},
	}

	nfo := &models.Movie{
		ID:    "ABC-123",
		Title: "Test",
		Poster: models.PosterState{
			PosterURL: "http://nfo-poster.jpg",
			CoverURL:  "http://nfo-cover.jpg",
		},
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// CroppedPosterURL should always use scraped value (not stored in NFO)
	if result.Merged.Poster.CroppedPosterURL != "http://scraped-cropped.jpg" {
		t.Errorf("CroppedPosterURL should use scraped, got %q", result.Merged.Poster.CroppedPosterURL)
	}
}

func TestApplyPreset_V5_InvalidPreset(t *testing.T) {
	_, _, err := ApplyPreset("invalid-preset", "prefer-nfo", "merge")
	if err == nil {
		t.Error("expected error for invalid preset")
	}
}

func TestApplyPreset_V5_EmptyPreset(t *testing.T) {
	s, a, err := ApplyPreset("", "prefer-scraper", "replace")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "prefer-scraper" {
		t.Errorf("expected prefer-scraper, got %q", s)
	}
	if a != "replace" {
		t.Errorf("expected replace, got %q", a)
	}
}

func TestApplyPreset_V5_ConservativePreset(t *testing.T) {
	s, a, err := ApplyPreset("conservative", "prefer-scraper", "replace")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "preserve-existing" {
		t.Errorf("expected preserve-existing, got %q", s)
	}
	if a != "merge" {
		t.Errorf("expected merge, got %q", a)
	}
}

func TestApplyPreset_V5_GapFillPreset(t *testing.T) {
	s, a, err := ApplyPreset("gap-fill", "prefer-scraper", "replace")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "fill-missing-only" {
		t.Errorf("expected fill-missing-only, got %q", s)
	}
	if a != "merge" {
		t.Errorf("expected merge, got %q", a)
	}
}

func TestApplyPreset_V5_AggressivePreset(t *testing.T) {
	s, a, err := ApplyPreset("aggressive", "prefer-nfo", "merge")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "prefer-scraper" {
		t.Errorf("expected prefer-scraper, got %q", s)
	}
	if a != "replace" {
		t.Errorf("expected replace, got %q", a)
	}
}

func TestParseArrayStrategy_V5_InvalidStrategy(t *testing.T) {
	_, err := ParseArrayStrategy("invalid")
	if err == nil {
		t.Error("expected error for invalid array strategy")
	}
}

func TestMergeMovieMetadataWithOptions_V5_ReleaseDateScalar(t *testing.T) {
	date1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)

	scraped := &models.Movie{
		ID:          "ABC-123",
		Title:       "Test",
		ReleaseDate: &date1,
		Runtime:     120,
	}

	nfo := &models.Movie{
		ID:          "ABC-123",
		Title:       "Test",
		ReleaseDate: &date2,
		Runtime:     90,
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if result.Merged.ReleaseDate == nil || !result.Merged.ReleaseDate.Equal(date1) {
		t.Errorf("expected scraped date, got %v", result.Merged.ReleaseDate)
	}
	if result.Merged.Runtime != 120 {
		t.Errorf("expected scraped runtime, got %d", result.Merged.Runtime)
	}
}

func TestMergeMovieMetadataWithOptions_V5_BooleanField(t *testing.T) {
	scraped := &models.Movie{
		ID:    "ABC-123",
		Title: "Test",
		Poster: models.PosterState{
			ShouldCropPoster: true,
		},
	}

	nfo := &models.Movie{
		ID:    "ABC-123",
		Title: "Test",
		Poster: models.PosterState{
			ShouldCropPoster: false,
		},
	}

	result, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferScraper, false)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	// Boolean field: ShouldCropPoster's isEmpty predicate is !v, so NFO's
	// false is empty and the scraper's true wins (both-have-data case here).
	if !result.Merged.Poster.ShouldCropPoster {
		t.Error("expected ShouldCropPoster=true from scraper")
	}
}

func TestIsFieldEmpty_V5_AllFields(t *testing.T) {
	emptyMovie := &models.Movie{}
	nonEmptyMovie := &models.Movie{
		ID:               "ABC-123",
		ContentID:        "abc123",
		DisplayTitle:     "Display",
		Title:            "Title",
		OriginalTitle:    "Original",
		Description:      "Desc",
		ReleaseYear:      2024,
		Runtime:          120,
		Director:         "Dir",
		Maker:            "Maker",
		Label:            "Label",
		Series:           "Series",
		RatingScore:      8.5,
		RatingVotes:      100,
		TrailerURL:       "http://trailer",
		OriginalFileName: "file.mp4",
		SourceName:       "r18dev",
		SourceURL:        "http://source",
		Actresses:        []models.Actress{{JapaneseName: "Test"}},
		Genres:           []models.Genre{{Name: "Action"}},
		Screenshots:      []string{"http://shot.jpg"},
		Translations:     []models.MovieTranslation{{Language: "en"}},
		Poster: models.PosterState{
			PosterURL:                "http://poster.jpg",
			CoverURL:                 "http://cover.jpg",
			CroppedPosterURL:         "http://cropped.jpg",
			OriginalPosterURL:        "http://orig-poster.jpg",
			OriginalCroppedPosterURL: "http://orig-cropped.jpg",
		},
	}

	for _, fieldName := range metadataFields {
		if fieldName == "ShouldCropPoster" {
			// ShouldCropPoster's isEmpty predicate is !v: a false (default) value
			// IS empty, so an empty movie's ShouldCropPoster is empty.
			if !isFieldEmptySpec(fieldName, emptyMovie) {
				t.Errorf("expected field %q to be empty for empty movie (false -> empty under !v)", fieldName)
			}
			continue
		}
		if !isFieldEmptySpec(fieldName, emptyMovie) {
			t.Errorf("expected field %q to be empty for empty movie", fieldName)
		}
	}

	// Check a few fields that should be non-empty
	nonEmptyFields := []string{"ID", "ContentID", "Title", "Maker", "Actresses", "Genres"}
	for _, fieldName := range nonEmptyFields {
		if isFieldEmptySpec(fieldName, nonEmptyMovie) {
			t.Errorf("expected field %q to be non-empty", fieldName)
		}
	}
}

func TestCountNonEmptyFields_V5_NilInput(t *testing.T) {
	count := countNonEmptyFields(nil)
	if count != 0 {
		t.Errorf("expected 0 for nil, got %d", count)
	}
}

func TestMakeProvenanceMap_V5_WithTimestamps(t *testing.T) {
	now := time.Now()
	movie := &models.Movie{
		ID:        "ABC-123",
		Title:     "Test",
		UpdatedAt: now,
	}

	prov := makeProvenanceMap(movie, "scraper")
	if len(prov) == 0 {
		t.Error("expected provenance entries")
	}
	for field, ds := range prov {
		if ds.Source != "scraper" {
			t.Errorf("field %q: expected source=scraper, got %q", field, ds.Source)
		}
		if ds.LastUpdated == nil {
			t.Errorf("field %q: expected LastUpdated to be set", field)
		}
	}
}
