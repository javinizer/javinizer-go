package scrape

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestRedactErrorURL_PreservesContextErrors(t *testing.T) {
	rawURL := "https://example.com/page?token=secret"

	// A wrapped context error whose message contains the raw URL.
	ctxErr := fmt.Errorf("fetch failed for %s: %w", rawURL, context.DeadlineExceeded)

	redacted := redactErrorURL(ctxErr, rawURL)

	// The URL must be scrubbed from the message.
	assert.NotContains(t, redacted.Error(), rawURL)
	assert.Contains(t, redacted.Error(), "example.com/page") // host/path preserved, secret stripped

	// The unwrap chain must be preserved so errors.Is still detects the context error.
	assert.True(t, errors.Is(redacted, context.DeadlineExceeded),
		"errors.Is must still detect context.DeadlineExceeded after URL redaction")

	// A Canceled error should also be preserved.
	cancelErr := fmt.Errorf("fetch failed for %s: %w", rawURL, context.Canceled)
	redactedCancel := redactErrorURL(cancelErr, rawURL)
	assert.True(t, errors.Is(redactedCancel, context.Canceled),
		"errors.Is must still detect context.Canceled after URL redaction")
}

func TestRedactErrorURL_PreservesScraperError(t *testing.T) {
	rawURL := "https://example.com/page?token=secret"
	se := &models.ScraperError{
		Scraper: "TestScraper",
		Kind:    models.ScraperErrorKindUnknown,
		Message: fmt.Sprintf("failed to fetch %s", rawURL),
	}

	redacted := redactErrorURL(se, rawURL)

	// ScraperError typing must be preserved.
	se2, ok := models.AsScraperError(redacted)
	assert.True(t, ok, "ScraperError typing must be preserved")
	assert.NotContains(t, se2.Message, rawURL)
	assert.Equal(t, "TestScraper", se2.Scraper)
}

func TestRedactErrorURL_NoChangeWhenURLAbsent(t *testing.T) {
	rawURL := "https://example.com/page?token=secret"
	err := errors.New("some unrelated error")

	redacted := redactErrorURL(err, rawURL)
	assert.Equal(t, err, redacted, "error should be unchanged when URL is not in message")
}

func TestRedactErrorURL_PanicURLScrubbed(t *testing.T) {
	rawURL := "https://example.com/page?token=secret"
	// Simulate a panic value that contains the raw URL.
	panicErr := fmt.Errorf("boom at %s", rawURL)
	redacted := redactErrorURL(panicErr, rawURL)
	assert.NotContains(t, redacted.Error(), rawURL, "raw URL must be scrubbed from panic-recovered errors")
	assert.NotContains(t, redacted.Error(), "token=secret", "secret query must be scrubbed")
}
