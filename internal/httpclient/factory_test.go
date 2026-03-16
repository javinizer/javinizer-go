package httpclient

import (
	"net/http"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
)

func TestNewTransport_NoProxy(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: false,
	}

	transport, err := NewTransport(proxyConfig)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}

	// Transport should be successfully created
	// DialContext will be set by DefaultTransport.Clone(), which is expected
	if transport == nil {
		t.Fatal("Expected valid transport")
	}
	if transport.Proxy != nil {
		t.Error("Expected transport.Proxy to be nil when proxy is not configured")
	}
}

func TestNewTransport_ProxyDisabled(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: false,
		URL:     "http://proxy.example.com:8080",
	}

	transport, err := NewTransport(proxyConfig)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}

	// Even with URL set, disabled means URL is ignored
	if transport == nil {
		t.Fatal("Expected valid transport when proxy disabled")
	}
	if transport.Proxy != nil {
		t.Error("Expected transport.Proxy to be nil when proxy is disabled")
	}
}

func TestNewTransport_HTTPProxy(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: true,
		URL:     "http://proxy.example.com:8080",
	}

	transport, err := NewTransport(proxyConfig)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}

	if transport.Proxy == nil {
		t.Error("Expected proxy to be configured")
	}
}

func TestNewTransport_HTTPProxyWithoutScheme(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: true,
		URL:     "proxy.example.com:8080",
	}

	transport, err := NewTransport(proxyConfig)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}
	if transport.Proxy == nil {
		t.Fatal("Expected proxy to be configured")
	}

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("http.NewRequest failed: %v", err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("transport.Proxy failed: %v", err)
	}
	if proxyURL == nil {
		t.Fatal("Expected resolved proxy URL")
	}
	if proxyURL.Scheme != "http" {
		t.Errorf("Expected normalized proxy scheme http, got %q", proxyURL.Scheme)
	}
	if proxyURL.Host != "proxy.example.com:8080" {
		t.Errorf("Expected normalized proxy host proxy.example.com:8080, got %q", proxyURL.Host)
	}
}

func TestNewTransport_HTTPSProxy(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: true,
		URL:     "https://proxy.example.com:8080",
	}

	transport, err := NewTransport(proxyConfig)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}

	if transport.Proxy == nil {
		t.Error("Expected proxy to be configured")
	}
}

func TestNewTransport_SOCKS5Proxy(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: true,
		URL:     "socks5://localhost:1080",
	}

	transport, err := NewTransport(proxyConfig)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}

	// SOCKS5 sets DialContext (not Dial)
	if transport.DialContext == nil {
		t.Error("Expected SOCKS5 DialContext to be configured")
	}
}

func TestNewTransport_ProxyWithAuth(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled:  true,
		URL:      "http://proxy.example.com:8080",
		Username: "testuser",
		Password: "testpass",
	}

	transport, err := NewTransport(proxyConfig)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}

	if transport.Proxy == nil {
		t.Error("Expected proxy to be configured")
	}
}

func TestNewTransport_SOCKS5ProxyWithAuth(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled:  true,
		URL:      "socks5://localhost:1080",
		Username: "testuser",
		Password: "testpass",
	}

	transport, err := NewTransport(proxyConfig)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}

	// SOCKS5 sets DialContext (not Dial)
	if transport.DialContext == nil {
		t.Error("Expected SOCKS5 DialContext to be configured")
	}
}

func TestNewTransport_InvalidURL(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: true,
		URL:     "://invalid",
	}

	_, err := NewTransport(proxyConfig)
	if err == nil {
		t.Error("Expected error for invalid proxy URL")
	}
}

func TestNewTransport_EmptyURL(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: true,
		URL:     "",
	}

	transport, err := NewTransport(proxyConfig)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}

	// Empty URL with enabled should still create valid transport (no-op proxy config)
	if transport == nil {
		t.Fatal("Expected valid transport for empty URL")
	}
	if transport.Proxy != nil {
		t.Error("Expected transport.Proxy to be nil when proxy URL is empty")
	}
}

func TestNewHTTPClient_NoProxy(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: false,
	}

	client, err := NewHTTPClient(proxyConfig, 30*time.Second)
	if err != nil {
		t.Fatalf("NewHTTPClient failed: %v", err)
	}

	if client.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", client.Timeout)
	}
}

