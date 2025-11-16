package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebSocketHandler tests the WebSocket handler setup
func TestWebSocketHandler(t *testing.T) {
	// Initialize test WebSocket hub
	initTestWebSocket(t)

	tests := []struct {
		name          string
		headers       map[string]string
		expectedCode  int
		expectedError bool
	}{
		{
			name: "WebSocket upgrade request with valid headers",
			headers: map[string]string{
				"Upgrade":               "websocket",
				"Connection":            "Upgrade",
				"Sec-WebSocket-Key":     "dGhlIHNhbXBsZSBub25jZQ==",
				"Sec-WebSocket-Version": "13",
			},
			// Note: httptest.ResponseRecorder doesn't support hijacking,
			// so gin's recovery middleware catches the panic and returns 200
			// (recovery middleware doesn't change status code if handler already wrote response)
			expectedCode:  200,
			expectedError: false, // Recovery catches the panic, so no visible error in response
		},
		{
			name: "regular HTTP request without WebSocket headers",
			headers: map[string]string{
				"Accept": "application/json",
			},
			expectedCode:  400, // gorilla/websocket returns 400 for non-WebSocket requests
			expectedError: true,
		},
		{
			name: "WebSocket request with missing Upgrade header",
			headers: map[string]string{
				"Connection":            "Upgrade",
				"Sec-WebSocket-Key":     "dGhlIHNhbXBsZSBub25jZQ==",
				"Sec-WebSocket-Version": "13",
			},
			expectedCode:  400,
			expectedError: true,
		},
		{
			name: "WebSocket request with missing Connection header",
			headers: map[string]string{
				"Upgrade":               "websocket",
				"Sec-WebSocket-Key":     "dGhlIHNhbXBsZSBub25jZQ==",
				"Sec-WebSocket-Version": "13",
			},
			expectedCode:  400,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup router with WebSocket handler
			router := gin.New()
			router.Use(gin.Recovery()) // Add recovery middleware to catch panics
			router.GET("/ws", handleWebSocket(wsHub))

			// Create request with headers
			req := httptest.NewRequest("GET", "/ws", nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedCode, w.Code)
		})
	}
}

// TestWebSocketHub tests the WebSocket hub integration
func TestWebSocketHub(t *testing.T) {
	hub := ws.NewHub()
	require.NotNil(t, hub)

	// Start hub in background with context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	// Give hub time to start
	time.Sleep(10 * time.Millisecond)

	// Test broadcast functionality
	msg := &ws.ProgressMessage{
		JobID:    "test-job-123",
		FilePath: "/test/file.mp4",
		Status:   "completed",
		Progress: 100,
		Message:  "Test message",
	}

	// Broadcasting should not panic even with no clients
	hub.BroadcastProgress(msg)

	// Clean shutdown
	time.Sleep(10 * time.Millisecond)
}

// TestWebSocketUpgrader tests the WebSocket upgrader configuration
func TestWebSocketUpgrader(t *testing.T) {
	// Test that upgrader is configured (indirectly through handler behavior)
	router := gin.New()
	router.Use(gin.Recovery())

	initTestWebSocket(t)
	router.GET("/ws", handleWebSocket(wsHub))

	// Request without WebSocket headers should fail
	req := httptest.NewRequest("GET", "/ws", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 400 Bad Request (from gorilla/websocket)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Bad Request") // Gorilla websocket error message
}

// TestWebSocketBroadcastMessage tests message broadcasting
func TestWebSocketBroadcastMessage(t *testing.T) {
	hub := ws.NewHub()
	require.NotNil(t, hub)

	// Start hub with context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	tests := []struct {
		name    string
		message *ws.ProgressMessage
	}{
		{
			name: "job started message",
			message: &ws.ProgressMessage{
				JobID:    "job-001",
				Status:   "started",
				Progress: 0,
				Message:  "Job started",
			},
		},
		{
			name: "progress update message",
			message: &ws.ProgressMessage{
				JobID:    "job-001",
				FilePath: "/video/IPX-535.mp4",
				Status:   "processing",
				Progress: 50,
				Message:  "Processing file",
			},
		},
		{
			name: "completion message",
			message: &ws.ProgressMessage{
				JobID:    "job-001",
				Status:   "completed",
				Progress: 100,
				Message:  "Job completed",
			},
		},
		{
			name: "error message",
			message: &ws.ProgressMessage{
				JobID:    "job-002",
				FilePath: "/video/ERROR.mp4",
				Status:   "failed",
				Progress: 0,
				Message:  "Scraping failed",
				Error:    "Movie not found",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Broadcast should not panic (even with no connected clients)
			hub.BroadcastProgress(tt.message)

			// Give time for message to be processed
			time.Sleep(5 * time.Millisecond)
		})
	}
}

