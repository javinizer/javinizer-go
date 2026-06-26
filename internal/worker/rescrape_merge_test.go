package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
)

// TestMergeRescrapeMovie verifies the rescrape merge helper that lets a
// rescrape honor the caller's preset/scalar_strategy/array_strategy instead
// of wholesale-replacing the existing Movie. See CodeRabbit finding
// rescrape_orchestrator.go:100 (review 4577702137).
func TestMergeRescrapeMovie(t *testing.T) {
	existing := &models.Movie{
		ID:    "OLD-001",
		Title: "Existing Title",
	}
	scraped := &models.Movie{
		ID:      "NEW-001", // rescrape resolved a corrected ID
		Title:   "Scraped Title",
		Runtime: 120, // only the scrape has this
	}

	t.Run("PreferNFO preserves existing fields, keeps scraped ID", func(t *testing.T) {
		merged := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{
			ScalarStrategy: nfo.PreferNFO,
			ArrayStrategy:  true,
		}, "file.mp4")
		// Scraped ID is always preserved (rescrape may correct it).
		assert.Equal(t, "NEW-001", merged.ID)
		// PreferNFO: existing wins when both have a value.
		assert.Equal(t, "Existing Title", merged.Title)
		// A field only in the scrape is carried through.
		assert.Equal(t, 120, merged.Runtime)
	})

	t.Run("PreferScraper takes scraped fields over existing", func(t *testing.T) {
		merged := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{
			ScalarStrategy: nfo.PreferScraper,
			ArrayStrategy:  true,
		}, "file.mp4")
		assert.Equal(t, "NEW-001", merged.ID)
		assert.Equal(t, "Scraped Title", merged.Title)
		assert.Equal(t, 120, merged.Runtime)
	})

	t.Run("nil existing falls back to scraped unchanged", func(t *testing.T) {
		merged := mergeRescrapeMovie(nil, scraped, workflow.MergeOptions{
			ScalarStrategy: nfo.PreferNFO,
		}, "file.mp4")
		assert.Equal(t, scraped, merged)
	})
}
