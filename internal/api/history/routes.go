package history

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	historypkg "github.com/javinizer/javinizer-go/internal/history"
)

// RegisterRoutes wires history API endpoints. Stats delegates to history.Logger.GetStats
// (which aggregates 6+ repo calls behind a single interface); other handlers call the
// repository directly.
func RegisterRoutes(protected *gin.RouterGroup, repo database.HistoryRepositoryInterface, logger *historypkg.Logger) {
	protected.GET("/history", getHistory(repo))
	protected.GET("/history/stats", getHistoryStats(logger))
	protected.DELETE("/history/:id", deleteHistory(repo))
	protected.DELETE("/history", deleteHistoryBulk(repo))
}
