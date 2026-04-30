package aggregator

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsRegexPattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		expected bool
	}{
		{"Starts with caret", "^Featured", true},
		{"Ends with dollar", "mosaic$", true},
		{"Contains dot star", ".*mosaic.*", true},
		{"Contains dot plus", ".+test", true},
		{"Contains backslash", "\\d+", true},
		{"Contains brackets", "[0-9]", true},
		{"Contains parentheses", "(test)", true},
		{"Contains pipe", "test|demo", true},
		{"Contains question mark", "test?", true},
		{"Contains asterisk", "test*", true},
		{"Contains plus", "test+", true},
		{"Plain string", "Featured Actress", false},
		{"Plain string with space", "Big Tits", false},
		{"Empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRegexPattern(tt.pattern)
			if result != tt.expected {
				t.Errorf("isRegexPattern(%q) = %v, want %v", tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestCompileGenreRegexes(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		wantLen  int
	}{
		{
			name:     "Only regex patterns",
			patterns: []string{"^Featured", ".*mosaic.*", "test$"},
			wantLen:  3,
		},
		{
			name:     "Mixed regex and plain",
			patterns: []string{"^Featured", "Plain Text", ".*mosaic.*"},
			wantLen:  2,
		},
		{
			name:     "Only plain strings",
			patterns: []string{"Featured Actress", "Big Tits"},
			wantLen:  0,
		},
		{
			name:     "Empty list",
			patterns: []string{},
			wantLen:  0,
		},
		{
			name:     "Invalid regex",
			patterns: []string{"^(unclosed", "valid$"},
			wantLen:  1, // Only valid one compiles
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Metadata: config.MetadataConfig{
					IgnoreGenres: tt.patterns,
				},
			}

			agg := New(cfg)

			if len(agg.ignoreGenreRegexes) != tt.wantLen {
				t.Errorf("compileGenreRegexes() compiled %d patterns, want %d",
					len(agg.ignoreGenreRegexes), tt.wantLen)
			}
		})
	}
}

func TestIsGenreIgnored(t *testing.T) {
	tests := []struct {
		name         string
		ignoreGenres []string
		genreToTest  string
		shouldIgnore bool
	}{
		{
			name:         "Exact match",
			ignoreGenres: []string{"Featured Actress", "Sample"},
			genreToTest:  "Featured Actress",
			shouldIgnore: true,
		},
		{
			name:         "Regex prefix match",
			ignoreGenres: []string{"^Featured"},
			genreToTest:  "Featured Actress",
			shouldIgnore: true,
		},
		{
			name:         "Regex suffix match with space",
			ignoreGenres: []string{"mosaic$"},
			genreToTest:  "HD mosaic",
			shouldIgnore: true, // Ends with "mosaic"
		},
		{
			name:         "Regex suffix match success",
			ignoreGenres: []string{"mosaic$"},
			genreToTest:  "mosaic",
			shouldIgnore: true,
		},
		{
			name:         "Regex contains match",
			ignoreGenres: []string{".*mosaic.*"},
			genreToTest:  "HD mosaic available",
			shouldIgnore: true,
		},
		{
			name:         "Multiple patterns first matches",
			ignoreGenres: []string{"^Featured", ".*mosaic.*", "Sample"},
			genreToTest:  "Featured Actress",
			shouldIgnore: true,
		},
		{
			name:         "Multiple patterns second matches",
			ignoreGenres: []string{"^Featured", ".*mosaic.*", "Sample"},
			genreToTest:  "HD mosaic",
			shouldIgnore: true,
		},
		{
			name:         "Multiple patterns third matches",
			ignoreGenres: []string{"^Featured", ".*mosaic.*", "Sample"},
			genreToTest:  "Sample",
			shouldIgnore: true,
		},
		{
			name:         "No match",
			ignoreGenres: []string{"^Featured", ".*mosaic.*"},
			genreToTest:  "Beautiful Girl",
			shouldIgnore: false,
		},
		{
			name:         "Case sensitive exact",
			ignoreGenres: []string{"Sample"},
			genreToTest:  "sample",
			shouldIgnore: false,
		},
		{
			name:         "Case sensitive regex",
			ignoreGenres: []string{"^featured"},
			genreToTest:  "Featured Actress",
			shouldIgnore: false,
		},
		{
			name:         "Case insensitive regex",
			ignoreGenres: []string{"(?i)^featured"},
			genreToTest:  "Featured Actress",
			shouldIgnore: true,
		},
		{
			name:         "Complex regex",
			ignoreGenres: []string{"^(HD|4K|VR)"},
			genreToTest:  "HD",
			shouldIgnore: true,
		},
		{
			name:         "Complex regex no match",
			ignoreGenres: []string{"^(HD|4K|VR)"},
			genreToTest:  "Beautiful Girl",
			shouldIgnore: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Metadata: config.MetadataConfig{
					IgnoreGenres: tt.ignoreGenres,
				},
			}

			agg := New(cfg)
			result := agg.isGenreIgnored(tt.genreToTest)

			if result != tt.shouldIgnore {
				t.Errorf("isGenreIgnored(%q) = %v, want %v",
					tt.genreToTest, result, tt.shouldIgnore)
			}
		})
	}
}

func TestGenreFilteringIntegration(t *testing.T) {
	// This test verifies that regex patterns work end-to-end in genre filtering
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"test"},
		},
		Metadata: config.MetadataConfig{
			IgnoreGenres: []string{
				"^Featured",  // Regex: starts with "Featured"
				".*mosaic.*", // Regex: contains "mosaic"
				"Sample",     // Exact: exactly "Sample"
				"^(HD|4K)",   // Regex: starts with HD or 4K
			},
		},
	}

	agg := New(cfg)

	// Verify regex compilation
	if len(agg.ignoreGenreRegexes) != 3 {
		t.Errorf("Expected 3 compiled regex patterns, got %d", len(agg.ignoreGenreRegexes))
	}

	// Test genres that should be filtered
	shouldFilter := []string{
		"Featured Actress", // Matches ^Featured
		"HD mosaic",        // Matches .*mosaic.*
		"Sample",           // Exact match
		"HD",               // Matches ^(HD|4K)
		"4K",               // Matches ^(HD|4K)
		"mosaic version",   // Matches .*mosaic.*
	}

	for _, genre := range shouldFilter {
		if !agg.isGenreIgnored(genre) {
			t.Errorf("Genre %q should be filtered but wasn't", genre)
		}
	}

	// Test genres that should NOT be filtered
	shouldKeep := []string{
		"Beautiful Girl",
		"Blowjob",
		"Creampie",
		"featured actress", // Case sensitive
		"High Definition",  // Not "HD"
	}

	for _, genre := range shouldKeep {
		if agg.isGenreIgnored(genre) {
			t.Errorf("Genre %q should be kept but was filtered", genre)
		}
	}
}

