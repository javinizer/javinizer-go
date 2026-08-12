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

func TestScraperCircuitBreaker_NonUnavailableResetsFailureCount(t *testing.T) {
	b := newScraperCircuitBreaker(3)
	name := "libredmm"

	// One Unavailable failure (below threshold, not tripped).
	b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: name})
	assert.Nil(t, b.skipFailure(name), "must not be tripped after 1 failure")

	// A NotFound outcome proves the host is reachable: reset the count.
	b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindNotFound, Scraper: name})

	// A later isolated Unavailable must not trip (counter was reset, so this is count=1, not 2).
	b.recordOutcome(name, &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: name})
	assert.Nil(t, b.skipFailure(name), "non-Unavailable must reset the count so a later isolated failure doesn't trip")
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

func TestScraper_ResolveContentID_ProbeClearsBreakerOnSuccess(t *testing.T) {
	reg := &stubRegistry{instances: map[string]models.Scraper{
		"dmm": &successContentIDResolver{name: "dmm"},
	}}
	s := &Scraper{
		registry: reg,
		breaker:  newScraperCircuitBreaker(1),
	}
	s.breaker.cooldown = 10 * time.Millisecond
	// Trip the breaker.
	s.breaker.recordOutcome("dmm", &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: "dmm"})
	require.NotNil(t, s.breaker.skipFailure("dmm"))

	// Let cooldown elapse so the half-open probe is allowed.
	time.Sleep(20 * time.Millisecond)

	// Resolver succeeds (consumes the probe via skipFailure, records success → clears).
	got := s.resolveContentID(context.Background(), "MOVIE-001", []string{"dmm"})
	assert.Equal(t, "RESOLVED-001", got)
	assert.Nil(t, s.breaker.skipFailure("dmm"), "successful resolver probe must clear the breaker so the query can run")
}

func TestScraper_ResolveContentID_SkipsTrippedResolver(t *testing.T) {
	// Build a Scraper with a tripped breaker for the resolver scraper and a
	// registry that returns a ContentIDResolverCtx that would block if called.
	reg := &stubRegistry{instances: map[string]models.Scraper{
		"dmm": &blockingContentIDResolver{name: "dmm"},
	}}
	s := &Scraper{
		registry: reg,
		breaker:  newScraperCircuitBreaker(1),
	}
	s.breaker.recordOutcome("dmm", &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: "dmm"})

	got := s.resolveContentID(context.Background(), "MOVIE-001", []string{"dmm"})
	assert.Equal(t, "MOVIE-001", got, "tripped resolver must be skipped, falling back to the original movieID")
}

// stubRegistry is a minimal ScraperInstanceResolver for breaker integration tests.
type stubRegistry struct {
	instances map[string]models.Scraper
}

func (r *stubRegistry) GetInstance(name string) (models.Scraper, bool) {
	s, ok := r.instances[name]
	return s, ok
}

func (r *stubRegistry) GetInstancesByPriorityForInput(names []string, _ string) []models.Scraper {
	var out []models.Scraper
	for _, n := range names {
		if s, ok := r.instances[n]; ok {
			out = append(out, s)
		}
	}
	return out
}

func (r *stubRegistry) GetAllInstances() []models.Scraper {
	var out []models.Scraper
	for _, s := range r.instances {
		out = append(out, s)
	}
	return out
}

func (r *stubRegistry) Names() []string {
	var out []string
	for n := range r.instances {
		out = append(out, n)
	}
	return out
}

// blockingContentIDResolver is a ContentIDResolverCtx whose ResolveContentIDCtx
// must never be called (it fails the test if invoked).
type blockingContentIDResolver struct {
	name string
}

