package scrape

import (
	"context"
	"errors"
	"testing"

	"net/http"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	httpclientiface "github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockScraper struct {
	name         string
	enabled      bool
	result       *models.ScraperResult
	err          error
	callCount    int
	errFirstCall bool
}

func (m *mockScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	m.callCount++
	if m.errFirstCall && m.callCount == 1 {
		return nil, errors.New("first call fails")
	}
	return m.result, m.err
}

func (m *mockScraper) Name() string                                        { return m.name }
func (m *mockScraper) GetURL(_ context.Context, id string) (string, error) { return "", nil }
func (m *mockScraper) IsEnabled() bool                                     { return m.enabled }
func (m *mockScraper) Config() *models.ScraperSettings {
	if m.result != nil {
		return &models.ScraperSettings{Enabled: m.enabled}
	}
	return nil
}
func (m *mockScraper) Close() error { return nil }

type mockMovieRepository struct {
	database.MovieRepositoryInterface
	upsertErr error
	deleteErr error
	findErr   error
}

func (m *mockMovieRepository) Upsert(ctx context.Context, movie *models.Movie) (*models.Movie, error) {
	if m.upsertErr != nil {
		return nil, m.upsertErr
	}
	return m.MovieRepositoryInterface.Upsert(ctx, movie)
}

func (m *mockMovieRepository) Delete(ctx context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	return m.MovieRepositoryInterface.Delete(ctx, id)
}

func (m *mockMovieRepository) FindByID(ctx context.Context, id string) (*models.Movie, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	return m.MovieRepositoryInterface.FindByID(ctx, id)
}

type testFixture struct {
	t          *testing.T
	db         *database.DB
	cfg        *config.Config
	registry   *scraperutil.ScraperRegistry
	movieRepo  database.MovieRepositoryInterface
	agg        *aggregator.Aggregator
	httpClient httpclientiface.HTTPClient
}

func newFixture(t *testing.T) *testFixture {
	t.Helper()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	movieRepo := database.NewMovieRepository(db)
	_, err = config.Prepare(cfg)
	require.NoError(t, err)
	return &testFixture{
		t:         t,
		db:        db,
		cfg:       cfg,
		registry:  scraperutil.NewScraperRegistry(),
		movieRepo: &mockMovieRepository{MovieRepositoryInterface: movieRepo},
	}
}

func (f *testFixture) buildAggregator() *aggregator.Aggregator {
	cfg := &aggregator.Config{
		Metadata:         aggregator.MetadataConfigFromApp(&f.cfg.Metadata),
		ScrapersPriority: f.cfg.Scrapers.Priority,
	}
	return aggregator.New(cfg,
		aggregator.NewGenreProcessor(cfg.Metadata, database.NewGenreReplacementRepository(f.db)),
		aggregator.NewWordProcessor(cfg.Metadata, database.NewWordReplacementRepository(f.db)),
		aggregator.NewAliasResolver(cfg.Metadata, database.NewActressAliasRepository(f.db)),
	)
}

func (f *testFixture) withScraper(name string, result *models.ScraperResult, err error) *testFixture {
	if result != nil && result.Source == "" {
		result.Source = name
	}
	f.registry.RegisterInstance(&mockScraper{name: name, enabled: true, result: result, err: err})
	f.cfg.Scrapers.Priority = append(f.cfg.Scrapers.Priority, name)
	return f
}

func (f *testFixture) withDisabledScraper(name string) *testFixture {
	f.registry.RegisterInstance(&mockScraper{name: name, enabled: false})
	return f
}

func (f *testFixture) withPriority(priority []string) *testFixture {
	f.cfg.Scrapers.Priority = priority
	return f
}

func (f *testFixture) withCachedMovie(id, title string) *testFixture {
	_, err := f.movieRepo.Upsert(context.Background(), &models.Movie{ID: id, Title: title})
	require.NoError(f.t, err)
	return f
}

