package scrape

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScraperCircuitBreaker_TripsAfterThresholdConsecutiveUnavailable(t *testing.T) {
	b := newScraperCircuitBreaker(2)
	name := "libredmm"

	assert.Nil(t, b.skipFailure(name), "breaker must not trip before any failure")

	b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: name})
	assert.Nil(t, b.skipFailure(name), "breaker must not trip after 1 failure (threshold=2)")

	b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: name})
	skip := b.skipFailure(name)
	require.NotNil(t, skip)
	assert.Equal(t, models.ScraperErrorKindUnavailable, skip.Kind)
	assert.Contains(t, skip.Message, "circuit breaker tripped")
	assert.Equal(t, name, skip.Scraper)
}

func TestScraperCircuitBreaker_SuccessResetsCounter(t *testing.T) {
	b := newScraperCircuitBreaker(2)
	name := "dmm"

	b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: name})
	b.recordOutcome(name, nil)
	b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: name})

	assert.Nil(t, b.skipFailure(name), "success must reset the counter so 1 post-reset failure does not trip")
}

func TestScraperCircuitBreaker_NotFoundDoesNotTrip(t *testing.T) {
	b := newScraperCircuitBreaker(2)
	name := "r18dev"

	for i := 0; i < 5; i++ {
		b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindNotFound, Scraper: name})
	}
	assert.Nil(t, b.skipFailure(name), "NotFound is per-movie and must never trip the breaker")
}

func TestScraperCircuitBreaker_BlockedAndRateLimitedDoNotTrip(t *testing.T) {
	for _, kind := range []models.ScraperErrorKind{models.ScraperErrorKindBlocked, models.ScraperErrorKindRateLimited} {
		b := newScraperCircuitBreaker(2)
		name := "javdb"
		for i := 0; i < 5; i++ {
			b.recordOutcome(name, &models.ScraperError{Kind: kind, Scraper: name})
		}
		assert.Nil(t, b.skipFailure(name), "kind %s must not trip the breaker", kind)
	}
}

func TestScraperCircuitBreaker_PerScraperIsolation(t *testing.T) {
	b := newScraperCircuitBreaker(1)
	b.recordOutcome("libredmm", &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: "libredmm"})

	assert.NotNil(t, b.skipFailure("libredmm"), "libredmm should be tripped")
	assert.Nil(t, b.skipFailure("dmm"), "dmm must be unaffected by libredmm's failures")
}

func TestScraperCircuitBreaker_ConcurrentAccess(t *testing.T) {
	b := newScraperCircuitBreaker(3)
	name := "libredmm"
	done := make(chan struct{})
	for g := 0; g < 10; g++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 100; i++ {
				b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: name})
				b.recordOutcome(name, nil)
				_ = b.skipFailure(name)
			}
		}()
	}
	for g := 0; g < 10; g++ {
		<-done
	}
}

func TestScraperCircuitBreaker_HalfOpenRecoveryAfterCooldown(t *testing.T) {
	b := newScraperCircuitBreaker(1)
	b.cooldown = 10 * time.Millisecond
	name := "libredmm"

	// Trip the breaker.
	b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: name})
	require.NotNil(t, b.skipFailure(name), "breaker must be tripped")

	// Wait for cooldown to elapse.
	time.Sleep(20 * time.Millisecond)

	// Cooldown elapsed: skipFailure allows a half-open probe through (returns nil).
	assert.Nil(t, b.skipFailure(name), "half-open probe must be allowed after cooldown")

	// A successful probe clears the trip entirely.
	b.recordOutcome(name, nil)
	assert.Nil(t, b.skipFailure(name), "success must clear the breaker")
}

func TestScraperCircuitBreaker_HalfOpenReTripsOnFailure(t *testing.T) {
	b := newScraperCircuitBreaker(1)
	b.cooldown = 10 * time.Millisecond
	name := "libredmm"

	b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: name})
	require.NotNil(t, b.skipFailure(name))

	time.Sleep(20 * time.Millisecond)
	assert.Nil(t, b.skipFailure(name), "half-open probe allowed")

	// Probe fails: the breaker re-trips immediately (failures was already >= threshold).
	b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: name})
	require.NotNil(t, b.skipFailure(name), "failed probe must re-trip the breaker")
}

func TestClassifyScraperError_TransportErrorClassifiedAsUnavailable(t *testing.T) {
	// A *url.Error wrapping a connection-refused OpError, mirroring what Resty
	// returns when a scraper host is unreachable.
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	urlErr := &url.Error{Op: "Get", URL: "https://libredmm.com/search", Err: opErr}

	se := classifyScraperError("libredmm", urlErr, "")
	require.NotNil(t, se)
	assert.Equal(t, models.ScraperErrorKindUnavailable, se.Kind, "transport errors must classify as Unavailable so the breaker trips")
	assert.True(t, se.Retryable)
	assert.True(t, se.Temporary)
	assert.Equal(t, "libredmm", se.Scraper)

	// A plain net.Error (e.g. a timeout) also classifies as Unavailable.
	var netErr net.Error = &netTimeoutError{}
	se2 := classifyScraperError("dmm", netErr, "")
	require.NotNil(t, se2)
	assert.Equal(t, models.ScraperErrorKindUnavailable, se2.Kind)
}

