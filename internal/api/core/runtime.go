package core

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/poster"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
)

// RuntimeState holds mutable server runtime components.
type RuntimeState struct {
	mu             sync.RWMutex
	wsHub          *ws.Hub
	wsHubCancel    context.CancelFunc
	wsHubShutdown  chan struct{}
	wsUpgrader     websocket.Upgrader
	ConfigUpdateMu sync.Mutex // serializes config updates (was package-level ConfigUpdateMutex)

	// posterManager is lazily initialized on first access and invalidated
	// on config reload. Protected by mu.
	posterManager poster.PosterManagerInterface

	// posterMgrCreateMu serializes poster-manager construction so that only
	// one goroutine calls createFn() on a cache miss; concurrent callers
	// wait and receive the cached result. Separate from mu to avoid holding
	// a write lock across the potentially slow createFn() call.
	posterMgrCreateMu sync.Mutex

	// bgTasksWg tracks handler-launched background goroutines (batch phase
	// runners) so graceful shutdown and test teardown can join them instead of
	// racing their final writes. See TrackBackgroundTask/WaitBackgroundTasks.
	bgTasksWg sync.WaitGroup
}

// NewRuntimeState creates an initialized runtime container.
func NewRuntimeState() *RuntimeState {
	return &RuntimeState{}
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

// TrackBackgroundTask registers a handler-launched background goroutine (e.g.
// the batch phase runners in prepareAndLaunchApply / StartBatchScrape) and
// returns its release function, which the caller MUST invoke (typically
// deferred at the top of the goroutine) when the goroutine returns.
//
// Combined with Shutdown + WaitBackgroundTasks this lets teardown actually
// JOIN background phase goroutines instead of inferring liveness from job
// status, which is wrong in both directions: a just-launched goroutine has
// not marked its job Running yet, and phases set the terminal lifecycle
// status before their deferred persistence executes.
//
// Precondition: do not launch new tasks concurrently with an in-flight
// WaitBackgroundTasks — sync.WaitGroup forbids Add racing Wait when the
// counter is zero (behavior unspecified). Only teardown drains wait today,
// after all handler launches have ceased; if production ever waits, handler
// launches must first be gated (e.g. by a shutting-down flag).
func (r *RuntimeState) TrackBackgroundTask() (done func()) {
	r.bgTasksWg.Add(1)
	return r.bgTasksWg.Done
}

// WaitBackgroundTasks blocks until every tracked background goroutine has
// returned or the timeout elapses, reporting whether all tasks finished.
//
// sync.WaitGroup has no timeout/query API, so the wait runs on a helper
// goroutine; on timeout that helper stays parked on Wait until the tasks
// eventually finish, stranding only this cheap goroutine, never the caller.
func (r *RuntimeState) WaitBackgroundTasks(timeout time.Duration) bool {
	settled := make(chan struct{})
	go func() {
		r.bgTasksWg.Wait()
		close(settled)
	}()
	select {
	case <-settled:
		return true
	case <-time.After(timeout):
		return false
	}
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

// GetPosterManager returns the cached PosterManager, constructing it on first access.
// The createFn callback is invoked only when the manager is nil (first access or
// after invalidation). Returns nil if createFn returns nil.
//
// A dedicated construction lock (posterMgrCreateMu) prevents multiple concurrent
// goroutines from all calling createFn() on a cache miss — only the first
// goroutine constructs the manager; others wait and receive the cached result.
func (r *RuntimeState) GetPosterManager(createFn func() poster.PosterManagerInterface) poster.PosterManagerInterface {
	r.mu.RLock()
	pm := r.posterManager
	r.mu.RUnlock()
	if pm != nil {
		return pm
	}

	r.posterMgrCreateMu.Lock()
	defer r.posterMgrCreateMu.Unlock()

	// Double-check after acquiring the create lock — another goroutine may
	// have completed construction while we were waiting.
	r.mu.RLock()
	pm = r.posterManager
	r.mu.RUnlock()
	if pm != nil {
		return pm
	}

	newPM := createFn()
	if newPM == nil {
		return nil
	}

	r.mu.Lock()
	r.posterManager = newPM
	r.mu.Unlock()

	return newPM
}

// InvalidatePosterManager nils the cached poster manager so it is reconstructed
// on next access. Called during config reload.
func (r *RuntimeState) InvalidatePosterManager() {
	r.mu.Lock()
	r.posterManager = nil
	r.mu.Unlock()
}

// noopOriginCheck allows all origins, used by tests.
//
//nolint:unused // used by same-package tests
func noopOriginCheck(_ *http.Request) bool { return true }
