package aggregator

import (
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
				Title:       []string{"r18dev", "dmm"},
				Description: []string{"dmm", "r18dev"},
			},
		},
	}

	agg := New(cfg)

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

	movie, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Title should use r18dev (first priority)
	assert.Equal(t, "R18 Title", movie.Title)
	// Description should use dmm (first priority)
	assert.Equal(t, "DMM Description", movie.Description)
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

	agg := New(cfg)

	movie, err := agg.Aggregate([]*models.ScraperResult{})
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
				Title: []string{}, // Empty - should use global
			},
		},
	}

	agg := New(cfg)

	// Verify resolved priorities
	assert.Equal(t, []string{"r18dev", "dmm"}, agg.resolvedPriorities["Title"])
}

// TestValidateRequiredFields_AdditionalFields tests validation for less commonly validated fields
func TestValidateRequiredFields_AdditionalFields(t *testing.T) {
	tests := []struct {
		name           string
		requiredFields []string
		movie          *models.Movie
		expectError    bool
		errorContains  string
	}{
		{
			name:           "missing director",
			requiredFields: []string{"director"},
			movie: &models.Movie{
				ID:    "IPX-001",
				Title: "Test Movie",
				// Director is missing
			},
			expectError:   true,
			errorContains: "Director",
		},
		{
			name:           "missing label",
			requiredFields: []string{"label"},
			movie: &models.Movie{
				ID:    "IPX-001",
				Title: "Test Movie",
				// Label is missing
			},
			expectError:   true,
			errorContains: "Label",
		},
		{
			name:           "missing series with alias 'set'",
			requiredFields: []string{"set"},
			movie: &models.Movie{
				ID:    "IPX-001",
				Title: "Test Movie",
				// Series is missing
			},
			expectError:   true,
			errorContains: "Series",
		},
		{
			name:           "missing runtime",
			requiredFields: []string{"runtime"},
			movie: &models.Movie{
				ID:      "IPX-001",
				Title:   "Test Movie",
				Runtime: 0, // Missing
			},
			expectError:   true,
			errorContains: "Runtime",
		},
		{
			name:           "missing posterurl with alias 'poster'",
			requiredFields: []string{"poster"},
			movie: &models.Movie{
				ID:    "IPX-001",
				Title: "Test Movie",
				// PosterURL is missing
			},
			expectError:   true,
			errorContains: "PosterURL",
		},
		{
			name:           "all fields present",
			requiredFields: []string{"director", "label", "runtime", "poster"},
			movie: &models.Movie{
				ID:        "IPX-001",
				Title:     "Test Movie",
				Director:  "John Doe",
				Label:     "Test Label",
				Runtime:   120,
				PosterURL: "http://example.com/poster.jpg",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Metadata: config.MetadataConfig{
					RequiredFields: tt.requiredFields,
				},
				Scrapers: config.ScrapersConfig{
					Priority: []string{"r18dev"},
				},
			}

			agg := New(cfg)
			err := agg.validateRequiredFields(tt.movie)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestLoadCachesFunctions tests cache loading with nil repositories
func TestLoadCachesFunctions(t *testing.T) {
	t.Run("loadGenreReplacementCache with nil repo", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority: []string{"r18dev"},
			},
		}

		agg := New(cfg)
		// genreReplacementRepo is nil by default
		assert.Nil(t, agg.genreReplacementRepo)

		// Should not panic when repo is nil
		agg.loadGenreReplacementCache()

		// Cache should remain empty
		agg.genreCacheMutex.RLock()
		assert.Empty(t, agg.genreReplacementCache)
		agg.genreCacheMutex.RUnlock()
	})

	t.Run("loadActressAliasCache with nil repo", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority: []string{"r18dev"},
			},
		}

		agg := New(cfg)
		// actressAliasRepo is nil by default
		assert.Nil(t, agg.actressAliasRepo)

		// Should not panic when repo is nil
		agg.loadActressAliasCache()

		// Cache should remain empty
		agg.aliasCacheMutex.RLock()
		assert.Empty(t, agg.actressAliasCache)
		agg.aliasCacheMutex.RUnlock()
	})
}

