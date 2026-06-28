package aggregator

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAggregate_PresentEmptyOverrideInheritsGlobal is the core regression test
// for the upgrade-safe semantics: a PRESENT empty override (`series: []`) must
// inherit the global priority list — NOT wipe the field. Commit 9f882f22 made
// per-field priority exclusive and documented "[] still means 'inherit global'",
// but the implementation treated [] as "consult no scrapers", wiping every field
// for configs carrying [] (common from the pre-9f882f22 merge era). The fix
// restores [] = inherit, so Series is populated by DMM via the global list.
func TestAggregate_PresentEmptyOverrideInheritsGlobal(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"dmm", "r18dev"}},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"},
				Fields: map[string][]string{
					"series": {}, // PRESENT empty: inherits global (NOT a deliberate empty field)
				},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))
	t.Logf("resolvedPriorities[Series] = %#v (must inherit global [dmm r18dev], not empty)", agg.resolvedPriorities["Series"])
	assert.Equal(t, []string{"dmm", "r18dev"}, agg.resolvedPriorities["Series"],
		"present [] override must resolve to the global priority list, not an empty list")

	releaseDate := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	results := []*models.ScraperResult{
		{
			Source:      "dmm",
			ID:          "ABC-001",
			Title:       "DMM Title",
			Series:      "Inherited Series", // DMM HAS Series — IS used via global inheritance
			ReleaseDate: &releaseDate,
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// present [] inherits global [dmm, r18dev] → DMM consulted → Series populated.
	assert.Equal(t, "Inherited Series", movie.Series,
		"present [] override must inherit the global list — Series populated by DMM, NOT wiped")

	// Fields WITHOUT an override also use the global list (Title from DMM).
	assert.Equal(t, "DMM Title", movie.Title)
}

// TestAggregateWithPriority_PresentEmptyOverrideInheritsCustomPriority asserts
// the same upgrade-safe semantics hold in the selected-scrapers scrape path
// (AggregateWithPriority, used by --scrapers / batch UI). A present `series: []`
// inherits customPriority (the user-selected scrapers) rather than wiping
// Series — consistency between both scrape paths is the whole point.
func TestAggregateWithPriority_PresentEmptyOverrideInheritsCustomPriority(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"dmm", "r18dev"}},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"},
				Fields: map[string][]string{
					"series": {}, // PRESENT empty: inherits customPriority
				},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	releaseDate := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	dmmResult := &models.ScraperResult{
		Source:      "dmm",
		ID:          "ABC-001",
		Title:       "DMM Title",
		Series:      "Inherited Series", // DMM HAS Series — IS used via customPriority inheritance
		ReleaseDate: &releaseDate,
	}

	// Simulate the batch scrape path: selected scrapers = [dmm].
	movie, _, err := agg.AggregateWithPriority([]*models.ScraperResult{dmmResult}, []string{"dmm"})
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Equal(t, "Inherited Series", movie.Series,
		"present [] override must inherit customPriority [dmm] in the selected-scrapers path — Series populated, NOT wiped")

	// Non-overridden fields also use customPriority (DMM).
	assert.Equal(t, "DMM Title", movie.Title)
}

// TestAggregate_SkipSentinelLeavesFieldEmpty locks in the deliberate-suppress
// path: `series: ["__skip__"]` leaves Series empty because "__skip__" matches no
// real scraper, so assignString consults nothing. This is the supported way to
// force an empty field now that [] means "inherit global" (commit 9f882f22's
// documented skip sentinel).
func TestAggregate_SkipSentinelLeavesFieldEmpty(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"dmm", "r18dev"}},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"},
				Fields: map[string][]string{
					"series": {"__skip__"}, // deliberate suppress: matches no scraper
				},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))
	t.Logf("resolvedPriorities[Series] = %#v (must be [__skip__] — matches no scraper)", agg.resolvedPriorities["Series"])
	assert.Equal(t, []string{"__skip__"}, agg.resolvedPriorities["Series"],
		"skip sentinel override resolves to itself (non-empty, so exclusive)")

	releaseDate := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	results := []*models.ScraperResult{
		{
			Source:      "dmm",
			ID:          "ABC-001",
			Title:       "DMM Title",
			Series:      "Should Not Leak", // DMM HAS Series — must NOT be used
			ReleaseDate: &releaseDate,
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Empty(t, movie.Series,
		`["__skip__"] override must leave Series empty — "__skip__" matches no real scraper`)
	assert.Equal(t, "DMM Title", movie.Title,
		"Title (no override) must still use the global priority list")
}

// TestAggregate_AbsentOverrideInheritsGlobal is the complement: an ABSENT key
// inherits the global priority list (the unchanged default path). Guards against
// the fix accidentally emptying every non-overridden field.
func TestAggregate_AbsentOverrideInheritsGlobal(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"dmm", "r18dev"}},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"},
				// "series" is ABSENT — must inherit global, NOT be empty.
				Fields: map[string][]string{
					"title": {"dmm"}, // a different field has an override
				},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	releaseDate := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	results := []*models.ScraperResult{
		{
			Source:      "dmm",
			ID:          "ABC-001",
			Title:       "DMM Title",
			Series:      "Inherited Series", // absent override → global [dmm,r18dev] → DMM provides it
			ReleaseDate: &releaseDate,
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// Absent override → inherits global → DMM consulted → Series populated.
	assert.Equal(t, "Inherited Series", movie.Series,
		"absent override must inherit the global list (DMM provides Series) — only [\"__skip__\"] empties the field")
	assert.Equal(t, "DMM Title", movie.Title)
}
