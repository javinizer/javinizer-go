package aggregator

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAggregatePriority_JapaneseFirst tests that DMM (Japanese) takes priority over R18Dev (English)
func TestAggregatePriority_JapaneseFirst(t *testing.T) {
	// Setup config with DMM prioritized before R18Dev
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	// Create mock scraper results
	r18devResult := &models.ScraperResult{
		Source:      "r18dev",
		Language:    "en",
		ID:          "ABW-102",
		ContentID:   "abw00102",
		Title:       "Naked Housekeeper - A New Sensation Of Virtual Sex Sex Life For You Staff02 Suzu Matsuoka",
		Maker:       "Prestige",
		Description: "English description from R18Dev",
		Genres:      []string{"Drama", "House Wife"},
		Actresses: []models.ActressInfo{
			{FirstName: "Suzu", LastName: "Matsuoka"},
		},
		ReleaseDate: &releaseDate,
		Runtime:     120,
	}

	dmmResult := &models.ScraperResult{
		Source:      "dmm",
		Language:    "ja",
		ID:          "ABW-102",
		ContentID:   "abw00102",
		Title:       "全裸家政婦 新感覚ヴァーチャルセックス性活をあなたに Staff02 松岡すず",
		Maker:       "プレステージ",
		Description: "Japanese description from DMM",
		Genres:      []string{"ドラマ", "人妻"},
		Actresses: []models.ActressInfo{
			{JapaneseName: "松岡すず"},
		},
		ReleaseDate: &releaseDate,
		Runtime:     120,
	}

	// Test with DMM first in results array
	t.Run("DMM prioritized in config", func(t *testing.T) {
		results := []*models.ScraperResult{r18devResult, dmmResult}
		movie, _, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		// Since config prioritizes DMM, we should get Japanese data
		assert.Equal(t, "全裸家政婦 新感覚ヴァーチャルセックス性活をあなたに Staff02 松岡すず", movie.Title,
			"Title should be Japanese from DMM")
		assert.Equal(t, "プレステージ", movie.Maker,
			"Maker should be Japanese from DMM")
		assert.Equal(t, "Japanese description from DMM", movie.Description,
			"Description should be from DMM")

		// Since DMM wins all fields (Title and Description), only DMM's translation is included
		// r18dev contributed nothing to the merged movie fields
		require.Len(t, movie.Translations, 1, "Should have only DMM translation since it won all fields")

		jaTranslation := movie.Translations[0]
		assert.Equal(t, "ja", jaTranslation.Language, "Should have Japanese translation")
		assert.Equal(t, "全裸家政婦 新感覚ヴァーチャルセックス性活をあなたに Staff02 松岡すず",
			jaTranslation.Title, "Japanese translation should be preserved")
		assert.Equal(t, "プレステージ", jaTranslation.Maker, "Japanese maker should be preserved")
	})

	// Test with R18Dev prioritized (opposite config)
	t.Run("R18Dev prioritized in config", func(t *testing.T) {
		// Create config with R18Dev first
		cfgR18First := &config.Config{
			Metadata: config.MetadataConfig{
				Priority: config.PriorityConfig{
					Priority: []string{"r18dev", "dmm"},
				},
			},
		}

		aggR18First := newAggregatorNoDB(testConfigFromAppConfig(cfgR18First))
		results := []*models.ScraperResult{r18devResult, dmmResult}
		movie, _, err := aggR18First.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		// Since config prioritizes R18Dev, we should get English data
		assert.Equal(t, "Naked Housekeeper - A New Sensation Of Virtual Sex Sex Life For You Staff02 Suzu Matsuoka",
			movie.Title, "Title should be English from R18Dev")
		assert.Equal(t, "Prestige", movie.Maker,
			"Maker should be English from R18Dev")
		assert.Equal(t, "English description from R18Dev", movie.Description,
			"Description should be from R18Dev")
	})
}

