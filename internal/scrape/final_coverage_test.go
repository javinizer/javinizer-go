package scrape

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	appconfig "github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/translation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type finalMovieRepo struct {
	database.MovieRepositoryInterface
	movie *models.Movie
	err   error
}

func (r *finalMovieRepo) FindByID(context.Context, string) (*models.Movie, error) {
	return r.movie, r.err
}

type finalScrapeActressRepo struct {
	database.ActressRepositoryInterface
}

func (r *finalScrapeActressRepo) FindByDMMID(context.Context, int) (*models.Actress, error) {
	return &models.Actress{ThumbURL: "https://db.example/thumb.jpg"}, nil
}

type finalTranslator struct{ calls int }

func (t *finalTranslator) Translate(context.Context, *models.Movie) (string, bool, *translation.TranslationOutput) {
	t.calls++
	return "", true, nil
}

type contextResolverScraper struct {
	*stubScrapeWithResult
	resolved string
	err      error
}

func (s *contextResolverScraper) ResolveContentID(string) (string, error) { return s.resolved, s.err }
func (s *contextResolverScraper) ResolveContentIDCtx(context.Context, string) (string, error) {
	return s.resolved, s.err
}

type panicQueryScraper struct{ *stubScrapeWithResult }

func (s *panicQueryScraper) ResolveSearchQuery(string) (string, bool) { panic("query resolver panic") }

type successfulURLScraper struct{ *stubScrapeWithResult }

func (s *successfulURLScraper) CanHandleURL(string) bool                { return true }
func (s *successfulURLScraper) ExtractIDFromURL(string) (string, error) { return "URL-123", nil }
func (s *successfulURLScraper) ScrapeURL(context.Context, string) (*models.ScraperResult, error) {
	return nil, nil
}

type finalAggregator struct {
	movie *models.Movie
	err   error
}

func (a *finalAggregator) Aggregate([]*models.ScraperResult) (*models.Movie, *aggregator.AggregateResult, error) {
	return a.movie, nil, a.err
}
func (a *finalAggregator) AggregateWithPriority([]*models.ScraperResult, []string) (*models.Movie, *aggregator.AggregateResult, error) {
	return a.movie, nil, a.err
}
func (a *finalAggregator) ReloadReplacementCaches(context.Context) {}

func TestValidateAndResolverEnrichmentRemainingBranches(t *testing.T) {
	movie := &models.Movie{Actresses: []models.Actress{{ThumbURL: "   "}}}
	assert.Zero(t, validateActressThumbnails(movie, &Config{}))

	complete := models.Actress{FirstName: "Complete", LastName: "Name", JapaneseName: "完全", ThumbURL: "https://example.com/thumb.jpg"}
	resolver := &testMetadataResolver{name: "unused", enabled: true}
	assert.Zero(t, enrichActressesFromResolvers(t.Context(), &models.Movie{Actresses: []models.Actress{complete}}, newTestRegistry(resolver), &Config{ScrapeActress: true, ValidateActressThumbnail: func(context.Context, string) error { return nil }}, &[]string{}))
	assert.Zero(t, resolver.calls)

	first := &testMetadataResolver{name: "first", enabled: true, metadata: models.ActressInfo{FirstName: "First", LastName: "Last", JapaneseName: "完全", ThumbURL: "https://example.com/thumb.jpg"}}
	second := &testMetadataResolver{name: "second", enabled: true}
	assert.Equal(t, 1, enrichActressesFromResolvers(t.Context(), &models.Movie{Actresses: []models.Actress{{}}}, newTestRegistry(first, second), &Config{ScrapeActress: true, ValidateActressThumbnail: func(context.Context, string) error { return nil }}, &[]string{}))
	assert.Zero(t, second.calls)
}

func TestTryCacheValidTranslationAndBothEnrichmentSources(t *testing.T) {
	oldLookup := lookupBuiltinActress
	t.Cleanup(func() { lookupBuiltinActress = oldLookup })
	lookupBuiltinActress = func(id int, _, _, _ string) (actresscache.Record, bool) {
		if id == 2 {
			return actresscache.Record{DMMID: 2, FirstName: "BuiltIn"}, true
		}
		return actresscache.Record{}, false
	}
	cached := &models.Movie{
		ID:           "CACHE-1",
		Translations: []models.MovieTranslation{{Language: "en", SettingsHash: "same"}},
		Actresses:    []models.Actress{{DMMID: 1}, {DMMID: 2}},
	}
	s := New(nil, nil, nil, &finalMovieRepo{movie: cached}, nil, &Config{TranslationEnabled: true, TranslationTargetLang: "en", TranslationSettingsHash: "same", ActressDBEnabled: true}, nil, nil)
	result := s.tryCache(t.Context(), ScrapeCmd{MovieID: "CACHE-1"}, &finalScrapeActressRepo{}, false, time.Now())
	require.NotNil(t, result)
	assert.Equal(t, "https://db.example/thumb.jpg", result.Movie.Actresses[0].ThumbURL)
	assert.Equal(t, "BuiltIn", result.Movie.Actresses[1].FirstName)
	assert.True(t, result.NeedsPersistence)
}

