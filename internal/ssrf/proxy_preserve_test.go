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

// Compat wrapper PINS proxy connections to validated IPs: net/http dials the
// proxy address when Proxy is set, and re-resolving the proxy hostname at
// dial time would reopen DNS rebinding onto private addresses. Hostname
// preservation exists only for explicit remote-DNS wrapping (SOCKS5
// DialContext transports). Literal-private dials still stop before any
// proxy dialer runs.
func TestWrapTransportPinsDialTargetForProxiedTransports(t *testing.T) {
	// The pinned dial resolves and validates targets first; proxies only
	// matter for what gets dialed, so the resolver must be controllable.
	cleanup := SetLookupIPForTest(func(_ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	defer cleanup()

	var dialed []string
	wrapped := WrapTransportWithSSRFCheck(&http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "proxy.example:8080"}),
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			return nil, errors.New("sentinel-stop")
		},
	})

	_, err := wrapped.DialContext(context.Background(), "tcp", "media.example:443")
	require.ErrorContains(t, err, "sentinel-stop")
	assert.Equal(t, []string{"93.184.216.34:443"}, dialed, "dial target is pinned to the validated IP (no rebinding window)")

	// A private literal still short-circuits *before* the proxy dialer runs.
	_, err = wrapped.DialContext(context.Background(), "tcp", "10.2.3.4:443")
	require.Error(t, err)
	assert.Len(t, dialed, 1, "blocked target was never dialed")
}
