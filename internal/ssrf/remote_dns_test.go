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
	// Local resolution MUST NOT run for remote-DNS dialers: proxy-only names
	// would fail, and split-horizon answers would be misjudged.
	restore := SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return nil, errors.New("local DNS cannot see proxy-resolved names")
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
	_, err := client.Get("http://proxy-only.example/")
	require.ErrorContains(t, err, "record-only dialer")
	assert.Equal(t, "proxy-only.example:80", dialed,
		"remote-DNS dialer receives the hostname unresolved (no local lookup)")

	// A name that IS locally resolvable but answers privately in local DNS
	// (split-horizon) must still pass through untouched.
	dialed = ""
	restore()
	restore = SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.9.9.9")}, nil
	})
	defer restore()
	_, err = client.Get("http://mirror.lan/")
	require.ErrorContains(t, err, "record-only dialer")
	assert.Equal(t, "mirror.lan:80", dialed, "split-horizon targets stay proxy-side")
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
