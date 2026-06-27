package aggregator

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAggregate_PresentEmptyOverrideLeavesFieldEmpty is the core regression for
// the "Remove all" + Save bug. A PRESENT empty override (`series: []`) must mean
// "consult NO scrapers for Series" — the field is left empty — rather than
// falling back to the global priority list. Before the fix, the `len(fp) > 0`
// guard skipped the present-empty override, so Series snapped back to the
// global list (DMM) and was populated. No skip sentinel — suppression is pure
// exclusivity: an empty scraper list consults nothing.
func TestAggregate_PresentEmptyOverrideLeavesFieldEmpty(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"dmm", "r18dev"}},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"},
				Fields: map[string][]string{
					"series": {}, // PRESENT empty: deliberate empty field (no scraper consulted)
				},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))
	t.Logf("resolvedPriorities[Series] = %#v (must be empty, not global)", agg.resolvedPriorities["Series"])

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

	// The bug: present [] was treated as absent, so Series fell back to the
	// global list [dmm, r18dev] and was populated by DMM. The fix: present []
	// resolves to an empty scraper list, so no source is consulted → Series empty.
	assert.Empty(t, movie.Series,
		"present [] override must leave Series empty — no fallback to global/DMM")

	// Fields WITHOUT an override still use the global list (Title from DMM),
	// proving the global fallback for non-overridden fields is intact.
	assert.Equal(t, "DMM Title", movie.Title,
		"Title (no override) must still use the global priority list")
}

// TestAggregateWithPriority_PresentEmptyOverrideLeavesFieldEmpty asserts the
// SAME pure-exclusivity semantics hold in the selected-scrapers scrape path
// (AggregateWithPriority, used by --scrapers / batch UI). A present `series: []`
// leaves Series empty even though customPriority ([dmm]) would otherwise
// populate it — consistency between both scrape paths is the whole point.
func TestAggregateWithPriority_PresentEmptyOverrideLeavesFieldEmpty(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"dmm", "r18dev"}},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"},
				Fields: map[string][]string{
					"series": {}, // PRESENT empty: deliberate empty field
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
		Series:      "Should Not Leak", // DMM HAS Series — must NOT be used
		ReleaseDate: &releaseDate,
	}

	// Simulate the batch scrape path: selected scrapers = [dmm].
	movie, _, err := agg.AggregateWithPriority([]*models.ScraperResult{dmmResult}, []string{"dmm"})
	require.NoError(t, err)
	require.NotNil(t, movie)

	assert.Empty(t, movie.Series,
		"present [] override must leave Series empty in the selected-scrapers path too — no fallback to customPriority [dmm]")

	// Non-overridden fields still use customPriority (DMM).
	assert.Equal(t, "DMM Title", movie.Title,
		"Title (no override) must use customPriority [dmm]")
}

// TestAggregate_PresentEmptyOverrideAbsentFieldStillInherits is the complement:
// an ABSENT key still inherits the global priority list (the unchanged default
// path). Only a PRESENT [] is a deliberate empty field. Guards against the fix
// accidentally emptying every non-overridden field.
func TestAggregate_PresentEmptyOverrideAbsentFieldStillInherits(t *testing.T) {
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
		"absent override must inherit the global list (DMM provides Series) — only PRESENT [] empties the field")
	assert.Equal(t, "DMM Title", movie.Title)
}
