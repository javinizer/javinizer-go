package httpclient

import (
	// pinnedSocksForward defined below
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"golang.org/x/net/proxy"
)

// SanitizeProxyURL removes credentials from proxy URL for safe logging
func SanitizeProxyURL(proxyURL string) string {
	u, err := url.Parse(normalizeProxyURL(proxyURL))
	if err != nil {
		return proxyURL // Return as-is if unparseable
	}
	if u.User != nil {
		// Replace user info with [REDACTED]
		u.User = url.User("[REDACTED]")
	}
	return u.String()
}

func normalizeProxyURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "://") {
		return trimmed
	}
	// Allow host:port inputs by defaulting to HTTP proxy scheme.
	return "http://" + trimmed
}

// NewTransport creates an http.Transport with optional proxy support
func NewTransport(proxyProfile *models.ProxyProfile) (*http.Transport, error) {
	if proxyProfile != nil && proxyProfile.URL != "" {
		logging.Debugf("HTTPClient: Creating transport with proxy: %s", SanitizeProxyURL(proxyProfile.URL))
	} else {
		logging.Debugf("HTTPClient: Creating transport without proxy")
	}

	// Clone default transport to preserve Go's safety timeouts
	// (DialContext timeout, TLSHandshakeTimeout, ExpectContinueTimeout, etc.)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Enforce config-only proxy behavior: never inherit HTTP(S)_PROXY from environment.
	transport.Proxy = nil

	if proxyProfile != nil && proxyProfile.URL != "" {
		parsedProxyURL, err := url.Parse(normalizeProxyURL(proxyProfile.URL))
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}

		// Handle authentication
		if proxyProfile.Username != "" && proxyProfile.Password != "" {
			parsedProxyURL.User = url.UserPassword(proxyProfile.Username, proxyProfile.Password)
		}

		// Check if SOCKS5
		if parsedProxyURL.Scheme == "socks5" {
			// Use golang.org/x/net/proxy for SOCKS5
			var auth *proxy.Auth
			if proxyProfile.Username != "" && proxyProfile.Password != "" {
				auth = &proxy.Auth{
					User:     proxyProfile.Username,
					Password: proxyProfile.Password,
				}
			}
			// Pin the SOCKS proxy endpoint at the x/net dialer level: the SOCKS5
			// dialer calls this forward to connect to the proxy server. Resolving
			// once + dialing pinned IPs prevents DNS rebinding from redirecting
			// proxy credentials to an unintended address.
			forward := &pinnedSocksForward{proxyURL: parsedProxyURL}
			dialer, err := proxy.SOCKS5("tcp", parsedProxyURL.Host, auth, forward)
			if err != nil {
				return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
			}
			// Use DialContext to honor request cancellation and timeouts
			// Check if dialer supports DialContext (it does - proxy.Dialer implements ContextDialer)
			if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
				transport.DialContext = contextDialer.DialContext
			} else {
				// Fallback: wrap Dial with context (shouldn't happen with proxy.SOCKS5)
				transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialer.Dial(network, addr)
				}
			}
			// Clear transport.Proxy to prevent HTTP_PROXY env vars from overriding SOCKS5
			transport.Proxy = nil
			// Transport.Proxy is nil yet the dialer owns DNS: announce remote-DNS
			// ownership so SSRF wrappers preserve hostnames (policy layers cannot
			// detect this structurally -- Go erases named func types at field
			// assignment).
			ssrf.MarkRemoteDNSTransport(transport)
		} else {
			// HTTP/HTTPS proxy
			transport.Proxy = http.ProxyURL(parsedProxyURL)
		}
	}

	return transport, nil
}

// NewHTTPClient creates a standard http.Client with proxy support
func NewHTTPClient(proxyProfile *models.ProxyProfile, timeout time.Duration) (*http.Client, error) {
	transport, err := NewTransport(proxyProfile)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}, nil
}

// NewRestyClient creates a resty.Client with proxy support
func NewRestyClient(proxyProfile *models.ProxyProfile, timeout time.Duration, retries int) (*resty.Client, error) {
	if proxyProfile != nil && proxyProfile.URL != "" {
		logging.Debugf("HTTPClient: Creating Resty client with proxy: %s", SanitizeProxyURL(proxyProfile.URL))
	} else {
		logging.Debugf("HTTPClient: Creating Resty client without proxy")
	}

	transport, err := NewTransport(proxyProfile)
	if err != nil {
		return nil, err
	}

	client := resty.New()
	client.SetTimeout(timeout)
	client.SetRetryCount(retries)
	client.SetTransport(transport)

	return client, nil
}

// NewRestyClientNoProxy creates a resty.Client that explicitly bypasses
// environment proxy variables by using a no-proxy transport.
func NewRestyClientNoProxy(timeout time.Duration, retries int) *resty.Client {
	client := resty.New()
	client.SetTimeout(timeout)
	client.SetRetryCount(retries)

	transport, err := NewTransport(nil)
	if err != nil {
		logging.Warnf("HTTP client: failed to create explicit no-proxy transport, using Resty default transport: %v", err)
		return client
	}

	client.SetTransport(transport)
	return client
}

// NewRestyClientWithFlareSolverr creates a resty.Client with optional FlareSolverr support
// Note: FlareSolverr config is passed separately since it's at ScrapersConfig.FlareSolverr (top-level),
// not inside ProxyConfig (which only holds proxy settings).
// Per N-8: returns *ScraperClientResult instead of 3-value tuple.
func NewRestyClientWithFlareSolverr(proxyProfile *models.ProxyProfile, flaresolverrCfg models.FlareSolverrConfig, timeout time.Duration, retries int) (*ScraperClientResult, error) {
	client, err := NewRestyClient(proxyProfile, timeout, retries)
	if err != nil {
		return nil, err
	}

	// If no flaresolverr config or disabled, return plain client
	if !flaresolverrCfg.Enabled {
		return &ScraperClientResult{
			Client:       client,
			FlareSolverr: nil,
		}, nil
	}

	// If FlareSolverr is enabled, create a client
	fs, err := newFlareSolverr(&flaresolverrCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create FlareSolverr client: %w", err)
	}
	fs.requestProxy = buildFlareSolverrRequestProxy(proxyProfile)
	if fs.requestProxy != nil {
		logging.Infof("FlareSolverr request proxy enabled: %s", SanitizeProxyURL(fs.requestProxy.URL))
	}
	logging.Infof("FlareSolverr enabled at %s", flaresolverrCfg.URL)

	return &ScraperClientResult{
		Client:       client,
		FlareSolverr: fs,
	}, nil
}

// pinnedSocksForward is the underlying dialer the x/net SOCKS5 client uses
// to connect to the proxy server itself. It resolves the proxy endpoint
// ONCE and dials the pinned IP (with failover) so DNS rebinding cannot
// redirect the proxy connection -- and its credentials -- elsewhere.
type pinnedSocksForward struct {
	proxyURL *url.URL
}

func (f *pinnedSocksForward) Dial(network, addr string) (net.Conn, error) {
	return f.DialContext(context.Background(), network, addr)
}

func (f *pinnedSocksForward) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	return ssrf.NativeSOCKSPinnedDial(f.proxyURL, nil, dialer)(ctx, network, addr)
}