func (b *blockingContentIDResolver) Name() string    { return b.name }
func (b *blockingContentIDResolver) IsEnabled() bool { return true }
func (b *blockingContentIDResolver) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, errors.New("must not be called")
}
func (b *blockingContentIDResolver) GetURL(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (b *blockingContentIDResolver) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}
func (b *blockingContentIDResolver) Close() error { return nil }
func (b *blockingContentIDResolver) ResolveContentIDCtx(_ context.Context, _ string) (string, error) {
	panic("ResolveContentIDCtx must not be called when the breaker is tripped")
}

func TestScraper_ResolveContentID_FailureReTripsBreaker(t *testing.T) {
	reg := &stubRegistry{instances: map[string]models.Scraper{
		"dmm": &failingContentIDResolver{name: "dmm"},
	}}
	s := &Scraper{
		registry: reg,
		breaker:  newScraperCircuitBreaker(1),
	}
	s.breaker.cooldown = 10 * time.Millisecond
	// Trip the breaker.
	s.breaker.recordOutcome("dmm", &models.ScraperError{Kind: models.ScraperErrorKindUnavailable, Scraper: "dmm"})
	require.NotNil(t, s.breaker.skipFailure("dmm"))

	// Let cooldown elapse so the half-open probe is allowed.
	time.Sleep(20 * time.Millisecond)

	// Resolver fails (transport error), consumes the probe, records failure → re-trips.
	got := s.resolveContentID(context.Background(), "MOVIE-001", []string{"dmm"})
	assert.Equal(t, "MOVIE-001", got, "failed resolver must fall back to original movieID")
	require.NotNil(t, s.breaker.skipFailure("dmm"), "failed resolver probe must re-trip the breaker")
}

// failingContentIDResolver is a ContentIDResolverCtx that always returns a transport error.
type failingContentIDResolver struct {
	name string
}

func (f *failingContentIDResolver) Name() string    { return f.name }
func (f *failingContentIDResolver) IsEnabled() bool { return true }
func (f *failingContentIDResolver) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, errors.New("must not be called")
}
func (f *failingContentIDResolver) GetURL(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (f *failingContentIDResolver) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}
func (f *failingContentIDResolver) Close() error { return nil }
func (f *failingContentIDResolver) ResolveContentIDCtx(_ context.Context, _ string) (string, error) {
	return "", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
}

func TestClassifyScraperError_TypedUnknownTransportReclassified(t *testing.T) {
	// A scraper wraps a transport error in a ScraperError with Kind=Unknown,
	// StatusCode=0, and Cause=transport error (e.g. JavDB's pattern).
	// classifyScraperError must reclassify it as Unavailable so the breaker trips.
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	typedWithCause := &models.ScraperError{
		Scraper:    "javdb",
		Kind:       models.ScraperErrorKindUnknown,
		StatusCode: 0,
		Message:    opErr.Error(),
		Cause:      opErr,
	}
	se := classifyScraperError("javdb", typedWithCause, "")
	require.NotNil(t, se)
	assert.Equal(t, models.ScraperErrorKindUnavailable, se.Kind, "typed Unknown status-0 with transport Cause must be reclassified as Unavailable")
	assert.True(t, se.Retryable)
	assert.True(t, se.Temporary)

	// A raw (unwrapped) transport error also classifies as Unavailable.
	se2 := classifyScraperError("javdb", opErr, "")
	require.NotNil(t, se2)
	assert.Equal(t, models.ScraperErrorKindUnavailable, se2.Kind, "raw transport error must be Unavailable")
}

// successContentIDResolver is a ContentIDResolverCtx that always succeeds.
type successContentIDResolver struct {
	name string
}

func (s *successContentIDResolver) Name() string    { return s.name }
func (s *successContentIDResolver) IsEnabled() bool { return true }
func (s *successContentIDResolver) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, errors.New("must not be called")
}
func (s *successContentIDResolver) GetURL(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (s *successContentIDResolver) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}
func (s *successContentIDResolver) Close() error { return nil }
func (s *successContentIDResolver) ResolveContentIDCtx(_ context.Context, _ string) (string, error) {
	return "RESOLVED-001", nil
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
