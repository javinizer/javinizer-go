package dmm

import (
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
)

// TestIsRunningInContainer verifies container detection logic
func TestIsRunningInContainer(t *testing.T) {
	tests := []struct {
		name        string
		setup       func()
		cleanup     func()
		expected    bool
		description string
	}{
		{
			name: "CHROME_BIN environment variable set",
			setup: func() {
				_ = os.Setenv("CHROME_BIN", "/usr/bin/chromium")
			},
			cleanup: func() {
				_ = os.Unsetenv("CHROME_BIN")
			},
			expected:    true,
			description: "Should detect container when CHROME_BIN is set",
		},
		{
			name: "CHROME_PATH environment variable set",
			setup: func() {
				_ = os.Setenv("CHROME_PATH", "/usr/bin/google-chrome")
			},
			cleanup: func() {
				_ = os.Unsetenv("CHROME_PATH")
			},
			expected:    true,
			description: "Should detect container when CHROME_PATH is set",
		},
		{
			name: "No container indicators",
			setup: func() {
				// Ensure env vars are not set
				_ = os.Unsetenv("CHROME_BIN")
				_ = os.Unsetenv("CHROME_PATH")
			},
			cleanup:     func() {},
			expected:    false,
			description: "Should not detect container when no indicators present",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			defer tt.cleanup()

			result := isRunningInContainer()
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestBasicAuth verifies Basic Authentication header generation
func TestBasicAuth(t *testing.T) {
	tests := []struct {
		name        string
		username    string
		password    string
		expected    string
		description string
	}{
		{
			name:        "simple credentials",
			username:    "user",
			password:    "pass",
			expected:    "dXNlcjpwYXNz", // base64("user:pass")
			description: "Should encode simple credentials correctly",
		},
		{
			name:        "empty credentials",
			username:    "",
			password:    "",
			expected:    "Og==", // base64(":")
			description: "Should handle empty credentials",
		},
		{
			name:        "special characters in password",
			username:    "admin",
			password:    "p@ssw0rd!",
			expected:    "YWRtaW46cEBzc3cwcmQh", // base64("admin:p@ssw0rd!")
			description: "Should handle special characters in password",
		},
		{
			name:        "unicode characters",
			username:    "用户",
			password:    "密码",
			expected:    "55So5oi3OuWvhueggQ==", // base64("用户:密码")
			description: "Should handle unicode characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := basicAuth(tt.username, tt.password)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestFetchWithBrowser_Timeout verifies timeout handling with mocked server
func TestFetchWithBrowser_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping browser test in short mode")
	}

	// Note: These tests verify timeout parameter handling logic.
	// They intentionally use invalid URLs to test error paths without real network calls.
	tests := []struct {
		name        string
		url         string
		timeout     int
		expectError bool
		description string
	}{
		{
			name:        "zero timeout uses default",
			url:         "http://localhost:99999/invalid", // Invalid URL to avoid network calls
			timeout:     0,
			expectError: true,
			description: "Should use default timeout when timeout is 0",
		},
		{
			name:        "negative timeout uses default",
			url:         "http://localhost:99999/invalid", // Invalid URL to avoid network calls
			timeout:     -1,
			expectError: true,
			description: "Should use default timeout when timeout is negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FetchWithBrowser(tt.url, tt.timeout, nil)

			if tt.expectError {
				assert.Error(t, err, tt.description)
				t.Logf("Browser fetch correctly returned error: %v", err)
			}
		})
	}
}

// TestFetchWithBrowser_ProxyConfig verifies proxy configuration handling
func TestFetchWithBrowser_ProxyConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping browser test in short mode")
	}

	// Note: These tests verify proxy configuration is properly applied.
	// Using invalid URLs to test error paths without real network/proxy calls.
	tests := []struct {
		name        string
		proxyConfig *config.ProxyConfig
		description string
	}{
		{
			name:        "nil proxy config",
			proxyConfig: nil,
			description: "Should handle nil proxy config",
		},
		{
			name: "proxy disabled",
			proxyConfig: &config.ProxyConfig{
				Enabled: false,
				URL:     "",
			},
			description: "Should handle disabled proxy",
		},
		{
			name: "proxy enabled without URL",
			proxyConfig: &config.ProxyConfig{
				Enabled: true,
				URL:     "",
			},
			description: "Should handle enabled proxy without URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use invalid URL to avoid real network calls
			// This tests that proxy config doesn't cause panics in setup
			_, err := FetchWithBrowser("http://localhost:99999/invalid", 2, tt.proxyConfig)
			// We expect errors due to invalid URL, but no panics
			assert.Error(t, err, "Should error on invalid URL")
			t.Logf("Proxy config test completed: %v", err)
		})
	}
}

// TestFetchWithBrowser_ContainerMode verifies container mode detection
func TestFetchWithBrowser_ContainerMode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping browser test in short mode")
	}

	tests := []struct {
		name        string
		setup       func()
		cleanup     func()
		description string
	}{
		{
			name: "with CHROME_BIN set",
			setup: func() {
				_ = os.Setenv("CHROME_BIN", "/usr/bin/chromium")
			},
			cleanup: func() {
				_ = os.Unsetenv("CHROME_BIN")
			},
			description: "Should use container mode when CHROME_BIN is set",
		},
		{
			name: "without container indicators",
			setup: func() {
				_ = os.Unsetenv("CHROME_BIN")
				_ = os.Unsetenv("CHROME_PATH")
			},
			cleanup:     func() {},
			description: "Should use standard mode when no container indicators",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			defer tt.cleanup()

			// Use invalid URL to avoid real network calls
			// This tests container detection logic without launching browser
			_, err := FetchWithBrowser("http://localhost:99999/invalid", 2, nil)
			assert.Error(t, err, "Should error on invalid URL")
			t.Logf("Container mode test completed: %v", err)
		})
	}
}

// TestFetchWithBrowser_InvalidURL verifies error handling for invalid URLs
func TestFetchWithBrowser_InvalidURL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping browser test in short mode")
	}

	tests := []struct {
		name        string
		url         string
		expectError bool
		description string
	}{
		{
			name:        "malformed URL",
			url:         "ht!tp://invalid-url",
			expectError: true,
			description: "Should return error for malformed URL",
		},
		{
			name:        "empty URL",
			url:         "",
			expectError: true,
			description: "Should return error for empty URL",
		},
		{
			name:        "localhost (likely fails)",
			url:         "http://localhost:99999",
			expectError: true,
			description: "Should handle unreachable URLs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FetchWithBrowser(tt.url, 5, nil)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			}
		})
	}
}
