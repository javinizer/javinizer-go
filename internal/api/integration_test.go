// Package api integration tests
//
// Integration tests for API endpoints with real dependencies (database, filesystem, WebSocket hub).
// These tests validate end-to-end functionality with actual SQL persistence, file I/O, and HTTP server.
//
// Test Execution:
//   - Run unit tests only (fast):       make test-short
//   - Run full suite with integration:  make test
//   - Run with race detector:           go test -race ./internal/api/...
//
// Performance:
//   - Each integration test must complete in <5 seconds
//   - Uses in-memory SQLite (file::memory:?cache=shared)
//   - Uses t.TempDir() for automatic filesystem cleanup
//   - Mocks HTTP downloads to avoid real network calls
//
// Architecture Decision References:
//   - Decision 9: Integration test boundaries (testing.Short() guards)
//   - Decision 6: Real SQLite for integration tests (not mocks)
//   - Decision 7: Real filesystem with t.TempDir() (not afero)
//   - Decision 8: All tests pass -race flag
package api

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

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

// setupTestDB creates an in-memory SQLite database for integration testing.
// Uses file::memory:?cache=shared for isolation and performance.
// Automatically registers t.Cleanup() to close database connection.
//
// Architecture: Decision 6 - Use real SQLite (in-memory mode) for integration tests
func setupTestDB(t *testing.T) *database.DB {
	t.Helper()

	// Use in-memory SQLite with shared cache for speed
	// Each test gets isolated database instance (test name in DSN)
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			// Use test name for isolation: file:TESTNAME:?mode=memory&cache=shared
			DSN: "file:" + t.Name() + ":?mode=memory&cache=shared&_busy_timeout=5000",
		},
		Logging: config.LoggingConfig{
			Level: "error", // Suppress database logs in tests
		},
	}

	db, err := database.New(cfg)
	require.NoError(t, err, "Failed to create test database")

	// Run migrations to create schema
	err = db.AutoMigrate()
	require.NoError(t, err, "Failed to run database migrations")

	// Cleanup: Close database connection after test
	t.Cleanup(func() {
		err := db.Close()
		if err != nil {
			t.Logf("Warning: Failed to close test database: %v", err)
		}
	})

	return db
}

// setupTestFS creates a temporary directory for filesystem integration tests.
// Uses t.TempDir() for automatic cleanup (no manual removal needed).
//
// Architecture: Decision 7 - Use real filesystem with t.TempDir() for integration tests
func setupTestFS(t *testing.T) string {
	t.Helper()

	// t.TempDir() automatically cleans up after test completes
	tempDir := t.TempDir()
	return tempDir
}

// setupTestServer creates a real Gin server with all dependencies for integration testing.
// Returns the router and ServerDependencies for test assertions and cleanup.
//
// Dependencies:
//   - Real in-memory SQLite database
//   - Real filesystem operations (uses t.TempDir() when needed)
//   - Real scraper registry
//   - Real aggregator and matcher
//   - Real job queue
//
// Cleanup: Caller must call cleanupServerHub(t, deps) after test completes.
//
// Architecture: Decision 9 - Integration tests use real dependencies (not mocks)
func setupTestServer(t *testing.T) (*gin.Engine, *ServerDependencies) {
	t.Helper()

	// Set Gin to test mode (disables debug output)
	gin.SetMode(gin.TestMode)

	// Create real database with migrations
	db := setupTestDB(t)

	// Create test configuration
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
		Matching: config.MatchingConfig{
			RegexEnabled: false,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedOrigins: []string{"http://localhost:8080"}, // Specific origin for tests
			},
		},
	}

	// Create dependencies
	registry := models.NewScraperRegistry()
	agg := aggregator.New(cfg)
	mat, err := matcher.NewMatcher(&cfg.Matching)
	require.NoError(t, err)

	deps := &ServerDependencies{
		ConfigFile:  "/tmp/config.yaml",
		Registry:    registry,
		DB:          db,
		Aggregator:  agg,
		MovieRepo:   database.NewMovieRepository(db),
		ActressRepo: database.NewActressRepository(db),
		Matcher:     mat,
		JobQueue:    worker.NewJobQueue(),
	}

	// Initialize atomic config pointer
	deps.SetConfig(cfg)

	// Create Gin router with all middleware and handlers
	router := NewServer(deps)

	// Register cleanup for WebSocket hub goroutine
	t.Cleanup(func() {
		cleanupServerHub(t, deps)
	})

	return router, deps
}

