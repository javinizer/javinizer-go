package actresscache

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckFetchTargetAllowsResolvedPublicHost(t *testing.T) {
	prev := lookupIP
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	defer func() { lookupIP = prev }()
	fetcher := mustFetcher(NewFetcher(&http.Client{Transport: &http.Transport{}}, 0, "test"))
	// Positive path: hostname resolved, all answers public -> allow.
	require.NoError(t, fetcher.checkFetchTarget(context.Background(), "https", "public.example.test"))
}

func TestNewFetcherRejectsCustomRoundTripper(t *testing.T) {
	// A wrapped RoundTripper dials invisibly; the fetcher must refuse it when
	// egress pinning is mandatory instead of silently downgrading the guard.
	_, err := NewFetcher(&http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}, 0, "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "*http.Transport")

	// Trusted-mirror opt-in accepts the custom transport untouched.
	fetcher, err2 := NewFetcherWithOptions(&http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}, 0, "test", nil, true)
	require.NoError(t, err2)
	// No resolution is promised for mirror mode: the guard stays lexical-only
	// and the caller's transport is used verbatim.
	assert.False(t, fetcher.resolveTargets)
	require.NoError(t, fetcher.checkFetchTarget(context.Background(), "https", "unyielding.invalid"))
}

func TestViaProxyBranches(t *testing.T) {
	bare := &Fetcher{}
	assert.False(t, bare.viaProxy("https", "example.test"), "nil proxyFunc means direct")
	assert.False(t, bare.viaProxy("", "example.test"), "empty scheme defaults physically, still direct")
	noProxy := &Fetcher{proxyFunc: func(*http.Request) (*url.URL, error) { return nil, nil }}
	assert.False(t, noProxy.viaProxy("https", "example.test"), "proxy func returning nil means direct")
	proxy := &Fetcher{proxyFunc: func(*http.Request) (*url.URL, error) {
		u, _ := url.Parse("http://corp.proxy:3128")
		return u, nil
	}}
	assert.True(t, proxy.viaProxy("https", "example.test"))
}

func TestFetcherGetDoesNotRetryPolicyErrors(t *testing.T) {
	calls := 0
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		calls++
		return serveOnce("HTTP/1.1 302 Found\r\nLocation: http://127.0.0.1/private\r\nContent-Length: 0\r\n\r\n")(ctx, network, addr)
	}
	client := &http.Client{Transport: &http.Transport{DialContext: dial}}
	fetcher := mustFetcher(NewFetcher(client, 0, "test"))
	_, _, err := fetcher.Get(context.Background(), "http://1.1.1.1/start", "*/*", 1024)
	require.Error(t, err)
	var blocked *BlockedFetchError
	require.True(t, errors.As(err, &blocked), "expected typed BlockedFetchError, got %v", err)
	assert.Equal(t, 1, calls, "policy errors must not burn retry attempts")
}

func TestViaProxyEmptySchemeWithProxy(t *testing.T) {
	proxy := &Fetcher{proxyFunc: func(*http.Request) (*url.URL, error) {
		u, _ := url.Parse("http://corp.proxy:3128")
		return u, nil
	}}
	assert.True(t, proxy.viaProxy("", "example.test"), "empty scheme must default to https and probe the proxy")
}