func TestGenreAutoAdd(t *testing.T) {
	// Create a temporary database
	db, err := database.New(&config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:", // In-memory database for testing
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Run migrations
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	tests := []struct {
		name        string
		enabled     bool
		autoAdd     bool
		genreName   string
		shouldExist bool
	}{
		{
			name:        "Auto-add enabled - new genre",
			enabled:     true,
			autoAdd:     true,
			genreName:   "New Genre",
			shouldExist: true,
		},
		{
			name:        "Auto-add disabled - new genre",
			enabled:     true,
			autoAdd:     false,
			genreName:   "Another Genre",
			shouldExist: false,
		},
		{
			name:        "Genre replacement disabled - auto-add ignored",
			enabled:     false,
			autoAdd:     true,
			genreName:   "Disabled Genre",
			shouldExist: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					Priority: []string{"test"},
				},
				Metadata: config.MetadataConfig{
					GenreReplacement: config.GenreReplacementConfig{
						Enabled: tt.enabled,
						AutoAdd: tt.autoAdd,
					},
				},
			}

			agg := NewWithDatabase(cfg, db)

			// Apply genre replacement (which triggers auto-add)
			result := agg.applyGenreReplacement(tt.genreName)

			// Result should always be the original genre name
			if result != tt.genreName {
				t.Errorf("Expected result '%s', got '%s'", tt.genreName, result)
			}

			// Check if genre exists in database
			repo := database.NewGenreReplacementRepository(db)
			replacement, err := repo.FindByOriginal(tt.genreName)

			if tt.shouldExist {
				if err != nil {
					t.Errorf("Expected genre to exist in database, but got error: %v", err)
				}
				if replacement.Original != tt.genreName {
					t.Errorf("Expected original '%s', got '%s'", tt.genreName, replacement.Original)
				}
				if replacement.Replacement != tt.genreName {
					t.Errorf("Expected replacement '%s', got '%s'", tt.genreName, replacement.Replacement)
				}
			} else {
				if err == nil {
					t.Error("Expected genre to not exist in database, but it does")
				}
			}
		})
	}
}

func TestGenreReplacementDisabledIgnoresExistingMappings(t *testing.T) {
	db, err := database.New(&config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	repo := database.NewGenreReplacementRepository(db)
	require.NoError(t, repo.Create(&models.GenreReplacement{
		Original:    "Drama",
		Replacement: "ドラマ",
	}))

	aggEnabled := NewWithDatabase(&config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"test"},
		},
		Metadata: config.MetadataConfig{
			GenreReplacement: config.GenreReplacementConfig{
				Enabled: true,
			},
		},
	}, db)
	assert.Equal(t, "ドラマ", aggEnabled.applyGenreReplacement("Drama"))

	aggDisabled := NewWithDatabase(&config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"test"},
		},
		Metadata: config.MetadataConfig{
			GenreReplacement: config.GenreReplacementConfig{
				Enabled: false,
			},
		},
	}, db)
	assert.Equal(t, "Drama", aggDisabled.applyGenreReplacement("Drama"))
}

func TestDisplayTitleFormatting(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev"},
			},
			NFO: config.NFOConfig{
				DisplayTitle: "[<ID>] <TITLE>",
			},
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
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

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Verify display name was formatted correctly
	assert.Equal(t, "[IPX-001] Test Movie", movie.DisplayTitle)
}

func TestDisplayTitleFormattingWithTemplate(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev"},
			},
			NFO: config.NFOConfig{
				DisplayTitle: "<TITLE> by <STUDIO>",
			},
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	agg := New(cfg)

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "IPX-001",
			Title:  "Amazing Movie",
			Maker:  "Idea Pocket",
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Verify display name was formatted correctly
	assert.Equal(t, "Amazing Movie by Idea Pocket", movie.DisplayTitle)
}

func TestDisplayTitleEmpty(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev"},
			},
			NFO: config.NFOConfig{
				DisplayTitle: "", // Empty - should not set DisplayTitle
			},
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	agg := New(cfg)

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "IPX-001",
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// DisplayTitle should be empty when not configured
	assert.Empty(t, movie.DisplayTitle)
}

func TestRequiredFieldsValidation(t *testing.T) {
	// Helper to create time pointer
	timePtr := func(s string) *time.Time {
		t, _ := time.Parse("2006-01-02", s)
		return &t
	}

	tests := []struct {
		name           string
		requiredFields []string
		movie          *models.ScraperResult
		shouldPass     bool
		expectedError  string
	}{
		{
			name:           "All required fields present",
			requiredFields: []string{"ID", "Title", "ReleaseDate"},
			movie: &models.ScraperResult{
				Source:      "r18dev",
				ID:          "IPX-001",
				Title:       "Test Movie",
				ReleaseDate: timePtr("2023-01-15"),
			},
			shouldPass: true,
		},
		{
			name:           "Missing single required field",
			requiredFields: []string{"ID", "Title", "Director"},
			movie: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Test Movie",
				// Director missing
			},
			shouldPass:    false,
			expectedError: "missing required fields: Director",
		},
		{
			name:           "Missing multiple required fields",
			requiredFields: []string{"ID", "Title", "Director", "Maker", "ReleaseDate"},
			movie: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				// Title, Director, Maker, ReleaseDate missing
			},
			shouldPass:    false,
			expectedError: "missing required fields: Title, Director, Maker, ReleaseDate",
		},
		{
			name:           "Case insensitive field names",
			requiredFields: []string{"id", "TITLE", "ReLEaSeDate"},
			movie: &models.ScraperResult{
				Source:      "r18dev",
				ID:          "IPX-001",
				Title:       "Test Movie",
				ReleaseDate: timePtr("2023-01-15"),
			},
			shouldPass: true,
		},
		{
			name:           "Field name aliases - CoverURL",
			requiredFields: []string{"cover_url", "poster"},
			movie: &models.ScraperResult{
				Source:    "r18dev",
				ID:        "IPX-001",
				CoverURL:  "https://example.com/cover.jpg",
				PosterURL: "https://example.com/poster.jpg",
			},
			shouldPass: true,
		},
		{
			name:           "Field name aliases - ContentID",
			requiredFields: []string{"content_id"},
			movie: &models.ScraperResult{
				Source:    "r18dev",
				ID:        "IPX-001",
				ContentID: "ipx00001",
			},
			shouldPass: true,
		},
		{
			name:           "Field name aliases - RatingScore",
			requiredFields: []string{"rating_score"},
			movie: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				Rating: &models.Rating{Score: 4.5, Votes: 100},
			},
			shouldPass: true,
		},
		{
			name:           "Empty required fields list",
			requiredFields: []string{},
			movie: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				// Minimal data
			},
			shouldPass: true,
		},
		{
			name:           "Unknown field names ignored",
			requiredFields: []string{"ID", "UnknownField", "AnotherUnknownField"},
			movie: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
			},
			shouldPass: true, // Unknown fields are ignored for forward compatibility
		},
		{
			name:           "Array field - Actresses required and present",
			requiredFields: []string{"actresses"},
			movie: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				Actresses: []models.ActressInfo{
					{FirstName: "Actress", LastName: "One"},
					{FirstName: "Actress", LastName: "Two"},
				},
			},
			shouldPass: true,
		},
		{
			name:           "Array field - Actresses required but empty",
			requiredFields: []string{"actresses"},
			movie: &models.ScraperResult{
				Source:    "r18dev",
				ID:        "IPX-001",
				Actresses: []models.ActressInfo{},
			},
			shouldPass:    false,
			expectedError: "missing required fields: Actresses",
		},
		{
			name:           "Array field - Genres required and present",
			requiredFields: []string{"genres"},
			movie: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				Genres: []string{"Drama", "Romance"},
			},
			shouldPass: true,
		},
		{
			name:           "Array field - Screenshots required and present",
			requiredFields: []string{"screenshots"},
			movie: &models.ScraperResult{
				Source:        "r18dev",
				ID:            "IPX-001",
				ScreenshotURL: []string{"https://example.com/ss1.jpg"},
			},
			shouldPass: true,
		},
		{
			name:           "Numeric field - Runtime required and present",
			requiredFields: []string{"runtime"},
			movie: &models.ScraperResult{
				Source:  "r18dev",
				ID:      "IPX-001",
				Runtime: 120,
			},
			shouldPass: true,
		},
		{
			name:           "Numeric field - Runtime required but zero",
			requiredFields: []string{"runtime"},
			movie: &models.ScraperResult{
				Source:  "r18dev",
				ID:      "IPX-001",
				Runtime: 0,
			},
			shouldPass:    false,
			expectedError: "missing required fields: Runtime",
		},
		{
			name:           "Numeric field - RatingScore zero is valid",
			requiredFields: []string{"rating"},
			movie: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				Rating: &models.Rating{Score: 0, Votes: 0},
			},
			shouldPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Metadata: config.MetadataConfig{
					RequiredFields: tt.requiredFields,
					Priority: config.PriorityConfig{
						Priority: []string{"r18dev"},
					},
				},
				Scrapers: config.ScrapersConfig{
					Priority: []string{"r18dev"},
				},
			}

			agg := New(cfg)
			results := []*models.ScraperResult{tt.movie}

			movie, _, err := agg.Aggregate(results)

			if tt.shouldPass {
				require.NoError(t, err, "Expected validation to pass but got error")
				require.NotNil(t, movie, "Expected movie to be returned")
			} else {
				require.Error(t, err, "Expected validation to fail but got no error")
				assert.Contains(t, err.Error(), tt.expectedError,
					"Error message should contain expected text")
				assert.Nil(t, movie, "Expected no movie when validation fails")
			}
		})
	}
}

