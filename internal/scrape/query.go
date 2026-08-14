package scrape

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"
	"time"

	neturl "net/url"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"golang.org/x/sync/errgroup"
)

type queryOutcome struct {
	result  *models.ScraperResult
	failure *models.ScraperError
}

func filterMovieScrapers(scrapers []models.Scraper) []models.Scraper {
	filtered := make([]models.Scraper, 0, len(scrapers))
	for _, s := range scrapers {
		if s == nil {
			continue
		}
		if c, ok := s.(models.MovieSearchCapable); ok && !c.SupportsMovieSearch() {
			continue
		}
		filtered = append(filtered, s)
	}
	return filtered
}

func resolveScraperNames(selectedScrapers, priorityOverride []string, cfg *Config) []string {
	if len(selectedScrapers) > 0 {
		return selectedScrapers
	}
	if len(priorityOverride) > 0 {
		return priorityOverride
	}
	if cfg != nil && len(cfg.ScrapersPriority) > 0 {
		return cfg.ScrapersPriority
	}
	return nil
}

func (s *Scraper) resolveContentID(ctx context.Context, movieID string, scraperNames []string) string {
	if len(scraperNames) == 0 || s.registry == nil {
		return movieID
	}

	for _, resolverName := range scraperNames {
		resolver, exists := s.registry.GetInstance(resolverName)
		if !exists || resolver == nil || !resolver.IsEnabled() {
			continue
		}
		if s.breaker != nil {
			if skip := s.breaker.skipFailure(resolverName); skip != nil {
				continue
			}
		}
		// Prefer the context-aware resolver so cancellation/timeouts reach the
		// lookup (DMM's ResolveContentID can issue HTTP). Fall back to the
		// non-context ContentIDResolver for scrapers that only implement that.
		if r, ok := resolver.(models.ContentIDResolverCtx); ok && r != nil {
			contentID, err := r.ResolveContentIDCtx(ctx, movieID)
			if s.breaker != nil && ctx.Err() == nil {
				if err == nil {
					s.breaker.recordOutcome(resolverName, nil)
				} else {
					s.breaker.recordOutcome(resolverName, classifyScraperError(resolverName, err, ""))
				}
			}
			if err != nil {
				logging.Debugf("[scrape] %s content-ID resolution failed: %v, using original ID", resolverName, err)
				return movieID
			}
			logging.Debugf("[scrape] Resolved content-ID: %s → %s", movieID, contentID)
			return contentID
		}
		if r, ok := resolver.(models.ContentIDResolver); ok && r != nil {
			contentID, err := r.ResolveContentID(movieID)
			if err != nil {
				logging.Debugf("[scrape] %s content-ID resolution failed: %v, using original ID", resolverName, err)
				return movieID
			}
			logging.Debugf("[scrape] Resolved content-ID: %s → %s", movieID, contentID)
			return contentID
		}
	}

	return movieID
}

// maxQueryConcurrency limits how many scrapers run in parallel.
// Scrapers are I/O-bound (HTTP requests), so parallelism helps latency
// without significantly increasing CPU or memory pressure.
var maxQueryConcurrency = runtime.NumCPU()

func (s *Scraper) queryAll(ctx context.Context, movieID, resolvedMovieID, rawInput string, scrapers []models.Scraper, startTime time.Time) ([]*models.ScraperResult, []models.ScraperError) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(scrapers) <= 1 {
		// Fast path: single scraper or empty — no goroutine overhead.
		if len(scrapers) == 0 {
			return nil, nil
		}
		outcome := s.queryWithBreaker(ctx, resolvedMovieID, rawInput, scrapers[0])
		var results []*models.ScraperResult
		var failures []models.ScraperError
		if outcome.result != nil {
			results = append(results, outcome.result)
		}
		if outcome.failure != nil {
			failures = append(failures, *outcome.failure)
		}
		return results, failures
	}

	// Pre-allocate indexed slices to preserve scraper ordering.
	outcomes := make([]queryOutcome, len(scrapers))

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(maxQueryConcurrency)

	for i, scraper := range scrapers {
		i, scraper := i, scraper
		g.Go(func() error {
			// Respect cancellation: don't start new scrapers if context is already done.
			select {
			case <-gCtx.Done():
				return gCtx.Err()
			default:
			}
			outcomes[i] = s.queryWithBreaker(gCtx, resolvedMovieID, rawInput, scraper)
			return nil // errors are captured in outcomes[i].failure
		})
	}

	// Wait for all scrapers to complete. errgroup cancels the group context
	// if any goroutine returns a non-nil error, but our goroutines always return nil.
	_ = g.Wait()

	// Collect results in scraper order.
	results := make([]*models.ScraperResult, 0, len(scrapers))
	failures := make([]models.ScraperError, 0)
	for _, outcome := range outcomes {
		if outcome.result != nil {
			results = append(results, outcome.result)
			continue
		}
		if outcome.failure != nil {
			failures = append(failures, *outcome.failure)
		}
	}

	// If the parent context was cancelled, append a classified context error
	// so its message is friendly ("scrape timed out"/"scrape canceled") and
	// its Kind is unavailable, rather than a raw sentinel.
	if ctx.Err() != nil {
		failures = append(failures, *classifyContextError("context", ctx.Err()))
	}

	return results, failures
}

