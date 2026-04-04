package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	// Test that NewServer properly initializes all routes
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
		Logging: config.LoggingConfig{
			Level: "info",
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
		DB:          nil, // Not needed for route testing
		Aggregator:  aggregator.New(cfg),
		MovieRepo:   newMockMovieRepo(),
		ActressRepo: newMockActressRepo(),
		Matcher:     mat,
		JobQueue:    worker.NewJobQueue(nil),
	}
	// Initialize atomic config pointer
	deps.SetConfig(cfg)

	router := NewServer(deps)
	defer cleanupServerHub(t, deps)
	require.NotNil(t, router)

	// Test that routes are registered
	routes := router.Routes()
	routePaths := make(map[string]bool)
	for _, route := range routes {
		routePaths[route.Path] = true
	}

	// Verify critical endpoints exist
	expectedRoutes := []string{
		"/health",
		"/ws/progress",
		"/api/v1/scrape",
		"/api/v1/movies/:id",
		"/api/v1/movies",
		"/api/v1/actresses",
		"/api/v1/actresses/:id",
		"/api/v1/actresses/search",
		"/api/v1/actresses/merge/preview",
		"/api/v1/actresses/merge",
		"/api/v1/config",
		"/api/v1/scrapers",
		"/api/v1/proxy/test",
		"/api/v1/scan",
		"/api/v1/browse",
		"/api/v1/cwd",
		"/api/v1/batch/scrape",
		"/api/v1/batch/:id",
		"/api/v1/batch/:id/cancel",
		"/api/v1/batch/:id/movies/:movieId",
		"/api/v1/batch/:id/movies/:movieId/poster-crop",
		"/api/v1/batch/:id/movies/:movieId/preview",
		"/api/v1/batch/:id/organize",
	}

	for _, route := range expectedRoutes {
		assert.True(t, routePaths[route], "Route %s should be registered", route)
	}
}

func TestNewServer_RouteParity(t *testing.T) {
	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Level: "info",
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
		JobQueue:    worker.NewJobQueue(nil),
	}
	deps.SetConfig(cfg)

	router := NewServer(deps)
	defer cleanupServerHub(t, deps)

	actual := make([]string, 0, len(router.Routes()))
	for _, route := range router.Routes() {
		actual = append(actual, route.Method+" "+route.Path)
	}

	expected := []string{
		"DELETE /api/v1/actresses/:id",
		"DELETE /api/v1/batch/:id",
		"DELETE /api/v1/history",
		"DELETE /api/v1/history/:id",
		"GET /api/v1/actresses",
		"GET /api/v1/actresses/:id",
		"GET /api/v1/actresses/search",
		"GET /api/v1/auth/status",
		"GET /api/v1/batch",
		"GET /api/v1/batch/:id",
		"GET /api/v1/config",
		"GET /api/v1/cwd",
		"GET /api/v1/history",
		"GET /api/v1/history/stats",
		"GET /api/v1/movies",
		"GET /api/v1/movies/:id",
		"GET /api/v1/posters/:filename",
		"GET /api/v1/scrapers",
		"GET /api/v1/temp/image",
		"GET /api/v1/temp/posters/:jobId/:filename",
		"GET /api/v1/version",
		"GET /docs",
		"GET /docs/openapi.json",
		"GET /health",
		"GET /swagger/*any",
		"GET /ws/progress",
		"HEAD /docs/openapi.json",
		"PATCH /api/v1/batch/:id/movies/:movieId",
		"POST /api/v1/actresses",
		"POST /api/v1/actresses/merge",
		"POST /api/v1/actresses/merge/preview",
		"POST /api/v1/auth/login",
		"POST /api/v1/auth/logout",
		"POST /api/v1/auth/setup",
		"POST /api/v1/batch/:id/cancel",
		"POST /api/v1/batch/:id/movies/:movieId/exclude",
		"POST /api/v1/batch/:id/movies/:movieId/poster-crop",
		"POST /api/v1/batch/:id/movies/:movieId/preview",
		"POST /api/v1/batch/:id/movies/:movieId/rescrape",
		"POST /api/v1/batch/:id/organize",
		"POST /api/v1/batch/:id/update",
		"POST /api/v1/batch/scrape",
		"POST /api/v1/browse",
		"POST /api/v1/browse/autocomplete",
		"POST /api/v1/movies/:id/compare-nfo",
		"POST /api/v1/movies/:id/rescrape",
		"POST /api/v1/proxy/test",
		"POST /api/v1/scan",
		"POST /api/v1/scrape",
		"POST /api/v1/translation/models",
		"POST /api/v1/translation/deepl/usage",
		"POST /api/v1/version/check",
		"PUT /api/v1/actresses/:id",
		"PUT /api/v1/config",
	}

	optionalStaticRoutes := map[string]struct{}{
		"GET /_app/*filepath":  {},
		"HEAD /_app/*filepath": {},
		"GET /robots.txt":      {},
		"GET /favicon.ico":     {},
	}

	expectedSet := make(map[string]struct{}, len(expected))
	for _, route := range expected {
		expectedSet[route] = struct{}{}
	}

	actualSet := make(map[string]struct{}, len(actual))
	for _, route := range actual {
		actualSet[route] = struct{}{}
	}

	for _, route := range expected {
		_, ok := actualSet[route]
		assert.True(t, ok, "missing expected route: %s", route)
	}

	for _, route := range actual {
		_, expectedRoute := expectedSet[route]
		_, optionalRoute := optionalStaticRoutes[route]
		if !expectedRoute && !optionalRoute {
			assert.Failf(t, "unexpected route", "route %s was registered but is not in expected parity list", route)
		}
	}
}

