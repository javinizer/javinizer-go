package aggregator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- New() with nil Config returns nil (line 104-106) ---

func TestNew_NilConfigReturnsNil(t *testing.T) {
	result := New(nil, nil, nil, nil)
	assert.Nil(t, result, "New with nil config should return nil")
}

// --- isUnknownActress: JapaneseName matches unknownText (line 281-283) ---

func TestIsUnknownActress_JapaneseNameMatches(t *testing.T) {
	// When nameKey doesn't match but JapaneseName does
	info := models.ActressInfo{JapaneseName: "未知の女優", FirstName: "Yui"}
	nameKey := resolveNameKey(info.JapaneseName, info.FirstName, info.LastName)
	// nameKey = "未知の女優" which matches unknownText directly
	assert.True(t, isUnknownActress(info, nameKey, "未知の女優"),
		"should return true when JapaneseName matches unknown text")
}

// --- buildTranslations: explicit translation merge with Title and OriginalTitle (lines 30-35) ---

func TestBuildTranslations_ExplicitMergeFillsOriginalTitle(t *testing.T) {
	// When a scraper provides explicit Translations where the second translation
	// fills OriginalTitle into an existing entry that only had Title
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{Priority: []string{"r18dev"}},
		},
	}
	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			Translations: []models.MovieTranslation{
				{Language: "ja", Title: "Japanese Title"},
				{Language: "ja", OriginalTitle: "Japanese Original Title"},
			},
		},
	}

	movie := &models.Movie{}
	translations := agg.buildTranslations(results, movie)
	require.Len(t, translations, 1)
	assert.Equal(t, "Japanese Title", translations[0].Title)
	assert.Equal(t, "Japanese Original Title", translations[0].OriginalTitle,
		"second translation should fill OriginalTitle into existing entry")
}

// --- buildTranslations: legacy merge with Title into existing (line 117-119) ---

func TestBuildTranslations_LegacyMergeFillsTitleIntoExisting(t *testing.T) {
	// When a scraper is a "winner" and its Title should merge into an existing
	// translation entry that has empty Title
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{Priority: []string{"r18dev"}},
		},
	}
	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source:   "r18dev",
			Language: "ja",
			Title:    "Winner Title",
			Translations: []models.MovieTranslation{
				{Language: "ja", Description: "Existing desc"}, // Title is empty
			},
		},
	}

	movie := &models.Movie{Title: "Winner Title"}
	translations := agg.buildTranslations(results, movie)
	require.Len(t, translations, 1)
	assert.Equal(t, "Winner Title", translations[0].Title,
		"legacy merge should fill Title into existing translation")
	assert.Equal(t, "Existing desc", translations[0].Description)
}

// --- validateRequiredFieldsScraped: label field (line 45-48) ---