func TestRequiredFieldsValidationAliases(t *testing.T) {
	// Test that all aliases for the same field work identically
	// Note: RatingScore is intentionally excluded because it has special handling:
	// RatingScore == 0 is treated as valid (not missing) because we cannot distinguish
	// "not scraped" from "intentionally 0" at validation time.
	// This behavior is tested separately in TestRequiredFieldsValidation.
	aliasGroups := map[string][]string{
		"ContentID":     {"contentid", "content_id", "CONTENTID"},
		"CoverURL":      {"coverurl", "cover_url", "cover", "COVER"},
		"PosterURL":     {"posterurl", "poster_url", "poster"},
		"TrailerURL":    {"trailerurl", "trailer_url", "trailer"},
		"Screenshots":   {"screenshots", "screenshot_url", "screenshoturl", "SCREENSHOTURL"},
		"OriginalTitle": {"originaltitle", "original_title"},
		"ReleaseDate":   {"releasedate", "release_date"},
	}

	for fieldName, aliases := range aliasGroups {
		t.Run(fieldName, func(t *testing.T) {
			// Create a movie with the field missing
			movie := &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
			}

			for _, alias := range aliases {
				cfg := &config.Config{
					Metadata: config.MetadataConfig{
						RequiredFields: []string{alias},
						Priority: config.PriorityConfig{
							Priority: []string{"r18dev"},
						},
					},
					Scrapers: config.ScrapersConfig{
						Priority: []string{"r18dev"},
					},
				}

				agg := New(cfg)
				results := []*models.ScraperResult{movie}

				_, _, err := agg.Aggregate(results)
				require.Error(t, err,
					"Alias %q should trigger validation error for missing %s", alias, fieldName)
				assert.Contains(t, err.Error(), fieldName,
					"Error should mention canonical field name %s when using alias %q", fieldName, alias)
			}
		})
	}
}

// TestAggregateErrorResilience tests that aggregation continues when some scrapers fail
// This validates AC-3.6.2: "Continue on individual scraper errors, only fail if ALL scrapers fail"
func TestAggregateErrorResilience(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm", "javlibrary"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev", "dmm", "javlibrary"},
			},
		},
	}

	agg := New(cfg)

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	t.Run("One scraper succeeds, others missing - should succeed", func(t *testing.T) {
		// Scenario: Only r18dev succeeds, dmm and javlibrary failed (not in results)
		results := []*models.ScraperResult{
			{
				Source:      "r18dev",
				ID:          "IPX-001",
				Title:       "R18 Title",
				Description: "R18 Description",
				ReleaseDate: &releaseDate,
				Runtime:     120,
			},
			// dmm failed - not included
			// javlibrary failed - not included
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err, "Should succeed with 1 successful scraper")
		require.NotNil(t, movie)

		// Should use data from the only successful scraper
		assert.Equal(t, "IPX-001", movie.ID)
		assert.Equal(t, "R18 Title", movie.Title)
		assert.Equal(t, "R18 Description", movie.Description)
		assert.Equal(t, 120, movie.Runtime)
	})

	t.Run("Two scrapers succeed with partial data - should merge", func(t *testing.T) {
		// Scenario: r18dev has title, dmm has description, javlibrary failed
		results := []*models.ScraperResult{
			{
				Source:      "r18dev",
				ID:          "IPX-001",
				Title:       "R18 Title",
				Description: "", // Empty
				ReleaseDate: &releaseDate,
			},
			{
				Source:      "dmm",
				ID:          "IPX-001",
				Title:       "", // Empty
				Description: "DMM Description",
				Runtime:     120,
			},
			// javlibrary failed - not included
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err, "Should succeed with 2 partial results")
		require.NotNil(t, movie)

		// Should use r18dev title (first priority)
		assert.Equal(t, "R18 Title", movie.Title)

		// Should use dmm description (first priority with data)
		assert.Equal(t, "DMM Description", movie.Description)

		// Should use dmm runtime (only source with data)
		assert.Equal(t, 120, movie.Runtime)
	})

	t.Run("All scrapers fail - should return error", func(t *testing.T) {
		// Scenario: All scrapers failed (empty results)
		results := []*models.ScraperResult{}

		movie, _, err := agg.Aggregate(results)
		assert.Error(t, err, "Should fail when all scrapers fail")
		assert.Nil(t, movie)
		assert.Contains(t, err.Error(), "no scraper results to aggregate")
	})

	t.Run("First priority scraper fails, fallback succeeds", func(t *testing.T) {
		// Scenario: r18dev failed, dmm succeeds
		results := []*models.ScraperResult{
			// r18dev failed - not included
			{
				Source:      "dmm",
				ID:          "IPX-001",
				Title:       "DMM Title",
				Description: "DMM Description",
				ReleaseDate: &releaseDate,
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err, "Should succeed with fallback scraper")
		require.NotNil(t, movie)

		// Should use dmm data (fallback from r18dev)
		assert.Equal(t, "DMM Title", movie.Title)
		assert.Equal(t, "DMM Description", movie.Description)
	})
}

