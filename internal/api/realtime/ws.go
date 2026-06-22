package realtime

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
)

// handleWebSocket handles WebSocket connections for real-time progress updates
// @Router /ws/progress [get]
// @Summary WebSocket progress updates
// @Description WebSocket endpoint for real-time progress updates during batch operations. Connect to receive streaming updates for batch scrape jobs, file organization, and downloads. Message format: JSON with job_id, type (progress/complete/error/cancelled), file, progress (0.0-1.0), message, and bytes_processed fields.
// @Tags realtime
// @Success 101
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
func handleWebSocket(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rt == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "websocket hub unavailable"})
			return
		}

		runtime := rt.GetRuntime()
		wsHub := runtime.WebSocketHub()
		if wsHub == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "websocket hub unavailable"})
			return
		}

		upgrader := runtime.WebSocketUpgrader()
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			logging.Errorf("Failed to upgrade to websocket: %v", err)
			return
		}

		client := ws.NewClient(conn)
		wsHub.Register(client)

		// Start pumps
		go client.WritePump()
		go client.ReadPump(wsHub)
	}
}
