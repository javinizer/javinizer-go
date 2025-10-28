package api

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
)

// handleWebSocket handles WebSocket connections
func handleWebSocket(wsHub *ws.Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
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
