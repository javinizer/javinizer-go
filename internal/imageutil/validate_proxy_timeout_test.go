package imageutil

import (
	"bufio"
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

// A proxy answering promptly within the configured timeout succeeds, and the
// success path clears the read deadline before the body streams.
func TestPinnedProxyTransportResponseHeaderTimeoutSuccessPath(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Drain the request first: closing a socket with unread client bytes
		// RSTs the connection on Windows and tears down the response.
		if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
			return
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
	}()

	proxyURL, err := url.Parse("http://" + listener.Addr().String())
	require.NoError(t, err)
	transport := &pinnedProxyTransport{base: &http.Transport{
		Proxy:                 http.ProxyURL(proxyURL),
		ResponseHeaderTimeout: 2 * time.Second,
	}}

	req, err := http.NewRequest(http.MethodGet, "http://93.184.216.34/image", nil)
	require.NoError(t, err)
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
