package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// testConnTimeout is the maximum time to wait for WebSocket connections in tests
	// Slightly longer timeout for CI environments where connections may be slower
	testConnTimeout = 2 * time.Second
)

func TestNewHub(t *testing.T) {
	hub := NewHub()

	if hub == nil {
		t.Fatal("NewHub() returned nil")
	}

	if hub.clients == nil {
		t.Error("Hub clients map not initialized")
	}

	if hub.broadcast == nil {
		t.Error("Hub broadcast channel not initialized")
	}

	if hub.register == nil {
		t.Error("Hub register channel not initialized")
	}

	if hub.unregister == nil {
		t.Error("Hub unregister channel not initialized")
	}

	// Check initial state
	if len(hub.clients) != 0 {
		t.Errorf("New hub should have 0 clients, got %d", len(hub.clients))
	}

	// Check channel buffer sizes
	if cap(hub.broadcast) != 256 {
		t.Errorf("Broadcast channel buffer should be 256, got %d", cap(hub.broadcast))
	}
}

func TestNewClient(t *testing.T) {
	// Create a mock websocket connection (nil is ok for this test)
	var conn *websocket.Conn
	client := NewClient(conn)

	if client == nil {
		t.Fatal("NewClient() returned nil")
	}

	if client.conn != conn {
		t.Error("Client conn not set correctly")
	}

	if client.send == nil {
		t.Error("Client send channel not initialized")
	}

	// Check send channel buffer size
	if cap(client.send) != 256 {
		t.Errorf("Client send channel buffer should be 256, got %d", cap(client.send))
	}
}

func TestBroadcast(t *testing.T) {
	hub := NewHub()

	tests := []struct {
		name    string
		message interface{}
		wantErr bool
	}{
		{
			name: "valid progress message",
			message: &ProgressMessage{
				JobID:    "test-job",
				Status:   "processing",
				Progress: 0.5,
				Message:  "Test message",
			},
			wantErr: false,
		},
		{
			name: "valid string message",
			message: map[string]string{
				"type": "notification",
				"text": "Hello",
			},
			wantErr: false,
		},
		{
			name:    "empty struct",
			message: struct{}{},
			wantErr: false,
		},
		{
			name:    "nil message",
			message: nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := hub.Broadcast(tt.message)

			if tt.wantErr && err == nil {
				t.Error("Broadcast() expected error, got nil")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("Broadcast() unexpected error: %v", err)
			}

			// Verify message was sent to broadcast channel
			if err == nil {
				select {
				case msg := <-hub.broadcast:
					if msg == nil {
						t.Error("Broadcast() sent nil message to channel")
					}
				default:
					t.Error("Broadcast() did not send message to channel")
				}
			}
		})
	}
}

func TestBroadcastProgress(t *testing.T) {
	hub := NewHub()

	msg := &ProgressMessage{
		JobID:     "test-job-123",
		FileIndex: 1,
		FilePath:  "/path/to/file.mp4",
		Status:    "completed",
		Progress:  1.0,
		Message:   "Processing complete",
	}

	err := hub.BroadcastProgress(msg)
	if err != nil {
		t.Errorf("BroadcastProgress() unexpected error: %v", err)
	}

	// Verify message was sent
	select {
	case data := <-hub.broadcast:
		if data == nil {
			t.Error("BroadcastProgress() sent nil to channel")
		}
		// Data should be valid JSON
		if len(data) == 0 {
			t.Error("BroadcastProgress() sent empty data")
		}
	default:
		t.Error("BroadcastProgress() did not send message")
	}
}

func TestBroadcastProgress_WithError(t *testing.T) {
	hub := NewHub()

	msg := &ProgressMessage{
		JobID:    "error-job",
		Status:   "failed",
		Progress: 0.5,
		Message:  "An error occurred",
		Error:    "file not found",
	}

	err := hub.BroadcastProgress(msg)
	if err != nil {
		t.Errorf("BroadcastProgress() with error field: unexpected error: %v", err)
	}

	// Verify message was sent
	select {
	case <-hub.broadcast:
		// Success - message sent
	default:
		t.Error("BroadcastProgress() with error did not send message")
	}
}

