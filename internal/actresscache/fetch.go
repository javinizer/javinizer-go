package actresscache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
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
	// remoteDNS is true for transports whose dialer owns hostname resolution
	// (SOCKS5): target names pass through unresolved -- locally pinning would
	// defeat proxy-only/split-horizon names -- while private IP literals stay
	// blocked and the lexical preflight keeps running.
	remoteDNS bool

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

// stampProxyDecision evaluates the proxy policy for THIS hop and pins the
// answer onto the request context (in place: redirect callbacks cannot
// substitute the request pointer, so we mutate the struct). Every hop gets
// its own evaluation -- e.g. a NO_PROXY match on a redirect target must not
// inherit hop 1's decision.
func (f *Fetcher) stampProxyDecision(req *http.Request) error {
	if f.proxyFunc == nil {
		return nil
	}
	decision, err := f.proxyFunc(req)
	if err != nil {
		return fmt.Errorf("actress cache proxy decision failed: %w", err)
	}
	*req = *req.WithContext(context.WithValue(req.Context(), proxyDecisionCtxKey{}, &proxyDecision{proxyURL: decision}))
	return nil
}

// requestProxied evaluates the transport's proxy policy against the ACTUAL
// request: synthetic probes carry no headers, so header-keyed policies (e.g.
// User-Agent-dependent routing) would misclassify the hop and downgrade the
// fail-closed branch below.
func (f *Fetcher) requestProxied(req *http.Request) bool {
	if f.proxyFunc == nil {
		return false
	}
	decision, err := f.proxyFunc(req)
	return err == nil && decision != nil
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
// checkFetchTarget validates host lexically and, when pinning is active,
// resolves it locally. authority (host[:port]) feeds the port-sensitive proxy
// decision probe (NO_PROXY entries may carry ports); pass host when no port
// is involved.
func (f *Fetcher) checkFetchTarget(ctx context.Context, req *http.Request) error {
	host := req.URL.Hostname()
	if isBlockedFetchHost(host) {
		return &BlockedFetchError{URL: host}
	}
	if f.remoteDNS {
		// Remote-DNS transports own name resolution; local answers prove
		// nothing about what the proxy will get.
		return nil
	}
	if !f.resolveTargets || hostIPLiteral(host) != nil {
		return nil
	}
	ips, err := lookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		// A prior evaluation (Get stamps one per attempt) replays; compute only
		// when nothing was stamped (e.g. caller-mutated redirects).
		proxied := f.requestProxied(req)
		if stamped, ok := req.Context().Value(proxyDecisionCtxKey{}).(*proxyDecision); ok {
			proxied = stamped.proxyURL != nil
		}
		if proxied {
			// A proxy resolves the hostname remotely, so dial-time checks
			// cannot see the real target: fail closed when local resolution
			// cannot prove the host is public. Classified unverifiable
			// (transient): a DNS blip must not become a permanent rejection.
			// Preserve the resolver error (notably context cancellation /
			// deadline) so callers classify the failure correctly.
			resolveErr := err
			if resolveErr == nil {
				resolveErr = errors.New("cannot resolve fetch target locally while proxied: no A/AAAA records")
			}
			return &ssrf.UnverifiableHostError{Host: host, Err: resolveErr}
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

// FetchTooLargeError marks an operator-policy byte-ceiling breach. Unlike a
// flaky download, the image will still be oversized tomorrow.
type FetchTooLargeError struct {
	URL   string
	Limit int64
}

// Error ...
func (e *FetchTooLargeError) Error() string {
	return fmt.Sprintf("response from %s exceeds %d bytes", e.URL, e.Limit)
}

// lookupIP resolves dial hostnames; replaced in tests.
var lookupIP = net.DefaultResolver.LookupIP

// proxyPinningTransport decides whether a proxy-routed request may proceed:
// a proxy re-resolves hostnames independently, so a plain-HTTP request whose
// hostname the local guard cleared could reach a private address via
// split-horizon DNS. Stdlib couples the absolute request line and the Host
// header, leaving no way to pin the target IP while preserving virtual
// hosting — so hostname targets over an active proxy are rejected as
// unverifiable. Literal-IP targets (nothing to re-resolve) pass through, as
// does HTTPS: CONNECT tunnels inherit the documented proxy-trust-boundary
// model on checkFetchTarget.
// proxiedDialCtxKey marks the ONE dial that serves a proxied request: the
// request layer records the canonical proxy endpoint it selected, and only
// a dial to that exact endpoint may skip the guarded/pinned path. Authority
// alone (even host:port) cannot vouch for routing -- a NO_PROXY rule can
// send a same-authority target directly, and DNS can move after preflight.
type proxiedDialCtxKey struct{}

// proxyDecision records the wrapper's evaluated proxy choice (nil URL = the
// request is explicitly direct). A stateful/rotating Proxy func could answer
// differently when net/http evaluates it for the real hop; the ledger makes
// the base transport replay THIS decision so markers, rejections, and dials
// cannot disagree.
type proxyDecision struct{ proxyURL *url.URL }

type proxyDecisionCtxKey struct{}

type proxyPinningTransport struct {
	base     http.RoundTripper
	proxyFor func(*http.Request) (*url.URL, error)
}

// RoundTrip ...
func (p *proxyPinningTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if p == nil || p.base == nil {
		return nil, fmt.Errorf("actress cache proxy pinning transport is not initialized")
	}
	// Compute the proxy decision from the ACTUAL request: custom Proxy funcs
	// may key on headers (e.g. User-Agent); a synthetic probe could declare
	// "direct" while the real hop is proxied, skipping both the plain-HTTP
	// hostname rejection and the request-bound endpoint marker.
	// Replay a stamped decision when one exists (Get evaluates once); only a
	// request that never passed Get's evaluation computes fresh here.
	var proxyURL *url.URL
	if p.proxyFor != nil {
		if stamped, ok := req.Context().Value(proxyDecisionCtxKey{}).(*proxyDecision); ok {
			proxyURL = stamped.proxyURL
		} else {
			decision, err := p.proxyFor(req)
			if err != nil {
				return nil, fmt.Errorf("actress cache proxy decision failed: %w", err)
			}
			proxyURL = decision
			req = req.WithContext(context.WithValue(req.Context(), proxyDecisionCtxKey{}, &proxyDecision{proxyURL: decision}))
		}
		if proxyURL != nil {
			// Dial exemption marker is hop-scoped: always refresh it for THIS
			// hop (redirects re-enter RoundTrip with the stamped decision).
			req = req.WithContext(context.WithValue(req.Context(), proxiedDialCtxKey{}, canonicalProxyDialTarget(proxyURL.Scheme, proxyURL.Hostname(), proxyURL.Port())))
		}
	}
	if req.URL.Scheme == "http" && proxyURL != nil && hostIPLiteral(req.URL.Hostname()) == nil {
		return nil, &ssrf.UnverifiableHostError{Host: req.URL.Hostname(), Err: errors.New("plain-HTTP proxy fetch of a hostname cannot pin the resolved address (use an HTTPS target or a CONNECT tunnel)")}
	}
	return p.base.RoundTrip(req)
}

// schemeHTTPS names the https scheme in one place (goconst threshold).
const schemeHTTPS = "https"

// canonicalProxyDialTarget renders scheme/hostname/port as the dial authority
// net/http will use: lowercase hostname, with the proxy scheme's default port
// filled in (net/http dials proxies on those defaults when no port is given).
func canonicalProxyDialTarget(proxyScheme, hostname, port string) string {
	if port == "" {
		switch strings.ToLower(proxyScheme) {
		case schemeHTTPS:
			port = "443"
		case "socks5", "socks5h":
			port = "1080"
		default:
			port = "80"
		}
	}
	return strings.ToLower(strings.TrimSpace(hostname)) + ":" + port
}

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
func NewFetcher(client *http.Client, delay time.Duration, userAgent string) (*Fetcher, error) {
	return NewFetcherWithHostDelays(client, delay, userAgent, nil)
}

// NewFetcherWithHostDelays ...
func NewFetcherWithHostDelays(client *http.Client, delay time.Duration, userAgent string, hostDelays map[string]time.Duration) (*Fetcher, error) {
	return NewFetcherWithOptions(client, delay, userAgent, hostDelays, false)
}

// NewFetcherWithOptions fails closed: with private hosts disabled it REJECTS
// caller transports that cannot be egress-pinned (wrapped RoundTrippers whose
// dialing is invisible, and transports carrying DialTLS/DialTLSContext that
// bypass DialContext for HTTPS). Retaining either would silently downgrade
// the SSRF guard to lexical-only checks, so construction returns an error
// instead. Trusted local mirrors may opt in via allowPrivateHosts.
func NewFetcherWithOptions(client *http.Client, delay time.Duration, userAgent string, hostDelays map[string]time.Duration, allowPrivateHosts bool) (*Fetcher, error) {
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
		transport, ok = http.DefaultTransport.(*http.Transport)
	}
	if !ok && !allowPrivateHosts {
		// A wrapped RoundTripper (cloudscraper-style) dials invisibly; with
		// egress pinning mandatory there is no safe fallback.
		return nil, fmt.Errorf("actresscache: fetch requires a *http.Transport (got %T) so egress can be pinned; pass allowPrivateHosts for trusted local mirrors", clientCopy.Transport)
	}
	if ok && (transport.DialTLSContext != nil || transport.DialTLS != nil) { //nolint:staticcheck // reading deprecated DialTLS is required to fail closed on unpinnable transports
		// A custom TLS dialer bypasses DialContext for HTTPS and cannot be
		// pinned; refuse to wrap and keep the request-layer guard only.
		if !allowPrivateHosts {
			return nil, fmt.Errorf("actresscache: fetch refuses transports with DialTLS/DialTLSContext (unpinnable TLS dialer would bypass the SSRF guard)")
		}
		log.Printf("actresscache: fetch client transport has DialTLS*/DialTLSContext; SSRF pinning unavailable for this allowed-private client")
		ok = false
	}
	if ok {
		fetcher.resolveTargets = true
		fetcher.remoteDNS = ssrf.TransportResolvesRemotely(transport)
		guarded := transport.Clone()
		fetcher.proxyFunc = guarded.Proxy
		if guarded.Proxy != nil {
			original := guarded.Proxy
			// Ledger: when the pinning wrapper already evaluated the policy
			// for this request, net/http MUST replay that decision -- a
			// rotating/stateful Proxy func could otherwise answer differently
			// on the real hop than the checks validated.
			guarded.Proxy = func(req *http.Request) (*url.URL, error) {
				if decision, ok := req.Context().Value(proxyDecisionCtxKey{}).(*proxyDecision); ok {
					return decision.proxyURL, nil
				}
				return original(req)
			}
		}
		// Respect a legacy-only Dial hook: assigning DialContext while
		// ignoring Dial would silently discard the caller's dialer.
		fallback := ssrf.DialContextFunc(guarded)
		if fetcher.remoteDNS {
			// SOCKS5-style transports: the dialer owns DNS, so hostnames pass
			// through untouched; private IP literals remain blocked here because
			// they need no DNS.
			guarded.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				if !fetcher.allowPrivateHosts {
					host, _, splitErr := net.SplitHostPort(addr)
					if splitErr == nil {
						if ip := ssrf.HostIPLiteral(host); ip != nil && ssrf.IsBlockedIP(ip) {
							return nil, &ssrf.BlockedTargetError{Target: host, Reason: "private/internal IP literal"}
						}
					}
				}
				return fallback(ctx, network, addr)
			}
		} else {
			guarded.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				if fetcher.allowPrivateHosts {
					return fallback(ctx, network, addr)
				}
				if endpoint, ok := ctx.Value(proxiedDialCtxKey{}).(string); ok && endpoint != "" {
					// Only dials serving THIS proxied request may use the proxy
					// lane, and only to the exact endpoint the request layer
					// selected. Even then the proxy hostname is resolved ONCE and
					// dialed pinned: the raw fallback would re-resolve at connect
					// time and a rebind could move the proxy connection somewhere
					// unvetted. (Proxies are trusted infrastructure, so answers are
					// pinned without the public-address gate.)
					if host, port, splitErr := net.SplitHostPort(addr); splitErr == nil && canonicalProxyDialTarget("", host, port) == endpoint {
						if net.ParseIP(host) != nil {
							return fallback(ctx, network, addr)
						}
						ips, rerr := lookupIP(ctx, "ip", host)
						if rerr != nil {
							return nil, fmt.Errorf("resolve configured proxy %s: %w", host, rerr)
						}
						if len(ips) == 0 {
							return nil, fmt.Errorf("resolve configured proxy %s: no addresses", host)
						}
						var dialErr error
						for _, ip := range ips {
							conn, cerr := fallback(ctx, network, net.JoinHostPort(ip.String(), port))
							if cerr == nil {
								return conn, nil
							}
							dialErr = errors.Join(dialErr, cerr)
						}
						return nil, dialErr
					}
				}
				return guardedDialContext(ctx, network, addr, fallback)
			}
		}
		clientCopy.Transport = guarded
	}
	previousCheckRedirect := client.CheckRedirect
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		// Restamp the hop-scoped proxy decision before decision-dependent checks.
		if err := fetcher.stampProxyDecision(req); err != nil {
			return err
		}
		if !fetcher.allowPrivateHosts {
			if err := fetcher.checkFetchTarget(req.Context(), req); err != nil {
				return err
			}
		}
		if err := fetcher.limiterForHost(req.URL.Hostname()).Wait(req.Context()); err != nil {
			return err
		}
		if previousCheckRedirect != nil {
			before := req.URL.String()
			if err := previousCheckRedirect(req, via); err != nil {
				return err
			}
			if req.URL.String() != before {
				// The caller's policy REWROTE req.URL: restamp the decision and
				// revalidate the final target so a rewritten hop cannot smuggle
				// a private address past the entry guard. (Unchanged hops keep
				// the decision taken before the callback -- re-evaluating would
				// let a stateful policy flip within the same hop.)
				if err := fetcher.stampProxyDecision(req); err != nil {
					return err
				}
				if !fetcher.allowPrivateHosts {
					return fetcher.checkFetchTarget(req.Context(), req)
				}
			}
		}
		return nil
	}
	if !fetcher.allowPrivateHosts && fetcher.resolveTargets {
		// Plain-HTTP proxy requests otherwise carry the original HOSTNAME to
		// the proxy, which re-resolves it independently (split-horizon DNS can
		// land the proxy on a private address our local check never sees).
		// Plain-HTTP hostname targets cannot be pinned through a proxy — Go's
		// stdlib writes one value into both the absolute request line and the
		// Host header — so they are REJECTED as unverifiable rather than
		// approximated. Literal-IP targets and HTTPS/CONNECT tunneled hosts
		// pass through (the latter under the documented proxy trust model).
		clientCopy.Transport = &proxyPinningTransport{base: clientCopy.Transport, proxyFor: fetcher.proxyFunc}
	}
	fetcher.client = &clientCopy
	return fetcher, nil
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
	// Headers first: proxy policy may key on them, and the preflight must
	// evaluate the decision against the exact request that will be dialed.
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", accept)
	// Evaluate the proxy decision ONCE per hop and carry it on the context:
	// preflight, the redirect wrapper, the marker, and net/http all replay
	// this single evaluation, so a stateful policy cannot answer differently
	// between the fail-closed check and the actual hop. Redirect hops restamp
	// their own decision in the CheckRedirect wrapper below.
	if err := f.stampProxyDecision(req); err != nil {
		return nil, nil, err
	}
	if !f.allowPrivateHosts {
		if err := f.checkFetchTarget(requestCtx, req); err != nil {
			return nil, nil, err
		}
	}
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
		// Read one byte past the cap to detect over-limit bodies; at
		// math.MaxInt64 the +1 would overflow to a negative limit and
		// LimitReader would yield an empty (hence undecodable) body.
		limit := maxBytes
		if limit < math.MaxInt64 {
			limit++
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
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
			// The policy ceiling is a persistent property of the resource, not
			// a transport fault: classification maps this to a rejection, so
			// default builds skip it and refreshes do not wedge publish aborts.
			return nil, resp.Header, &FetchTooLargeError{URL: rawURL, Limit: maxBytes}
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
