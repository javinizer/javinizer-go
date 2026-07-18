package scrape

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.True(t, isContextError(ctx, context.Canceled))
	assert.True(t, isContextError(ctx, context.DeadlineExceeded))
	assert.True(t, isContextError(ctx, fmt.Errorf("wrapped: %w", context.DeadlineExceeded)))
	assert.False(t, isContextError(context.Background(), errors.New("regular error")))
}

func TestClassifyContextError(t *testing.T) {
	err := classifyContextError("test", context.DeadlineExceeded)
	assert.Equal(t, "test", err.Scraper)
	assert.Equal(t, models.ScraperErrorKindUnavailable, err.Kind)
	assert.True(t, err.Retryable)
	assert.True(t, err.Temporary)
	assert.Equal(t, "scrape timed out", err.Message)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	canceled := classifyContextError("test", context.Canceled)
	assert.Equal(t, models.ScraperErrorKindUnavailable, canceled.Kind)
	assert.Equal(t, "scrape canceled", canceled.Message)
	assert.ErrorIs(t, canceled, context.Canceled)

	wrapped := fmt.Errorf("wrapped: %w", context.DeadlineExceeded)
	wrappedErr := classifyContextError("test", wrapped)
	assert.Equal(t, "scrape timed out", wrappedErr.Message)
	assert.ErrorIs(t, wrappedErr, context.DeadlineExceeded)
}

func TestClassifyScraperError_WithTypedError(t *testing.T) {
	original := models.NewScraperNotFoundError("orig", "not found")
	err := classifyScraperError("test", original, "")
	assert.Equal(t, "test", err.Scraper)
	assert.Equal(t, models.ScraperErrorKindNotFound, err.Kind)
	assert.Equal(t, 0, err.StatusCode)
	assert.False(t, err.Retryable)
	assert.Equal(t, "orig", original.Scraper)
}

func TestClassifyScraperError_WithGenericError(t *testing.T) {
	err := classifyScraperError("test", errors.New("network failed"), "")
	assert.Equal(t, "test", err.Scraper)
	assert.Equal(t, models.ScraperErrorKindUnknown, err.Kind)
	assert.Equal(t, "network failed", err.Message)
}

func TestClassifyScraperError_FallbackMsg(t *testing.T) {
	err := classifyScraperError("test", errors.New("retry failed"), "custom fallback")
	assert.Equal(t, "custom fallback", err.Message)
}

type stubScrape struct {
	name    string
	enabled bool
}

func (s *stubScrape) Name() string { return s.name }
func (s *stubScrape) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *stubScrape) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (s *stubScrape) IsEnabled() bool                                    { return s.enabled }
func (s *stubScrape) Close() error                                       { return nil }
func (s *stubScrape) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: s.enabled}
}

type stubScrapeWithResult struct {
	name    string
	enabled bool
	result  *models.ScraperResult
}

func (s *stubScrapeWithResult) Name() string { return s.name }
func (s *stubScrapeWithResult) Search(_ context.Context, id string) (*models.ScraperResult, error) {
	r := *s.result
	r.ID = id
	return &r, nil
}
func (s *stubScrapeWithResult) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (s *stubScrapeWithResult) IsEnabled() bool                                    { return s.enabled }
func (s *stubScrapeWithResult) Close() error                                       { return nil }
func (s *stubScrapeWithResult) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: s.enabled}
}

func TestQueryRaw_SuccessWithResult(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&stubScrapeWithResult{name: "test", enabled: true, result: &models.ScraperResult{Source: "test", Title: "Hello"}})
	engine := NewQueryOnly(reg)
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "test")
	require.Nil(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Hello", result.Title)
	assert.Equal(t, "TEST-001", result.ID)
}

func TestQueryRaw_NilResult(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&stubScrape{name: "test", enabled: true})
	engine := NewQueryOnly(reg)
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "test")
	require.Nil(t, err)
	assert.Nil(t, result)
}

func TestIsContextError_WrappedDeadlineExceeded(t *testing.T) {
	wrapped := fmt.Errorf("request failed: %w", context.DeadlineExceeded)
	assert.True(t, isContextError(context.Background(), wrapped))
}

func TestIsContextError_WrappedCanceled(t *testing.T) {
	wrapped := fmt.Errorf("cancelled: %w", context.Canceled)
	assert.True(t, isContextError(context.Background(), wrapped))
}

func TestQueryRaw_DisabledScraper(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&stubScrape{name: "test", enabled: false})
	engine := NewQueryOnly(reg)
	result, err := engine.QueryRaw(context.Background(), "TEST-001", "test")
	assert.Nil(t, result)
	assert.NotNil(t, err)
	assert.Equal(t, models.ScraperErrorKindUnknown, err.Kind)
	assert.Contains(t, err.Message, "not enabled")
}