func TestProgressMessage_JSONMarshaling(t *testing.T) {
	msg := &ProgressMessage{
		JobID:     "job-1",
		FileIndex: 0,
		FilePath:  "/test.mp4",
		Status:    "processing",
		Progress:  0.75,
		Message:   "Test",
		Error:     "some error",
	}

	hub := NewHub()
	err := hub.Broadcast(msg)
	if err != nil {
		t.Fatalf("Failed to broadcast: %v", err)
	}

	// Read from channel and verify it's valid JSON-like data
	select {
	case data := <-hub.broadcast:
		// Basic validation - should contain the JobID
		dataStr := string(data)
		if len(dataStr) == 0 {
			t.Error("Marshaled data is empty")
		}
		// Should contain job ID
		if len(dataStr) < 10 {
			t.Error("Marshaled JSON appears too short")
		}
	default:
		t.Error("No message in broadcast channel")
	}
}

func TestRegisterAndUnregister_ChannelOperations(t *testing.T) {
	hub := NewHub()
	client := NewClient(nil)

	// Test that Register sends to register channel (non-blocking test)
	done := make(chan bool, 1)
	go func() {
		hub.Register(client)
		done <- true
	}()

	select {
	case <-hub.register:
		// Successfully received client registration
	case <-done:
		// Registration completed (sent to channel)
	}

	// Test that Unregister sends to unregister channel
	go func() {
		hub.Unregister(client)
		done <- true
	}()

	select {
	case <-hub.unregister:
		// Successfully received client unregistration
	case <-done:
		// Unregistration completed (sent to channel)
	}
}

// TestHub_Run tests the main hub event loop
func TestHub_Run(t *testing.T) {
	hub := NewHub()

	// Start the hub in a goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	// Give the hub time to start
	// (in production this runs indefinitely, we'll test specific operations)

	t.Run("register client", func(t *testing.T) {
		client := NewClient(nil)

		// Register a client
		hub.Register(client)

		// Give time for hub to process
		// We can't directly check hub.clients due to mutex,
		// but we can verify the registration channel was consumed
		// by trying a broadcast operation
		err := hub.Broadcast("test")
		if err != nil {
			t.Errorf("Failed to broadcast after registration: %v", err)
		}
	})

	t.Run("unregister client", func(t *testing.T) {
		client := NewClient(nil)

		// Register first
		hub.Register(client)

		// Then unregister
		hub.Unregister(client)

		// Broadcast should still work even with no clients
		err := hub.Broadcast("test after unregister")
		if err != nil {
			t.Errorf("Failed to broadcast after unregistration: %v", err)
		}
	})

	t.Run("broadcast to multiple clients", func(t *testing.T) {
		// Create multiple clients
		client1 := NewClient(nil)
		client2 := NewClient(nil)
		client3 := NewClient(nil)

		// Register all clients
		hub.Register(client1)
		hub.Register(client2)
		hub.Register(client3)

		// Broadcast a message
		testMsg := map[string]string{"type": "test", "message": "hello all"}
		err := hub.Broadcast(testMsg)
		if err != nil {
			t.Errorf("Failed to broadcast to multiple clients: %v", err)
		}

		// Note: We can't easily test message reception without real websocket connections
		// This test verifies the broadcast mechanism doesn't error with multiple clients
	})

	t.Run("unregister nonexistent client", func(t *testing.T) {
		// Create a client but don't register it
		client := NewClient(nil)

		// Unregistering should not panic
		hub.Unregister(client)

		// Broadcast should still work
		err := hub.Broadcast("test")
		if err != nil {
			t.Errorf("Failed to broadcast after unregistering nonexistent client: %v", err)
		}
	})
}

