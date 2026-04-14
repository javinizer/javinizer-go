package jobs

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// listJobs godoc
// @Summary List batch jobs
// @Description Get a list of batch jobs with operation counts and optional status filter
// @Tags jobs
// @Produce json
// @Param status query string false "Filter by job status (organized, reverted, completed, etc.)"
// @Success 200 {object} JobListResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/jobs [get]
func listJobs(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		statusFilter := c.Query("status")

		jobs, err := deps.JobRepo.List()
		if err != nil {
			logging.Errorf("Failed to list jobs: %v", err)
			c.JSON(500, ErrorResponse{Error: "Failed to retrieve jobs"})
			return
		}

		items := make([]JobListItem, 0)

		for _, job := range jobs {
			// Apply status filter if provided
			if statusFilter != "" && job.Status != statusFilter {
				continue
			}

			// Get operation count for this job
			opCount, err := deps.BatchFileOpRepo.CountByBatchJobID(job.ID)
			if err != nil {
				logging.Errorf("Failed to count operations for job %s: %v", job.ID, err)
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to retrieve operation counts"})
				return
			}

			revertedCount, err := deps.BatchFileOpRepo.CountByBatchJobIDAndRevertStatus(job.ID, models.RevertStatusReverted)
			if err != nil {
				logging.Errorf("Failed to count reverted operations for job %s: %v", job.ID, err)
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to retrieve revert counts"})
				return
			}

			item := JobListItem{
				ID:             job.ID,
				Status:         job.Status,
				TotalFiles:     job.TotalFiles,
				Completed:      job.Completed,
				Failed:         job.Failed,
				OperationCount: opCount,
				RevertedCount:  revertedCount,
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

			items = append(items, item)
		}

		c.JSON(200, JobListResponse{Jobs: items})
	}
}