func TestValidateRequiredFieldsScraped_LabelMissing(t *testing.T) {
	movie := &models.Movie{Label: ""}
	err := validateRequiredFieldsScraped(movie, []string{"label"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Label")
}

func TestValidateRequiredFieldsScraped_LabelPresent(t *testing.T) {
	movie := &models.Movie{Label: "Some Label"}
	err := validateRequiredFieldsScraped(movie, []string{"label"})
	assert.NoError(t, err)
}

// --- NewAliasResolver with nil cfg returns nil (line 37-39) ---

func TestNewAliasResolver_NilConfigReturnsNil(t *testing.T) {
	result := NewAliasResolver(nil, nil)
	assert.Nil(t, result, "NewAliasResolver with nil config should return nil")
}

// --- NewGenreProcessor with nil cfg returns nil (line 47-49) ---

func TestNewGenreProcessor_NilConfigReturnsNil(t *testing.T) {
	result := NewGenreProcessor(nil, nil)
	assert.Nil(t, result, "NewGenreProcessor with nil config should return nil")
}

// --- genre_processor: auto-add with non-duplicate error (lines 101-104) ---

func TestGenreProcessor_ApplyReplacement_AutoAddNonDuplicateError(t *testing.T) {
	mockRepo := &mockGenreLookupRepo{
		replacements: map[string]string{},
		createErr:    errors.New("some db error"), // Not ErrDuplicateKey
	}
	cfg := &MetadataConfig{
		GenreReplacement: genreReplacementConfigView{
			Enabled: true,
			AutoAdd: true,
		},
	}
	gp := NewGenreProcessor(cfg, mockRepo)

	// Genre not in cache → triggers auto-add → Create returns non-duplicate error
	result := gp.applyReplacement("NewGenre")
	assert.Equal(t, "NewGenre", result, "identity mapping should still be returned even if Create fails")
}

func TestGenreProcessor_ApplyReplacement_AutoAddDuplicateKeyError(t *testing.T) {
	mockRepo := &mockGenreLookupRepo{
		replacements: map[string]string{},
		createErr:    database.ErrDuplicateKey, // Duplicate key error should be silently ignored
	}
	cfg := &MetadataConfig{
		GenreReplacement: genreReplacementConfigView{
			Enabled: true,
			AutoAdd: true,
		},
	}
	gp := NewGenreProcessor(cfg, mockRepo)

	result := gp.applyReplacement("NewGenre")
	assert.Equal(t, "NewGenre", result, "identity mapping should be returned, duplicate key error ignored")
}

// --- genre_processor: loadCacheLocked with nil repo (line 165-167) ---

func TestGenreProcessor_LoadCacheLocked_NilRepoInNew(t *testing.T) {
	cfg := &MetadataConfig{
		GenreReplacement: genreReplacementConfigView{Enabled: true},
	}
	gp := NewGenreProcessor(cfg, nil)
	require.NotNil(t, gp)
	// loadCacheLocked is called internally when repo is nil → early return
	// Verify the processor works correctly without a repo
	assert.Equal(t, "Test", gp.applyReplacement("Test"))
}

// --- NewWordProcessor with nil cfg returns nil (line 61-63) ---

func TestNewWordProcessor_NilConfigReturnsNil(t *testing.T) {
	result := NewWordProcessor(nil, nil)
	assert.Nil(t, result, "NewWordProcessor with nil config should return nil")
}

// --- ReloadReplacementCaches on nil Aggregator ---

func TestAggregator_ReloadReplacementCaches_NilReceiver(t *testing.T) {
	var a *Aggregator
	a.ReloadReplacementCaches(context.Background()) // should not panic
}

// --- Aggregate with ContentID and various string fields ---

func TestAggregate_AllStringFieldsFromMultipleSources(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev", "dmm"},
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	releaseDate := time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)

	results := []*models.ScraperResult{
		{
			Source:        "r18dev",
			ID:            "IPX-001",
			ContentID:     "ipx001",
			Title:         "English Title",
			OriginalTitle: "Japanese Title",
			Description:   "English Description",
			Director:      "Director A",
			Maker:         "Maker A",
			Label:         "Label A",
			Series:        "Series A",
			PosterURL:     "https://example.com/poster.jpg",
			CoverURL:      "https://example.com/cover.jpg",
			TrailerURL:    "https://example.com/trailer.mp4",
			Runtime:       120,
			ReleaseDate:   &releaseDate,
		},
		{
			Source:        "dmm",
			ID:            "IPX-001",
			ContentID:     "ipx001dmm",
			Title:         "DMM Title",
			OriginalTitle: "DMM Original",
			Description:   "DMM Description",
			Director:      "Director B",
			Maker:         "Maker B",
			Label:         "Label B",
			Series:        "Series B",
		},
	}

	movie, aggResult, err := a.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)
	require.NotNil(t, aggResult)

	// r18dev is first priority, so all fields should come from r18dev
	assert.Equal(t, "IPX-001", movie.ID)
	assert.Equal(t, "ipx001", movie.ContentID)
	assert.Equal(t, "English Title", movie.Title)
	assert.Equal(t, "Japanese Title", movie.OriginalTitle)
	assert.Equal(t, "English Description", movie.Description)
	assert.Equal(t, "Director A", movie.Director)
	assert.Equal(t, "Maker A", movie.Maker)
	assert.Equal(t, "Label A", movie.Label)
	assert.Equal(t, "Series A", movie.Series)
	assert.Equal(t, "https://example.com/poster.jpg", movie.Poster.PosterURL)
	assert.Equal(t, "https://example.com/cover.jpg", movie.Poster.CoverURL)
	assert.Equal(t, "https://example.com/trailer.mp4", movie.TrailerURL)
	assert.Equal(t, 120, movie.Runtime)
	assert.NotNil(t, movie.ReleaseDate)
	assert.Equal(t, 2023, movie.ReleaseYear)
}

