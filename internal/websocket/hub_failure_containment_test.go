package websocket

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func startFailureContainmentHub(t *testing.T, hub *Hub) (context.CancelFunc, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		hub.Run(ctx)
		close(stopped)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(testConnTimeout):
			t.Error("hub did not stop after cancellation")
		}
	})
	return cancel, stopped
}

func receiveFailureContainmentFrame(t *testing.T, client *Client) ([]byte, bool) {
	t.Helper()
	select {
	case frame, open := <-client.send:
		return frame, open
	case <-time.After(testConnTimeout):
		t.Fatal("client did not receive a frame or channel closure")
		return nil, false
	}
}

func TestHubFailureContainment_SaturatedClientDoesNotBlockHealthyClient(t *testing.T) {
	hub := NewHub()
	// Unbuffered control channels acknowledge that Run has selected each
	// operation. A later control operation is a barrier for the previous case.
	hub.register = make(chan *Client)
	hub.unregister = make(chan *Client)
	slow, healthy := NewClient(nil), NewClient(nil)
	queued := []byte("already queued for slow client")
	for i := 0; i < cap(slow.send); i++ {
		slow.send <- queued
	}
	startFailureContainmentHub(t, hub)
	hub.Register(slow)
	hub.Register(healthy)

	require.NoError(t, hub.Broadcast(map[string]int{"sequence": 1}))
	frame, open := receiveFailureContainmentFrame(t, healthy)
	require.True(t, open)
	require.JSONEq(t, `{"sequence":1}`, string(frame))

	// This send cannot be selected until Run completes the whole broadcast,
	// including eviction, regardless of map iteration order. Use an unrelated
	// client so this barrier cannot repair a missing slow-client eviction.
	hub.Unregister(NewClient(nil))
	hub.mu.RLock()
	_, slowRegistered := hub.clients[slow]
	_, healthyRegistered := hub.clients[healthy]
	hub.mu.RUnlock()
	require.False(t, slowRegistered, "a saturated client must be evicted")
	require.True(t, healthyRegistered, "backpressure must not evict healthy clients")

	for i := 0; i < cap(slow.send); i++ {
		frame, open := receiveFailureContainmentFrame(t, slow)
		require.True(t, open, "previously queued frames remain readable after eviction")
		require.Equal(t, queued, frame)
	}
	_, open = receiveFailureContainmentFrame(t, slow)
	require.False(t, open, "the evicted client's channel must be closed without enqueueing the new frame")

	require.NoError(t, hub.Broadcast(map[string]int{"sequence": 2}))
	frame, open = receiveFailureContainmentFrame(t, healthy)
	require.True(t, open)
	require.JSONEq(t, `{"sequence":2}`, string(frame), "broadcasting continues after eviction")
	hub.Unregister(healthy)
	_, open = receiveFailureContainmentFrame(t, healthy)
	require.False(t, open)
}

func TestHubFailureContainment_RegisterAndUnregisterReturnAfterShutdown(t *testing.T) {
	hub := NewHub()
	hub.register = make(chan *Client)
	// No buffering means this queue cannot accept a send after Run exits;
	// Unregister must observe shutdown rather than win a select lottery.
	hub.unregister = make(chan *Client)
	client := NewClient(nil)
	cancel, stopped := startFailureContainmentHub(t, hub)
	hub.Register(client)
	cancel()
	select {
	case <-stopped:
	case <-time.After(testConnTimeout):
		t.Fatal("hub did not finish shutdown")
	}
	_, open := receiveFailureContainmentFrame(t, client)
	require.False(t, open, "shutdown closes registered clients")

	returned := make(chan struct{})
	go func() {
		hub.Register(client)
		hub.Unregister(client)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(testConnTimeout):
		t.Fatal("Register or Unregister blocked after the hub stopped")
	}
	require.Empty(t, hub.clients, "shutdown removes all registrations")
}

func TestHubFailureContainment_InvalidProgressArgsNeverEnqueue(t *testing.T) {
	hub := NewHub()
	// Structured message arguments are intentionally open-ended. A channel is
	// a genuine unsupported JSON value, not a substituted serializer failure.
	message := &ProgressMessage{
		JobID:       "job-with-invalid-args",
		Status:      ProgressStatusPending,
		MessageCode: "PROCESSING",
		MessageArgs: map[string]any{"unsupported": make(chan struct{})},
	}
	err := hub.BroadcastProgress(message)
	var unsupported *json.UnsupportedTypeError
	require.ErrorAs(t, err, &unsupported)
	select {
	case frame := <-hub.broadcast:
		t.Fatalf("invalid progress message was enqueued: %q", frame)
	default:
	}

	message.MessageArgs = map[string]any{"filename": "fixture.mp4"}
	require.NoError(t, hub.BroadcastProgress(message), "a bad message must not poison later broadcasts")
	select {
	case frame := <-hub.broadcast:
		var received ProgressMessage
		require.NoError(t, json.Unmarshal(frame, &received))
		require.Equal(t, message.JobID, received.JobID)
		require.Equal(t, message.MessageArgs, received.MessageArgs)
	default:
		t.Fatal("valid progress message was not enqueued")
	}
}
