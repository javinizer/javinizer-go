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

// The proxied-request exemption follows the request's chosen proxy endpoint
// AND pins it: net/http dials the proxy (canonical scheme default port
// included) with the marker, and the resolved+validated answers are dialed,
// not the freshly-re-resolvable hostname.
func TestProxyDialExemptionFollowsTheProxiedRequest(t *testing.T) {
	prevLookup := lookupIP
	defer func() { lookupIP = prevLookup }()
	lookupIP = func(_ context.Context, _, _ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("198.51.100.7")}, nil
	}
	sentinel := errors.New("reached proxy endpoint")
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
	require.Equal(t, []string{"198.51.100.7:80"}, dialed, "proxy dial is pinned to the resolved answer")
}

// An unresolvable configured proxy fails the request loudly instead of
// silently re-resolving on someone else's resolver.
func TestProxyDialExemptionFailsWhenProxyUnresolvable(t *testing.T) {
	prevLookup := lookupIP
	defer func() { lookupIP = prevLookup }()
	lookupIP = func(_ context.Context, _, _ string) ([]net.IP, error) { return nil, errors.New("nxdomain") }
	sentinel := errors.New("must not dial")
	transport := &http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "proxy.example"}),
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return nil, sentinel
		},
	}
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	_, err = fetcher.client.Get("https://img.example/thumb.jpg")
	require.Error(t, err)
	assert.NotErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), "resolve configured proxy")
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

// Literal-IP proxy endpoints dial through untouched (no resolution needed).
func TestProxyDialExemptionLiteralIPPassthrough(t *testing.T) {
	prevLookup := lookupIP
	defer func() { lookupIP = prevLookup }()
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		t.Fatal("literal proxy endpoints must not resolve")
		return nil, nil
	}
	sentinel := errors.New("reached literal proxy")
	var dialed string
	transport := &http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "192.0.2.5:3128"}),
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			dialed = addr
			return nil, sentinel
		},
	}
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	_, err = fetcher.client.Get("https://img.example/thumb.jpg")
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, "192.0.2.5:3128", dialed)
}

// An empty resolver answer set fails loudly, never riding the raw fallback.
func TestProxyDialExemptionFailsOnEmptyAnswers(t *testing.T) {
	prevLookup := lookupIP
	defer func() { lookupIP = prevLookup }()
	lookupIP = func(context.Context, string, string) ([]net.IP, error) { return nil, nil }
	sentinel := errors.New("must not dial")
	transport := &http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "proxy.example"}),
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return nil, sentinel
		},
	}
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	_, err = fetcher.client.Get("https://img.example/thumb.jpg")
	require.Error(t, err)
	assert.NotErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), "no addresses")
}

func TestCanonicalProxyDialTargetSchemeDefaults(t *testing.T) {
	assert.Equal(t, "proxy.example:443", canonicalProxyDialTarget("https", "Proxy.EXAMPLE", ""))
	assert.Equal(t, "proxy.example:1080", canonicalProxyDialTarget("socks5", "proxy.example", ""))
	assert.Equal(t, "proxy.example:1080", canonicalProxyDialTarget("socks5h", "proxy.example", ""))
	assert.Equal(t, "proxy.example:80", canonicalProxyDialTarget("http", "proxy.example", ""))
	assert.Equal(t, "proxy.example:8443", canonicalProxyDialTarget("http", "proxy.example", "8443"))
}

// The preflight (checkFetchTarget) must evaluate the proxy decision on the
// REQUEST that will actually run -- headers included. A User-Agent-keyed
// proxy whose target cannot be resolved locally must fail closed
// (unverifiable), not sail into a CONNECT the proxy resolves privately.
func TestGetFailsClosedForHeaderKeyedProxyWhenLocalDNSFails(t *testing.T) {
	prevLookup := lookupIP
	defer func() { lookupIP = prevLookup }()
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return nil, errors.New("nxdomain: home-view only")
	}
	uaKeyed := func(req *http.Request) (*url.URL, error) {
		if req.Header.Get("User-Agent") == "cache-builder" {
			return &url.URL{Scheme: "http", Host: "corp.proxy:3128"}, nil
		}
		return nil, nil
	}
	transport := &http.Transport{Proxy: uaKeyed, DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("no dial should happen")
	}}
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "cache-builder", nil, false)
	require.NoError(t, err)
	_, _, err = fetcher.Get(context.Background(), "https://mirror.lan.local/thumb.jpg", "image/*", 1<<20)
	var unsure *ssrf.UnverifiableHostError
	require.ErrorAs(t, err, &unsure, "header-matched proxy + unverifiable local DNS must fail closed")
}

// A rotating Proxy func answers differently per call; the marker, rejection,
// and dial must all follow the SAME decision -- the wrapper evaluates once,
// and the base transport replays it from the request ledger.
func TestProxiedDialFollowsTheEvaluatedDecision(t *testing.T) {
	prevLookup := lookupIP
	defer func() { lookupIP = prevLookup }()
	lookupIP = func(_ context.Context, _, host string) ([]net.IP, error) {
		switch host {
		case "proxy-a.example":
			return []net.IP{net.ParseIP("198.51.100.11")}, nil
		case "proxy-b.example":
			return []net.IP{net.ParseIP("198.51.100.22")}, nil
		}
		return nil, errors.New("unreachable target (fine: proxies answer)")
	}
	calls := 0
	rotating := func(*http.Request) (*url.URL, error) {
		calls++
		if calls%2 == 1 {
			return &url.URL{Scheme: "http", Host: "proxy-a.example:3128"}, nil
		}
		return &url.URL{Scheme: "http", Host: "proxy-b.example:3128"}, nil
	}
	sentinel := errors.New("dial observed")
	var dialed []string
	transport := &http.Transport{
		Proxy: rotating,
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			return nil, sentinel
		},
	}
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	_, err = fetcher.client.Get("https://media.example/thumb.jpg")
	require.ErrorIs(t, err, sentinel)
	require.Len(t, dialed, 1)
	assert.Equal(t, "198.51.100.11:3128", dialed[0],
		"the dial must use the FIRST evaluated decision (marker cannot disagree)")
}

// The ledgered Proxy func falls back to the original policy for requests the
// wrapper never evaluated (defensive: internal callers + future paths).
func TestProxyLedgerFallsBackToOriginalPolicy(t *testing.T) {
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "proxy-c.example:8080"})}
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	inner := fetcher.client.Transport.(*proxyPinningTransport).base.(*http.Transport)
	got, err := inner.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "direct-target.example"}})
	require.NoError(t, err)
	assert.Equal(t, "proxy-c.example:8080", got.Host)
}
