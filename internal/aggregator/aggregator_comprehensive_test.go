package aggregator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAggregateBasic tests basic aggregation with multiple scrapers
func TestAggregateBasic(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev", "dmm"},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	results := []*models.ScraperResult{
		{
			Source:      "r18dev",
			ID:          "IPX-001",
			Title:       "R18 Title",
			Description: "R18 Description",
			ReleaseDate: &releaseDate,
		},
		{
			Source:      "dmm",
			ID:          "IPX-001",
			Title:       "DMM Title",
			Description: "DMM Description",
			ReleaseDate: &releaseDate,
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// With simplified priorities, all fields use the same global priority
	// Title should use r18dev (first priority)
	assert.Equal(t, "R18 Title", movie.Title)
	// Description should also use r18dev (first priority - same for all fields)
	assert.Equal(t, "R18 Description", movie.Description)
	// ID should match
	assert.Equal(t, "IPX-001", movie.ID)
	// Release date should be set
	assert.NotNil(t, movie.ReleaseDate)
	assert.Equal(t, 2021, movie.ReleaseYear)
}

// TestAggregateNoResults tests error handling when no results provided
func TestAggregateNoResults(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	movie, _, err := agg.Aggregate([]*models.ScraperResult{})
	assert.Error(t, err)
	assert.Nil(t, movie)
	assert.Contains(t, err.Error(), "no scraper results")
}

// TestAggregateEmptyPriorityUsesGlobal tests that empty priority arrays fall back to global
func TestAggregateEmptyPriorityUsesGlobal(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev", "dmm"},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	assert.Equal(t, []string{"r18dev", "dmm"}, agg.resolvedPriorities["Title"])
}

func TestLoadCachesFunctions(t *testing.T) {
	t.Run("GenreProcessor with nil repo", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority: []string{"r18dev"},
			},
		}

		gp := NewGenreProcessor(MetadataConfigFromApp(&cfg.Metadata), nil)

		// Should not panic when repo is nil
		gp.Reload(context.Background())

		// Replacement should return original when no cache
		assert.Equal(t, "TestGenre", gp.applyReplacement("TestGenre"))
	})

	t.Run("AliasResolver with nil repo", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority: []string{"r18dev"},
			},
		}

		ar := NewAliasResolver(MetadataConfigFromApp(&cfg.Metadata), nil)

		// Should not panic when repo is nil
		ar.Reload(context.Background())

		// Resolution should be no-op when no cache
		actress := &models.Actress{FirstName: "Test", LastName: "Actress"}
		ar.Resolve(actress)
		assert.Equal(t, "Test", actress.FirstName)
	})
}

// TestGetRatingByPriority tests rating field selection
func TestGetRatingByPriority(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := map[string]*models.ScraperResult{
		"r18dev": {
			Source: "r18dev",
			Rating: nil, // No rating
		},
		"dmm": {
			Source: "dmm",
			Rating: &models.Rating{
				Score: 4.5,
				Votes: 100,
			},
		},
	}

	score, votes, _, _ := agg.getRatingByPriorityWithSource(results, []string{"r18dev", "dmm"})
	assert.Equal(t, 4.5, score)
	assert.Equal(t, 100, votes)

	// Test with both having values - should use priority
	results["r18dev"].Rating = &models.Rating{
		Score: 5.0,
		Votes: 200,
	}
	score, votes, _, _ = agg.getRatingByPriorityWithSource(results, []string{"r18dev", "dmm"})
	assert.Equal(t, 5.0, score)
	assert.Equal(t, 200, votes)

	// Test with zero values ignored
	results["r18dev"].Rating = &models.Rating{
		Score: 0,
		Votes: 0,
	}
	score, votes, _, _ = agg.getRatingByPriorityWithSource(results, []string{"r18dev", "dmm"})
	assert.Equal(t, 4.5, score)
	assert.Equal(t, 100, votes)
}

// TestGetActressesByPriority tests actress aggregation and merging
func TestGetActressesByPriority(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
					ThumbURL:     "",
				},
			},
		},
		{
			Source: "dmm",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					JapaneseName: "波多野結衣",
					ThumbURL:     "https://example.com/thumb.jpg",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev", "dmm"},
	}

	actresses := merger.Merge(sources, opts)

	// Should merge data from both sources
	assert.Equal(t, "Yui", actresses[0].FirstName)
	assert.Equal(t, "Hatano", actresses[0].LastName)
	assert.Equal(t, "波多野結衣", actresses[0].JapaneseName)
	assert.Equal(t, 12345, actresses[0].DMMID)
	assert.Equal(t, "https://example.com/thumb.jpg", actresses[0].ThumbURL)
}

// TestGetActressesByPriorityMultiple tests multiple actresses
func TestGetActressesByPriorityMultiple(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
				},
				{
					FirstName:    "Jun",
					LastName:     "Amamiya",
					JapaneseName: "雨宮淳",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev"},
	}

	actresses := merger.Merge(sources, opts)
	require.Len(t, actresses, 2)
}

