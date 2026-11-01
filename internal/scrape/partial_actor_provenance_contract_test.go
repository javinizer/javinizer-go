package scrape

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// A custom AggregatorInterface adapter can retain partial actors that the default
// aggregator filters. Source evidence alone must not manufacture an identity key.
func TestScrapePartialAdapterActorDoesNotFabricateProvenance(t *testing.T) {
	f := newFixture(t).withCachedMovie("ABC-001", "Persisted title")
	before, err := f.movieRepo.FindByID(t.Context(), "ABC-001")
	require.NoError(t, err)
	const sourceName = "identity-adapter"
	partialInfo := models.ActressInfo{ThumbURL: "https://example.invalid/partial.jpg"}
	neighborInfo := models.ActressInfo{DMMID: 72, FirstName: "Known", LastName: "Neighbor"}
	raw := &models.ScraperResult{Source: sourceName, ID: "ABC-001", ContentID: "raw001", Title: "Raw title", Actresses: []models.ActressInfo{partialInfo, neighborInfo}}
	rawBefore := raw.Clone()
	partial := models.Actress{ThumbURL: partialInfo.ThumbURL}
	partial.SetScrapeEvidence(sourceName, partialInfo, 0)
	neighbor := models.Actress{DMMID: 72, FirstName: "Known", LastName: "Neighbor"}
	neighbor.SetScrapeEvidence(sourceName, neighborInfo, 0)
	adapter := &partialIdentityAggregator{movie: &models.Movie{ID: "ABC-001", ContentID: "raw001", Title: "Aggregate title", Actresses: []models.Actress{partial, neighbor}}}
	source := &mockScraper{name: sourceName, enabled: true, result: raw}
	f.registry.RegisterInstance(source)
	engine := New(f.registry, adapter, nil, f.movieRepo, nil, &Config{ScrapersPriority: []string{sourceName}}, nil, nil)
	cmd := ScrapeCmd{MovieID: "ABC-001", ForceRefresh: true}

	for _, repaired := range []bool{false, true} {
		if repaired {
			// The adapter supplies a newly resolved identity on retry; raw scrape
			// evidence remains the original partial record, not rewritten history.
			adapter.movie.Actresses[0].DMMID = 71
		}
		prototype := adapter.movie.Clone()
		result, err := engine.Scrape(t.Context(), cmd)
		require.NoError(t, err)
		require.Equal(t, StatusCompleted, result.Status)
		require.NotContains(t, result.ActressSources, "")
		wantSources := map[string]string{"dmmid:72": sourceName}
		if repaired {
			wantSources["dmmid:71"] = sourceName
		}
		require.Equal(t, wantSources, result.ActressSources)
		require.Len(t, result.Movie.Credits, len(wantSources))
		require.Len(t, result.Movie.Actresses, len(wantSources))
		known := result.Movie.Credits[len(result.Movie.Credits)-1]
		require.Equal(t, 72, known.Scraped.DMMID)
		require.Equal(t, "Known", known.Scraped.FirstName)
		require.Equal(t, "Neighbor", known.Scraped.LastName)
		require.Equal(t, sourceName, known.Source)
		require.Equal(t, string(models.CreditOriginScrape), known.Origin)
		if repaired {
			require.Equal(t, 71, result.Movie.Credits[0].Scraped.DMMID)
			require.Equal(t, sourceName, result.Movie.Credits[0].Source)
		}
		require.Equal(t, prototype, adapter.movie, "Scrape must not mutate adapter-owned metadata")
		require.Equal(t, rawBefore, raw, "identity resolution must not rewrite source evidence")
		require.Equal(t, []*models.ScraperResult{rawBefore}, result.ScraperResults)
		after, err := f.movieRepo.FindByID(t.Context(), "ABC-001")
		require.NoError(t, err)
		require.Equal(t, before, after, "query must not persist its newly resolved cast")
		for _, table := range []string{"actresses", "movie_credits", "movie_actresses"} {
			var count int64
			require.NoError(t, f.db.DB.Table(table).Count(&count).Error)
			require.Zero(t, count, "query must not create rows in %s", table)
		}
	}
	require.Equal(t, 2, adapter.calls)
	require.Equal(t, 2, source.callCount)
}
