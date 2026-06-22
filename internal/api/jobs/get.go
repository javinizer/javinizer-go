package jobs

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// getJob godoc
// @Summary Get a single batch job
// @Description Get details for a specific batch job by ID
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} contracts.JobListItem
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/jobs/{id} [get]
func getJob(deps JobDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")

		result, err := deps.GetJobWithStats(c.Request.Context(), jobID)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to retrieve job"})
			return
		}

		job := result.Job
		item := contracts.JobListItem{
			ID:             job.ID,
			Status:         job.Status,
			TotalFiles:     job.TotalFiles,
			Completed:      job.Completed,
			Failed:         job.Failed,
			OperationCount: result.OpCount,
			RevertedCount:  result.RevertedCount,
			Progress:       job.Progress,
			Destination:    job.Destination,
			StartedAt:      job.StartedAt.Format(time.RFC3339),
		}

		if job.CompletedAt != nil {
			s := job.CompletedAt.Format(time.RFC3339)
			item.CompletedAt = &s
		}
		if job.OrganizedAt != nil {
			s := job.OrganizedAt.Format(time.RFC3339)
			item.OrganizedAt = &s
		}
		if job.RevertedAt != nil {
			s := job.RevertedAt.Format(time.RFC3339)
			item.RevertedAt = &s
		}

		c.JSON(http.StatusOK, item)
	}
}
