package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
)

func TestBroadcastProgress_NilRuntime(t *testing.T) {
	// Should not panic with nil runtime
	broadcastProgress(nil, nil)
}

func TestBroadcastProgress_RuntimeWithNilHub(t *testing.T) {
	runtime := core.NewRuntimeState()
	// Should not panic - runtime has no WebSocketHub
	broadcastProgress(runtime, nil)
}
