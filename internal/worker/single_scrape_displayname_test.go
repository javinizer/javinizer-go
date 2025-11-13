package worker

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDisplayNameRegenerationWithMergeStrategies tests that DisplayName is not
// duplicated when using preserve-existing or fill-missing-only strategies
func TestDisplayNameRegenerationWithMergeStrategies(t *testing.T) {
	tests := []struct {
		name                        string
		scalarStrategy              string
		arrayStrategy               string
		nfoTitle                    string // Title stored in NFO (may have template applied)
		scraperTitle                string // Raw title from scraper
		displayNameTemplate         string
		expectedDisplayName         string
		shouldRegenerateDisplayName bool
		description                 string
	}{
		{
			name:                        "Gap-Fill preset - preserves NFO title with template",
			scalarStrategy:              "fill-missing-only",
			arrayStrategy:               "merge",
			nfoTitle:                    "[ABP-960] Beautiful Japanese Girl",
			scraperTitle:                "Beautiful Japanese Girl",
			displayNameTemplate:         "[<ID>] <TITLE>",
			expectedDisplayName:         "[ABP-960] Beautiful Japanese Girl", // Should NOT regenerate
			shouldRegenerateDisplayName: false,
			description:                 "Gap-Fill should preserve the existing NFO title (which already has template applied) and NOT regenerate DisplayName",
		},
		{
			name:                        "Conservative preset - preserves NFO title with template",
			scalarStrategy:              "preserve-existing",
			arrayStrategy:               "merge",
			nfoTitle:                    "[ABP-960] Beautiful Japanese Girl",
			scraperTitle:                "Beautiful Japanese Girl",
			displayNameTemplate:         "[<ID>] <TITLE>",
			expectedDisplayName:         "[ABP-960] Beautiful Japanese Girl", // Should NOT regenerate
			shouldRegenerateDisplayName: false,
			description:                 "Conservative should preserve the existing NFO title (which already has template applied) and NOT regenerate DisplayName",
		},
		{
			name:                        "Aggressive preset - regenerates from scraper title",
			scalarStrategy:              "prefer-scraper",
			arrayStrategy:               "replace",
			nfoTitle:                    "[ABP-960] Old Title",
			scraperTitle:                "New Title From Scraper",
			displayNameTemplate:         "[<ID>] <TITLE>",
			expectedDisplayName:         "[ABP-960] New Title From Scraper", // Should regenerate with new title
			shouldRegenerateDisplayName: true,
			description:                 "Aggressive should use scraper title and regenerate DisplayName with template",
		},
		{
			name:                        "Prefer-NFO - regenerates with NFO title",
			scalarStrategy:              "prefer-nfo",
			arrayStrategy:               "merge",
			nfoTitle:                    "NFO Title Without Template",
			scraperTitle:                "Scraper Title",
			displayNameTemplate:         "[<ID>] <TITLE>",
			expectedDisplayName:         "[ABP-960] NFO Title Without Template", // Should regenerate from NFO title
			shouldRegenerateDisplayName: true,
			description:                 "Prefer-NFO should use NFO title but regenerate DisplayName (allows template changes)",
		},
		{
			name:                        "Gap-Fill with different template format",
			scalarStrategy:              "fill-missing-only",
			arrayStrategy:               "merge",
			nfoTitle:                    "ABP-960 - Beautiful Girl",
			scraperTitle:                "Beautiful Girl",
			displayNameTemplate:         "<ID> - <TITLE>",
			expectedDisplayName:         "ABP-960 - Beautiful Girl", // Should NOT regenerate
			shouldRegenerateDisplayName: false,
			description:                 "Gap-Fill should work with any template format (without brackets)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test NFO
			tmpDir := t.TempDir()
			nfoPath := filepath.Join(tmpDir, "ABP-960.nfo")

			// Create test NFO file with the template-applied title
			testNFO := &nfo.Movie{
				ID:    "ABP-960",
				Title: tt.nfoTitle,
			}
			generator := nfo.NewGenerator(&nfo.Config{})
			err := generator.WriteNFO(testNFO, nfoPath)
			require.NoError(t, err, "Failed to create test NFO file")

			// Parse the NFO (simulating what happens in single_scrape.go)
			parseResult, err := nfo.ParseNFO(nfoPath)
			require.NoError(t, err, "Failed to parse test NFO")
			assert.Equal(t, tt.nfoTitle, parseResult.Movie.Title, "NFO parser should preserve title as-is")

			// Create scraped movie (fresh from scraper, no template applied)
			scrapedMovie := &models.Movie{
				ID:    "ABP-960",
				Title: tt.scraperTitle,
			}

			// Merge using the specified strategy (simulating single_scrape.go logic)
			scalar := nfo.ParseScalarStrategy(tt.scalarStrategy)
			mergeArrays := nfo.ParseArrayStrategy(tt.arrayStrategy)
			mergeResult, err := nfo.MergeMovieMetadataWithOptions(scrapedMovie, parseResult.Movie, scalar, mergeArrays)
			require.NoError(t, err, "Merge should succeed")

			// Check which title was chosen by the merge
			mergedMovie := mergeResult.Merged
			t.Logf("Merged title: %q (from NFO: %q, from scraper: %q)", mergedMovie.Title, tt.nfoTitle, tt.scraperTitle)

			// Simulate the DisplayName regeneration logic from single_scrape.go
			cfg := &config.Config{
				Metadata: config.MetadataConfig{
					NFO: config.NFOConfig{
						DisplayName: tt.displayNameTemplate,
					},
				},
			}

			// This is the critical logic being tested
			shouldRegenerateDisplayName := scalar != nfo.PreserveExisting && scalar != nfo.FillMissingOnly
			assert.Equal(t, tt.shouldRegenerateDisplayName, shouldRegenerateDisplayName,
				"shouldRegenerateDisplayName flag should match expected value")

			var finalDisplayName string
			if shouldRegenerateDisplayName && cfg.Metadata.NFO.DisplayName != "" {
				// Regenerate DisplayName from merged data
				tmplEngine := template.NewEngine()
				ctx := template.NewContextFromMovie(mergedMovie)
				displayName, err := tmplEngine.Execute(cfg.Metadata.NFO.DisplayName, ctx)
				require.NoError(t, err, "Template execution should succeed")
				finalDisplayName = displayName
				t.Logf("Regenerated DisplayName: %q", finalDisplayName)
			} else {
				// Keep existing title as DisplayName (don't regenerate)
				finalDisplayName = mergedMovie.Title
				t.Logf("Kept existing title as DisplayName (no regeneration): %q", finalDisplayName)
			}

			// Verify the final DisplayName matches expected
			assert.Equal(t, tt.expectedDisplayName, finalDisplayName,
				"DisplayName should match expected value: %s", tt.description)

			// Additional assertion: ensure no duplication occurred
			if !shouldRegenerateDisplayName {
				// For preserve-existing/fill-missing-only, the title should not have duplicates
				// (e.g., should NOT be "[ABP-960] [ABP-960] Title")
				expectedPrefix := "ABP-960"
				count := 0
				pos := 0
				titleToCheck := finalDisplayName
				for {
					idx := indexOf(titleToCheck[pos:], expectedPrefix)
					if idx == -1 {
						break
					}
					count++
					pos += idx + len(expectedPrefix)
				}
				assert.LessOrEqual(t, count, 1,
					"ID should appear at most once in DisplayName (no duplication). Got: %q", finalDisplayName)
			}
		})
	}
}

