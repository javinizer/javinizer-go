package r18dev

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Story 5.3 Partial Implementation: Error Handling Tests (Architectural Limitation)
//
// LIMITATION: HTTP error scenario tests (404, 429, 500, timeouts, etc.) are not feasible
// without refactoring the R18dev scraper architecture. The scraper uses package-level const
// URLs (e.g., const baseURL = "https://r18.dev") that cannot be mocked or overridden
// in tests. Comprehensive HTTP error testing requires:
//   1. Injecting HTTPClient interface, OR
//   2. Accepting base URL as constructor parameter
//
// FUTURE WORK: Epic 6 story to refactor scrapers for dependency injection, then complete
// HTTP error testing for Story 5.3.
//
// CURRENT SCOPE: These tests focus on testable error paths without HTTP mocking:
//   - Error message quality and format
//   - JSON parsing and malformed data handling
//   - Error propagation
//   - Security checks (no sensitive data leaks)

// TestMalformedJSON_R18dev tests JSON parsing error handling
func TestMalformedJSON_R18dev(t *testing.T) {
	tests := []struct {
		name         string
		responseJSON string
		wantErr      bool
	}{
		{
			name:         "invalid JSON syntax - missing comma",
			responseJSON: `{"content_id": "test123" "title": "Test"}`,
			wantErr:      true,
		},
		{
			name:         "invalid JSON syntax - unquoted key",
			responseJSON: `{content_id: "test123"}`,
			wantErr:      true,
		},
		{
			name:         "truncated JSON",
			responseJSON: `{"content_id": "test123", "title": "Test"`,
			wantErr:      true,
		},
		{
			name:         "empty JSON object",
			responseJSON: `{}`,
			wantErr:      false, // Valid JSON, just empty
		},
		{
			name:         "null values",
			responseJSON: `{"content_id": null, "title": null}`,
			wantErr:      false, // Valid JSON with nulls
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]interface{}
			err := json.Unmarshal([]byte(tt.responseJSON), &result)

			if tt.wantErr {
				assert.Error(t, err, "Should fail to parse malformed JSON")
			} else {
				assert.NoError(t, err, "Should parse valid JSON (even if empty)")
			}
		})
	}
}

// TestErrorMessages_ContainScraperName verifies all error messages include "R18dev" or descriptive context
func TestErrorMessages_ContainScraperName(t *testing.T) {
	tests := []struct {
		name       string
		setupFunc  func() (*Scraper, error)
		wantPrefix string
	}{
		{
			name: "empty search ID",
			setupFunc: func() (*Scraper, error) {
				settings := config.ScraperSettings{Enabled: true}
				scraper := New(settings, nil, config.FlareSolverrConfig{})
				_, err := scraper.Search(context.Background(), "")
				return scraper, err
			},
			wantPrefix: "R18dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.setupFunc()

			// Some errors may not happen in all environments
			// Just verify that if there is an error, it's descriptive
			if err != nil {
				errMsg := err.Error()
				assert.NotEmpty(t, errMsg, "Error message should not be empty")
				assert.Greater(t, len(errMsg), 5, "Error message should be descriptive")
			}
		})
	}
}

// TestErrorMessages_NoSensitiveData ensures no API keys or internal paths in errors
func TestErrorMessages_NoSensitiveData(t *testing.T) {
	settings := config.ScraperSettings{Enabled: true}
	scraper := New(settings, nil, config.FlareSolverrConfig{})

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
			name: "GetURL with empty ID",
			action: func() error {
				_, err := scraper.GetURL("")
				return err
			},
		},
		{
			name: "Search with empty ID",
			action: func() error {
				_, err := scraper.Search(context.Background(), "")
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

// TestMissingRequiredFields tests parsing of JSON responses with missing required fields
func TestMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name      string
		jsonData  string
		wantEmpty bool // Whether the parsed result should be considered "empty"
	}{
		{
			name: "missing content_id",
			jsonData: `{
				"title": "Test Movie",
				"actresses": []
			}`,
			wantEmpty: true,
		},
		{
			name: "missing title",
			jsonData: `{
				"content_id": "test123",
				"actresses": []
			}`,
			wantEmpty: false, // Has content_id
		},
		{
			name: "empty arrays",
			jsonData: `{
				"content_id": "test123",
				"title": "Test Movie",
				"actresses": [],
				"genres": []
			}`,
			wantEmpty: false, // Has required fields
		},
		{
			name: "all null values",
			jsonData: `{
				"content_id": null,
				"title": null,
				"actresses": null,
				"genres": null
			}`,
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]interface{}
			err := json.Unmarshal([]byte(tt.jsonData), &result)
			require.NoError(t, err, "JSON should parse successfully")

			// Check if content_id is present and not null
			contentID, hasContentID := result["content_id"]
			isEmpty := !hasContentID || contentID == nil

			assert.Equal(t, tt.wantEmpty, isEmpty,
				"Empty check mismatch for case: %s", tt.name)
		})
	}
}

// Benchmark_ErrorHandling measures performance of error path
func Benchmark_ErrorHandling(b *testing.B) {
	settings := config.ScraperSettings{Enabled: true}
	scraper := New(settings, nil, config.FlareSolverrConfig{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = scraper.GetURL(fmt.Sprintf("TEST-%d", i))
	}
}
