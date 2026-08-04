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
	fetcher := NewFetcher(&http.Client{Transport: &http.Transport{}}, 0, "test")
	// Positive path: hostname resolved, all answers public -> allow.
	require.NoError(t, fetcher.checkFetchTarget(context.Background(), "https", "public.example.test"))
}

func TestCheckFetchTargetSkipsResolutionForCustomTransport(t *testing.T) {
	lookupCalls := 0
	prev := lookupIP
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		lookupCalls++
		return nil, errors.New("should not be called")
	}
	defer func() { lookupIP = prev }()
	// A non-*http.Transport (custom RoundTripper) owns its own connections:
	// the guard stays lexical and never resolves.
	fetcher := NewFetcher(&http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}, 0, "test")
	require.NoError(t, fetcher.checkFetchTarget(context.Background(), "https", "unyielding.invalid"))
	assert.Zero(t, lookupCalls)
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
	client := &http.Client{Transport: fetchTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"http://127.0.0.1/private"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	fetcher := NewFetcher(client, 0, "test")
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
