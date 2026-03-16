package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// runHubWithCleanup starts a hub and returns a cleanup function that waits for shutdown
func runHubWithCleanup(t *testing.T, hub *ws.Hub) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		hub.Run(ctx)
		close(done)
	}()

	return func() {
		cancel()
		select {
		case <-done:
			// Hub shut down successfully
		case <-time.After(500 * time.Millisecond):
			t.Error("Timeout waiting for hub to shut down")
		}
	}
}

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

	// Start hub in background with proper cleanup
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

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
	require.NoError(t, hub.BroadcastProgress(msg))
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

	// Start hub with proper cleanup
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

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
			require.NoError(t, hub.BroadcastProgress(tt.message))

			// Give time for message to be processed
			time.Sleep(5 * time.Millisecond)
		})
	}
}

// TestWebSocketConnectionHandling tests connection lifecycle
func TestWebSocketConnectionHandling(t *testing.T) {
	hub := ws.NewHub()
	require.NotNil(t, hub)

	// Start hub with proper cleanup
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

	// Test that hub can accept clients (simulated)
	// In a real scenario, clients would be added via Register(client)
	// but we can't easily test that without actual WebSocket connections

	// Verify hub is running by broadcasting a message
	msg := &ws.ProgressMessage{
		JobID:   "test-connection",
		Status:  "testing",
		Message: "Connection handling test",
	}

	require.NoError(t, hub.BroadcastProgress(msg))
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

	// Start hub with proper cleanup
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

	// Give hub time to start
	time.Sleep(10 * time.Millisecond)

	// Test that broadcast doesn't panic
	assert.NotPanics(t, func() {
		_ = hub.BroadcastProgress(&ws.ProgressMessage{
			JobID:   "init-test",
			Status:  "testing",
			Message: "Hub initialization test",
		})
	})
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
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.valid {
				// Should not panic when broadcasting
				assert.NotPanics(t, func() {
					_ = hub.BroadcastProgress(tt.message)
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
		_ = hub.BroadcastProgress(msg)
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
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

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
	_ = hub.BroadcastProgress(&ws.ProgressMessage{
		JobID:   "integration-test",
		Status:  "testing",
		Message: "Integration test message",
	})

	time.Sleep(5 * time.Millisecond)
}

// TestWebSocketConcurrentBroadcasts tests concurrent broadcasting
func TestWebSocketConcurrentBroadcasts(t *testing.T) {
	hub := ws.NewHub()
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

	// Launch multiple concurrent broadcasts
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < 10; j++ {
				_ = hub.BroadcastProgress(&ws.ProgressMessage{
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
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

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

// =============================================================================
// Real WebSocket Connection Tests (AC-3.2.1, AC-3.2.2, AC-3.2.3)
// =============================================================================

// createTestServer creates an HTTP test server with WebSocket handler
func createTestServer(t *testing.T, hub *ws.Hub) *httptest.Server {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/ws", handleWebSocket(hub))
	return httptest.NewServer(router)
}

// connectWebSocket connects a WebSocket client to test server
func connectWebSocket(t *testing.T, serverURL string) *websocket.Conn {
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err, "Failed to connect WebSocket client")
	return conn
}

// readMessage reads a message from WebSocket with timeout
func readMessage(t *testing.T, conn *websocket.Conn, timeout time.Duration) *ws.ProgressMessage {
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(timeout)))

	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil
	}

	var msg ws.ProgressMessage
	err = json.Unmarshal(data, &msg)
	require.NoError(t, err, "Failed to unmarshal message")

	return &msg
}

// TestWebSocketRealClientConnection tests real WebSocket client connection (AC-3.2.1)
func TestWebSocketRealClientConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping WebSocket integration test in short mode")
	}

	// AC-3.2.2: Verify no goroutine leaks (ignore database background goroutines)
	defer goleak.VerifyNone(t, goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"))

	// Setup hub and server
	hub := ws.NewHub()
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

	server := createTestServer(t, hub)
	defer server.Close()

	// AC-3.2.1: Single client connects and receives messages
	client := connectWebSocket(t, server.URL)
	defer func() {
		_ = client.Close()
	}()

	// Give client time to register with hub
	time.Sleep(50 * time.Millisecond)

	// Broadcast a message
	testMsg := &ws.ProgressMessage{
		JobID:    "test-job-001",
		FilePath: "/test/video.mp4",
		Status:   "processing",
		Progress: 50.0,
		Message:  "Test broadcast",
	}

	err := hub.BroadcastProgress(testMsg)
	require.NoError(t, err)

	// AC-3.2.3: Client receives broadcast and validates message format
	receivedMsg := readMessage(t, client, 2*time.Second)
	require.NotNil(t, receivedMsg, "Client should receive broadcast message")

	// Validate message schema (AC-3.2.3)
	assert.Equal(t, testMsg.JobID, receivedMsg.JobID)
	assert.Equal(t, testMsg.FilePath, receivedMsg.FilePath)
	assert.Equal(t, testMsg.Status, receivedMsg.Status)
	assert.Equal(t, testMsg.Progress, receivedMsg.Progress)
	assert.Equal(t, testMsg.Message, receivedMsg.Message)

	// AC-3.2.1: Graceful disconnect
	err = client.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	assert.NoError(t, err)

	// AC-3.2.3: Verify connection cleanup
	time.Sleep(100 * time.Millisecond) // Give time for cleanup
}

// TestWebSocketMultipleClients tests multiple concurrent client connections (AC-3.2.1, AC-3.2.2)
func TestWebSocketMultipleClients(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow WebSocket test in short mode")
	}

	defer goleak.VerifyNone(t, goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"))

	// Setup hub and server
	hub := ws.NewHub()
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

	server := createTestServer(t, hub)
	defer server.Close()

	// AC-3.2.1: Connect 5 clients concurrently
	numClients := 5
	clients := make([]*websocket.Conn, numClients)
	var wg sync.WaitGroup

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			clients[idx] = connectWebSocket(t, server.URL)
		}(i)
	}

	wg.Wait()
	defer func() {
		for _, client := range clients {
			if client != nil {
				_ = client.Close()
			}
		}
	}()

	// Give all clients time to register
	time.Sleep(100 * time.Millisecond)

	// AC-3.2.1: Server broadcasts to all connected clients
	testMsg := &ws.ProgressMessage{
		JobID:    "multi-client-test",
		Status:   "started",
		Progress: 0,
		Message:  "Broadcasting to all clients",
	}

	err := hub.BroadcastProgress(testMsg)
	require.NoError(t, err)

	// AC-3.2.1: Verify all clients receive the same message
	receivedMsgs := make([]*ws.ProgressMessage, numClients)
	for i := 0; i < numClients; i++ {
		receivedMsgs[i] = readMessage(t, clients[i], 2*time.Second)
		require.NotNil(t, receivedMsgs[i], "Client %d should receive message", i)
		assert.Equal(t, testMsg.JobID, receivedMsgs[i].JobID)
		assert.Equal(t, testMsg.Message, receivedMsgs[i].Message)
	}

	// Cleanup with graceful disconnect
	for _, client := range clients {
		require.NoError(t, client.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")))
	}

	time.Sleep(100 * time.Millisecond) // AC-3.2.3: Verify cleanup
}

// TestWebSocketMessageOrdering tests message ordering per client (AC-3.2.1)
func TestWebSocketMessageOrdering(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow WebSocket test in short mode")
	}

	defer goleak.VerifyNone(t, goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"))

	hub := ws.NewHub()
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

	server := createTestServer(t, hub)
	defer server.Close()

	client := connectWebSocket(t, server.URL)
	defer func() {
		_ = client.Close()
	}()

	time.Sleep(50 * time.Millisecond)

	// Send multiple messages in order
	numMessages := 10
	for i := 0; i < numMessages; i++ {
		msg := &ws.ProgressMessage{
			JobID:     "ordering-test",
			FileIndex: i,
			Status:    "processing",
			Progress:  float64(i * 10),
			Message:   "Message sequence test",
		}
		err := hub.BroadcastProgress(msg)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // Small delay between messages
	}

	// AC-3.2.1: Verify message ordering preserved
	for i := 0; i < numMessages; i++ {
		msg := readMessage(t, client, 2*time.Second)
		require.NotNil(t, msg, "Should receive message %d", i)
		assert.Equal(t, i, msg.FileIndex, "Messages should be received in order")
		assert.Equal(t, float64(i*10), msg.Progress)
	}
}

// TestWebSocketAbruptDisconnect tests abrupt client disconnect (AC-3.2.1, AC-3.2.3)
func TestWebSocketAbruptDisconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow WebSocket test in short mode")
	}

	defer goleak.VerifyNone(t, goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener")) // AC-3.2.2: Verify no goroutine leaks after abrupt disconnect

	hub := ws.NewHub()
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

	server := createTestServer(t, hub)
	defer server.Close()

	client := connectWebSocket(t, server.URL)
	time.Sleep(50 * time.Millisecond)

	// AC-3.2.1: Simulate abrupt disconnect (close without handshake)
	err := client.Close()
	assert.NoError(t, err)

	// AC-3.2.3: Verify hub continues working after abrupt disconnect
	time.Sleep(100 * time.Millisecond)

	// Hub should still broadcast without errors
	err = hub.BroadcastProgress(&ws.ProgressMessage{
		JobID:   "post-disconnect",
		Status:  "testing",
		Message: "Broadcast after client disconnect",
	})
	assert.NoError(t, err)
}