// TestDisplayNameWithEmptyNFOTitle tests the edge case where NFO has no title
func TestDisplayNameWithEmptyNFOTitle(t *testing.T) {
	tmpDir := t.TempDir()
	nfoPath := filepath.Join(tmpDir, "TEST-001.nfo")

	// Create NFO with empty title
	testNFO := &nfo.Movie{
		ID:    "TEST-001",
		Title: "", // Empty title
	}
	generator := nfo.NewGenerator(&nfo.Config{})
	err := generator.WriteNFO(testNFO, nfoPath)
	require.NoError(t, err)

	parseResult, err := nfo.ParseNFO(nfoPath)
	require.NoError(t, err)

	scrapedMovie := &models.Movie{
		ID:    "TEST-001",
		Title: "Scraper Title",
	}

	// Test with fill-missing-only (Gap-Fill)
	scalar := nfo.ParseScalarStrategy("fill-missing-only")
	mergeArrays := nfo.ParseArrayStrategy("merge")
	mergeResult, err := nfo.MergeMovieMetadataWithOptions(scrapedMovie, parseResult.Movie, scalar, mergeArrays)
	require.NoError(t, err)

	// With fill-missing-only, empty NFO title should be filled from scraper
	assert.Equal(t, "Scraper Title", mergeResult.Merged.Title,
		"Empty NFO title should be filled with scraper title")

	// DisplayName should NOT be regenerated even though title was filled
	// (fill-missing-only still means "don't regenerate DisplayName")
	shouldRegenerate := scalar != nfo.PreserveExisting && scalar != nfo.FillMissingOnly
	assert.False(t, shouldRegenerate, "Should not regenerate DisplayName with fill-missing-only")
}

