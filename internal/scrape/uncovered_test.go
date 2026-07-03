package scrape

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ScrapeStatus JSON marshaling (scrape.go) ---

func TestScrapeStatus_MarshalJSON_Uncovered(t *testing.T) {
	status := StatusCompleted
	data, err := json.Marshal(status)
	require.NoError(t, err)
	assert.Equal(t, `"completed"`, string(data))
}

func TestScrapeStatus_UnmarshalJSON_Uncovered(t *testing.T) {
	var s ScrapeStatus
	err := json.Unmarshal([]byte(`"failed"`), &s)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, s)
}

func TestScrapeStatus_UnmarshalJSON_InvalidUncovered(t *testing.T) {
	var s ScrapeStatus
	err := json.Unmarshal([]byte(`123`), &s)
	assert.Error(t, err)
}

// --- Provenance / actress (provenance.go) ---

func TestBuildActressSourcesFromScrapeResults_ResolvedPrioritiesUncovered(t *testing.T) {
	results := []*models.ScraperResult{
		{Source: "src1", ID: "MOV-001", Actresses: []models.ActressInfo{{DMMID: 100, JapaneseName: "Test"}}},
	}
	actresses := []models.Actress{{DMMID: 100, JapaneseName: "Test"}}
	resolvedPriorities := map[string][]string{"Actress": {"src1"}}
	sources := buildActressSourcesFromScrapeResults(results, resolvedPriorities, nil, actresses)
	assert.Equal(t, "src1", sources["dmmid:100"])
}

func TestBuildActressSourcesFromScrapeResults_EmptySourceSkippedUncovered(t *testing.T) {
	results := []*models.ScraperResult{
		{Source: "", ID: "MOV-001"},
		{Source: "  ", ID: "MOV-002"},
	}
	actresses := []models.Actress{{DMMID: 100}}
	sources := buildActressSourcesFromScrapeResults(results, nil, nil, actresses)
	assert.Nil(t, sources, "empty/whitespace sources should be skipped")
}

func TestBuildActressSourcesFromScrapeResults_NilResultSkippedUncovered(t *testing.T) {
	results := []*models.ScraperResult{nil, {Source: "src1", ID: "MOV-001"}}
	actresses := []models.Actress{{DMMID: 100}}
	sources := buildActressSourcesFromScrapeResults(results, nil, nil, actresses)
	assert.Nil(t, sources, "nil result should not contribute actress sources")
}

func TestBuildActressSourcesFromScrapeResults_NoActressMatchUncovered(t *testing.T) {
	results := []*models.ScraperResult{
		{Source: "src1", ID: "MOV-001", Actresses: []models.ActressInfo{{DMMID: 999}}},
	}
	actresses := []models.Actress{{DMMID: 100}}
	sources := buildActressSourcesFromScrapeResults(results, nil, nil, actresses)
	assert.Nil(t, sources, "no matching actresses should return nil")
}

func TestBuildFieldSourcesFromCachedMovie_ShouldCropPosterUncovered(t *testing.T) {
	m := &models.Movie{
		ID:         "MOV-001",
		SourceName: "src1",
		Poster:     models.PosterState{ShouldCropPoster: true},
	}
	sources := buildFieldSourcesFromCachedMovie(m)
	assert.Equal(t, "src1", sources["should_crop_poster"])
}

func TestBuildFieldSourcesFromCachedMovie_AllFieldsUncovered(t *testing.T) {
	now := time.Now()
	m := &models.Movie{
		ID:            "MOV-001",
		ContentID:     "mov001",
		Title:         "Title",
		DisplayTitle:  "Display",
		OriginalTitle: "Original",
		Description:   "Desc",
		Director:      "Dir",
		Maker:         "Maker",
		Label:         "Label",
		Series:        "Series",
		Poster:        models.PosterState{PosterURL: "p.jpg", CoverURL: "c.jpg"},
		TrailerURL:    "trailer.mp4",
		Runtime:       120,
		RatingScore:   8.5,
		RatingVotes:   100,
		Actresses:     []models.Actress{{DMMID: 1}},
		Genres:        []models.Genre{{Name: "Drama"}},
		Screenshots:   []string{"ss1.jpg"},
		ReleaseDate:   &now,
	}
	sources := buildFieldSourcesFromCachedMovie(m)
	expectedFields := []string{"id", "content_id", "title", "display_title", "original_title",
		"description", "director", "maker", "label", "series",
		"poster_url", "cover_url", "trailer_url", "runtime",
		"release_date", "rating_score", "rating_votes",
		"actresses", "genres", "screenshot_urls"}
	for _, field := range expectedFields {
		assert.Equal(t, "scraper", sources[field], "field %s should have source 'scraper'", field)
	}
}