// --- Aggregate: Rating with votes ---

func TestAggregate_RatingFromPriority(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev", "dmm"},
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "TEST-001",
			Title:  "Test",
			Rating: &models.Rating{Score: 8.5, Votes: 100},
		},
		{
			Source: "dmm",
			ID:     "TEST-001",
			Title:  "Test DMM",
			Rating: &models.Rating{Score: 7.0, Votes: 200},
		},
	}

	movie, _, err := a.Aggregate(results)
	require.NoError(t, err)
	assert.Equal(t, 8.5, movie.RatingScore)
	assert.Equal(t, 100, movie.RatingVotes)
}

// --- Aggregate: Screenshots from priority ---

func TestAggregate_ScreenshotsFromPriority(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev", "dmm"},
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	results := []*models.ScraperResult{
		{
			Source:        "r18dev",
			ID:            "TEST-001",
			Title:         "Test",
			ScreenshotURL: []string{"https://example.com/1.jpg", "https://example.com/2.jpg"},
		},
	}

	movie, _, err := a.Aggregate(results)
	require.NoError(t, err)
	assert.Len(t, movie.Screenshots, 2)
}

// --- Aggregate: Genres with replacement and filtering ---

func TestAggregate_GenresWithReplacementAndFiltering(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev"},
		Metadata: &MetadataConfig{
			IgnoreGenres: []string{"^feat\\..*", "Unwanted"},
		},
	}
	gp := NewGenreProcessor(cfg.Metadata, nil)
	a := New(cfg, gp, nil, nil)

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "TEST-001",
			Title:  "Test",
			Genres: []string{"Drama", "Unwanted", "feat.Special", "Action"},
		},
	}

	movie, _, err := a.Aggregate(results)
	require.NoError(t, err)
	// "Unwanted" should be filtered by exact match, "feat.Special" by regex
	genreNames := make([]string, len(movie.Genres))
	for i, g := range movie.Genres {
		genreNames[i] = g.Name
	}
	assert.Contains(t, genreNames, "Drama")
	assert.Contains(t, genreNames, "Action")
	assert.NotContains(t, genreNames, "Unwanted")
	assert.NotContains(t, genreNames, "feat.Special")
}

// --- Aggregate: Required field validation ---

func TestAggregate_RequiredFieldValidation(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev"},
		Metadata: &MetadataConfig{
			RequiredFields: []string{"title", "maker"},
		},
	}
	a := New(cfg, nil, nil, nil)

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "TEST-001",
			Title:  "Test",
		},
	}

	_, _, err := a.Aggregate(results)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "required field")
}

// --- Aggregate: Actresses from multiple sources ---

func TestAggregate_ActressesFromMultipleSources(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev", "dmm"},
		Metadata: &MetadataConfig{
			NFO: nfoConfigView{
				UnknownActressMode: models.UnknownActressModeFallback,
				UnknownActressText: "Unknown",
			},
		},
	}
	a := New(cfg, nil, nil, nil)

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "TEST-001",
			Title:  "Test",
			Actresses: []models.ActressInfo{
				{DMMID: 100, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"},
			},
		},
		{
			Source: "dmm",
			ID:     "TEST-001",
			Title:  "Test",
			Actresses: []models.ActressInfo{
				{DMMID: 100, FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://example.com/thumb.jpg"},
			},
		},
	}

	movie, _, err := a.Aggregate(results)
	require.NoError(t, err)
	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, "Yui", movie.Actresses[0].FirstName)
	assert.Equal(t, "Hatano", movie.Actresses[0].LastName)
	assert.Equal(t, "波多野結衣", movie.Actresses[0].JapaneseName)
	assert.Equal(t, "https://example.com/thumb.jpg", movie.Actresses[0].ThumbURL)
}

// --- Aggregate: Unknown actress fallback ---

func TestAggregate_UnknownActressFallback(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev"},
		Metadata: &MetadataConfig{
			NFO: nfoConfigView{
				UnknownActressMode: models.UnknownActressModeFallback,
				UnknownActressText: "Unknown Actress",
			},
		},
	}
	a := New(cfg, nil, nil, nil)

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "TEST-001",
			Title:  "Test",
			// No actresses
		},
	}

	movie, _, err := a.Aggregate(results)
	require.NoError(t, err)
	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, "Unknown Actress", movie.Actresses[0].FirstName)
}

