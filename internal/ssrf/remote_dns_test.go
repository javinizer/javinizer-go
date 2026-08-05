package ssrf

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A SOCKS5-style dialer (remote DNS, Transport.Proxy nil) must keep the
// original hostname: pinning to a locally resolved IP defeats split-horizon
// and proxy-side DNS.
func TestWrapTransportPreservesHostnameForRemoteDNSDialer(t *testing.T) {
	restore := SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	defer restore()
	var dialed string
	socksLike := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = addr
		return nil, errors.New("record-only dialer")
	}
	transport := &http.Transport{DialContext: socksLike}
	wrapped := WrapTransportPreservingHostnames(transport)
	require.Same(t, transport, wrapped)

	client := &http.Client{Transport: wrapped}
	_, err := client.Get("http://split-horizon.example/")
	require.ErrorContains(t, err, "record-only dialer")
	assert.Equal(t, "split-horizon.example:80", dialed, "remote-DNS dialer must receive the hostname, not a pinned IP")
}

// Custom dialers WITHOUT the remote-DNS marker keep fail-closed pinning.
func TestWrapTransportPinsUnmarkedCustomDialer(t *testing.T) {
	restore := SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	defer restore()
	var dialed string
	transport := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = addr
		return nil, errors.New("record-only dialer")
	}}
	wrapped := WrapTransportWithSSRFCheck(transport)
	client := &http.Client{Transport: wrapped}
	_, err := client.Get("http://pinned-target.example/")
	require.ErrorContains(t, err, "record-only dialer")
	assert.Equal(t, "93.184.216.34:80", dialed, "unmarked dial paths stay pinned to validated IPs")
}
