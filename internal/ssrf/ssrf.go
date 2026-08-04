package ssrf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	lookupIPMu sync.RWMutex
	lookupIP   = net.LookupIP
)

// testAllowedHosts are exact hostnames the guard bypasses for tests.
// Production code must never call AllowHostForTest.
var testAllowedHosts sync.Map

func setLookupIPForTest(fn func(string) ([]net.IP, error)) func() {
	lookupIPMu.Lock()
	defer lookupIPMu.Unlock()
	original := lookupIP
	lookupIP = fn
	return func() {
		lookupIPMu.Lock()
		defer lookupIPMu.Unlock()
		lookupIP = original
	}
}

func currentLookupIP() func(string) ([]net.IP, error) {
	lookupIPMu.RLock()
	defer lookupIPMu.RUnlock()
	return lookupIP
}

func hostAllowedForTest(host string) bool {
	_, ok := testAllowedHosts.Load(normalizeHost(host))
	return ok
}

// blockedTargetPrefixes are special-use/internal ranges that must never be
// dialed from URL-driven fetch paths: RFC1918, loopback, link-local, CGNAT
// (cloud metadata endpoints live there), IETF/protocol-assignment and
// benchmark space, documentation prefixes, multicast/reserved space, and the
// IPv6 transition ranges that tunnel embedded IPv4 through anycast relays.
var blockedTargetPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	// NAT64 well-known + local-use prefixes embed/carry translated IPv4.
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/3"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("4000::/2"),
	netip.MustParsePrefix("8000::/1"),
}

// IsBlockedIP reports whether ip is internal, non-public, or special-use.
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsLoopback() || address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() ||
		address.IsPrivate() {
		return true
	}
	for _, prefix := range blockedTargetPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func normalizeHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}

// IsBlockedHost reports whether host lexically targets internal space:
// localhost names or an IP literal matching IsBlockedIP. Hostnames are not
// resolved here.
func IsBlockedHost(host string) bool {
	host = normalizeHost(host)
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := HostIPLiteral(host); ip != nil {
		return IsBlockedIP(ip)
	}
	return false
}

// HostIPLiteral parses host as an IP literal, tolerating IPv6 zones and
// bracketed forms; returns nil for hostnames.
func HostIPLiteral(host string) net.IP {
	host = normalizeHost(host)
	if i := strings.LastIndex(host, "%"); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	return net.ParseIP(host)
}

// BlockedTargetError rejects a URL or address that maps to internal or
// special-use network space (policy block, never retryable).
type BlockedTargetError struct {
	Target string
	Reason string
}

// Error ...
func (e *BlockedTargetError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("SSRF blocked: %s", e.Target)
	}
	return fmt.Sprintf("SSRF blocked: %s (%s)", e.Target, e.Reason)
}

// UnverifiableHostError fails closed when a hostname cannot be proven public
// (DNS failure, empty answer). It is a transient classification: callers
// must not record it as a permanent rejection.
type UnverifiableHostError struct {
	Host string
	Err  error
}

// Error ...
func (e *UnverifiableHostError) Error() string {
	return fmt.Sprintf("SSRF unverifiable: hostname %q cannot be proven public: %v", e.Host, e.Err)
}

// Unwrap ...
func (e *UnverifiableHostError) Unwrap() error { return e.Err }

// resolvePublicIPs resolves host and verifies every answer is public. A
// literal IP skips resolution. DNS failure yields *UnverifiableHostError;
// blocked answers yield *BlockedTargetError.
func resolvePublicIPs(host string) ([]net.IP, error) {
	if hostAllowedForTest(host) {
		ips, err := currentLookupIP()(host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			if ip := net.ParseIP(host); ip != nil {
				return []net.IP{ip}, nil
			}
			return nil, fmt.Errorf("no addresses")
		}
		return ips, nil
	}
	if ip := HostIPLiteral(host); ip != nil {
		if IsBlockedIP(ip) {
			return nil, &BlockedTargetError{Target: host, Reason: "private/internal IP literal"}
		}
		return []net.IP{ip}, nil
	}
	ips, err := currentLookupIP()(host)
	if err != nil {
		return nil, &UnverifiableHostError{Host: host, Err: err}
	}
	if len(ips) == 0 {
		return nil, &UnverifiableHostError{Host: host, Err: errors.New("resolved to no addresses")}
	}
	for _, ip := range ips {
		if IsBlockedIP(ip) {
			return nil, &BlockedTargetError{Target: host, Reason: "resolves to private/internal IP"}
		}
	}
	return ips, nil
}

