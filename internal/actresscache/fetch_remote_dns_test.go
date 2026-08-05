package actresscache

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SOCKS5 transports (registered as remote-DNS by httpclient) keep target
// hostnames end to end: no local DNS runs, no pinning rewrites the dial,
// and the dialer (socks, owning resolution) receives the name verbatim.
func TestFetcherPreservesHostnamesForRemoteDNSTransports(t *testing.T) {
	prevLookup := lookupIP
	defer func() { lookupIP = prevLookup }()
	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		t.Fatal("local DNS must not run for remote-DNS transports")
		return nil, nil
	}
	var dialed []string
	socksLike := func(_ context.Context, network, addr string) (net.Conn, error) {
		dialed = append(dialed, addr)
		return serveOnce("HTTP/1.1 200 OK\r\nContent-Type: image/jpeg\r\nContent-Length: 2\r\n\r\nok")(context.Background(), network, addr)
	}
	transport := &http.Transport{DialContext: socksLike}
	ssrf.MarkRemoteDNSTransport(transport)

	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	body, _, err := fetcher.Get(context.Background(), "http://proxy-only.example/x.jpg", "image/*", 1<<20)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
	require.Equal(t, []string{"proxy-only.example:80"}, dialed, "the socks dialer gets the hostname, not a pinned IP")

	// Private IP literals need no DNS at all: still blocked, dialed untouched.
	_, _, err = fetcher.Get(context.Background(), "http://10.0.0.5/x.jpg", "image/*", 1<<20)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "10.0.0.5")
	assert.Len(t, dialed, 1, "blocked literal never reaches the remote dialer")
}

// The remote-DNS dial hook itself (used when the preflight was skipped by an
// embedding flow) still refuses private IP literals before dialing.
func TestRemoteDNSDialBlocksPrivateLiteral(t *testing.T) {
	sentinel := errors.New("raw dialer ran")
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, sentinel
	}}
	ssrf.MarkRemoteDNSTransport(transport)
	fetcher, err := NewFetcherWithOptions(&http.Client{Transport: transport}, 0, "test", nil, false)
	require.NoError(t, err)
	inner := fetcher.client.Transport.(*proxyPinningTransport).base.(*http.Transport)
	_, err = inner.DialContext(context.Background(), "tcp", "169.254.169.254:80")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private/internal IP literal")
	assert.NotErrorIs(t, err, sentinel)
}
