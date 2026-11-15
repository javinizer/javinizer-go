package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestCompleteRescrapeOrphansUseCommittedPredecessor(t *testing.T) {
	for _, tc := range []struct {
		name, current, callerOld, next, sibling string
		want                                    []string
	}{
		{name: "duplicate caller ID", current: "OLD-1", callerOld: "OLD-1", next: "NEW-1", want: []string{"OLD-1"}},
		{name: "stale caller ID", current: "CURRENT-1", callerOld: "STALE-1", next: "NEW-1", want: []string{"CURRENT-1"}},
		{name: "missing caller ID", current: "OLD-1", next: "NEW-1", want: []string{"OLD-1"}},
		{name: "missing predecessor", callerOld: "STALE-1", next: "NEW-1"},
		{name: "unchanged ID", current: "SAME-1", callerOld: "STALE-1", next: "SAME-1"},
		{name: "sibling retains predecessor", current: "OLD-1", callerOld: "OLD-1", next: "NEW-1", sibling: "OLD-1"},
		{name: "empty replacement ID", current: "OLD-1", callerOld: "OLD-1", want: []string{"OLD-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const path = "/source/movie.mp4"
			rm := resultstore.New(2, []string{path, "/source/sibling.mp4"})
			require.NoError(t, rm.CommitResult(path, &resultstore.MovieResult{Movie: &models.Movie{ID: tc.current}, Status: models.JobStatusCompleted}, 0))
			if tc.sibling != "" {
				require.NoError(t, rm.CommitResult("/source/sibling.mp4", &resultstore.MovieResult{Movie: &models.Movie{ID: tc.sibling}, Status: models.JobStatusCompleted}, 0))
			}
			inputs := makeRescrapeInputs(nil)
			inputs.ResultMap = rm
			replacement := &resultstore.MovieResult{Movie: &models.Movie{ID: tc.next}, Status: models.JobStatusCompleted}
			got, err := NewRescrapePhase().CompleteRescrape(inputs, path, replacement, 1, tc.next, tc.callerOld, nil)
			require.NoError(t, err)
			require.Equal(t, models.RescrapeStatusSuccess, got.Status)
			require.Equal(t, tc.want, got.OrphanedMovieIDs)
			require.Equal(t, tc.next, rm.GetCurrentMovieID(path))
			require.Equal(t, uint64(2), rm.GetRevision(path))
		})
	}
}

func TestMergeRescrapeMovieSuccessfulStrategiesPreserveScrapedBaseline(t *testing.T) {
	for _, strategy := range []nfo.MergeStrategy{nfo.PreferScraper, nfo.PreferNFO, nfo.MergeArrays, nfo.PreserveExisting, nfo.FillMissingOnly} {
		for _, hasExisting := range []bool{false, true} {
			name := string(strategy) + "/scraped-only"
			if hasExisting {
				name = string(strategy) + "/both-sources"
			}
			t.Run(name, func(t *testing.T) {
				scraped := &models.Movie{ID: "NEW-1", Title: "Fresh title"}
				scraped.Poster.PosterURL = "https://example.invalid/fresh.jpg"
				scraped.Poster.ShouldCropPoster = true
				var existing *models.Movie
				if hasExisting {
					existing = &models.Movie{ID: "OLD-1", Title: "Curated title"}
					existing.Poster.PosterURL = "https://example.invalid/stale.jpg"
				}
				got := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{ScalarStrategy: strategy, ArrayStrategy: true}, "/source/movie.mp4")
				require.NotNil(t, got)
				require.Equal(t, "NEW-1", got.ID)
				require.Equal(t, scraped.Poster.PosterURL, got.Poster.PosterURL)
				require.Equal(t, scraped.Poster.PosterURL, got.Poster.OriginalPosterURL)
				require.NotNil(t, got.Poster.OriginalShouldCropPoster)
				require.True(t, *got.Poster.OriginalShouldCropPoster)
				if !hasExisting || strategy == nfo.PreferScraper || strategy == nfo.MergeArrays {
					require.Equal(t, "Fresh title", got.Title)
				} else {
					require.Equal(t, "Curated title", got.Title)
				}
			})
		}
	}
}
