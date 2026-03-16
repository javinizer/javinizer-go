package api

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
)

var (
	wsTestMu sync.Mutex
)

// cleanupServerHub cleans up the global hub created by NewServer
func cleanupServerHub(t *testing.T, deps *ServerDependencies) {
	t.Helper()
	if deps.wsCancel != nil {
		deps.wsCancel()
		// Wait for hub to shut down gracefully (max 500ms)
		time.Sleep(100 * time.Millisecond)
	}
}

// initTestWebSocket initializes the package-level wsHub and wsUpgrader for testing.
// This prevents nil pointer panics in processBatchJob and similar functions.
// Note: wsHub is initialized once and reused across tests to avoid race conditions
// with background goroutines. wsUpgrader is always reinitialized to ensure test
// isolation when tests run in different orders (some tests call NewServer which sets
// stricter origin checking, so we need to reset to test-friendly settings).
func initTestWebSocket(t *testing.T) {
	t.Helper()

	wsTestMu.Lock()
	defer wsTestMu.Unlock()

	// Always reinitialize wsUpgrader for testing (allow all origins)
	// This ensures test isolation even if NewServer() was called by another test.
	// The mutex prevents race conditions during reinitialization.
	wsUpgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins in tests
		},
	}

	// Initialize wsHub if not already initialized
	// This follows the same pattern as NewServer() to ensure consistency
	if wsHub == nil {
		wsHub = ws.NewHub()
		wsHubShutdown = make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		wsHubCancel = cancel

		go func() {
			wsHub.Run(ctx)
			close(wsHubShutdown)
		}()

		// Clean up on test completion - ensure hub stops gracefully
		// This cleanup will be inherited by all tests in the package
		// Note: We don't set wsHub = nil because other goroutines might still have references
		t.Cleanup(func() {
			if wsHubCancel != nil {
				wsHubCancel()
				if wsHubShutdown != nil {
					select {
					case <-wsHubShutdown:
						// Hub shut down successfully
					case <-time.After(500 * time.Millisecond):
						// Timeout waiting for shutdown
					}
				}
				wsHubCancel = nil
			}
		})
	}
}
