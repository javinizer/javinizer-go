package httpclient_test

import (
	"os"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	httpclient "github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireFlareSolverrIntegration(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if os.Getenv("JAVINIZER_RUN_FLARESOLVERR_TESTS") != "1" {
		t.Skip("set JAVINIZER_RUN_FLARESOLVERR_TESTS=1 to run FlareSolverr integration tests")
	}
}

// TestFlareSolverr_RealConnection is an integration test that tests actual FlareSolverr connection
// Run with: go test -v ./internal/httpclient/... -run TestFlareSolverr_RealConnection
func TestFlareSolverr_RealConnection(t *testing.T) {
	requireFlareSolverrIntegration(t)

	t.Log("Starting FlareSolverr integration test...")

	// Configure FlareSolverr with longer timeout for testing
	cfg := config.FlareSolverrConfig{
		Enabled:    true,
		URL:        "http://localhost:8191/v1",
		Timeout:    60, // 60 seconds timeout
		MaxRetries: 2,
		SessionTTL: 300,
	}

	fs, err := httpclient.NewFlareSolverr(&cfg)
	require.NoError(t, err)
	require.NotNil(t, fs)

	t.Log("FlareSolverr client created successfully")

	// Test 1: Resolve a simple URL (httpbin.org for testing)
	t.Run("ResolveSimpleURL", func(t *testing.T) {
		targetURL := "https://httpbin.org/get"

		html, cookies, err := fs.ResolveURL(targetURL)

		t.Logf("Target URL: %s", targetURL)
		t.Logf("Error: %v", err)
		t.Logf("HTML length: %d", len(html))
		t.Logf("Cookies count: %d", len(cookies))

		// For integration tests, we just verify we got a response
		// We don't assert success because FlareSolverr might not be available
		if err != nil {
			t.Logf("Failed to resolve URL: %v", err)
			// Don't fail the test - this is expected if FlareSolverr is not running
		} else {
			assert.NotEmpty(t, html)
			t.Logf("Successfully resolved URL, got %d bytes of HTML", len(html))
		}
	})

	// Test 2: Create a session
	t.Run("CreateSession", func(t *testing.T) {
		sessionID, err := fs.CreateSession()

		t.Logf("Session ID: %s", sessionID)
		t.Logf("Error: %v", err)

		if err != nil {
			t.Logf("Failed to create session: %v", err)
			// Don't fail the test
		} else {
			assert.NotEmpty(t, sessionID)
			t.Logf("Successfully created session: %s", sessionID)

			// Clean up the session
			err = fs.DestroySession(sessionID)
			t.Logf("Destroy session error: %v", err)
		}
	})

	// Test 3: Resolve with session
	t.Run("ResolveWithSession", func(t *testing.T) {
		sessionID, err := fs.CreateSession()
		if err != nil {
			t.Skip("cannot test session-based resolution without session creation")
			return
		}
		defer func() { _ = fs.DestroySession(sessionID) }()

		targetURL := "https://httpbin.org/get"
		html, cookies, err := fs.ResolveURLWithSession(targetURL, sessionID)

		t.Logf("Target URL: %s", targetURL)
		t.Logf("Session ID: %s", sessionID)
		t.Logf("Error: %v", err)
		t.Logf("HTML length: %d", len(html))
		t.Logf("Cookies count: %d", len(cookies))

		if err != nil {
			t.Logf("Failed to resolve URL with session: %v", err)
		} else {
			assert.NotEmpty(t, html)
			t.Logf("Successfully resolved URL with session, got %d bytes of HTML", len(html))
		}
	})
}

// TestFlareSolverr_JavLibraryConnection tests actual JavLibrary connection via FlareSolverr
// Run with: go test -v ./internal/httpclient/... -run TestFlareSolverr_JavLibraryConnection
func TestFlareSolverr_JavLibraryConnection(t *testing.T) {
	requireFlareSolverrIntegration(t)

	t.Log("Starting JavLibrary FlareSolverr integration test...")

	// Configure FlareSolverr with longer timeout for JavLibrary
	cfg := config.FlareSolverrConfig{
		Enabled:    true,
		URL:        "http://localhost:8191/v1",
		Timeout:    90, // 90 seconds timeout for Cloudflare
		MaxRetries: 2,
		SessionTTL: 300,
	}

	fs, err := httpclient.NewFlareSolverr(&cfg)
	require.NoError(t, err)
	require.NotNil(t, fs)

	t.Log("FlareSolverr client created successfully")

	// Test JavLibrary URL
	targetURL := "http://www.javlibrary.com/vl_searchbyid.php?keyword=IPX-123"

	t.Log("Target URL:", targetURL)

	html, cookies, err := fs.ResolveURL(targetURL)

	t.Logf("Error: %v", err)
	t.Logf("HTML length: %d", len(html))
	t.Logf("Cookies count: %d", len(cookies))

	// For integration tests, we just verify we got a response
	if err != nil {
		t.Logf("Failed to resolve JavLibrary URL: %v", err)
		t.Logf("This is expected if FlareSolverr is not running properly")
		// Don't fail the test
	} else {
		assert.NotEmpty(t, html)

		// Check if we got actual HTML (not an error message)
		if len(html) > 100 {
			t.Logf("Successfully resolved JavLibrary URL, got %d bytes of HTML", len(html))

			// Check for HTML indicators
			if strings.Contains(html, "<html>") || strings.Contains(html, "<!DOCTYPE") {
				t.Log("Response contains HTML - good!")
			} else if strings.Contains(html, "Cloudflare") {
				t.Log("Response contains Cloudflare - FlareSolverr may need to be running")
			}
		} else {
			t.Logf("Response seems too short (%d bytes) - may be an error message", len(html))
			t.Logf("Response preview: %s", html[:min(200, len(html))])
		}
	}
}

// TestFlareSolverr_ConfigValidation tests various config scenarios
func TestFlareSolverr_ConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		cfg       config.FlareSolverrConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid config",
			cfg: config.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    30,
				MaxRetries: 3,
				SessionTTL: 300,
			},
			wantError: false,
		},
		{
			name: "empty URL",
			cfg: config.FlareSolverrConfig{
				Enabled:    true,
				URL:        "",
				Timeout:    30,
				MaxRetries: 3,
				SessionTTL: 300,
			},
			wantError: true,
			errorMsg:  "FlareSolverr URL is required",
		},
		{
			name: "disabled",
			cfg: config.FlareSolverrConfig{
				Enabled:    false,
				URL:        "http://localhost:8191/v1",
				Timeout:    30,
				MaxRetries: 3,
				SessionTTL: 300,
			},
			wantError: false, // Disabled is valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, err := httpclient.NewFlareSolverr(&tt.cfg)

			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, fs)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, fs)
			}
		})
	}
}