func (f *testFixture) withMockOverrides(upsertErr, deleteErr, findErr error) *testFixture {
	mock := f.movieRepo.(*mockMovieRepository)
	mock.upsertErr = upsertErr
	mock.deleteErr = deleteErr
	mock.findErr = findErr
	return f
}

func (f *testFixture) withHTTPClient() *testFixture {
	f.httpClient = &http.Client{}
	return f
}

func (f *testFixture) build() *Scraper {
	f.agg = f.buildAggregator()
	cfg := &Config{
		ScrapersPriority:      f.cfg.Scrapers.Priority,
		TranslationEnabled:    f.cfg.Metadata.Translation.Enabled,
		TranslationTargetLang: f.cfg.Metadata.Translation.TargetLanguage,
		ActressDBEnabled:      f.cfg.Metadata.ActressDatabase.Enabled,
		UserAgent:             f.cfg.Scrapers.UserAgent,
		Referer:               f.cfg.Scrapers.Referer,
		TempDir:               f.cfg.System.TempDir,
	}
	if cfg.TranslationEnabled {
		cfg.TranslationSettingsHash = f.cfg.Metadata.Translation.SettingsHash()
	}
	return New(f.registry, f.agg, database.NewActressRepository(f.db), f.movieRepo, f.httpClient, cfg, nil, nil)
}

func TestScrape_EmptyMovieID(t *testing.T) {
	s := newFixture(t).build()
	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: ""}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty MovieID")
	assert.Nil(t, result)
}

func TestScrape_AllScrapersFail(t *testing.T) {
	s := newFixture(t).
		withScraper("failing", nil, errors.New("network error")).
		build()
	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, StatusFailed, result.Status)
}

func TestScrape_CacheHit(t *testing.T) {
	f := newFixture(t)
	f.withCachedMovie("TEST-001", "Cached Movie")
	s := f.build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StatusCompleted, result.Status)
	assert.Equal(t, "Cached Movie", result.Movie.Title)
}

func TestScrape_CacheMiss_Scrapes(t *testing.T) {
	s := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Scraped Movie", Maker: "Test Studio"}, nil).
		build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StatusCompleted, result.Status)
	require.NotNil(t, result.Movie)
	assert.Equal(t, "TEST-001", result.Movie.ID)
}

func TestScrape_ForceRefresh_BypassesCache(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Freshly Scraped"}, nil)
	f.withCachedMovie("TEST-001", "Cached Movie")
	s := f.build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001", ForceRefresh: true}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)
	assert.Equal(t, "Freshly Scraped", result.Movie.Title)

	// Scrape no longer persists — the cached movie remains unchanged.
	// Callers (e.g. Workflow.Scrape) handle cache deletion and persistence.
	saved, err := f.movieRepo.FindByID(context.Background(), "TEST-001")
	assert.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, "Cached Movie", saved.Title, "cache should remain unchanged — Scrape is a pure query")
}

func TestScrape_NoDBWrites(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "No Persist"}, nil)
	s := f.build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)

	// Scrape is a pure query — it should NOT persist to the database.
	// Persistence is the caller's responsibility (e.g. Workflow.Scrape).
	_, err = f.movieRepo.FindByID(context.Background(), "TEST-001")
	assert.True(t, database.IsNotFound(err), "Scrape should not save movie to database")
}

func TestScrape_ProgressCallback(t *testing.T) {
	s := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Progress"}, nil).
		build()

	type progressCall struct {
		step ProgressStep
		pct  float64
	}
	var calls []progressCall
	progress := func(step ProgressStep, pct float64, msg string) {
		calls = append(calls, progressCall{step: step, pct: pct})
	}

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, progress)
	assert.NoError(t, err)
	require.NotNil(t, result)
	require.Greater(t, len(calls), 0, "should have progress callbacks")
	require.Equal(t, ProgressStepScrape, calls[0].step, "first callback should be 'scrape'")
	for i := 1; i < len(calls); i++ {
		assert.GreaterOrEqual(t, calls[i].pct, calls[i-1].pct, "percentages should be monotonically non-decreasing")
	}
}