func TestNewServer_PublicAndProtectedSmoke(t *testing.T) {
	router, deps := setupAuthenticatedTestServer(t)
	defer cleanupServerHub(t, deps)

	publicReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	publicW := httptest.NewRecorder()
	router.ServeHTTP(publicW, publicReq)
	assert.Equal(t, http.StatusOK, publicW.Code)

	protectedReq := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	protectedW := httptest.NewRecorder()
	router.ServeHTTP(protectedW, protectedReq)
	assert.Equal(t, http.StatusServiceUnavailable, protectedW.Code)
}

func TestNewServer_CORSHeaders(t *testing.T) {
	cfg := &config.Config{
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedOrigins: []string{"http://localhost:3000"}, // Specific origin for test
			},
		},
		Logging: config.LoggingConfig{
			Level: "info",
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
		JobQueue:    worker.NewJobQueue(nil),
	}
	// Initialize atomic config pointer
	deps.SetConfig(cfg)

	router := NewServer(deps)
	defer cleanupServerHub(t, deps)

	// Test OPTIONS request (CORS preflight)
	req := httptest.NewRequest("OPTIONS", "/api/v1/movies", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 204 and proper CORS headers for allowed origin
	assert.Equal(t, 204, w.Code)
	assert.Equal(t, "http://localhost:3000", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "GET")
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "Content-Type")
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestNewServer_StaticFiles(t *testing.T) {
	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Level: "info",
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
		JobQueue:    worker.NewJobQueue(nil),
	}
	// Initialize atomic config pointer
	deps.SetConfig(cfg)

	router := NewServer(deps)
	defer cleanupServerHub(t, deps)

	// Test that docs endpoint is registered
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return HTML (even if OpenAPI file doesn't exist in test)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Javinizer API Documentation")
}

func TestServeScalarDocs(t *testing.T) {
	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Level: "info",
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
		JobQueue:    worker.NewJobQueue(nil),
	}
	// Initialize atomic config pointer
	deps.SetConfig(cfg)

	router := NewServer(deps)
	defer cleanupServerHub(t, deps)

	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))

	body := w.Body.String()
	assert.Contains(t, body, "<!doctype html>")
	assert.Contains(t, body, "Javinizer API Documentation")
	assert.Contains(t, body, "@scalar/api-reference")
	assert.Contains(t, body, "/docs/openapi.json")
}

func TestLogServerInfo(t *testing.T) {
	// Test that LogServerInfo doesn't panic
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}

	// This should not panic
	assert.NotPanics(t, func() {
		LogServerInfo(cfg)
	})
}

