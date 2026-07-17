package scrape

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mappedRetryScraper struct {
	name string
}

func (m *mappedRetryScraper) Name() string    { return m.name }
func (m *mappedRetryScraper) IsEnabled() bool { return true }
func (m *mappedRetryScraper) Close() error    { return nil }
func (m *mappedRetryScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}
func (m *mappedRetryScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (m *mappedRetryScraper) ResolveSearchQuery(input string) (string, bool) {
	return "MAPPED-" + input, true
}
func (m *mappedRetryScraper) Search(_ context.Context, query string) (*models.ScraperResult, error) {
	if strings.HasPrefix(query, "MAPPED-") {
		return nil, errors.New("mapped query failed")
	}
	return nil, context.DeadlineExceeded
}

func TestQuerySingle_MappedQueryRetryContextError(t *testing.T) {
	scraper := &mappedRetryScraper{name: "mapped"}
	outcome := querySingle(context.Background(), "TEST-001", scraper)
	require.NotNil(t, outcome.failure)
	assert.Equal(t, "mapped", outcome.failure.Scraper)
	assert.Equal(t, models.ScraperErrorKindUnavailable, outcome.failure.Kind)
	assert.True(t, outcome.failure.Retryable)
	assert.True(t, outcome.failure.Temporary)
}

func TestClassifyScraperError_EmptyMessageFallback(t *testing.T) {
	origErr := &models.ScraperError{Scraper: "orig", Cause: errors.New("root cause")}
	result := classifyScraperError("test", origErr, "")
	assert.Equal(t, "test", result.Scraper)
	assert.Equal(t, origErr.Kind, result.Kind)
	assert.NotEmpty(t, result.Message)
	assert.Equal(t, "orig scraper error", result.Message)
}

func TestQueryRaw_NilContext(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&stubScrapeWithResult{name: "test", enabled: true, result: &models.ScraperResult{Source: "test", Title: "Hello"}})
	engine := NewQueryOnly(reg)
	result, err := engine.QueryRaw(nil, "TEST-001", "test")
	require.Nil(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Hello", result.Title)
}

func TestQueryRaw_UnknownScraperName(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&stubScrapeWithResult{name: "test", enabled: true, result: &models.ScraperResult{Source: "test"}})
	engine := NewQueryOnly(reg)
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "nonexistent")
	assert.Nil(t, result)
	require.NotNil(t, err)
	assert.Equal(t, models.ScraperErrorKindUnknown, err.Kind)
	assert.Contains(t, err.Message, "not registered")
}

func TestQueryRaw_ScraperReturnsError(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&mockScraper{name: "errscraper", enabled: true, err: errors.New("boom")})
	engine := NewQueryOnly(reg)
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "errscraper")
	assert.Nil(t, result)
	require.NotNil(t, err)
	assert.Equal(t, models.ScraperErrorKindUnknown, err.Kind)
	assert.Equal(t, "boom", err.Message)
}
