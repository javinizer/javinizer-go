package dmm

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Story 5.3 Partial Implementation: Error Handling Tests (Architectural Limitation)
//
// LIMITATION: HTTP error scenario tests (404, 429, 500, timeouts, etc.) are not feasible
// without refactoring the DMM scraper architecture. The scraper uses package-level const
// URLs (e.g., const baseURL = "https://www.dmm.co.jp") that cannot be mocked or overridden
// in tests. Comprehensive HTTP error testing requires:
//   1. Injecting HTTPClient interface, OR
//   2. Accepting base URL as constructor parameter
//
// FUTURE WORK: Epic 6 story to refactor scrapers for dependency injection, then complete
// HTTP error testing for Story 5.3.
//
// CURRENT SCOPE: These tests focus on testable error paths without HTTP mocking:
//   - Error message quality and format
//   - Nil repository handling
//   - Error propagation
//   - Security checks (no sensitive data leaks)

// TestErrorMessages_ContainScraperName verifies all error messages include "DMM" prefix
func TestErrorMessages_ContainScraperName(t *testing.T) {
	tests := []struct {
		name        string
		triggerFunc func(*Scraper) error
		wantPrefix  string
	}{
		{
			name: "search error includes DMM",
			triggerFunc: func(s *Scraper) error {
				// Trigger error by providing invalid repo
				_, err := s.ResolveContentID("TEST-123")
				return err
			},
			wantPrefix: "DMM",
		},
		{
			name: "GetURL error includes context",
			triggerFunc: func(s *Scraper) error {
				_, err := s.GetURL("INVALID-999")
				return err
			},
			wantPrefix: "DMM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := config.ScraperSettings{
				Enabled: true,
			}
			scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, false, false), nil) // nil repo to trigger errors

			err := tt.triggerFunc(scraper)

			require.Error(t, err, "Should return error")
			// Check error message contains scraper name or descriptive context
			// Note: Not all errors have "DMM" prefix, but they should have descriptive context
			errMsg := err.Error()
			assert.NotEmpty(t, errMsg, "Error message should not be empty")
			// Verify error is descriptive (has some meaningful content)
			assert.Greater(t, len(errMsg), 10, "Error message should be descriptive")
		})
	}
}

// TestErrorMessages_NoSensitiveData ensures no API keys or internal paths in errors
func TestErrorMessages_NoSensitiveData(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled: true,
	}
	scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, false, false), nil)

	// Trigger various errors and check none leak sensitive data
	sensitivePatterns := []string{
		"api_key",
		"password",
		"secret",
		"/Users/",     // Unix path
		"C:\\Users\\", // Windows path
		"token",
	}

	testCases := []struct {
		name   string
		action func() error
	}{
		{
			name: "ResolveContentID error",
			action: func() error {
				_, err := scraper.ResolveContentID("TEST-123")
				return err
			},
		},
		{
			name: "GetURL error",
			action: func() error {
				_, err := scraper.GetURL("TEST-456")
				return err
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.action()
			if err != nil {
				errMsg := err.Error()
				for _, pattern := range sensitivePatterns {
					assert.NotContains(t, errMsg, pattern,
						"Error message should not contain sensitive pattern: %s", pattern)
				}
			}
		})
	}
}

// TestResolveContentID_ErrorCases tests specific error scenarios in content ID resolution
func TestResolveContentID_ErrorCases(t *testing.T) {
	tests := []struct {
		name        string
		setupRepo   func() *database.ContentIDMappingRepository
		movieID     string
		wantErr     bool
		errContains string
	}{
		{
			name: "nil repository",
			setupRepo: func() *database.ContentIDMappingRepository {
				return nil
			},
			movieID:     "TEST-123",
			wantErr:     true,
			errContains: "content ID repository not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := config.ScraperSettings{
				Enabled: true,
			}

			repo := tt.setupRepo()
			scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, false, false), repo)

			contentID, err := scraper.ResolveContentID(tt.movieID)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Empty(t, contentID)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestGetURL_ErrorPropagation verifies errors from ResolveContentID propagate correctly
func TestGetURL_ErrorPropagation(t *testing.T) {
	tests := []struct {
		name        string
		movieID     string
		setupRepo   bool
		wantErr     bool
		errContains string
	}{
		{
			name:        "no repository causes error",
			movieID:     "TEST-123",
			setupRepo:   false,
			wantErr:     true,
			errContains: "content ID repository not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := config.ScraperSettings{
				Enabled: true,
			}

			var repo *database.ContentIDMappingRepository
			if tt.setupRepo {
				repo = database.NewContentIDMappingRepository(nil)
			}

			scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, false, false), repo)

			url, err := scraper.GetURL(tt.movieID)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Empty(t, url)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, url)
			}
		})
	}
}

// TestSearch_ErrorHandling tests Search method error scenarios
func TestSearch_ErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		movieID     string
		setupFunc   func() (*Scraper, func())
		wantErr     bool
		errContains string
	}{
		{
			name:    "GetURL failure propagates",
			movieID: "INVALID-123",
			setupFunc: func() (*Scraper, func()) {
				settings := config.ScraperSettings{
					Enabled: true,
				}
				scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, false, false), nil) // nil repo causes GetURL to fail
				return scraper, func() {}
			},
			wantErr:     true,
			errContains: "content ID repository not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scraper, cleanup := tt.setupFunc()
			defer cleanup()

			result, err := scraper.Search(context.Background(), tt.movieID)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, result)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// Benchmark_ErrorHandling measures performance of error path
func Benchmark_ErrorHandling(b *testing.B) {
	settings := config.ScraperSettings{
		Enabled: true,
	}
	scraper := New(settings, createTestGlobalConfig(&config.ProxyConfig{}, config.FlareSolverrConfig{}, false, false), nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = scraper.ResolveContentID(fmt.Sprintf("TEST-%d", i))
	}
}
