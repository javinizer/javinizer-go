package batch

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// batchScrape godoc
// @Summary Batch scrape movies
// @Description Scrape metadata for multiple movies in batch. Automatically discovers and includes all parts of multi-part files.
// @Tags web
// @Accept json
// @Produce json
// @Param request body BatchScrapeRequest true "Batch scrape parameters"
// @Success 200 {object} BatchScrapeResponse
// @Failure 400 {object} ErrorResponse
// @Router /api/v1/batch/scrape [post]
func batchScrape(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req BatchScrapeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}

		// Apply preset if specified (overrides individual strategy fields)
		if req.Preset != "" {
			var presetErr error
			req.ScalarStrategy, req.ArrayStrategy, presetErr = nfo.ApplyPreset(req.Preset, req.ScalarStrategy, req.ArrayStrategy)
			if presetErr != nil {
				c.JSON(400, ErrorResponse{Error: presetErr.Error()})
				return
			}
			logging.Infof("Applied preset '%s': scalar=%s, array=%s", req.Preset, req.ScalarStrategy, req.ArrayStrategy)
		}

		// Security: Validate all submitted files against directory security settings
		cfg := deps.GetConfig()
		for _, filePath := range req.Files {
			dir := filepath.Dir(filePath)
			if !isDirAllowed(dir, cfg.API.Security.AllowedDirectories, cfg.API.Security.DeniedDirectories) {
				// Security: Don't leak directory paths in error messages
				c.JSON(403, ErrorResponse{Error: "Access denied to requested directory"})
				return
			}
		}

		// Auto-discover sibling multi-part files
		allFiles, fileMatchInfo := discoverSiblingPartsWithMetadata(req.Files, deps.GetMatcher(), cfg)

		if len(allFiles) > len(req.Files) {
			logging.Infof("Auto-discovered %d sibling files for batch job (original: %d, total: %d)",
				len(allFiles)-len(req.Files), len(req.Files), len(allFiles))
		}

		// Create job with all files (original + discovered siblings)
		job := deps.JobQueue.CreateJob(allFiles)

		// Set destination for the job
		if req.Destination != "" {
			job.SetDestination(req.Destination)
		}

		// Set folder mode overrides for the job (used during organization)
		if req.OperationMode != "" {
			job.SetOperationModeOverride(req.OperationMode)
		}

		// Populate file match metadata (multipart info from discovery)
		for path, info := range fileMatchInfo {
			job.SetFileMatchInfo(path, info)
		}

		// Start processing in background - use getters for thread-safe access
		go processBatchJob(&BatchProcessOptions{
			Job:                   job,
			JobQueue:              deps.JobQueue,
			Registry:              deps.GetRegistry(),
			Aggregator:            deps.GetAggregator(),
			MovieRepo:             deps.MovieRepo,
			Matcher:               deps.GetMatcher(),
			Strict:                req.Strict,
			Force:                 req.Force,
			UpdateMode:            req.Update,
			Destination:           req.Destination,
			Cfg:                   deps.GetConfig(),
			SelectedScrapers:      req.SelectedScrapers,
			ScalarStrategy:        req.ScalarStrategy,
			ArrayStrategy:         req.ArrayStrategy,
			DB:                    deps.DB,
			OperationModeOverride: req.OperationMode,
			Emitter:               deps.EventEmitter,
		})

		c.JSON(200, BatchScrapeResponse{
			JobID: job.ID,
		})
	}
}

// getBatchJob godoc
// @Summary Get batch job status
// @Description Retrieve the status of a batch scraping job
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} BatchJobResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/batch/{id} [get]
func getBatchJob(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		includeData := c.Query("include_data") == "true"

		if includeData {
			getBatchJobFull(deps, c, jobID)
		} else {
			getBatchJobSlim(deps, c, jobID)
		}
	}
}