// TestGetFieldByPriority tests priority-based field selection
func TestGetFieldByPriority(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
	}

	agg := New(cfg)

	results := map[string]*models.ScraperResult{
		"r18dev": {
			Source: "r18dev",
			Title:  "R18 Title",
			Maker:  "", // Empty
		},
		"dmm": {
			Source: "dmm",
			Title:  "DMM Title",
			Maker:  "DMM Maker",
		},
	}

	// Test string field - should get first non-empty
	title := agg.getFieldByPriority(results, []string{"r18dev", "dmm"}, func(r *models.ScraperResult) string {
		return r.Title
	})
	assert.Equal(t, "R18 Title", title)

	// Test with first empty - should fall back to second
	maker := agg.getFieldByPriority(results, []string{"r18dev", "dmm"}, func(r *models.ScraperResult) string {
		return r.Maker
	})
	assert.Equal(t, "DMM Maker", maker)

	// Test with non-existent source
	empty := agg.getFieldByPriority(results, []string{"nonexistent"}, func(r *models.ScraperResult) string {
		return r.Title
	})
	assert.Equal(t, "", empty)
}

// TestGetIntFieldByPriority tests integer field selection
func TestGetIntFieldByPriority(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
	}

	agg := New(cfg)

	results := map[string]*models.ScraperResult{
		"r18dev": {
			Source:  "r18dev",
			Runtime: 0, // Zero value
		},
		"dmm": {
			Source:  "dmm",
			Runtime: 120,
		},
	}

	runtime := agg.getIntFieldByPriority(results, []string{"r18dev", "dmm"}, func(r *models.ScraperResult) int {
		return r.Runtime
	})
	assert.Equal(t, 120, runtime)

	// Test with both having values - should use priority
	results["r18dev"].Runtime = 115
	runtime = agg.getIntFieldByPriority(results, []string{"r18dev", "dmm"}, func(r *models.ScraperResult) int {
		return r.Runtime
	})
	assert.Equal(t, 115, runtime)
}

// TestGetTimeFieldByPriority tests time pointer field selection
func TestGetTimeFieldByPriority(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
	}

	agg := New(cfg)

	date1 := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2021, 1, 10, 0, 0, 0, 0, time.UTC)

	results := map[string]*models.ScraperResult{
		"r18dev": {
			Source:      "r18dev",
			ReleaseDate: nil, // Nil value
		},
		"dmm": {
			Source:      "dmm",
			ReleaseDate: &date2,
		},
	}

	releaseDate := agg.getTimeFieldByPriority(results, []string{"r18dev", "dmm"}, func(r *models.ScraperResult) *time.Time {
		return r.ReleaseDate
	})
	assert.Equal(t, date2, *releaseDate)

	// Test with both having values - should use priority
	results["r18dev"].ReleaseDate = &date1
	releaseDate = agg.getTimeFieldByPriority(results, []string{"r18dev", "dmm"}, func(r *models.ScraperResult) *time.Time {
		return r.ReleaseDate
	})
	assert.Equal(t, date1, *releaseDate)
}

// TestGetRatingByPriority tests rating field selection
func TestGetRatingByPriority(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
	}

	agg := New(cfg)

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

	score, votes := agg.getRatingByPriority(results, []string{"r18dev", "dmm"})
	assert.Equal(t, 4.5, score)
	assert.Equal(t, 100, votes)

	// Test with both having values - should use priority
	results["r18dev"].Rating = &models.Rating{
		Score: 5.0,
		Votes: 200,
	}
	score, votes = agg.getRatingByPriority(results, []string{"r18dev", "dmm"})
	assert.Equal(t, 5.0, score)
	assert.Equal(t, 200, votes)

	// Test with zero values ignored
	results["r18dev"].Rating = &models.Rating{
		Score: 0,
		Votes: 0,
	}
	score, votes = agg.getRatingByPriority(results, []string{"r18dev", "dmm"})
	assert.Equal(t, 4.5, score)
	assert.Equal(t, 100, votes)
}

