package events

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// deleteEventsResponse is the response shape for DELETE /events
type deleteEventsResponse struct {
	Deleted int64  `json:"deleted"`
	Message string `json:"message"`
}

// deleteEvents godoc
// @Summary Delete old events
// @Description Delete events older than a specified number of days
// @Tags events
// @Produce json
// @Param older_than_days query int true "Delete events older than N days (minimum 1)"
// @Success 200 {object} deleteEventsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/events [delete]
func deleteEvents(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		olderThanDaysStr := c.Query("older_than_days")

		// Validate required parameter (T-07-02)
		if olderThanDaysStr == "" {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "older_than_days query parameter is required"})
			return
		}

		olderThanDays, err := strconv.Atoi(olderThanDaysStr)
		if err != nil || olderThanDays < 1 {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "older_than_days must be a positive integer (minimum 1)"})
			return
		}

		// Calculate cutoff date
		cutoff := time.Now().AddDate(0, 0, -olderThanDays)

		// Count before delete for response
		result := deps.DB.Where("datetime(created_at) < datetime(?)", cutoff.UTC().Format(database.SqliteTimeFormat)).Delete(&models.Event{})
		if result.Error != nil {
			logging.Errorf("Failed to delete events older than %d days: %v", olderThanDays, result.Error)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to delete events"})
			return
		}

		deleted := result.RowsAffected

		c.JSON(http.StatusOK, deleteEventsResponse{
			Deleted: deleted,
			Message: fmt.Sprintf("Events older than %d days deleted", olderThanDays),
		})
	}
}