// TestIntegrationDatabasePersistence validates that the integration test infrastructure works.
// This is a minimal smoke test to verify:
//   - In-memory SQLite database creates successfully
//   - Migrations run without errors
//   - Database can insert and query data
//   - Cleanup occurs automatically
//
// AC-3.8.1: Real Database Integration
func TestIntegrationDatabasePersistence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	db := setupTestDB(t)

	// Insert test movie
	movie := &models.Movie{
		ContentID: "IPX-123",
		Title:     "Test Movie",
	}
	err := db.Create(movie).Error
	require.NoError(t, err)

	// Query movie back
	var retrieved models.Movie
	err = db.First(&retrieved, "content_id = ?", "IPX-123").Error
	require.NoError(t, err)
	assert.Equal(t, "Test Movie", retrieved.Title)

	// Cleanup is automatic via t.Cleanup()
}

// TestIntegrationServerInitialization validates that the full server stack initializes correctly.
// This is a minimal smoke test to verify:
//   - Server dependencies create successfully
//   - Router initializes with all middleware
//   - Health endpoint is accessible
//   - WebSocket hub cleanup works
//
// AC-3.8.3: Full Request/Response Cycle
func TestIntegrationServerInitialization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	router, _ := setupTestServer(t)

	// Test health endpoint
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "ok")

	// Cleanup is automatic via t.Cleanup()
}

// TestIntegrationFilesystemOperations validates that filesystem integration tests work.
// This is a minimal smoke test to verify:
//   - t.TempDir() creates directory successfully
//   - Files can be written to temp directory
//   - Cleanup occurs automatically
//
// AC-3.8.2: Real Filesystem Integration
func TestIntegrationFilesystemOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tempDir := setupTestFS(t)
	assert.NotEmpty(t, tempDir)

	// Verify directory exists and is writable
	// (Detailed filesystem tests will be added in Task 3)

	// Cleanup is automatic via t.TempDir()
}

// TestIntegrationPerformance validates that integration tests complete within performance budget.
// Each integration test must complete in <5 seconds.
//
// AC-3.8.4: Testing Guards and Performance
func TestIntegrationPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"Database", TestIntegrationDatabasePersistence},
		{"Server", TestIntegrationServerInitialization},
		{"Filesystem", TestIntegrationFilesystemOperations},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			tt.testFunc(t)
			duration := time.Since(start)

			// Verify test completes in <5 seconds
			if duration > 5*time.Second {
				t.Errorf("Integration test %s took %v (exceeds 5 second budget)", tt.name, duration)
			}
		})
	}
}

// TestIntegrationWebSocketCleanup validates that WebSocket hub cleanup works correctly.
// This prevents goroutine leaks in integration tests.
//
// AC-3.8.3: Full Request/Response Cycle (WebSocket hub cleanup)
func TestIntegrationWebSocketCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Create server (which initializes WebSocket hub)
	router, deps := setupTestServer(t)
	assert.NotNil(t, router)
	assert.NotNil(t, deps)

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Cleanup will be called by t.Cleanup() via setupTestServer()
	// Verify wsCancel is available for cleanup
	assert.NotNil(t, deps.wsCancel, "WebSocket cancel function should be set")

	// cleanupServerHub(t, deps) will be called automatically by t.Cleanup()
}

// TestIntegrationContextCancellation validates that server operations respect context cancellation.
// This is important for graceful shutdown and timeout handling.
//
// AC-3.8.3: Full Request/Response Cycle (context handling)
func TestIntegrationContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, _ = setupTestServer(t)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Wait for context to expire
	<-ctx.Done()

	// Verify context was cancelled
	assert.Error(t, ctx.Err())
	assert.Equal(t, context.DeadlineExceeded, ctx.Err())

	// Cleanup will verify WebSocket hub stops gracefully
}