func TestBuildFieldSourcesFromCachedMovie_NoFieldsSetUncovered(t *testing.T) {
	m := &models.Movie{}
	sources := buildFieldSourcesFromCachedMovie(m)
	assert.Nil(t, sources)
}

func TestBuildActressSourcesFromCachedMovie_EmptySourceNameUncovered(t *testing.T) {
	m := &models.Movie{
		Actresses: []models.Actress{{DMMID: 50, FirstName: "A", LastName: "B"}},
	}
	sources := buildActressSourcesFromCachedMovie(m)
	assert.Equal(t, "scraper", sources["dmmid:50"])
}

// --- Actress enrichment (actress.go) ---

func TestEnrichActressesFromDB_DisabledUncovered(t *testing.T) {
	count := enrichActressesFromDB(context.Background(), nil, nil, &Config{ActressDBEnabled: false})
	assert.Equal(t, 0, count)
}

func TestEnrichActressesFromDB_NilCfgUncovered(t *testing.T) {
	count := enrichActressesFromDB(context.Background(), nil, nil, nil)
	assert.Equal(t, 0, count)
}

func TestEnrichActressesFromDB_NilMovieUncovered(t *testing.T) {
	mockRepo := &mockActressRepoForUncovered{}
	count := enrichActressesFromDB(context.Background(), nil, mockRepo, &Config{ActressDBEnabled: true})
	assert.Equal(t, 0, count)
}

func TestEnrichActressesFromDB_NilRepoUncovered(t *testing.T) {
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 1}}}
	count := enrichActressesFromDB(context.Background(), movie, nil, &Config{ActressDBEnabled: true})
	assert.Equal(t, 0, count)
}

func TestLookupActress_FirstNameLastNameUncovered(t *testing.T) {
	repo := &mockActressRepoForUncovered{
		findByFirstLast: &models.Actress{ThumbURL: "thumb.jpg", JapaneseName: "テスト"},
	}
	actress := &models.Actress{FirstName: "Airi", LastName: "Suzumura"}
	found, err := lookupActress(context.Background(), repo, actress)
	require.NoError(t, err)
	assert.Equal(t, "thumb.jpg", found.ThumbURL)
}

func TestEnrichActressFields_PartialUpdateUncovered(t *testing.T) {
	actress := &models.Actress{ThumbURL: "existing.jpg"}
	dbActress := &models.Actress{ThumbURL: "new.jpg", FirstName: "NewFirst"}
	changed := enrichActressFields(actress, dbActress)
	assert.True(t, changed)
	assert.Equal(t, "existing.jpg", actress.ThumbURL, "should not overwrite existing ThumbURL")
	assert.Equal(t, "NewFirst", actress.FirstName)
}

// --- Cache (cache.go) ---

func TestTryCache_NilMovieRepoUncovered(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, &Config{}, nil, nil)
	result := s.tryCache(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil, time.Time{})
	assert.Nil(t, result)
}

func TestTryCache_FindByIDNotFoundUncovered(t *testing.T) {
	mockMovieRepo := &mockMovieRepository{findErr: database.ErrNotFound}
	s := New(nil, nil, nil, mockMovieRepo, nil, &Config{}, nil, nil)
	result := s.tryCache(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil, time.Time{})
	assert.Nil(t, result)
}

func TestTryCache_CacheHitNoTranslationUncovered(t *testing.T) {
	f := newFixture(t)
	f.withCachedMovie("TEST-001", "Cached Movie")
	s := f.build()

	result := s.tryCache(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil, time.Time{})
	require.NotNil(t, result)
	assert.Equal(t, StatusCompleted, result.Status)
	assert.Equal(t, "Cached Movie", result.Movie.Title)
	assert.False(t, result.NeedsPersistence)
	assert.True(t, result.Cached)
	require.Len(t, result.ScraperResults, 1)
	assert.Equal(t, "scraper", result.ScraperResults[0].Source)
}

