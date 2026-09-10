package jobs

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/models"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
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

		// Route the status filter down to SQL — restore probes hit
		// /api/v1/jobs?status=running on every authenticated page load, and
		// filtering in memory made each probe read the full job history
		// (codex P2, PR #253).
		results, err := deps.ListJobsWithStatsByStatus(c.Request.Context(), statusFilter)
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
				NoopCount:      result.NoopCount,
				Progress:       job.Progress,
				Destination:    job.Destination,
				StartedAt:      job.StartedAt.Format(time.RFC3339),
			}

			// For running jobs, decode the results envelope for the durable phase
			// marker: status alone cannot distinguish an in-flight scrape from an
			// apply (organize) phase, which transitions the job back to running
			// with JobPhaseApply (codex P2, PR #253). Envelope decode failure leaves
			// the phase empty — callers treat that as non-scrape.
			if job.Status == models.JobStatusRunning {
				snapshot, _ := jobpersist.Decode(&job)
				item.CurrentPhase = snapshot.CurrentPhase
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
