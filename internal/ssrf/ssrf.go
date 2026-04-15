package ssrf

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

var ErrSSRFBlocked = fmt.Errorf("SSRF blocked: URL resolves to private/internal IP")

var lookupIP = net.LookupIP

func SetLookupIPForTest(fn func(string) ([]net.IP, error)) func() {
	orig := lookupIP
	lookupIP = fn
	return func() { lookupIP = orig }
}

func IsPrivateIP(ip net.IP) bool {
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
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}
	if ip.IsUnspecified() {
		return true
	}
	return false
}

func CheckURL(rawURL string) error {
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
	ips, err := lookupIP(host)
	if err != nil {
		return fmt.Errorf("SSRF blocked: failed to resolve hostname %q: %w", host, err)
	}
	for _, ip := range ips {
		if IsPrivateIP(ip) {
			return fmt.Errorf("SSRF blocked: %s resolves to private/internal IP", host)
		}
	}
	return nil
}

func CheckRedirect(req *http.Request, via []*http.Request) error {
	if err := CheckURL(req.URL.String()); err != nil {
		return fmt.Errorf("SSRF blocked: redirect to private/internal IP: %w", err)
	}
	if len(via) >= 10 {
		return fmt.Errorf("SSRF blocked: too many redirects (>10)")
	}
	return nil
}

func NewSSRFSafeClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	originalDialContext := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("SSRF blocked: invalid address %q: %w", addr, err)
		}
		ips, err := lookupIP(host)
		if err != nil {
			return nil, fmt.Errorf("SSRF blocked: failed to resolve %q: %w", host, err)
		}
		for _, ip := range ips {
			if IsPrivateIP(ip) {
				return nil, fmt.Errorf("SSRF blocked: %s resolves to private/internal IP %s", host, ip)
			}
		}
		if originalDialContext != nil {
			return originalDialContext(ctx, network, addr)
		}
		return (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, network, addr)
	}

	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: CheckRedirect,
	}
}

func WrapTransportWithSSRFCheck(transport *http.Transport) *http.Transport {
	originalDialContext := transport.DialContext
	if originalDialContext == nil {
		originalDialContext = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
	}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("SSRF blocked: invalid address %q: %w", addr, err)
		}
		ips, err := lookupIP(host)
		if err != nil {
			return nil, fmt.Errorf("SSRF blocked: failed to resolve %q: %w", host, err)
		}
		for _, ip := range ips {
			if IsPrivateIP(ip) {
				return nil, fmt.Errorf("SSRF blocked: %s resolves to private/internal IP %s", host, ip)
			}
		}
		return originalDialContext(ctx, network, addr)
	}
	return transport
}
