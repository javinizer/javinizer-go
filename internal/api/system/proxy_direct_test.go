package system

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Direct (no-proxy) profile takes the default pinning wrapper and succeeds
// against a loopback target allowed for tests.
func TestDirectProxyDirectProfilePinsAndConnects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer ssrf.AllowHostForTest("127.0.0.1")()

	result := TestDirectProxy(context.Background(), server.URL, &models.ProxyProfile{}, "")
	assert.True(t, result.Success, "message: %s", result.Message)
	assert.Equal(t, http.StatusOK, result.StatusCode)
}

// A SOCKS5 profile takes the hostname-preserving wrapper; with no SOCKS
// listener the connect fails fast (no local DNS rewrite of the target).
func TestDirectProxySOCKS5ProfilePreservesHostnames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()
	defer ssrf.AllowHostForTest("127.0.0.1")()

	result := TestDirectProxy(context.Background(), server.URL, &models.ProxyProfile{URL: "socks5://127.0.0.1:59999"}, "")
	assert.False(t, result.Success)
	require.NotEmpty(t, result.Message)
	assert.Contains(t, result.ProxyURL, "socks5://127.0.0.1:59999")
}