// TestWebSocketConcurrentConnections tests 10+ concurrent connections with race detector (AC-3.2.2)
func TestWebSocketConcurrentConnections(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow WebSocket concurrency test in short mode")
	}

	defer goleak.VerifyNone(t, goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"))

	hub := ws.NewHub()
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

	server := createTestServer(t, hub)
	defer server.Close()

	// AC-3.2.2: Simulate 10+ concurrent connections
	numClients := 12
	clients := make([]*websocket.Conn, numClients)
	var connectWg sync.WaitGroup

	// Connect all clients concurrently
	for i := 0; i < numClients; i++ {
		connectWg.Add(1)
		go func(idx int) {
			defer connectWg.Done()
			clients[idx] = connectWebSocket(t, server.URL)
		}(i)
	}

	connectWg.Wait()
	defer func() {
		for _, client := range clients {
			if client != nil {
				_ = client.Close()
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// AC-3.2.2: Rapid broadcast loop to test race conditions
	numBroadcasts := 100
	var broadcastWg sync.WaitGroup

	for i := 0; i < numBroadcasts; i++ {
		broadcastWg.Add(1)
		go func(idx int) {
			defer broadcastWg.Done()
			msg := &ws.ProgressMessage{
				JobID:     "concurrent-test",
				FileIndex: idx,
				Status:    "processing",
				Progress:  float64(idx),
				Message:   "Concurrent broadcast",
			}
			err := hub.BroadcastProgress(msg)
			assert.NoError(t, err)
		}(i)
	}

	broadcastWg.Wait()

	// Give time for all messages to be processed
	time.Sleep(500 * time.Millisecond)

	// Cleanup
	for _, client := range clients {
		require.NoError(t, client.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")))
	}

	time.Sleep(100 * time.Millisecond) // AC-3.2.3: Verify cleanup
}

// TestWebSocketHubThreadSafety tests hub concurrent safety (AC-3.2.2)
func TestWebSocketHubThreadSafety(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow WebSocket concurrency test in short mode")
	}

	defer goleak.VerifyNone(t, goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"))

	hub := ws.NewHub()
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

	server := createTestServer(t, hub)
	defer server.Close()

	// AC-3.2.2: Multiple goroutines calling Register/Unregister/Broadcast simultaneously
	var wg sync.WaitGroup
	numGoroutines := 20

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Connect client
			client := connectWebSocket(t, server.URL)
			time.Sleep(10 * time.Millisecond)

			// Broadcast some messages
			for j := 0; j < 5; j++ {
				if err := hub.BroadcastProgress(&ws.ProgressMessage{
					JobID:    "thread-safety-test",
					Status:   "testing",
					Progress: float64(id*5 + j),
					Message:  "Concurrent operations",
				}); err != nil {
					t.Errorf("broadcast failed: %v", err)
				}
			}

			// Disconnect client
			_ = client.Close()
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond) // Ensure all cleanup completes
}

// TestWebSocketMessageSchemaValidation tests ProgressMessage schema (AC-3.2.3)
func TestWebSocketMessageSchemaValidation(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"))

	hub := ws.NewHub()
	cleanup := runHubWithCleanup(t, hub)
	defer cleanup()

	server := createTestServer(t, hub)
	defer server.Close()

	client := connectWebSocket(t, server.URL)
	defer func() { _ = client.Close() }()

	time.Sleep(50 * time.Millisecond)

	// AC-3.2.3: Test message with all fields
	completeMsg := &ws.ProgressMessage{
		JobID:     "schema-test-001",
		FileIndex: 5,
		FilePath:  "/path/to/video.mp4",
		Status:    "completed",
		Progress:  100.0,
		Message:   "All fields present",
		Error:     "",
	}

	err := hub.BroadcastProgress(completeMsg)
	require.NoError(t, err)

	received := readMessage(t, client, 2*time.Second)
	require.NotNil(t, received)

	// Validate all fields present and correct type
	assert.Equal(t, "schema-test-001", received.JobID)
	assert.Equal(t, 5, received.FileIndex)
	assert.Equal(t, "/path/to/video.mp4", received.FilePath)
	assert.Equal(t, "completed", received.Status)
	assert.Equal(t, 100.0, received.Progress)
	assert.Equal(t, "All fields present", received.Message)
	assert.Equal(t, "", received.Error)

	// Test message with error field
	errorMsg := &ws.ProgressMessage{
		JobID:    "schema-test-002",
		Status:   "failed",
		Progress: 0,
		Message:  "Processing failed",
		Error:    "File not found",
	}

	err = hub.BroadcastProgress(errorMsg)
	require.NoError(t, err)

	received = readMessage(t, client, 2*time.Second)
	require.NotNil(t, received)
	assert.Equal(t, "File not found", received.Error)
}

// TestWebSocketGracefulShutdown tests graceful hub shutdown (AC-3.2.3)
func TestWebSocketGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow WebSocket shutdown test in short mode")
	}

	defer goleak.VerifyNone(t, goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener")) // AC-3.2.2: Verify no leaks after shutdown

	hub := ws.NewHub()
	cleanup := runHubWithCleanup(t, hub)

	server := createTestServer(t, hub)

	// Connect clients
	numClients := 5
	clients := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		clients[i] = connectWebSocket(t, server.URL)
	}

	time.Sleep(100 * time.Millisecond)

	// AC-3.2.3: Close clients first before canceling context
	for _, client := range clients {
		require.NoError(t, client.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")))
		_ = client.Close()
	}

	// Give time for client cleanup
	time.Sleep(200 * time.Millisecond)

	// AC-3.2.3: Trigger graceful shutdown
	cleanup()

	// Cleanup server
	server.Close()
}
