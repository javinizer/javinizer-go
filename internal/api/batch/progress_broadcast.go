package batch

import (
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
)

func broadcastProgress(runtime *core.RuntimeState, msg *ws.ProgressMessage) {
	if runtime == nil {
		return
	}
	if runtime.WebSocketHub() == nil {
		return
	}
	if err := runtime.WebSocketHub().BroadcastProgress(msg); err != nil {
		logging.Warnf("Failed to broadcast progress update for job %s: %v", msg.JobID, err)
	}
}
