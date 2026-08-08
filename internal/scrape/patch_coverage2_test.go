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

// stubScrapeWithURLHandler implements both Scraper and URLHandler for QueryRaw URL tests.
type stubScrapeWithURLHandler struct {
	name      string
	enabled   bool
	result    *models.ScraperResult
	canHandle bool
}

func (s *stubScrapeWithURLHandler) Name() string { return s.name }
func (s *stubScrapeWithURLHandler) Search(_ context.Context, id string) (*models.ScraperResult, error) {
	if s.result != nil {
		r := *s.result
		r.ID = id
		return &r, nil
	}
	return nil, errors.New("not found")
}
func (s *stubScrapeWithURLHandler) GetURL(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (s *stubScrapeWithURLHandler) IsEnabled() bool { return s.enabled }
func (s *stubScrapeWithURLHandler) Close() error    { return nil }
func (s *stubScrapeWithURLHandler) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: s.enabled}
}
func (s *stubScrapeWithURLHandler) CanHandleURL(string) bool                { return s.canHandle }
func (s *stubScrapeWithURLHandler) ExtractIDFromURL(string) (string, error) { return "URL-123", nil }
func (s *stubScrapeWithURLHandler) ScrapeURL(context.Context, string) (*models.ScraperResult, error) {
	return nil, models.NewScraperNotFoundError(s.name, "not a direct page")
}

func TestQueryRaw_URLInputResolvesID(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&stubScrapeWithURLHandler{
		name:      "test",
		enabled:   true,
		canHandle: true,
		result:    &models.ScraperResult{Source: "test", Title: "Hello"},
	})
	engine := NewQueryOnly(reg)

	// A URL input should be resolved to the extracted ID before Search
	result, err := engine.QueryRaw(context.Background(), "https://example.com/video/URL-123", "test")
	require.Nil(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "URL-123", result.ID)
	assert.Equal(t, "Hello", result.Title)
}

func TestRedactErrorURL_NilError(t *testing.T) {
	result := redactErrorURL(nil, "https://example.com/page?token=secret")
	assert.Nil(t, result)
}

func TestRedactErrorURL_EmptyURL(t *testing.T) {
	err := errors.New("some error")
	result := redactErrorURL(err, "")
	assert.Equal(t, err, result)
}

func TestRedactErrorURL_URLNotInMessage(t *testing.T) {
	rawURL := "https://example.com/page?token=secret"
	err := errors.New("unrelated error")
	result := redactErrorURL(err, rawURL)
	assert.Equal(t, err, result)
}

func TestRedactErrorURL_ScraperErrorWithCause(t *testing.T) {
	rawURL := "https://example.com/page?token=secret"
	causeErr := fmt.Errorf("inner error at %s", rawURL)
	se := &models.ScraperError{
		Scraper: "test",
		Kind:    models.ScraperErrorKindUnknown,
		Message: fmt.Sprintf("outer error at %s", rawURL),
		Cause:   causeErr,
	}

	result := redactErrorURL(se, rawURL)
	se2, ok := models.AsScraperError(result)
	require.True(t, ok)
	assert.NotContains(t, se2.Message, rawURL)
	assert.NotContains(t, se2.Message, "token=secret")
	// Cause should also be redacted
	require.NotNil(t, se2.Cause)
	assert.NotContains(t, se2.Cause.Error(), "token=secret")
}

func TestRedactSourceURL_ParseError(t *testing.T) {
	// An invalid URL that fails url.Parse should be returned as-is
	result := RedactSourceURL("://invalid-url")
	assert.Equal(t, "://invalid-url", result)
}

func TestRedactErrorURL_RedactedEqualsRaw(t *testing.T) {
	// If RedactSourceURL returns the same URL (no secrets to strip),
	// redactErrorURL should return the original error unchanged.
	rawURL := "https://example.com/page"
	err := fmt.Errorf("failed at %s", rawURL)
	result := redactErrorURL(err, rawURL)
	assert.Equal(t, err, result)
}

func TestQueryRaw_CancelledContext(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&stubScrape{name: "test", enabled: true})
	engine := NewQueryOnly(reg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := engine.QueryRaw(ctx, "TEST-001", "test")
	require.Nil(t, result)
	require.NotNil(t, err)
	assert.Equal(t, models.ScraperErrorKindUnavailable, err.Kind)
}
