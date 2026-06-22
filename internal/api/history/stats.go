package history

import (
	"net/http"

	"github.com/gin-gonic/gin"
	historypkg "github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// getHistoryStats godoc
// @Summary Get history statistics
// @Description Get aggregated statistics about history records
// @Tags history
// @Produce json
// @Success 200 {object} HistoryStats
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/history/stats [get]
func getHistoryStats(logger *historypkg.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		stats, err := logger.GetStats(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to get statistics"})
			return
		}

		byOperation := map[string]int64{
			string(models.HistoryOpScrape):   stats.Scrape,
			string(models.HistoryOpOrganize): stats.Organize,
			string(models.HistoryOpDownload): stats.Download,
			string(models.HistoryOpNFO):      stats.NFO,
		}

		c.JSON(http.StatusOK, HistoryStats{
			Total:       stats.Total,
			Success:     stats.Success,
			Failed:      stats.Failed,
			Reverted:    stats.Reverted,
			ByOperation: byOperation,
		})
	}
}