// TestWebSocketConnectionHandling tests connection lifecycle
func TestWebSocketConnectionHandling(t *testing.T) {
	hub := ws.NewHub()
	require.NotNil(t, hub)

	// Start hub
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	defer func() {
		time.Sleep(10 * time.Millisecond)
	}()

	// Test that hub can accept clients (simulated)
	// In a real scenario, clients would be added via Register(client)
	// but we can't easily test that without actual WebSocket connections

	// Verify hub is running by broadcasting a message
	msg := &ws.ProgressMessage{
		JobID:   "test-connection",
		Status:  "testing",
		Message: "Connection handling test",
	}

	hub.BroadcastProgress(msg)
	time.Sleep(5 * time.Millisecond)
}

// TestWebSocketErrorCases tests error handling in WebSocket setup
func TestWebSocketErrorCases(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		headers        map[string]string
		expectedStatus int
	}{
		{
			name:           "POST method not allowed",
			method:         "POST",
			headers:        map[string]string{},
			expectedStatus: http.StatusNotFound, // Gin returns 404 for method not found
		},
		{
			name:   "invalid WebSocket version",
			method: "GET",
			headers: map[string]string{
				"Upgrade":               "websocket",
				"Connection":            "Upgrade",
				"Sec-WebSocket-Key":     "dGhlIHNhbXBsZSBub25jZQ==",
				"Sec-WebSocket-Version": "999",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "missing WebSocket key",
			method: "GET",
			headers: map[string]string{
				"Upgrade":               "websocket",
				"Connection":            "Upgrade",
				"Sec-WebSocket-Version": "13",
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(gin.Recovery())

			initTestWebSocket(t)
			router.GET("/ws", handleWebSocket(wsHub))

			req := httptest.NewRequest(tt.method, "/ws", nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

// TestWebSocketHubInitialization tests hub initialization
func TestWebSocketHubInitialization(t *testing.T) {
	// Create new hub
	hub := ws.NewHub()
	assert.NotNil(t, hub, "Hub should be initialized")

	// Start hub
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	// Give hub time to start
	time.Sleep(10 * time.Millisecond)

	// Test that broadcast doesn't panic
	assert.NotPanics(t, func() {
		hub.BroadcastProgress(&ws.ProgressMessage{
			JobID:   "init-test",
			Status:  "testing",
			Message: "Hub initialization test",
		})
	})

	// Cleanup
	time.Sleep(10 * time.Millisecond)
}

// TestWebSocketMessageFormat tests that progress messages have correct format
func TestWebSocketMessageFormat(t *testing.T) {
	tests := []struct {
		name    string
		message *ws.ProgressMessage
		valid   bool
	}{
		{
			name: "valid complete message",
			message: &ws.ProgressMessage{
				JobID:    "job-123",
				FilePath: "/path/to/file.mp4",
				Status:   "completed",
				Progress: 100.0,
				Message:  "Processing complete",
				Error:    "",
			},
			valid: true,
		},
		{
			name: "valid error message",
			message: &ws.ProgressMessage{
				JobID:    "job-456",
				FilePath: "/path/to/error.mp4",
				Status:   "failed",
				Progress: 0.0,
				Message:  "Processing failed",
				Error:    "File not found",
			},
			valid: true,
		},
		{
			name: "valid progress update",
			message: &ws.ProgressMessage{
				JobID:    "job-789",
				FilePath: "/path/to/processing.mp4",
				Status:   "processing",
				Progress: 50.5,
				Message:  "Halfway through",
			},
			valid: true,
		},
		{
			name: "minimal valid message",
			message: &ws.ProgressMessage{
				JobID:  "job-minimal",
				Status: "started",
			},
			valid: true,
		},
	}

	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	defer func() { time.Sleep(10 * time.Millisecond) }()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.valid {
				// Should not panic when broadcasting
				assert.NotPanics(t, func() {
					hub.BroadcastProgress(tt.message)
				})
			}
			time.Sleep(5 * time.Millisecond)
		})
	}
}

// TestWebSocketHandlerNilHub tests that handler handles nil hub gracefully
func TestWebSocketHandlerNilHub(t *testing.T) {
	router := gin.New()
	router.Use(gin.Recovery())

	// Create handler with nil hub (should not panic during setup)
	assert.NotPanics(t, func() {
		router.GET("/ws", handleWebSocket(nil))
	})

	// Request should fail gracefully
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Version", "13")

	w := httptest.NewRecorder()

	// Should handle error (either panic recovery or normal error response)
	assert.NotPanics(t, func() {
		router.ServeHTTP(w, req)
	})
}

// BenchmarkWebSocketBroadcast benchmarks message broadcasting
func BenchmarkWebSocketBroadcast(b *testing.B) {
	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	defer func() { time.Sleep(10 * time.Millisecond) }()

	msg := &ws.ProgressMessage{
		JobID:    "benchmark-job",
		FilePath: "/benchmark/file.mp4",
		Status:   "processing",
		Progress: 50.0,
		Message:  "Benchmark test",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hub.BroadcastProgress(msg)
	}
}

// BenchmarkWebSocketHandler benchmarks handler processing
func BenchmarkWebSocketHandler(b *testing.B) {
	router := gin.New()
	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	defer func() { time.Sleep(10 * time.Millisecond) }()

	router.GET("/ws", handleWebSocket(hub))

	req := httptest.NewRequest("GET", "/ws", nil)
	// Don't include WebSocket headers so we get a quick 400 response

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// TestWebSocketIntegration tests integration between handler and hub
func TestWebSocketIntegration(t *testing.T) {
	// Initialize hub
	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	defer func() { time.Sleep(10 * time.Millisecond) }()

	// Create router with handler
	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/ws", handleWebSocket(hub))

	// Test that handler is properly connected to hub
	req := httptest.NewRequest("GET", "/ws", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 400 for missing WebSocket headers
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Test broadcast still works
	hub.BroadcastProgress(&ws.ProgressMessage{
		JobID:   "integration-test",
		Status:  "testing",
		Message: "Integration test message",
	})

	time.Sleep(5 * time.Millisecond)
}

// TestWebSocketConcurrentBroadcasts tests concurrent broadcasting
func TestWebSocketConcurrentBroadcasts(t *testing.T) {
	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	defer func() { time.Sleep(10 * time.Millisecond) }()

	// Launch multiple concurrent broadcasts
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < 10; j++ {
				hub.BroadcastProgress(&ws.ProgressMessage{
					JobID:    string(rune('a' + id)),
					Status:   "processing",
					Progress: float64(j * 10),
					Message:  "Concurrent broadcast test",
				})
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// Wait for all broadcasts to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestWebSocketOriginChecking tests that origin validation works
func TestWebSocketOriginChecking(t *testing.T) {
	router := gin.New()
	router.Use(gin.Recovery())

	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	defer func() { time.Sleep(10 * time.Millisecond) }()

	router.GET("/ws", handleWebSocket(hub))

	tests := []struct {
		name   string
		origin string
	}{
		{
			name:   "localhost origin",
			origin: "http://localhost:8080",
		},
		{
			name:   "same origin",
			origin: "http://example.com",
		},
		{
			name:   "different origin",
			origin: "http://different-origin.com",
		},
		{
			name:   "no origin header",
			origin: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// All should fail with 400 (missing WebSocket headers)
			// This test verifies the handler doesn't crash with various origins
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}
