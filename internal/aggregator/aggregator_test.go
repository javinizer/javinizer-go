package aggregator

import (
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
		name          string
		ignoreGenres  []string
		genreToTest   string
		shouldIgnore  bool
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
				"^Featured",      // Regex: starts with "Featured"
				".*mosaic.*",     // Regex: contains "mosaic"
				"Sample",         // Exact: exactly "Sample"
				"^(HD|4K)",       // Regex: starts with HD or 4K
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
		"Featured Actress",  // Matches ^Featured
		"HD mosaic",         // Matches .*mosaic.*
		"Sample",            // Exact match
		"HD",                // Matches ^(HD|4K)
		"4K",                // Matches ^(HD|4K)
		"mosaic version",    // Matches .*mosaic.*
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
	defer db.Close()

	// Run migrations
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	tests := []struct {
		name        string
		autoAdd     bool
		genreName   string
		shouldExist bool
	}{
		{
			name:        "Auto-add enabled - new genre",
			autoAdd:     true,
			genreName:   "New Genre",
			shouldExist: true,
		},
		{
			name:        "Auto-add disabled - new genre",
			autoAdd:     false,
			genreName:   "Another Genre",
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

func TestDisplayNameFormatting(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				ID:    []string{"r18dev"},
				Title: []string{"r18dev"},
			},
			NFO: config.NFOConfig{
				DisplayName: "[<ID>] <TITLE>",
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

	movie, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Verify display name was formatted correctly
	assert.Equal(t, "[IPX-001] Test Movie", movie.DisplayName)
}

func TestDisplayNameFormattingWithTemplate(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				ID:    []string{"r18dev"},
				Title: []string{"r18dev"},
				Maker: []string{"r18dev"},
			},
			NFO: config.NFOConfig{
				DisplayName: "<TITLE> by <STUDIO>",
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

	movie, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Verify display name was formatted correctly
	assert.Equal(t, "Amazing Movie by Idea Pocket", movie.DisplayName)
}

func TestDisplayNameEmpty(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				ID: []string{"r18dev"},
			},
			NFO: config.NFOConfig{
				DisplayName: "", // Empty - should not set DisplayName
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

	movie, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// DisplayName should be empty when not configured
	assert.Empty(t, movie.DisplayName)
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
			name:           "Numeric field - RatingScore zero is treated as missing",
			requiredFields: []string{"rating"},
			movie: &models.ScraperResult{
				Source: "r18dev",
				ID:     "IPX-001",
				Rating: &models.Rating{Score: 0, Votes: 0},
			},
			shouldPass:    false,
			expectedError: "missing required fields: RatingScore",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Metadata: config.MetadataConfig{
					RequiredFields: tt.requiredFields,
					Priority: config.PriorityConfig{
						ID: []string{"r18dev"},
					},
				},
				Scrapers: config.ScrapersConfig{
					Priority: []string{"r18dev"},
				},
			}

			agg := New(cfg)
			results := []*models.ScraperResult{tt.movie}

			movie, err := agg.Aggregate(results)

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
	aliasGroups := map[string][]string{
		"ContentID":     {"contentid", "content_id", "CONTENTID"},
		"CoverURL":      {"coverurl", "cover_url", "cover", "COVER"},
		"PosterURL":     {"posterurl", "poster_url", "poster"},
		"TrailerURL":    {"trailerurl", "trailer_url", "trailer"},
		"Screenshots":   {"screenshots", "screenshot_url", "screenshoturl", "SCREENSHOTURL"},
		"OriginalTitle": {"originaltitle", "original_title"},
		"ReleaseDate":   {"releasedate", "release_date"},
		"RatingScore":   {"rating", "ratingscore", "rating_score"},
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
							ID: []string{"r18dev"},
						},
					},
					Scrapers: config.ScrapersConfig{
						Priority: []string{"r18dev"},
					},
				}

				agg := New(cfg)
				results := []*models.ScraperResult{movie}

				_, err := agg.Aggregate(results)
				require.Error(t, err,
					"Alias %q should trigger validation error for missing %s", alias, fieldName)
				assert.Contains(t, err.Error(), fieldName,
					"Error should mention canonical field name %s when using alias %q", fieldName, alias)
			}
		})
	}
}
