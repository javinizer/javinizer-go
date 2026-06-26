package jobs

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// revertCheck godoc
// @Summary Check for overlapping batches before revert
// @Description Returns advisory information about later batches that share file paths with the target batch. This does NOT block the revert — it provides warnings only (D-07).
// @Tags jobs
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} contracts.RevertCheckResponse
// @Failure 403 {object} contracts.ErrorResponse "Revert is disabled"
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/jobs/{id}/revert-check [get]
func revertCheck(deps JobDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")

		// Guard: revert must be explicitly enabled in config
		if !deps.AllowRevert {
			c.JSON(http.StatusForbidden, contracts.ErrorResponse{Error: "Revert is disabled. Enable it in Settings > File Operations."})
			return
		}

		// Load target job — 404 if not found
		job, err := deps.JobRepo.FindByID(c.Request.Context(), jobID)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to retrieve job"})
			return
		}

		// Load all operations for the target job
		targetOps, err := deps.BatchFileOpRepo.FindByBatchJobID(c.Request.Context(), jobID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to retrieve operations"})
			return
		}

		// Build a set of paths from the target job's applied operations
		targetPaths := make(map[string]bool)
		for _, op := range targetOps {
			if op.RevertStatus != models.RevertStatusApplied {
				continue
			}
			targetPaths[op.OriginalPath] = true
			targetPaths[op.NewPath] = true
		}

		// No applied operations — no overlaps possible
		if len(targetPaths) == 0 {
			c.JSON(http.StatusOK, contracts.RevertCheckResponse{
				JobID:              jobID,
				OverlappingBatches: []contracts.OverlapInfo{},
			})
			return
		}

		// Find later batches that have path overlaps
		allJobs, err := deps.JobRepo.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to retrieve job list"})
			return
		}

		var overlappingBatches []contracts.OverlapInfo
		for i := range allJobs {
			laterJob := &allJobs[i]

			// Skip the target job itself and already-reverted jobs
			if laterJob.ID == jobID || laterJob.Status == models.JobStatusReverted {
				continue
			}

			// Only check later batches (created at or after the target job)
			// Use After with ID tiebreaker to avoid false negatives for equal timestamps
			if laterJob.StartedAt.Before(job.StartedAt) {
				continue
			}
			if laterJob.StartedAt.Equal(job.StartedAt) && laterJob.ID <= jobID {
				continue
			}

			laterOps, err := deps.BatchFileOpRepo.FindByBatchJobID(c.Request.Context(), laterJob.ID)
			if err != nil {
				// Fail closed: this check protects destructive revert work, so a
				// read failure must not produce an incomplete overlapping-batches
				// list that could falsely mark the revert safe.
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to evaluate overlapping batches"})
				return
			}

			overlapCount := 0
			for _, laterOp := range laterOps {
				if laterOp.RevertStatus != models.RevertStatusApplied {
					continue
				}
				// Check if any of the later batch's paths overlap with the target batch's paths
				if targetPaths[laterOp.OriginalPath] || targetPaths[laterOp.NewPath] {
					overlapCount++
				}
			}

			if overlapCount > 0 {
				createdAt := ""
				if !laterJob.StartedAt.IsZero() {
					createdAt = laterJob.StartedAt.UTC().Format("2006-01-02T15:04:05Z")
				}
				overlappingBatches = append(overlappingBatches, contracts.OverlapInfo{
					JobID:          laterJob.ID,
					CreatedAt:      createdAt,
					OperationCount: overlapCount,
				})
			}
		}

		// Ensure we return an empty array (not nil) when no overlaps found
		if overlappingBatches == nil {
			overlappingBatches = []contracts.OverlapInfo{}
		}

		c.JSON(http.StatusOK, contracts.RevertCheckResponse{
			JobID:              jobID,
			OverlappingBatches: overlappingBatches,
		})
	}
}
