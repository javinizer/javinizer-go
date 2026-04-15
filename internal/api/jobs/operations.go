package jobs

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// listOperations godoc
// @Summary List operations for a batch job
// @Description Get all file operations for a specific batch job with before/after paths
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} OperationListResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/jobs/{id}/operations [get]
func listOperations(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")

		// Validate job exists
		job, err := deps.JobRepo.FindByID(jobID)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, ErrorResponse{Error: "Job not found"})
				return
			}
			logging.Errorf("Failed to find job %s: %v", jobID, err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to retrieve job"})
			return
		}

		// Get operations for the job
		ops, err := deps.BatchFileOpRepo.FindByBatchJobID(jobID)
		if err != nil {
			logging.Errorf("Failed to find operations for job %s: %v", jobID, err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to retrieve operations"})
			return
		}

		// Map operations to response items (omit NFOSnapshot — internal revert data, per T-04-03)
		items := make([]OperationItem, 0, len(ops))
		for _, op := range ops {
			item := OperationItem{
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

		c.JSON(http.StatusOK, OperationListResponse{
			JobID:      jobID,
			JobStatus:  job.Status,
			Operations: items,
			Total:      int64(len(items)),
		})
	}
}