// TestAggregateConcurrentCacheAccess tests concurrent access to genre and actress caches
// This validates AC-3.6.4: Race detector must pass for concurrent aggregation
func TestAggregateConcurrentCacheAccess(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  dbPath,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev", "dmm"},
			},
			GenreReplacement: config.GenreReplacementConfig{
				Enabled: true,
				AutoAdd: true,
			},
			ActressDatabase: config.ActressDatabaseConfig{
				Enabled:      true,
				ConvertAlias: true,
			},
			IgnoreGenres: []string{"^Featured", "Sample"},
		},
	}

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.AutoMigrate()
	require.NoError(t, err)

	// Pre-populate genre replacement
	genreRepo := database.NewGenreReplacementRepository(db)
	err = genreRepo.Create(&models.GenreReplacement{
		Original:    "ドラマ",
		Replacement: "Drama",
	})
	require.NoError(t, err)

	// Pre-populate actress alias
	aliasRepo := database.NewActressAliasRepository(db)
	err = aliasRepo.Create(&models.ActressAlias{
		AliasName:     "Yui Hatano",
		CanonicalName: "Hatano Yui",
	})
	require.NoError(t, err)

	agg := NewWithDatabase(cfg, db)

	// Create test data with genres and actresses that will trigger cache access
	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	createTestResult := func(id string) *models.ScraperResult {
		return &models.ScraperResult{
			Source:      "r18dev",
			Language:    "en",
			ID:          id,
			Title:       "Test Movie " + id,
			ReleaseDate: &releaseDate,
			Genres:      []string{"ドラマ", "Romance", "Sample", "Featured Actress"},
			Actresses: []models.ActressInfo{
				{
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
					DMMID:        12345,
				},
			},
		}
	}

	// Run multiple aggregations concurrently to test race conditions
	// This tests:
	// 1. Concurrent reads of genreReplacementCache in applyGenreReplacement()
	// 2. Concurrent reads of actressAliasCache in applyActressAlias()
	// 3. Concurrent reads of ignoreGenreRegexes in isGenreIgnored()
	t.Run("Concurrent aggregations with cache access", func(t *testing.T) {
		const numGoroutines = 10

		done := make(chan bool, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(iteration int) {
				defer func() { done <- true }()

				results := []*models.ScraperResult{createTestResult("IPX-" + fmt.Sprint(iteration))}
				movie, _, err := agg.Aggregate(results)

				// All should succeed
				if err != nil {
					t.Errorf("Goroutine %d: Unexpected error: %v", iteration, err)
					return
				}

				if movie == nil {
					t.Errorf("Goroutine %d: Expected movie, got nil", iteration)
					return
				}

				// Verify genre replacement worked (ドラマ -> Drama)
				foundDrama := false
				for _, genre := range movie.Genres {
					if genre.Name == "Drama" {
						foundDrama = true
					}
					// Verify ignored genres were filtered
					if genre.Name == "Sample" || genre.Name == "Featured Actress" {
						t.Errorf("Goroutine %d: Found ignored genre: %s", iteration, genre.Name)
					}
				}

				if !foundDrama {
					t.Errorf("Goroutine %d: Expected 'Drama' genre from replacement, not found", iteration)
				}

				// Verify actress alias conversion worked (Yui Hatano -> Hatano Yui)
				if len(movie.Actresses) != 1 {
					t.Errorf("Goroutine %d: Expected 1 actress, got %d", iteration, len(movie.Actresses))
					return
				}

				// Check FullName() returns canonical form
				if movie.Actresses[0].FullName() != "Hatano Yui" {
					t.Errorf("Goroutine %d: Expected actress name 'Hatano Yui', got '%s'",
						iteration, movie.Actresses[0].FullName())
				}
			}(i)
		}

		// Wait for all goroutines to finish
		for i := 0; i < numGoroutines; i++ {
			<-done
		}
	})

	// Test concurrent cache reloads to detect write races
	t.Run("Concurrent cache reload while aggregating", func(t *testing.T) {
		const numAggregations = 5
		const numReloads = 3

		done := make(chan bool, numAggregations+numReloads)

		// Start aggregation goroutines (readers)
		for i := 0; i < numAggregations; i++ {
			go func(iteration int) {
				defer func() { done <- true }()

				results := []*models.ScraperResult{createTestResult("IPX-" + fmt.Sprint(iteration+100))}
				_, _, err := agg.Aggregate(results)

				if err != nil {
					t.Errorf("Aggregation goroutine %d: Unexpected error: %v", iteration, err)
				}
			}(i)
		}

		// Start cache reload goroutines (writers)
		for i := 0; i < numReloads; i++ {
			go func(iteration int) {
				defer func() { done <- true }()

				// Reload genre replacement cache (write operation)
				agg.ReloadGenreReplacements()

				// Reload actress alias cache (write operation)
				agg.loadActressAliasCache()
			}(i)
		}

		// Wait for all goroutines to finish
		for i := 0; i < numAggregations+numReloads; i++ {
			<-done
		}
	})
}

