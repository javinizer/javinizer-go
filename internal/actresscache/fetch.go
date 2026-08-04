package actresscache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/javinizer/javinizer-go/internal/ssrf"
)

const (
	maxFetchAttempts = 3
	retryBaseDelay   = 250 * time.Millisecond
	maxRetryDelay    = 5 * time.Minute
)

var fetchAttempts = maxFetchAttempts

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
	// allowPrivateHosts disables the default SSRF guard that rejects
	// loopback/private/link-local fetch and redirect targets. Set only via
	// NewFetcherWithOptions (opt-in for trusted local mirrors); fixed at
	// construction so it can never flip mid-run.
	allowPrivateHosts bool

	// resolveTargets enables DNS validation of fetch-target hostnames at the
	// request/redirect layer. It is only meaningful when the underlying
	// transport is a real HTTP transport that dials (possibly via proxy);
	// custom transports control their own connections.
	resolveTargets bool
	// proxyFunc mirrors the wrapped transport's proxy configuration so lookup
	// failures can fail closed when a proxy would resolve targets remotely.
	proxyFunc func(*http.Request) (*url.URL, error)
	// proxyHosts are the configured proxies' hostnames per scheme; they are
	// trusted infrastructure and exempt from the dial-time pin (targets are
	// validated by name at the request layer instead).
	proxyHosts map[string]struct{}

	client     *http.Client
	delay      time.Duration
	hostDelays map[string]time.Duration
	mu         sync.Mutex
	limiters   map[string]*ratelimit.Limiter
	userAgent  string
}

// BlockedFetchError rejects requests or redirects targeting internal network
// hosts, so remote-supplied URLs cannot smuggle SSRF probes (loopback,
// private networks, cloud metadata endpoints) through the cache builder.
type BlockedFetchError struct {
	URL string
}

// Error ...
func (e *BlockedFetchError) Error() string {
	return fmt.Sprintf("refusing to fetch internal address: %s", e.URL)
}

// Blocklist policy lives in internal/ssrf (THE single guard). These local
// helpers exist only so the fetcher's proxy-trust paths can delegate.
func hostIPLiteral(host string) net.IP { return ssrf.HostIPLiteral(host) }

func isBlockedFetchHost(host string) bool { return ssrf.IsBlockedHost(host) }

func isBlockedIP(ip net.IP) bool { return ssrf.IsBlockedIP(ip) }

// viaProxy reports whether fetches to host would traverse a configured
// HTTP(S) proxy (which resolves the target remotely).
func (f *Fetcher) viaProxy(scheme, host string) bool {
	if f.proxyFunc == nil {
		return false
	}
	if scheme == "" {
		scheme = "https"
	}
	// Probe with the request's own scheme: HTTP_PROXY and HTTPS_PROXY are
	// configured independently, so an HTTP thumbnail URL must not be judged
	// by the HTTPS route.
	proxyURL, err := f.proxyFunc(&http.Request{URL: &url.URL{Scheme: scheme, Host: host}})
	return err == nil && proxyURL != nil
}

// checkFetchTarget validates the request host lexically and — when the
// fetcher drives a real dialing transport — resolves hostnames locally: with
// an HTTP(S) proxy configured the transport only ever dials the proxy
// address, so pinning there cannot see the real target. Custom transports
// make their own connections and skip resolution.
//
// Residual limitation (accepted, documented): when a proxy is in use the
// proxy performs the final resolution, so a hostname that is public in the
// local resolver view but private from the proxy's vantage point still
// passes. This is inherent to proxying — the proxy operator is part of the
// deployment's trust boundary — and the guard remains aimed at the common
// cases: literal/private targets and locally-resolvable internal names are
// rejected even under a proxy, and unresolvable names fail closed.
func (f *Fetcher) checkFetchTarget(ctx context.Context, scheme, host string) error {
	if isBlockedFetchHost(host) {
		return &BlockedFetchError{URL: host}
	}
	if !f.resolveTargets || hostIPLiteral(host) != nil {
		return nil
	}
	ips, err := lookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		if f.viaProxy(scheme, host) {
			// A proxy resolves the hostname remotely, so dial-time checks
			// cannot see the real target: fail closed when local resolution
			// cannot prove the host is public. Classified unverifiable
			// (transient): a DNS blip must not become a permanent rejection.
			return &ssrf.UnverifiableHostError{Host: host, Err: errors.New("cannot resolve fetch target locally while proxied")}
		}
		//nolint:nilerr // no proxy: dial surfaces DNS errors authoritatively.
		return nil
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return &BlockedFetchError{URL: host}
		}
	}
	return nil
}

// lookupIP resolves dial hostnames; replaced in tests.
var lookupIP = net.DefaultResolver.LookupIP