// --- Apply translation (apply_translation.go) ---

func TestNewTranslationHTTPClientUncovered(t *testing.T) {
	client := newTranslationHTTPClient()
	require.NotNil(t, client)
	assert.Equal(t, 30*time.Second, client.Timeout)
}

func TestMergeOrAppendTranslation_EmptySliceUncovered(t *testing.T) {
	result := mergeOrAppendTranslation(nil, models.MovieTranslation{Language: "ja", Title: "Test"}, false)
	assert.Len(t, result, 1)
	assert.Equal(t, "ja", result[0].Language)
}

// --- Query (query.go) ---

func TestResolveScraperNames_PriorityOverrideOnlyUncovered(t *testing.T) {
	assert.Equal(t, []string{"b"}, resolveScraperNames(nil, []string{"b"}, nil))
}

func TestQuerySingle_PanicRecoveryUncovered(t *testing.T) {
	panicScraper := &panicScraperUncovered{name: "panic"}
	outcome := querySingle(context.Background(), "MOV-001", panicScraper)
	assert.NotNil(t, outcome.failure)
	assert.Equal(t, "panic", outcome.failure.Scraper)
	// safeSearch catches the panic and returns an error, which querySingle stores as Cause
	assert.NotNil(t, outcome.failure.Cause)
	assert.Contains(t, outcome.failure.Cause.Error(), "panic")
}

func TestQueryAll_EmptyScrapersUncovered(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, &Config{}, nil, nil)
	results, failures := s.queryAll(context.Background(), "MOV-001", "MOV-001", nil, time.Now())
	assert.Nil(t, results)
	assert.Nil(t, failures)
}

func TestQueryAll_SingleScraperUncovered(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&mockScraper{name: "test", enabled: true, result: &models.ScraperResult{ID: "MOV-001", Title: "Test"}, err: nil})
	scrapers := registry.GetInstancesByPriorityForInput([]string{"test"}, "")
	s := New(registry, nil, nil, nil, nil, &Config{}, nil, nil)
	results, failures := s.queryAll(context.Background(), "MOV-001", "MOV-001", scrapers, time.Now())
	require.Len(t, results, 1)
	assert.Equal(t, "MOV-001", results[0].ID)
	_ = failures
}

func TestResolveContentID_NoScraperNamesUncovered(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, &Config{}, nil, nil)
	assert.Equal(t, "MOV-001", s.resolveContentID(context.Background(), "MOV-001", nil))
}

// --- Helper types for tests ---

type mockActressRepoForUncovered struct {
	database.ActressRepositoryInterface
	findByDMMIDVal     *models.Actress
	findByDMMIDErr     error
	findByNameVal      *models.Actress
	findByNameErr      error
	findByFirstLast    *models.Actress
	findByFirstLastErr error
}

func (m *mockActressRepoForUncovered) FindByDMMID(_ context.Context, _ int) (*models.Actress, error) {
	if m.findByDMMIDErr != nil {
		return nil, m.findByDMMIDErr
	}
	return m.findByDMMIDVal, nil
}

func (m *mockActressRepoForUncovered) FindByJapaneseName(_ context.Context, _ string) (*models.Actress, error) {
	if m.findByNameErr != nil {
		return nil, m.findByNameErr
	}
	return m.findByNameVal, nil
}

func (m *mockActressRepoForUncovered) FindByFirstNameLastName(_ context.Context, _, _ string) (*models.Actress, error) {
	if m.findByFirstLastErr != nil {
		return nil, m.findByFirstLastErr
	}
	return m.findByFirstLast, nil
}

type panicScraperUncovered struct {
	name string
}

func (p *panicScraperUncovered) Name() string { return p.name }
func (p *panicScraperUncovered) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	panic("unexpected panic in scraper")
}
func (p *panicScraperUncovered) IsEnabled() bool                                    { return true }
func (p *panicScraperUncovered) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (p *panicScraperUncovered) Config() *models.ScraperSettings                    { return nil }
func (p *panicScraperUncovered) Close() error                                       { return nil }