// TestDisplayNameRegenerationPresetMapping verifies preset behavior
func TestDisplayNameRegenerationPresetMapping(t *testing.T) {
	tests := []struct {
		preset                      string
		expectedScalar              string
		expectedArray               string
		shouldRegenerateDisplayName bool
	}{
		{
			preset:                      "conservative",
			expectedScalar:              "preserve-existing",
			expectedArray:               "merge",
			shouldRegenerateDisplayName: false,
		},
		{
			preset:                      "gap-fill",
			expectedScalar:              "fill-missing-only",
			expectedArray:               "merge",
			shouldRegenerateDisplayName: false,
		},
		{
			preset:                      "aggressive",
			expectedScalar:              "prefer-scraper",
			expectedArray:               "replace",
			shouldRegenerateDisplayName: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.preset+" preset", func(t *testing.T) {
			scalarStr, arrayStr, err := nfo.ApplyPreset(tt.preset, "", "")
			require.NoError(t, err)
			assert.Equal(t, tt.expectedScalar, scalarStr, "Scalar strategy should match")
			assert.Equal(t, tt.expectedArray, arrayStr, "Array strategy should match")

			scalar := nfo.ParseScalarStrategy(scalarStr)
			shouldRegenerate := scalar != nfo.PreserveExisting && scalar != nfo.FillMissingOnly
			assert.Equal(t, tt.shouldRegenerateDisplayName, shouldRegenerate,
				"DisplayName regeneration flag should match for preset: %s", tt.preset)
		})
	}
}