// guardedDialContext resolves addr's host and dials only the resolved
// address, so a hostname pointing at a private/loopback/link-local IP —
// including via DNS rebinding between a pre-check and connect — is rejected
// before any connection leaves the process.
func guardedDialContext(ctx context.Context, network, addr string, fallback func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if isBlockedFetchHost(host) {
		return nil, &BlockedFetchError{URL: addr}
	}
	ips, err := lookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("fetch target %s resolved to no addresses", host)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return nil, &BlockedFetchError{URL: addr}
		}
	}
	// Try every resolved address: dual-stack/multi-address hosts must not
	// fail just because the first answer is unreachable.
	var dialErr error
	for _, ip := range ips {
		conn, err := fallback(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		dialErr = err
	}
	return nil, dialErr
}

// NewFetcher ...
func NewFetcher(client *http.Client, delay time.Duration, userAgent string) *Fetcher {
	return NewFetcherWithHostDelays(client, delay, userAgent, nil)
}

// NewFetcherWithHostDelays ...
func NewFetcherWithHostDelays(client *http.Client, delay time.Duration, userAgent string, hostDelays map[string]time.Duration) *Fetcher {
	return NewFetcherWithOptions(client, delay, userAgent, hostDelays, false)
}

// NewFetcherWithOptions ...
func NewFetcherWithOptions(client *http.Client, delay time.Duration, userAgent string, hostDelays map[string]time.Duration, allowPrivateHosts bool) *Fetcher {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	normalizedDelays := make(map[string]time.Duration, len(hostDelays))
	for host, hostDelay := range hostDelays {
		normalizedDelays[normalizeRateLimitHost(host)] = hostDelay
	}
	fetcher := &Fetcher{
		allowPrivateHosts: allowPrivateHosts,
		delay:             delay,
		hostDelays:        normalizedDelays,
		limiters:          make(map[string]*ratelimit.Limiter),
		userAgent:         userAgent,
	}
	clientCopy := *client
	// Pin fetches to resolved public addresses at the transport level when
	// the caller uses a standard transport; custom transports stay untouched.
	transport, ok := clientCopy.Transport.(*http.Transport)
	if !ok && clientCopy.Transport == nil {
		transport = http.DefaultTransport.(*http.Transport)
		ok = transport != nil
	}
	if ok {
		fetcher.resolveTargets = true
		guarded := transport.Clone()
		fetcher.proxyFunc = guarded.Proxy
		if guarded.Proxy != nil {
			fetcher.proxyHosts = make(map[string]struct{}, 2)
			// Probe both schemes: HTTP_PROXY and HTTPS_PROXY may name
			// different hosts, so thumbnail fetches over either stay trusted.
			for _, scheme := range []string{"http", "https"} {
				if proxyURL, err := guarded.Proxy(&http.Request{URL: &url.URL{Scheme: scheme, Host: "dial-probe.invalid"}}); err == nil && proxyURL != nil {
					fetcher.proxyHosts[strings.ToLower(proxyURL.Hostname())] = struct{}{}
				}
			}
		}
		fallback := guarded.DialContext
		if fallback == nil {
			dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
			fallback = dialer.DialContext
		}
		guarded.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if fetcher.allowPrivateHosts {
				return fallback(ctx, network, addr)
			}
			if host, _, splitErr := net.SplitHostPort(addr); splitErr == nil && fetcher.proxyHosts != nil {
				if _, trusted := fetcher.proxyHosts[strings.ToLower(host)]; trusted {
					// The configured proxy itself (often a private corporate
					// address) is dialed to reach public targets that were
					// vetted at the request layer.
					return fallback(ctx, network, addr)
				}
			}
			return guardedDialContext(ctx, network, addr, fallback)
		}
		clientCopy.Transport = guarded
	}
	previousCheckRedirect := client.CheckRedirect
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if !fetcher.allowPrivateHosts {
			if err := fetcher.checkFetchTarget(req.Context(), req.URL.Scheme, req.URL.Hostname()); err != nil {
				return err
			}
		}
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
	if !f.allowPrivateHosts {
		if err := f.checkFetchTarget(requestCtx, req.URL.Scheme, req.URL.Hostname()); err != nil {
			return nil, nil, err
		}
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", accept)
	limiter := f.limiterForHost(req.URL.Hostname())
	for attempt := 0; attempt < fetchAttempts; attempt++ {
		if err := limiter.Wait(requestCtx); err != nil {
			return nil, nil, err
		}
		resp, err := f.client.Do(req)
		if err != nil {
			if attempt+1 == fetchAttempts || requestCtx.Err() != nil {
				return nil, nil, err
			}
			// Policy/typed failures are not transport faults: never burn a retry.
			var blocked *ssrf.BlockedTargetError
			var blockedFetch *BlockedFetchError
			var unverifiable *ssrf.UnverifiableHostError
			if errors.As(err, &blocked) || errors.As(err, &blockedFetch) || errors.As(err, &unverifiable) {
				return nil, nil, err
			}
			if err := waitRetry(requestCtx, retryBaseDelay<<attempt); err != nil {
				return nil, nil, err
			}
			continue
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			if attempt+1 < fetchAttempts && retryableStatus(resp.StatusCode) {
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
			if attempt+1 == fetchAttempts || requestCtx.Err() != nil {
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
