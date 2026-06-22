package jobs

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/models"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// listJobs godoc
// @Summary List batch jobs
// @Description Get a list of batch jobs with operation counts and optional status filter
// @Tags jobs
// @Produce json
// @Param status query string false "Filter by job status (organized, reverted, completed, etc.)"
// @Success 200 {object} contracts.JobListResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/jobs [get]
func listJobs(deps JobDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		statusFilter := c.Query("status")

		results, err := deps.ListJobsWithStats(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to retrieve jobs"})
			return
		}

		items := make([]contracts.JobListItem, 0)

		for _, result := range results {
			job := result.Job

			// Apply status filter if provided
			if statusFilter != "" && job.Status != models.JobStatus(statusFilter) {
				continue
			}

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

			items = append(items, item)
		}

		c.JSON(http.StatusOK, contracts.JobListResponse{Jobs: items})
	}
}
