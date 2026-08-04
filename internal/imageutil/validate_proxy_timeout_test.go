package imageutil

import (
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A proxy that accepts the request but never answers must be cut off by the
// transport's ResponseHeaderTimeout instead of hanging validation.
func TestPinnedProxyTransportAppliesResponseHeaderTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		time.Sleep(5 * time.Second)
	}()

	proxyURL, err := url.Parse("http://" + listener.Addr().String())
	require.NoError(t, err)
	transport := &pinnedProxyTransport{base: &http.Transport{
		Proxy:                 http.ProxyURL(proxyURL),
		ResponseHeaderTimeout: 100 * time.Millisecond,
	}}

	// Literal public origin: no DNS; the plain-HTTP arm dials the hanging
	// proxy and must be cut off by the header timeout.
	req, err := http.NewRequest(http.MethodGet, "http://93.184.216.34/image", nil)
	require.NoError(t, err)
	start := time.Now()
	_, err = transport.RoundTrip(req)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 3*time.Second)
	assert.ErrorContains(t, err, "timeout")
}