// TestGetActressesByPriorityUnknownText tests unknown actress text
func TestGetActressesByPriorityUnknownText(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source:    "r18dev",
			Actresses: []models.ActressInfo{}, // Empty
		},
	}
	opts := actressMergeOptions{
		Priority:    []string{"r18dev"},
		SkipUnknown: false,
		UnknownText: "Unknown",
	}

	actresses := merger.Merge(sources, opts)
	require.Len(t, actresses, 1)
	assert.Equal(t, "Unknown", actresses[0].FirstName)
	assert.Equal(t, "Unknown", actresses[0].JapaneseName)
}

// TestGetGenresByPriority tests genre selection
func TestGetGenresByPriority(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := map[string]*models.ScraperResult{
		"r18dev": {
			Source: "r18dev",
			Genres: []string{"Drama", "Romance"},
		},
		"dmm": {
			Source: "dmm",
			Genres: []string{"Action", "Comedy"},
		},
	}

	// Should use r18dev (first priority)
	genres, _ := agg.getGenresByPriorityWithSource(results, []string{"r18dev", "dmm"})
	assert.Equal(t, []string{"Drama", "Romance"}, genres)

	// Empty genres should fall back to next
	results["r18dev"].Genres = []string{}
	genres, _ = agg.getGenresByPriorityWithSource(results, []string{"r18dev", "dmm"})
	assert.Equal(t, []string{"Action", "Comedy"}, genres)
}

// TestGetScreenshotsByPriority tests screenshot URL selection
func TestGetScreenshotsByPriority(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := map[string]*models.ScraperResult{
		"r18dev": {
			Source:        "r18dev",
			ScreenshotURL: []string{"https://r18.com/1.jpg", "https://r18.com/2.jpg"},
		},
		"dmm": {
			Source:        "dmm",
			ScreenshotURL: []string{"https://dmm.com/1.jpg"},
		},
	}

	screenshots := agg.getScreenshotsByPriorityWithSource(results, []string{"r18dev", "dmm"}, nil)
	assert.Equal(t, []string{"https://r18.com/1.jpg", "https://r18.com/2.jpg"}, screenshots)

	// Empty should fall back
	results["r18dev"].ScreenshotURL = []string{}
	screenshots = agg.getScreenshotsByPriorityWithSource(results, []string{"r18dev", "dmm"}, nil)
	assert.Equal(t, []string{"https://dmm.com/1.jpg"}, screenshots)
}

// TestBuildTranslations tests translation building
func TestBuildTranslations(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source:      "r18dev",
			Language:    "en",
			Title:       "English Title",
			Description: "English Description",
			Maker:       "English Maker",
		},
		{
			Source:      "dmm",
			Language:    "ja",
			Title:       "Japanese Title",
			Description: "Japanese Description",
			Maker:       "Japanese Maker",
		},
	}

	// Create a movie where both scrapers are "winners":
	// r18dev wins Title, dmm wins Description
	movie := &models.Movie{
		Title:       "English Title",        // from r18dev
		Description: "Japanese Description", // from dmm
	}

	translations := agg.buildTranslations(results, movie)
	require.Len(t, translations, 2)

	// Find each translation
	var enTranslation, jaTranslation *models.MovieTranslation
	for i := range translations {
		if translations[i].Language == "en" {
			enTranslation = &translations[i]
		}
		if translations[i].Language == "ja" {
			jaTranslation = &translations[i]
		}
	}

	require.NotNil(t, enTranslation)
	assert.Equal(t, "English Title", enTranslation.Title)
	assert.Equal(t, "English Description", enTranslation.Description)
	assert.Equal(t, "r18dev", enTranslation.SourceName)

	require.NotNil(t, jaTranslation)
	assert.Equal(t, "Japanese Title", jaTranslation.Title)
	assert.Equal(t, "Japanese Description", jaTranslation.Description)
	assert.Equal(t, "dmm", jaTranslation.SourceName)
}

// TestBuildTranslationsSkipsNoLanguage tests that results without language are skipped
func TestBuildTranslationsSkipsNoLanguage(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source:   "r18dev",
			Language: "", // No language
			Title:    "Title",
		},
	}

	movie := &models.Movie{Title: "Title"}
	translations := agg.buildTranslations(results, movie)
	assert.Len(t, translations, 0)
}

// TestApplyGenreReplacementWithDatabase tests genre replacement with database
func TestApplyGenreReplacementWithDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  dbPath,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			GenreReplacement: config.GenreReplacementConfig{
				Enabled: true,
				AutoAdd: false,
			},
		},
	}

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)

	// Add genre replacement
	repo := database.NewGenreReplacementRepository(db)
	err = repo.Create(context.TODO(), &models.GenreReplacement{
		Original:    "ドラマ",
		Replacement: "Drama",
	})
	require.NoError(t, err)

	agg := newAggregatorWithRepos(testConfigFromAppConfig(cfg),
		database.NewGenreReplacementRepository(db),
		database.NewWordReplacementRepository(db),
		database.NewActressAliasRepository(db),
	)

	// Test replacement
	result := agg.genreProcessor.applyReplacement("ドラマ")
	assert.Equal(t, "Drama", result)

	// Test non-existent
	result = agg.genreProcessor.applyReplacement("Unknown")
	assert.Equal(t, "Unknown", result)
}

