package actresscache

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// Exercise the wrapped DialContext closure NewFetcherWithHostDelays installs:
// AllowPrivateHosts passthrough, trusted proxy host passthrough, and the
// guarded default path.
func TestWrappedDialContextBranches(t *testing.T) {
	spy := &spyDialer{}
	base := &http.Transport{DialContext: spy.dial}

	// AllowPrivateHosts bypasses the guard.
	f := mustFetcher(NewFetcherWithOptions(&http.Client{Transport: base}, 0, "test", nil, true))
	dial := f.client.Transport.(*http.Transport).DialContext
	_, err := dial(context.Background(), "tcp", "127.0.0.1:443")
	require.NoError(t, err)
	require.Contains(t, spy.calls, "127.0.0.1:443")

	// Trusted proxy host bypasses the guard too.
	spy2 := &spyDialer{}
	proxyURL, _ := url.Parse("http://corp.proxy:3128")
	base2 := &http.Transport{DialContext: spy2.dial, Proxy: http.ProxyURL(proxyURL)}
	f2 := mustFetcher(NewFetcher(&http.Client{Transport: base2}, 0, "test"))
	dial2 := f2.client.Transport.(*http.Transport).DialContext
	_, err = dial2(context.Background(), "tcp", "corp.proxy:3128")
	require.NoError(t, err)
	require.Contains(t, spy2.calls, "corp.proxy:3128")
	// Non-proxy host still routes through guardedDialContext: use a literal
	// internal address to prove it (no DNS needed).
	_, err = dial2(context.Background(), "tcp", "10.0.0.1:443")
	require.Error(t, err)

	// Untrusted host address hits the guarded resolver path: stub the
	// resolver to a private IP and assert the refusal.
	base3 := &http.Transport{DialContext: spy2.dial}
	f3 := mustFetcher(NewFetcher(&http.Client{Transport: base3}, 0, "test"))
	dial3 := f3.client.Transport.(*http.Transport).DialContext
	prev := lookupIP
	lookupIP = func(context.Context, string, string) ([]net.IP, error) { return []net.IP{net.ParseIP("10.1.2.3")}, nil }
	defer func() { lookupIP = prev }()
	_, err = dial3(context.Background(), "tcp", "home.lan:443")
	require.Error(t, err)
}
