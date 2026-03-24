package history

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// getHistoryStats godoc
// @Summary Get history statistics
// @Description Get aggregated statistics about history records
// @Tags history
// @Produce json
// @Success 200 {object} HistoryStats
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/history/stats [get]
func getHistoryStats(historyRepo *database.HistoryRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		total, err := historyRepo.Count()
		if err != nil {
			logging.Errorf("Failed to count history: %v", err)
			c.JSON(500, ErrorResponse{Error: "Failed to get statistics"})
			return
		}

		success, err := historyRepo.CountByStatus("success")
		if err != nil {
			logging.Errorf("Failed to count success history: %v", err)
			c.JSON(500, ErrorResponse{Error: "Failed to get statistics"})
			return
		}

		failed, err := historyRepo.CountByStatus("failed")
		if err != nil {
			logging.Errorf("Failed to count failed history: %v", err)
			c.JSON(500, ErrorResponse{Error: "Failed to get statistics"})
			return
		}

		reverted, err := historyRepo.CountByStatus("reverted")
		if err != nil {
			logging.Errorf("Failed to count reverted history: %v", err)
			c.JSON(500, ErrorResponse{Error: "Failed to get statistics"})
			return
		}

		// Get counts by operation
		byOperation := make(map[string]int64)
		operations := []string{"scrape", "organize", "download", "nfo"}
		for _, op := range operations {
			count, err := historyRepo.CountByOperation(op)
			if err != nil {
				logging.Errorf("Failed to count %s history: %v", op, err)
				continue
			}
			byOperation[op] = count
		}

		c.JSON(200, HistoryStats{
			Total:       total,
			Success:     success,
			Failed:      failed,
			Reverted:    reverted,
			ByOperation: byOperation,
		})
	}
}