// TestApplyGenreReplacementAutoAdd tests auto-add functionality
func TestApplyGenreReplacementAutoAdd(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  dbPath,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			GenreReplacement: config.GenreReplacementConfig{
				Enabled: true,
				AutoAdd: true,
			},
		},
	}

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)

	agg := newAggregatorWithRepos(testConfigFromAppConfig(cfg),
		database.NewGenreReplacementRepository(db),
		database.NewWordReplacementRepository(db),
		database.NewActressAliasRepository(db),
	)

	// Apply to new genre - should auto-add
	result := agg.genreProcessor.applyReplacement("NewGenre")
	assert.Equal(t, "NewGenre", result)

	// Verify it was added to database
	repo := database.NewGenreReplacementRepository(db)
	replacement, err := repo.FindByOriginal(context.TODO(), "NewGenre")
	require.NoError(t, err)
	assert.Equal(t, "NewGenre", replacement.Original)
	assert.Equal(t, "NewGenre", replacement.Replacement)
}

// TestReloadGenreReplacements tests cache reloading
func TestReloadGenreReplacements(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  dbPath,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			GenreReplacement: config.GenreReplacementConfig{
				Enabled: true,
			},
		},
	}

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)

	agg := newAggregatorWithRepos(testConfigFromAppConfig(cfg),
		database.NewGenreReplacementRepository(db),
		database.NewWordReplacementRepository(db),
		database.NewActressAliasRepository(db),
	)

	// Initially no replacement
	result := agg.genreProcessor.applyReplacement("TestGenre")
	assert.Equal(t, "TestGenre", result)

	// Add replacement directly to database
	repo := database.NewGenreReplacementRepository(db)
	err = repo.Create(context.TODO(), &models.GenreReplacement{
		Original:    "TestGenre",
		Replacement: "Replaced",
	})
	require.NoError(t, err)

	// Should still return original (cache not reloaded)
	result = agg.genreProcessor.applyReplacement("TestGenre")
	assert.Equal(t, "TestGenre", result)

	// Reload cache
	agg.genreProcessor.Reload(context.Background())

	// Should now return replacement
	result = agg.genreProcessor.applyReplacement("TestGenre")
	assert.Equal(t, "Replaced", result)
}

// TestCopySlice tests slice copying utility
func TestCopySlice(t *testing.T) {
	original := []string{"a", "b", "c"}
	copied := copySlice(original)

	assert.Equal(t, original, copied)
	assert.NotSame(t, &original[0], &copied[0]) // Different memory

	// Modify original - copied should not change
	original[0] = "modified"
	assert.Equal(t, "modified", original[0])
	assert.Equal(t, "a", copied[0])

	// Test nil slice
	nilCopy := copySlice(nil)
	assert.Nil(t, nilCopy)
}

// TestGetFieldPriorityFromConfig tests config field extraction
func TestGetFieldPriorityFromConfig(t *testing.T) {
	// Test with explicit priority set
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"},
			},
		},
	}

	// Test that explicit priority is returned (fieldKey is ignored for simplified priorities)
	priority := getFieldPriorityFromConfig(testConfigFromAppConfig(cfg), "ID")
	assert.Equal(t, []string{"dmm", "r18dev"}, priority)

	// Test that same priority is returned for any field (fieldKey is always ignored)
	priority = getFieldPriorityFromConfig(testConfigFromAppConfig(cfg), "Title")
	assert.Equal(t, []string{"dmm", "r18dev"}, priority)

	// With simplified priorities, fieldKey is always ignored - returns explicit priority
	priority = getFieldPriorityFromConfig(testConfigFromAppConfig(cfg), "UnknownField")
	assert.Equal(t, []string{"dmm", "r18dev"}, priority)
}

// TestAggregateSourceMetadata tests that source name and URL are set
func TestAggregateSourceMetadata(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source:    "r18dev",
			SourceURL: "https://r18.dev/movie/12345",
			ID:        "IPX-001",
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Equal(t, "r18dev", movie.SourceName)
	assert.Equal(t, "https://r18.dev/movie/12345", movie.SourceURL)
}

// TestAggregateTimestamps tests that timestamps are set
func TestAggregateTimestamps(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	before := time.Now().UTC()

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "IPX-001",
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	after := time.Now().UTC()

	// Timestamps should be set and within range
	assert.False(t, movie.CreatedAt.IsZero())
	assert.False(t, movie.UpdatedAt.IsZero())
	assert.True(t, movie.CreatedAt.After(before.Add(-time.Second)))
	assert.True(t, movie.CreatedAt.Before(after.Add(time.Second)))
}