func TestNewHTTPClient_WithProxy(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: true,
		URL:     "http://proxy.example.com:8080",
	}

	client, err := NewHTTPClient(proxyConfig, 60*time.Second)
	if err != nil {
		t.Fatalf("NewHTTPClient failed: %v", err)
	}

	if client.Timeout != 60*time.Second {
		t.Errorf("Expected timeout 60s, got %v", client.Timeout)
	}
}

func TestNewHTTPClient_InvalidProxy(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: true,
		URL:     "://invalid",
	}

	_, err := NewHTTPClient(proxyConfig, 30*time.Second)
	if err == nil {
		t.Error("Expected error for invalid proxy URL")
	}
}

func TestNewRestyClient_NoProxy(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: false,
	}

	client, err := NewRestyClient(proxyConfig, 30*time.Second, 3)
	if err != nil {
		t.Fatalf("NewRestyClient failed: %v", err)
	}

	if client == nil {
		t.Error("Expected valid resty client")
	}
}

func TestNewRestyClient_WithProxy(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: true,
		URL:     "http://proxy.example.com:8080",
	}

	client, err := NewRestyClient(proxyConfig, 45*time.Second, 5)
	if err != nil {
		t.Fatalf("NewRestyClient failed: %v", err)
	}

	if client == nil {
		t.Error("Expected valid resty client")
	}
}

func TestNewRestyClient_InvalidProxy(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: true,
		URL:     "://invalid",
	}

	_, err := NewRestyClient(proxyConfig, 30*time.Second, 3)
	if err == nil {
		t.Error("Expected error for invalid proxy URL")
	}
}

func TestNewTransport_NilConfig(t *testing.T) {
	transport, err := NewTransport(nil)
	if err != nil {
		t.Fatalf("NewTransport with nil config failed: %v", err)
	}

	// Nil config should create valid transport with defaults
	if transport == nil {
		t.Fatal("Expected valid transport with nil config")
	}
	if transport.Proxy != nil {
		t.Error("Expected transport.Proxy to be nil for nil config")
	}
}

func TestNewHTTPClient_NilConfig(t *testing.T) {
	client, err := NewHTTPClient(nil, 30*time.Second)
	if err != nil {
		t.Fatalf("NewHTTPClient with nil config failed: %v", err)
	}

	if client == nil {
		t.Error("Expected valid client with nil config")
	}
}

func TestNewRestyClient_NilConfig(t *testing.T) {
	client, err := NewRestyClient(nil, 30*time.Second, 3)
	if err != nil {
		t.Fatalf("NewRestyClient with nil config failed: %v", err)
	}

	if client == nil {
		t.Error("Expected valid resty client with nil config")
	}
}

func TestNewRestyClientNoProxy(t *testing.T) {
	client := NewRestyClientNoProxy(30*time.Second, 2)
	if client == nil {
		t.Fatal("Expected valid no-proxy resty client")
	}

	transport, ok := client.GetClient().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Expected *http.Transport, got %T", client.GetClient().Transport)
	}
	if transport.Proxy != nil {
		t.Error("Expected no-proxy transport (Proxy=nil)")
	}
}

func TestSanitizeProxyURL_WithCredentials(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "HTTP with credentials",
			input:    "http://user:pass@proxy.example.com:8080",
			expected: "http://%5BREDACTED%5D@proxy.example.com:8080", // URL-encoded [REDACTED]
		},
		{
			name:     "SOCKS5 with credentials",
			input:    "socks5://admin:secret@localhost:1080",
			expected: "socks5://%5BREDACTED%5D@localhost:1080", // URL-encoded [REDACTED]
		},
		{
			name:     "No credentials",
			input:    "http://proxy.example.com:8080",
			expected: "http://proxy.example.com:8080",
		},
		{
			name:     "No scheme host only",
			input:    "proxy.example.com:8080",
			expected: "http://proxy.example.com:8080",
		},
		{
			name:     "Invalid URL",
			input:    "://invalid",
			expected: "://invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeProxyURL(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeProxyURL(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNewTransport_SOCKS5ClearsHTTPProxy(t *testing.T) {
	proxyConfig := &config.ProxyConfig{
		Enabled: true,
		URL:     "socks5://localhost:1080",
	}

	transport, err := NewTransport(proxyConfig)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}

	// SOCKS5 should clear transport.Proxy to avoid HTTP_PROXY env var override
	if transport.Proxy != nil {
		t.Error("Expected transport.Proxy to be nil for SOCKS5 proxy")
	}

	// DialContext should be set for SOCKS5
	if transport.DialContext == nil {
		t.Error("Expected DialContext to be configured for SOCKS5")
	}
}
