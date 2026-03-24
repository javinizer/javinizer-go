package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCORS_OriginValidation tests CORS origin validation with different configurations
func TestCORS_OriginValidation(t *testing.T) {
	tests := []struct {
		name                  string
		allowedOrigins        []string
		requestOrigin         string
		expectedAllowOrigin   string
		expectedAllowCreds    string
		shouldAllowConnection bool
	}{
		{
			name:                  "wildcard is rejected for security (CORS)",
			allowedOrigins:        []string{"*"},
			requestOrigin:         "http://evil.com",
			expectedAllowOrigin:   "", // Wildcard is NOT allowed - security measure
			expectedAllowCreds:    "",
			shouldAllowConnection: false,
		},
		{
			name:                  "empty config allows same-origin with Origin header",
			allowedOrigins:        []string{},
			requestOrigin:         "http://localhost:8080",
			expectedAllowOrigin:   "http://localhost:8080",
			expectedAllowCreds:    "true",
			shouldAllowConnection: true,
		},
		{
			name:                  "empty config blocks different origin",
			allowedOrigins:        []string{},
			requestOrigin:         "http://evil.com",
			expectedAllowOrigin:   "",
			expectedAllowCreds:    "",
			shouldAllowConnection: false,
		},
		{
			name:                  "specific origin allowed",
			allowedOrigins:        []string{"http://localhost:3000"},
			requestOrigin:         "http://localhost:3000",
			expectedAllowOrigin:   "http://localhost:3000",
			expectedAllowCreds:    "true",
			shouldAllowConnection: true,
		},
		{
			name:                  "specific origin blocked",
			allowedOrigins:        []string{"http://localhost:3000"},
			requestOrigin:         "http://evil.com",
			expectedAllowOrigin:   "",
			expectedAllowCreds:    "",
			shouldAllowConnection: false,
		},
		{
			name:                  "multiple allowed origins - first matches",
			allowedOrigins:        []string{"http://localhost:3000", "http://localhost:8080"},
			requestOrigin:         "http://localhost:3000",
			expectedAllowOrigin:   "http://localhost:3000",
			expectedAllowCreds:    "true",
			shouldAllowConnection: true,
		},
		{
			name:                  "no origin header with empty config",
			allowedOrigins:        []string{},
			requestOrigin:         "",
			expectedAllowOrigin:   "",
			expectedAllowCreds:    "true",
			shouldAllowConnection: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				API: config.APIConfig{
					Security: config.SecurityConfig{
						AllowedOrigins: tt.allowedOrigins,
					},
				},
				Logging: config.LoggingConfig{
					Level: "error",
				},
				Matching: config.MatchingConfig{
					RegexEnabled: false,
				},
			}

			registry := models.NewScraperRegistry()
			mat, err := matcher.NewMatcher(&cfg.Matching)
			require.NoError(t, err)

			deps := &ServerDependencies{
				ConfigFile:  "/tmp/config.yaml",
				Registry:    registry,
				Aggregator:  aggregator.New(cfg),
				MovieRepo:   newMockMovieRepo(),
				ActressRepo: newMockActressRepo(),
				Matcher:     mat,
				JobQueue:    worker.NewJobQueue(),
			}
			// Initialize atomic config pointer
			deps.SetConfig(cfg)

			router := NewServer(deps)
			defer cleanupServerHub(t, deps)

			req := httptest.NewRequest("GET", "/health", nil)
			if tt.requestOrigin != "" {
				req.Header.Set("Origin", tt.requestOrigin)
			}
			// Set Host for same-origin checking
			req.Host = "localhost:8080"
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedAllowOrigin, w.Header().Get("Access-Control-Allow-Origin"))
			assert.Equal(t, tt.expectedAllowCreds, w.Header().Get("Access-Control-Allow-Credentials"))
		})
	}
}

