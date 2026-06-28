package aggregator

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAggregate_AllFieldsEmptyOverrideInheritsGlobal is the end-to-end regression
// test for the user-reported bug: an upgraded config carries EVERY per-field
// priority as `[]` (common from the pre-9f882f22 merge era, where [] was harmless
// because it was merged with global). After 9f882f22 made per-field priority
// exclusive, those [] entries wiped every field — id/content_id/poster_url all
// empty — so scraped movies collapsed into a single movie group (movie_id="") in
// the review UI and posters showed "No poster".
//
// With the fix ([] = inherit global), an all-[] config must aggregate id and
// content_id from the global priority list (non-empty), restoring correct
// per-movie grouping.
func TestAggregate_AllFieldsEmptyOverrideInheritsGlobal(t *testing.T) {
	emptyFields := map[string][]string{}
	for _, f := range []string{
		"id", "content_id", "title", "original_title", "description",
		"director", "maker", "label", "series", "poster_url", "cover_url",
		"trailer_url", "runtime", "release_date", "rating", "actress",
		"genre", "screenshot_url",
	} {
		emptyFields[f] = []string{} // every field explicitly []
	}

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"dmm", "r18dev"}},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"dmm", "r18dev"},
				Fields:   emptyFields,
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	// Every field's resolved priority must be the global list, not an empty list.
	assert.Equal(t, []string{"dmm", "r18dev"}, agg.resolvedPriorities["ID"],
		"id: [] must inherit global — this is the field the review UI groups by")
	assert.Equal(t, []string{"dmm", "r18dev"}, agg.resolvedPriorities["ContentID"],
		"content_id: [] must inherit global")
	assert.Equal(t, []string{"dmm", "r18dev"}, agg.resolvedPriorities["PosterURL"],
		"poster_url: [] must inherit global — otherwise the review UI shows 'No poster'")

	releaseDate := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	results := []*models.ScraperResult{
		{
			Source:      "dmm",
			ID:          "ABC-001",
			ContentID:   "1abc001",
			Title:       "DMM Title",
			PosterURL:   "https://example.com/poster.jpg",
			Series:      "DMM Series",
			Maker:       "DMM Maker",
			ReleaseDate: &releaseDate,
		},
	}

	movie, _, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)

	// The headline assertions: id and content_id MUST be populated (not wiped),
	// so FileMatchInfo.MovieID stays distinct per file and the review UI does not
	// collapse all files into one movie group.
	assert.Equal(t, "ABC-001", movie.ID, "id must be aggregated from global priority — empty [] must not wipe it")
	assert.Equal(t, "1abc001", movie.ContentID, "content_id must be aggregated from global priority — empty [] must not wipe it")
	assert.Equal(t, "https://example.com/poster.jpg", movie.Poster.PosterURL, "poster_url must be aggregated from global priority — empty [] must not wipe it")
	assert.Equal(t, "DMM Title", movie.Title)
	assert.Equal(t, "DMM Series", movie.Series)
}
