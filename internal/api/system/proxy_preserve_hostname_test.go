package system

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestProxyTestPreservesHostnames(t *testing.T) {
	assert.False(t, proxyTestPreservesHostnames(nil))
	assert.False(t, proxyTestPreservesHostnames(&models.ProxyProfile{}))
	assert.False(t, proxyTestPreservesHostnames(&models.ProxyProfile{URL: "http://proxy.example.com:8080"}))
	assert.False(t, proxyTestPreservesHostnames(&models.ProxyProfile{URL: "://not-a-url"}))
	assert.True(t, proxyTestPreservesHostnames(&models.ProxyProfile{URL: "socks5://127.0.0.1:1080"}))
	assert.True(t, proxyTestPreservesHostnames(&models.ProxyProfile{URL: "SOCKS5://user:pass@gateway:1080"}))
}
