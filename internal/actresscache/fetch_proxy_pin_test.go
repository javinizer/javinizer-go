package actresscache

import (
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyPinningRejectsHostnameOverPlainHTTPProxy(t *testing.T) {
	called := 0
	base := fetchTransport(func(*http.Request) (*http.Response, error) {
		called++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody}, nil
	})
	tr := &proxyPinningTransport{base: base, viaProxy: func(string, string) bool { return true }}
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
	tr := &proxyPinningTransport{base: base, viaProxy: func(string, string) bool { return true }}
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
	tr := &proxyPinningTransport{base: base, viaProxy: func(string, string) bool { return false }}
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
	tr := &proxyPinningTransport{base: base, viaProxy: func(string, string) bool { return true }}
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