// TestAggregateWithAllFields tests comprehensive field aggregation
func TestAggregateWithAllFields(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev"},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	results := []*models.ScraperResult{
		{
			Source:        "r18dev",
			SourceURL:     "https://r18.dev/12345",
			Language:      "en",
			ID:            "IPX-001",
			ContentID:     "ipx00001",
			Title:         "Test Movie",
			OriginalTitle: "テスト映画",
			Description:   "Test description",
			ReleaseDate:   &releaseDate,
			Runtime:       120,
			Director:      "Test Director",
			Maker:         "Test Maker",
			Label:         "Test Label",
			Series:        "Test Series",
			Rating: &models.Rating{
				Score: 4.5,
				Votes: 100,
			},
			Actresses: []models.ActressInfo{
				{
					FirstName:    "Test",
					LastName:     "Actress",
					JapaneseName: "テスト女優",
				},
			},
			Genres:        []string{"Drama", "Romance"},
			PosterURL:     "https://example.com/poster.jpg",
			CoverURL:      "https://example.com/cover.jpg",
			ScreenshotURL: []string{"https://example.com/ss1.jpg", "https://example.com/ss2.jpg"},
			TrailerURL:    "https://example.com/trailer.mp4",
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Verify all fields
	assert.Equal(t, "IPX-001", movie.ID)
	assert.Equal(t, "ipx00001", movie.ContentID)
	assert.Equal(t, "Test Movie", movie.Title)
	assert.Equal(t, "テスト映画", movie.OriginalTitle)
	assert.Equal(t, "Test description", movie.Description)
	assert.Equal(t, 120, movie.Runtime)
	assert.Equal(t, "Test Director", movie.Director)
	assert.Equal(t, "Test Maker", movie.Maker)
	assert.Equal(t, "Test Label", movie.Label)
	assert.Equal(t, "Test Series", movie.Series)
	assert.Equal(t, 4.5, movie.RatingScore)
	assert.Equal(t, 100, movie.RatingVotes)
	assert.Equal(t, "https://example.com/poster.jpg", movie.Poster.PosterURL)
	assert.Equal(t, "https://example.com/cover.jpg", movie.Poster.CoverURL)
	assert.Equal(t, "https://example.com/trailer.mp4", movie.TrailerURL)
	assert.Equal(t, 2021, movie.ReleaseYear)

	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, "Test", movie.Actresses[0].FirstName)

	require.Len(t, movie.Genres, 2)
	assert.Equal(t, "Drama", movie.Genres[0].Name)

	assert.Equal(t, []string{"https://example.com/ss1.jpg", "https://example.com/ss2.jpg"}, movie.Screenshots)

	require.Len(t, movie.Translations, 1)
	assert.Equal(t, "en", movie.Translations[0].Language)
}

// TestAggregator_NilReceiverMethods tests that nil-receiver calls return errors
func TestAggregator_NilReceiverMethods(t *testing.T) {
	var agg *Aggregator
	cfg, err := agg.Config()
	assert.Nil(t, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil Aggregator")

	te, err := agg.TemplateEngine()
	assert.Nil(t, te)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil Aggregator")
}

func TestNewAggregatorResolvesDefaultPriority(t *testing.T) {
	// Test with empty config - priority should be derived from scraper registrations
	// (which won't happen in this test since scrapers aren't imported)
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{}, // Empty global priority
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: nil,
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	// With simplified priorities and no configured or derived priority,
	// resolved priorities should be empty (scrapers not imported in tests)
	assert.Empty(t, agg.resolvedPriorities["Title"])
}

// TestAggregateGenresWithFiltering tests genre filtering in aggregation
func TestAggregateGenresWithFiltering(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			IgnoreGenres: []string{"Sample", "Featured Actress"},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "IPX-001",
			Genres: []string{"Drama", "Sample", "Romance", "Featured Actress"},
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Should filter out ignored genres
	require.Len(t, movie.Genres, 2)
	assert.Equal(t, "Drama", movie.Genres[0].Name)
	assert.Equal(t, "Romance", movie.Genres[1].Name)
}

// TestAggregateGenresWithReplacement tests genre replacement in aggregation
func TestAggregateGenresWithReplacement(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  dbPath,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			GenreReplacement: config.GenreReplacementConfig{
				Enabled: true,
			},
		},
	}

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)

	// Add genre replacements
	repo := database.NewGenreReplacementRepository(db)
	err = repo.Create(context.TODO(), &models.GenreReplacement{
		Original:    "ドラマ",
		Replacement: "Drama",
	})
	require.NoError(t, err)

	agg := newAggregatorWithRepos(testConfigFromAppConfig(cfg),
		database.NewGenreReplacementRepository(db),
		database.NewWordReplacementRepository(db),
		database.NewActressAliasRepository(db),
	)

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "IPX-001",
			Genres: []string{"ドラマ", "Romance"},
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	require.Len(t, movie.Genres, 2)
	assert.Equal(t, "Drama", movie.Genres[0].Name)
	assert.Equal(t, "Romance", movie.Genres[1].Name)
}

