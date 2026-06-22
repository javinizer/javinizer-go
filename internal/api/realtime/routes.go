package realtime

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

func RegisterRoutes(router *gin.Engine, rt *core.APIRuntime, authMiddleware gin.HandlerFunc) {
	router.GET("/ws/progress", authMiddleware, handleWebSocket(rt))
}
