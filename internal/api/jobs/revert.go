package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
)

// buildRevertResponse constructs a contracts.RevertResultResponse from the revert result,
// populating error details for any outcomes that are not fully reverted.
func buildRevertResponse(jobID string, jobStatus models.JobStatus, result *history.RevertBatchResult) contracts.RevertResultResponse {
	resp := contracts.RevertResultResponse{
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

	return resp
}

// emitRevertEvent emits a best-effort revert event via the event emitter.
// Failures are non-critical and already logged by the emitter.
func emitRevertEvent(ctx context.Context, emitter eventlog.EventEmitter, message string, jobID string, result *history.RevertBatchResult, extraFields map[string]any) {
	if emitter == nil {
		return
	}
	sev := models.SeverityInfo
	if result.Failed > 0 && result.Succeeded > 0 {
		sev = models.SeverityWarn
	} else if result.Failed > 0 {
		sev = models.SeverityError
	}
	fields := map[string]any{"job_id": jobID, "succeeded": result.Succeeded, "skipped": result.Skipped, "failed": result.Failed}
	for k, v := range extraFields {
		fields[k] = v
	}
	_ = emitter.EmitOrganizeEvent(ctx, "revert", message, sev, fields)
}

// revertBatch godoc
// @Summary Revert a batch job
// @Description Revert all file organization operations for a batch job, moving files back to original paths
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} contracts.RevertResultResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 403 {object} contracts.ErrorResponse "Revert is disabled"
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/jobs/{id}/revert [post]
func revertBatch(deps JobDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")

		// Guard: revert must be explicitly enabled in config
		if !deps.AllowRevert {
			c.JSON(http.StatusForbidden, contracts.ErrorResponse{Error: "Revert is disabled. Enable it in Settings > File Operations."})
			return
		}

		// Load job from DB — 404 if not found
		job, err := deps.JobRepo.FindByID(c.Request.Context(), jobID)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to retrieve job"})
			return
		}

		if job.Status == models.JobStatusReverted {
			c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: "Batch already reverted"})
			return
		}
		if job.Status != models.JobStatusOrganized {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Job is not in organized status"})
			return
		}

		result, err := deps.Reverter.RevertBatch(c.Request.Context(), jobID)
		if err != nil {
			if errors.Is(err, history.ErrBatchAlreadyReverted) {
				c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: "Batch already reverted"})
				return
			}
			if errors.Is(err, history.ErrNoOperationsFound) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "No operations found for batch"})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to revert batch"})
			return
		}

		jobStatus := models.JobStatusOrganized
		if result.Failed == 0 && result.Skipped == 0 {
			now := time.Now()
			job.Status = models.JobStatusReverted
			job.RevertedAt = &now
			if err := deps.JobRepo.Update(c.Request.Context(), job); err != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to persist reverted status"})
				return
			}
			if batchJob, ok := deps.JobStore.GetJobForControl(jobID); ok {
				batchJob.MarkReverted()
			}
			jobStatus = models.JobStatusReverted
		} else if result.Failed == 0 {
			jobStatus = models.JobStatusOrganized
		}

		c.JSON(http.StatusOK, buildRevertResponse(jobID, jobStatus, result))

		emitRevertEvent(c.Request.Context(), deps.EventEmitter, fmt.Sprintf("Batch revert completed for job %s", jobID), jobID, result, nil)
	}
}

// revertOperation godoc
// @Summary Revert a specific movie within a batch job
// @Description Revert file operations for a specific movie within a batch job
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Param movieId path string true "Movie ID"
// @Success 200 {object} contracts.RevertResultResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 403 {object} contracts.ErrorResponse "Revert is disabled"
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/jobs/{id}/operations/{movieId}/revert [post]
func revertOperation(deps JobDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		movieID := c.Param("movieId")

		// Guard: revert must be explicitly enabled in config
		if !deps.AllowRevert {
			c.JSON(http.StatusForbidden, contracts.ErrorResponse{Error: "Revert is disabled. Enable it in Settings > File Operations."})
			return
		}

		// Load job from DB — 404 if not found
		job, err := deps.JobRepo.FindByID(c.Request.Context(), jobID)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to retrieve job"})
			return
		}

		if job.Status != models.JobStatusOrganized && job.Status != models.JobStatusReverted {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "Job is not in a revertible status"})
			return
		}

		// Call Reverter for individual movie
		result, err := deps.Reverter.RevertScrape(c.Request.Context(), jobID, movieID)
		if err != nil {
			if errors.Is(err, history.ErrBatchAlreadyReverted) {
				c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: "Operation already reverted"})
				return
			}
			if errors.Is(err, history.ErrNoOperationsFound) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "No operations found for the specified movie"})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to revert operation"})
			return
		}

		// After individual revert, check if ALL operations for the batch are now reverted
		// Only update job status to "reverted" when ALL operations are done
		pendingCount, err := deps.BatchFileOpRepo.CountByBatchJobIDAndRevertStatus(c.Request.Context(), jobID, models.RevertStatusApplied)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to verify revert completion"})
			return
		}
		failedCount, err := deps.BatchFileOpRepo.CountByBatchJobIDAndRevertStatus(c.Request.Context(), jobID, models.RevertStatusFailed)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to verify revert completion"})
			return
		}

		jobStatus := models.JobStatusOrganized
		if pendingCount == 0 && failedCount == 0 {
			now := time.Now()
			job.Status = models.JobStatusReverted
			job.RevertedAt = &now
			if err := deps.JobRepo.Update(c.Request.Context(), job); err != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to persist reverted status"})
				return
			}
			if batchJob, ok := deps.JobStore.GetJobForControl(jobID); ok {
				batchJob.MarkReverted()
			}
			jobStatus = models.JobStatusReverted
		}

		c.JSON(http.StatusOK, buildRevertResponse(jobID, jobStatus, result))

		emitRevertEvent(c.Request.Context(), deps.EventEmitter, fmt.Sprintf("Reverted movie %s in job %s", movieID, jobID), jobID, result, map[string]any{"movie_id": movieID})
	}
}