// TestAggregateActressMergingByJapaneseName tests actress merging logic
func TestAggregateActressMergingByJapaneseName(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
		Metadata: config.MetadataConfig{
			ActressDatabase: config.ActressDatabaseConfig{
				ConvertAlias: false,
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "IPX-001",
			Actresses: []models.ActressInfo{
				{
					JapaneseName: "波多野結衣",
					FirstName:    "Yui",
					LastName:     "Hatano",
				},
			},
		},
		{
			Source: "dmm",
			ID:     "IPX-001",
			Actresses: []models.ActressInfo{
				{
					JapaneseName: "波多野結衣",
					DMMID:        12345,
					ThumbURL:     "https://example.com/thumb.jpg",
				},
			},
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Should have merged into one actress
	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, "波多野結衣", movie.Actresses[0].JapaneseName)
	assert.Equal(t, "Yui", movie.Actresses[0].FirstName)
	assert.Equal(t, "Hatano", movie.Actresses[0].LastName)
	assert.Equal(t, 12345, movie.Actresses[0].DMMID)
	assert.Equal(t, "https://example.com/thumb.jpg", movie.Actresses[0].ThumbURL)
}

func TestAggregateActressMergingJapaneseNameVsFirstName(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "javlibrary"},
		},
		Metadata: config.MetadataConfig{
			ActressDatabase: config.ActressDatabaseConfig{
				ConvertAlias: false,
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source: "dmm",
			ID:     "ABP-880",
			Actresses: []models.ActressInfo{
				{
					DMMID:        1044046,
					JapaneseName: "河合あすな",
					ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/kawai_asuna.jpg",
				},
			},
		},
		{
			Source: "javlibrary",
			ID:     "ABP-880",
			Actresses: []models.ActressInfo{
				{
					FirstName: "河合あすな",
				},
			},
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	require.Len(t, movie.Actresses, 1, "should merge Japanese-name-in-FirstName with JapaneseName")
	assert.Equal(t, "河合あすな", movie.Actresses[0].JapaneseName)
	assert.Equal(t, 1044046, movie.Actresses[0].DMMID)
	assert.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/kawai_asuna.jpg", movie.Actresses[0].ThumbURL)
}

// TestAggregateActressAliasConversion tests actress alias conversion in full aggregation
func TestAggregateActressAliasConversion(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  dbPath,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			ActressDatabase: config.ActressDatabaseConfig{
				Enabled:      true,
				ConvertAlias: true,
			},
		},
	}

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)

	// Add actress alias
	aliasRepo := database.NewActressAliasRepository(db)
	err = aliasRepo.Create(context.TODO(), &models.ActressAlias{
		AliasName:     "Yui Hatano",
		CanonicalName: "Hatano Yui",
	})
	require.NoError(t, err)

	agg := newAggregatorWithRepos(testConfigFromAppConfig(cfg),
		database.NewGenreReplacementRepository(db),
		database.NewWordReplacementRepository(db),
		database.NewActressAliasRepository(db),
	)

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "IPX-001",
			Actresses: []models.ActressInfo{
				{
					FirstName: "Yui",
					LastName:  "Hatano",
				},
			},
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	require.Len(t, movie.Actresses, 1)
	// Should be converted to canonical form
	assert.Equal(t, "Hatano Yui", movie.Actresses[0].FullName())
}

// TestAggregateGenresWithRegexFiltering tests regex-based genre filtering
func TestAggregateGenresWithRegexFiltering(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			IgnoreGenres: []string{
				"^Featured", // Regex: starts with "Featured"
				".*VR$",     // Regex: ends with "VR"
				"Sample",    // Exact match
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "IPX-001",
			Genres: []string{
				"Drama",
				"Featured Actress", // Should be filtered (matches ^Featured)
				"Romance",
				"Sample",          // Should be filtered (exact)
				"HD VR",           // Should be filtered (matches .*VR$)
				"Virtual Reality", // Should NOT be filtered (doesn't end with VR)
			},
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Should keep only: Drama, Romance, Virtual Reality
	require.Len(t, movie.Genres, 3)
	genreNames := []string{}
	for _, g := range movie.Genres {
		genreNames = append(genreNames, g.Name)
	}
	assert.Contains(t, genreNames, "Drama")
	assert.Contains(t, genreNames, "Romance")
	assert.Contains(t, genreNames, "Virtual Reality")
	assert.NotContains(t, genreNames, "Featured Actress")
	assert.NotContains(t, genreNames, "Sample")
	assert.NotContains(t, genreNames, "HD VR")
}

// TestAggregateRequiredFieldsValidation tests that required field validation works
func TestAggregateRequiredFieldsValidation(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			RequiredFields: []string{"ID", "Title", "ReleaseDate"},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	t.Run("all required fields present", func(t *testing.T) {
		releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)
		results := []*models.ScraperResult{
			{
				Source:      "r18dev",
				ID:          "IPX-001",
				Title:       "Test Movie",
				ReleaseDate: &releaseDate,
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)
		assert.Equal(t, "IPX-001", movie.ID)
		assert.Equal(t, "Test Movie", movie.Title)
	})

	t.Run("missing required field - should fail", func(t *testing.T) {
		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "", // Missing title
			},
		}

		movie, _, err := agg.Aggregate(results)
		assert.Error(t, err)
		assert.Nil(t, movie)
		assert.Contains(t, err.Error(), "required field validation failed")
		assert.Contains(t, err.Error(), "Title")
	})

	t.Run("multiple missing required fields", func(t *testing.T) {
		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "", // Missing ID
				Title:  "", // Missing title
			},
		}

		movie, _, err := agg.Aggregate(results)
		assert.Error(t, err)
		assert.Nil(t, movie)
		assert.Contains(t, err.Error(), "ID")
		assert.Contains(t, err.Error(), "Title")
	})
}