// TestAggregateNilAndInvalidData tests handling of nil and malformed scraper results
// This validates AC-3.7.1: Nil/invalid data handling without panics
func TestAggregateNilAndInvalidData(t *testing.T) {
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

	agg := New(cfg)

	t.Run("nil actresses vs empty array - both treated as no actresses", func(t *testing.T) {
		// Scenario: One result with nil actresses, one with empty array
		results := []*models.ScraperResult{
			{
				Source:    "r18dev",
				ID:        "IPX-001",
				Title:     "Test Movie",
				Actresses: nil, // Nil array
			},
			{
				Source:    "dmm",
				ID:        "IPX-001",
				Title:     "Test Movie",
				Actresses: []models.ActressInfo{}, // Empty array
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		// Both should result in zero actresses
		assert.Equal(t, 0, len(movie.Actresses), "Nil and empty actress arrays should both result in no actresses")
	})

	t.Run("empty string for required field ID - still accepted", func(t *testing.T) {
		// Scenario: Result has empty ID but other fields present
		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "", // Empty ID
				Title:  "Test Movie",
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)
		// Empty ID is accepted - aggregator doesn't validate required fields
		assert.Equal(t, "", movie.ID)
	})

	t.Run("invalid date format - date field ignored gracefully", func(t *testing.T) {
		// Scenario: One result with valid date, verification that nil dates don't cause issues
		validDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)
		results := []*models.ScraperResult{
			{
				Source:      "r18dev",
				ID:          "IPX-001",
				Title:       "Test Movie",
				ReleaseDate: &validDate,
			},
			{
				Source:      "dmm",
				ID:          "IPX-001",
				Title:       "Test Movie",
				ReleaseDate: nil, // Nil date
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		// Should use the valid date from r18dev (first priority)
		require.NotNil(t, movie.ReleaseDate)
		assert.Equal(t, validDate, *movie.ReleaseDate)
	})

	t.Run("negative runtime - skipped and defaults to zero", func(t *testing.T) {
		// Scenario: Scraper returns negative runtime
		results := []*models.ScraperResult{
			{
				Source:  "r18dev",
				ID:      "IPX-001",
				Title:   "Test Movie",
				Runtime: -120, // Negative runtime
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		// Aggregator skips negative runtime values (treats as zero/invalid)
		assert.Equal(t, 0, movie.Runtime, "Negative runtime should be skipped")
	})

	t.Run("nil genres vs empty genres - both treated as no genres", func(t *testing.T) {
		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Test Movie",
				Genres: nil, // Nil genres
			},
			{
				Source: "dmm",
				ID:     "IPX-001",
				Title:  "Test Movie",
				Genres: []string{}, // Empty genres
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		// Both should result in zero genres
		assert.Equal(t, 0, len(movie.Genres), "Nil and empty genre arrays should both result in no genres")
	})
}

// TestAggregatePartialData tests aggregation with minimal or incomplete data
// This validates AC-3.7.2: Partial data scenarios handled correctly
func TestAggregatePartialData(t *testing.T) {
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

	agg := New(cfg)

	t.Run("minimal valid result - only ID and Title", func(t *testing.T) {
		// Scenario: Result with absolute minimum data
		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Minimal Movie",
				// All other fields empty/nil
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err, "Should accept minimal valid data")
		require.NotNil(t, movie)

		assert.Equal(t, "IPX-001", movie.ID)
		assert.Equal(t, "Minimal Movie", movie.Title)
		assert.Equal(t, "", movie.Description, "Empty description should be preserved")
		assert.Equal(t, 0, movie.Runtime, "Zero runtime should be preserved")
	})

	t.Run("partial actress data - no DMMID", func(t *testing.T) {
		// Scenario: Actress has name but DMMID=0 (not set)
		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Test Movie",
				Actresses: []models.ActressInfo{
					{
						FirstName:    "Yui",
						LastName:     "Hatano",
						JapaneseName: "波多野結衣",
						DMMID:        0, // No DMMID
					},
				},
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		// Actress should be included without DMMID
		require.Equal(t, 1, len(movie.Actresses))
		assert.Equal(t, "Yui", movie.Actresses[0].FirstName)
		assert.Equal(t, "Hatano", movie.Actresses[0].LastName)
		assert.Equal(t, 0, movie.Actresses[0].DMMID)
	})

	t.Run("actress DMMID upgraded from non-positive placeholder", func(t *testing.T) {
		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Test Movie",
				Actresses: []models.ActressInfo{
					{
						JapaneseName: "波多野結衣",
						DMMID:        -123456, // Placeholder/surrogate from a source without real DMM ID
					},
				},
			},
			{
				Source: "dmm",
				ID:     "IPX-001",
				Title:  "Test Movie",
				Actresses: []models.ActressInfo{
					{
						JapaneseName: "波多野結衣",
						DMMID:        12345, // Real DMM ID
					},
				},
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)
		require.Len(t, movie.Actresses, 1)
		assert.Equal(t, 12345, movie.Actresses[0].DMMID)
	})

	t.Run("gap filling from lower priority scraper", func(t *testing.T) {
		// Scenario: r18dev has title, dmm has description
		results := []*models.ScraperResult{
			{
				Source:      "r18dev",
				ID:          "IPX-001",
				Title:       "R18 Title",
				Description: "", // Empty
			},
			{
				Source:      "dmm",
				ID:          "IPX-001",
				Title:       "", // Empty
				Description: "DMM Description",
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		// Should use r18dev title (first priority)
		assert.Equal(t, "R18 Title", movie.Title)

		// Should use dmm description (dmm is first priority for description)
		assert.Equal(t, "DMM Description", movie.Description)
	})

	t.Run("invalid genre data - empty strings and duplicates", func(t *testing.T) {
		// Scenario: Genres with empty strings, duplicates, and valid entries
		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				ID:     "IPX-001",
				Title:  "Test Movie",
				Genres: []string{"Drama", "", "Drama", "Romance", "  ", "Drama"}, // Empty strings and duplicates
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		// Aggregator merges genres and removes duplicates
		// Note: Empty strings ARE currently accepted by aggregator (no validation)
		genreNames := make(map[string]bool)
		for _, genre := range movie.Genres {
			genreNames[genre.Name] = true
		}

		// Should have valid genres present
		assert.Contains(t, genreNames, "Drama", "Drama should be present")
		assert.Contains(t, genreNames, "Romance", "Romance should be present")

		// Note: Aggregator may not deduplicate genres from a single scraper
		// This test verifies that invalid data (empty, duplicates) doesn't cause panics
		assert.Greater(t, len(movie.Genres), 0, "Should have some genres")
	})

	t.Run("mixing valid and invalid data across scrapers", func(t *testing.T) {
		validDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

		// Scenario: Multiple scrapers with mix of valid/invalid data
		results := []*models.ScraperResult{
			{
				Source:      "r18dev",
				ID:          "IPX-001",
				Title:       "Valid Title",
				Description: "",         // Empty
				ReleaseDate: &validDate, // Valid
				Runtime:     0,          // Zero (invalid)
				Genres:      []string{}, // Empty
				Actresses:   nil,        // Nil
			},
			{
				Source:      "dmm",
				ID:          "IPX-001",
				Title:       "", // Empty
				Description: "Valid Description",
				ReleaseDate: nil,                                                          // Nil
				Runtime:     120,                                                          // Valid
				Genres:      []string{"Drama", "Romance"},                                 // Valid
				Actresses:   []models.ActressInfo{{FirstName: "Yui", LastName: "Hatano"}}, // Valid
			},
		}

		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		// Should use best data from each scraper based on priority
		assert.Equal(t, "Valid Title", movie.Title)
		assert.Equal(t, "Valid Description", movie.Description)
		assert.NotNil(t, movie.ReleaseDate)
		assert.Equal(t, 120, movie.Runtime)
		assert.Greater(t, len(movie.Genres), 0)
		assert.Greater(t, len(movie.Actresses), 0)
	})
}

// TestAggregateConcurrencySameID tests concurrent aggregation of the same movie ID
// This validates AC-3.7.3: Same movie ID aggregated concurrently produces consistent results
func TestAggregateConcurrencySameID(t *testing.T) {
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

	agg := New(cfg)

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	// Create consistent test data
	createResults := func() []*models.ScraperResult {
		return []*models.ScraperResult{
			{
				Source:      "r18dev",
				ID:          "IPX-001",
				Title:       "R18 Title",
				Description: "R18 Description",
				ReleaseDate: &releaseDate,
				Runtime:     120,
			},
			{
				Source:      "dmm",
				ID:          "IPX-001",
				Title:       "DMM Title",
				Description: "DMM Description",
				ReleaseDate: &releaseDate,
				Runtime:     120,
			},
		}
	}

	t.Run("10 goroutines aggregate same movie ID simultaneously", func(t *testing.T) {
		const numGoroutines = 10

		type result struct {
			movie *models.Movie
			err   error
		}

		results := make(chan result, numGoroutines)

		// Start all goroutines simultaneously
		for i := 0; i < numGoroutines; i++ {
			go func() {
				movie, _, err := agg.Aggregate(createResults())
				results <- result{movie, err}
			}()
		}

		// Collect all results
		var movies []*models.Movie
		for i := 0; i < numGoroutines; i++ {
			res := <-results
			require.NoError(t, res.err, "All concurrent aggregations should succeed")
			require.NotNil(t, res.movie)
			movies = append(movies, res.movie)
		}

		// Verify all results are consistent
		firstMovie := movies[0]
		for i, movie := range movies {
			assert.Equal(t, firstMovie.ID, movie.ID, "Goroutine %d: Inconsistent ID", i)
			assert.Equal(t, firstMovie.Title, movie.Title, "Goroutine %d: Inconsistent Title", i)
			assert.Equal(t, firstMovie.Description, movie.Description, "Goroutine %d: Inconsistent Description", i)
			assert.Equal(t, firstMovie.Runtime, movie.Runtime, "Goroutine %d: Inconsistent Runtime", i)

			// With simplified priorities, all fields use the same priority (r18dev first)
			assert.Equal(t, "R18 Title", movie.Title, "Should use r18dev title (first priority)")
			assert.Equal(t, "R18 Description", movie.Description, "Should use r18dev description (first priority)")
		}
	})
}

// TestAggregateConcurrencyDifferentMovies tests concurrent aggregation of different movie IDs
// This validates AC-3.7.3: Different movies concurrently - no cross-contamination
func TestAggregateConcurrencyDifferentMovies(t *testing.T) {
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

	agg := New(cfg)

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	t.Run("10 goroutines aggregate different movies concurrently", func(t *testing.T) {
		const numMovies = 10

		type result struct {
			expectedID    string
			expectedTitle string
			movie         *models.Movie
			err           error
		}

		results := make(chan result, numMovies)

		// Start aggregations for different movies
		for i := 0; i < numMovies; i++ {
			movieID := fmt.Sprintf("IPX-%03d", i+1)
			movieTitle := fmt.Sprintf("Movie %d", i+1)

			go func(id, title string) {
				scraperResults := []*models.ScraperResult{
					{
						Source:      "r18dev",
						ID:          id,
						Title:       title,
						ReleaseDate: &releaseDate,
					},
				}

				movie, _, err := agg.Aggregate(scraperResults)
				results <- result{id, title, movie, err}
			}(movieID, movieTitle)
		}

		// Collect and verify all results
		for i := 0; i < numMovies; i++ {
			res := <-results
			require.NoError(t, res.err, "Aggregation should succeed for %s", res.expectedID)
			require.NotNil(t, res.movie)

			// Verify no cross-contamination: each movie has its correct data
			assert.Equal(t, res.expectedID, res.movie.ID,
				"Movie %s should have its own ID, not another movie's", res.expectedID)
			assert.Equal(t, res.expectedTitle, res.movie.Title,
				"Movie %s should have its own title, not another movie's", res.expectedID)
		}
	})
}

// Benchmark tests for aggregator performance (AC-3.7.4)

func BenchmarkAggregateSingleMovie(b *testing.B) {
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

	agg := New(cfg)

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	results := []*models.ScraperResult{
		{
			Source:      "r18dev",
			ID:          "IPX-001",
			Title:       "R18 Title",
			Description: "R18 Description",
			ReleaseDate: &releaseDate,
			Runtime:     120,
			Actresses:   []models.ActressInfo{{FirstName: "Yui", LastName: "Hatano", DMMID: 12345}},
			Genres:      []string{"Drama", "Romance"},
		},
		{
			Source:      "dmm",
			ID:          "IPX-001",
			Title:       "DMM Title",
			Description: "DMM Description",
			ReleaseDate: &releaseDate,
			Runtime:     120,
			Actresses:   []models.ActressInfo{{FirstName: "Yui", LastName: "Hatano", DMMID: 12345}},
			Genres:      []string{"Drama", "Romance"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = agg.Aggregate(results)
	}
}

func BenchmarkAggregateBatch(b *testing.B) {
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

	agg := New(cfg)

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	// Test different batch sizes
	benchmarks := []struct {
		name  string
		count int
	}{
		{"10Movies", 10},
		{"50Movies", 50},
		{"100Movies", 100},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Pre-create movie results
			allResults := make([][]*models.ScraperResult, bm.count)
			for i := 0; i < bm.count; i++ {
				allResults[i] = []*models.ScraperResult{
					{
						Source:      "r18dev",
						ID:          fmt.Sprintf("IPX-%03d", i+1),
						Title:       fmt.Sprintf("Movie %d", i+1),
						ReleaseDate: &releaseDate,
						Runtime:     120,
					},
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, results := range allResults {
					_, _, _ = agg.Aggregate(results)
				}
			}
		})
	}
}

// TestAggregateAllScrapersEmptyResults tests handling when all scrapers return completely empty results
// This validates AC-3.7.1: Both scrapers return empty/nil results → return appropriate error
func TestAggregateAllScrapersEmptyResults(t *testing.T) {
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

	agg := New(cfg)

	t.Run("both scrapers return completely empty ScraperResults", func(t *testing.T) {
		// Scenario: Both scrapers return ScraperResults with no fields set (empty structs)
		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				// All fields empty/zero values
			},
			{
				Source: "dmm",
				// All fields empty/zero values
			},
		}

		movie, _, err := agg.Aggregate(results)

		// The aggregator should still return a movie (with empty fields) since results were provided
		// It doesn't validate that fields are populated - that's the caller's responsibility
		require.NoError(t, err, "Aggregator accepts empty results and lets caller validate")
		require.NotNil(t, movie)

		// Verify all fields are empty/zero
		assert.Equal(t, "", movie.ID, "Empty ID from empty results")
		assert.Equal(t, "", movie.Title, "Empty title from empty results")
		assert.Equal(t, "", movie.Description, "Empty description from empty results")
	})

	t.Run("no results provided to aggregate", func(t *testing.T) {
		// Scenario: Empty results array
		results := []*models.ScraperResult{}

		movie, _, err := agg.Aggregate(results)

		// This SHOULD return an error - no results to aggregate
		require.Error(t, err, "Should return error when no results provided")
		assert.Nil(t, movie)
		assert.Contains(t, err.Error(), "no scraper results", "Error message should be descriptive")
	})
}

// TestScreenshotsFallback tests screenshot aggregation fallback behavior
// Validates AGGREGATE-01: Empty arrays treated as "no data"
// Validates AGGREGATE-02: Fallback to lower priority source when higher has empty screenshots
func TestScreenshotsFallback(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "javbus"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "javbus"},
			},
		},
	}

	agg := New(cfg)

	tests := []struct {
		name            string
		results         []*models.ScraperResult
		priority        []string
		wantScreenshots []string
		wantLen         int
	}{
		{
			name: "DMM empty, fallback to JavBus",
			results: []*models.ScraperResult{
				{Source: "dmm", ScreenshotURL: []string{}},
				{Source: "javbus", ScreenshotURL: []string{"url1", "url2", "url3"}},
			},
			priority:        []string{"dmm", "javbus"},
			wantScreenshots: []string{"url1", "url2", "url3"},
			wantLen:         3,
		},
		{
			name: "DMM populated, no fallback",
			results: []*models.ScraperResult{
				{Source: "dmm", ScreenshotURL: []string{"dmm1", "dmm2"}},
				{Source: "javbus", ScreenshotURL: []string{"javbus1"}},
			},
			priority:        []string{"dmm", "javbus"},
			wantScreenshots: []string{"dmm1", "dmm2"},
			wantLen:         2,
		},
		{
			name: "All sources empty",
			results: []*models.ScraperResult{
				{Source: "dmm", ScreenshotURL: []string{}},
				{Source: "javbus", ScreenshotURL: nil},
			},
			priority:        []string{"dmm", "javbus"},
			wantScreenshots: []string{},
			wantLen:         0,
		},
		{
			name: "Multiple sources with screenshots - first priority wins",
			results: []*models.ScraperResult{
				{Source: "dmm", ScreenshotURL: []string{"dmm1", "dmm2", "dmm3"}},
				{Source: "javbus", ScreenshotURL: []string{"javbus1", "javbus2"}},
			},
			priority:        []string{"dmm", "javbus"},
			wantScreenshots: []string{"dmm1", "dmm2", "dmm3"},
			wantLen:         3,
		},
		{
			name: "Nil treated as empty",
			results: []*models.ScraperResult{
				{Source: "dmm", ScreenshotURL: nil},
				{Source: "javbus", ScreenshotURL: []string{"url1", "url2"}},
			},
			priority:        []string{"dmm", "javbus"},
			wantScreenshots: []string{"url1", "url2"},
			wantLen:         2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultsBySource := make(map[string]*models.ScraperResult)
			for _, result := range tt.results {
				resultsBySource[result.Source] = result
			}

			screenshots := agg.getScreenshotsByPriority(resultsBySource, tt.priority)

			assert.Equal(t, tt.wantLen, len(screenshots), "Screenshot count mismatch")
			if tt.wantLen > 0 {
				assert.Equal(t, tt.wantScreenshots, screenshots, "Screenshots mismatch")
			}
		})
	}
}

