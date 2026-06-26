package batch

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
)

// batchScrape godoc
// @Summary Batch scrape movies
// @Description Scrape metadata for multiple movies in batch. Automatically discovers and includes all parts of multi-part files.
// @Tags web
// @Accept json
// @Produce json
// @Param request body contracts.BatchScrapeRequest true "Batch scrape parameters"
// @Success 200 {object} contracts.BatchScrapeResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Router /api/v1/batch/scrape [post]
func batchScrape(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req contracts.BatchScrapeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		// Security: Validate all submitted files against directory security settings
		deps := rt.Deps()
		apiCfg := rt.GetAPIConfig()
		secCfg := apiCfg.SecurityConfig()
		for _, filePath := range req.Files {
			dir := filepath.Dir(filePath)
			if !isDirAllowed(deps.GetFs(), dir, secCfg) {
				// Security: Don't leak directory paths in error messages
				c.JSON(http.StatusForbidden, contracts.ErrorResponse{Error: "Access denied to requested directory"})
				return
			}
		}

		output, err := StartScrapeUseCase(c.Request.Context(), rt, StartScrapeInput{
			Files:            req.Files,
			Destination:      req.Destination,
			OperationMode:    req.OperationMode,
			Preset:           req.Preset,
			ScalarStrategy:   req.ScalarStrategy,
			ArrayStrategy:    req.ArrayStrategy,
			Update:           &req.Update,
			SelectedScrapers: req.SelectedScrapers,
			Strict:           req.Strict,
			Force:            req.Force,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, contracts.BatchScrapeResponse{
			JobID: output.JobID,
		})
	}
}

// getBatchJob godoc
// @Summary Get batch job status
// @Description Retrieve the status of a batch scraping job
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} contracts.BatchJobResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id} [get]
func getBatchJob(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")
		includeData := c.Query("include_data") == "true"

		if includeData {
			getBatchJobFull(deps, c, jobID)
		} else {
			getBatchJobSlim(deps, c, jobID)
		}
	}
}

func getBatchJobFull(deps *core.APIDeps, c *gin.Context, jobID string) {
	job, ok := deps.GetJobStore().GetJob(jobID)
	if !ok {
		c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
		return
	}

	logging.Debugf("[GET /batch/%s] Returning full job with %d results, completed=%d, failed=%d",
		jobID, len(job.Results), job.Completed, job.Failed)

	c.JSON(http.StatusOK, buildBatchJobResponse(job))
}

func getBatchJobSlim(deps *core.APIDeps, c *gin.Context, jobID string) {
	status, ok := deps.GetJobStore().GetJob(jobID)
	if !ok {
		c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
		return
	}

	logging.Debugf("[GET /batch/%s] Returning slim job with %d results, completed=%d, failed=%d",
		jobID, len(status.Results), status.Completed, status.Failed)

	c.JSON(http.StatusOK, buildBatchJobSlimResponse(status))
}

// cancelBatchJob godoc
// @Summary Cancel batch job
// @Description Cancel a running batch scraping job
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/cancel [post]
func cancelBatchJob(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		job, err := prepareBatchRequest(deps, rt, c, withSkipRunningCheck())
		if err != nil {
			return
		}

		status := job.GetStatus().Status
		switch status {
		case models.JobStatusCompleted, models.JobStatusFailed, models.JobStatusCancelled, models.JobStatusOrganized, models.JobStatusReverted:
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: fmt.Sprintf("Job is already %s", string(status))})
			return
		}

		job.Cancel()

		tempDir := job.GetStatus().TempDir
		if tempDir == "" {
			tempDir = rt.GetAPIConfig().BatchConfig().TempDir
		}

		// Wait for the job to reach a terminal state before cleaning up temp posters.
		// Cancel() only sets the cancelled flag and cancels the context — the job's
		// scrape/apply goroutines may still be running and reading/writing poster files.
		// Deleting the temp poster directory while they're active causes file-not-found
		// errors or partial writes.
		go func() {
			select {
			case <-job.Done():
			case <-time.After(5 * time.Second):
				logging.Warnf("CancelJob: timed out waiting for job %s to finish, proceeding with cleanup", job.GetID())
			}
			cleanupJobTempPosters(deps.GetFs(), job.GetID(), tempDir)
		}()

		c.JSON(http.StatusOK, gin.H{"message": "Job cancelled successfully"})
	}
}