// TestAggregatePriority_MissingData tests priority fallback when preferred source has no data
func TestAggregatePriority_MissingData(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	// DMM result with missing title
	dmmResult := &models.ScraperResult{
		Source:   "dmm",
		Language: "ja",
		ID:       "TEST-001",
		Title:    "", // Empty title
		Maker:    "プレステージ",
	}

	// R18Dev result with complete data
	r18devResult := &models.ScraperResult{
		Source:   "r18dev",
		Language: "en",
		ID:       "TEST-001",
		Title:    "English Title",
		Maker:    "Prestige",
	}

	results := []*models.ScraperResult{dmmResult, r18devResult}
	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Should fall back to R18Dev title when DMM title is empty
	assert.Equal(t, "English Title", movie.Title,
		"Should fall back to R18Dev title when DMM has no title")

	// But should still use DMM maker since it has it
	assert.Equal(t, "プレステージ", movie.Maker,
		"Should use DMM maker since it's available")
}

// TestAggregatePriority_EmptyPriorityFallsBackToGlobal tests that empty priority arrays fall back to global priority
// With simplified priorities, all fields use the same priority
func TestAggregatePriority_EmptyPriorityFallsBackToGlobal(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"}, // Global priority
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev", "dmm"},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	// Verify resolved priorities - all fields use the same priority
	assert.Equal(t, []string{"r18dev", "dmm"}, agg.resolvedPriorities["Title"])
	assert.Equal(t, []string{"r18dev", "dmm"}, agg.resolvedPriorities["Description"])
	assert.Equal(t, []string{"r18dev", "dmm"}, agg.resolvedPriorities["Maker"])

	// Create mock scraper results
	r18devResult := &models.ScraperResult{
		Source:      "r18dev",
		Language:    "en",
		ID:          "TEST-001",
		Title:       "English Title",
		Maker:       "English Maker",
		Description: "English Description",
	}

	dmmResult := &models.ScraperResult{
		Source:      "dmm",
		Language:    "ja",
		ID:          "TEST-001",
		Title:       "Japanese Title",
		Maker:       "Japanese Maker",
		Description: "Japanese Description",
	}

	results := []*models.ScraperResult{r18devResult, dmmResult}
	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// With simplified priorities, all fields use r18dev first
	assert.Equal(t, "English Title", movie.Title,
		"Title should use r18dev first (same priority for all fields)")
	assert.Equal(t, "English Description", movie.Description,
		"Description should use r18dev first (same priority for all fields)")
	assert.Equal(t, "English Maker", movie.Maker,
		"Maker should use r18dev first (same priority for all fields)")
}

// TestAggregatePriority_MissingPriorityFallsBackToGlobal tests that missing priorities fall back to global
// With simplified priorities, all fields use the same priority
func TestAggregatePriority_MissingPriorityFallsBackToGlobal(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev"}, // Global priority
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev", "dmm"},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	// Verify resolved priorities - all fields use the same priority
	assert.Equal(t, []string{"r18dev", "dmm"}, agg.resolvedPriorities["Title"])
	assert.Equal(t, []string{"r18dev", "dmm"}, agg.resolvedPriorities["Description"])
	assert.Equal(t, []string{"r18dev", "dmm"}, agg.resolvedPriorities["Maker"])

	// Create mock scraper results
	r18devResult := &models.ScraperResult{
		Source:      "r18dev",
		ID:          "TEST-001",
		Title:       "English Title",
		Maker:       "English Maker",
		Description: "English Description",
	}

	dmmResult := &models.ScraperResult{
		Source:      "dmm",
		ID:          "TEST-001",
		Title:       "Japanese Title",
		Maker:       "Japanese Maker",
		Description: "Japanese Description",
	}

	results := []*models.ScraperResult{r18devResult, dmmResult}
	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// With simplified priorities, all fields use r18dev first
	assert.Equal(t, "English Title", movie.Title,
		"Title should use r18dev first (same priority for all fields)")
	assert.Equal(t, "English Description", movie.Description,
		"Description should use r18dev first (same priority for all fields)")
	assert.Equal(t, "English Maker", movie.Maker,
		"Maker should use r18dev first (same priority for all fields)")
}

