package aggregator

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAggregateWithPerFieldOverrideExcludingSource(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev", "mgstage", "libredmm"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Fields: map[string][]string{
					"id":         {"dmm", "r18dev", "libredmm"},
					"content_id": {"dmm", "r18dev", "libredmm"},
					"title":      {"dmm", "r18dev", "libredmm"},
					"maker":      {"dmm", "r18dev", "libredmm"},
					"actress":    {"dmm", "r18dev", "libredmm"},
				},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))
	t.Logf("resolvedPriorities[ID] = %v", agg.resolvedPriorities["ID"])
	t.Logf("resolvedPriorities[Title] = %v", agg.resolvedPriorities["Title"])
	t.Logf("resolvedPriorities[Actress] = %v", agg.resolvedPriorities["Actress"])

	releaseDate := time.Date(2025, 5, 31, 0, 0, 0, 0, time.UTC)

	results := []*models.ScraperResult{
		{
			Source:      "mgstage",
			ID:          "200GANA-3215",
			ContentID:   "200GANA-3215",
			Title:       "マジ軟派、初撮。 2172",
			Maker:       "ナンパTV",
			ReleaseDate: &releaseDate,
			Actresses: []models.ActressInfo{
				{JapaneseName: "テスト女優"},
			},
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// With exclusive per-field overrides, the per-field sources (dmm, r18dev,
	// libredmm) are the ONLY sources consulted for these fields. None of them
	// produced a result (only mgstage did, and mgstage is excluded from every
	// override), so these fields stay empty — they do NOT fall back to mgstage
	// via the global priority list. This is the restored v1 exclusive behavior (#50).
	assert.Empty(t, movie.ID, "ID must stay empty — per-field override excludes mgstage and no other source has data")
	assert.Empty(t, movie.ContentID, "ContentID must stay empty — per-field override is exclusive")
	assert.Empty(t, movie.Title, "Title must stay empty — per-field override is exclusive")
	assert.Empty(t, movie.Maker, "Maker must stay empty — per-field override is exclusive")
	assert.Empty(t, movie.Actresses, "Actresses must stay empty — per-field override is exclusive")
}

func TestAggregatePerFieldPreference(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev", "mgstage"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Fields: map[string][]string{
					"title": {"mgstage", "dmm"},
				},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source: "dmm",
			ID:     "200GANA-3215",
			Title:  "DMM Title",
		},
		{
			Source: "mgstage",
			ID:     "200GANA-3215",
			Title:  "MGStage Title",
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Equal(t, "MGStage Title", movie.Title, "Per-field override should set mgstage as preferred for title")
}