// deleteBatchJob godoc
// @Summary Delete batch job
// @Description Delete a completed or cancelled batch job and its temp files. Running jobs must be cancelled first.
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id} [delete]
func deleteBatchJob(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		jobID := c.Param("id")

		if err := deps.GetJobStore().DeleteJob(jobID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: err.Error()})
				return
			}
			if strings.Contains(err.Error(), "cannot delete running job") {
				c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: fmt.Sprintf("Failed to delete job: %v", err)})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Job deleted successfully"})
	}
}

// listBatchJobs godoc
// @Summary List batch jobs
// @Description Get a paginated list of batch jobs with operation counts. Only job metadata is included; use GET /batch/{id} with include_data=true for full results.
// @Tags web
// @Produce json
// @Param limit query int false "Maximum number of jobs to return (default 50, max 200)" minimum(1) maximum(200) default(50)
// @Param offset query int false "Number of jobs to skip (default 0)" minimum(0) default(0)
// @Success 200 {object} contracts.BatchJobListResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/batch [get]
func listBatchJobs(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		// Parse pagination parameters with sensible defaults
		limit := 50
		offset := 0

		if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 {
			limit = l
			if limit > 200 {
				limit = 200
			}
		}
		if o, err := strconv.Atoi(c.Query("offset")); err == nil && o >= 0 {
			offset = o
		}

		output, err := ListJobsUseCase(c.Request.Context(), deps, ListJobsInput{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		c.JSON(http.StatusOK, contracts.BatchJobListResponse{
			Jobs:  output.Jobs,
			Total: output.Total,
		})
	}
}

// parseAndConvertJobResults handles the three DB persistence formats for job results
// and converts them into BatchFileResult for API responses. It delegates the actual
// format detection and parsing to worker.ParseJobResultsJSON, then converts
// MovieResult → BatchFileResult via movieResultToResponse.
//
// fs is used to drop cropped_poster_url values whose temp artifact is missing on
// disk (worker.ClearMissingTempPosters), keeping the list view consistent with the
// detail view so the frontend falls back to poster_url instead of a broken image.
// A nil fs falls back to the real filesystem.
func parseAndConvertJobResults(job *models.Job, fs afero.Fs) map[string]*contracts.BatchFileResult {
	if job.Results == "" {
		return make(map[string]*contracts.BatchFileResult)
	}

	parsed, err := worker.ParseJobResultsJSON([]byte(job.Results))
	if err != nil {
		logging.Warnf("Failed to parse results for job %s: %v", job.ID, err)
		return make(map[string]*contracts.BatchFileResult)
	}

	// Drop cropped_poster_url values pointing at temp posters that no longer
	// exist on disk (e.g. after upgrade from a version that did not preserve the
	// temp dir). Mirrors reconstructBatchJob so list and detail views agree.
	worker.ClearMissingTempPosters(fs, job.TempDir, job.ID, parsed.Results)

	results := make(map[string]*contracts.BatchFileResult, len(parsed.Results))
	for filePath, mr := range parsed.Results {
		var prov *worker.ProvenanceData
		if parsed.Provenance != nil {
			prov = parsed.Provenance[filePath]
		}
		result := movieResultToResponse(mr, prov)
		if result.ResultID == "" {
			// Deterministic UUID for legacy records that lack result_id.
			// Uses uuid.NewSHA1 so the same file path always gets the same ID
			// across API calls, preventing UI state thrash on polling.
			result.ResultID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(filePath)).String()
		}
		results[filePath] = result
	}
	return results
}
