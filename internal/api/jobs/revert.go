package jobs

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// revertBatch godoc
// @Summary Revert a batch job
// @Description Revert all file organization operations for a batch job, moving files back to original paths
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} RevertResultResponse
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse "Revert is disabled"
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/jobs/{id}/revert [post]
func revertBatch(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")

		// Guard: revert must be explicitly enabled in config
		if !deps.GetConfig().Output.AllowRevert {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "Revert is disabled. Enable it in Settings > File Operations."})
			return
		}

		// Load job from DB — 404 if not found
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

		if job.Status != string(models.JobStatusOrganized) {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Job is not in organized status"})
			return
		}

		result, err := deps.Reverter.RevertBatch(c.Request.Context(), jobID)
		if err != nil {
			if errors.Is(err, history.ErrBatchAlreadyReverted) {
				c.JSON(http.StatusConflict, ErrorResponse{Error: "Batch already reverted"})
				return
			}
			if errors.Is(err, history.ErrNoOperationsFound) {
				c.JSON(http.StatusNotFound, ErrorResponse{Error: "No operations found for batch"})
				return
			}
			logging.Errorf("Failed to revert batch %s: %v", jobID, err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to revert batch"})
			return
		}

		jobStatus := string(models.JobStatusOrganized)
		if result.Failed == 0 && result.Skipped == 0 {
			now := time.Now()
			job.Status = string(models.JobStatusReverted)
			job.RevertedAt = &now
			if err := deps.JobRepo.Update(job); err != nil {
				logging.Errorf("Failed to update job %s status to reverted: %v", jobID, err)
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to persist reverted status"})
				return
			}
			if batchJob, ok := deps.JobQueue.GetJobPointer(jobID); ok {
				batchJob.MarkReverted()
			}
			jobStatus = string(models.JobStatusReverted)
		} else if result.Failed == 0 {
			jobStatus = string(models.JobStatusOrganized)
		}

		// Build response
		resp := RevertResultResponse{
			JobID:     jobID,
			Status:    jobStatus,
			Total:     result.Total,
			Succeeded: result.Succeeded,
			Skipped:   result.Skipped,
			Failed:    result.Failed,
		}

		for _, o := range result.Outcomes {
			if o.Outcome != models.RevertOutcomeReverted {
				resp.Errors = append(resp.Errors, contracts.RevertFileError{
					OperationID:  o.OperationID,
					MovieID:      o.MovieID,
					OriginalPath: o.OriginalPath,
					NewPath:      o.NewPath,
					Error:        o.Error,
					Outcome:      o.Outcome,
					Reason:       o.Reason,
				})
			}
		}

		c.JSON(http.StatusOK, resp)

		if deps.EventEmitter != nil {
			sev := models.SeverityInfo
			if result.Failed > 0 && result.Succeeded > 0 {
				sev = models.SeverityWarn
			} else if result.Failed > 0 {
				sev = models.SeverityError
			}
			if err := deps.EventEmitter.EmitOrganizeEvent("revert", fmt.Sprintf("Batch revert completed for job %s", jobID), sev, map[string]interface{}{"job_id": jobID, "succeeded": result.Succeeded, "skipped": result.Skipped, "failed": result.Failed}); err != nil {
				logging.Warnf("Failed to emit batch revert event: %v", err)
			}
		}
	}
}

// revertOperation godoc
// @Summary Revert a specific movie within a batch job
// @Description Revert file operations for a specific movie within a batch job
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Param movieId path string true "Movie ID"
// @Success 200 {object} RevertResultResponse
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse "Revert is disabled"
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/jobs/{id}/operations/{movieId}/revert [post]
func revertOperation(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		movieID := c.Param("movieId")

		// Guard: revert must be explicitly enabled in config
		if !deps.GetConfig().Output.AllowRevert {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "Revert is disabled. Enable it in Settings > File Operations."})
			return
		}

		// Load job from DB — 404 if not found
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

		if job.Status != string(models.JobStatusOrganized) && job.Status != string(models.JobStatusReverted) {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Job is not in a revertible status"})
			return
		}

		// Call Reverter for individual movie
		result, err := deps.Reverter.RevertScrape(c.Request.Context(), jobID, movieID)
		if err != nil {
			if errors.Is(err, history.ErrBatchAlreadyReverted) {
				c.JSON(http.StatusConflict, ErrorResponse{Error: "Operation already reverted"})
				return
			}
			if errors.Is(err, history.ErrNoOperationsFound) {
				c.JSON(http.StatusNotFound, ErrorResponse{Error: "No operations found for the specified movie"})
				return
			}
			logging.Errorf("Failed to revert movie %s in batch %s: %v", movieID, jobID, err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to revert operation"})
			return
		}

		// After individual revert, check if ALL operations for the batch are now reverted
		// Only update job status to "reverted" when ALL operations are done
		pendingCount, err := deps.BatchFileOpRepo.CountByBatchJobIDAndRevertStatus(jobID, models.RevertStatusApplied)
		if err != nil {
			logging.Errorf("Failed to count pending operations for job %s: %v", jobID, err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to verify revert completion"})
			return
		}
		failedCount, err := deps.BatchFileOpRepo.CountByBatchJobIDAndRevertStatus(jobID, models.RevertStatusFailed)
		if err != nil {
			logging.Errorf("Failed to count failed operations for job %s: %v", jobID, err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to verify revert completion"})
			return
		}

		jobStatus := job.Status
		if pendingCount == 0 && failedCount == 0 {
			now := time.Now()
			job.Status = string(models.JobStatusReverted)
			job.RevertedAt = &now
			if err := deps.JobRepo.Update(job); err != nil {
				logging.Errorf("Failed to update job %s status to reverted: %v", jobID, err)
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to persist reverted status"})
				return
			}
			if batchJob, ok := deps.JobQueue.GetJobPointer(jobID); ok {
				batchJob.MarkReverted()
			}
			jobStatus = string(models.JobStatusReverted)
		}

		// Build response
		resp := RevertResultResponse{
			JobID:     jobID,
			Status:    jobStatus,
			Total:     result.Total,
			Succeeded: result.Succeeded,
			Skipped:   result.Skipped,
			Failed:    result.Failed,
		}

		for _, o := range result.Outcomes {
			if o.Outcome != models.RevertOutcomeReverted {
				resp.Errors = append(resp.Errors, contracts.RevertFileError{
					OperationID:  o.OperationID,
					MovieID:      o.MovieID,
					OriginalPath: o.OriginalPath,
					NewPath:      o.NewPath,
					Error:        o.Error,
					Outcome:      o.Outcome,
					Reason:       o.Reason,
				})
			}
		}

		c.JSON(http.StatusOK, resp)

		if deps.EventEmitter != nil {
			sev := models.SeverityInfo
			if result.Failed > 0 && result.Succeeded > 0 {
				sev = models.SeverityWarn
			} else if result.Failed > 0 {
				sev = models.SeverityError
			}
			if err := deps.EventEmitter.EmitOrganizeEvent("revert", fmt.Sprintf("Reverted movie %s in job %s", movieID, jobID), sev, map[string]interface{}{"job_id": jobID, "movie_id": movieID, "succeeded": result.Succeeded, "skipped": result.Skipped, "failed": result.Failed}); err != nil {
				logging.Warnf("Failed to emit operation revert event: %v", err)
			}
		}
	}
}
