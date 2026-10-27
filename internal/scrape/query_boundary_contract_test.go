package scrape

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type queryContractScraper struct {
	mockScraper
	contexts []context.Context
	queries  []string
}

func (s *queryContractScraper) Search(ctx context.Context, query string) (*models.ScraperResult, error) {
	s.contexts = append(s.contexts, ctx)
	s.queries = append(s.queries, query)
	return s.mockScraper.Search(ctx, query)
}

type panicQueryResolver struct {
	queryContractScraper
	resolved []string
}

func (s *panicQueryResolver) ResolveSearchQuery(query string) (string, bool) {
	s.resolved = append(s.resolved, query)
	panic("fixture query resolver failed")
}

func TestQueryRawContainsOptionalResolverPanic(t *testing.T) {
	adapter := &panicQueryResolver{queryContractScraper: queryContractScraper{mockScraper: mockScraper{name: "panic-resolver", enabled: true}}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(adapter)

	result, failure := NewQueryOnly(registry).QueryRaw(t.Context(), "QUERY-001", adapter.Name())

	require.Nil(t, result)
	require.NotNil(t, failure)
	require.Equal(t, "panic-resolver", failure.Scraper, "stable adapter name must identify the failing hook")
	require.Equal(t, "panic: fixture query resolver failed", failure.Message)
	require.Equal(t, []string{"QUERY-001"}, adapter.resolved)
	require.Empty(t, adapter.queries, "a panicking resolver must not fall through to Search")
	require.Zero(t, adapter.callCount)
}

// Every write capability is recorded: Scrape is a query even when aggregation
// rejects a result, and must leave persistence to its caller.
type queryWriteSpy struct {
	database.MovieRepositoryInterface
	writes []string
}

func (r *queryWriteSpy) Create(context.Context, *models.Movie) error {
	r.writes = append(r.writes, "create")
	return nil
}
func (r *queryWriteSpy) Update(context.Context, *models.Movie) error {
	r.writes = append(r.writes, "update")
	return nil
}
func (r *queryWriteSpy) Delete(context.Context, string) error {
	r.writes = append(r.writes, "delete")
	return nil
}
func (r *queryWriteSpy) Upsert(_ context.Context, movie *models.Movie) (*models.Movie, error) {
	r.writes = append(r.writes, "upsert")
	return movie, nil
}
func (r *queryWriteSpy) UpsertWithTranslations(_ context.Context, movie *models.Movie, _ []models.GenreTranslationData, _ []models.ActressTranslationData) (*models.Movie, error) {
	r.writes = append(r.writes, "upsert translations")
	return movie, nil
}

func TestScrapeDefaultsAndAggregationValidationRemainQueryOnly(t *testing.T) {
	for _, tc := range []struct {
		name, title string
		wantError   bool
	}{
		{name: "nil config and context default safely", title: "Fixture title"},
		{name: "missing required title is rejected", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &queryContractScraper{mockScraper: mockScraper{name: "fixture", enabled: true, result: &models.ScraperResult{Source: "fixture", ID: "QUERY-001", Title: tc.title}}}
			registry := scraperutil.NewScraperRegistry()
			registry.RegisterInstance(adapter)
			agg := aggregator.New(&aggregator.Config{Metadata: &aggregator.MetadataConfig{RequiredFields: []string{"ID", "Title"}}, ScrapersPriority: []string{"fixture"}}, nil, nil, nil)
			repository := &queryWriteSpy{}
			fs := afero.NewMemMapFs()
			engine := New(registry, agg, nil, repository, nil, nil, nil, fs)

			// Explicit selection bypasses cache reads but still resolves RawInput,
			// queries the adapter, and runs the real aggregation/postprocess path.
			result, err := engine.Scrape(nil, ScrapeCmd{RawInput: " QUERY-001 ", SelectedScrapers: []string{"fixture"}})

			if tc.wantError {
				require.EqualError(t, err, "required field validation failed: missing required fields: Title")
				require.Nil(t, result, "invalid metadata must not be published as a scrape result")
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, StatusCompleted, result.Status)
				require.Equal(t, "QUERY-001", result.RequestedMovieID)
				require.Equal(t, "QUERY-001", result.Movie.ID)
				require.Equal(t, tc.title, result.Movie.Title)
				require.Equal(t, "fixture", result.FieldSources["title"])
			}
			require.Equal(t, []string{"QUERY-001"}, adapter.queries)
			require.Len(t, adapter.contexts, 1)
			require.NotNil(t, adapter.contexts[0], "nil public context must become a usable context")
			require.NoError(t, adapter.contexts[0].Err())
			require.Empty(t, repository.writes)
			entries, readErr := afero.ReadDir(fs, ".")
			require.NoError(t, readErr)
			require.Empty(t, entries, "query must not generate poster or metadata files")
		})
	}
}
