package batch

import (
	"context"
	"fmt"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/types"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// organizeJob godoc
// @Summary Organize batch job files
// @Description Organize files from a completed scrape job (move files, download artwork, create NFO)
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param request body OrganizeRequest true "Organization parameters"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/batch/{id}/organize [post]
func organizeJob(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")

		var req OrganizeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}

		// Use GetJobPointer to get the real job (not a snapshot) for mutations
		job, ok := deps.JobQueue.GetJobPointer(jobID)
		if !ok {
			c.JSON(404, ErrorResponse{Error: "Job not found"})
			return
		}

		// Check if job is in correct state (completed scraping)
		status := job.GetStatus()
		if status.Status != worker.JobStatusCompleted {
			c.JSON(400, ErrorResponse{Error: "Job must be completed before organizing"})
			return
		}

		cfg := deps.GetConfig()

		// Determine effective operation mode from request or config
		effectiveMode := cfg.Output.GetOperationMode()
		if req.OperationMode != "" {
			parsed, err := types.ParseOperationMode(req.OperationMode)
			if err != nil {
				c.JSON(400, ErrorResponse{Error: fmt.Sprintf("Invalid operation_mode: %v", err)})
				return
			}
			effectiveMode = parsed
		}

		// Allow organize for organize and in-place modes only
		// Preview mode should use the preview endpoint, not organize
		// Metadata-only mode does not perform file operations
		if effectiveMode == types.OperationModePreview {
			c.JSON(400, ErrorResponse{Error: "Preview mode should use the preview endpoint, not organize"})
			return
		}
		if effectiveMode == types.OperationModeMetadataOnly {
			c.JSON(400, ErrorResponse{Error: "Organize not available in metadata-only mode"})
			return
		}

		// Security: Validate destination directory against allow/deny lists
		if !isDirAllowed(req.Destination, cfg.API.Security.AllowedDirectories, cfg.API.Security.DeniedDirectories) {
			c.JSON(403, ErrorResponse{Error: "Access denied to requested directory"})
			return
		}

		// Reuse the batch job lifecycle for organize progress polling.
		job.MarkStarted()

		// Set operation mode override on the job for processOrganizeJob
		if req.OperationMode != "" {
			job.SetOperationModeOverride(req.OperationMode)
		}

		// Start organization in background with panic recovery
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logging.Errorf("Organize job %s panicked: %v", jobID, r)
					job.MarkFailed()
					if deps.JobQueue != nil {
						deps.JobQueue.PersistJob(job)
					}
					broadcastProgress(&ws.ProgressMessage{
						JobID:   jobID,
						Status:  "error",
						Message: fmt.Sprintf("Organize job panicked: %v", r),
					})
				}
			}()
			ctx, cancel := context.WithCancel(c.Request.Context())
			job.SetCancelFunc(cancel)
			defer cancel()
			processOrganizeJob(ctx, job, deps.JobQueue, req.Destination, req.CopyOnly, req.LinkMode, deps.DB, cfg, deps.GetRegistry(), deps.EventEmitter)
		}()

		c.JSON(200, gin.H{"message": "Organization started"})
	}
}

// updateBatchJob godoc
// @Summary Update batch job files
// @Description Generate NFOs and download media files in place without moving video files
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/batch/{id}/update [post]
func updateBatchJob(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")

		// Use GetJobPointer to get the real job (not a snapshot) for mutations
		job, ok := deps.JobQueue.GetJobPointer(jobID)
		if !ok {
			c.JSON(404, ErrorResponse{Error: "Job not found"})
			return
		}

		// Check if job is in correct state (completed scraping)
		status := job.GetStatus()
		if status.Status != worker.JobStatusCompleted {
			c.JSON(400, ErrorResponse{Error: "Job must be completed before updating"})
			return
		}

		// Reuse the batch job lifecycle for update progress polling.
		job.MarkStarted()

		// Start update in background with panic recovery
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logging.Errorf("Update job %s panicked: %v", jobID, r)
					job.MarkFailed()
					if deps.JobQueue != nil {
						deps.JobQueue.PersistJob(job)
					}
					broadcastProgress(&ws.ProgressMessage{
						JobID:   jobID,
						Status:  "error",
						Message: fmt.Sprintf("Update job panicked: %v", r),
					})
				}
			}()
			processUpdateJob(job, deps.GetConfig(), deps.DB, deps.GetRegistry(), deps.EventEmitter)
		}()

		c.JSON(200, gin.H{"message": "Update started"})
	}
}

// previewOrganize godoc
// @Summary Preview organize output
// @Description Generate a preview of the expected output structure for a movie
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param movieId path string true "Movie ID"
// @Param request body OrganizePreviewRequest true "Preview parameters"
// @Success 200 {object} OrganizePreviewResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/batch/{id}/movies/{movieId}/preview [post]
func previewOrganize(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		movieID := c.Param("movieId")

		var req OrganizePreviewRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}

		cfg := deps.GetConfig()

		// Determine operation mode for preview
		effectiveMode := cfg.Output.GetOperationMode()
		if req.OperationMode != "" {
			parsed, err := types.ParseOperationMode(req.OperationMode)
			if err != nil {
				c.JSON(400, ErrorResponse{Error: fmt.Sprintf("Invalid operation_mode: %v", err)})
				return
			}
			effectiveMode = parsed
		}

		// Security: Validate destination directory for modes that use it
		if effectiveMode == types.OperationModeOrganize || effectiveMode == types.OperationModePreview {
			if !isDirAllowed(req.Destination, cfg.API.Security.AllowedDirectories, cfg.API.Security.DeniedDirectories) {
				c.JSON(403, ErrorResponse{Error: "Access denied to requested directory"})
				return
			}
		}

		// Get the batch job (already a snapshot, don't call GetStatus() again)
		job, ok := deps.JobQueue.GetJob(jobID)
		if !ok {
			c.JSON(404, ErrorResponse{Error: "Job not found"})
			return
		}

		// Find the movie and collect all file results for this movie (for multi-part support)
		var movie *models.Movie
		fileResults := make([]*worker.FileResult, 0)

		// Collect all results matching this movieID
		for _, result := range job.Results {
			if result.MovieID == movieID {
				if result.Data != nil {
					if m, ok := result.Data.(*models.Movie); ok {
						movie = m
					}
				}
				fileResults = append(fileResults, result)
			}
		}

		// If not found by MovieID, try searching by the actual movie.ID (in case of content ID resolution)
		if movie == nil {
			for _, result := range job.Results {
				if result.Data != nil {
					if m, ok := result.Data.(*models.Movie); ok && m.ID == movieID {
						movie = m
						fileResults = append(fileResults, result)
					}
				}
			}
		}

		if movie == nil {
			c.JSON(404, ErrorResponse{Error: fmt.Sprintf("Movie %s not found in job", movieID)})
			return
		}

		// Sort fileResults by PartNumber to ensure deterministic order
		// (map iteration order is random in Go, so fileResults[0] might not be part 1)
		sort.Slice(fileResults, func(i, j int) bool {
			return fileResults[i].PartNumber < fileResults[j].PartNumber
		})

		// Use the helper function from processors.go - pass all file results for multi-part support
		preview := generatePreview(movie, fileResults, req.Destination, deps.GetConfig(), effectiveMode)
		c.JSON(200, preview)
	}
}
