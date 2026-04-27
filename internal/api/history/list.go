package history

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// toHistoryRecord converts a models.History to a HistoryRecord for API responses.
func toHistoryRecord(h models.History) HistoryRecord {
	return HistoryRecord{
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
	}
}

// paginateAndConvert paginates a slice of History records and converts them to
// HistoryRecord API response objects.
func paginateAndConvert(all []models.History, limit, offset int) (records []HistoryRecord, total int64) {
	total = int64(len(all))
	start := offset
	end := offset + limit
	if start > len(all) {
		start = len(all)
	}
	if end > len(all) {
		end = len(all)
	}
	records = make([]HistoryRecord, 0, end-start)
	for _, h := range all[start:end] {
		records = append(records, toHistoryRecord(h))
	}
	return records, total
}

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

		operation := c.Query("operation")
		status := c.Query("status")
		movieID := c.Query("movie_id")

		var records []HistoryRecord
		var total int64

		if movieID != "" {
			history, findErr := historyRepo.FindByMovieID(movieID)
			if findErr != nil {
				logging.Errorf("Failed to get history by movie ID: %v", findErr)
				c.JSON(500, ErrorResponse{Error: "Failed to retrieve history"})
				return
			}
			records, total = paginateAndConvert(history, limit, offset)
		} else if operation != "" {
			history, findErr := historyRepo.FindByOperation(operation, 0)
			if findErr != nil {
				logging.Errorf("Failed to get history by operation: %v", findErr)
				c.JSON(500, ErrorResponse{Error: "Failed to retrieve history"})
				return
			}
			records, total = paginateAndConvert(history, limit, offset)
		} else if status != "" {
			history, findErr := historyRepo.FindByStatus(status, 0)
			if findErr != nil {
				logging.Errorf("Failed to get history by status: %v", findErr)
				c.JSON(500, ErrorResponse{Error: "Failed to retrieve history"})
				return
			}
			records, total = paginateAndConvert(history, limit, offset)
		} else {
			var err error
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

			records = make([]HistoryRecord, 0, len(history))
			for _, h := range history {
				records = append(records, toHistoryRecord(h))
			}
		}

		c.JSON(200, HistoryListResponse{
			Records: records,
			Total:   total,
			Limit:   limit,
			Offset:  offset,
		})
	}
}
