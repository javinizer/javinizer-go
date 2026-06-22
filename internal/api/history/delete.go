package history

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// deleteHistory godoc
// @Summary Delete a history record
// @Description Delete a single history record by ID
// @Tags history
// @Produce json
// @Param id path int true "History record ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/history/{id} [delete]
func deleteHistory(repo database.HistoryRepositoryInterface) gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Invalid history ID"})
			return
		}

		_, err = repo.FindByID(c.Request.Context(), uint(id))
		if err != nil {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "History record not found"})
			return
		}

		if err := repo.Delete(c.Request.Context(), uint(id)); err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to delete history record"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "History record deleted"})
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
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/history [delete]
func deleteHistoryBulk(repo database.HistoryRepositoryInterface) gin.HandlerFunc {
	return func(c *gin.Context) {
		olderThanDaysStr := c.Query("older_than_days")
		movieID := c.Query("movie_id")

		if olderThanDaysStr == "" && movieID == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Must specify either older_than_days or movie_id"})
			return
		}

		var deleted int64

		if movieID != "" {
			records, err := repo.FindByMovieID(c.Request.Context(), movieID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to delete history"})
				return
			}
			deleted = int64(len(records))

			if err := repo.DeleteByMovieID(c.Request.Context(), movieID); err != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to delete history"})
				return
			}
		} else if olderThanDaysStr != "" {
			days, err := strconv.Atoi(olderThanDaysStr)
			if err != nil || days < 1 {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Invalid older_than_days value"})
				return
			}

			cutoffDate := time.Now().AddDate(0, 0, -days)

			countBefore, err := repo.Count(c.Request.Context())
			if err != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to count history"})
				return
			}

			if err := repo.DeleteOlderThan(c.Request.Context(), cutoffDate); err != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to delete history"})
				return
			}

			countAfter, err := repo.Count(c.Request.Context())
			if err != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to count history"})
				return
			}
			deleted = countBefore - countAfter
		}

		c.JSON(http.StatusOK, DeleteHistoryBulkResponse{Deleted: deleted})
	}
}