func TestTranslatorConstructorDisabledAndNewNilConfig(t *testing.T) {
	assert.IsType(t, noOpTranslator{}, NewTranslatorFromApp(&appconfig.TranslationConfig{}))
	s := New(nil, nil, nil, nil, nil, nil, nil, nil)
	require.NotNil(t, s.cfg)
	require.NotNil(t, s.fs)
	require.NotNil(t, s.translator)
}

func TestContextContentIDResolverAndQueryPanic(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resolved string
		err      error
		want     string
	}{
		{"success", "mapped", nil, "mapped"},
		{"failure", "", errors.New("resolve failed"), "original"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := scraperutil.NewScraperRegistry()
			reg.RegisterInstance(&contextResolverScraper{stubScrapeWithResult: &stubScrapeWithResult{name: "ctx", enabled: true, result: &models.ScraperResult{}}, resolved: tc.resolved, err: tc.err})
			s := NewQueryOnly(reg)
			assert.Equal(t, tc.want, s.resolveContentID(t.Context(), "original", []string{"ctx"}))
		})
	}
	outcome := querySingle(t.Context(), "id", "", &panicQueryScraper{stubScrapeWithResult: &stubScrapeWithResult{name: "panic-query", enabled: true, result: &models.ScraperResult{}}})
	require.NotNil(t, outcome.failure)
	assert.Contains(t, outcome.failure.Message, "query resolver panic")
}

func TestResolveScrapeInputSuccessfulURLBranches(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&successfulURLScraper{stubScrapeWithResult: &stubScrapeWithResult{name: "url", enabled: true, result: &models.ScraperResult{}}})
	cfg := &Config{ScrapersPriority: []string{"url"}}
	resolved, err := resolveScrapeInput(t.Context(), ScrapeCmd{RawInput: "https://example.com/video"}, reg, cfg)
	require.NoError(t, err)
	assert.Equal(t, "URL-123", resolved.MovieID)
	assert.Equal(t, []string{"url"}, resolved.PriorityOverride)

	resolved, err = resolveScrapeInput(t.Context(), ScrapeCmd{RawInput: "https://example.com/video", SelectedScrapers: []string{"url"}}, reg, cfg)
	require.NoError(t, err)
	assert.Equal(t, []string{"url"}, resolved.SelectedScrapers)
}

func TestPostProcessAllOptionalEnrichmentsAndTranslation(t *testing.T) {
	oldLookup := lookupBuiltinActress
	t.Cleanup(func() { lookupBuiltinActress = oldLookup })
	lookupBuiltinActress = func(id int, _, _, _ string) (actresscache.Record, bool) {
		if id == 2 {
			return actresscache.Record{DMMID: 2, FirstName: "BuiltIn"}, true
		}
		return actresscache.Record{}, false
	}
	translator := &finalTranslator{}
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 1}, {DMMID: 2}}}
	result, err := postProcessScraped(t.Context(), movie, nil, nil, nil, &Config{ActressDBEnabled: true, TranslationEnabled: true}, translator, &finalScrapeActressRepo{}, ScrapeCmd{}, false, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 1, translator.calls)
	assert.Equal(t, "BuiltIn", result.Movie.Actresses[1].FirstName)
}

func TestScrapeNilContextAggregationErrorFallbackAndPostProcessError(t *testing.T) {
	// Nil context and non-nil actress repository assignment, ending at no-results.
	emptyReg := scraperutil.NewScraperRegistry()
	emptyEngine := New(emptyReg, nil, &finalScrapeActressRepo{}, nil, nil, &Config{}, nil, nil)
	failed, err := emptyEngine.Scrape(nil, ScrapeCmd{MovieID: "NONE", ForceRefresh: true})
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, failed.Status)

	reg := scraperutil.NewScraperRegistry()
	resolver := &contextResolverScraper{stubScrapeWithResult: &stubScrapeWithResult{name: "ctx", enabled: true, result: &models.ScraperResult{Source: "ctx", Title: "Title"}}, resolved: "mapped"}
	reg.RegisterInstance(resolver)

	aggErr := errors.New("aggregate failed")
	engine := New(reg, &finalAggregator{err: aggErr}, nil, nil, nil, &Config{ScrapersPriority: []string{"ctx"}}, nil, nil)
	_, err = engine.Scrape(t.Context(), ScrapeCmd{MovieID: "original", ForceRefresh: true})
	assert.ErrorIs(t, err, aggErr)

	engine = New(reg, &finalAggregator{movie: &models.Movie{}}, nil, nil, nil, &Config{ScrapersPriority: []string{"ctx"}}, nil, nil)
	result, err := engine.Scrape(t.Context(), ScrapeCmd{MovieID: "original", ForceRefresh: true})
	require.NoError(t, err)
	assert.Equal(t, "mapped", result.Movie.ContentID)

	oldPostProcess := runPostProcessScraped
	runPostProcessScraped = func(context.Context, *models.Movie, []*models.ScraperResult, *aggregator.AggregateResult, ScraperInstanceResolver, *Config, Translator, database.ActressRepositoryInterface, ScrapeCmd, bool, time.Time) (*ScrapeResult, error) {
		return nil, errors.New("post-process failed")
	}
	_, err = engine.Scrape(t.Context(), ScrapeCmd{MovieID: "original", ForceRefresh: true})
	runPostProcessScraped = oldPostProcess
	assert.ErrorContains(t, err, "post-process failed")
}
