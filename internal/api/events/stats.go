package events

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
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
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/events/stats [get]
func eventStats(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		total, err := deps.EventRepo.Count()
		if err != nil {
			logging.Errorf("Failed to count events: %v", err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to count events"})
			return
		}

		byType := make(map[string]int64)
		for _, t := range []string{models.EventCategoryScraper, models.EventCategoryOrganize, models.EventCategorySystem} {
			count, err := deps.EventRepo.CountByType(t)
			if err != nil {
				logging.Errorf("Failed to count events by type %s: %v", t, err)
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to count events"})
				return
			}
			byType[t] = count
		}

		bySeverity := make(map[string]int64)
		for _, s := range []string{models.SeverityDebug, models.SeverityInfo, models.SeverityWarn, models.SeverityError} {
			count, err := deps.EventRepo.CountBySeverity(s)
			if err != nil {
				logging.Errorf("Failed to count events by severity %s: %v", s, err)
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to count events"})
				return
			}
			bySeverity[s] = count
		}

		bySource, err := deps.EventRepo.CountGroupBySource()
		if err != nil {
			logging.Errorf("Failed to count events by source: %v", err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to count events"})
			return
		}

		c.JSON(http.StatusOK, eventStatsResponse{
			Total:      total,
			ByType:     byType,
			BySeverity: bySeverity,
			BySource:   bySource,
		})
	}
}
