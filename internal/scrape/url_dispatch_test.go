package scrape

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// urlHandlerScraper is a test scraper that implements models.URLHandler.
// It returns a configurable result/error from ScrapeURL.
type urlHandlerScraper struct {
	name      string
	enabled   bool
	result    *models.ScraperResult
	err       error
	panicVal  interface{}
	canHandle bool
}

func (s *urlHandlerScraper) Name() string { return s.name }
func (s *urlHandlerScraper) Search(context.Context, string) (*models.ScraperResult, error) {
	return nil, errors.New("search not implemented")
}
func (s *urlHandlerScraper) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *urlHandlerScraper) IsEnabled() bool                                { return s.enabled }
func (s *urlHandlerScraper) Config() *models.ScraperSettings                { return nil }
func (s *urlHandlerScraper) Close() error                                   { return nil }
func (s *urlHandlerScraper) CanHandleURL(string) bool                       { return s.canHandle }
func (s *urlHandlerScraper) ExtractIDFromURL(string) (string, error)        { return "extracted", nil }
func (s *urlHandlerScraper) ScrapeURL(_ context.Context, _ string) (*models.ScraperResult, error) {
	if s.panicVal != nil {
		panic(s.panicVal)
	}
	if s.result != nil {
		return s.result, nil
	}
	return nil, s.err
}

func TestQuerySingle_URLDirectScrapeSuccess(t *testing.T) {
	s := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		result: &models.ScraperResult{
			ID:        "ONED-120",
			Title:     "Test Movie",
			Source:    "test-scraper",
			SourceURL: "https://example.com/page",
		},
	}

	outcome := querySingle(context.Background(), "movie-id", "https://example.com/page", s)
	require.NotNil(t, outcome.result)
	assert.Equal(t, "ONED-120", outcome.result.ID)
	// SourceURL should be redacted (no query params to strip here, so unchanged)
	assert.Equal(t, "https://example.com/page", outcome.result.SourceURL)
}

func TestQuerySingle_URLDirectScrapeNilResult(t *testing.T) {
	s := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		result:    nil,
		err:       nil,
	}

	outcome := querySingle(context.Background(), "movie-id", "https://example.com/page", s)
	// nil result with nil error → NotFound → falls through to Search → Search fails with unknown
	require.NotNil(t, outcome.failure)
	assert.Equal(t, models.ScraperErrorKindUnknown, outcome.failure.Kind)
}

func TestQuerySingle_URLDirectScrapeSparseResult(t *testing.T) {
	s := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		result: &models.ScraperResult{
			ID:    "",
			Title: "Some Title",
		},
	}

	outcome := querySingle(context.Background(), "movie-id", "https://example.com/page", s)
	// sparse result (no ID) → NotFound → falls through to Search → Search fails with unknown
	require.NotNil(t, outcome.failure)
	assert.Equal(t, models.ScraperErrorKindUnknown, outcome.failure.Kind)
}

func TestQuerySingle_URLDirectScrapeNotFoundFallsThroughToSearch(t *testing.T) {
	s := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		result:    nil,
		err:       models.NewScraperNotFoundError("test-scraper", "not a direct page"),
	}

	outcome := querySingle(context.Background(), "movie-id", "https://example.com/page", s)
	// NotFound from ScrapeURL → falls through to Search → Search fails with unknown
	require.NotNil(t, outcome.failure)
	assert.Equal(t, models.ScraperErrorKindUnknown, outcome.failure.Kind)
}

func TestQuerySingle_URLDirectScrapeStatusError(t *testing.T) {
	s := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		result:    nil,
		err:       models.NewScraperStatusError("test-scraper", 403, "access blocked"),
	}

	outcome := querySingle(context.Background(), "movie-id", "https://example.com/page", s)
	// 403 is not NotFound → terminal failure, scraper name normalized
	require.NotNil(t, outcome.failure)
	assert.Equal(t, models.ScraperErrorKindBlocked, outcome.failure.Kind)
	assert.Equal(t, "test-scraper", outcome.failure.Scraper)
}

func TestQuerySingle_URLDirectScrapeContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		result:    nil,
		err:       context.Canceled,
	}

	outcome := querySingle(ctx, "movie-id", "https://example.com/page", s)
	require.NotNil(t, outcome.failure)
	assert.Contains(t, outcome.failure.Message, "cancel")
}

