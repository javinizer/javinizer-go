package actresscache

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func constProxyFor(raw string) func(*http.Request) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return func(*http.Request) (*url.URL, error) { return parsed, nil }
}

func TestProxyPinningRejectsHostnameOverPlainHTTPProxy(t *testing.T) {
	called := 0
	base := fetchTransport(func(*http.Request) (*http.Response, error) {
		called++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody}, nil
	})
	tr := &proxyPinningTransport{base: base, proxyFor: constProxyFor("http://proxy.example:3128")}
	req, err := http.NewRequest(http.MethodGet, "http://media.example/thumb.jpg", nil)
	require.NoError(t, err)
	_, err = tr.RoundTrip(req)
	var unverifiable *ssrf.UnverifiableHostError
	require.ErrorAs(t, err, &unverifiable, "hostname over a plain-HTTP proxy cannot be pinned")
	assert.Zero(t, called, "rejected request must never reach upstream")
}

func TestProxyPinningAllowsLiteralTargetOverProxy(t *testing.T) {
	called := 0
	base := fetchTransport(func(*http.Request) (*http.Response, error) {
		called++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody}, nil
	})
	tr := &proxyPinningTransport{base: base, proxyFor: constProxyFor("http://proxy.example:3128")}
	req, err := http.NewRequest(http.MethodGet, "http://93.184.216.34/thumb.jpg", nil)
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, 1, called, "literals cannot be re-resolved by the proxy")
}

func TestProxyPinningPassthroughsWhenNotProxied(t *testing.T) {
	called := 0
	base := fetchTransport(func(*http.Request) (*http.Response, error) {
		called++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody}, nil
	})
	tr := &proxyPinningTransport{base: base, proxyFor: func(*http.Request) (*url.URL, error) { return nil, nil }}
	for _, url := range []string{"http://media.example/a", "https://tunnel.example/b"} {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		require.NoError(t, err)
		resp, err := tr.RoundTrip(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
	assert.Equal(t, 2, called, "non-proxy and HTTPS paths pass through")
}

func TestProxyPinningAllowsHTTPSHostnameThroughProxy(t *testing.T) {
	called := 0
	base := fetchTransport(func(*http.Request) (*http.Response, error) {
		called++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody}, nil
	})
	tr := &proxyPinningTransport{base: base, proxyFor: constProxyFor("http://proxy.example:3128")}
	req, err := http.NewRequest(http.MethodGet, "https://media.example/thumb.jpg", nil)
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, 1, called, "CONNECT tunnels keep the proxy-trust-boundary model")
}

func TestProxyPinningUninitializedFailsClosed(t *testing.T) {
	var tr *proxyPinningTransport // nil base must fail closed, not panic
	req, err := http.NewRequest(http.MethodGet, "http://media.example/x", nil)
	require.NoError(t, err)
	_, err = tr.RoundTrip(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

// The proxy decision must be computed from the ACTUAL request: a Proxy func
// keyed on request fields (here User-Agent) would be misclassified by a
// synthetic probe that carries none, letting a proxied plain-HTTP hostname
// sail through unverifiable.
func TestProxyPinningUsesActualRequestDecision(t *testing.T) {
	called := 0
	base := fetchTransport(func(*http.Request) (*http.Response, error) {
		called++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody}, nil
	})
	uaKeyed := func(req *http.Request) (*url.URL, error) {
		if req.Header.Get("User-Agent") == "cache-builder" {
			u, _ := url.Parse("http://proxy.example:3128")
			return u, nil
		}
		return nil, nil
	}
	tr := &proxyPinningTransport{base: base, proxyFor: uaKeyed}

	proxiedReq, err := http.NewRequest(http.MethodGet, "http://media.example/thumb.jpg", nil)
	require.NoError(t, err)
	proxiedReq.Header.Set("User-Agent", "cache-builder")
	_, err = tr.RoundTrip(proxiedReq)
	var unverifiable *ssrf.UnverifiableHostError
	require.ErrorAs(t, err, &unverifiable, "the request the proxy actually serves must be rejected")
	assert.Zero(t, called)

	directReq, err := http.NewRequest(http.MethodGet, "http://media.example/thumb.jpg", nil)
	require.NoError(t, err)
	directReq.Header.Set("User-Agent", "other")
	resp, err := tr.RoundTrip(directReq)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, 1, called, "truly direct requests still pass through")
}

// Proxy decision errors must fail the request closed (matching net/http).
func TestProxyPinningFailsClosedOnProxyDecisionError(t *testing.T) {
	tr := &proxyPinningTransport{
		base: fetchTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: http.NoBody}, nil
		}),
		proxyFor: func(*http.Request) (*url.URL, error) { return nil, errors.New("bogus proxy conf") },
	}
	req, err := http.NewRequest(http.MethodGet, "https://media.example/x", nil)
	require.NoError(t, err)
	_, err = tr.RoundTrip(req)
	require.ErrorContains(t, err, "bogus proxy conf")
}

// Plain-HTTP hostname targets are ALLOWED over native socks5 proxying (the
// tunnel owns DNS); they would be flat-out rejected via an HTTP(S) proxy.
func TestPlainHTTPHostnameAllowedViaNativeSocks(t *testing.T) {
	prevLookup := lookupIP
	defer func() { lookupIP = prevLookup }()
	lookupIP = func(_ context.Context, _, host string) ([]net.IP, error) {
		switch host {
		case "legacy-socks.example":
			return []net.IP{net.ParseIP("198.51.100.60")}, nil
		case "media.example":
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		}
		return nil, errors.New("unresolvable: " + host)
	}
	sentinel := errors.New("dial observed")
	var dialed []string
	transport := &http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "socks5", Host: "legacy-socks.example:1080"}),
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			return nil, sentinel
		},
	}
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	_, _, err = fetcher.Get(context.Background(), "http://media.example/thumb.jpg", "image/*", 1<<20)
	require.ErrorIs(t, err, sentinel, "the hop proceeds to the pinned socks endpoint instead of being rejected")
	require.NotEmpty(t, dialed)
	for _, addr := range dialed {
		assert.Equal(t, "198.51.100.60:1080", addr, "every retry goes through the pinned socks endpoint")
	}
}