func getBatchJobFull(deps *ServerDependencies, c *gin.Context, jobID string) {
	job, ok := deps.JobQueue.GetJob(jobID)
	if !ok {
		c.JSON(404, ErrorResponse{Error: "Job not found"})
		return
	}

	logging.Debugf("[GET /batch/%s] Returning full job with %d results, completed=%d, failed=%d",
		jobID, len(job.Results), job.Completed, job.Failed)

	var completedAt *string
	if job.CompletedAt != nil {
		str := job.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		completedAt = &str
	}

	results := make(map[string]*BatchFileResult)
	for filePath, fileResult := range job.Results {
		var endedAt *string
		if fileResult.EndedAt != nil {
			str := fileResult.EndedAt.Format("2006-01-02T15:04:05Z07:00")
			endedAt = &str
		}

		results[filePath] = &BatchFileResult{
			FilePath:       fileResult.FilePath,
			MovieID:        fileResult.MovieID,
			Status:         string(fileResult.Status),
			Error:          fileResult.Error,
			FieldSources:   fileResult.FieldSources,
			ActressSources: fileResult.ActressSources,
			Data:           fileResult.Data,
			StartedAt:      fileResult.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
			EndedAt:        endedAt,
			IsMultiPart:    fileResult.IsMultiPart,
			PartNumber:     fileResult.PartNumber,
			PartSuffix:     fileResult.PartSuffix,
		}
	}

	c.JSON(200, BatchJobResponse{
		ID:                    job.ID,
		Status:                string(job.Status),
		TotalFiles:            job.TotalFiles,
		Completed:             job.Completed,
		Failed:                job.Failed,
		Excluded:              job.Excluded,
		Progress:              job.Progress,
		Destination:           job.Destination,
		Results:               results,
		StartedAt:             job.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
		CompletedAt:           completedAt,
		OperationModeOverride: job.OperationModeOverride,
		PersistError:          job.PersistError,
	})
}

func getBatchJobSlim(deps *ServerDependencies, c *gin.Context, jobID string) {
	jobPtr, ok := deps.JobQueue.GetJobPointer(jobID)
	if !ok {
		c.JSON(404, ErrorResponse{Error: "Job not found"})
		return
	}

	slim := jobPtr.GetStatusSlim()

	logging.Debugf("[GET /batch/%s] Returning slim job with %d results, completed=%d, failed=%d",
		jobID, len(slim.Results), slim.Completed, slim.Failed)

	var completedAt *string
	if slim.CompletedAt != nil {
		str := slim.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		completedAt = &str
	}

	results := make(map[string]*contracts.BatchFileResultSlim)
	for filePath, fileResult := range slim.Results {
		var endedAt *string
		if fileResult.EndedAt != nil {
			str := fileResult.EndedAt.Format("2006-01-02T15:04:05Z07:00")
			endedAt = &str
		}

		results[filePath] = &contracts.BatchFileResultSlim{
			FilePath:       fileResult.FilePath,
			MovieID:        fileResult.MovieID,
			Status:         string(fileResult.Status),
			Error:          fileResult.Error,
			FieldSources:   fileResult.FieldSources,
			ActressSources: fileResult.ActressSources,
			StartedAt:      fileResult.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
			EndedAt:        endedAt,
			IsMultiPart:    fileResult.IsMultiPart,
			PartNumber:     fileResult.PartNumber,
			PartSuffix:     fileResult.PartSuffix,
		}
	}

	c.JSON(200, contracts.BatchJobResponseSlim{
		ID:                    slim.ID,
		Status:                string(slim.Status),
		TotalFiles:            slim.TotalFiles,
		Completed:             slim.Completed,
		Failed:                slim.Failed,
		Excluded:              slim.Excluded,
		Progress:              slim.Progress,
		Destination:           slim.Destination,
		Results:               results,
		StartedAt:             slim.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
		CompletedAt:           completedAt,
		OperationModeOverride: slim.OperationModeOverride,
		PersistError:          slim.PersistError,
	})
}