func querySingle(ctx context.Context, movieID, rawInput string, scraper models.Scraper) (outcome queryOutcome) {
	defer func() {
		if r := recover(); r != nil {
			outcome = queryOutcome{
				failure: &models.ScraperError{
					Scraper: scraper.Name(),
					Message: panicutil.FormatRecover(r).Error(),
				},
			}
		}
	}()

	if rawInput != "" {
		if uh, ok := scraper.(models.URLHandler); ok && uh.CanHandleURL(rawInput) {
			result, err := safeScrapeURL(ctx, uh, rawInput)
			if err == nil && result != nil {
				if strings.TrimSpace(result.ID) == "" {
					err = models.NewScraperNotFoundError(scraper.Name(), "direct URL returned result without ID")
				} else {
					outcome = queryOutcome{result: result}
					return
				}
			}
			if err == nil && result == nil {
				err = models.NewScraperNotFoundError(scraper.Name(), "URL handler returned no result")
			}
			if isContextError(ctx, err) {
				outcome = queryOutcome{failure: classifyContextError(scraper.Name(), err)}
				return
			}
			if se, ok := models.AsScraperError(err); ok {
				if se.Kind != models.ScraperErrorKindNotFound {
					se.Scraper = scraper.Name()
					outcome = queryOutcome{failure: se}
					return
				}
			} else {
				outcome = queryOutcome{failure: classifyScraperError(scraper.Name(), err, "")}
				return
			}
		}
	}

	scraperQuery := movieID
	if mappedQuery, ok := models.ResolveSearchQueryForScraper(scraper, movieID); ok {
		scraperQuery = mappedQuery
	}

	scraperResult, err := safeSearch(ctx, scraper, scraperQuery)
	if err != nil {
		if isContextError(ctx, err) {
			outcome = queryOutcome{failure: classifyContextError(scraper.Name(), err)}
			return
		}

		if scraperQuery != movieID {
			retryResult, retryErr := safeSearch(ctx, scraper, movieID)
			if retryErr == nil {
				outcome = queryOutcome{result: retryResult}
				return
			}
			if isContextError(ctx, retryErr) {
				outcome = queryOutcome{failure: classifyContextError(scraper.Name(), retryErr)}
				return
			}
			outcome = queryOutcome{failure: classifyScraperError(scraper.Name(), retryErr, fmt.Sprintf("%v (mapped query: %v)", retryErr, err))}
			return
		}

		outcome = queryOutcome{failure: classifyScraperError(scraper.Name(), err, "")}
		return
	}

	outcome = queryOutcome{result: scraperResult}
	return
}

// isContextError checks if the error is a context cancellation/deadline error,
// either via errors.Is(err, ctx.Err()) or by checking the sentinel errors directly.
// This catches cases where the scraper returns context.DeadlineExceeded from its
// own request context while the parent ctx.Err() is nil.
func isContextError(ctx context.Context, err error) bool {
	if errors.Is(err, ctx.Err()) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	return false
}

// classifyContextError constructs a typed ScraperError for context cancellation/deadline
// errors. AsScraperError() cannot extract typed fields from raw context errors because
// they are not ScraperError instances, so this function explicitly sets Kind=unavailable,
// Retryable=true, Temporary=true.
func classifyContextError(scraperName string, err error) *models.ScraperError {
	msg := err.Error()
	if errors.Is(err, context.DeadlineExceeded) {
		msg = "scrape timed out"
	} else if errors.Is(err, context.Canceled) {
		msg = "scrape canceled"
	}
	return &models.ScraperError{
		Scraper:   scraperName,
		Kind:      models.ScraperErrorKindUnavailable,
		Message:   msg,
		Retryable: true,
		Temporary: true,
		Cause:     err,
	}
}

