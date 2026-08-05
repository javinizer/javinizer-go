package ssrf

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

// Compat wrapper keeps the hostname for transports whose Proxy is set
// (SOCKS5/CONNECT dialers own resolution), but literal-private dials still
// stop before any proxy dialer runs.
func TestWrapTransportPreservesHostnameForProxiedDialers(t *testing.T) {
	// The pinned dial resolves and validates targets first; proxies only
	// matter for what gets dialed, so the resolver must be controllable.
	cleanup := SetLookupIPForTest(func(_ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	defer cleanup()

	var dialed []string
	wrapped := WrapTransportWithSSRFCheck(&http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "socks5", Host: "127.0.0.1:1080"}),
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			return nil, errors.New("sentinel-stop")
		},
	})

	_, err := wrapped.DialContext(context.Background(), "tcp", "media.example:443")
	require.ErrorContains(t, err, "sentinel-stop")
	assert.Equal(t, []string{"media.example:443"}, dialed, "proxy dialer keeps hostnames unchanged")

	// A private literal still short-circuits *before* the proxy dialer runs.
	_, err = wrapped.DialContext(context.Background(), "tcp", "10.2.3.4:443")
	require.Error(t, err)
	assert.Len(t, dialed, 1, "blocked target was never dialed")
}
