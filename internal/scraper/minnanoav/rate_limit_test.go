package minnanoav

import (
	"context"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// An omitted rate_limit keeps the conservative 1s crawl default; an explicit
// rate_limit: 0 in config must mean no delay (the documented contract).
func TestRateLimitPresenceRespected(t *testing.T) {
	ctx := context.Background()

	explicitOff := &models.ScraperSettings{Enabled: true, RateLimit: 0}
	explicitOff.SetRateLimitPresence(true)
	s := newScraperWithClient(explicitOff, resty.New())
	require.NoError(t, s.rateLimiter.Wait(ctx))
	require.NoError(t, s.rateLimiter.Wait(ctx), "explicit rate_limit: 0 must not throttle")

	omitted := &models.ScraperSettings{Enabled: true}
	sDef := newScraperWithClient(omitted, resty.New())
	require.NoError(t, sDef.rateLimiter.Wait(ctx))
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	require.Error(t, sDef.rateLimiter.Wait(short), "omitted rate_limit keeps the 1s default")
}