// TestAggregateConcurrentCacheReload tests cache reload during active aggregation
// This validates AC-3.7.3: Cache updates during active aggregation → no stale data served
func TestAggregateConcurrentCacheReload(t *testing.T) {
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

	agg := New(cfg)

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	t.Run("cache reload during aggregation operations", func(t *testing.T) {
		const numAggregations = 10
		const numReloads = 5

		results := make(chan error, numAggregations+numReloads)

		// Create consistent test data
		createResults := func(id string) []*models.ScraperResult {
			return []*models.ScraperResult{
				{
					Source:      "r18dev",
					ID:          id,
					Title:       fmt.Sprintf("Movie %s Title", id),
					Description: "R18 Description",
					ReleaseDate: &releaseDate,
					Runtime:     120,
				},
				{
					Source:      "dmm",
					ID:          id,
					Title:       "DMM Title",
					Description: fmt.Sprintf("Movie %s Description", id),
					ReleaseDate: &releaseDate,
					Runtime:     120,
				},
			}
		}

		// Start multiple aggregations
		for i := 0; i < numAggregations; i++ {
			go func(idx int) {
				movieID := fmt.Sprintf("IPX-%03d", idx+1)
				_, _, err := agg.Aggregate(createResults(movieID))
				results <- err
			}(i)
		}

		// Concurrently reload caches (this simulates cache updates during aggregation)
		// The ReloadCaches method is thread-safe and should not interfere with ongoing aggregations
		for i := 0; i < numReloads; i++ {
			go func() {
				// Simulate cache reload by creating a new aggregator with same config
				// This tests that creating new aggregator instances doesn't corrupt ongoing operations
				_ = New(cfg)
				results <- nil
			}()
		}

		// Collect all results and verify no errors
		for i := 0; i < numAggregations+numReloads; i++ {
			err := <-results
			assert.NoError(t, err, "Concurrent aggregation and cache reload should not fail")
		}
	})

	t.Run("aggregation consistency during cache operations", func(t *testing.T) {
		// Test that multiple aggregations of the same movie during cache operations
		// produce consistent results
		const numConcurrent = 20
		movieID := "IPX-999"

		results := make(chan *models.Movie, numConcurrent)

		createResults := func() []*models.ScraperResult {
			return []*models.ScraperResult{
				{
					Source:      "r18dev",
					ID:          movieID,
					Title:       "Consistent Title",
					Description: "R18 Description",
					ReleaseDate: &releaseDate,
				},
				{
					Source:      "dmm",
					ID:          movieID,
					Title:       "DMM Title",
					Description: "DMM Description",
					ReleaseDate: &releaseDate,
				},
			}
		}

		// Start aggregations while also creating new aggregator instances (cache operations)
		for i := 0; i < numConcurrent; i++ {
			go func(idx int) {
				// Alternate between using existing aggregator and creating new ones
				var movie *models.Movie
				var err error
				if idx%2 == 0 {
					movie, _, err = agg.Aggregate(createResults())
				} else {
					// Create new aggregator (cache initialization) and aggregate
					newAgg := New(cfg)
					movie, _, err = newAgg.Aggregate(createResults())
				}
				require.NoError(t, err)
				results <- movie
			}(i)
		}

		// Collect results and verify consistency
		var movies []*models.Movie
		for i := 0; i < numConcurrent; i++ {
			movies = append(movies, <-results)
		}

		// With simplified priorities, all fields use the same priority (r18dev first)
		for i, movie := range movies {
			assert.Equal(t, movieID, movie.ID, "Goroutine %d: Movie ID mismatch", i)
			assert.Equal(t, "Consistent Title", movie.Title, "Goroutine %d: Should use r18dev title", i)
			assert.Equal(t, "R18 Description", movie.Description, "Goroutine %d: Should use r18dev description", i)
		}
	})
}

