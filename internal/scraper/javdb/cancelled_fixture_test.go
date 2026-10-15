package javdb

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type offlineFixtureTransport func(*http.Request) (*http.Response, error)

func (f offlineFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Cancellation is observed at the transport seam without consulting a resolver
// or socket. Keep the production URL and verify the caller forwarded its context.
func newCancelledFixtureClient(t *testing.T, expected map[string]int) *resty.Client {
	t.Helper()
	requests := make(map[string]int)
	t.Cleanup(func() { assert.Equal(t, expected, requests) })
	return resty.New().SetTransport(offlineFixtureTransport(func(req *http.Request) (*http.Response, error) {
		requests[req.Method+" "+req.URL.String()]++
		assert.ErrorIs(t, req.Context().Err(), context.Canceled)
		return nil, context.Canceled
	}))
}

func requireCancelledScraperError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	classified, ok := models.AsScraperError(err)
	require.True(t, ok, "cancelled transport error must retain scraper classification")
	assert.Equal(t, "JavDB", classified.Scraper)
	assert.Equal(t, models.ScraperErrorKindUnknown, classified.Kind)
	assert.Zero(t, classified.StatusCode)
	assert.Contains(t, classified.Message, "request failed")
	assert.Contains(t, classified.Message, context.Canceled.Error())
}