// TestGetActressesByPriority tests actress aggregation and merging
func TestGetActressesByPriority(t *testing.T) {
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

	agg := New(cfg)

	results := map[string]*models.ScraperResult{
		"r18dev": {
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
		"dmm": {
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

	actresses := agg.getActressesByPriority(results, []string{"r18dev", "dmm"})
	require.Len(t, actresses, 1)

	// Should merge data from both sources
	assert.Equal(t, "Yui", actresses[0].FirstName)
	assert.Equal(t, "Hatano", actresses[0].LastName)
	assert.Equal(t, "波多野結衣", actresses[0].JapaneseName)
	assert.Equal(t, 12345, actresses[0].DMMID)
	assert.Equal(t, "https://example.com/thumb.jpg", actresses[0].ThumbURL)
}

// TestGetActressesByPriorityMultiple tests multiple actresses
func TestGetActressesByPriorityMultiple(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			ActressDatabase: config.ActressDatabaseConfig{
				ConvertAlias: false,
			},
		},
	}

	agg := New(cfg)

	results := map[string]*models.ScraperResult{
		"r18dev": {
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

	actresses := agg.getActressesByPriority(results, []string{"r18dev"})
	require.Len(t, actresses, 2)
}

// TestGetActressesByPriorityUnknownText tests unknown actress text
func TestGetActressesByPriorityUnknownText(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			ActressDatabase: config.ActressDatabaseConfig{
				ConvertAlias: false,
			},
			NFO: config.NFOConfig{
				UnknownActressText: "Unknown",
			},
		},
	}

	agg := New(cfg)

	results := map[string]*models.ScraperResult{
		"r18dev": {
			Source:    "r18dev",
			Actresses: []models.ActressInfo{}, // Empty
		},
	}

	actresses := agg.getActressesByPriority(results, []string{"r18dev"})
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

	agg := New(cfg)

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
	genres := agg.getGenresByPriority(results, []string{"r18dev", "dmm"})
	assert.Equal(t, []string{"Drama", "Romance"}, genres)

	// Empty genres should fall back to next
	results["r18dev"].Genres = []string{}
	genres = agg.getGenresByPriority(results, []string{"r18dev", "dmm"})
	assert.Equal(t, []string{"Action", "Comedy"}, genres)
}

// TestGetScreenshotsByPriority tests screenshot URL selection
func TestGetScreenshotsByPriority(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
	}

	agg := New(cfg)

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

	screenshots := agg.getScreenshotsByPriority(results, []string{"r18dev", "dmm"})
	assert.Equal(t, []string{"https://r18.com/1.jpg", "https://r18.com/2.jpg"}, screenshots)

	// Empty should fall back
	results["r18dev"].ScreenshotURL = []string{}
	screenshots = agg.getScreenshotsByPriority(results, []string{"r18dev", "dmm"})
	assert.Equal(t, []string{"https://dmm.com/1.jpg"}, screenshots)
}

