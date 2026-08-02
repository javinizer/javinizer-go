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
	// AllowPrivateHosts disables the default SSRF guard that rejects
	// loopback/private/link-local fetch and redirect targets. Opt-in for
	// trusted local mirrors; leave unset for remote-driven URLs.
	AllowPrivateHosts bool

	// resolveTargets enables DNS validation of fetch-target hostnames at the
	// request/redirect layer. It is only meaningful when the underlying
	// transport is a real HTTP transport that dials (possibly via proxy);
	// custom transports control their own connections.
	resolveTargets bool
	// proxyFunc mirrors the wrapped transport's proxy configuration so lookup
	// failures can fail closed when a proxy would resolve targets remotely.
	proxyFunc func(*http.Request) (*url.URL, error)

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

// blockedCIDRs are special-use ranges not covered by net.IP classification
// that still route to internal infrastructure: CGNAT (hosts cloud metadata
// IPs like 100.100.100.200), IETF protocol assignments, benchmark ranges,
// and reserved space.
var blockedCIDRs = mustCIDRs("100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4")

func mustCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, block, err := net.ParseCIDR(c)
		if err != nil {
			panic(fmt.Sprintf("invalid built-in CIDR %q: %v", c, err))
		}
		out = append(out, block)
	}
	return out
}

// hostIPLiteral parses host as an IP literal, tolerating IPv6 zones and
// bracketed forms; returns nil for hostnames.
func hostIPLiteral(host string) net.IP {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if i := strings.LastIndex(host, "%"); i >= 0 { // IPv6 zone, e.g. fe80::1%eth0
		host = host[:i]
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	return net.ParseIP(host)
}

// isBlockedFetchHost reports whether host is localhost or a non-public IP
// literal. Hostnames are not resolved here.
func isBlockedFetchHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := hostIPLiteral(host); ip != nil {
		return isBlockedIP(ip)
	}
	return false
}

// isBlockedIP reports whether ip is a non-public or special-use address.
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	for _, cidr := range blockedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// viaProxy reports whether fetches to host would traverse a configured
// HTTP(S) proxy (which resolves the target remotely).
func (f *Fetcher) viaProxy(host string) bool {
	if f.proxyFunc == nil {
		return false
	}
	proxyURL, err := f.proxyFunc(&http.Request{URL: &url.URL{Scheme: "https", Host: host}})
	return err == nil && proxyURL != nil
}

// checkFetchTarget validates the request host lexically and — when the
// fetcher drives a real dialing transport — resolves hostnames locally: with
// an HTTP(S) proxy configured the transport only ever dials the proxy
// address, so pinning there cannot see the real target. Custom transports
// make their own connections and skip resolution.
func (f *Fetcher) checkFetchTarget(ctx context.Context, host string) error {
	if isBlockedFetchHost(host) {
		return &BlockedFetchError{URL: host}
	}
	if !f.resolveTargets || hostIPLiteral(host) != nil {
		return nil
	}
	ips, err := lookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		if f.viaProxy(host) {
			// A proxy resolves the hostname remotely, so dial-time checks
			// cannot see the real target: fail closed when local resolution
			// cannot prove the host is public.
			return &BlockedFetchError{URL: host}
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
		fallback := guarded.DialContext
		if fallback == nil {
			dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
			fallback = dialer.DialContext
		}
		guarded.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if fetcher.AllowPrivateHosts {
				return fallback(ctx, network, addr)
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
		if !fetcher.AllowPrivateHosts {
			if err := fetcher.checkFetchTarget(req.Context(), req.URL.Hostname()); err != nil {
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
	if !f.AllowPrivateHosts {
		if err := f.checkFetchTarget(requestCtx, req.URL.Hostname()); err != nil {
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
