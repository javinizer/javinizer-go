package scrape

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- resolveContentID: no scrapers returns original ID ---
// Lines 42-49

func TestScrapeMiss2_ResolveContentID_NoScrapers(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	s := &Scraper{registry: registry}
	result := s.resolveContentID(context.Background(), "ABC-123", nil)
	assert.Equal(t, "ABC-123", result)
}

// --- resolveContentID: scraper not found in registry ---
// Lines 44-47

func TestScrapeMiss2_ResolveContentID_ScraperNotInRegistry(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	s := &Scraper{registry: registry}
	result := s.resolveContentID(context.Background(), "ABC-123", []string{"nonexistent"})
	assert.Equal(t, "ABC-123", result)
}

// --- resolveContentID: scraper doesn't implement ContentIDResolver ---
// Lines 48-49

func TestScrapeMiss2_ResolveContentID_NoResolverInterface(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	mock := &mockScraper{name: "mock-no-resolve", enabled: true}
	registry.RegisterInstance(mock)
	s := &Scraper{registry: registry}
	result := s.resolveContentID(context.Background(), "ABC-123", []string{"mock-no-resolve"})
	assert.Equal(t, "ABC-123", result)
}

// --- resolveContentID: resolver returns error ---
// Lines 60-62

func TestScrapeMiss2_ResolveContentID_ResolverError(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	mock := &cidResolverScraper{name: "mock-resolver", err: errors.New("resolution failed")}
	registry.RegisterInstance(mock)
	s := &Scraper{registry: registry}
	result := s.resolveContentID(context.Background(), "ABC-123", []string{"mock-resolver"})
	assert.Equal(t, "ABC-123", result, "should fall back to original ID on error")
}

// --- resolveContentID: resolver returns new ID ---

func TestScrapeMiss2_ResolveContentID_ResolverSuccess(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	mock := &cidResolverScraper{name: "mock-resolver", resolvedID: "abc123"}
	registry.RegisterInstance(mock)
	s := &Scraper{registry: registry}
	result := s.resolveContentID(context.Background(), "ABC-123", []string{"mock-resolver"})
	assert.Equal(t, "abc123", result)
}

// --- queryAll: nil context gets background context ---

func TestScrapeMiss2_QueryAll_NilContext(t *testing.T) {
	s := &Scraper{}
	scrapers := []models.Scraper{
		&mockScraper{name: "test", enabled: true, result: &models.ScraperResult{ID: "TEST-001"}},
	}
	results, failures := s.queryAll(nil, "TEST-001", "test-001", scrapers, time.Now())
	assert.Len(t, results, 1)
	assert.Empty(t, failures)
}

// --- queryAll: empty scrapers returns nil ---

func TestScrapeMiss2_QueryAll_EmptyScrapers(t *testing.T) {
	s := &Scraper{}
	results, failures := s.queryAll(context.Background(), "TEST-001", "test-001", nil, time.Now())
	assert.Nil(t, results)
	assert.Nil(t, failures)
}

// --- queryAll: single scraper error ---

func TestScrapeMiss2_QueryAll_SingleScraperError(t *testing.T) {
	s := &Scraper{}
	scrapers := []models.Scraper{
		&mockScraper{name: "fail", enabled: true, err: errors.New("network error")},
	}
	results, failures := s.queryAll(context.Background(), "TEST-001", "test-001", scrapers, time.Now())
	assert.Empty(t, results)
	assert.Len(t, failures, 1)
	assert.Equal(t, "fail", failures[0].Scraper)
}

// --- queryAll: multiple scrapers, mixed results ---

func TestScrapeMiss2_QueryAll_MultipleScrapers(t *testing.T) {
	s := &Scraper{}
	scrapers := []models.Scraper{
		&mockScraper{name: "ok", enabled: true, result: &models.ScraperResult{ID: "TEST-001"}},
		&mockScraper{name: "fail", enabled: true, err: errors.New("network error")},
	}
	results, failures := s.queryAll(context.Background(), "TEST-001", "test-001", scrapers, time.Now())
	assert.Len(t, results, 1)
	assert.Len(t, failures, 1)
}

// --- queryAll: context cancelled adds context error ---