// --- Aggregate: Word replacement applied ---

func TestAggregate_WordReplacementApplied(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev"},
		Metadata: &MetadataConfig{
			WordReplacement: wordReplacementConfigView{Enabled: true},
		},
	}
	wp := newWordProcessorWithCache(cfg.Metadata, nil, map[string]string{
		"BadWord": "GoodWord",
	})
	a := New(cfg, nil, wp, nil)

	results := []*models.ScraperResult{
		{
			Source:      "r18dev",
			ID:          "TEST-001",
			Title:       "Movie with BadWord in title",
			Description: "Description with BadWord",
		},
	}

	movie, _, err := a.Aggregate(results)
	require.NoError(t, err)
	assert.Contains(t, movie.Title, "GoodWord")
	assert.NotContains(t, movie.Title, "BadWord")
	assert.Contains(t, movie.Description, "GoodWord")
}

// --- Aggregate: SourceName and SourceURL from results ---

func TestAggregate_SourceNameAndURL(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev"},
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	results := []*models.ScraperResult{
		{
			Source:    "r18dev",
			SourceURL: "https://r18.dev/videos/TEST-001",
			ID:        "TEST-001",
			Title:     "Test",
		},
	}

	movie, _, err := a.Aggregate(results)
	require.NoError(t, err)
	assert.Equal(t, "r18dev", movie.SourceName)
	assert.Equal(t, "https://r18.dev/videos/TEST-001", movie.SourceURL)
}

// --- Aggregate: CreatedAt and UpdatedAt are set ---

func TestAggregate_CreatedAtUpdatedAtSet(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev"},
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	before := time.Now().UTC()

	results := []*models.ScraperResult{
		{Source: "r18dev", ID: "TEST-001", Title: "Test"},
	}

	movie, _, err := a.Aggregate(results)
	require.NoError(t, err)
	assert.False(t, movie.CreatedAt.IsZero(), "CreatedAt should be set")
	assert.False(t, movie.UpdatedAt.IsZero(), "UpdatedAt should be set")
	assert.True(t, !movie.CreatedAt.Before(before), "CreatedAt should be >= test start time")
}

// --- AggregateWithPriority: custom priority overrides default ---

func TestAggregateWithPriority_CustomPrioritySelectsSecond(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev"},
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	results := []*models.ScraperResult{
		{Source: "r18dev", ID: "TEST-001", Title: "From R18Dev"},
		{Source: "dmm", ID: "TEST-001", Title: "From DMM"},
	}

	movie, _, err := a.AggregateWithPriority(results, []string{"dmm", "r18dev"})
	require.NoError(t, err)
	assert.Equal(t, "From DMM", movie.Title, "custom priority should select DMM first")
}

// --- Aggregate: ShouldCropPoster from priority ---

func TestAggregate_ShouldCropPoster(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev"},
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	results := []*models.ScraperResult{
		{
			Source:           "r18dev",
			ID:               "TEST-001",
			Title:            "Test",
			PosterURL:        "https://example.com/poster.jpg",
			ShouldCropPoster: true,
		},
	}

	movie, aggResult, err := a.Aggregate(results)
	require.NoError(t, err)
	assert.True(t, movie.Poster.ShouldCropPoster)
	assert.Equal(t, "r18dev", aggResult.FieldSources["should_crop_poster"])
}

// --- Aggregate: ShouldCropPoster false (no field source entry) ---

func TestAggregate_ShouldCropPosterFalse(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev"},
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	results := []*models.ScraperResult{
		{
			Source:           "r18dev",
			ID:               "TEST-001",
			Title:            "Test",
			PosterURL:        "https://example.com/poster.jpg",
			ShouldCropPoster: false,
		},
	}

	movie, aggResult, err := a.Aggregate(results)
	require.NoError(t, err)
	assert.False(t, movie.Poster.ShouldCropPoster)
	_, hasSource := aggResult.FieldSources["should_crop_poster"]
	assert.False(t, hasSource, "should_crop_poster should not be in field sources when false")
}
