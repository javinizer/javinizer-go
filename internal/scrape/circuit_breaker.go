package scrape

import (
	"fmt"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// circuitBreakerThreshold is the number of consecutive circuit-breakable
// failures from a single scraper that trips its circuit breaker. Once
// tripped, the scraper is skipped for the remainder of the batch instead of
// every file re-attempting a dead host and blocking on its per-request
// timeout. A successful result resets the counter, so a single transient
// failure does not disable a scraper.
const circuitBreakerThreshold = 2

// circuitBreakerCooldown is how long a tripped breaker stays fully closed
// before allowing a single half-open probe. This bounds the blast radius of a
// transient outage: even though the Scraper engine is cached across batches
// in API/TUI mode (so the breaker outlives a single batch), a tripped scraper
// recovers automatically once the host comes back. A probe that succeeds
// clears the breaker; one that fails re-arms the cooldown.
const circuitBreakerCooldown = 60 * time.Second

// scraperCircuitBreaker tracks consecutive circuit-breakable failures per
// scraper within a single Scraper engine. Only infrastructure-level failures
// (Kind == Unavailable: timeouts, 5xx, connection refused, DNS failures) trip
// the breaker — per-movie NotFound and per-request Blocked/RateLimited do not.
//
// The breaker is half-open: after circuitBreakerCooldown, one probe is
// allowed through; success clears the trip, failure re-arms it. This makes
// the breaker safe regardless of whether the Scraper is per-batch (CLI) or
// process-global (API/TUI): a transient outage never permanently disables a
// scraper.
type scraperCircuitBreaker struct {
	mu        sync.Mutex
	failures  map[string]int
	tripped   map[string]bool
	trippedAt map[string]time.Time
	threshold int
	cooldown  time.Duration
}

func newScraperCircuitBreaker(threshold int) *scraperCircuitBreaker {
	if threshold < 1 {
		threshold = circuitBreakerThreshold
	}
	return &scraperCircuitBreaker{
		failures:  make(map[string]int),
		tripped:   make(map[string]bool),
		trippedAt: make(map[string]time.Time),
		threshold: threshold,
		cooldown:  circuitBreakerCooldown,
	}
}

// skipFailure returns a ScraperError if the breaker for name is tripped (the
// caller should skip the scraper), or nil if the scraper should be invoked.
// When the cooldown has elapsed, it allows a single half-open probe through
// (re-arming trippedAt so concurrent callers still skip) and returns nil.
func (b *scraperCircuitBreaker) skipFailure(name string) *models.ScraperError {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.tripped[name] {
		return nil
	}
	if time.Since(b.trippedAt[name]) < b.cooldown {
		return &models.ScraperError{
			Scraper:   name,
			Kind:      models.ScraperErrorKindUnavailable,
			Message:   fmt.Sprintf("scraper skipped: circuit breaker tripped after %d consecutive failure(s)", b.failures[name]),
			Retryable: true,
			Temporary: true,
		}
	}
	// Cooldown elapsed: allow one half-open probe. Re-arm trippedAt so
	// concurrent callers continue to skip while the probe is in flight.
	b.trippedAt[name] = time.Now()
	return nil
}

// recordOutcome updates the breaker state for the named scraper after a query.
// A successful result (nil failure) resets the consecutive-failure counter and
// clears any trip. A circuit-breakable failure increments it and trips the
// breaker once the threshold is reached. Non-breakable failures are ignored.
func (b *scraperCircuitBreaker) recordOutcome(name string, failure *models.ScraperError) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if failure == nil {
		delete(b.failures, name)
		delete(b.tripped, name)
		delete(b.trippedAt, name)
		return
	}
	if failure.Kind != models.ScraperErrorKindUnavailable {
		// A non-Unavailable outcome (e.g. 404 NotFound, 403 Blocked, 429
		// RateLimited) proves the host is reachable, so the consecutive-failure
		// count is reset. This prevents a later isolated Unavailable from
		// tripping the breaker as if the prior failures were consecutive, and
		// clears a tripped breaker so a recovered host isn't skipped for another
		// cooldown window.
		delete(b.failures, name)
		delete(b.tripped, name)
		delete(b.trippedAt, name)
		return
	}
	b.failures[name]++
	if b.failures[name] >= b.threshold {
		b.tripped[name] = true
		b.trippedAt[name] = time.Now()
	}
}
