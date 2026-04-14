package jobs

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// getJob godoc
// @Summary Get a single batch job
// @Description Get details for a specific batch job by ID
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} JobListItem
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/jobs/{id} [get]
func getJob(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")

		job, err := deps.JobRepo.FindByID(jobID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, ErrorResponse{Error: "Job not found"})
				return
			}
			logging.Errorf("Failed to find job %s: %v", jobID, err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to retrieve job"})
			return
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

		c.JSON(http.StatusOK, item)
	}
}
