package nfo_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/stretchr/testify/require"
)

func TestMergeMovieMetadataWithOptions_OriginalCropPointerContract(t *testing.T) {
	states := []string{"nil", "false", "true"}
	strategies := []nfo.MergeStrategy{
		nfo.PreferScraper, nfo.PreferNFO, nfo.MergeArrays,
		nfo.PreserveExisting, nfo.FillMissingOnly,
	}
	timestamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	newMovie := func(state string) *models.Movie {
		movie := &models.Movie{
			ID: "ABC-123", ContentID: "abc00123", Title: "Title",
			CreatedAt: timestamp.Add(-time.Hour), UpdatedAt: timestamp,
		}
		if state != "nil" {
			value := state == "true"
			movie.Poster.OriginalShouldCropPoster = &value
		}
		return movie
	}
	snapshot := func(movie *models.Movie) models.Movie {
		copy := *movie
		copy.Poster = movie.Poster.Clone()
		return copy
	}

	for _, source := range []string{"scraper", "nfo"} {
		for _, state := range states {
			for _, strategy := range strategies {
				for _, mergeArrays := range []bool{false, true} {
					t.Run(fmt.Sprintf("single/%s/%s/%s/arrays=%t", source, state, strategy, mergeArrays), func(t *testing.T) {
						movie := newMovie(state)
						before := snapshot(movie)
						pointer := movie.Poster.OriginalShouldCropPoster
						var scraped, existing *models.Movie
						if source == "scraper" {
							scraped = movie
						} else {
							existing = movie
						}
						result, err := nfo.MergeMovieMetadataWithOptions(scraped, existing, strategy, mergeArrays)
						require.NoError(t, err)
						require.Same(t, movie, result.Merged)
						require.True(t, pointer == result.Merged.Poster.OriginalShouldCropPoster)
						require.Equal(t, before, *movie)
						count := 3 // ID, ContentID and Title; timestamps are not metadata fields.
						provenance, present := result.Provenance["OriginalShouldCropPoster"]
						if state == "true" {
							count++
							require.True(t, present)
							require.Equal(t, source, provenance.Source)
							require.Equal(t, 1.0, provenance.Confidence)
							require.Equal(t, &timestamp, provenance.LastUpdated)
						} else {
							require.False(t, present)
						}
						require.Len(t, result.Provenance, count)
						expectedStats := nfo.MergeStats{TotalFields: count}
						if source == "scraper" {
							expectedStats.FromScraper = count
						} else {
							expectedStats.FromNFO = count
						}
						require.Equal(t, expectedStats, result.Stats)
					})
				}
			}
		}
	}

	// The generic two-source merger does not merge this reset-baseline pointer.
	// Worker rescrape code establishes its own baseline after the generic merge.
	for _, scrapedState := range states {
		for _, nfoState := range states {
			for _, strategy := range strategies {
				for _, mergeArrays := range []bool{false, true} {
					t.Run(fmt.Sprintf("both/%s/%s/%s/arrays=%t", scrapedState, nfoState, strategy, mergeArrays), func(t *testing.T) {
						scraped, existing := newMovie(scrapedState), newMovie(nfoState)
						scrapedBefore, existingBefore := snapshot(scraped), snapshot(existing)
						scrapedPointer, existingPointer := scraped.Poster.OriginalShouldCropPoster, existing.Poster.OriginalShouldCropPoster
						result, err := nfo.MergeMovieMetadataWithOptions(scraped, existing, strategy, mergeArrays)
						require.NoError(t, err)
						require.NotSame(t, scraped, result.Merged)
						require.NotSame(t, existing, result.Merged)
						require.Nil(t, result.Merged.Poster.OriginalShouldCropPoster)
						require.NotContains(t, result.Provenance, "OriginalShouldCropPoster")
						require.Equal(t, 3, result.Stats.TotalFields)
						require.Equal(t, scrapedBefore, *scraped)
						require.Equal(t, existingBefore, *existing)
						require.True(t, scrapedPointer == scraped.Poster.OriginalShouldCropPoster)
						require.True(t, existingPointer == existing.Poster.OriginalShouldCropPoster)
					})
				}
			}
		}
	}
}
