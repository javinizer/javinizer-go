package realtime

import "github.com/gin-gonic/gin"

func RegisterRoutes(router *gin.Engine, deps *ServerDependencies, authMiddleware gin.HandlerFunc) {
	router.GET("/ws/progress", authMiddleware, handleWebSocket(deps.EnsureRuntime().WebSocketHub()))
}