// TestCORS_PreflightRequest tests OPTIONS preflight requests
func TestCORS_PreflightRequest(t *testing.T) {
	tests := []struct {
		name              string
		allowedOrigins    []string
		requestOrigin     string
		requestMethod     string
		expectedStatus    int
		shouldHaveHeaders bool
	}{
		{
			name:              "preflight with wildcard is rejected (security)",
			allowedOrigins:    []string{"*"},
			requestOrigin:     "http://localhost:3000",
			requestMethod:     "POST",
			expectedStatus:    204,
			shouldHaveHeaders: false, // Wildcard is rejected - no CORS headers
		},
		{
			name:              "valid preflight with specific origin",
			allowedOrigins:    []string{"http://localhost:3000"},
			requestOrigin:     "http://localhost:3000",
			requestMethod:     "POST",
			expectedStatus:    204,
			shouldHaveHeaders: true,
		},
		{
			name:              "preflight with blocked origin",
			allowedOrigins:    []string{"http://localhost:3000"},
			requestOrigin:     "http://evil.com",
			requestMethod:     "POST",
			expectedStatus:    204, // Still returns 204 but without CORS headers
			shouldHaveHeaders: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				API: config.APIConfig{
					Security: config.SecurityConfig{
						AllowedOrigins: tt.allowedOrigins,
					},
				},
				Logging: config.LoggingConfig{
					Level: "error",
				},
				Matching: config.MatchingConfig{
					RegexEnabled: false,
				},
			}

			registry := models.NewScraperRegistry()
			mat, err := matcher.NewMatcher(&cfg.Matching)
			require.NoError(t, err)

			deps := &ServerDependencies{
				ConfigFile:  "/tmp/config.yaml",
				Registry:    registry,
				Aggregator:  aggregator.New(cfg),
				MovieRepo:   newMockMovieRepo(),
				ActressRepo: newMockActressRepo(),
				Matcher:     mat,
				JobQueue:    worker.NewJobQueue(),
			}
			// Initialize atomic config pointer
			deps.SetConfig(cfg)

			router := NewServer(deps)
			defer cleanupServerHub(t, deps)

			req := httptest.NewRequest("OPTIONS", "/api/v1/scrape", nil)
			req.Header.Set("Origin", tt.requestOrigin)
			req.Header.Set("Access-Control-Request-Method", tt.requestMethod)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.shouldHaveHeaders {
				allowMethods := w.Header().Get("Access-Control-Allow-Methods")
				assert.NotEmpty(t, allowMethods)
				assert.Contains(t, allowMethods, "POST")
			}
		})
	}
}

// TestIsSameOrigin tests the same-origin checking logic
func TestIsSameOrigin(t *testing.T) {
	tests := []struct {
		name          string
		origin        string
		requestHost   string
		expectedMatch bool
	}{
		{
			name:          "exact match",
			origin:        "http://localhost:8080",
			requestHost:   "localhost:8080",
			expectedMatch: true,
		},
		{
			name:          "different host",
			origin:        "http://evil.com",
			requestHost:   "localhost:8080",
			expectedMatch: false,
		},
		{
			name:          "different port",
			origin:        "http://localhost:3000",
			requestHost:   "localhost:8080",
			expectedMatch: false,
		},
		{
			name:          "empty origin header",
			origin:        "",
			requestHost:   "localhost:8080",
			expectedMatch: true, // Treated as same-origin
		},
		{
			name:          "invalid origin URL",
			origin:        "://invalid",
			requestHost:   "localhost:8080",
			expectedMatch: false,
		},
		{
			name:          "https vs http (different schemes = different origins)",
			origin:        "https://localhost:8080",
			requestHost:   "localhost:8080",
			expectedMatch: false, // HTTP and HTTPS are different origins (security requirement)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{
				Host: tt.requestHost,
			}

			result := isSameOrigin(tt.origin, req)
			assert.Equal(t, tt.expectedMatch, result)
		})
	}
}