// TestAggregateDisplayTitleTemplate verifies that aggregator does NOT apply DisplayTitle
// regardless of template config. DisplayTitle is applied by Workflow via ApplyDisplayTitleFromSource.
func TestAggregateDisplayTitleTemplate(t *testing.T) {
	t.Run("valid template — aggregator does not apply DisplayTitle", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority: []string{"r18dev"},
			},
			Metadata: config.MetadataConfig{
				NFO: config.NFOConfig{
					Format: config.NFOFormatConfig{
						DisplayTitle: "[<ID>] <TITLE>",
					},
				},
			},
		}

		agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Test Movie",
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		assert.Empty(t, movie.DisplayTitle, "aggregator should not apply DisplayTitle")
	})

	t.Run("template with multiple fields — aggregator does not apply DisplayTitle", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority: []string{"r18dev"},
			},
			Metadata: config.MetadataConfig{
				NFO: config.NFOConfig{
					Format: config.NFOFormatConfig{
						DisplayTitle: "<TITLE> by <STUDIO> (<YEAR>)",
					},
				},
			},
		}

		agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

		releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)
		results := []*models.ScraperResult{
			{
				Source:      "r18dev",
				ID:          "IPX-001",
				Title:       "Amazing Movie",
				Maker:       "Idea Pocket",
				ReleaseDate: &releaseDate,
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		assert.Empty(t, movie.DisplayTitle, "aggregator should not apply DisplayTitle")
	})

	t.Run("empty template — DisplayTitle remains empty", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority: []string{"r18dev"},
			},
			Metadata: config.MetadataConfig{
				NFO: config.NFOConfig{
					Format: config.NFOFormatConfig{
						DisplayTitle: "",
					},
				},
			},
		}

		agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Test Movie",
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		// Empty template: aggregator leaves DisplayTitle empty (Workflow applies fallback)
		assert.Empty(t, movie.DisplayTitle)
	})

	t.Run("invalid template — aggregator does not apply DisplayTitle", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority: []string{"r18dev"},
			},
			Metadata: config.MetadataConfig{
				NFO: config.NFOConfig{
					Format: config.NFOFormatConfig{
						DisplayTitle: "<INVALID_TAG>",
					},
				},
			},
		}

		agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Test Movie",
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		assert.Empty(t, movie.DisplayTitle, "aggregator should not apply DisplayTitle")
	})
}

// TestActressDMMIDDeduplicationSameIDDifferentNames tests that actresses with the same DMMID
// but different names from multiple scrapers are deduplicated into a single actress.
// This is the core scenario mentioned in Story 2.4 AC-2.4.2:
// "r18dev and dmm may return same actress with different names but same DMMID"
func TestActressDMMIDDeduplicationSameIDDifferentNames(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
					ThumbURL:     "https://r18dev.example.com/thumb.jpg",
				},
			},
		},
		{
			Source: "dmm",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					FirstName:    "Yuui", // Different romanization
					LastName:     "Hatano",
					JapaneseName: "波多野ゆい", // Different Japanese name
					ThumbURL:     "https://dmm.example.com/thumb.jpg",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev", "dmm"},
	}

	actresses := merger.Merge(sources, opts)

	// Should have exactly 1 actress (deduplicated by DMMID)
	require.Len(t, actresses, 1, "Should deduplicate actresses with same DMMID")

	// Should use data from r18dev (higher priority)
	assert.Equal(t, 12345, actresses[0].DMMID)
	assert.Equal(t, "Yui", actresses[0].FirstName, "Should use r18dev FirstName (higher priority)")
	assert.Equal(t, "Hatano", actresses[0].LastName, "Should use r18dev LastName (higher priority)")
	assert.Equal(t, "波多野結衣", actresses[0].JapaneseName, "Should use r18dev JapaneseName (higher priority)")
	assert.Equal(t, "https://r18dev.example.com/thumb.jpg", actresses[0].ThumbURL, "Should use r18dev ThumbURL (higher priority)")
}