// CheckTarget validates u for outbound fetching: http(s) scheme, non-empty
// hostname, and a publicly resolvable target.
func CheckTarget(ctx context.Context, u *url.URL) error {
	if u == nil {
		return &BlockedTargetError{Target: "<nil>", Reason: "nil URL"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return &BlockedTargetError{Target: u.Scheme, Reason: "non-http(s) scheme"}
	}
	host := u.Hostname()
	if host == "" {
		return &BlockedTargetError{Target: u.String(), Reason: "empty hostname"}
	}
	if IsBlockedHost(host) && !hostAllowedForTest(host) {
		return &BlockedTargetError{Target: host, Reason: "private/internal target"}
	}
	if HostIPLiteral(host) != nil {
		return nil
	}
	_ = ctx // resolution seams do not take a context; the parameter stabilizes the API
	_, err := resolvePublicIPs(host)
	return err
}

// CheckURL validates that rawURL uses an http(s) scheme and does not resolve
// to a private or internal IP address. Kept for compatibility with the
// pre-unification API; new code should use CheckTarget.
func CheckURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	return CheckTarget(context.Background(), parsed)
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if err := CheckTarget(req.Context(), req.URL); err != nil {
		return fmt.Errorf("SSRF blocked: redirect to private/internal IP: %w", err)
	}
	if len(via) >= 10 {
		return fmt.Errorf("SSRF blocked: too many redirects (>10)")
	}
	return nil
}

// dialPinned resolves addr's host once, validates every answer, and dials
// only validated IPs (trying each in order for DNS failover). The hostname
// is never re-resolved at connect time, so DNS rebinding between the check
// and the dial cannot redirect the connection.
func dialPinned(ctx context.Context, network, addr string, fallback func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("SSRF blocked: invalid address %q: %w", addr, err)
	}
	if hostAllowedForTest(host) {
		return fallback(ctx, network, addr)
	}
	ips, err := resolvePublicIPs(host)
	if err != nil {
		return nil, err
	}
	var dialErr error
	for _, ip := range ips {
		conn, derr := fallback(ctx, network, net.JoinHostPort(ip.String(), port))
		if derr == nil {
			return conn, nil
		}
		dialErr = errors.Join(dialErr, derr)
	}
	return nil, dialErr
}

// NewPinnedDialTransport clones base (or http.DefaultTransport when nil) and
// installs a dial function that pins connections to pre-validated public
// IPs. Proxy behavior: whatever is dialed is validated — with a proxy
// configured that is the proxy connection (target policy still applies at
// the URL layer via CheckTarget/redirect checks); without one it is the
// target itself. Fails closed for exotic transports.
func NewPinnedDialTransport(base *http.Transport) (*http.Transport, error) {
	if base == nil {
		t, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, errors.New("SSRF: http.DefaultTransport is not a *http.Transport")
		}
		base = t
	}
	// Fails closed: a custom TLS dialer bypasses DialContext for HTTPS, which
	// would silently skip validation+pinning.
	if base.DialTLSContext != nil || base.DialTLS != nil { //nolint:staticcheck // reading deprecated DialTLS is required to fail closed on transports that would bypass the pinned dial
		return nil, errors.New("SSRF: transports with DialTLSContext/DialTLS cannot be pinned")
	}
	clone := base.Clone()
	fallback := clone.DialContext
	if fallback == nil {
		fallback = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	clone.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialPinned(ctx, network, addr, fallback)
	}
	return clone, nil
}

// NewSSRFSafeClient returns an HTTP client that blocks requests to private or
// internal IP addresses, with pinned dialing and per-redirect revalidation.
// Kept for compatibility; new code should prefer NewPinnedDialTransport.
func NewSSRFSafeClient(timeout time.Duration) *http.Client {
	// Construction cannot fail with a nil base unless http.DefaultTransport is
	// exotic; in that case fall back to a fresh *http.Transport (still pinned),
	// NEVER to an unguarded one.
	base := &http.Transport{}
	if dt, ok := http.DefaultTransport.(*http.Transport); ok && dt.DialTLSContext == nil && dt.DialTLS == nil { //nolint:staticcheck // fail closed on unpinnable defaults
		base = dt
	}
	// base is always pinnable here (fresh or devoid of TLS dialers), so this
	// cannot fail.
	transport, _ := NewPinnedDialTransport(base)
	transport.Proxy = nil
	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: checkRedirect,
	}
}

// WrapTransportWithSSRFCheck installs pinned public-IP dialing on transport
// in place. Kept for compatibility; new code should use
// NewPinnedDialTransport and keep the returned clone.
func WrapTransportWithSSRFCheck(transport *http.Transport) *http.Transport {
	fallback := transport.DialContext
	if fallback == nil {
		fallback = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
	}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialPinned(ctx, network, addr, fallback)
	}
	return transport
}