func TestNewServer_GinMode(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		wantMode string
	}{
		{
			name:     "debug mode",
			logLevel: "debug",
			wantMode: "debug",
		},
		{
			name:     "release mode",
			logLevel: "info",
			wantMode: "release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Logging: config.LoggingConfig{
					Level: tt.logLevel,
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
				JobQueue:    worker.NewJobQueue(nil),
			}
			// Initialize atomic config pointer
			deps.SetConfig(cfg)

			router := NewServer(deps)
			defer cleanupServerHub(t, deps)
			require.NotNil(t, router)

			// Router should be created without panic
		})
	}
}

func TestNewServer_AllEndpointsAccessible(t *testing.T) {
	// Integration test - verify all endpoints are accessible (no 404)
	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Level: "info",
		},
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	registry := models.NewScraperRegistry()
	registry.Register(&mockScraper{name: "r18dev", enabled: true})

	mat, err := matcher.NewMatcher(&cfg.Matching)
	require.NoError(t, err)

	// Use in-memory database for testing
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

	deps := &ServerDependencies{
		ConfigFile:  "/tmp/config.yaml",
		Registry:    registry,
		DB:          db,
		Aggregator:  aggregator.New(cfg),
		MovieRepo:   database.NewMovieRepository(db),
		ActressRepo: database.NewActressRepository(db),
		Matcher:     mat,
		JobQueue:    worker.NewJobQueue(nil),
	}
	// Initialize atomic config pointer
	deps.SetConfig(cfg)

	router := NewServer(deps)
	defer cleanupServerHub(t, deps)

	// Test GET endpoints
	getEndpoints := []string{
		"/health",
		"/api/v1/movies",
		"/api/v1/config",
		"/api/v1/scrapers",
		"/api/v1/cwd",
		"/docs",
	}

	for _, endpoint := range getEndpoints {
		t.Run("GET "+endpoint, func(t *testing.T) {
			req := httptest.NewRequest("GET", endpoint, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			// Should not return 404
			assert.NotEqual(t, 404, w.Code, "Endpoint %s should exist", endpoint)
		})
	}
}

func TestNewServer_SecurityHeaders(t *testing.T) {
	// Test that server properly handles security-related scenarios
	cfg := &config.Config{
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedOrigins: []string{"http://localhost:3000"}, // Specific origin for test
			},
		},
		Logging: config.LoggingConfig{
			Level: "info",
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
		JobQueue:    worker.NewJobQueue(nil),
	}
	// Initialize atomic config pointer
	deps.SetConfig(cfg)

	router := NewServer(deps)
	defer cleanupServerHub(t, deps)

	t.Run("CORS rejects wildcard and blocked origins", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		req.Header.Set("Origin", "http://evil.com")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Wildcard "*" is NOT allowed - blocked origins should have no CORS headers
		assert.Equal(t, "", w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("Large request body handling", func(t *testing.T) {
		// Test that server can handle large request bodies without crashing
		largeBody := strings.Repeat("x", 1024*1024) // 1MB
		req := httptest.NewRequest("POST", "/api/v1/scrape", strings.NewReader(largeBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Should return 400 (bad request) not 500 (server error)
		assert.True(t, w.Code == 400 || w.Code == 413, "Should handle large body gracefully")
	})
}

func TestNewServer_InvalidRoutes(t *testing.T) {
	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Level: "info",
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
		JobQueue:    worker.NewJobQueue(nil),
	}
	// Initialize atomic config pointer
	deps.SetConfig(cfg)

	router := NewServer(deps)
	defer cleanupServerHub(t, deps)

	invalidRoutes := []string{
		"/nonexistent",
		"/api/v1/invalid",
		"/api/v2/movies",
		"/../../../etc/passwd",
	}

	for _, route := range invalidRoutes {
		t.Run("Invalid:"+route, func(t *testing.T) {
			req := httptest.NewRequest("GET", route, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			// Should return 404
			assert.Equal(t, 404, w.Code)
		})
	}
}

func TestNewServer_SPARouteFallbackForHTML(t *testing.T) {
	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Level: "info",
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
		JobQueue:    worker.NewJobQueue(nil),
	}
	deps.SetConfig(cfg)

	router := NewServer(deps)
	defer cleanupServerHub(t, deps)

	req := httptest.NewRequest("GET", "/some/spa/route", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)
	if w.Code == 301 || w.Code == 302 || w.Code == 307 || w.Code == 308 {
		location := w.Header().Get("Location")
		if location == "" || location == "." || location == "./" {
			location = "/"
		}
		if location[0] != '/' {
			location = "/" + location
		}
		redirectReq := httptest.NewRequest("GET", location, nil)
		redirectReq.Header.Set("Accept", "text/html")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, redirectReq)
	}

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, strings.ToLower(w.Body.String()), "<!doctype html>")
}