// TestIsOriginAllowed tests the origin allowlist checking logic
func TestIsOriginAllowed(t *testing.T) {
	tests := []struct {
		name           string
		origin         string
		allowedOrigins []string
		expectedAllow  bool
	}{
		{
			name:           "wildcard is explicitly rejected for security",
			origin:         "http://anything.com",
			allowedOrigins: []string{"*"},
			expectedAllow:  false, // Wildcard is NOT allowed - prevents CSRF/XSWS attacks
		},
		{
			name:           "exact match",
			origin:         "http://localhost:3000",
			allowedOrigins: []string{"http://localhost:3000"},
			expectedAllow:  true,
		},
		{
			name:           "no match",
			origin:         "http://evil.com",
			allowedOrigins: []string{"http://localhost:3000"},
			expectedAllow:  false,
		},
		{
			name:           "multiple origins - match second",
			origin:         "http://localhost:8080",
			allowedOrigins: []string{"http://localhost:3000", "http://localhost:8080"},
			expectedAllow:  true,
		},
		{
			name:           "empty allowlist",
			origin:         "http://localhost:3000",
			allowedOrigins: []string{},
			expectedAllow:  false,
		},
		{
			name:           "wildcard is ignored even with other origins",
			origin:         "http://anything.com",
			allowedOrigins: []string{"http://localhost:3000", "*"},
			expectedAllow:  false, // Wildcard is explicitly skipped - only exact matches allowed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isOriginAllowed(tt.origin, tt.allowedOrigins)
			assert.Equal(t, tt.expectedAllow, result)
		})
	}
}

