package scrape

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"golang.org/x/sync/errgroup"
)

type queryOutcome struct {
	result  *models.ScraperResult
	failure *models.ScraperError
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
	if len(scraperNames) == 0 {
		return movieID
	}
	resolverName := scraperNames[0]
	resolver, exists := s.registry.GetInstance(resolverName)
	if !exists || resolver == nil {
		return movieID
	}
	// Prefer the context-aware resolver so cancellation/timeouts reach the
	// lookup (DMM's ResolveContentID can issue HTTP). Fall back to the
	// non-context ContentIDResolver for scrapers that only implement that.
	if r, ok := resolver.(models.ContentIDResolverCtx); ok && r != nil {
		contentID, err := r.ResolveContentIDCtx(ctx, movieID)
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
	return movieID
}

// maxQueryConcurrency limits how many scrapers run in parallel.
// Scrapers are I/O-bound (HTTP requests), so parallelism helps latency
// without significantly increasing CPU or memory pressure.
var maxQueryConcurrency = runtime.NumCPU()

func (s *Scraper) queryAll(ctx context.Context, movieID, resolvedMovieID string, scrapers []models.Scraper, startTime time.Time) ([]*models.ScraperResult, []models.ScraperError) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(scrapers) <= 1 {
		// Fast path: single scraper or empty — no goroutine overhead.
		if len(scrapers) == 0 {
			return nil, nil
		}
		outcome := querySingle(ctx, resolvedMovieID, scrapers[0])
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
			outcomes[i] = querySingle(gCtx, resolvedMovieID, scraper)
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

	// If the parent context was cancelled, append a context error.
	if ctx.Err() != nil {
		failures = append(failures, models.ScraperError{Scraper: "context", Cause: ctx.Err()})
	}

	return results, failures
}

func querySingle(ctx context.Context, movieID string, scraper models.Scraper) (outcome queryOutcome) {
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
	return &models.ScraperError{
		Scraper:   scraperName,
		Kind:      models.ScraperErrorKindUnavailable,
		Message:   err.Error(),
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
		return &copied
	}
	msg := fallbackMsg
	if msg == "" {
		msg = err.Error()
	}
	return &models.ScraperError{
		Scraper: scraperName,
		Kind:    models.ScraperErrorKindUnknown,
		Message: msg,
		Cause:   err,
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