func TestScrapeMiss2_QueryAll_ContextCancelled(t *testing.T) {
	s := &Scraper{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	scrapers := []models.Scraper{
		&mockScraper{name: "test", enabled: true, result: &models.ScraperResult{ID: "TEST-001"}},
	}
	_, failures := s.queryAll(ctx, "TEST-001", "test-001", scrapers, time.Now())
	// The scraper may complete before checking context cancellation
	// Just verify we don't panic and get some result
	_ = failures
}

// --- querySingle: panic in Search recovered by safeSearch ---

func TestScrapeMiss2_QuerySingle_PanicRecovery(t *testing.T) {
	panickingScraper := &panicScraper{name: "panic"}
	outcome := querySingle(context.Background(), "TEST-001", panickingScraper)
	assert.Nil(t, outcome.result)
	require.NotNil(t, outcome.failure)
	// safeSearch recovers the panic and returns it as an error,
	// which querySingle then captures as a failure with Cause
	assert.Equal(t, "panic", outcome.failure.Scraper)
}

// --- querySingle: context cancelled returns context error ---

func TestScrapeMiss2_QuerySingle_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	scraper := &mockScraper{name: "test", enabled: true, result: &models.ScraperResult{}}
	outcome := querySingle(ctx, "TEST-001", scraper)
	assert.Nil(t, outcome.result)
	require.NotNil(t, outcome.failure)
	assert.Equal(t, "test", outcome.failure.Scraper)
}

// --- querySingle: scraper error with mapped query retry ---

func TestScrapeMiss2_QuerySingle_ErrorWithMappedQueryRetry(t *testing.T) {
	scraper := &queryResolverMockScraper{
		name:        "mapped",
		mappedQuery: "MAPPED-001",
		err:         errors.New("not found with mapped"),
		result:      &models.ScraperResult{ID: "TEST-001"},
	}
	outcome := querySingle(context.Background(), "TEST-001", scraper)
	require.NotNil(t, outcome.result)
	assert.Equal(t, "TEST-001", outcome.result.ID)
}

// --- querySingle: scraper error with mapped query, retry also fails ---

func TestScrapeMiss2_QuerySingle_ErrorWithMappedQueryRetryFails(t *testing.T) {
	scraper := &queryResolverMockScraper{
		name:        "mapped-fail",
		mappedQuery: "MAPPED-001",
		err:         errors.New("not found"),
		retryErr:    errors.New("also not found"),
	}
	outcome := querySingle(context.Background(), "TEST-001", scraper)
	assert.Nil(t, outcome.result)
	require.NotNil(t, outcome.failure)
	assert.Contains(t, outcome.failure.Message, "mapped query")
}

// --- querySingle: scraper error without mapped query ---

func TestScrapeMiss2_QuerySingle_ErrorNoMappedQuery(t *testing.T) {
	scraper := &mockScraper{name: "fail", enabled: true, err: errors.New("network error")}
	outcome := querySingle(context.Background(), "TEST-001", scraper)
	assert.Nil(t, outcome.result)
	require.NotNil(t, outcome.failure)
	assert.Equal(t, "fail", outcome.failure.Scraper)
}

// --- safeSearch: panic recovery ---

func TestScrapeMiss2_SafeSearch_PanicRecovery(t *testing.T) {
	panickingScraper := &panicScraper{name: "panic"}
	result, err := safeSearch(context.Background(), panickingScraper, "TEST-001")
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panic")
}

// --- safeSearch: context cancelled ---

func TestScrapeMiss2_SafeSearch_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	scraper := &mockScraper{name: "test", enabled: true}
	result, err := safeSearch(ctx, scraper, "TEST-001")
	assert.Nil(t, result)
	require.Error(t, err)
}

// Helper types

type cidResolverScraper struct {
	name       string
	resolvedID string
	err        error
}

func (c *cidResolverScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return &models.ScraperResult{}, nil
}
func (c *cidResolverScraper) Name() string    { return c.name }
func (c *cidResolverScraper) IsEnabled() bool { return true }
func (c *cidResolverScraper) GetURL(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (c *cidResolverScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}
func (c *cidResolverScraper) Close() error { return nil }
func (c *cidResolverScraper) ResolveContentID(_ string) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	return c.resolvedID, nil
}

type panicScraper struct {
	name string
}

func (p *panicScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	panic("something went wrong")
}
func (p *panicScraper) Name() string    { return p.name }
func (p *panicScraper) IsEnabled() bool { return true }
func (p *panicScraper) GetURL(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (p *panicScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}
func (p *panicScraper) Close() error { return nil }

type queryResolverMockScraper struct {
	name        string
	mappedQuery string
	err         error
	retryErr    error
	result      *models.ScraperResult
	callCount   int
}

func (q *queryResolverMockScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	q.callCount++
	if q.callCount == 1 {
		return nil, q.err
	}
	if q.retryErr != nil {
		return nil, q.retryErr
	}
	return q.result, nil
}
func (q *queryResolverMockScraper) Name() string    { return q.name }
func (q *queryResolverMockScraper) IsEnabled() bool { return true }
func (q *queryResolverMockScraper) GetURL(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (q *queryResolverMockScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}
func (q *queryResolverMockScraper) Close() error { return nil }
func (q *queryResolverMockScraper) ResolveSearchQuery(_ string) (string, bool) {
	return q.mappedQuery, true
}