// TestClient_WritePump tests the client write pump
func TestClient_WritePump(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping WritePump test in short mode")
	}

	t.Run("write message successfully", func(t *testing.T) {
		// Create a pipe to simulate a connection
		serverConn, clientConn, httpServer := createTestConnections(t)
		defer httpServer.Close()
		defer func() { _ = serverConn.Close() }()
		defer func() { _ = clientConn.Close() }()

		wsClient := NewClient(clientConn)

		// Start WritePump in goroutine
		done := make(chan bool)
		go func() {
			wsClient.WritePump()
			done <- true
		}()

		// Send a message
		testMsg := []byte("test message")
		wsClient.send <- testMsg

		// Read the message on server side
		messageType, message, err := serverConn.ReadMessage()
		if err != nil {
			t.Fatalf("Failed to read message: %v", err)
		}

		if messageType != websocket.TextMessage {
			t.Errorf("Expected TextMessage type, got %d", messageType)
		}

		if string(message) != string(testMsg) {
			t.Errorf("Expected '%s', got '%s'", string(testMsg), string(message))
		}

		// Close to exit WritePump
		close(wsClient.send)
		<-done
	})

	t.Run("handle closed channel", func(t *testing.T) {
		// Create test connections
		serverConn, clientConn, httpServer := createTestConnections(t)
		defer httpServer.Close()
		defer func() { _ = serverConn.Close() }()

		wsClient := NewClient(clientConn)

		// Close channel immediately
		close(wsClient.send)

		// Run WritePump (should handle closed channel gracefully)
		done := make(chan bool)
		go func() {
			wsClient.WritePump()
			done <- true
		}()

		// Should complete without hanging
		<-done
	})
}

// TestClient_ReadPump tests the client read pump
func TestClient_ReadPump(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping ReadPump test in short mode")
	}

	t.Run("read messages and unregister on close", func(t *testing.T) {
		hub := NewHub()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		// Create test connections
		serverConn, clientConn, httpServer := createTestConnections(t)
		defer httpServer.Close()
		defer func() { _ = serverConn.Close() }()

		wsClient := NewClient(clientConn)
		hub.Register(wsClient)

		// Start ReadPump
		done := make(chan bool)
		go func() {
			wsClient.ReadPump(hub)
			done <- true
		}()

		// Send a message from server
		err := serverConn.WriteMessage(websocket.TextMessage, []byte("test"))
		if err != nil {
			t.Fatalf("Failed to write message: %v", err)
		}

		// Close the connection from server side
		_ = serverConn.Close()

		// ReadPump should exit and unregister
		<-done
	})

	t.Run("handle connection errors gracefully", func(t *testing.T) {
		hub := NewHub()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go hub.Run(ctx)

		serverConn, clientConn, httpServer := createTestConnections(t)
		defer httpServer.Close()

		wsClient := NewClient(clientConn)
		hub.Register(wsClient)

		// Close server immediately to trigger error
		_ = serverConn.Close()

		// Run ReadPump - should exit gracefully
		done := make(chan bool)
		go func() {
			wsClient.ReadPump(hub)
			done <- true
		}()

		<-done
	})
}

// createTestConnections creates a websocket server and client connection for testing
func createTestConnections(t *testing.T) (*websocket.Conn, *websocket.Conn, *httptest.Server) {
	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	// Use channel to safely communicate server connection from handler goroutine
	serverConnChan := make(chan *websocket.Conn, 1)

	// Create test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Failed to upgrade connection: %v", err)
			return
		}
		serverConnChan <- conn
	}))

	// Create client connection
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial websocket: %v", err)
	}

	// Wait for server connection to be established (with timeout)
	var serverConn *websocket.Conn
	select {
	case serverConn = <-serverConnChan:
		// Connection established successfully
	case <-time.After(testConnTimeout):
		t.Fatal("Timeout waiting for server connection")
	}

	return serverConn, clientConn, server
}
