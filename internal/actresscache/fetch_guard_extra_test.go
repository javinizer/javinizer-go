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
	reqPub := &http.Request{URL: &url.URL{Scheme: "https", Host: "public.example.test"}}
	require.NoError(t, fetcher.checkFetchTarget(context.Background(), reqPub))
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
	reqUn := &http.Request{URL: &url.URL{Scheme: "https", Host: "unyielding.invalid"}}
	require.NoError(t, fetcher.checkFetchTarget(context.Background(), reqUn))
}

func TestRequestProxiedEvaluatesTheActualRequest(t *testing.T) {
	var probed *http.Request
	proxy := &Fetcher{proxyFunc: func(req *http.Request) (*url.URL, error) {
		probed = req
		return &url.URL{Scheme: "http", Host: "corp.proxy:3128"}, nil
	}}
	req, err := http.NewRequest(http.MethodGet, "http://target.example:8080/x", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "cache-builder")
	require.NotNil(t, proxy.proxyDecisionFor(req))
	// The SAME request object is probed: header-keyed policies (and
	// port-sensitive NO_PROXY rules) decide with full fidelity.
	require.Same(t, req, probed)
	assert.Equal(t, "target.example:8080", probed.URL.Host)
	assert.Equal(t, "cache-builder", probed.Header.Get("User-Agent"))
}

func TestRequestProxiedBranches(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.test/x", nil)
	require.NoError(t, err)

	bare := &Fetcher{}
	assert.Nil(t, bare.proxyDecisionFor(req), "nil proxyFunc means direct")

	noProxy := &Fetcher{proxyFunc: func(*http.Request) (*url.URL, error) { return nil, nil }}
	assert.Nil(t, noProxy.proxyDecisionFor(req), "proxy func returning nil means direct")

	errProxy := &Fetcher{proxyFunc: func(*http.Request) (*url.URL, error) { return nil, errors.New("conf broken") }}
	assert.Nil(t, errProxy.proxyDecisionFor(req), "decision errors stay direct here; RoundTrip fails closed")

	proxy := &Fetcher{proxyFunc: func(*http.Request) (*url.URL, error) {
		u, _ := url.Parse("http://corp.proxy:3128")
		return u, nil
	}}
	assert.NotNil(t, proxy.proxyDecisionFor(req))
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

func TestRequestProxiedWithProxyMatchesAnyScheme(t *testing.T) {
	proxy := &Fetcher{proxyFunc: func(*http.Request) (*url.URL, error) {
		u, _ := url.Parse("http://corp.proxy:3128")
		return u, nil
	}}
	for _, rawURL := range []string{"https://example.test/x", "http://example.test/x"} {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		require.NoError(t, err)
		assert.NotNil(t, proxy.proxyDecisionFor(req), rawURL)
	}
}

// A caller-supplied CheckRedirect that rewrites the target is re-guarded.
func TestFetchRedirectCallbackRewritesGuardPostTarget(t *testing.T) {
	// The first request gets a canned 302 from a pinnable literal host; the
	// caller's policy then mutates the target to a private address. Follow-up
	// must be blocked even though the callback returned nil.
	mutator := func(req *http.Request, via []*http.Request) error {
		u, _ := url.Parse("http://127.0.0.1/leaked")
		req.URL = u
		return nil
	}
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return serveOnce("HTTP/1.1 302 Found\r\nLocation: http://origin.public/x\r\nContent-Length: 0\r\n\r\n")(ctx, network, addr)
	}
	client := &http.Client{Transport: &http.Transport{DialContext: dial}, CheckRedirect: mutator}
	fetcher := mustFetcher(NewFetcher(client, 0, "test"))
	_, _, err := fetcher.Get(context.Background(), "http://93.184.216.34/start", "*/*", 1024)
	var blocked *BlockedFetchError
	require.ErrorAs(t, err, &blocked, "rewritten redirect target must be re-guarded before dispatch")
}