// TestActressDMMIDUpgradeScenario tests the DMMID upgrade scenario where:
// 1. First scraper provides actress without DMMID (only name)
// 2. Second scraper provides same actress with DMMID
// 3. The actress should be upgraded with the DMMID and merged
func TestActressDMMIDUpgradeScenario(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					DMMID:        0, // No DMMID from r18dev
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
					ThumbURL:     "https://r18dev.example.com/thumb.jpg",
				},
			},
		},
		{
			Source: "dmm",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345, // DMM provides DMMID
					FirstName:    "",    // Partial data - no first name
					LastName:     "",    // Partial data - no last name
					JapaneseName: "波多野結衣",
					ThumbURL:     "",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev", "dmm"},
	}

	actresses := merger.Merge(sources, opts)

	// Should have exactly 1 actress (merged by name, then upgraded with DMMID)
	require.Len(t, actresses, 1, "Should merge actress and upgrade with DMMID")

	// Should have DMMID from dmm
	assert.Equal(t, 12345, actresses[0].DMMID, "Should upgrade actress with DMMID from dmm")

	// Should keep data from r18dev (higher priority)
	assert.Equal(t, "Yui", actresses[0].FirstName, "Should keep r18dev FirstName")
	assert.Equal(t, "Hatano", actresses[0].LastName, "Should keep r18dev LastName")
	assert.Equal(t, "波多野結衣", actresses[0].JapaneseName, "Should keep JapaneseName")
	assert.Equal(t, "https://r18dev.example.com/thumb.jpg", actresses[0].ThumbURL, "Should keep r18dev ThumbURL")
}

// TestActressDMMIDPartialDataMerging tests that when multiple scrapers provide
// the same actress (by DMMID) with partial data, all fields are merged according to priority
func TestActressDMMIDPartialDataMerging(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					FirstName:    "Yui",
					LastName:     "", // Missing last name
					JapaneseName: "波多野結衣",
					ThumbURL:     "", // Missing thumb URL
				},
			},
		},
		{
			Source: "dmm",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					FirstName:    "Yuui", // Different but lower priority
					LastName:     "Hatano",
					JapaneseName: "波多野ゆい", // Different but lower priority
					ThumbURL:     "https://dmm.example.com/thumb.jpg",
				},
			},
		},
		{
			Source: "javlibrary",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					FirstName:    "Yui H.", // Even lower priority
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
					ThumbURL:     "https://javlib.example.com/thumb.jpg",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev", "dmm", "javlibrary"},
	}

	actresses := merger.Merge(sources, opts)

	// Should have exactly 1 actress (deduplicated by DMMID)
	require.Len(t, actresses, 1, "Should deduplicate actresses with same DMMID")

	// Should merge data respecting priority order
	assert.Equal(t, 12345, actresses[0].DMMID)
	assert.Equal(t, "Yui", actresses[0].FirstName, "Should use r18dev FirstName (highest priority)")
	assert.Equal(t, "Hatano", actresses[0].LastName, "Should use dmm LastName (r18dev had empty)")
	assert.Equal(t, "波多野結衣", actresses[0].JapaneseName, "Should use r18dev JapaneseName (highest priority)")
	assert.Equal(t, "https://dmm.example.com/thumb.jpg", actresses[0].ThumbURL, "Should use dmm ThumbURL (r18dev had empty)")
}

