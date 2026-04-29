package dmm

import (
	"context"
	"os"
	"testing"

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

func TestValidateBrowserURL(t *testing.T) {
	tests := []struct {
		name        string
		rawURL      string
		wantErr     string
		description string
	}{
		{
			name:        "empty URL",
			rawURL:      "",
			wantErr:     "browser URL is required",
			description: "Should reject empty URLs",
		},
		{
			name:        "malformed URL",
			rawURL:      "ht!tp://invalid-url",
			wantErr:     "invalid browser URL",
			description: "Should reject malformed URLs",
		},
		{
			name:        "unsupported scheme",
			rawURL:      "file:///tmp/test.html",
			wantErr:     "invalid browser URL scheme",
			description: "Should reject non-http schemes",
		},
		{
			name:        "missing host",
			rawURL:      "https:///path-only",
			wantErr:     "invalid browser URL: missing host",
			description: "Should reject URLs without a host",
		},
		{
			name:        "invalid port",
			rawURL:      "http://localhost:99999/invalid",
			wantErr:     "invalid browser URL port: 99999",
			description: "Should reject ports outside the valid range",
		},
		{
			name:        "valid http URL",
			rawURL:      "http://localhost:8080/path",
			description: "Should accept valid http URLs",
		},
		{
			name:        "valid https URL",
			rawURL:      "https://video.dmm.co.jp/av/content/?id=test123",
			description: "Should accept valid https URLs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBrowserURL(tt.rawURL)
			if tt.wantErr == "" {
				assert.NoError(t, err, tt.description)
				return
			}

			if assert.Error(t, err, tt.description) {
				assert.ErrorContains(t, err, tt.wantErr, tt.description)
			}
		})
	}
}

func TestFetchWithBrowser_FailsFastOnInvalidURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		timeout     int
		wantErr     string
		description string
	}{
		{
			name:        "malformed URL",
			url:         "ht!tp://invalid-url",
			timeout:     5,
			wantErr:     "invalid browser URL",
			description: "Should fail before browser launch on malformed URLs",
		},
		{
			name:        "empty URL",
			url:         "",
			timeout:     5,
			wantErr:     "browser URL is required",
			description: "Should fail before browser launch on empty URLs",
		},
		{
			name:        "invalid port with zero timeout",
			url:         "http://localhost:99999",
			timeout:     0,
			wantErr:     "invalid browser URL port: 99999",
			description: "Should validate the URL before attempting browser startup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FetchWithBrowser(context.Background(), tt.url, tt.timeout, nil)
			if assert.Error(t, err, tt.description) {
				assert.ErrorContains(t, err, tt.wantErr, tt.description)
			}
		})
	}
}
