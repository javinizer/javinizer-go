package scrape

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// The exported aggregator interface permits adapters to return partial metadata.
// This tests the engine's fallback contract, not the default aggregator's output.
type partialIdentityAggregator struct {
	movie    *models.Movie
	calls    int
	selected []string
	inputs   []*models.ScraperResult
}

func (a *partialIdentityAggregator) Aggregate(in []*models.ScraperResult) (*models.Movie, *aggregator.AggregateResult, error) {
	a.calls++
	a.inputs = in
	return a.movie.Clone(), &aggregator.AggregateResult{FieldSources: map[string]string{"title": "identity-adapter"}}, nil
}
func (a *partialIdentityAggregator) AggregateWithPriority(in []*models.ScraperResult, selected []string) (*models.Movie, *aggregator.AggregateResult, error) {
	a.selected = append([]string(nil), selected...)
	return a.Aggregate(in)
}
func (*partialIdentityAggregator) ReloadReplacementCaches(context.Context) {}

var _ aggregator.AggregatorInterface = (*partialIdentityAggregator)(nil)

type fallbackIdentityScraper struct {
	mockScraper
	resolved string
	searched string
}

func (s *fallbackIdentityScraper) ResolveContentID(string) (string, error) { return s.resolved, nil }
func (s *fallbackIdentityScraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	s.searched = id
	return s.mockScraper.Search(ctx, id)
}

func TestScrapePartialAggregateContentIDFallbackDoesNotReplaceEvidence(t *testing.T) {
	for _, selected := range []bool{false, true} {
		mode := "configured priority"
		if selected {
			mode = "selected priority"
		}
		t.Run(mode, func(t *testing.T) {
			for _, tc := range []struct{ name, resolved, aggregate, want string }{
				{"fill omitted content ID", "resolved001", "", "resolved001"},
				{"preserve aggregate content ID", "resolved001", "aggregate001", "aggregate001"},
				{"original input is not new evidence", "ABC-001", "", ""},
				{"empty resolution is not evidence", "", "", ""},
			} {
				t.Run(tc.name, func(t *testing.T) {
					f := newFixture(t).withCachedMovie("ABC-001", "Persisted title")
					before, err := f.movieRepo.FindByID(t.Context(), "ABC-001")
					require.NoError(t, err)
					raw := &models.ScraperResult{Source: "identity-adapter", ID: "ABC-001", ContentID: "raw001", Title: "Raw title"}
					source := &fallbackIdentityScraper{mockScraper: mockScraper{name: "identity-adapter", enabled: true, result: raw}, resolved: tc.resolved}
					f.registry.RegisterInstance(source)
					aggregate := &partialIdentityAggregator{movie: &models.Movie{ID: "ABC-001", ContentID: tc.aggregate, Title: "Aggregate title"}}
					engine := New(f.registry, aggregate, nil, f.movieRepo, nil, &Config{ScrapersPriority: []string{"identity-adapter"}}, nil, nil)
					cmd := ScrapeCmd{MovieID: "ABC-001", ForceRefresh: true}
					if selected {
						cmd.SelectedScrapers = []string{"identity-adapter"}
					}
					result, err := engine.Scrape(t.Context(), cmd)
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, StatusCompleted, result.Status)
					require.Equal(t, "ABC-001", result.RequestedMovieID)
					require.Equal(t, before.ContentID, result.ExpectedContentID)
					require.Equal(t, tc.want, result.Movie.ContentID)
					require.Equal(t, "Aggregate title", result.Movie.Title)
					require.Equal(t, "ABC-001", result.Movie.ID)
					require.Equal(t, "identity-adapter", result.FieldSources["title"])
					require.Equal(t, tc.aggregate, aggregate.movie.ContentID, "engine must not mutate adapter-owned prototype")
					require.Equal(t, 1, aggregate.calls)
					require.Equal(t, cmd.SelectedScrapers, aggregate.selected)
					require.Equal(t, tc.resolved, source.searched)
					require.Equal(t, 1, source.callCount)
					require.Equal(t, []*models.ScraperResult{raw}, aggregate.inputs)
					require.Equal(t, []*models.ScraperResult{raw}, result.ScraperResults)
					require.Equal(t, "raw001", raw.ContentID, "fallback must not rewrite raw evidence")
					require.Equal(t, "Raw title", raw.Title)
					after, err := f.movieRepo.FindByID(t.Context(), "ABC-001")
					require.NoError(t, err)
					require.Equal(t, before, after, "Scrape is a query, not persistence")
				})
			}
		})
	}
}
