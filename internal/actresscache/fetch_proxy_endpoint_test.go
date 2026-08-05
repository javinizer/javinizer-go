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

// The proxied-request exemption follows the request's chosen proxy endpoint:
// net/http dials the proxy (canonical scheme default port included) with the
// marker, and only that dial skips the guard.
func TestProxyDialExemptionFollowsTheProxiedRequest(t *testing.T) {
	sentinel := errors.New("reached raw proxy endpoint")
	var dialed []string
	transport := &http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "proxy.example"}),
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			return nil, sentinel
		},
	}
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	_, err = fetcher.client.Get("https://img.example/thumb.jpg")
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, []string{"proxy.example:80"}, dialed, "proxied dial reaches the canonical proxy endpoint raw")
}

// A dial to the SAME authority without the request marker (direct routing
// via NO_PROXY, or rebinding past preflight) must stay guarded.
func TestDialWithoutRequestMarkerStaysGuarded(t *testing.T) {
	prevLookup := lookupIP
	defer func() { lookupIP = prevLookup }()
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.99")}, nil
	}
	sentinel := errors.New("raw fallback leaked without marker")
	var dialed []string
	record := func(_ context.Context, _, addr string) (net.Conn, error) {
		dialed = append(dialed, addr)
		return nil, sentinel
	}
	transport := &http.Transport{
		Proxy:       http.ProxyURL(&url.URL{Scheme: "http", Host: "proxy.example:3128"}),
		DialContext: record,
	}
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	inner := fetcher.client.Transport.(*proxyPinningTransport).base.(*http.Transport)

	_, err = inner.DialContext(context.Background(), "tcp", "proxy.example:3128")
	require.Error(t, err)
	assert.NotErrorIs(t, err, sentinel)
	assert.Empty(t, dialed, "markerless dial must never reach the raw fallback")
}

// Targets routed directly (proxy func returns nil for them) are not stamped:
// the dial resolves locally, validates, and pins.
func TestDirectTargetIsNotStampedAndStaysPinned(t *testing.T) {
	prevLookup := lookupIP
	defer func() { lookupIP = prevLookup }()
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	sentinel := errors.New("dialed")
	var dialed []string
	noProxy := func(req *http.Request) (*url.URL, error) {
		if req.URL.Hostname() == "direct.example" {
			return nil, nil
		}
		return &url.URL{Scheme: "http", Host: "proxy.example:3128"}, nil
	}
	transport := &http.Transport{
		Proxy: noProxy,
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			return nil, sentinel
		},
	}
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	_, err = fetcher.client.Get("https://direct.example/thumb.jpg")
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, []string{"93.184.216.34:443"}, dialed, "direct target is pinned to the validated IP")
}
