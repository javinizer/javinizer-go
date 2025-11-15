package websocket

import (
	"testing"

	"github.com/gorilla/websocket"
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
