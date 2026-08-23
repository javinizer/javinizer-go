package worker

// POSTER-WRITE-HARDENING P2 — write-back editable-set + provenance merge
// (D5/R12-7/R13-7): per-key live-wins semantics, plus original_title/director
// coverage on the movie merge.

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyWriteBack_MergesEditableSet_AdditionalFields(t *testing.T) {
	t.Run("original_title_and_director", func(t *testing.T) {
		baseline := &models.Movie{ID: "X-1", OriginalTitle: "orig", Director: "dir-a"}
		phaseOut := &models.Movie{ID: "X-1", OriginalTitle: "orig", Director: "dir-stale"}
		live := &models.Movie{ID: "X-1", OriginalTitle: "orig-user", Director: "dir-a"} // user edited original_title mid-phase
		merged := mergeLiveReviewEdits(baseline, phaseOut, live)
		assert.Equal(t, "orig-user", merged.OriginalTitle, "live drift wins")
		assert.Equal(t, "dir-stale", merged.Director, "unedited field: live==baseline ⇒ phase-side value wins")
	})

	t.Run("provenance_per_key_live_wins", func(t *testing.T) {
		frozen := &resultstore.ProvenanceData{
			FieldSources:   map[string]string{"title": "scraper-a", "director": "scraper-b"},
			ActressSources: map[string]string{"A-ONE": "scraper"},
		}
		live := &resultstore.ProvenanceData{
			FieldSources:   map[string]string{"title": "user"},
			ActressSources: map[string]string{"A-ONE": "user"},
		}
		got := mergeWriteBackProvenance(frozen, live)
		require.NotNil(t, got)
		assert.Equal(t, map[string]string{"title": "user", "director": "scraper-b"}, got.FieldSources,
			"live wins its edited keys; frozen keeps the rest")
		assert.Equal(t, map[string]string{"A-ONE": "user"}, got.ActressSources)
	})

	t.Run("scraper_results_resolve_as_one_global_set", func(t *testing.T) {
		frozen := &resultstore.ProvenanceData{
			FieldSources:   map[string]string{"director": "scraper"},
			ScraperResults: []*models.ScraperResult{{Source: "dmm", Title: "old"}},
		}
		liveSame := &resultstore.ProvenanceData{
			FieldSources:   map[string]string{"title": "user"},
			ScraperResults: []*models.ScraperResult{{Source: "dmm", Title: "old"}},
		}
		kept := mergeWriteBackProvenance(frozen, liveSame)
		require.Len(t, kept.ScraperResults, 1)
		assert.Equal(t, "old", kept.ScraperResults[0].Title, "unchanged raw set falls back to frozen")
		assert.Equal(t, map[string]string{"director": "scraper", "title": "user"}, kept.FieldSources)

		liveRescrape := &resultstore.ProvenanceData{
			FieldSources:   map[string]string{"title": "rescrape"},
			ScraperResults: []*models.ScraperResult{{Source: "r18dev", Title: "new"}},
		}
		updated := mergeWriteBackProvenance(frozen, liveRescrape)
		require.Len(t, updated.ScraperResults, 1)
		assert.Equal(t, "r18dev", updated.ScraperResults[0].Source, "changed live raw set wins globally")
	})

	t.Run("provenance_empty_maps_collapse_to_nil", func(t *testing.T) {
		got := mergeWriteBackProvenance(&resultstore.ProvenanceData{}, &resultstore.ProvenanceData{})
		require.NotNil(t, got)
		assert.Nil(t, got.FieldSources)
		assert.Nil(t, got.ActressSources)
	})

	t.Run("provenance_nil_sides", func(t *testing.T) {
		frozen := &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "scraper"}}
		live := &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "user"}}
		assert.Nil(t, mergeWriteBackProvenance(nil, nil))
		gotFrozen := mergeWriteBackProvenance(frozen, nil)
		require.NotNil(t, gotFrozen)
		assert.Equal(t, map[string]string{"title": "scraper"}, gotFrozen.FieldSources, "nil live ⇒ frozen")
		gotLive := mergeWriteBackProvenance(nil, live)
		require.NotNil(t, gotLive)
		assert.Equal(t, map[string]string{"title": "user"}, gotLive.FieldSources, "nil frozen ⇒ live")
	})
}