// TestBuildTranslations tests translation building
func TestBuildTranslations(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
	}

	agg := New(cfg)

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

	translations := agg.buildTranslations(results)
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

	agg := New(cfg)

	results := []*models.ScraperResult{
		{
			Source:   "r18dev",
			Language: "", // No language
			Title:    "Title",
		},
	}

	translations := agg.buildTranslations(results)
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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.AutoMigrate()
	require.NoError(t, err)

	// Add genre replacement
	repo := database.NewGenreReplacementRepository(db)
	err = repo.Create(&models.GenreReplacement{
		Original:    "ドラマ",
		Replacement: "Drama",
	})
	require.NoError(t, err)

	agg := NewWithDatabase(cfg, db)

	// Test replacement
	result := agg.applyGenreReplacement("ドラマ")
	assert.Equal(t, "Drama", result)

	// Test non-existent
	result = agg.applyGenreReplacement("Unknown")
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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.AutoMigrate()
	require.NoError(t, err)

	agg := NewWithDatabase(cfg, db)

	// Apply to new genre - should auto-add
	result := agg.applyGenreReplacement("NewGenre")
	assert.Equal(t, "NewGenre", result)

	// Verify it was added to database
	repo := database.NewGenreReplacementRepository(db)
	replacement, err := repo.FindByOriginal("NewGenre")
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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.AutoMigrate()
	require.NoError(t, err)

	agg := NewWithDatabase(cfg, db)

	// Initially no replacement
	result := agg.applyGenreReplacement("TestGenre")
	assert.Equal(t, "TestGenre", result)

	// Add replacement directly to database
	repo := database.NewGenreReplacementRepository(db)
	err = repo.Create(&models.GenreReplacement{
		Original:    "TestGenre",
		Replacement: "Replaced",
	})
	require.NoError(t, err)

	// Should still return original (cache not reloaded)
	result = agg.applyGenreReplacement("TestGenre")
	assert.Equal(t, "TestGenre", result)

	// Reload cache
	agg.ReloadGenreReplacements()

	// Should now return replacement
	result = agg.applyGenreReplacement("TestGenre")
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
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				ID:          []string{"r18dev", "dmm"},
				Title:       []string{"dmm"},
				Description: []string{},
			},
		},
	}

	// Test defined field
	priority := getFieldPriorityFromConfig(cfg, "ID")
	assert.Equal(t, []string{"r18dev", "dmm"}, priority)

	// Test different field
	priority = getFieldPriorityFromConfig(cfg, "Title")
	assert.Equal(t, []string{"dmm"}, priority)

	// Test empty array
	priority = getFieldPriorityFromConfig(cfg, "Description")
	assert.Equal(t, []string{}, priority)

	// Test unknown field
	priority = getFieldPriorityFromConfig(cfg, "UnknownField")
	assert.Nil(t, priority)
}

// TestAggregateSourceMetadata tests that source name and URL are set
func TestAggregateSourceMetadata(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	agg := New(cfg)

	results := []*models.ScraperResult{
		{
			Source:    "r18dev",
			SourceURL: "https://r18.dev/movie/12345",
			ID:        "IPX-001",
		},
	}

	movie, err := agg.Aggregate(results)
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

	agg := New(cfg)

	before := time.Now().UTC()

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "IPX-001",
		},
	}

	movie, err := agg.Aggregate(results)
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
	}

	agg := New(cfg)

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

	movie, err := agg.Aggregate(results)
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
	assert.Equal(t, "https://example.com/poster.jpg", movie.PosterURL)
	assert.Equal(t, "https://example.com/cover.jpg", movie.CoverURL)
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

// TestNewAggregatorResolvesDefaultPriority tests default priority fallback
func TestNewAggregatorResolvesDefaultPriority(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{}, // Empty global priority
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{},
		},
	}

	agg := New(cfg)

	// Should fall back to sensible default
	assert.Equal(t, []string{"r18dev", "dmm"}, agg.resolvedPriorities["Title"])
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

	agg := New(cfg)

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "IPX-001",
			Genres: []string{"Drama", "Sample", "Romance", "Featured Actress"},
		},
	}

	movie, err := agg.Aggregate(results)
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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.AutoMigrate()
	require.NoError(t, err)

	// Add genre replacements
	repo := database.NewGenreReplacementRepository(db)
	err = repo.Create(&models.GenreReplacement{
		Original:    "ドラマ",
		Replacement: "Drama",
	})
	require.NoError(t, err)

	agg := NewWithDatabase(cfg, db)

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "IPX-001",
			Genres: []string{"ドラマ", "Romance"},
		},
	}

	movie, err := agg.Aggregate(results)
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

	agg := New(cfg)

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

	movie, err := agg.Aggregate(results)
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

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.AutoMigrate()
	require.NoError(t, err)

	// Add actress alias
	aliasRepo := database.NewActressAliasRepository(db)
	err = aliasRepo.Create(&models.ActressAlias{
		AliasName:     "Yui Hatano",
		CanonicalName: "Hatano Yui",
	})
	require.NoError(t, err)

	agg := NewWithDatabase(cfg, db)

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

	movie, err := agg.Aggregate(results)
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

	agg := New(cfg)

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

	movie, err := agg.Aggregate(results)
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

	agg := New(cfg)

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

		movie, err := agg.Aggregate(results)
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

		movie, err := agg.Aggregate(results)
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

		movie, err := agg.Aggregate(results)
		assert.Error(t, err)
		assert.Nil(t, movie)
		assert.Contains(t, err.Error(), "ID")
		assert.Contains(t, err.Error(), "Title")
	})
}