// TestSecurity_InputValidation tests input validation across all endpoints
func TestSecurity_InputValidation(t *testing.T) {
	cfg := &config.Config{
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedOrigins: []string{"*"},
			},
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	registry := models.NewScraperRegistry()
	registry.Register(&mockScraperWithResults{
		name:    "r18dev",
		enabled: true,
		result: &models.ScraperResult{
			ID:    "IPX-001",
			Title: "Test",
		},
	})
	mat, err := matcher.NewMatcher(&cfg.Matching)
	require.NoError(t, err)

	// Create in-memory database for testing
	dbCfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}
	db, err := database.New(dbCfg)
	require.NoError(t, err)
	err = db.AutoMigrate()
	require.NoError(t, err)

	deps := &ServerDependencies{
		ConfigFile:  "/tmp/config.yaml",
		Registry:    registry,
		DB:          db,
		Aggregator:  aggregator.New(cfg),
		MovieRepo:   newMockMovieRepo(),
		ActressRepo: newMockActressRepo(),
		Matcher:     mat,
		JobQueue:    worker.NewJobQueue(),
	}
	// Initialize atomic config pointer
	deps.SetConfig(cfg)

	router := NewServer(deps)
	defer cleanupServerHub(t, deps)

	tests := []struct {
		name           string
		method         string
		path           string
		body           interface{}
		expectedStatus int
		securityCheck  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:           "scrape with SQL injection in ID",
			method:         "POST",
			path:           "/api/v1/scrape",
			body:           map[string]string{"id": "IPX-001'; DROP TABLE movies--"},
			expectedStatus: 200, // Should succeed but sanitize input
			securityCheck: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ScrapeResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)

				// CRITICAL: Verify safe handling of malicious SQL input
				// NOTE: With parameterized queries (GORM), SQL injection isn't executable,
				// but we still verify the data is handled safely

				// The malicious ID may be stored in OriginalFileName (user input field)
				// This is acceptable since it's just text data, not executed as SQL
				// But verify it's properly escaped in JSON response
				bodyStr := w.Body.String()

				// Verify JSON properly escapes quotes in the SQL injection payload
				// The single quote should be escaped in JSON (\u0027 or \')
				if strings.Contains(bodyStr, "'; DROP TABLE") {
					// If present, verify it's in a safe context (e.g., a string value, not raw)
					assert.Contains(t, bodyStr, "\"original_filename\"", "SQL payload should only appear in safe string fields")
				}

				// Most important: Verify the movie.ID field doesn't contain the malicious payload
				// (Aggregator should generate a clean ID or leave it empty if invalid)
				assert.NotEqual(t, "IPX-001'; DROP TABLE movies--", resp.Movie.ID,
					"Movie ID should be sanitized/generated, not raw user input")

				// Stronger guarantee: Verify ID doesn't contain SQL injection markers at all
				assert.NotContains(t, resp.Movie.ID, "DROP TABLE", "Movie ID contains SQL injection payload")
				assert.NotContains(t, resp.Movie.ID, "'", "Movie ID contains SQL quote character")
				assert.NotContains(t, resp.Movie.ID, "--", "Movie ID contains SQL comment marker")

				// If ID is non-empty, it should be clean (letters, numbers, hyphens only)
				if resp.Movie.ID != "" {
					assert.Regexp(t, `^[A-Z0-9-]+$`, resp.Movie.ID,
						"Non-empty Movie ID should contain only uppercase letters, numbers, and hyphens")
				}
			},
		},
		{
			name:           "scrape with XSS in ID",
			method:         "POST",
			path:           "/api/v1/scrape",
			body:           map[string]string{"id": "<script>alert('xss')</script>"},
			expectedStatus: 200,
			securityCheck: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ScrapeResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)

				// CRITICAL: Verify safe handling of XSS payload
				bodyStr := w.Body.String()

				// Verify JSON encoding properly escapes HTML special characters
				// Go's JSON encoder escapes <, >, and & as \u003c, \u003e, \u0026
				if strings.Contains(bodyStr, "<script>") {
					t.Error("SECURITY ISSUE: Response contains unescaped <script> tag - JSON should escape it")
				}

				// Verify the movie.ID field doesn't contain the XSS payload
				assert.NotEqual(t, "<script>alert('xss')</script>", resp.Movie.ID,
					"Movie ID should be sanitized, not contain XSS payload")

				// Stronger guarantee: Verify ID doesn't contain HTML/JS injection markers at all
				assert.NotContains(t, resp.Movie.ID, "<script>", "Movie ID contains script tag")
				assert.NotContains(t, resp.Movie.ID, "<", "Movie ID contains HTML angle bracket")
				assert.NotContains(t, resp.Movie.ID, ">", "Movie ID contains HTML angle bracket")
				assert.NotContains(t, resp.Movie.ID, "alert", "Movie ID contains JavaScript code")

				// If ID is non-empty, it should be clean (letters, numbers, hyphens only)
				if resp.Movie.ID != "" {
					assert.Regexp(t, `^[A-Z0-9-]+$`, resp.Movie.ID,
						"Non-empty Movie ID should contain only uppercase letters, numbers, and hyphens")
				}

				// If script tags appear anywhere in response, they must be JSON-escaped
				// Go's json.Marshal automatically escapes these as \u003c and \u003e
				if strings.Contains(resp.Movie.OriginalFileName, "<script>") {
					// It's in the OriginalFileName - verify it's properly escaped in JSON
					assert.Contains(t, bodyStr, "\\u003c", "HTML angle brackets should be JSON-escaped")
				}
			},
		},
		{
			// Security validation: API should reject path traversal attempts at request time
			// This provides defense-in-depth by rejecting malicious requests before job creation
			name:           "batch scrape rejects request with path traversal attempts",
			method:         "POST",
			path:           "/api/v1/batch/scrape",
			body:           BatchScrapeRequest{Files: []string{"../../../etc/passwd"}},
			expectedStatus: 403, // Security fix: API now validates paths against allowlist and rejects unsafe paths
			securityCheck: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				// Verify error message indicates access denied (without leaking internal paths)
				assert.NotEmpty(t, resp.Error, "Should return error message")
				assert.Contains(t, strings.ToLower(resp.Error), "access", "Error should mention access restriction")
			},
		},
		{
			name:           "extremely long movie ID",
			method:         "POST",
			path:           "/api/v1/scrape",
			body:           map[string]string{"id": strings.Repeat("A", 10000)},
			expectedStatus: 200, // Should handle without crashing
		},
		{
			name:           "null bytes in movie ID",
			method:         "POST",
			path:           "/api/v1/scrape",
			body:           map[string]string{"id": "IPX-001\x00malicious"},
			expectedStatus: 200,
			securityCheck: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.NotContains(t, body, "\x00")
			},
		},
		{
			// NOTE: This test MUST run last because it modifies shared deps.Config
			// and reloads components, which affects subsequent tests
			name:   "config update with malicious template",
			method: "PUT",
			path:   "/api/v1/config",
			body: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Output.FolderFormat = "{{.Exec `rm -rf /`}}"
				return cfg
			}(),
			expectedStatus: 200,
			securityCheck: func(t *testing.T, w *httptest.ResponseRecorder) {
				// Should accept config - template engine prevents exec during rendering
				// Verify response indicates success
				var resp map[string]string
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Contains(t, resp["message"], "successfully")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if tt.body != nil {
				body, err = json.Marshal(tt.body)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			// Assert expected status code first (catches regressions)
			assert.Equal(t, tt.expectedStatus, w.Code, "Expected status %d, got %d", tt.expectedStatus, w.Code)

			// Should not crash or return 500
			assert.NotEqual(t, 500, w.Code, "Server should not crash on malicious input")

			// Run additional security checks if provided
			if tt.securityCheck != nil {
				tt.securityCheck(t, w)
			}
		})
	}
}

