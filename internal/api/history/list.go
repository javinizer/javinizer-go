package history

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
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
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/history [get]
func getHistory(repo database.HistoryRepositoryInterface) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, offset := core.ParsePagination(c, 50, 500)

		filter := database.HistoryFilter{
			MovieID:   c.Query("movie_id"),
			Operation: models.HistoryOperation(c.Query("operation")),
			Status:    models.HistoryStatus(c.Query("status")),
		}
		total, err := repo.CountFiltered(c.Request.Context(), filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: countHistoryErrorMessage})
			return
		}
		history, err := repo.ListFiltered(c.Request.Context(), filter, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: retrieveHistoryErrorMessage})
			return
		}
		records := make([]HistoryRecord, 0, len(history))
		for _, h := range history {
			records = append(records, toHistoryRecord(h))
		}

		c.JSON(http.StatusOK, HistoryListResponse{
			Records: records,
			Total:   total,
			Limit:   limit,
			Offset:  offset,
		})
	}
}
