package core

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/logging"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
)

// RuntimeState holds mutable server runtime components.
type RuntimeState struct {
	mu            sync.RWMutex
	wsHub         *ws.Hub
	wsHubCancel   context.CancelFunc
	wsHubShutdown chan struct{}
	wsUpgrader    websocket.Upgrader
}

var (
	defaultRuntimeMu sync.RWMutex
	defaultRuntime   *RuntimeState
)

// NewRuntimeState creates an initialized runtime container.
func NewRuntimeState() *RuntimeState {
	return &RuntimeState{}
}

// SetDefaultRuntimeState stores runtime state used by components that cannot receive deps directly.
func SetDefaultRuntimeState(runtime *RuntimeState) {
	defaultRuntimeMu.Lock()
	defer defaultRuntimeMu.Unlock()
	defaultRuntime = runtime
}

// DefaultRuntimeState returns the shared runtime state.
func DefaultRuntimeState() *RuntimeState {
	defaultRuntimeMu.RLock()
	defer defaultRuntimeMu.RUnlock()
	return defaultRuntime
}

// Shutdown stops active runtime goroutines.
func (r *RuntimeState) Shutdown() {
	r.mu.Lock()
	cancel := r.wsHubCancel
	shutdown := r.wsHubShutdown
	r.wsHubCancel = nil
	r.wsHubShutdown = nil
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if shutdown != nil {
		select {
		case <-shutdown:
		case <-time.After(500 * time.Millisecond):
			logging.Warnf("WebSocket hub did not shut down within timeout")
		}
	}
}

// ResetWebSocketHub restarts the WebSocket hub and returns the active hub.
func (r *RuntimeState) ResetWebSocketHub() *ws.Hub {
	r.Shutdown()

	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	shutdown := make(chan struct{})

	go func() {
		hub.Run(ctx)
		close(shutdown)
	}()

	r.mu.Lock()
	r.wsHub = hub
	r.wsHubCancel = cancel
	r.wsHubShutdown = shutdown
	r.mu.Unlock()

	return hub
}

// WebSocketHub returns the active WebSocket hub.
func (r *RuntimeState) WebSocketHub() *ws.Hub {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.wsHub
}

// SetWebSocketUpgrader configures the WebSocket upgrader.
func (r *RuntimeState) SetWebSocketUpgrader(upgrader websocket.Upgrader) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wsUpgrader = upgrader
}

// WebSocketUpgrader returns the currently configured upgrader.
func (r *RuntimeState) WebSocketUpgrader() websocket.Upgrader {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.wsUpgrader
}

// SetWebSocketHubForTesting overrides the active hub for tests.
func (r *RuntimeState) SetWebSocketHubForTesting(hub *ws.Hub) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wsHub = hub
}

// SetWebSocketUpgraderForTesting overrides the upgrader for tests.
func (r *RuntimeState) SetWebSocketUpgraderForTesting(upgrader websocket.Upgrader) {
	r.SetWebSocketUpgrader(upgrader)
}

// NoopOriginCheck allows all origins, used by tests.
func NoopOriginCheck(_ *http.Request) bool { return true }