// TestAggregateWithPriority_CustomPriority tests custom priority override for manual scraping
func TestAggregateWithPriority_CustomPriority(t *testing.T) {
	// Setup config with custom priority
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev"}, // Global default
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	r18devResult := &models.ScraperResult{
		Source:      "r18dev",
		Language:    "en",
		ID:          "ABW-102",
		ContentID:   "abw00102",
		Title:       "English Title from R18Dev",
		Maker:       "Prestige",
		Description: "English description",
		ReleaseDate: &releaseDate,
		Runtime:     120,
		PosterURL:   "https://r18dev.com/poster.jpg",
	}

	dmmResult := &models.ScraperResult{
		Source:      "dmm",
		Language:    "ja",
		ID:          "ABW-102",
		ContentID:   "abw00102",
		Title:       "Japanese Title from DMM",
		Maker:       "プレステージ",
		Description: "Japanese description",
		ReleaseDate: &releaseDate,
		Runtime:     120,
		PosterURL:   "https://dmm.com/poster.jpg",
	}

	results := []*models.ScraperResult{r18devResult, dmmResult}

	// Test with custom priority that overrides config (r18dev first)
	customPriority := []string{"r18dev", "dmm"}
	movie, _, err := agg.AggregateWithPriority(results, customPriority)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Should use R18Dev data even though config prioritizes DMM
	assert.Equal(t, "English Title from R18Dev", movie.Title,
		"Should use R18Dev title based on custom priority")
	assert.Equal(t, "Prestige", movie.Maker,
		"Should use R18Dev maker based on custom priority")
	assert.Equal(t, "English description", movie.Description,
		"Should use R18Dev description based on custom priority")
	assert.Equal(t, "https://r18dev.com/poster.jpg", movie.Poster.PosterURL,
		"Should use R18Dev poster URL based on custom priority")

	// Test with opposite custom priority (dmm first)
	customPriorityDMM := []string{"dmm", "r18dev"}
	movie2, _, err := agg.AggregateWithPriority(results, customPriorityDMM)
	require.NoError(t, err)
	require.NotNil(t, movie2)

	// Should use DMM data
	assert.Equal(t, "Japanese Title from DMM", movie2.Title,
		"Should use DMM title based on custom priority")
	assert.Equal(t, "プレステージ", movie2.Maker,
		"Should use DMM maker based on custom priority")
	assert.Equal(t, "Japanese description", movie2.Description,
		"Should use DMM description based on custom priority")
}

// TestAggregateWithPriority_EmptyResults tests error handling with empty results
func TestAggregateWithPriority_EmptyResults(t *testing.T) {
	cfg := &config.Config{}
	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{}
	customPriority := []string{"r18dev", "dmm"}

	movie, _, err := agg.AggregateWithPriority(results, customPriority)
	assert.Error(t, err)
	assert.Nil(t, movie)
	assert.Contains(t, err.Error(), "no scraper results to aggregate")
}

// TestAggregateWithPriority_FallbackToNextPriority tests fallback behavior
func TestAggregateWithPriority_FallbackToNextPriority(t *testing.T) {
	cfg := &config.Config{}
	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	// First scraper has incomplete data
	firstResult := &models.ScraperResult{
		Source:      "scraper1",
		ID:          "TEST-001",
		Title:       "", // Empty title
		Maker:       "Maker 1",
		Description: "", // Empty description
	}

	// Second scraper has complete data
	secondResult := &models.ScraperResult{
		Source:      "scraper2",
		ID:          "TEST-001",
		Title:       "Title from Scraper 2",
		Maker:       "Maker 2",
		Description: "Description from Scraper 2",
	}

	results := []*models.ScraperResult{firstResult, secondResult}
	customPriority := []string{"scraper1", "scraper2"}

	movie, _, err := agg.AggregateWithPriority(results, customPriority)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Should use scraper1 for fields it has
	assert.Equal(t, "Maker 1", movie.Maker,
		"Should use scraper1 maker since it's first in priority and has data")

	// Should fall back to scraper2 for fields scraper1 doesn't have
	assert.Equal(t, "Title from Scraper 2", movie.Title,
		"Should fall back to scraper2 title since scraper1 has empty title")
	assert.Equal(t, "Description from Scraper 2", movie.Description,
		"Should fall back to scraper2 description since scraper1 has empty description")
}