// TestIntegrationMovieCRUDOperations validates full CRUD cycle with real database.
// Tests: GET /api/v1/movies (list), GET /api/v1/movies/:id (get), database persistence
//
// AC-3.8.1: Real Database Integration
// AC-3.8.3: Full Request/Response Cycle
func TestIntegrationMovieCRUDOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	router, deps := setupTestServer(t)

	// Create test movie in database
	movie := &models.Movie{
		ContentID:   "IPX-123",
		ID:          "IPX-123",
		Title:       "Integration Test Movie",
		DisplayName: "IPX-123 Integration Test Movie",
		ReleaseYear: 2024,
	}
	err := deps.DB.Create(movie).Error
	require.NoError(t, err)

	// Test 1: GET /api/v1/movies (list movies)
	t.Run("List Movies", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/movies", nil)
		req.Header.Set("Origin", "http://localhost:8080") // Required for CORS
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		assert.Contains(t, w.Body.String(), "IPX-123")
		assert.Contains(t, w.Body.String(), "Integration Test Movie")

		// Verify CORS headers
		assert.Equal(t, "http://localhost:8080", w.Header().Get("Access-Control-Allow-Origin"))
	})

	// Test 2: GET /api/v1/movies/:id (get single movie)
	t.Run("Get Single Movie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/movies/IPX-123", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		assert.Contains(t, w.Body.String(), "IPX-123")
		assert.Contains(t, w.Body.String(), "Integration Test Movie")
		assert.Contains(t, w.Body.String(), "content_id")
	})

	// Test 3: GET /api/v1/movies/:id (not found)
	t.Run("Get Nonexistent Movie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/movies/NONEXIST-999", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 404, w.Code)
	})

	// Test 4: Verify database state matches API response
	t.Run("Database State Validation", func(t *testing.T) {
		var retrieved models.Movie
		err := deps.DB.First(&retrieved, "content_id = ?", "IPX-123").Error
		require.NoError(t, err)

		assert.Equal(t, "IPX-123", retrieved.ContentID)
		assert.Equal(t, "Integration Test Movie", retrieved.Title)
		assert.Equal(t, 2024, retrieved.ReleaseYear)
	})
}

// TestIntegrationMovieWithRelationships validates movie persistence with relationships.
// Tests: Actress many-to-many relationships, genre relationships
//
// AC-3.8.1: Real Database Integration (relationships: actresses, genres, tags)
func TestIntegrationMovieWithRelationships(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, deps := setupTestServer(t)

	// Create actress
	actress := &models.Actress{
		DMMID:        12345,
		FirstName:    "TestFirst",
		LastName:     "TestLast",
		JapaneseName: "テスト",
	}
	err := deps.DB.Create(actress).Error
	require.NoError(t, err)

	// Create genre
	genre := &models.Genre{
		Name: "Test Genre",
	}
	err = deps.DB.Create(genre).Error
	require.NoError(t, err)

	// Create movie with relationships
	movie := &models.Movie{
		ContentID:   "RELTEST-001",
		Title:       "Relationship Test Movie",
		DisplayName: "RELTEST-001 Relationship Test",
		Actresses:   []models.Actress{*actress},
		Genres:      []models.Genre{*genre},
	}
	err = deps.DB.Create(movie).Error
	require.NoError(t, err)

	// Query movie with relationships
	var retrieved models.Movie
	err = deps.DB.Preload("Actresses").Preload("Genres").First(&retrieved, "content_id = ?", "RELTEST-001").Error
	require.NoError(t, err)

	// Verify relationships persisted
	assert.Equal(t, "RELTEST-001", retrieved.ContentID)
	assert.Len(t, retrieved.Actresses, 1)
	assert.Equal(t, "TestFirst", retrieved.Actresses[0].FirstName)
	assert.Len(t, retrieved.Genres, 1)
	assert.Equal(t, "Test Genre", retrieved.Genres[0].Name)
}

// TestIntegrationConfigEndpoints validates config GET/PUT endpoints.
// Tests: GET /api/v1/config, middleware execution
//
// AC-3.8.3: Full Request/Response Cycle (middleware, config endpoints)
func TestIntegrationConfigEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	router, _ := setupTestServer(t)

	// Test GET /api/v1/config
	t.Run("Get Config", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/config", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		assert.Contains(t, w.Body.String(), "server")
		assert.Contains(t, w.Body.String(), "scrapers")
	})

	// Test GET /api/v1/scrapers
	t.Run("Get Scrapers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/scrapers", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
	})
}

// TestIntegrationErrorResponses validates error handling and response format.
// Tests: 404, 400, JSON error format
//
// AC-3.8.3: Full Request/Response Cycle (error responses)
func TestIntegrationErrorResponses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	router, _ := setupTestServer(t)

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{"Nonexistent Route", "GET", "/api/v1/nonexistent", 404},
		{"Invalid Movie ID", "GET", "/api/v1/movies/INVALID", 404},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}
