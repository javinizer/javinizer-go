package events

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// validEventTypes and validSeverities for input validation (T-07-01)
var validEventTypes = map[string]bool{
	models.EventCategoryScraper:  true,
	models.EventCategoryOrganize: true,
	models.EventCategorySystem:   true,
}

var validSeverities = map[string]bool{
	models.SeverityDebug: true,
	models.SeverityInfo:  true,
	models.SeverityWarn:  true,
	models.SeverityError: true,
}

// eventListResponse is the response shape for GET /events
type eventListResponse struct {
	Events []models.Event `json:"events"`
	Total  int64          `json:"total"`
}

// listEvents godoc
// @Summary List events
// @Description Get a paginated list of structured events with composable type, severity, source, and date range filters
// @Tags events
// @Produce json
// @Param type query string false "Filter by event type (scraper, organize, system)"
// @Param severity query string false "Filter by severity (debug, info, warn, error)"
// @Param source query string false "Filter by source (e.g., r18dev, organizer, server)"
// @Param start query string false "Start date (ISO 8601, e.g., 2026-01-01T00:00:00Z)"
// @Param end query string false "End date (ISO 8601, e.g., 2026-01-31T23:59:59Z)"
// @Param limit query int false "Max events to return (default 50, max 200)" default(50)
// @Param offset query int false "Offset for pagination" default(0)
// @Success 200 {object} eventListResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/events [get]
func listEvents(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		eventType := c.Query("type")
		severity := c.Query("severity")
		source := c.Query("source")
		limitStr := c.DefaultQuery("limit", "50")
		offsetStr := c.DefaultQuery("offset", "0")
		startStr := c.Query("start")
		endStr := c.Query("end")

		if eventType != "" && !validEventTypes[eventType] {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid type filter. Must be one of: scraper, organize, system"})
			return
		}

		if severity != "" && !validSeverities[severity] {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid severity filter. Must be one of: debug, info, warn, error"})
			return
		}

		if source != "" && len(strings.TrimSpace(source)) == 0 {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Source filter must not be empty"})
			return
		}

		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			limit = 50
		}
		if limit > 200 {
			limit = 200
		}

		offset, err := strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			offset = 0
		}

		filter := database.EventFilter{
			EventType: eventType,
			Severity:  severity,
			Source:    source,
		}

		if startStr != "" {
			start, err := time.Parse(time.RFC3339, startStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid start date format. Use ISO 8601 (e.g., 2026-01-01T00:00:00Z)"})
				return
			}
			filter.Start = &start
		}
		if endStr != "" {
			end, err := time.Parse(time.RFC3339, endStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid end date format. Use ISO 8601 (e.g., 2026-01-31T23:59:59Z)"})
				return
			}
			filter.End = &end
		}

		events, err := deps.EventRepo.FindFiltered(filter, limit, offset)
		if err != nil {
			logging.Errorf("Failed to list events: %v", err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to retrieve events"})
			return
		}

		total, err := deps.EventRepo.CountFiltered(filter)
		if err != nil {
			logging.Errorf("Failed to count events: %v", err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to count events"})
			return
		}

		if events == nil {
			events = []models.Event{}
		}

		c.JSON(http.StatusOK, eventListResponse{
			Events: events,
			Total:  total,
		})
	}
}
