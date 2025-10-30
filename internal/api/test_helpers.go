package api

import (
	"testing"

	ws "github.com/javinizer/javinizer-go/internal/websocket"
)

// initTestWebSocket initializes the package-level wsHub for testing.
// This prevents nil pointer panics in processBatchJob and similar functions.
// Note: wsHub is initialized once and reused across tests to avoid race conditions
// with background goroutines.
func initTestWebSocket(t *testing.T) {
	t.Helper()

	// Only initialize if not already initialized
	if wsHub == nil {
		wsHub = ws.NewHub()
		go wsHub.Run()
	}

	// Don't set wsHub to nil in cleanup - background goroutines may still be using it
	// The hub will be reused across all tests in this package
}
