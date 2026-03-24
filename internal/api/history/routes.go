package history

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

func RegisterRoutes(protected *gin.RouterGroup, deps *core.ServerDependencies) {
	protected.GET("/history", getHistory(deps.HistoryRepo))
	protected.GET("/history/stats", getHistoryStats(deps.HistoryRepo))
	protected.DELETE("/history/:id", deleteHistory(deps.HistoryRepo))
	protected.DELETE("/history", deleteHistoryBulk(deps.HistoryRepo))
}