func TestQuerySingle_URLDirectScrapeGenericError(t *testing.T) {
	s := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		result:    nil,
		err:       errors.New("network failure"),
	}

	outcome := querySingle(context.Background(), "movie-id", "https://example.com/page", s)
	require.NotNil(t, outcome.failure)
	// Generic error (not ScraperError, not context) → classifyScraperError
	assert.Equal(t, "test-scraper", outcome.failure.Scraper)
}

func TestQuerySingle_URLDirectScrapePanic(t *testing.T) {
	s := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		panicVal:  "boom at https://example.com/page?token=secret",
	}

	outcome := querySingle(context.Background(), "movie-id", "https://example.com/page?token=secret", s)
	// Panic in ScrapeURL → safeScrapeURL recovers, then querySingle's own
	// defer also recovers → the querySingle defer wins (it wraps the outer call)
	require.NotNil(t, outcome.failure)
	assert.Equal(t, "test-scraper", outcome.failure.Scraper)
	// The panic message should be scrubbed by safeScrapeURL before reaching
	// querySingle's recover
	assert.NotContains(t, outcome.failure.Message, "token=secret")
}

func TestSafeScrapeURL_Success(t *testing.T) {
	handler := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		result: &models.ScraperResult{
			ID:        "ONED-120",
			SourceURL: "https://example.com/page?token=secret",
		},
	}

	result, err := safeScrapeURL(context.Background(), handler, "https://example.com/page?token=secret")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ONED-120", result.ID)
	// SourceURL should be redacted (token stripped)
	assert.Equal(t, "https://example.com/page", result.SourceURL)
}

func TestSafeScrapeURL_ErrorRedacted(t *testing.T) {
	rawURL := "https://example.com/page?token=secret"
	handler := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		result:    nil,
		err:       errors.New("failed to fetch " + rawURL),
	}

	result, err := safeScrapeURL(context.Background(), handler, rawURL)
	require.Error(t, err)
	assert.Nil(t, result)
	// The raw URL with token should be scrubbed from the error message
	assert.NotContains(t, err.Error(), "token=secret")
	assert.Contains(t, err.Error(), "example.com/page")
}

func TestSafeScrapeURL_PanicRedacted(t *testing.T) {
	rawURL := "https://example.com/page?token=secret"
	handler := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		panicVal:  "boom at " + rawURL,
	}

	result, err := safeScrapeURL(context.Background(), handler, rawURL)
	require.Error(t, err)
	assert.Nil(t, result)
	// The panic message should have the raw URL scrubbed
	assert.NotContains(t, err.Error(), "token=secret")
}

func TestSafeScrapeURL_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handler := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		result:    &models.ScraperResult{ID: "OK"},
	}

	result, err := safeScrapeURL(ctx, handler, "https://example.com/page")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestSafeScrapeURL_NilResultNilError(t *testing.T) {
	handler := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		result:    nil,
		err:       nil,
	}

	result, err := safeScrapeURL(context.Background(), handler, "https://example.com/page")
	assert.Nil(t, result)
	assert.NoError(t, err)
}

func TestQuerySingle_NoRawInputFallsToSearch(t *testing.T) {
	s := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		result:    nil,
		err:       errors.New("search not implemented"),
	}

	// Empty rawInput → should go straight to Search, not ScrapeURL
	outcome := querySingle(context.Background(), "movie-id", "", s)
	require.NotNil(t, outcome.failure)
	assert.Equal(t, "test-scraper", outcome.failure.Scraper)
}

func TestQuerySingle_URLNotHandledFallsToSearch(t *testing.T) {
	s := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: false, // CanHandleURL returns false
		result:    nil,
		err:       errors.New("search not implemented"),
	}

	outcome := querySingle(context.Background(), "movie-id", "https://example.com/page", s)
	require.NotNil(t, outcome.failure)
	assert.Equal(t, "test-scraper", outcome.failure.Scraper)
}

func TestQuerySingle_URLDirectScrapeScraperErrorNormalized(t *testing.T) {
	s := &urlHandlerScraper{
		name:      "test-scraper",
		enabled:   true,
		canHandle: true,
		result:    nil,
		err: &models.ScraperError{
			Scraper: "WrongName",
			Kind:    models.ScraperErrorKindRateLimited,
			Message: "rate limited",
		},
	}

	outcome := querySingle(context.Background(), "movie-id", "https://example.com/page", s)
	require.NotNil(t, outcome.failure)
	assert.Equal(t, models.ScraperErrorKindRateLimited, outcome.failure.Kind)
	// Scraper name should be normalized to the registry name
	assert.Equal(t, "test-scraper", outcome.failure.Scraper)
}
