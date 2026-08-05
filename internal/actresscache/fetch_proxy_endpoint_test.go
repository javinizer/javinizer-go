package actresscache

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The proxy dial exemption must key on the exact host:port endpoint. A
// directly-routed target that merely SHARES the proxy's hostname (a NO_PROXY
// rule for it, or DNS rebinding past the request preflight) must still pass
// through the guarded, pinned dial.
func TestProxyEndpointExemptionIsExactHostPort(t *testing.T) {
	prevLookup := lookupIP
	defer func() { lookupIP = prevLookup }()
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.99")}, nil // guard must reject
	}

	var dialed []string
	sentinel := errors.New("reached raw proxy endpoint")
	record := func(_ context.Context, _, addr string) (net.Conn, error) {
		dialed = append(dialed, addr)
		return nil, sentinel
	}
	proxyURL, err := url.Parse("http://proxy.example:3128")
	require.NoError(t, err)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DialContext: record}
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	pinning, ok := fetcher.client.Transport.(*proxyPinningTransport)
	require.True(t, ok, "proxied fetch clients sit behind the pinning transport")
	guarded, ok := pinning.base.(*http.Transport)
	require.True(t, ok)
	dial := guarded.DialContext
	require.NotNil(t, dial)

	// Exact proxy endpoint: raw fallback, hostname preserved for the proxy.
	_, err = dial(context.Background(), "tcp", "proxy.example:3128")
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, []string{"proxy.example:3128"}, dialed)

	// Same hostname, different port: NOT the proxy endpoint -- the guard
	// resolves + validates (and here blocks the private answer).
	_, err = dial(context.Background(), "tcp", "proxy.example:8080")
	require.Error(t, err)
	assert.NotErrorIs(t, err, sentinel)
	assert.Len(t, dialed, 1, "non-endpoint dials must never reach the raw fallback")
}

func TestProxyEndpointExemptsSchemeDefaultPort(t *testing.T) {
	var dialed []string
	sentinel := errors.New("reached raw proxy endpoint")
	transport := &http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "proxy-default.example"}),
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			return nil, sentinel
		},
	}
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	dial := fetcher.client.Transport.(*proxyPinningTransport).base.(*http.Transport).DialContext
	_, err = dial(context.Background(), "tcp", "proxy-default.example:80")
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, []string{"proxy-default.example:80"}, dialed)
}

func TestProxyEndpointExemptsHTTPSDefaultPort(t *testing.T) {
	sentinel := errors.New("reached raw proxy endpoint")
	transport := &http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "https", Host: "secure-proxy.example"}),
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			return nil, sentinel
		},
	}
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	dial := fetcher.client.Transport.(*proxyPinningTransport).base.(*http.Transport).DialContext
	_, err = dial(context.Background(), "tcp", "secure-proxy.example:443")
	require.ErrorIs(t, err, sentinel, "https proxy with no explicit port exempts the :443 endpoint")
}