func TestScrape_DisabledScraperSkipped(t *testing.T) {
	s := newFixture(t).
		withDisabledScraper("disabled").
		withScraper("enabled", &models.ScraperResult{ID: "TEST-001", Title: "From Enabled"}, nil).
		withPriority([]string{"disabled", "enabled"}).
		build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)
	assert.Equal(t, "From Enabled", result.Movie.Title)
}

func TestScrape_ContextCancellation(t *testing.T) {
	s := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Cancelled"}, nil).
		build()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := s.Scrape(ctx, ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StatusFailed, result.Status)
	assert.NotEmpty(t, result.Message, "Scrape with cancelled context should produce a message on result")
	assert.Contains(t, result.Message, "context canceled", "message should mention cancellation")
}

func TestScrape_CustomScrapers_SelectsSpecificScraper(t *testing.T) {
	s := newFixture(t).
		withScraper("excluded", &models.ScraperResult{Source: "excluded", ID: "TEST-001", Title: "Wrong"}, nil).
		withScraper("selected", &models.ScraperResult{Source: "selected", ID: "TEST-001", Title: "Correct"}, nil).
		withPriority([]string{"excluded", "selected"}).
		build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{
		MovieID:          "TEST-001",
		SelectedScrapers: []string{"selected"},
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)
	assert.Equal(t, "Correct", result.Movie.Title)
}

func TestScrape_WithHTTPClient_DoesNotPanic(t *testing.T) {
	s := newFixture(t).
		withHTTPClient().
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "HTTP Test"}, nil).
		build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
}

func TestScrape_ReturnsResultWithoutDBWrite(t *testing.T) {
	f := newFixture(t).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Result Only"}, nil)
	s := f.build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)
	assert.Equal(t, "TEST-001", result.Movie.ID)

	// Scrape is a pure query — should NOT persist to the database.
	// Callers (e.g. Workflow.Scrape) are responsible for persistence.
	_, findErr := f.movieRepo.FindByID(context.Background(), "TEST-001")
	assert.True(t, database.IsNotFound(findErr), "Scrape should not save movie to database")

	require.NotNil(t, result.FieldSources, "FieldSources should be populated")
	assert.Equal(t, "mock", result.FieldSources["id"])
}

func TestScrape_FindByIDError_ContinuesToScrape(t *testing.T) {
	fixture := newFixture(t)
	fixture.withMockOverrides(nil, nil, errors.New("db connection lost"))
	fixture.withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Scraped After DB Error"}, nil)
	s := fixture.build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)
	assert.Equal(t, "Scraped After DB Error", result.Movie.Title)
	assert.Equal(t, StatusCompleted, result.Status)
	require.NotNil(t, result.FieldSources, "should have field sources from scrape (not cache)")
	assert.Equal(t, "mock", result.FieldSources["id"])
}

func TestScrape_ForceRefresh_SkipsCache(t *testing.T) {
	f := newFixture(t)
	f.withCachedMovie("TEST-001", "Cached")
	f.withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Force Refreshed"}, nil)
	s := f.build()

	// ForceRefresh skips the cache lookup and scrapes fresh.
	// Cache deletion is now the caller's responsibility (not Scrape's).
	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001", ForceRefresh: true}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)
	assert.Equal(t, "Force Refreshed", result.Movie.Title)

	// Note: Scrape no longer deletes from cache — the old cached movie still exists.
	// Callers (e.g. Workflow.Scrape) handle cache deletion before calling Scrape.
}

