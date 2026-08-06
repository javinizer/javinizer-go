package ssrf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// IsSOCKSProxyURL reports whether u selects native socks5/socks5h proxying
// (net/http routes it over DialContext; the SOCKS endpoint owns target DNS).
func IsSOCKSProxyURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	s := strings.ToLower(u.Scheme)
	return s == "socks5" || s == "socks5h"
}

// CanonicalProxyEndpoint renders the dial target net/http will use for the
// proxy: lowercase host and the scheme default port filled in.
func CanonicalProxyEndpoint(proxyURL *url.URL) string {
	host := strings.ToLower(strings.TrimSpace(proxyURL.Hostname()))
	port := proxyURL.Port()
	if port == "" {
		switch strings.ToLower(proxyURL.Scheme) {
		case "https":
			port = "443"
		case "socks5", "socks5h":
			port = "1080"
		default:
			port = "80"
		}
	}
	return net.JoinHostPort(host, port)
}

// NativeSOCKSPinnedDial builds the dial hook for a transport using NATIVE
// socks proxying: calls naming the proxy endpoint resolve it once and dial
// pinned answers with failover (no DNS re-evaluation at connect time so
// rebinding cannot move the proxy connection). Anything else (a DIRECT hop
// under an env policy) passes through untouched -- target-level private
// literals stay blocked.
func NativeSOCKSPinnedDial(proxyURL *url.URL, lookup func(context.Context, string) ([]net.IPAddr, error), fallback func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	endpoint := CanonicalProxyEndpoint(proxyURL)
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("SSRF blocked: invalid address %q: %w", addr, err)
		}
		if ip := HostIPLiteral(host); ip != nil && IsBlockedIP(ip) {
			return nil, &BlockedTargetError{Target: host, Reason: "private/internal IP literal"}
		}
		// Only the exact proxy endpoint canonical authority runs the pin lane;
		// everything else (direct routes, non-proxy hops) passes untouched.
		if !strings.EqualFold(addr, endpoint) {
			return fallback(ctx, network, addr)
		}
		if ip := net.ParseIP(host); ip != nil {
			return fallback(ctx, network, addr)
		}
		if lookup == nil {
			lookup = net.DefaultResolver.LookupIPAddr
		}
		addrs, lerr := lookup(ctx, host)
		if lerr != nil {
			return nil, fmt.Errorf("resolve configured socks proxy %s: %w", host, lerr)
		}
		if len(addrs) == 0 {
			return nil, fmt.Errorf("resolve configured socks proxy %s: no addresses", host)
		}
		var dialErr error
		for _, candidate := range addrs {
			conn, derr := fallback(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if derr == nil {
				return conn, nil
			}
			dialErr = errors.Join(dialErr, derr)
		}
		return nil, dialErr
	}
}