// TestPerFieldPriorityOverrideIsExclusive verifies that a per-field metadata.priority
// override is EXCLUSIVE: only the scrapers listed in the override are consulted
// for that field, with NO fallback to the global priority list. This restores
// v1 (PowerShell Javinizer) semantics — see issue #50.
//
// Scenario from #50: metadata.priority.series: [tokyohot]. tokyohot does not
// provide Series, while r18dev/dmm do. With exclusive semantics, Series must
// stay empty (tokyohot is the only allowed source and it has none) rather than
// being populated by r18dev/dmm via global fallback.
func TestPerFieldPriorityOverrideIsExclusive(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm", "tokyohot"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev", "dmm", "tokyohot"},
				Fields: map[string][]string{
					"series": {"tokyohot"},
				},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	// The Series override must NOT be merged with the global priority list.
	assert.Equal(t, []string{"tokyohot"}, agg.resolvedPriorities["Series"],
		"per-field Series override must be exclusive — not merged with the global priority list")

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	results := []*models.ScraperResult{
		{
			Source:      "tokyohot",
			Language:    "ja",
			ID:          "TH-001",
			Title:       "Tokyohot Title",
			Maker:       "Tokyohot Maker",
			Series:      "", // tokyohot does not provide Series
			ReleaseDate: &releaseDate,
		},
		{
			Source: "r18dev",
			ID:     "TH-001",
			Title:  "R18Dev Title",
			Series: "R18Dev Series", // r18dev HAS Series — must NOT leak in via fallback
		},
		{
			Source: "dmm",
			ID:     "TH-001",
			Title:  "DMM Title",
			Series: "DMM Series", // dmm HAS Series — must NOT leak in via fallback
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// The bug: Series was populated by r18dev/dmm via global fallback.
	// The fix: Series stays empty because the exclusive override [tokyohot]
	// is the only allowed source and tokyohot has no Series.
	assert.Empty(t, movie.Series,
		"Series must be empty — per-field override [tokyohot] is exclusive and tokyohot has no Series")
}

func TestUnknownActressFilteredFromScraperResults(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"mgstage"},
		},
		Metadata: config.MetadataConfig{
			NFO: config.NFOConfig{
				Format: config.NFOFormatConfig{
					UnknownActressText: "Unknown",
				}, // UNKNOWN: ['Format: config.NFOFormatConfig{', '},']
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source: "mgstage",
			ID:     "200GANA-3215",
			Title:  "マジ軟派、初撮。 2172",
			Actresses: []models.ActressInfo{
				{FirstName: "Unknown"},
			},
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Equal(t, 0, len(movie.Actresses), "Actress named 'Unknown' should be filtered out")
}

func TestUnknownActressJapaneseNameFiltered(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"mgstage"},
		},
		Metadata: config.MetadataConfig{
			NFO: config.NFOConfig{
				Format: config.NFOFormatConfig{
					UnknownActressText: "Unknown",
				}, // UNKNOWN: ['Format: config.NFOFormatConfig{', '},']
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source: "mgstage",
			ID:     "200GANA-3215",
			Title:  "マジ軟派、初撮。 2172",
			Actresses: []models.ActressInfo{
				{JapaneseName: "Unknown"},
				{JapaneseName: "テスト女優"},
			},
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Equal(t, 1, len(movie.Actresses), "Only non-Unknown actress should remain")
	assert.Equal(t, "テスト女優", movie.Actresses[0].JapaneseName)
}

func TestUnknownActressFallbackModeKeepsFromScraper(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"mgstage"},
		},
		Metadata: config.MetadataConfig{
			NFO: config.NFOConfig{
				Format: config.NFOFormatConfig{
					UnknownActressMode: models.UnknownActressModeFallback,
					UnknownActressText: "Unknown",
				}, // UNKNOWN: ['Format: config.NFOFormatConfig{', '},']
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source: "mgstage",
			ID:     "200GANA-3215",
			Title:  "マジ軟派、初撮。 2172",
			Actresses: []models.ActressInfo{
				{FirstName: "Unknown"},
			},
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Equal(t, 1, len(movie.Actresses), "Fallback mode should keep Unknown actress from scraper")
}

func TestUnknownActressFallbackModeAddsPlaceholder(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"mgstage"},
		},
		Metadata: config.MetadataConfig{
			NFO: config.NFOConfig{
				Format: config.NFOFormatConfig{
					UnknownActressMode: models.UnknownActressModeFallback,
					UnknownActressText: "Unknown",
				}, // UNKNOWN: ['Format: config.NFOFormatConfig{', '},']
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source: "mgstage",
			ID:     "200GANA-3215",
			Title:  "マジ軟派、初撮。 2172",
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Equal(t, 1, len(movie.Actresses), "Fallback mode should add Unknown placeholder")
	assert.Equal(t, "Unknown", movie.Actresses[0].FirstName)
}

func TestUnknownActressSkipModeNoPlaceholder(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"mgstage"},
		},
		Metadata: config.MetadataConfig{
			NFO: config.NFOConfig{
				Format: config.NFOFormatConfig{
					UnknownActressMode: models.UnknownActressModeSkip,
					UnknownActressText: "Unknown",
				}, // UNKNOWN: ['Format: config.NFOFormatConfig{', '},']
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source: "mgstage",
			ID:     "200GANA-3215",
			Title:  "マジ軟派、初撮。 2172",
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Equal(t, 0, len(movie.Actresses), "Skip mode should not add Unknown placeholder")
}

func TestIsUnknownActress(t *testing.T) {
	tests := []struct {
		name        string
		info        models.ActressInfo
		unknownText string
		want        bool
	}{
		{"first name unknown", models.ActressInfo{FirstName: "Unknown"}, "unknown", true},
		{"japanese name unknown", models.ActressInfo{JapaneseName: "Unknown"}, "unknown", true},
		{"last name unknown", models.ActressInfo{LastName: "Unknown"}, "unknown", true},
		{"case insensitive", models.ActressInfo{FirstName: "UNKNOWN"}, "unknown", true},
		{"normal name", models.ActressInfo{JapaneseName: "テスト女優"}, "unknown", false},
		{"empty unknown text", models.ActressInfo{FirstName: "Unknown"}, "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nameKey := resolveNameKey(tc.info.JapaneseName, tc.info.FirstName, tc.info.LastName)
			got := isUnknownActress(tc.info, nameKey, tc.unknownText)
			assert.Equal(t, tc.want, got)
		})
	}
}