// cancelBatchJob godoc
// @Summary Cancel batch job
// @Description Cancel a running batch scraping job
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/batch/{id}/cancel [post]
func cancelBatchJob(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")

		// Use GetJobPointer to get the real job (not a snapshot) so Cancel() works
		job, ok := deps.JobQueue.GetJobPointer(jobID)
		if !ok {
			c.JSON(404, ErrorResponse{Error: "Job not found"})
			return
		}

		job.Cancel()

		// Cleanup temp posters for cancelled job (batch job is gone, temp posters no longer needed)
		// Use job's stored TempDir for consistent cleanup path
		tempDir := job.GetTempDir()
		if tempDir == "" {
			tempDir = deps.GetConfig().System.TempDir
		}
		go cleanupJobTempPosters(jobID, tempDir)

		c.JSON(200, gin.H{"message": "Job cancelled successfully"})
	}
}

// deleteBatchJob godoc
// @Summary Delete batch job
// @Description Delete a completed or cancelled batch job and its temp files. Running jobs must be cancelled first.
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/batch/{id} [delete]
func deleteBatchJob(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")

		job, ok := deps.JobQueue.GetJobPointer(jobID)
		if !ok {
			c.JSON(404, ErrorResponse{Error: "Job not found"})
			return
		}

		if job.GetStatus().Status == worker.JobStatusRunning {
			c.JSON(400, ErrorResponse{Error: "Cannot delete running job. Cancel it first."})
			return
		}

		tempDir := job.GetTempDir()
		if tempDir == "" {
			tempDir = deps.GetConfig().System.TempDir
		}

		if err := deps.JobQueue.DeleteJob(jobID, tempDir); err != nil {
			c.JSON(500, ErrorResponse{Error: fmt.Sprintf("Failed to delete job: %v", err)})
			return
		}

		c.JSON(200, gin.H{"message": "Job deleted successfully"})
	}
}

// listBatchJobs godoc
// @Summary List batch jobs
// @Description Get a list of batch jobs with operation counts
// @Tags web
// @Produce json
// @Success 200 {object} contracts.BatchJobListResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/batch [get]
func listBatchJobs(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobs, err := deps.JobRepo.List()
		if err != nil {
			c.JSON(500, ErrorResponse{Error: "Failed to list jobs"})
			return
		}

		response := contracts.BatchJobListResponse{
			Jobs: make([]contracts.BatchJobResponse, 0, len(jobs)),
		}

		for _, job := range jobs {
			var completedAt *string
			if job.CompletedAt != nil {
				str := job.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
				completedAt = &str
			}

			var results map[string]*contracts.BatchFileResult
			if job.Results != "" {
				if err := json.Unmarshal([]byte(job.Results), &results); err != nil {
					results = make(map[string]*contracts.BatchFileResult)
				}
			} else {
				results = make(map[string]*contracts.BatchFileResult)
			}

			var excluded map[string]bool
			if job.Excluded != "" {
				if err := json.Unmarshal([]byte(job.Excluded), &excluded); err != nil {
					excluded = make(map[string]bool)
				}
			} else {
				excluded = make(map[string]bool)
			}

			opCount, err := deps.BatchFileOpRepo.CountByBatchJobID(job.ID)
			if err != nil {
				logging.Errorf("Failed to count operations for job %s: %v", job.ID, err)
				c.JSON(500, ErrorResponse{Error: "Failed to retrieve operation counts"})
				return
			}

			revertedCount, err := deps.BatchFileOpRepo.CountByBatchJobIDAndRevertStatus(job.ID, models.RevertStatusReverted)
			if err != nil {
				logging.Errorf("Failed to count reverted operations for job %s: %v", job.ID, err)
				c.JSON(500, ErrorResponse{Error: "Failed to retrieve revert counts"})
				return
			}

			response.Jobs = append(response.Jobs, contracts.BatchJobResponse{
				ID:             job.ID,
				Status:         job.Status,
				TotalFiles:     job.TotalFiles,
				Completed:      job.Completed,
				Failed:         job.Failed,
				OperationCount: opCount,
				RevertedCount:  revertedCount,
				Excluded:       excluded,
				Progress:       job.Progress,
				Destination:    job.Destination,
				Results:        results,
				StartedAt:      job.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
				CompletedAt:    completedAt,
			})
		}

		c.JSON(200, response)
	}
}