func TestScrape_UpsertNotCalled(t *testing.T) {
	f := newFixture(t).
		withMockOverrides(errors.New("disk full"), nil, nil).
		withScraper("mock", &models.ScraperResult{ID: "TEST-001", Title: "Unsaveable", Maker: "Test Studio"}, nil)
	s := f.build()

	// Scrape is a pure query — it should NOT call Upsert, even when movieRepo has errors.
	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, StatusCompleted, result.Status)
	require.NotNil(t, result.Movie, "movie data should be returned")
	assert.Equal(t, "Unsaveable", result.Movie.Title, "movie title should be preserved in result")
}

func TestNormalizeNameForKey(t *testing.T) {
	tests := []struct {
		name string
		in   string
		out  string
	}{
		{"whitespace collapse", "  Hello  World  ", "hello world"},
		{"empty", "", ""},
		{"only spaces", "  ", ""},
		{"lowercase", "ABC", "abc"},
		{"mixed case", "MiXeD CaSe", "mixed case"},
		{"unicode japanese", "鈴村 あいり", "鈴村 あいり"},
		{"unicode mixed", "Suzumura 鈴木", "suzumura 鈴木"},
		{"punctuation", "O'Brien", "o'brien"},
		{"leading/trailing unicode", "　スペース　", "スペース"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.out, models.NormalizeActressNameKey(tc.in), "models.NormalizeActressNameKey(%q)", tc.in)
		})
	}
}

func TestActressSourceKeyFromModel(t *testing.T) {
	tests := []struct {
		name     string
		actress  models.Actress
		expected string
	}{
		{name: "DMMID", actress: models.Actress{DMMID: 123}, expected: "dmmid:123"},
		{name: "JapaneseName", actress: models.Actress{JapaneseName: " Suzumura Airi "}, expected: "name:suzumura airi"},
		{name: "FirstName+LastName", actress: models.Actress{FirstName: "Airi", LastName: "Suzumura"}, expected: "name:airi suzumura"},
		{name: "LastName+FirstName", actress: models.Actress{LastName: "Suzumura", FirstName: "Airi"}, expected: "name:airi suzumura"},
		{name: "Empty", actress: models.Actress{}, expected: ""},
		{name: "DMMID overrides name", actress: models.Actress{DMMID: 99, JapaneseName: "Test"}, expected: "dmmid:99"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, actressSourceKeyFromModel(tc.actress))
		})
	}
}

func TestActressSourceKeysFromInfo_Dedup(t *testing.T) {
	keys := actressSourceKeysFromInfo(models.ActressInfo{DMMID: 123, JapaneseName: "鈴村あいり", FirstName: "Airi", LastName: "Suzumura"})
	assert.Contains(t, keys, "dmmid:123")
	assert.Contains(t, keys, "name:鈴村あいり")
}

func TestActressSourceKeysFromInfo_Empty(t *testing.T) {
	keys := actressSourceKeysFromInfo(models.ActressInfo{})
	assert.Empty(t, keys)
}

func TestResolveScraperNames(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, resolveScraperNames([]string{"a", "b"}, nil, nil))
	assert.Equal(t, []string{"c"}, resolveScraperNames(nil, []string{"c"}, nil))
	cfg := &Config{ScrapersPriority: []string{"d"}}
	assert.Equal(t, []string{"d"}, resolveScraperNames(nil, nil, cfg))
	assert.Nil(t, resolveScraperNames(nil, nil, nil))
	assert.Equal(t, []string{"a"}, resolveScraperNames([]string{"a"}, []string{"b"}, cfg))
	cfg.ScrapersPriority = []string{"d"}
	assert.Equal(t, []string{"d"}, resolveScraperNames([]string{}, nil, cfg))
}

func TestBuildNoResultsError(t *testing.T) {
	assert.Equal(t, "No results from any scraper", buildNoResultsError(nil))
	assert.Contains(t, buildNoResultsError([]models.ScraperError{{Scraper: "s1", Cause: errors.New("timeout")}}), "s1: timeout")
	assert.Contains(t, buildNoResultsError([]models.ScraperError{{Scraper: "s1"}}), "s1: no result")
}

