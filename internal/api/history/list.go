package history

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// getHistory godoc
// @Summary List history records
// @Description Get a paginated list of history records with optional filtering
// @Tags history
// @Produce json
// @Param limit query int false "Number of records to return (default: 50, max: 500)"
// @Param offset query int false "Number of records to skip (default: 0)"
// @Param operation query string false "Filter by operation type (scrape, organize, download, nfo)"
// @Param status query string false "Filter by status (success, failed, reverted)"
// @Param movie_id query string false "Filter by movie ID"
// @Success 200 {object} HistoryListResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/history [get]
func getHistory(historyRepo *database.HistoryRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, offset := core.ParsePagination(c, 50, 500)

		// Get filter params
		operation := c.Query("operation")
		status := c.Query("status")
		movieID := c.Query("movie_id")

		var records []HistoryRecord
		var total int64
		var err error

		// Apply filters
		if movieID != "" {
			// Filter by movie ID
			history, findErr := historyRepo.FindByMovieID(movieID)
			if findErr != nil {
				logging.Errorf("Failed to get history by movie ID: %v", findErr)
				c.JSON(500, ErrorResponse{Error: "Failed to retrieve history"})
				return
			}
			total = int64(len(history))

			// Apply pagination manually
			start := offset
			end := offset + limit
			if start > len(history) {
				start = len(history)
			}
			if end > len(history) {
				end = len(history)
			}
			for _, h := range history[start:end] {
				records = append(records, HistoryRecord{
					ID:           h.ID,
					MovieID:      h.MovieID,
					Operation:    h.Operation,
					OriginalPath: h.OriginalPath,
					NewPath:      h.NewPath,
					Status:       h.Status,
					ErrorMessage: h.ErrorMessage,
					Metadata:     h.Metadata,
					DryRun:       h.DryRun,
					CreatedAt:    h.CreatedAt.Format(time.RFC3339),
				})
			}
		} else if operation != "" {
			// Filter by operation
			history, findErr := historyRepo.FindByOperation(operation, 0) // Get all, then paginate
			if findErr != nil {
				logging.Errorf("Failed to get history by operation: %v", findErr)
				c.JSON(500, ErrorResponse{Error: "Failed to retrieve history"})
				return
			}
			total = int64(len(history))

			// Apply pagination manually
			start := offset
			end := offset + limit
			if start > len(history) {
				start = len(history)
			}
			if end > len(history) {
				end = len(history)
			}
			for _, h := range history[start:end] {
				records = append(records, HistoryRecord{
					ID:           h.ID,
					MovieID:      h.MovieID,
					Operation:    h.Operation,
					OriginalPath: h.OriginalPath,
					NewPath:      h.NewPath,
					Status:       h.Status,
					ErrorMessage: h.ErrorMessage,
					Metadata:     h.Metadata,
					DryRun:       h.DryRun,
					CreatedAt:    h.CreatedAt.Format(time.RFC3339),
				})
			}
		} else if status != "" {
			// Filter by status
			history, findErr := historyRepo.FindByStatus(status, 0) // Get all, then paginate
			if findErr != nil {
				logging.Errorf("Failed to get history by status: %v", findErr)
				c.JSON(500, ErrorResponse{Error: "Failed to retrieve history"})
				return
			}
			total = int64(len(history))

			// Apply pagination manually
			start := offset
			end := offset + limit
			if start > len(history) {
				start = len(history)
			}
			if end > len(history) {
				end = len(history)
			}
			for _, h := range history[start:end] {
				records = append(records, HistoryRecord{
					ID:           h.ID,
					MovieID:      h.MovieID,
					Operation:    h.Operation,
					OriginalPath: h.OriginalPath,
					NewPath:      h.NewPath,
					Status:       h.Status,
					ErrorMessage: h.ErrorMessage,
					Metadata:     h.Metadata,
					DryRun:       h.DryRun,
					CreatedAt:    h.CreatedAt.Format(time.RFC3339),
				})
			}
		} else {
			// No filter - get paginated list
			total, err = historyRepo.Count()
			if err != nil {
				logging.Errorf("Failed to count history: %v", err)
				c.JSON(500, ErrorResponse{Error: "Failed to count history"})
				return
			}

			history, findErr := historyRepo.List(limit, offset)
			if findErr != nil {
				logging.Errorf("Failed to list history: %v", findErr)
				c.JSON(500, ErrorResponse{Error: "Failed to retrieve history"})
				return
			}

			for _, h := range history {
				records = append(records, HistoryRecord{
					ID:           h.ID,
					MovieID:      h.MovieID,
					Operation:    h.Operation,
					OriginalPath: h.OriginalPath,
					NewPath:      h.NewPath,
					Status:       h.Status,
					ErrorMessage: h.ErrorMessage,
					Metadata:     h.Metadata,
					DryRun:       h.DryRun,
					CreatedAt:    h.CreatedAt.Format(time.RFC3339),
				})
			}
		}

		// Ensure records is never nil
		if records == nil {
			records = []HistoryRecord{}
		}

		c.JSON(200, HistoryListResponse{
			Records: records,
			Total:   total,
			Limit:   limit,
			Offset:  offset,
		})
	}
}
