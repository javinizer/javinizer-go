package r18dev

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNumericPresenceDisablesBothRetryLayers(t *testing.T) {
	var settings models.ScraperSettings
	require.NoError(t, json.Unmarshal([]byte(`{"rate_limit":0,"retry_count":0}`), &settings))
	settings.MergeDefaultsFrom(models.ScraperSettings{RateLimit: 500, RetryCount: 3})
	s := newScraper(&settings, nil, models.FlareSolverrConfig{}, nil)
	assert.Zero(t, s.maxRetries)
	assert.Zero(t, s.client.RetryCount)
	assert.Zero(t, s.settings.RateLimit)
	// A disabled limiter never waits, even if its context is already canceled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, s.rateLimiter.Wait(ctx))
	literal := models.ScraperSettings{}
	defaulted := newScraper(&literal, nil, models.FlareSolverrConfig{}, nil)
	assert.Equal(t, 3, defaulted.maxRetries)
	assert.Equal(t, 3, defaulted.client.RetryCount)
}