func TestNewServer_RobotsTxtServed(t *testing.T) {
	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Level: "info",
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
		JobQueue:    worker.NewJobQueue(nil),
	}
	deps.SetConfig(cfg)

	router := NewServer(deps)
	defer cleanupServerHub(t, deps)

	req := httptest.NewRequest("GET", "/robots.txt", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "User-agent:")
}

func TestServerDependencies_Shutdown(t *testing.T) {
	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Level: "info",
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
		JobQueue:    worker.NewJobQueue(nil),
	}
	deps.SetConfig(cfg)

	// Create server to initialize wsCancel
	_ = NewServer(deps)

	// Test that Shutdown doesn't panic
	assert.NotPanics(t, func() {
		deps.Shutdown()
	})

	// Test calling Shutdown again (should be idempotent)
	assert.NotPanics(t, func() {
		deps.Shutdown()
	})
}

func TestServerDependencies_ShutdownWithNilCancel(t *testing.T) {
	// Test Shutdown with nil wsCancel
	deps := &ServerDependencies{}

	// Should not panic even if wsCancel is nil
	assert.NotPanics(t, func() {
		deps.Shutdown()
	})
}

func TestServerDependencies_GetSetConfig(t *testing.T) {
	deps := &ServerDependencies{}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 9090,
		},
	}

	// Test SetConfig
	deps.SetConfig(cfg)

	// Test GetConfig
	got := deps.GetConfig()
	assert.Equal(t, cfg.Server.Host, got.Server.Host)
	assert.Equal(t, cfg.Server.Port, got.Server.Port)
}

func TestServerDependencies_GetConfigPanic(t *testing.T) {
	deps := &ServerDependencies{}

	// GetConfig should panic when config is not set
	assert.Panics(t, func() {
		deps.GetConfig()
	})
}

func TestServerDependencies_SetConfigNilPanic(t *testing.T) {
	deps := &ServerDependencies{}

	// SetConfig should panic when given nil config
	assert.Panics(t, func() {
		deps.SetConfig(nil)
	})
}

// TestIsSameOrigin and TestIsOriginAllowed are in handlers_security_test.go

func TestAcceptsHTML(t *testing.T) {
	tests := []struct {
		name     string
		accept   string
		expected bool
	}{
		{
			name:     "empty accept header",
			accept:   "",
			expected: false,
		},
		{
			name:     "text/html only",
			accept:   "text/html",
			expected: true,
		},
		{
			name:     "text/html with quality",
			accept:   "text/html;q=0.9",
			expected: true,
		},
		{
			name:     "text/html with q=0",
			accept:   "text/html;q=0",
			expected: false,
		},
		{
			name:     "application/json only",
			accept:   "application/json",
			expected: false,
		},
		{
			name:     "mixed with html",
			accept:   "text/html, application/json",
			expected: true,
		},
		{
			name:     "wildcard",
			accept:   "*/*",
			expected: false,
		},
		{
			name:     "browser-like accept header",
			accept:   "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := setupTestRouter()
			router.GET("/test", func(c *gin.Context) {
				if acceptsHTML(c) {
					c.String(200, "html")
				} else {
					c.String(200, "other")
				}
			})

			req := httptest.NewRequest("GET", "/test", nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if tt.expected {
				assert.Equal(t, "html", w.Body.String())
			} else {
				assert.Equal(t, "other", w.Body.String())
			}
		})
	}
}

func TestResolveSwaggerPath(t *testing.T) {
	// Test that resolveSwaggerPath returns a valid path
	path := resolveSwaggerPath()

	// Should return either Docker or local path
	assert.True(t,
		path == "/app/docs/swagger/swagger.json" || path == "./docs/swagger/swagger.json",
		"Expected Docker or local swagger path, got: %s", path)
}

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}