// TestAggregateDisplayNameTemplate tests NFO display name template generation
func TestAggregateDisplayNameTemplate(t *testing.T) {
	t.Run("valid template", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority: []string{"r18dev"},
			},
			Metadata: config.MetadataConfig{
				NFO: config.NFOConfig{
					DisplayName: "[<ID>] <TITLE>",
				},
			},
		}

		agg := New(cfg)

		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Test Movie",
			},
		}

		movie, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		assert.Equal(t, "[IPX-001] Test Movie", movie.DisplayName)
	})

	t.Run("template with multiple fields", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority: []string{"r18dev"},
			},
			Metadata: config.MetadataConfig{
				NFO: config.NFOConfig{
					DisplayName: "<TITLE> by <STUDIO> (<YEAR>)",
				},
			},
		}

		agg := New(cfg)

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

		movie, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		assert.Equal(t, "Amazing Movie by Idea Pocket (2021)", movie.DisplayName)
	})

	t.Run("empty template - no display name", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority: []string{"r18dev"},
			},
			Metadata: config.MetadataConfig{
				NFO: config.NFOConfig{
					DisplayName: "",
				},
			},
		}

		agg := New(cfg)

		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Test Movie",
			},
		}

		movie, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		assert.Empty(t, movie.DisplayName)
	})

	t.Run("invalid template - silently ignored", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority: []string{"r18dev"},
			},
			Metadata: config.MetadataConfig{
				NFO: config.NFOConfig{
					DisplayName: "<INVALID_TAG>",
				},
			},
		}

		agg := New(cfg)

		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Test Movie",
			},
		}

		movie, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		// Invalid template should be silently ignored
		assert.Empty(t, movie.DisplayName)
	})
}

// TestActressDMMIDDeduplicationSameIDDifferentNames tests that actresses with the same DMMID
// but different names from multiple scrapers are deduplicated into a single actress.
// This is the core scenario mentioned in Story 2.4 AC-2.4.2:
// "r18dev and dmm may return same actress with different names but same DMMID"
func TestActressDMMIDDeduplicationSameIDDifferentNames(t *testing.T) {
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

	agg := New(cfg)

	results := map[string]*models.ScraperResult{
		"r18dev": {
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
		"dmm": {
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

	actresses := agg.getActressesByPriority(results, []string{"r18dev", "dmm"})

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

	agg := New(cfg)

	results := map[string]*models.ScraperResult{
		"r18dev": {
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
		"dmm": {
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

	actresses := agg.getActressesByPriority(results, []string{"r18dev", "dmm"})

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
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm", "javlibrary"},
		},
		Metadata: config.MetadataConfig{
			ActressDatabase: config.ActressDatabaseConfig{
				ConvertAlias: false,
			},
		},
	}

	agg := New(cfg)

	results := map[string]*models.ScraperResult{
		"r18dev": {
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
		"dmm": {
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
		"javlibrary": {
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

	actresses := agg.getActressesByPriority(results, []string{"r18dev", "dmm", "javlibrary"})

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
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "javlibrary"},
		},
		Metadata: config.MetadataConfig{
			ActressDatabase: config.ActressDatabaseConfig{
				ConvertAlias: false,
			},
		},
	}

	agg := New(cfg)

	results := map[string]*models.ScraperResult{
		"r18dev": {
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
		"javlibrary": {
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

	actresses := agg.getActressesByPriority(results, []string{"r18dev", "javlibrary"})

	// Should have exactly 1 actress (merged by name since both have same name)
	require.Len(t, actresses, 1, "Should merge actresses with same name even without DMMID")

	// Should use data from r18dev (higher priority)
	assert.Equal(t, 0, actresses[0].DMMID)
	assert.Equal(t, "Unknown", actresses[0].FirstName)
	assert.Equal(t, "Actress", actresses[0].LastName)
	assert.Equal(t, "未知の女優", actresses[0].JapaneseName)
	assert.Equal(t, "https://r18dev.example.com/thumb1.jpg", actresses[0].ThumbURL)
}
