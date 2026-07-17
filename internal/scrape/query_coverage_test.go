package scrape

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
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
	assert.Contains(t, err.Message, "deadline")
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
