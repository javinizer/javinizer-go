package events

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
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
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/events [delete]
func deleteEvents(eventRepo database.EventRepositoryInterface) gin.HandlerFunc {
	return func(c *gin.Context) {
		olderThanDaysStr := c.Query("older_than_days")

		// Validate required parameter (T-07-02)
		if olderThanDaysStr == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "older_than_days query parameter is required"})
			return
		}

		olderThanDays, err := strconv.Atoi(olderThanDaysStr)
		if err != nil || olderThanDays < 1 {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "older_than_days must be a positive integer (minimum 1)"})
			return
		}

		// Calculate cutoff date
		cutoff := time.Now().AddDate(0, 0, -olderThanDays)

		// Delete events older than cutoff
		deleted, err := eventRepo.DeleteOlderThan(c.Request.Context(), cutoff)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to delete events"})
			return
		}

		c.JSON(http.StatusOK, deleteEventsResponse{
			Deleted: deleted,
			Message: fmt.Sprintf("Events older than %d days deleted", olderThanDays),
		})
	}
}