func TestAggregator_BuildTranslations(t *testing.T) {
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
	agg := New(cfg)

	t.Run("merges translations from multiple results", func(t *testing.T) {
		results := []*models.ScraperResult{
			{
				Source:   "r18dev",
				Language: "en",
				Title:    "English Title",
				Translations: []models.MovieTranslation{
					{Language: "en", Title: "English Title", Description: "English desc"},
					{Language: "ja", Title: "Japanese Title"},
				},
			},
		}
		movie := &models.Movie{Title: "English Title"}

		translations := agg.buildTranslations(results, movie)
		assert.Len(t, translations, 2)

		var enFound, jaFound bool
		for _, tr := range translations {
			if tr.Language == "en" {
				enFound = true
				assert.Equal(t, "English Title", tr.Title)
			}
			if tr.Language == "ja" {
				jaFound = true
				assert.Equal(t, "Japanese Title", tr.Title)
			}
		}
		assert.True(t, enFound)
		assert.True(t, jaFound)
	})

	t.Run("deduplicates same language translations", func(t *testing.T) {
		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				Translations: []models.MovieTranslation{
					{Language: "en", Title: "First Title"},
					{Language: "en", Description: "Second desc"},
				},
			},
		}
		movie := &models.Movie{}

		translations := agg.buildTranslations(results, movie)
		assert.Len(t, translations, 1)
		assert.Equal(t, "First Title", translations[0].Title)
		assert.Equal(t, "Second desc", translations[0].Description)
	})

	t.Run("skips non-winner legacy translations", func(t *testing.T) {
		results := []*models.ScraperResult{
			{Source: "r18dev", Language: "en", Title: "Different Title", Description: "Desc"},
		}
		movie := &models.Movie{Title: "Winning Title"}

		translations := agg.buildTranslations(results, movie)
		assert.Empty(t, translations)
	})

	t.Run("includes legacy translation when scraper wins", func(t *testing.T) {
		results := []*models.ScraperResult{
			{Source: "r18dev", Language: "en", Title: "Winner", Description: "Desc"},
		}
		movie := &models.Movie{Title: "Winner"}

		translations := agg.buildTranslations(results, movie)
		assert.Len(t, translations, 1)
		assert.Equal(t, "en", translations[0].Language)
		assert.Equal(t, "Winner", translations[0].Title)
	})

	t.Run("skips results without language", func(t *testing.T) {
		results := []*models.ScraperResult{
			{Source: "r18dev", Language: "", Title: "Title"},
		}
		movie := &models.Movie{Title: "Title"}

		translations := agg.buildTranslations(results, movie)
		assert.Empty(t, translations)
	})

	t.Run("winner based on original title match", func(t *testing.T) {
		results := []*models.ScraperResult{
			{Source: "dmm", Language: "ja", OriginalTitle: "Original"},
		}
		movie := &models.Movie{OriginalTitle: "Original"}

		translations := agg.buildTranslations(results, movie)
		assert.Len(t, translations, 1)
		assert.Equal(t, "ja", translations[0].Language)
	})

	t.Run("winner based on description match", func(t *testing.T) {
		results := []*models.ScraperResult{
			{Source: "r18dev", Language: "en", Description: "Matched Desc"},
		}
		movie := &models.Movie{Description: "Matched Desc"}

		translations := agg.buildTranslations(results, movie)
		assert.Len(t, translations, 1)
	})

	t.Run("winner based on director match", func(t *testing.T) {
		results := []*models.ScraperResult{
			{Source: "r18dev", Language: "en", Director: "Director A"},
		}
		movie := &models.Movie{Director: "Director A"}

		translations := agg.buildTranslations(results, movie)
		assert.Len(t, translations, 1)
	})

	t.Run("winner based on maker match", func(t *testing.T) {
		results := []*models.ScraperResult{
			{Source: "r18dev", Language: "en", Maker: "Maker X"},
		}
		movie := &models.Movie{Maker: "Maker X"}

		translations := agg.buildTranslations(results, movie)
		assert.Len(t, translations, 1)
	})

	t.Run("winner based on label match", func(t *testing.T) {
		results := []*models.ScraperResult{
			{Source: "r18dev", Language: "en", Label: "Label Y"},
		}
		movie := &models.Movie{Label: "Label Y"}

		translations := agg.buildTranslations(results, movie)
		assert.Len(t, translations, 1)
	})

	t.Run("winner based on series match", func(t *testing.T) {
		results := []*models.ScraperResult{
			{Source: "r18dev", Language: "en", Series: "Series Z"},
		}
		movie := &models.Movie{Series: "Series Z"}

		translations := agg.buildTranslations(results, movie)
		assert.Len(t, translations, 1)
	})

	t.Run("merges non-empty fields into existing translation", func(t *testing.T) {
		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				Translations: []models.MovieTranslation{
					{Language: "en", Title: "Title Only"},
					{Language: "en", Description: "Desc Added", Director: "Dir Added", Maker: "Maker Added", Label: "Label Added", Series: "Series Added"},
				},
			},
		}
		movie := &models.Movie{}

		translations := agg.buildTranslations(results, movie)
		assert.Len(t, translations, 1)
		assert.Equal(t, "Title Only", translations[0].Title)
		assert.Equal(t, "Desc Added", translations[0].Description)
		assert.Equal(t, "Dir Added", translations[0].Director)
		assert.Equal(t, "Maker Added", translations[0].Maker)
		assert.Equal(t, "Label Added", translations[0].Label)
		assert.Equal(t, "Series Added", translations[0].Series)
	})

	t.Run("does not overwrite existing non-empty fields", func(t *testing.T) {
		results := []*models.ScraperResult{
			{
				Source: "r18dev",
				Translations: []models.MovieTranslation{
					{Language: "en", Title: "First Title", Description: "First Desc"},
					{Language: "en", Title: "Second Title", Description: "Second Desc"},
				},
			},
		}
		movie := &models.Movie{}

		translations := agg.buildTranslations(results, movie)
		assert.Len(t, translations, 1)
		assert.Equal(t, "First Title", translations[0].Title)
		assert.Equal(t, "First Desc", translations[0].Description)
	})

	t.Run("legacy translation includes all fields", func(t *testing.T) {
		results := []*models.ScraperResult{
			{
				Source:        "r18dev",
				Language:      "en",
				Title:         "Title",
				OriginalTitle: "OrigTitle",
				Description:   "Desc",
				Director:      "Dir",
				Maker:         "Maker",
				Label:         "Label",
				Series:        "Series",
			},
		}
		movie := &models.Movie{Title: "Title"}

		translations := agg.buildTranslations(results, movie)
		assert.Len(t, translations, 1)
		assert.Equal(t, "en", translations[0].Language)
		assert.Equal(t, "Title", translations[0].Title)
		assert.Equal(t, "OrigTitle", translations[0].OriginalTitle)
		assert.Equal(t, "Desc", translations[0].Description)
		assert.Equal(t, "Dir", translations[0].Director)
		assert.Equal(t, "Maker", translations[0].Maker)
		assert.Equal(t, "Label", translations[0].Label)
		assert.Equal(t, "Series", translations[0].Series)
		assert.Equal(t, "r18dev", translations[0].SourceName)
	})
}

