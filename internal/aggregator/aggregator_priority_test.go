package aggregator

import (
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
				ID:          []string{"dmm", "r18dev"},
				ContentID:   []string{"dmm", "r18dev"},
				Title:       []string{"dmm", "r18dev"},
				Maker:       []string{"dmm", "r18dev"},
				Description: []string{"dmm", "r18dev"},
				Actress:     []string{"dmm", "r18dev"},
				Genre:       []string{"dmm", "r18dev"},
			},
		},
	}

	agg := New(cfg)

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
		movie, err := agg.Aggregate(results)
		require.NoError(t, err)
		require.NotNil(t, movie)

		// Since config prioritizes DMM, we should get Japanese data
		assert.Equal(t, "全裸家政婦 新感覚ヴァーチャルセックス性活をあなたに Staff02 松岡すず", movie.Title,
			"Title should be Japanese from DMM")
		assert.Equal(t, "プレステージ", movie.Maker,
			"Maker should be Japanese from DMM")
		assert.Equal(t, "Japanese description from DMM", movie.Description,
			"Description should be from DMM")

		// Check translations are preserved
		require.Len(t, movie.Translations, 2, "Should have both translations")

		// Find each translation
		var enTranslation, jaTranslation *models.MovieTranslation
		for i := range movie.Translations {
			if movie.Translations[i].Language == "en" {
				enTranslation = &movie.Translations[i]
			}
			if movie.Translations[i].Language == "ja" {
				jaTranslation = &movie.Translations[i]
			}
		}

		require.NotNil(t, enTranslation, "Should have English translation")
		require.NotNil(t, jaTranslation, "Should have Japanese translation")

		assert.Equal(t, "Naked Housekeeper - A New Sensation Of Virtual Sex Sex Life For You Staff02 Suzu Matsuoka",
			enTranslation.Title, "English translation should be preserved")
		assert.Equal(t, "Prestige", enTranslation.Maker, "English maker should be preserved")

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
					ID:          []string{"r18dev", "dmm"},
					ContentID:   []string{"r18dev", "dmm"},
					Title:       []string{"r18dev", "dmm"},
					Maker:       []string{"r18dev", "dmm"},
					Description: []string{"r18dev", "dmm"},
					Actress:     []string{"r18dev", "dmm"},
					Genre:       []string{"r18dev", "dmm"},
				},
			},
		}

		aggR18First := New(cfgR18First)
		results := []*models.ScraperResult{r18devResult, dmmResult}
		movie, err := aggR18First.Aggregate(results)
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
				ID:    []string{"dmm", "r18dev"},
				Title: []string{"dmm", "r18dev"},
				Maker: []string{"dmm", "r18dev"},
			},
		},
	}

	agg := New(cfg)

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
	movie, err := agg.Aggregate(results)
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
func TestAggregatePriority_EmptyPriorityFallsBackToGlobal(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"}, // Global priority
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Title:       []string{},      // Empty - should use global
				Description: []string{"dmm"}, // Explicit priority
				Maker:       []string{},      // Empty - should use global
			},
		},
	}

	agg := New(cfg)

	// Verify resolved priorities
	assert.Equal(t, []string{"r18dev", "dmm"}, agg.resolvedPriorities["Title"],
		"Empty title priority should resolve to global priority")
	assert.Equal(t, []string{"dmm"}, agg.resolvedPriorities["Description"],
		"Explicit description priority should be preserved")
	assert.Equal(t, []string{"r18dev", "dmm"}, agg.resolvedPriorities["Maker"],
		"Empty maker priority should resolve to global priority")

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
	movie, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Title should use global priority (r18dev first) because title priority is empty
	assert.Equal(t, "English Title", movie.Title,
		"Title with empty priority should use global priority (r18dev first)")

	// Description should use explicit priority (dmm only)
	assert.Equal(t, "Japanese Description", movie.Description,
		"Description should use explicit priority (dmm)")

	// Maker should use global priority (r18dev first) because maker priority is empty
	assert.Equal(t, "English Maker", movie.Maker,
		"Maker with empty priority should use global priority (r18dev first)")
}

// TestAggregatePriority_MissingPriorityFallsBackToGlobal tests that missing priorities fall back to global
func TestAggregatePriority_MissingPriorityFallsBackToGlobal(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev"}, // Global priority
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				// Only define some fields, others are nil/missing
				Title: []string{"r18dev", "dmm"}, // Explicit
				// Description, Maker, etc. are missing
			},
		},
	}

	agg := New(cfg)

	// Verify resolved priorities
	assert.Equal(t, []string{"r18dev", "dmm"}, agg.resolvedPriorities["Title"],
		"Explicit title priority should be preserved")
	assert.Equal(t, []string{"dmm", "r18dev"}, agg.resolvedPriorities["Description"],
		"Missing description priority should resolve to global priority")
	assert.Equal(t, []string{"dmm", "r18dev"}, agg.resolvedPriorities["Maker"],
		"Missing maker priority should resolve to global priority")

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
	movie, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Title should use explicit priority (r18dev first)
	assert.Equal(t, "English Title", movie.Title,
		"Title should use explicit priority (r18dev first)")

	// Description should use global priority (dmm first) because it's missing
	assert.Equal(t, "Japanese Description", movie.Description,
		"Description with missing priority should use global priority (dmm first)")

	// Maker should use global priority (dmm first) because it's missing
	assert.Equal(t, "Japanese Maker", movie.Maker,
		"Maker with missing priority should use global priority (dmm first)")
}