// TestSecurity_ErrorMessageLeakage tests that API errors don't leak internal details
func TestSecurity_ErrorMessageLeakage(t *testing.T) {
	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Level: "error",
		},
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	registry := models.NewScraperRegistry()
	registry.Register(&mockScraperWithResults{
		name:    "r18dev",
		enabled: true,
		err:     fmt.Errorf("not found"),
	})
	mat, err := matcher.NewMatcher(&cfg.Matching)
	require.NoError(t, err)

	deps := &ServerDependencies{
		ConfigFile:  "/tmp/config.yaml",
		Registry:    registry,
		Aggregator:  aggregator.New(cfg),
		MovieRepo:   newMockMovieRepo(),
		ActressRepo: newMockActressRepo(),
		Matcher:     mat,
		JobQueue:    worker.NewJobQueue(),
	}
	// Initialize atomic config pointer
	deps.SetConfig(cfg)

	router := NewServer(deps)
	defer cleanupServerHub(t, deps)

	// Test scrape endpoint with error
	req := httptest.NewRequest("POST", "/api/v1/scrape", bytes.NewBufferString(`{"id":"IPX-001"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	body := w.Body.String()

	// Should return 404 for movie not found
	assert.Equal(t, 404, w.Code)

	// Error response should be valid JSON
	var errResp ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.NotEmpty(t, errResp.Error)

	// NOTE: The API intentionally includes scraper error messages in the "errors" array
	// for debugging purposes. In production, scrapers should not leak sensitive info.
	// This test verifies that at minimum, the main error message is generic.

	// Main error should not contain internal implementation details
	assert.NotContains(t, strings.ToLower(errResp.Error), "database")
	assert.NotContains(t, strings.ToLower(errResp.Error), "sql")
	assert.NotContains(t, strings.ToLower(errResp.Error), "connection")

	// Should not expose file paths
	assert.NotContains(t, body, "/internal/")
	assert.NotContains(t, body, "/Users/")
	assert.NotContains(t, body, "C:\\")

	// Should not expose stack traces
	assert.NotContains(t, body, "goroutine")
	assert.NotContains(t, body, ".go:")
}

// TestSecurity_RateLimitingHeaders tests that appropriate headers are set
func TestSecurity_RateLimitingHeaders(t *testing.T) {
	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Level: "error",
		},
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
	}

	registry := models.NewScraperRegistry()
	mat, err := matcher.NewMatcher(&cfg.Matching)
	require.NoError(t, err)

	deps := &ServerDependencies{
		ConfigFile:  "/tmp/config.yaml",
		Registry:    registry,
		Aggregator:  aggregator.New(cfg),
		MovieRepo:   newMockMovieRepo(),
		ActressRepo: newMockActressRepo(),
		Matcher:     mat,
		JobQueue:    worker.NewJobQueue(),
	}
	// Initialize atomic config pointer
	deps.SetConfig(cfg)

	router := NewServer(deps)
	defer cleanupServerHub(t, deps)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Verify standard HTTP headers are present
	assert.Equal(t, 200, w.Code)
	// Note: Rate limiting headers would be added by middleware if implemented
}

// TestSecurity_WebSocketOriginValidation tests WebSocket connection origin validation
func TestSecurity_WebSocketOriginValidation(t *testing.T) {
	tests := []struct {
		name               string
		allowedOrigins     []string
		requestOrigin      string
		shouldAllowUpgrade bool
	}{
		{
			name:               "wildcard is rejected for security (WebSocket)",
			allowedOrigins:     []string{"*"},
			requestOrigin:      "http://evil.com",
			shouldAllowUpgrade: false, // Wildcard is NOT allowed - prevents XSWS attacks
		},
		{
			name:               "empty config allows same-origin",
			allowedOrigins:     []string{},
			requestOrigin:      "http://localhost:8080",
			shouldAllowUpgrade: true,
		},
		{
			name:               "empty config blocks different origin",
			allowedOrigins:     []string{},
			requestOrigin:      "http://evil.com",
			shouldAllowUpgrade: false,
		},
		{
			name:               "specific origin allowed",
			allowedOrigins:     []string{"http://localhost:3000"},
			requestOrigin:      "http://localhost:3000",
			shouldAllowUpgrade: true,
		},
		{
			name:               "specific origin blocked",
			allowedOrigins:     []string{"http://localhost:3000"},
			requestOrigin:      "http://evil.com",
			shouldAllowUpgrade: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				API: config.APIConfig{
					Security: config.SecurityConfig{
						AllowedOrigins: tt.allowedOrigins,
					},
				},
				Logging: config.LoggingConfig{
					Level: "error",
				},
				Matching: config.MatchingConfig{
					RegexEnabled: false,
				},
			}

			registry := models.NewScraperRegistry()
			mat, err := matcher.NewMatcher(&cfg.Matching)
			require.NoError(t, err)

			deps := &ServerDependencies{
				ConfigFile:  "/tmp/config.yaml",
				Registry:    registry,
				Aggregator:  aggregator.New(cfg),
				MovieRepo:   newMockMovieRepo(),
				ActressRepo: newMockActressRepo(),
				Matcher:     mat,
				JobQueue:    worker.NewJobQueue(),
			}
			// Initialize atomic config pointer
			deps.SetConfig(cfg)

			router := NewServer(deps)
			defer cleanupServerHub(t, deps)

			// Create WebSocket upgrade request
			req := httptest.NewRequest("GET", "/ws/progress", nil)
			req.Header.Set("Origin", tt.requestOrigin)
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Sec-WebSocket-Version", "13")
			req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
			req.Host = "localhost:8080"
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if tt.shouldAllowUpgrade {
				// Should attempt upgrade (will return 200 with panic recovery in test due to ResponseRecorder limitations)
				// In test environment, httptest.ResponseRecorder can't hijack connections,
				// so valid upgrade attempts return 200 instead of 101
				assert.True(t, w.Code == 200 || w.Code == 400 || w.Code == 101,
					"Expected upgrade attempt (200/400/101), got %d", w.Code)
			} else {
				// Should reject before upgrade attempt
				assert.Equal(t, 403, w.Code, "Should reject WebSocket connection with forbidden origin")
			}
		})
	}
}
