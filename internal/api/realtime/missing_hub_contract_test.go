package realtime_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/realtime"
	"github.com/stretchr/testify/require"
)

// Standalone route registration may precede hub initialization. Unlike
// server.NewServer, RegisterRoutes does not initialize the hub itself; it must
// reject an upgrade safely while that runtime component is unavailable.
func TestStandaloneRoutesRejectUpgradeBeforeHubInitialization(t *testing.T) {
	rt := core.NewAPIRuntime(&core.APIDeps{})
	t.Cleanup(rt.Shutdown)
	rt.InitAPIConfig()
	runtime := rt.EnsureRuntime()
	require.Nil(t, runtime.WebSocketHub())

	originChecks := 0
	runtime.SetWebSocketUpgrader(websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool {
			originChecks++
			return true
		},
	})

	router := gin.New() // No recovery middleware may hide an upgrade panic.
	authCalls := 0
	realtime.RegisterRoutes(router, rt, func(c *gin.Context) {
		authCalls++
		c.Next()
	})
	req := httptest.NewRequest(http.MethodGet, "/ws/progress", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, req)

	require.Equal(t, 1, authCalls)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.JSONEq(t, `{"error":"websocket hub unavailable"}`, response.Body.String())
	require.Empty(t, response.Header().Get("Upgrade"))
	require.Empty(t, response.Header().Get("Sec-WebSocket-Accept"))
	require.Zero(t, originChecks, "an unavailable hub must be rejected before attempting an upgrade")
	require.Nil(t, runtime.WebSocketHub(), "the handler must not implicitly initialize a hub")
}
