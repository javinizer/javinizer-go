package ssrf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

var (
	lookupIPMu sync.RWMutex
	lookupIP   func(context.Context, string) ([]net.IP, error)
)

// setLookupIPForTest overrides the DNS resolver for testing. Returns a cleanup
// function that restores the original resolver.
func setLookupIPForTest(fn func(string) ([]net.IP, error)) func() {
	if fn == nil {
		return setLookupIPContextForTest(nil)
	}
	return setLookupIPContextForTest(func(_ context.Context, host string) ([]net.IP, error) {
		return fn(host)
	})
}

func setLookupIPContextForTest(fn func(context.Context, string) ([]net.IP, error)) func() {
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

func resolvePublicIPs(ctx context.Context, host string) ([]net.IP, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lookupIPMu.RLock()
	lookup := lookupIP
	lookupIPMu.RUnlock()
	var ips []net.IP
	var err error
	if lookup != nil {
		ips, err = lookup(ctx, host)
	} else {
		ips, err = net.DefaultResolver.LookupIP(ctx, "ip", host)
	}
	if err != nil {
		return nil, fmt.Errorf("SSRF blocked: failed to resolve hostname %q: %w", host, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("SSRF blocked: empty DNS answer for %q", host)
	}
	for _, ip := range ips {
		if ip.To16() == nil {
			return nil, fmt.Errorf("SSRF blocked: invalid IP for %q", host)
		}
		if isPrivateIP(ip) {
			return nil, fmt.Errorf("SSRF blocked: %s resolves to private/internal IP %s", host, ip)
		}
	}
	return ips, nil
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	if ip.IsLinkLocalUnicast() {
		return true
	}
	if ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsUnspecified() {
		return true
	}
	return false
}

// CheckURL validates that rawURL uses an http(s) scheme and does not resolve to a private or internal IP address.
func CheckURL(rawURL string) error {
	return checkURL(context.Background(), rawURL)
}

func checkURL(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("SSRF blocked: non-http(s) scheme %q", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("SSRF blocked: empty hostname")
	}
	_, err = resolvePublicIPs(ctx, host)
	return err
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if err := checkURL(req.Context(), req.URL.String()); err != nil {
		return fmt.Errorf("SSRF blocked: redirect to private/internal IP: %w", err)
	}
	if len(via) >= 10 {
		return fmt.Errorf("SSRF blocked: too many redirects (>10)")
	}
	return nil
}

// NewSSRFSafeClient returns an HTTP client that blocks requests to private or internal IP addresses.
func NewSSRFSafeClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	wrapTransportWithSSRFCheck(transport, 30*time.Second)

	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: checkRedirect,
	}
}

// WrapTransportWithSSRFCheck wraps the given transport's dialer to block connections to private or internal IP addresses.
func WrapTransportWithSSRFCheck(transport *http.Transport, dialBudget ...time.Duration) *http.Transport {
	var budget time.Duration
	if len(dialBudget) > 0 {
		budget = dialBudget[0]
	}
	return wrapTransportWithSSRFCheck(transport, budget)
}

func wrapTransportWithSSRFCheck(transport *http.Transport, dialTimeout time.Duration) *http.Transport {
	originalDialContext := transport.DialContext
	if originalDialContext == nil {
		originalDialContext = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
		if dialTimeout <= 0 {
			dialTimeout = 30 * time.Second
		}
	}
	transport.DialContext = pinnedDialerWithTimeout(originalDialContext, dialTimeout)
	//nolint:staticcheck // Inspect the legacy TLS callback to close its HTTPS bypass.
	if transport.DialTLSContext != nil || transport.DialTLS != nil {
		transport.DialTLSContext = func(context.Context, string, string) (net.Conn, error) {
			return nil, fmt.Errorf("SSRF blocked: custom TLS dialers cannot preserve validated endpoint and hostname verification")
		}
		transport.DialTLS = nil //nolint:staticcheck // Disable the legacy TLS bypass.
	}
	return transport
}

func pinnedDialerWithTimeout(dial func(context.Context, string, string) (net.Conn, error), timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("SSRF blocked: invalid address %q: %w", addr, err)
		}
		n, err := strconv.Atoi(port)
		if host == "" || err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("SSRF blocked: invalid address %q", addr)
		}
		if network != "tcp" && network != "tcp4" && network != "tcp6" {
			return nil, fmt.Errorf("SSRF blocked: unsupported network %q", network)
		}
		ips, err := resolvePublicIPs(ctx, host)
		if err != nil {
			return nil, err
		}
		var failures []error
		for i, ip := range ips {
			if network == "tcp4" && ip.To4() == nil || network == "tcp6" && ip.To4() != nil {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			attemptCtx := ctx
			cancelAttempt := func() {}
			if timeout > 0 {
				remaining := 0
				for _, candidate := range ips[i:] {
					if network == "tcp4" && candidate.To4() == nil || network == "tcp6" && candidate.To4() != nil {
						continue
					}
					remaining++
				}
				if deadline, ok := ctx.Deadline(); ok && remaining > 1 {
					attemptCtx, cancelAttempt = context.WithTimeout(ctx, time.Until(deadline)/time.Duration(remaining))
				}
			}
			conn, err := dial(attemptCtx, network, net.JoinHostPort(ip.String(), port))
			cancelAttempt()
			if err == nil {
				return conn, nil
			}
			failures = append(failures, err)
			if timeout == 0 {
				return nil, err
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(failures) == 0 {
			return nil, fmt.Errorf("SSRF blocked: no validated addresses for network %q", network)
		}
		return nil, errors.Join(failures...)
	}
}
