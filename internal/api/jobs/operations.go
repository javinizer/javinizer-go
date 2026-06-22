package jobs

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// listOperations godoc
// @Summary List operations for a batch job
// @Description Get all file operations for a specific batch job with before/after paths
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} contracts.OperationListResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/jobs/{id}/operations [get]
func listOperations(deps JobDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")

		// Validate job exists
		job, err := deps.JobRepo.FindByID(c.Request.Context(), jobID)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to retrieve job"})
			return
		}

		// Get operations for the job
		ops, err := deps.BatchFileOpRepo.FindByBatchJobID(c.Request.Context(), jobID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to retrieve operations"})
			return
		}

		// Map operations to response items (omit NFOSnapshot — internal revert data, per T-04-03)
		items := make([]contracts.OperationItem, 0, len(ops))
		for _, op := range ops {
			item := contracts.OperationItem{
				ID:             op.ID,
				MovieID:        op.MovieID,
				OriginalPath:   op.OriginalPath,
				NewPath:        op.NewPath,
				OperationType:  op.OperationType,
				RevertStatus:   op.RevertStatus,
				InPlaceRenamed: op.InPlaceRenamed,
				CreatedAt:      op.CreatedAt.Format(time.RFC3339),
			}
			if op.RevertedAt != nil {
				s := op.RevertedAt.Format(time.RFC3339)
				item.RevertedAt = &s
			}
			items = append(items, item)
		}

		c.JSON(http.StatusOK, contracts.OperationListResponse{
			JobID:      jobID,
			JobStatus:  job.Status,
			Operations: items,
			Total:      int64(len(items)),
		})
	}
}