func TestBuildActressSourcesFromScrapeResults_Nil(t *testing.T) {
	assert.Nil(t, buildActressSourcesFromScrapeResults(nil, nil, nil, nil))
	results := []*models.ScraperResult{{Source: "src", ID: "MOV-001"}}
	assert.Nil(t, buildActressSourcesFromScrapeResults(results, nil, nil, nil))
	assert.Nil(t, buildActressSourcesFromScrapeResults(results, nil, nil, []models.Actress{}))
}

func TestBuildActressSourcesFromScrapeResults_MatchByDMMID(t *testing.T) {
	results := []*models.ScraperResult{
		{Source: "src1", ID: "MOV-001", Actresses: []models.ActressInfo{
			{DMMID: 100, JapaneseName: "Test", FirstName: "First", LastName: "Last"},
		}},
	}
	actresses := []models.Actress{
		{DMMID: 100, JapaneseName: "Test", FirstName: "First", LastName: "Last"},
	}
	sources := buildActressSourcesFromScrapeResults(results, nil, nil, actresses)
	assert.Equal(t, "src1", sources["dmmid:100"])
}

func TestBuildActressSourcesFromScrapeResults_CustomPriority(t *testing.T) {
	results := []*models.ScraperResult{
		{Source: "src1", ID: "MOV-001", Actresses: []models.ActressInfo{{DMMID: 100, JapaneseName: "Test"}}},
		{Source: "src2", ID: "MOV-001", Actresses: []models.ActressInfo{{DMMID: 100, JapaneseName: "Test"}}},
	}
	actresses := []models.Actress{
		{DMMID: 100, JapaneseName: "Test"},
	}
	sources := buildActressSourcesFromScrapeResults(results, nil, []string{"src2", "src1"}, actresses)
	assert.Equal(t, "src2", sources["dmmid:100"])
}

func TestBuildFieldSourcesFromCachedMovie(t *testing.T) {
	assert.Nil(t, buildFieldSourcesFromCachedMovie(nil))

	m := &models.Movie{ID: "MOV-001", Title: "Test", SourceName: "src1"}
	sources := buildFieldSourcesFromCachedMovie(m)
	assert.Equal(t, "src1", sources["id"])
	assert.Equal(t, "src1", sources["title"])

	m2 := &models.Movie{ID: "MOV-001", Title: "Test"}
	sources2 := buildFieldSourcesFromCachedMovie(m2)
	assert.Equal(t, "scraper", sources2["id"])
}

func TestBuildActressSourcesFromCachedMovie(t *testing.T) {
	assert.Nil(t, buildActressSourcesFromCachedMovie(nil))
	assert.Nil(t, buildActressSourcesFromCachedMovie(&models.Movie{}))

	m := &models.Movie{
		SourceName: "src1",
		Actresses:  []models.Actress{{DMMID: 50, FirstName: "Actress", LastName: "A"}},
	}
	sources := buildActressSourcesFromCachedMovie(m)
	assert.Equal(t, "src1", sources["dmmid:50"])
}

type mockQueryResolverScraper struct {
	*mockScraper
	resolvedQuery string
	matched       bool
}

func (m *mockQueryResolverScraper) ResolveSearchQuery(input string) (string, bool) {
	return m.resolvedQuery, m.matched
}

func TestQuerySingle_NoResolver(t *testing.T) {
	ms := &mockScraper{name: "basic", result: &models.ScraperResult{ID: "MOV-001", Title: "Direct"}, err: nil}
	outcome := querySingle(context.Background(), "MOV-001", ms)
	require.NotNil(t, outcome.result)
	assert.Equal(t, "MOV-001", outcome.result.ID)
}