func TestApplyWordReplacement(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{
				Enabled: true,
			},
		},
	}

	agg := &Aggregator{
		config:               cfg,
		wordReplacementCache: map[string]string{"foo": "bar", "baz": "qux"},
	}
	agg.wordReplacementSorted = []struct{ orig, repl string }{
		{"baz", "qux"},
		{"foo", "bar"},
	}

	assert.Equal(t, "bar qux", agg.applyWordReplacement("foo baz"))
	assert.Equal(t, "hello", agg.applyWordReplacement("hello"))
}

func TestApplyWordReplacement_Disabled(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: false},
		},
	}

	agg := &Aggregator{config: cfg, wordReplacementCache: map[string]string{"foo": "bar"}}
	assert.Equal(t, "foo", agg.applyWordReplacement("foo"))
}

func TestLoadWordReplacementCache(t *testing.T) {
	mockRepo := &mockWordRepo{replacements: map[string]string{"old": "new"}}

	agg := &Aggregator{wordReplacementRepo: mockRepo}
	agg.loadWordReplacementCache()

	agg.wordCacheMutex.RLock()
	cache := agg.wordReplacementCache
	agg.wordCacheMutex.RUnlock()

	assert.Equal(t, "new", cache["old"])
}

func TestApplyWordReplacements(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}

	agg := &Aggregator{
		config:               cfg,
		wordReplacementCache: map[string]string{"bad": "good"},
	}
	agg.wordReplacementSorted = []struct{ orig, repl string }{{"bad", "good"}}

	movie := &models.Movie{
		Title:         "bad title",
		OriginalTitle: "bad orig",
		Description:   "bad desc",
		Director:      "bad dir",
		Maker:         "bad maker",
		Label:         "bad label",
		Series:        "bad series",
	}

	agg.applyWordReplacements(movie)

	assert.Equal(t, "good title", movie.Title)
	assert.Equal(t, "good orig", movie.OriginalTitle)
	assert.Equal(t, "good desc", movie.Description)
	assert.Equal(t, "good dir", movie.Director)
	assert.Equal(t, "good maker", movie.Maker)
	assert.Equal(t, "good label", movie.Label)
	assert.Equal(t, "good series", movie.Series)
}

func TestApplyWordReplacements_WithTranslations(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}

	agg := &Aggregator{
		config:               cfg,
		wordReplacementCache: map[string]string{"bad": "good"},
	}
	agg.wordReplacementSorted = []struct{ orig, repl string }{{"bad", "good"}}

	movie := &models.Movie{
		Title:  "bad title",
		Series: "bad series",
		Translations: []models.MovieTranslation{
			{Title: "bad trans", OriginalTitle: "bad orig", Description: "bad desc", Director: "bad dir", Maker: "bad maker", Label: "bad label", Series: "bad series"},
		},
	}

	agg.applyWordReplacements(movie)

	assert.Equal(t, "good title", movie.Title)
	assert.Equal(t, "good trans", movie.Translations[0].Title)
	assert.Equal(t, "good orig", movie.Translations[0].OriginalTitle)
	assert.Equal(t, "good desc", movie.Translations[0].Description)
	assert.Equal(t, "good dir", movie.Translations[0].Director)
	assert.Equal(t, "good maker", movie.Translations[0].Maker)
	assert.Equal(t, "good label", movie.Translations[0].Label)
	assert.Equal(t, "good series", movie.Translations[0].Series)
}

func TestReloadWordReplacements(t *testing.T) {
	mockRepo := &mockWordRepo{replacements: map[string]string{"foo": "bar"}}
	agg := &Aggregator{wordReplacementRepo: mockRepo}
	agg.ReloadWordReplacements()

	agg.wordCacheMutex.RLock()
	cache := agg.wordReplacementCache
	sorted := agg.wordReplacementSorted
	agg.wordCacheMutex.RUnlock()

	assert.Equal(t, "bar", cache["foo"])
	assert.Len(t, sorted, 1)
}

func TestLoadWordReplacementCache_NilRepo(t *testing.T) {
	agg := &Aggregator{wordReplacementRepo: nil}
	agg.loadWordReplacementCache()

	agg.wordCacheMutex.RLock()
	cache := agg.wordReplacementCache
	agg.wordCacheMutex.RUnlock()

	assert.Nil(t, cache)
}

func TestLoadWordReplacementCache_Error(t *testing.T) {
	mockRepo := &mockWordRepo{err: fmt.Errorf("db error")}
	agg := &Aggregator{wordReplacementRepo: mockRepo}
	agg.loadWordReplacementCache()

	agg.wordCacheMutex.RLock()
	cache := agg.wordReplacementCache
	agg.wordCacheMutex.RUnlock()

	assert.Nil(t, cache)
}

func TestLoadWordReplacementCache_SortOrder(t *testing.T) {
	mockRepo := &mockWordRepo{replacements: map[string]string{
		"aa":  "1",
		"bbb": "2",
		"cc":  "3",
	}}
	agg := &Aggregator{wordReplacementRepo: mockRepo}
	agg.loadWordReplacementCache()

	agg.wordCacheMutex.RLock()
	sorted := agg.wordReplacementSorted
	agg.wordCacheMutex.RUnlock()

	require.Len(t, sorted, 3)
	assert.Equal(t, "bbb", sorted[0].orig)
}

func TestApplyWordReplacement_EmptyText(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}
	agg := &Aggregator{config: cfg, wordReplacementCache: map[string]string{"foo": "bar"}}
	agg.wordReplacementSorted = []struct{ orig, repl string }{{"foo", "bar"}}

	assert.Equal(t, "", agg.applyWordReplacement(""))
}

func TestApplyWordReplacement_EmptySorted(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}
	agg := &Aggregator{config: cfg}

	assert.Equal(t, "hello", agg.applyWordReplacement("hello"))
}

type mockWordRepo struct {
	replacements map[string]string
	err          error
}

func (m *mockWordRepo) Create(_ *models.WordReplacement) error { return nil }
func (m *mockWordRepo) Upsert(_ *models.WordReplacement) error { return nil }
func (m *mockWordRepo) FindByOriginal(_ string) (*models.WordReplacement, error) {
	return nil, nil
}
func (m *mockWordRepo) List() ([]models.WordReplacement, error) { return nil, nil }
func (m *mockWordRepo) Delete(_ string) error                   { return nil }
func (m *mockWordRepo) GetReplacementMap() (map[string]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.replacements, nil
}
