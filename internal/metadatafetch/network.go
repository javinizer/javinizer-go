package metadatafetch

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/ssrf"
	"golang.org/x/net/proxy"
)

func parseProxy(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "socks5" || u.Hostname() == "" || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(u.Hostname(), "%") {
		return nil, ErrOptions
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, ErrOptions
	}
	if u.User != nil {
		password, _ := u.User.Password()
		if len(u.User.Username()) < 1 || len(u.User.Username()) > 255 || len(password) > 255 {
			return nil, ErrOptions
		}
	}
	return u, nil
}

func (c *Client) roundTrip(parent context.Context, target *url.URL) (*http.Response, func(), error) {
	ctx, cancelRequest := context.WithCancel(parent)
	owner := &connectionOwner{cancel: cancelRequest}
	stopClose := context.AfterFunc(parent, owner.close)
	var transport *http.Transport
	var response *http.Response
	cleanup := func() {
		stopClose()
		owner.close()
		if response != nil {
			_ = response.Body.Close()
		}
		if transport != nil {
			transport.CloseIdleConnections()
		}
	}
	transferred := false
	defer func() {
		if !transferred {
			cleanup()
		}
	}()
	address, err := targetAddress(target)
	if err != nil {
		return nil, nil, err
	}
	// Keep a single budget for target DNS, proxy DNS and all candidate connects.
	connectCtx, cancel := context.WithTimeout(ctx, c.options.Limits.ConnectTimeout)
	defer cancel()
	ips, err := c.publicIPs(connectCtx, strings.ToLower(target.Hostname()))
	if err != nil {
		return nil, nil, err
	}
	dial := c.dial
	if c.proxy != nil {
		proxyIPs, err := c.publicIPs(connectCtx, c.proxy.Hostname())
		if err != nil {
			return nil, nil, err
		}
		// Proxy DNS is independently validated and pinned. A configured proxy
		// endpoint never makes a private target valid.
		forward := contextDialer{dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialCandidates(ctx, network, c.proxy.Port(), proxyIPs, c.dial)
		}}
		var auth *proxy.Auth
		if c.proxy.User != nil {
			password, _ := c.proxy.User.Password()
			auth = &proxy.Auth{User: c.proxy.User.Username(), Password: password}
		}
		proxyDialer, err := proxy.SOCKS5("tcp", c.proxy.Host, auth, forward)
		if err != nil {
			return nil, nil, ErrConnect
		}
		contextProxy, ok := proxyDialer.(proxy.ContextDialer)
		if !ok {
			return nil, nil, ErrConnect
		}
		dial = contextProxy.DialContext
	}
	_, port, _ := net.SplitHostPort(address)
	transport = &http.Transport{
		// A fresh transport per hop and no keepalives rule out reuse that could
		// bypass the next hop's complete DNS validation. Proxy=nil ignores env.
		DisableKeepAlives: true, DisableCompression: true,
		MaxConnsPerHost: 1, MaxResponseHeaderBytes: c.options.Limits.MaxHeaderBytes,
		ResponseHeaderTimeout: c.options.Limits.HeaderTimeout,
		TLSHandshakeTimeout:   c.options.Limits.TLSHandshakeTimeout,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: c.rootCAs},
		DialContext: func(_ context.Context, network, requested string) (net.Conn, error) {
			requestedAddress, addressErr := targetAddress(&url.URL{Scheme: target.Scheme, Host: requested})
			if addressErr != nil || requestedAddress != address {
				return nil, ErrTarget
			}
			return owner.connect(connectCtx, network, port, ips, dial)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, nil, ErrURL
	}
	// Request.Close is required in addition to DisableKeepAlives: Go 1.27
	// may otherwise drain an early-closed response body in the background even
	// when the transport will not reuse the connection. Do not trust the peer
	// to honor the outbound Connection header.
	req.Close = true
	req.Header.Set("Accept-Encoding", "identity")
	// RoundTrip performs no automatic redirect processing, cookie handling,
	// referer forwarding or retries through another routing mechanism.
	response, err = transport.RoundTrip(req)
	if err != nil {
		return nil, nil, err
	}
	transferred = true
	return response, cleanup, nil
}

func (c *Client) publicIPs(ctx context.Context, host string) ([]net.IP, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		var err error
		ips, err = c.resolve(ctx, host)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrTarget
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, ErrTarget
	}
	owned := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		// Reuse the shared SSRF policy on literal-only URLs: LookupIP on a
		// literal never queries DNS. Reject non-unicast and shared-carrier space
		// additionally, without changing any legacy scraper's network policy.
		if ip.To16() == nil || !ip.IsGlobalUnicast() || carrierShared(ip) || ssrf.CheckURL("http://"+net.JoinHostPort(ip.String(), "80")) != nil {
			return nil, ErrTarget
		}
		owned = append(owned, append(net.IP(nil), ip...))
	}
	return owned, nil
}

func carrierShared(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64
}

func dialCandidates(ctx context.Context, network, port string, ips []net.IP, dial dialer) (net.Conn, error) {
	// Reuse shared validated literal pinning at the actual direct/SOCKS dial
	// boundary as well. It receives only a literal, never the original hostname.
	transport := ssrf.WrapTransportWithSSRFCheck(&http.Transport{DialContext: dial})
	var lastErr error
	for i, ip := range ips {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx := ctx
		cancel := func() {}
		if deadline, ok := ctx.Deadline(); ok && len(ips)-i > 1 {
			attemptCtx, cancel = context.WithTimeout(ctx, time.Until(deadline)/time.Duration(len(ips)-i))
		}
		conn, err := transport.DialContext(attemptCtx, network, net.JoinHostPort(ip.String(), port))
		cancel()
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A socket deadline (notably during SOCKS negotiation) can fire just
	// before the context timer. Preserve its safe timeout category as well.
	var timeout net.Error
	if errors.Is(lastErr, context.DeadlineExceeded) || errors.As(lastErr, &timeout) && timeout.Timeout() {
		return nil, context.DeadlineExceeded
	}
	return nil, ErrConnect
}

// contextDialer ensures SOCKS uses the context-aware path, not a goroutine
// around a legacy Dial which could escape the request's cancellation budget.
type contextDialer struct{ dial dialer }

func (d contextDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.dial(ctx, network, address)
}

func (d contextDialer) Dial(string, string) (net.Conn, error) { return nil, ErrConnect }