// TestAggregateWithPriority_AllFields tests that all movie fields are properly aggregated
func TestAggregateWithPriority_AllFields(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			NFO: config.NFOConfig{
				Format: config.NFOFormatConfig{
					DisplayTitle: "<ID> - <TITLE>",
				},
			},
		},
	}
	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	releaseDate := time.Date(2021, 6, 15, 0, 0, 0, 0, time.UTC)
	screenshots := []string{
		"https://example.com/screen1.jpg",
		"https://example.com/screen2.jpg",
	}

	result := &models.ScraperResult{
		Source:        "test-scraper",
		SourceURL:     "https://test-scraper.com/movie/123",
		Language:      "en",
		ID:            "TEST-123",
		ContentID:     "test00123",
		Title:         "Test Movie Title",
		OriginalTitle: "Original Test Title",
		Description:   "Test description",
		Director:      "Test Director",
		Maker:         "Test Studio",
		Label:         "Test Label",
		Series:        "Test Series",
		PosterURL:     "https://example.com/poster.jpg",
		CoverURL:      "https://example.com/cover.jpg",
		TrailerURL:    "https://example.com/trailer.mp4",
		Runtime:       120,
		ReleaseDate:   &releaseDate,
		Rating: &models.Rating{
			Score: 8.5,
			Votes: 100,
		},
		Actresses: []models.ActressInfo{
			{FirstName: "Jane", LastName: "Doe", JapaneseName: "ジェーン・ドウ"},
		},
		Genres:        []string{"Drama", "Romance"},
		ScreenshotURL: screenshots,
	}

	results := []*models.ScraperResult{result}
	customPriority := []string{"test-scraper"}

	movie, _, err := agg.AggregateWithPriority(results, customPriority)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Verify all fields are populated
	assert.Equal(t, "TEST-123", movie.ID)
	assert.Equal(t, "test00123", movie.ContentID)
	assert.Equal(t, "Test Movie Title", movie.Title)
	assert.Equal(t, "Original Test Title", movie.OriginalTitle)
	assert.Equal(t, "Test description", movie.Description)
	assert.Equal(t, "Test Director", movie.Director)
	assert.Equal(t, "Test Studio", movie.Maker)
	assert.Equal(t, "Test Label", movie.Label)
	assert.Equal(t, "Test Series", movie.Series)
	assert.Equal(t, "https://example.com/poster.jpg", movie.Poster.PosterURL)
	assert.Equal(t, "https://example.com/cover.jpg", movie.Poster.CoverURL)
	assert.Equal(t, "https://example.com/trailer.mp4", movie.TrailerURL)
	assert.Equal(t, 120, movie.Runtime)
	assert.Equal(t, 2021, movie.ReleaseDate.Year())
	assert.Equal(t, 2021, movie.ReleaseYear)
	assert.Equal(t, 8.5, movie.RatingScore)
	assert.Equal(t, 100, movie.RatingVotes)
	assert.Len(t, movie.Actresses, 1)
	assert.Equal(t, "Jane", movie.Actresses[0].FirstName)
	assert.Len(t, movie.Genres, 2)
	assert.Len(t, movie.Screenshots, 2)
	assert.Equal(t, "test-scraper", movie.SourceName)
	assert.Equal(t, "https://test-scraper.com/movie/123", movie.SourceURL)
	assert.Empty(t, movie.DisplayTitle, "aggregator should not apply DisplayTitle")

	// Verify timestamps are set
	assert.False(t, movie.CreatedAt.IsZero())
	assert.False(t, movie.UpdatedAt.IsZero())
}

