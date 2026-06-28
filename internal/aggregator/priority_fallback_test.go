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

// TestAggregateWithPriorityRespectsPerFieldSkip verifies that AggregateWithPriority
// (the selected-scrapers scrape path, scrape.go:311) honors a per-field __skip__
// override instead of applying customPriority to every field.
//
// Regression for the BMD-284 bug: batch-scraping with DMM selected while
// metadata.priority.series: ["__skip__"] still populated Series from DMM,
// because AggregateWithPriority used cmd.SelectedScrapers as a flat priority for
// every field and ignored the per-field Fields override. The per-field override
// must be exclusive and win over customPriority (same semantics as
// resolvePriorities, PR #51/#50); customPriority is only the fallback for fields
// without an override.
// TestAggregateWithPriority_HonorsExclusivePerFieldOverride is the BMD-284
// regression: scraping with selected scrapers (--scrapers dmm) must still honor
// an exclusive per-field override. `series: [tokyohot]` means Series comes from
// tokyohot ONLY — tokyohot didn't run (only dmm did) and there is NO fallback,
// so Series stays empty even though DMM provides it. This is the v1 (original
// PowerShell Javinizer) exclusivity semantics (#50), now applied consistently
// in the AggregateWithPriority path too. No skip sentinel — suppression is the
// emergent result of pointing a field at a scraper that didn't run / lacks it.
func TestAggregateWithPriority_HonorsExclusivePerFieldOverride(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"dmm", "r18dev"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"},
				Fields: map[string][]string{
					"series": {"tokyohot"}, // exclusive: only tokyohot consulted for Series
				},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	releaseDate := time.Date(2004, 3, 12, 0, 0, 0, 0, time.UTC)

	dmmResult := &models.ScraperResult{
		Source:      "dmm",
		Language:    "ja",
		ID:          "BMD-284",
		Title:       "DMM Title",
		Maker:       "ビッグモーカル",
		Series:      "淫楽口ワイヤル", // DMM HAS Series — must NOT leak in via fallback
		ReleaseDate: &releaseDate,
	}

	results := []*models.ScraperResult{dmmResult}

	// Simulate the batch scrape path: selected scrapers = [dmm].
	movie, _, err := agg.AggregateWithPriority(results, []string{"dmm"})
	require.NoError(t, err)
	require.NotNil(t, movie)

	// The BMD-284 bug: Series was populated by DMM because customPriority [dmm]
	// was applied to every field, ignoring the exclusive `series: [tokyohot]`
	// override. The fix: the per-field override is exclusive, tokyohot didn't
	// run, so Series stays empty — no fallback to DMM.
	assert.Empty(t, movie.Series,
		"Series must be empty — exclusive override [tokyohot] has no fallback to DMM")

	// Fields WITHOUT an override use customPriority (DMM), proving the fallback
	// for non-overridden fields is intact.
	assert.Equal(t, "DMM Title", movie.Title,
		"Title (no override) must use customPriority [dmm]")
	assert.Equal(t, "ビッグモーカル", movie.Maker,
		"Maker (no override) must use customPriority [dmm]")
}

// TestAggregateWithPriority_PerFieldOverrideSelectsSource verifies the
// selective-source-choice that motivated reverting to pure exclusivity:
// `maker: [r18dev]` routes Maker to r18dev even when scraping with `--scrapers
// dmm` (dmm ran, but the exclusive per-field override picks r18dev — which must
// also have run to contribute). This works consistently in both scrape paths.
func TestAggregateWithPriority_PerFieldOverrideSelectsSource(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"dmm", "r18dev"}},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"},
				Fields: map[string][]string{
					"maker": {"r18dev"}, // selective source choice: Maker from r18dev, not dmm
				},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{Source: "dmm", ID: "X-1", Title: "DMM Title", Maker: "DMM Maker"},
		{Source: "r18dev", ID: "X-1", Title: "R18 Title", Maker: "R18 Maker"},
	}

	// Both ran (--scrapers dmm,r18dev). The exclusive `maker: [r18dev]` override
	// picks r18dev for Maker, while Title (no override) uses customPriority order
	// ([dmm, r18dev] → dmm first).
	movie, _, err := agg.AggregateWithPriority(results, []string{"dmm", "r18dev"})
	require.NoError(t, err)
	assert.Equal(t, "R18 Maker", movie.Maker,
		"Maker must come from r18dev — exclusive per-field override selects the source")
	assert.Equal(t, "DMM Title", movie.Title,
		"Title (no override) uses customPriority [dmm, r18dev] → dmm first")
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

// TestResolvePrioritiesNilMetadataNoPanic verifies that a config with no
// Metadata block (MetadataConfigFromApp returns nil) does not panic during
// resolvePriorities, and that field priorities fall back to ScrapersPriority.
// Regression guard for the CodeRabbit finding on PR #51: resolvePriorities
// dereferenced a.cfg.Metadata without a nil check, which would panic for
// configs that rely only on ScrapersPriority.
func TestResolvePrioritiesNilMetadataNoPanic(t *testing.T) {
	cfg := &Config{
		Metadata:         nil, // no metadata block — MetadataConfigFromApp returns nil
		ScrapersPriority: []string{"dmm", "r18dev"},
	}

	// Before the fix, this panicked: a.cfg != nil was true but a.cfg.Metadata
	// was nil, so a.cfg.Metadata.Priority.GetFieldPriority(...) dereferenced nil.
	assert.NotPanics(t, func() {
		_ = newAggregatorNoDB(cfg) // New() calls resolvePriorities()
	})

	agg := newAggregatorNoDB(cfg)
	// Fields without a per-field override fall back to the global ScrapersPriority.
	assert.Equal(t, []string{"dmm", "r18dev"}, agg.resolvedPriorities["ID"])
	assert.Equal(t, []string{"dmm", "r18dev"}, agg.resolvedPriorities["Series"])
	assert.Equal(t, []string{"dmm", "r18dev"}, agg.resolvedPriorities["Actress"])
}

// TestGetFieldPriorityFromConfig_PerFieldOverride covers the non-empty per-field
// override branch (return fp) of getFieldPriorityFromConfig. The only in-tree
// caller (resolvePriorities) passes fieldKey="" for the global, so the
// per-field return path is otherwise unreachable — exercise it directly.
func TestGetFieldPriorityFromConfig_PerFieldOverride(t *testing.T) {
	cfg := &Config{
		Metadata: &MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"}, // global
				Fields: map[string][]string{
					"title": {"r18dev", "libredmm"}, // non-empty per-field override
				},
			},
		},
	}

	// A present non-empty per-field override is returned verbatim (exclusive).
	got := getFieldPriorityFromConfig(cfg, "title")
	assert.Equal(t, []string{"r18dev", "libredmm"}, got)

	// A field with NO override falls through to the global Priority list.
	gotGlobal := getFieldPriorityFromConfig(cfg, "series")
	assert.Equal(t, []string{"dmm", "r18dev"}, gotGlobal)
}