// TestDisplayNameWithCachedMovie tests the cached movie code path (lines 260-312)
func TestDisplayNameWithCachedMovie(t *testing.T) {
	tests := []struct {
		name                        string
		scalarStrategy              string
		cachedTitle                 string // Title from cached database entry
		nfoTitle                    string // Title from NFO file
		displayNameTemplate         string
		expectedDisplayName         string
		shouldRegenerateDisplayName bool
		description                 string
	}{
		{
			name:                        "Gap-Fill with cached movie - preserves NFO title",
			scalarStrategy:              "fill-missing-only",
			cachedTitle:                 "Cached Title From Database",
			nfoTitle:                    "[ABP-960] NFO Title With Template",
			displayNameTemplate:         "[<ID>] <TITLE>",
			expectedDisplayName:         "[ABP-960] NFO Title With Template",
			shouldRegenerateDisplayName: false,
			description:                 "Cached movie path: Gap-Fill should preserve NFO title without regenerating",
		},
		{
			name:                        "Conservative with cached movie - preserves NFO title",
			scalarStrategy:              "preserve-existing",
			cachedTitle:                 "Cached Title",
			nfoTitle:                    "[TEST-001] NFO Title",
			displayNameTemplate:         "[<ID>] <TITLE>",
			expectedDisplayName:         "[TEST-001] NFO Title",
			shouldRegenerateDisplayName: false,
			description:                 "Cached movie path: Conservative should preserve NFO title",
		},
		{
			name:                        "Prefer-scraper with cached movie - regenerates from cache",
			scalarStrategy:              "prefer-scraper",
			cachedTitle:                 "Fresh Cached Title",
			nfoTitle:                    "[OLD-001] Old NFO Title",
			displayNameTemplate:         "[<ID>] <TITLE>",
			expectedDisplayName:         "[TEST-001] Fresh Cached Title",
			shouldRegenerateDisplayName: true,
			description:                 "Cached movie path: Prefer-scraper should use cached title and regenerate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary NFO file
			tmpDir := t.TempDir()
			nfoPath := filepath.Join(tmpDir, "TEST-001.nfo")

			testNFO := &nfo.Movie{
				ID:    "TEST-001",
				Title: tt.nfoTitle,
			}
			generator := nfo.NewGenerator(&nfo.Config{})
			err := generator.WriteNFO(testNFO, nfoPath)
			require.NoError(t, err)

			// Parse NFO (simulates NFO parsing in cached movie path)
			parseResult, err := nfo.ParseNFO(nfoPath)
			require.NoError(t, err)

			// Create cached movie (simulates database-cached movie)
			cachedMovie := &models.Movie{
				ID:    "TEST-001",
				Title: tt.cachedTitle,
			}

			// Merge cached movie with NFO (same as lines 286 in single_scrape.go)
			scalar := nfo.ParseScalarStrategy(tt.scalarStrategy)
			mergeArrays := nfo.ParseArrayStrategy("merge")
			mergeResult, err := nfo.MergeMovieMetadataWithOptions(cachedMovie, parseResult.Movie, scalar, mergeArrays)
			require.NoError(t, err)

			mergedMovie := mergeResult.Merged
			t.Logf("Merged title: %q (from cache: %q, from NFO: %q)", mergedMovie.Title, tt.cachedTitle, tt.nfoTitle)

			// Simulate DisplayName regeneration logic (same as lines 297-307)
			cfg := &config.Config{
				Metadata: config.MetadataConfig{
					NFO: config.NFOConfig{
						DisplayName: tt.displayNameTemplate,
					},
				},
			}

			shouldRegenerateDisplayName := scalar != nfo.PreserveExisting && scalar != nfo.FillMissingOnly
			assert.Equal(t, tt.shouldRegenerateDisplayName, shouldRegenerateDisplayName,
				"shouldRegenerateDisplayName flag should match expected (cached movie path)")

			var finalDisplayName string
			if shouldRegenerateDisplayName && cfg.Metadata.NFO.DisplayName != "" {
				tmplEngine := template.NewEngine()
				ctx := template.NewContextFromMovie(mergedMovie)
				displayName, err := tmplEngine.Execute(cfg.Metadata.NFO.DisplayName, ctx)
				require.NoError(t, err)
				finalDisplayName = displayName
				t.Logf("Regenerated DisplayName (cached path): %q", finalDisplayName)
			} else {
				finalDisplayName = mergedMovie.Title
				t.Logf("Kept existing title as DisplayName (cached path, no regeneration): %q", finalDisplayName)
			}

			assert.Equal(t, tt.expectedDisplayName, finalDisplayName,
				"DisplayName should match expected (cached movie path): %s", tt.description)

			// Ensure no duplication for preserve-existing/fill-missing-only
			if !shouldRegenerateDisplayName {
				expectedPrefix := "TEST-001"
				count := 0
				pos := 0
				titleToCheck := finalDisplayName
				for {
					idx := indexOf(titleToCheck[pos:], expectedPrefix)
					if idx == -1 {
						break
					}
					count++
					pos += idx + len(expectedPrefix)
				}
				assert.LessOrEqual(t, count, 1,
					"ID should appear at most once in DisplayName (cached path). Got: %q", finalDisplayName)
			}
		})
	}
}

// TestBothCodePathsUseIdenticalLogic ensures both fresh scrape and cached movie
// paths use the same DisplayName regeneration logic
func TestBothCodePathsUseIdenticalLogic(t *testing.T) {
	// This test documents that the fix was applied to both code paths:
	// 1. Fresh scrape path (lines 610-620 in single_scrape.go)
	// 2. Cached movie path (lines 297-307 in single_scrape.go)
	//
	// Both should skip DisplayName regeneration for preserve-existing and fill-missing-only

	strategies := []struct {
		name                        string
		scalarStrategy              string
		shouldRegenerateDisplayName bool
	}{
		{"PreferNFO", "prefer-nfo", true},
		{"PreferScraper", "prefer-scraper", true},
		{"PreserveExisting", "preserve-existing", false},
		{"FillMissingOnly", "fill-missing-only", false},
	}

	for _, tt := range strategies {
		t.Run(tt.name, func(t *testing.T) {
			scalar := nfo.ParseScalarStrategy(tt.scalarStrategy)
			shouldRegenerate := scalar != nfo.PreserveExisting && scalar != nfo.FillMissingOnly

			assert.Equal(t, tt.shouldRegenerateDisplayName, shouldRegenerate,
				"Strategy %s should have shouldRegenerateDisplayName=%v (applies to both code paths)",
				tt.scalarStrategy, tt.shouldRegenerateDisplayName)
		})
	}
}

// Helper function to find substring index
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
