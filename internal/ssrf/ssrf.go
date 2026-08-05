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
	lookupIP   = func(ctx context.Context, host string) ([]net.IP, error) {
		return net.DefaultResolver.LookupIP(ctx, "ip", host)
	}
)

// testAllowedHosts are exact hostnames the guard bypasses for tests.
// Production code must never call AllowHostForTest.
var testAllowedHosts sync.Map

func setLookupIPForTest(fn func(string) ([]net.IP, error)) func() {
	lookupIPMu.Lock()
	defer lookupIPMu.Unlock()
	original := lookupIP
	lookupIP = func(_ context.Context, host string) ([]net.IP, error) { return fn(host) }
	return func() {
		lookupIPMu.Lock()
		defer lookupIPMu.Unlock()
		lookupIP = original
	}
}

func currentLookupIP() func(context.Context, string) ([]net.IP, error) {
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
func resolvePublicIPs(ctx context.Context, host string) ([]net.IP, error) {
	if hostAllowedForTest(host) {
		ips, err := currentLookupIP()(ctx, host)
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
	ips, err := currentLookupIP()(ctx, host)
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
	_, err := resolvePublicIPs(ctx, host)
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
func dialPinned(ctx context.Context, network, addr string, fallback func(context.Context, string, string) (net.Conn, error), preserveHostname bool) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("SSRF blocked: invalid address %q: %w", addr, err)
	}
	if hostAllowedForTest(host) {
		return fallback(ctx, network, addr)
	}
	if preserveHostname {
		// Remote-DNS dialers (SOCKS5) own NAME resolution completely: the
		// hostname goes to the proxy unresolved so proxy-only names and
		// split-horizon answers work. IP LITERALS need no DNS at all, though,
		// so private/internal literals stay blocked here regardless of who
		// would have resolved them.
		if ip := HostIPLiteral(host); ip != nil && IsBlockedIP(ip) {
			return nil, &BlockedTargetError{Target: host, Reason: "private/internal IP literal"}
		}
		return fallback(ctx, network, addr)
	}
	ips, err := resolvePublicIPs(ctx, host)
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
	// Honor a legacy-only Dial hook: assigning DialContext while ignoring it
	// would silently discard the caller's configured dialer.
	fallback := DialContextFunc(clone)
	// NewPinnedDialTransport keeps IP-pinning semantics even when the original
	// had a proxy (the proxy itself gets pinned) — its callers are hardened
	// wrapper paths, not SOCKS-compat ones.
	clone.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialPinned(ctx, network, addr, fallback, false)
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
//
// A custom TLS dialer would bypass the pinned DialContext for HTTPS, so any
// DialTLS/DialTLSContext on the transport is cleared here rather than
// silently left to defeat the guard.
func WrapTransportWithSSRFCheck(transport *http.Transport) *http.Transport {
	// Pin by default even when Proxy is set: net/http dials the PROXY address,
	// which we resolve+validate; dialing the original hostname afterwards
	// would re-resolve it and allow DNS rebinding onto a private address.
	// Only explicit remote-DNS paths (WrapTransportPreservingHostnames, used
	// for SOCKS5 DialContext transports) may keep the hostname.
	return wrapTransport(transport, false)
}

// remoteDNSTransports tracks transports whose dial path owns hostname
// resolution (SOCKS5 via x/net/proxy installs DialContext while
// http.Transport.Proxy stays nil, so policy code cannot rely on Proxy != nil
// or on named-func method detection -- Go erases either at assignment).
// Bounded with FIFO eviction: short-lived transports (admin proxy-test
// clicks recreate one per attempt) must not grow process memory forever.
const maxRemoteDNSTransports = 256

var remoteDNSRegistry = struct {
	sync.Mutex
	set   map[*http.Transport]struct{}
	order []*http.Transport
}{set: make(map[*http.Transport]struct{})}

// MarkRemoteDNSTransport declares that tr's dial path resolves names
// remotely. Wrappers must preserve hostnames (never locally pin), while
// still blocking private IP literals. Re-marking the same pointer is a no-op.
func MarkRemoteDNSTransport(tr *http.Transport) {
	if tr == nil {
		return
	}
	remoteDNSRegistry.Lock()
	defer remoteDNSRegistry.Unlock()
	if _, exists := remoteDNSRegistry.set[tr]; exists {
		return
	}
	for len(remoteDNSRegistry.order) >= maxRemoteDNSTransports {
		oldest := remoteDNSRegistry.order[0]
		remoteDNSRegistry.order = remoteDNSRegistry.order[1:]
		delete(remoteDNSRegistry.set, oldest)
	}
	remoteDNSRegistry.set[tr] = struct{}{}
	remoteDNSRegistry.order = append(remoteDNSRegistry.order, tr)
}

// TransportResolvesRemotely reports whether tr's dial path owns DNS. Check
// BEFORE cloning a transport -- clones are distinct pointers.
func TransportResolvesRemotely(tr *http.Transport) bool {
	if tr == nil {
		return false
	}
	remoteDNSRegistry.Lock()
	defer remoteDNSRegistry.Unlock()
	_, ok := remoteDNSRegistry.set[tr]
	return ok
}

// DialContextFunc returns the transport's effective dial entry point:
// DialContext when set; otherwise a context-wrapped adaptation of the
// deprecated Dial hook; otherwise a default dialer. Wrappers must use this
// instead of checking DialContext alone -- installing a DialContext while
// ignoring a legacy Dial makes net/http silently discard the caller's dialer.
func DialContextFunc(transport *http.Transport) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if transport.DialContext != nil {
		return transport.DialContext
	}
	if transport.Dial != nil { //nolint:staticcheck // reading the deprecated hook is required to respect it
		legacy := transport.Dial //nolint:staticcheck // honored on purpose
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			type outcome struct {
				conn net.Conn
				err  error
			}
			result := make(chan outcome)
			abandoned := make(chan struct{})
			go func() {
				conn, err := legacy(network, addr)
				select {
				case result <- outcome{conn, err}:
				case <-abandoned:
					// The caller already gave up: a late-arriving connection
					// must be closed by the owner that created it, not leaked.
					if conn != nil {
						_ = conn.Close()
					}
				}
			}()
			select {
			case o := <-result:
				return o.conn, o.err
			case <-ctx.Done():
				// Legacy Dial cannot be canceled: abandon, and let the goroutine
				// close whatever eventually arrives.
				close(abandoned)
				return nil, ctx.Err()
			}
		}
	}
	return (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
}

// WrapTransportPreservingHostnames is WrapTransportWithSSRFCheck for dial
// paths whose hostname resolution happens remotely (SOCKS5 via x/net/proxy
// installs DialContext while http.Transport.Proxy stays nil). Without this
// the wrapper would pin to a locally resolved IP and defeat SOCKS5 remote-DNS
// and split-horizon semantics.
func WrapTransportPreservingHostnames(transport *http.Transport) *http.Transport {
	return wrapTransport(transport, true)
}

func wrapTransport(transport *http.Transport, preserveHostnames bool) *http.Transport {
	transport.DialTLSContext = nil
	transport.DialTLS = nil //nolint:staticcheck // cleared intentionally: unpinnable
	fallback := DialContextFunc(transport)
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialPinned(ctx, network, addr, fallback, preserveHostnames)
	}
	return transport
}
