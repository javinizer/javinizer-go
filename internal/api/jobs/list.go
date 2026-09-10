package jobs

import (
	"net/http"
	"strconv"
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
// @Param phase query string false "Filter by durable phase marker (scrape, apply); empty matches all phases"
// @Param limit query int false "Maximum number of jobs to return (0 = unbounded)"
// @Success 200 {object} contracts.JobListResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/jobs [get]
func listJobs(deps JobDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		statusFilter := c.Query("status")
		phaseFilter := c.Query("phase")
		limit := 0
		if raw := c.Query("limit"); raw != "" {
			if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
				limit = parsed
			}
		}

		// Route both filters down to SQL — restore probes hit
		// /api/v1/jobs?status=running&phase=scrape&limit=1 on every
		// authenticated page load; filtering in memory made each probe read
		// the full job history (codex P2, PR #253), and a row cap applied
		// before the phase predicate could hide older scrape jobs behind
		// newer apply-phase rows (codex P2, PR #255).
		results, err := deps.ListJobsWithStatsByStatusAndPhase(c.Request.Context(), statusFilter, phaseFilter, limit)
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