func TestClassifyScraperError_NonTransportErrorStaysUnknown(t *testing.T) {
	se := classifyScraperError("r18dev", errors.New("some scraper-internal logic error"), "")
	require.NotNil(t, se)
	assert.Equal(t, models.ScraperErrorKindUnknown, se.Kind, "non-transport errors must not trip the breaker")
}

func TestScraperCircuitBreaker_HalfOpenNotFoundProbeClearsTrip(t *testing.T) {
	b := newScraperCircuitBreaker(1)
	b.cooldown = 10 * time.Millisecond
	name := "libredmm"

	// Trip the breaker with an Unavailable failure.
	b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: name})
	require.NotNil(t, b.skipFailure(name), "breaker must be tripped")

	// Cooldown elapses -> half-open probe allowed.
	time.Sleep(20 * time.Millisecond)
	assert.Nil(t, b.skipFailure(name), "half-open probe must be allowed")

	// Probe returns NotFound (host is reachable, just no match). This must
	// clear the trip so the recovered host isn't skipped for another 60s.
	b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindNotFound, Scraper: name})
	assert.Nil(t, b.skipFailure(name), "non-Unavailable probe outcome must clear the trip")
}

func TestClassifyScraperError_WrappedBareNetErrorClassifiedAsUnavailable(t *testing.T) {
	// A bare net.OpError wrapped with fmt.Errorf("%%w") (no *url.Error in the
	// chain) must still classify as Unavailable via errors.As(err, &netErr).
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	wrapped := fmt.Errorf("dial failed: %w", opErr)

	se := classifyScraperError("libredmm", wrapped, "")
	require.NotNil(t, se)
	assert.Equal(t, models.ScraperErrorKindUnavailable, se.Kind, "wrapped bare net.Error must classify as Unavailable")
}

func TestScraperCircuitBreaker_ThresholdBelowOneUsesDefault(t *testing.T) {
	b := newScraperCircuitBreaker(0)
	assert.Equal(t, circuitBreakerThreshold, b.threshold, "threshold<1 must fall back to the default")

	b0 := newScraperCircuitBreaker(-1)
	assert.Equal(t, circuitBreakerThreshold, b0.threshold)
}

func TestIsTransportError_NilAndNonTransport(t *testing.T) {
	assert.False(t, isTransportError(nil), "nil must not be a transport error")
	assert.False(t, isTransportError(errors.New("logic error")), "plain errors must not be transport errors")
}

func TestQueryWithBreaker_SkipsTrippedScraper(t *testing.T) {
	s := &Scraper{breaker: newScraperCircuitBreaker(1)}
	name := "deadscraper"
	// Trip the breaker.
	s.breaker.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: name})

	call := &countingScraper{name: name}
	outcome := s.queryWithBreaker(context.Background(), "MOVIE-001", "", call)
	require.NotNil(t, outcome.failure, "tripped breaker must skip the scraper and return a failure")
	assert.Equal(t, models.ScraperErrorKindUnavailable, outcome.failure.Kind)
	assert.Equal(t, 0, call.searchCalls, "tripped scraper must not be invoked")
}

func TestQueryWithBreaker_RecordsSuccessAndResets(t *testing.T) {
	s := &Scraper{breaker: newScraperCircuitBreaker(2)}
	name := "flaky"

	call := &countingScraper{name: name, result: &models.ScraperResult{Source: name, ID: "MOVIE-001"}}
	outcome := s.queryWithBreaker(context.Background(), "MOVIE-001", "", call)
	require.NotNil(t, outcome.result, "successful scrape must return the result")
	assert.Nil(t, s.breaker.skipFailure(name), "success must leave the breaker untripped")
}

// countingScraper is a minimal models.Scraper stub for breaker integration tests.
type countingScraper struct {
	name        string
	result      *models.ScraperResult
	err         error
	searchCalls int
}

func (c *countingScraper) Name() string    { return c.name }
func (c *countingScraper) IsEnabled() bool { return true }
func (c *countingScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	c.searchCalls++
	if c.err != nil {
		return nil, c.err
	}
	return c.result, nil
}
func (c *countingScraper) GetURL(_ context.Context, _ string) (string, error) { return "", nil }
func (c *countingScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}
func (c *countingScraper) Close() error { return nil }

// netTimeoutError is a minimal net.Error for testing.
type netTimeoutError struct{}

func (netTimeoutError) Error() string   { return "i/o timeout" }
func (netTimeoutError) Timeout() bool   { return true }
func (netTimeoutError) Temporary() bool { return true }
