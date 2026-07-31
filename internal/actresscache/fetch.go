package actresscache

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/ratelimit"
)

const (
	maxFetchAttempts = 3
	retryBaseDelay   = 250 * time.Millisecond
	maxRetryDelay    = 5 * time.Minute
)

// HTTPError ...
type HTTPError struct {
	StatusCode int
	URL        string
}

// Error ...
func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d for %s", e.StatusCode, e.URL)
}

// IsTransient ...
func (e *HTTPError) IsTransient() bool {
	return e != nil && retryableStatus(e.StatusCode)
}

// Fetcher ...
type Fetcher struct {
	client     *http.Client
	delay      time.Duration
	hostDelays map[string]time.Duration
	mu         sync.Mutex
	limiters   map[string]*ratelimit.Limiter
	userAgent  string
}

// NewFetcher ...
func NewFetcher(client *http.Client, delay time.Duration, userAgent string) *Fetcher {
	return NewFetcherWithHostDelays(client, delay, userAgent, nil)
}

// NewFetcherWithHostDelays ...
func NewFetcherWithHostDelays(client *http.Client, delay time.Duration, userAgent string, hostDelays map[string]time.Duration) *Fetcher {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	normalizedDelays := make(map[string]time.Duration, len(hostDelays))
	for host, hostDelay := range hostDelays {
		normalizedDelays[normalizeRateLimitHost(host)] = hostDelay
	}
	fetcher := &Fetcher{
		delay:      delay,
		hostDelays: normalizedDelays,
		limiters:   make(map[string]*ratelimit.Limiter),
		userAgent:  userAgent,
	}
	clientCopy := *client
	previousCheckRedirect := client.CheckRedirect
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := fetcher.limiterForHost(req.URL.Hostname()).Wait(req.Context()); err != nil {
			return err
		}
		if previousCheckRedirect != nil {
			return previousCheckRedirect(req, via)
		}
		return nil
	}
	fetcher.client = &clientCopy
	return fetcher
}

// Get ...
func (f *Fetcher) Get(ctx context.Context, rawURL, accept string, maxBytes int64) ([]byte, http.Header, error) {
	if f == nil || f.client == nil || f.limiters == nil {
		return nil, nil, fmt.Errorf("actress cache fetcher is not initialized")
	}
	requestCtx := ctx
	// cancel ...
	var cancel context.CancelFunc
	if f.client.Timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, f.client.Timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", accept)
	limiter := f.limiterForHost(req.URL.Hostname())
	for attempt := 0; attempt < maxFetchAttempts; attempt++ {
		if err := limiter.Wait(requestCtx); err != nil {
			return nil, nil, err
		}
		resp, err := f.client.Do(req)
		if err != nil {
			if attempt+1 == maxFetchAttempts || requestCtx.Err() != nil {
				return nil, nil, err
			}
			if err := waitRetry(requestCtx, retryBaseDelay<<attempt); err != nil {
				return nil, nil, err
			}
			continue
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			if attempt+1 < maxFetchAttempts && retryableStatus(resp.StatusCode) {
				delay := retryDelay(resp.Header, attempt)
				_ = resp.Body.Close()
				if err := waitRetry(requestCtx, delay); err != nil {
					return nil, nil, err
				}
				continue
			}
			headers := resp.Header
			_ = resp.Body.Close()
			return nil, headers, &HTTPError{StatusCode: resp.StatusCode, URL: rawURL}
		}
		if maxBytes <= 0 {
			maxBytes = 8 << 20
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		_ = resp.Body.Close()
		if err != nil {
			if attempt+1 == maxFetchAttempts || requestCtx.Err() != nil {
				return nil, resp.Header, err
			}
			if err := waitRetry(requestCtx, retryBaseDelay<<attempt); err != nil {
				return nil, resp.Header, err
			}
			continue
		}
		if int64(len(body)) > maxBytes {
			return nil, resp.Header, fmt.Errorf("response from %s exceeds %d bytes", rawURL, maxBytes)
		}
		return body, resp.Header, nil
	}
	return nil, nil, fmt.Errorf("request attempts exhausted for %s", rawURL)
}

func (f *Fetcher) limiterForHost(host string) *ratelimit.Limiter {
	key := normalizeRateLimitHost(host)
	f.mu.Lock()
	defer f.mu.Unlock()
	if limiter, ok := f.limiters[key]; ok {
		return limiter
	}
	limiter := ratelimit.NewLimiter(f.delayForHost(key))
	f.limiters[key] = limiter
	return limiter
}

func (f *Fetcher) delayForHost(host string) time.Duration {
	if delay, ok := f.hostDelays[normalizeRateLimitHost(host)]; ok {
		return delay
	}
	return f.delay
}

// normalizeRateLimitHost ...
func normalizeRateLimitHost(host string) string {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return strings.TrimPrefix(host, "www.")
}

// retryableStatus ...
func retryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// retryDelay ...
func retryDelay(headers http.Header, attempt int) time.Duration {
	if raw := strings.TrimSpace(headers.Get("Retry-After")); raw != "" {
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds >= 0 {
			maxSeconds := int64(maxRetryDelay / time.Second)
			if seconds >= maxSeconds {
				return maxRetryDelay
			}
			return time.Duration(seconds) * time.Second
		}
		if raw != "" && strings.Trim(raw, "0123456789") == "" {
			return maxRetryDelay
		}
		if retryAt, err := http.ParseTime(raw); err == nil {
			if delay := time.Until(retryAt); delay > 0 {
				if delay > maxRetryDelay {
					return maxRetryDelay
				}
				return delay
			}
			return 0
		}
	}
	delay := retryBaseDelay << attempt
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}

// waitRetry ...
func waitRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
