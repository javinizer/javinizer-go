package core

import (
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRuntimeState(t *testing.T) {
	rt := NewRuntimeState()
	assert.NotNil(t, rt, "NewRuntimeState should return non-nil RuntimeState")
	assert.Nil(t, rt.WebSocketHub(), "New RuntimeState should have nil WebSocketHub")
}

func TestDefaultRuntimeState(t *testing.T) {
	t.Run("returns nil when not set", func(t *testing.T) {
		defaultRuntimeMu.Lock()
		original := defaultRuntime
		defaultRuntime = nil
		defaultRuntimeMu.Unlock()
		t.Cleanup(func() {
			defaultRuntimeMu.Lock()
			defaultRuntime = original
			defaultRuntimeMu.Unlock()
		})

		rt := DefaultRuntimeState()
		assert.Nil(t, rt, "DefaultRuntimeState should return nil when not initialized")
	})

	t.Run("returns runtime when set", func(t *testing.T) {
		defaultRuntimeMu.Lock()
		original := defaultRuntime
		defaultRuntimeMu.Unlock()
		t.Cleanup(func() {
			defaultRuntimeMu.Lock()
			defaultRuntime = original
			defaultRuntimeMu.Unlock()
		})

		rt := NewRuntimeState()
		SetDefaultRuntimeState(rt)

		got := DefaultRuntimeState()
		assert.Equal(t, rt, got, "DefaultRuntimeState should return the set runtime")
	})
}

func TestSetDefaultRuntimeState(t *testing.T) {
	defaultRuntimeMu.Lock()
	original := defaultRuntime
	defaultRuntimeMu.Unlock()
	t.Cleanup(func() {
		defaultRuntimeMu.Lock()
		defaultRuntime = original
		defaultRuntimeMu.Unlock()
	})

	rt1 := NewRuntimeState()
	SetDefaultRuntimeState(rt1)
	assert.Equal(t, rt1, DefaultRuntimeState(), "First SetDefaultRuntimeState should work")

	rt2 := NewRuntimeState()
	SetDefaultRuntimeState(rt2)
	assert.Equal(t, rt2, DefaultRuntimeState(), "Second SetDefaultRuntimeState should replace first")
}

func TestRuntimeState_Shutdown(t *testing.T) {
	t.Run("no-op when no WebSocket hub", func(t *testing.T) {
		rt := NewRuntimeState()
		assert.NotPanics(t, func() {
			rt.Shutdown()
		}, "Shutdown on empty RuntimeState should not panic")
	})

	t.Run("stops WebSocket hub", func(t *testing.T) {
		rt := NewRuntimeState()
		hub := rt.ResetWebSocketHub()
		require.NotNil(t, hub, "ResetWebSocketHub should return a hub")
		require.NotNil(t, rt.wsHubShutdown, "Shutdown channel should be set")

		shutdownDone := make(chan struct{})
		go func() {
			rt.Shutdown()
			close(shutdownDone)
		}()

		select {
		case <-shutdownDone:
		case <-time.After(2 * time.Second):
			t.Fatal("Shutdown did not complete within 2 seconds")
		}

		assert.Nil(t, rt.wsHubCancel, "WebSocket hub cancel should be cleared after shutdown")
		assert.Nil(t, rt.wsHubShutdown, "WebSocket hub shutdown channel should be cleared")
	})
}

func TestRuntimeState_ResetWebSocketHub(t *testing.T) {
	t.Run("creates and starts WebSocket hub", func(t *testing.T) {
		rt := NewRuntimeState()
		hub := rt.ResetWebSocketHub()

		assert.NotNil(t, hub, "ResetWebSocketHub should return a hub")
		assert.NotNil(t, rt.wsHubCancel, "Cancel function should be set")
		assert.NotNil(t, rt.wsHubShutdown, "Shutdown channel should be set")
	})

	t.Run("restarts WebSocket hub when called multiple times", func(t *testing.T) {
		rt := NewRuntimeState()

		hub1 := rt.ResetWebSocketHub()
		require.NotNil(t, hub1, "First ResetWebSocketHub should return a hub")

		hub2 := rt.ResetWebSocketHub()
		assert.NotNil(t, hub2, "Second ResetWebSocketHub should return a hub")
		assert.NotEqual(t, hub1, hub2, "Each ResetWebSocketHub should create a new hub")
	})
}

func TestRuntimeState_WebSocketHub(t *testing.T) {
	t.Run("returns nil when not set", func(t *testing.T) {
		rt := NewRuntimeState()
		assert.Nil(t, rt.WebSocketHub(), "WebSocketHub should return nil when not initialized")
	})

	t.Run("returns hub when set", func(t *testing.T) {
		rt := NewRuntimeState()
		hub := rt.ResetWebSocketHub()
		assert.Equal(t, hub, rt.WebSocketHub(), "WebSocketHub should return the set hub")
	})
}

func TestRuntimeState_WebSocketUpgrader(t *testing.T) {
	rt := NewRuntimeState()

	t.Run("returns default upgrader when not set", func(t *testing.T) {
		upgrader := rt.WebSocketUpgrader()
		assert.IsType(t, websocket.Upgrader{}, upgrader, "WebSocketUpgrader should return a websocket.Upgrader")
	})

	t.Run("returns set upgrader", func(t *testing.T) {
		customUpgrader := websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 2048,
		}
		rt.SetWebSocketUpgrader(customUpgrader)

		got := rt.WebSocketUpgrader()
		assert.Equal(t, customUpgrader.ReadBufferSize, got.ReadBufferSize, "Should return the set upgrader")
		assert.Equal(t, customUpgrader.WriteBufferSize, got.WriteBufferSize, "Should return the set upgrader")
	})
}

func TestRuntimeState_SetWebSocketHubForTesting(t *testing.T) {
	rt := NewRuntimeState()
	testHub := ws.NewHub()

	rt.SetWebSocketHubForTesting(testHub)
	assert.Equal(t, testHub, rt.WebSocketHub(), "SetWebSocketHubForTesting should set the hub")
}

func TestRuntimeState_SetWebSocketUpgraderForTesting(t *testing.T) {
	rt := NewRuntimeState()
	testUpgrader := websocket.Upgrader{
		ReadBufferSize: 512,
	}

	rt.SetWebSocketUpgraderForTesting(testUpgrader)
	got := rt.WebSocketUpgrader()
	assert.Equal(t, testUpgrader.ReadBufferSize, got.ReadBufferSize, "SetWebSocketUpgraderForTesting should set the upgrader")
}

func TestNoopOriginCheck(t *testing.T) {
	result := NoopOriginCheck(nil)
	assert.True(t, result, "NoopOriginCheck should always return true")

	req, err := http.NewRequest("GET", "http://example.com", nil)
	require.NoError(t, err)
	result = NoopOriginCheck(req)
	assert.True(t, result, "NoopOriginCheck should return true for any request")
}

func TestDefaultRuntimeState_ThreadSafety(t *testing.T) {
	rt := NewRuntimeState()
	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			SetDefaultRuntimeState(rt)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = DefaultRuntimeState()
		}
		done <- true
	}()

	for i := 0; i < 2; i++ {
		<-done
	}
}

func TestRuntimeState_ConcurrentAccess(t *testing.T) {
	rt := NewRuntimeState()

	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			_ = rt.WebSocketHub()
			_ = rt.WebSocketUpgrader()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			rt.SetWebSocketUpgrader(websocket.Upgrader{})
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = rt.WebSocketHub()
		}
		done <- true
	}()

	for i := 0; i < 3; i++ {
		<-done
	}
}