func TestQuerySingle_ResolverUsesMappedQuery(t *testing.T) {
	ms := &mockQueryResolverScraper{
		mockScraper:   &mockScraper{name: "resolver", result: &models.ScraperResult{ID: "MOV-001", Title: "Resolved"}, err: nil},
		resolvedQuery: "MAPPED-001",
		matched:       true,
	}
	outcome := querySingle(context.Background(), "MOV-001", ms)
	require.NotNil(t, outcome.result)
}

func TestQuerySingle_ResolverRetriesOnError(t *testing.T) {
	ms := &mockQueryResolverScraper{
		mockScraper:   &mockScraper{name: "retry", result: &models.ScraperResult{ID: "MOV-001", Title: "Retried"}, errFirstCall: true},
		resolvedQuery: "MAPPED-001",
		matched:       true,
	}
	outcome := querySingle(context.Background(), "MOV-001", ms)
	require.NotNil(t, outcome.result)
	assert.Equal(t, "Retried", outcome.result.Title)
}

func TestQuerySingle_ResolverBothFail(t *testing.T) {
	firstErr := errors.New("mapped query failed")
	ms := &mockQueryResolverScraper{
		mockScraper:   &mockScraper{name: "fail", result: nil, err: firstErr},
		matched:       true,
		resolvedQuery: "MAPPED-001",
	}
	outcome := querySingle(context.Background(), "MOV-001", ms)
	require.NotNil(t, outcome.failure)
	assert.Equal(t, "fail", outcome.failure.Scraper)
	assert.ErrorContains(t, outcome.failure, "mapped query failed",
		"retry error should wrap the original mapped-query error")
}

func TestQuerySingle_CancelledContext(t *testing.T) {
	ms := &mockScraper{name: "cancel", result: nil, err: nil}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome := querySingle(ctx, "MOV-001", ms)
	require.Nil(t, outcome.result)
	require.NotNil(t, outcome.failure)
	assert.ErrorIs(t, outcome.failure, context.Canceled)
}

func TestQuerySingle_ResolverReturnsNoMatch(t *testing.T) {
	ms := &mockQueryResolverScraper{
		mockScraper:   &mockScraper{name: "no_match", result: &models.ScraperResult{ID: "MOV-001", Title: "Direct"}, err: nil},
		resolvedQuery: "",
		matched:       false,
	}
	outcome := querySingle(context.Background(), "MOV-001", ms)
	require.NotNil(t, outcome.result)
	assert.Equal(t, "MOV-001", outcome.result.ID)
	assert.Nil(t, outcome.failure)
}

func TestScrape_ActressEnrichment(t *testing.T) {
	f := newFixture(t)
	actressRepo := database.NewActressRepository(f.db)
	err := actressRepo.Create(context.Background(), &models.Actress{
		DMMID: 100, JapaneseName: "Test Actress", FirstName: "Test", LastName: "Actress",
		ThumbURL: "https://example.com/thumb.jpg",
	})
	require.NoError(t, err)

	f.withScraper("mock", &models.ScraperResult{
		ID: "TEST-001", Title: "Enriched Movie",
		Actresses: []models.ActressInfo{
			{DMMID: 100, JapaneseName: "Test Actress"},
		},
	}, nil)
	s := f.build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)
	require.Len(t, result.Movie.Actresses, 1)
	assert.Equal(t, "https://example.com/thumb.jpg", result.Movie.Actresses[0].ThumbURL,
		"actress should be enriched from DB")
}

func TestResolveScraperNames_PriorityOverrideScrapeCmd(t *testing.T) {
	s := newFixture(t).
		withScraper("override", &models.ScraperResult{ID: "TEST-001", Title: "Override Win"}, nil).
		withScraper("default", &models.ScraperResult{ID: "TEST-001", Title: "Default"}, nil).
		withPriority([]string{"default", "override"}).
		build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001", PriorityOverride: []string{"override"}}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)
	assert.Equal(t, "Override Win", result.Movie.Title)
}
