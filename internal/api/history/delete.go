package history

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// deleteHistory godoc
// @Summary Delete a history record
// @Description Delete a single history record by ID
// @Tags history
// @Produce json
// @Param id path int true "History record ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/history/{id} [delete]
func deleteHistory(historyRepo *database.HistoryRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil {
			c.JSON(400, ErrorResponse{Error: "Invalid history ID"})
			return
		}

		// Check if record exists
		_, err = historyRepo.FindByID(uint(id))
		if err != nil {
			c.JSON(404, ErrorResponse{Error: "History record not found"})
			return
		}

		// Delete the record
		if err := historyRepo.Delete(uint(id)); err != nil {
			logging.Errorf("Failed to delete history record %d: %v", id, err)
			c.JSON(500, ErrorResponse{Error: "Failed to delete history record"})
			return
		}

		c.JSON(200, gin.H{"message": "History record deleted"})
	}
}

// deleteHistoryBulk godoc
// @Summary Delete history records in bulk
// @Description Delete multiple history records based on criteria
// @Tags history
// @Produce json
// @Param older_than_days query int false "Delete records older than N days"
// @Param movie_id query string false "Delete all records for a specific movie"
// @Success 200 {object} DeleteHistoryBulkResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/history [delete]
func deleteHistoryBulk(historyRepo *database.HistoryRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		olderThanDaysStr := c.Query("older_than_days")
		movieID := c.Query("movie_id")

		if olderThanDaysStr == "" && movieID == "" {
			c.JSON(400, ErrorResponse{Error: "Must specify either older_than_days or movie_id"})
			return
		}

		var deleted int64

		if movieID != "" {
			// Count records before deletion
			records, err := historyRepo.FindByMovieID(movieID)
			if err != nil {
				logging.Errorf("Failed to find history by movie ID: %v", err)
				c.JSON(500, ErrorResponse{Error: "Failed to delete history"})
				return
			}
			deleted = int64(len(records))

			// Delete by movie ID
			if err := historyRepo.DeleteByMovieID(movieID); err != nil {
				logging.Errorf("Failed to delete history by movie ID: %v", err)
				c.JSON(500, ErrorResponse{Error: "Failed to delete history"})
				return
			}
		} else if olderThanDaysStr != "" {
			days, err := strconv.Atoi(olderThanDaysStr)
			if err != nil || days < 1 {
				c.JSON(400, ErrorResponse{Error: "Invalid older_than_days value"})
				return
			}

			// Calculate cutoff date
			cutoffDate := time.Now().AddDate(0, 0, -days)

			// Count records before deletion (approximate)
			countBefore, _ := historyRepo.Count()

			// Delete old records
			if err := historyRepo.DeleteOlderThan(cutoffDate); err != nil {
				logging.Errorf("Failed to delete old history: %v", err)
				c.JSON(500, ErrorResponse{Error: "Failed to delete history"})
				return
			}

			// Count records after deletion
			countAfter, _ := historyRepo.Count()
			deleted = countBefore - countAfter
		}

		c.JSON(200, DeleteHistoryBulkResponse{Deleted: deleted})
	}
}