// classifyScraperError wraps a scraper error, preserving typed fields (Kind,
// StatusCode, Retryable, Temporary) via AsScraperError when available. If the
// error is not a ScraperError, it falls back to a generic unknown classification.
// The fallbackMsg is used as the Message when the error has no typed fields.
func classifyScraperError(scraperName string, err error, fallbackMsg string) *models.ScraperError {
	if se, ok := models.AsScraperError(err); ok {
		copied := *se
		copied.Scraper = scraperName
		if copied.Message == "" {
			copied.Message = err.Error()
		}
		if copied.Kind == models.ScraperErrorKindUnknown && isTransportError(copied.Cause) {
			copied.Kind = models.ScraperErrorKindUnavailable
			copied.Retryable = true
			copied.Temporary = true
		}
		return &copied
	}
	msg := fallbackMsg
	if msg == "" {
		msg = err.Error()
	}
	kind := models.ScraperErrorKindUnknown
	retryable := false
	temporary := false
	if isTransportError(err) {
		kind = models.ScraperErrorKindUnavailable
		retryable = true
		temporary = true
	}
	return &models.ScraperError{
		Scraper:   scraperName,
		Kind:      kind,
		Message:   msg,
		Cause:     err,
		Retryable: retryable,
		Temporary: temporary,
	}
}

func safeSearch(ctx context.Context, scraper models.Scraper, id string) (result *models.ScraperResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = panicutil.HandleRecover(r)
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	result, err = scraper.Search(ctx, id)
	if result != nil {
		result.NormalizeMediaURLs()
	}
	return result, err
}

//nolint:unused // used by circuit_breaker_test.go
func (s *Scraper) queryWithBreaker(ctx context.Context, movieID, rawInput string, scraper models.Scraper) queryOutcome {
	if s.breaker != nil {
		if skip := s.breaker.skipFailure(scraper.Name()); skip != nil {
			return queryOutcome{failure: skip}
		}
	}
	outcome := querySingle(ctx, movieID, rawInput, scraper)
	if s.breaker != nil && ctx.Err() == nil {
		if outcome.result != nil {
			s.breaker.recordOutcome(scraper.Name(), nil)
		} else if outcome.failure != nil {
			s.breaker.recordOutcome(scraper.Name(), outcome.failure)
		}
	}
	return outcome
}

//nolint:unused // used by circuit_breaker_test.go
func isTransportError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *neturl.Error
	return errors.As(err, &urlErr)
}

//nolint:unused // used by redact_error_test.go
type urlRedactedError struct {
	msg   string
	cause error
}

//nolint:unused // used by redact_error_test.go
func (e *urlRedactedError) Error() string { return e.msg }

//nolint:unused // used by redact_error_test.go
func (e *urlRedactedError) Unwrap() error { return e.cause }

//nolint:unused // used by redact_error_test.go
func redactErrorURL(err error, rawURL string) error {
	if err == nil || rawURL == "" {
		return err
	}
	redacted := RedactSourceURL(rawURL)
	if redacted == rawURL || !strings.Contains(err.Error(), rawURL) {
		return err
	}
	if se, ok := models.AsScraperError(err); ok {
		clone := *se
		clone.Message = strings.ReplaceAll(clone.Message, rawURL, redacted)
		if clone.Cause != nil {
			clone.Cause = redactErrorURL(clone.Cause, rawURL)
		}
		return &clone
	}
	return &urlRedactedError{
		msg:   strings.ReplaceAll(err.Error(), rawURL, redacted),
		cause: err,
	}
}

//nolint:unused // used by url_dispatch_test.go
func safeScrapeURL(ctx context.Context, handler models.URLHandler, url string) (result *models.ScraperResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			redacted := strings.ReplaceAll(fmt.Sprint(r), url, RedactSourceURL(url))
			err = panicutil.HandleRecover(redacted)
			err = redactErrorURL(err, url)
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	result, err = handler.ScrapeURL(ctx, url)
	if result != nil {
		result.NormalizeMediaURLs()
		result.SourceURL = RedactSourceURL(result.SourceURL)
	}
	if err != nil {
		err = redactErrorURL(err, url)
	}
	return result, err
}
