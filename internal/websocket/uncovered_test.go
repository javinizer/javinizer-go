package websocket

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHub_Run_ContextCancelUncovered(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		hub.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// Hub exited successfully
	case <-time.After(2 * time.Second):
		t.Fatal("Hub.Run should exit when context is cancelled")
	}
}

func TestHub_Broadcast_NilHubUncovered(t *testing.T) {
	var hub *Hub
	err := hub.Broadcast("test")
	assert.NoError(t, err, "nil hub should not panic")
}

func TestHub_BroadcastProgress_NilHubUncovered(t *testing.T) {
	var hub *Hub
	err := hub.BroadcastProgress(&ProgressMessage{JobID: "test"})
	assert.NoError(t, err, "nil hub should not panic")
}

func TestHub_RegisterUnregister_FullCycleUncovered(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	client := NewClient(nil)
	hub.Register(client)
	time.Sleep(50 * time.Millisecond)

	hub.Unregister(client)
	time.Sleep(50 * time.Millisecond)

	// Hub should still work
	err := hub.Broadcast("after unregister")
	assert.NoError(t, err)
}

func TestProgressMessage_JSONRoundTripUncovered(t *testing.T) {
	msg := ProgressMessage{
		JobID:     "job-123",
		FileIndex: 5,
		FilePath:  "/path/to/file.mp4",
		Status:    "completed",
		Progress:  1.0,
		Message:   "Done",
		Error:     "",
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var decoded ProgressMessage
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, "job-123", decoded.JobID)
	assert.Equal(t, 5, decoded.FileIndex)
	assert.Equal(t, 1.0, decoded.Progress)
}

func TestClient_CloseSendChan_IdempotentUncovered(t *testing.T) {
	client := NewClient(nil)
	client.closeSendChan()
	// Second call should not panic
	client.closeSendChan()
}

func TestHub_Broadcast_FullBufferUncovered(t *testing.T) {
	hub := NewHub()
	// Fill the broadcast channel
	for i := 0; i < 300; i++ {
		err := hub.Broadcast(map[string]string{"msg": "fill"})
		if err != nil {
			break
		}
	}
	// Broadcast should not panic even when buffer is full
	err := hub.Broadcast("overflow test")
	assert.NoError(t, err)
}
