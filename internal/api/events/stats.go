package events

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/eventlog"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// eventStatsResponse is the response shape for GET /events/stats
type eventStatsResponse struct {
	Total      int64            `json:"total"`
	ByType     map[string]int64 `json:"by_type"`
	BySeverity map[string]int64 `json:"by_severity"`
	BySource   map[string]int64 `json:"by_source"`
}

// eventStats godoc
// @Summary Get event statistics
// @Description Get event counts grouped by type, severity, and source
// @Tags events
// @Produce json
// @Success 200 {object} eventStatsResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/events/stats [get]
func eventStats(eventRepo database.EventRepositoryInterface) gin.HandlerFunc {
	return func(c *gin.Context) {
		stats, err := eventlog.GetStats(c.Request.Context(), eventRepo)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to count events"})
			return
		}

		c.JSON(http.StatusOK, eventStatsResponse{
			Total:      stats.Total,
			ByType:     stats.ByType,
			BySeverity: stats.BySeverity,
			BySource:   stats.BySource,
		})
	}
}