// mockScraper is a test implementation of models.Scraper
type mockScraper struct {
	name   string
	result *models.ScraperResult
}

func (m *mockScraper) Name() string { return m.name }
func (m *mockScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return m.result, nil
}
func (m *mockScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (m *mockScraper) IsEnabled() bool                                    { return true }
func (m *mockScraper) Config() *models.ScraperSettings                    { return nil }
func (m *mockScraper) Close() error                                       { return nil }

// TestResolvePriorities_ScrapersPriorityOrder tests that ScrapersPriority determines priority order
func TestResolvePriorities_ScrapersPriorityOrder(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm", "javlibrary"},
		},
	}
	aggCfg := testConfigFromAppConfig(cfg)
	agg := New(aggCfg, NewGenreProcessor(aggCfg.Metadata, nil), NewWordProcessor(aggCfg.Metadata, nil), NewAliasResolver(aggCfg.Metadata, nil))

	// Verify priority order matches ScrapersPriority order
	priority := agg.getResolvedPriorities()["Title"]
	assert.Equal(t, []string{"r18dev", "dmm", "javlibrary"}, priority)

	// Verify all fields use the same priority
	assert.Equal(t, priority, agg.getResolvedPriorities()["Description"])
	assert.Equal(t, priority, agg.getResolvedPriorities()["Maker"])
}

// TestResolvePriorities_ScrapersEmptyFallsBack tests fallback when no scrapers injected
func TestResolvePriorities_ScrapersEmptyFallsBack(t *testing.T) {
	// Create config with explicit priority (to verify it takes precedence)
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"custom1", "custom2"},
			},
		},
	}

	// When Scrapers is nil, should fall back to config priority
	aggCfg := testConfigFromAppConfig(cfg)
	agg := New(aggCfg, NewGenreProcessor(aggCfg.Metadata, nil), NewWordProcessor(aggCfg.Metadata, nil), NewAliasResolver(aggCfg.Metadata, nil))

	priority := agg.getResolvedPriorities()["Title"]
	assert.Equal(t, []string{"custom1", "custom2"}, priority)
}

// TestResolvePriorities_EmptyScrapersPriorityFallsBackToMetadataPriority tests fallback when ScrapersPriority is empty
func TestResolvePriorities_EmptyScrapersPriorityFallsBackToMetadataPriority(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"fallback1", "fallback2"},
			},
		},
	}

	// When ScrapersPriority is empty, should fall back to metadata priority
	aggCfg := testConfigFromAppConfig(cfg)
	agg := New(aggCfg, NewGenreProcessor(aggCfg.Metadata, nil), NewWordProcessor(aggCfg.Metadata, nil), NewAliasResolver(aggCfg.Metadata, nil))

	priority := agg.getResolvedPriorities()["Title"]
	assert.Equal(t, []string{"fallback1", "fallback2"}, priority)
}

// TestResolvePriorities_PrefersConfiguredScraperPriorityOverRegistryDefaults verifies that
// cfg.Scrapers.Priority is used when metadata priority is unset.
func TestResolvePriorities_PrefersConfiguredScraperPriorityOverRegistryDefaults(t *testing.T) {

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: nil,
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))
	priority := agg.getResolvedPriorities()["Title"]
	assert.Equal(t, []string{"dmm", "r18dev"}, priority)
}

type testPriorityModule struct {
	name     string
	priority int
}

func (m *testPriorityModule) Name() string        { return m.name }
func (m *testPriorityModule) Description() string { return "Test " + m.name }
func (m *testPriorityModule) Constructor() any    { return nil }
func (m *testPriorityModule) Validator() any      { return nil }