// TestActressDMMIDZeroNotDeduplicated tests that actresses with DMMID=0 are NOT deduplicated
// This validates the business rule: "Zero DMMID allowed: Some actresses may not have DMM ID (cannot deduplicate)"
func TestActressDMMIDZeroNotDeduplicated(t *testing.T) {
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					DMMID:        0, // No DMMID
					FirstName:    "Unknown",
					LastName:     "Actress",
					JapaneseName: "未知の女優",
					ThumbURL:     "https://r18dev.example.com/thumb1.jpg",
				},
			},
		},
		{
			Source: "javlibrary",
			Actresses: []models.ActressInfo{
				{
					DMMID:        0, // Also no DMMID
					FirstName:    "Unknown",
					LastName:     "Actress",
					JapaneseName: "未知の女優",
					ThumbURL:     "https://javlib.example.com/thumb2.jpg",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev", "javlibrary"},
	}

	actresses := merger.Merge(sources, opts)

	// Should have exactly 1 actress (merged by name since both have same name)
	require.Len(t, actresses, 1, "Should merge actresses with same name even without DMMID")

	// Should use data from r18dev (higher priority)
	assert.Equal(t, 0, actresses[0].DMMID)
	assert.Equal(t, "Unknown", actresses[0].FirstName)
	assert.Equal(t, "Actress", actresses[0].LastName)
	assert.Equal(t, "未知の女優", actresses[0].JapaneseName)
	assert.Equal(t, "https://r18dev.example.com/thumb1.jpg", actresses[0].ThumbURL)
}

// TestGetRatingByPriorityWithSource_SkipsOutOfRange is a regression test for
// out-of-range rating scores. A scraper returning a 0–100 percentage (or
// garbage) must be skipped and the next valid priority source used, rather
// than the corrupt score being persisted into movie/DB/NFO. Old
// getRatingByPriority did `if !isRatingScoreValid(score) { warn; continue }`;
// the rewrite dropped the range check.
func TestGetRatingByPriorityWithSource_SkipsOutOfRange(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev", "dmm"}},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{Priority: []string{"r18dev", "dmm"}},
		},
	}
	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := map[string]*models.ScraperResult{
		"r18dev": {Source: "r18dev", Rating: &models.Rating{Score: 85.0, Votes: 100}}, // out-of-range
		"dmm":    {Source: "dmm", Rating: &models.Rating{Score: 7.5, Votes: 200}},     // valid
	}
	score, votes, source, _ := agg.getRatingByPriorityWithSource(results, []string{"r18dev", "dmm"})
	assert.Equal(t, 7.5, score, "out-of-range priority-1 score should be skipped for valid priority-2")
	assert.Equal(t, 200, votes)
	assert.Equal(t, "dmm", source)

	// When every source is out-of-range, nothing is stored (score 0, no source).
	results["dmm"].Rating.Score = 50.0
	score, votes, source, _ = agg.getRatingByPriorityWithSource(results, []string{"r18dev", "dmm"})
	assert.Equal(t, 0.0, score, "all-out-of-range should yield no score")
	assert.Equal(t, 0, votes)
	assert.Equal(t, "", source)
}

// TestAggregate_GenresWordReplacementApplied is a regression test for the
// genre word-replacement drop: configured WORD replacements must be applied to
// each genre token before genre replacement + ignore. The rewrite dropped this
// and wordProcessor.applyToMovie does not touch movie.Genres, so user word maps
// no longer normalized genre tokens. (The pre-existing
// TestAggregate_GenresWordReplacement actually tests GENRE replacement via the
// genreProcessor cache, not word replacement — which is why this slipped.)
func TestAggregate_GenresWordReplacementApplied(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
		Metadata: config.MetadataConfig{
			Priority:         config.PriorityConfig{Priority: []string{"r18dev"}},
			WordReplacement:  config.WordReplacementConfig{Enabled: true},
			GenreReplacement: config.GenreReplacementConfig{Enabled: false},
		},
	}
	meta := MetadataConfigFromApp(&cfg.Metadata)
	// Word map "HD" -> "FHD": genre "HD Video" must become "FHD Video".
	wp := newWordProcessorWithCache(meta, nil, map[string]string{"HD": "FHD"})
	agg := New(testConfigFromAppConfig(cfg), NewGenreProcessor(meta, nil), wp, NewAliasResolver(meta, nil))

	results := []*models.ScraperResult{
		{Source: "r18dev", ID: "IPX-1", Title: "T", Genres: []string{"HD Video", "Drama"}},
	}
	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	names := make([]string, len(movie.Genres))
	for i, g := range movie.Genres {
		names[i] = g.Name
	}
	assert.Contains(t, names, "FHD Video", "word replacement should normalize genre token HD->FHD")
	assert.NotContains(t, names, "HD Video")
	assert.Contains(t, names, "Drama")
}

// TestGetRatingByPriorityWithSource_SkippedWarningNamesSource is a regression
// test for C2-AGG-1 + cycle-1 NIT-11. Post-MAJOR-4, out-of-range scores are
// skipped before returning, which made the old assignRating warning block dead.
// The warning now lives in the skip path and NAMES the skipped source (the old
// warning named only the stored rating, never the source). Verify the 4th
// return value carries the source name and is empty when nothing is skipped.
func TestGetRatingByPriorityWithSource_SkippedWarningNamesSource(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev", "dmm"}},
		Metadata: config.MetadataConfig{Priority: config.PriorityConfig{Priority: []string{"r18dev", "dmm"}}},
	}
	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := map[string]*models.ScraperResult{
		"r18dev": {Source: "r18dev", Rating: &models.Rating{Score: 85.0, Votes: 100}}, // skipped OOR
		"dmm":    {Source: "dmm", Rating: &models.Rating{Score: 7.5, Votes: 200}},     // valid
	}
	_, _, _, warning := agg.getRatingByPriorityWithSource(results, []string{"r18dev", "dmm"})
	assert.Contains(t, warning, "r18dev", "skipped-source warning should name the source (NIT-11)")
	assert.Contains(t, warning, "85.00", "warning should report the skipped score")

	// No skips -> no warning.
	results["r18dev"].Rating.Score = 6.0
	_, _, _, warning = agg.getRatingByPriorityWithSource(results, []string{"r18dev", "dmm"})
	assert.Empty(t, warning, "no out-of-range skip should yield no warning")
}
