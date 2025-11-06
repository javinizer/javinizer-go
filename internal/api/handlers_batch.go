package api

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// batchScrape godoc
// @Summary Batch scrape movies
// @Description Scrape metadata for multiple movies in batch
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

		// Create job
		job := deps.JobQueue.CreateJob(req.Files)

		// Start processing in background - use getters for thread-safe access
		go processBatchJob(job, deps.GetRegistry(), deps.GetAggregator(), deps.MovieRepo, deps.GetMatcher(), req.Strict, req.Force, req.Update, req.Destination, deps.GetConfig(), req.SelectedScrapers, deps.DB)

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

		// GetJob() now returns a snapshot (result of GetStatus()), not a pointer
		// So we don't need to call GetStatus() again
		job, ok := deps.JobQueue.GetJob(jobID)
		if !ok {
			c.JSON(404, ErrorResponse{Error: "Job not found"})
			return
		}

		// Debug logging to trace the state
		logging.Debugf("[GET /batch/%s] Returning job with %d results, completed=%d, failed=%d",
			jobID, len(job.Results), job.Completed, job.Failed)

		var completedAt *string
		if job.CompletedAt != nil {
			str := job.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
			completedAt = &str
		}

		// Transform results from worker.FileResult to BatchFileResult
		results := make(map[string]*BatchFileResult)
		for filePath, fileResult := range job.Results {
			var endedAt *string
			if fileResult.EndedAt != nil {
				str := fileResult.EndedAt.Format("2006-01-02T15:04:05Z07:00")
				endedAt = &str
			}

			results[filePath] = &BatchFileResult{
				FilePath:  fileResult.FilePath,
				MovieID:   fileResult.MovieID,
				Status:    string(fileResult.Status),
				Error:     fileResult.Error,
				Data:      fileResult.Data,
				StartedAt: fileResult.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
				EndedAt:   endedAt,
			}
		}

		c.JSON(200, BatchJobResponse{
			ID:          job.ID,
			Status:      string(job.Status),
			TotalFiles:  job.TotalFiles,
			Completed:   job.Completed,
			Failed:      job.Failed,
			Progress:    job.Progress,
			Results:     results,
			StartedAt:   job.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
			CompletedAt: completedAt,
		})
	}
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

		c.JSON(200, gin.H{"message": "Job cancelled successfully"})
	}
}

// updateBatchMovie godoc
// @Summary Update movie in batch job
// @Description Update a movie's metadata within a batch job's results
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param movieId path string true "Movie ID"
// @Param request body UpdateMovieRequest true "Updated movie data"
// @Success 200 {object} MovieResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/batch/{id}/movies/{movieId} [patch]
func updateBatchMovie(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		movieID := c.Param("movieId")

		var req UpdateMovieRequest
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

		// Get a snapshot to search for the file
		status := job.GetStatus()
		var foundFilePath string
		var foundResult *worker.FileResult
		for filePath, result := range status.Results {
			if result.MovieID == movieID {
				foundFilePath = filePath
				foundResult = result
				break
			}
		}

		// If not found by MovieID, try searching by the actual movie.ID (in case of content ID resolution)
		if foundResult == nil {
			for filePath, result := range status.Results {
				if result.Data != nil {
					if m, ok := result.Data.(*models.Movie); ok && m.ID == movieID {
						foundFilePath = filePath
						foundResult = result
						break
					}
				}
			}
		}

		if foundResult == nil {
			c.JSON(404, ErrorResponse{Error: fmt.Sprintf("Movie %s not found in job", movieID)})
			return
		}

		// Update database first (before updating job state) to complete any mutations
		// before exposing the pointer to concurrent readers
		if err := deps.MovieRepo.Upsert(req.Movie); err != nil {
			logging.Errorf("Failed to update movie in database: %v", err)
			// Don't fail the request if DB update fails
		}

		// Use AtomicUpdateFileResult to safely update the movie data without race conditions
		err := job.AtomicUpdateFileResult(foundFilePath, func(current *worker.FileResult) (*worker.FileResult, error) {
			// Update the movie data
			current.Data = req.Movie
			// Always sync MovieID to keep job state consistent (handles both content ID resolution and user edits)
			current.MovieID = req.Movie.ID
			return current, nil
		})

		if err != nil {
			logging.Errorf("Failed to update file result: %v", err)
			c.JSON(500, ErrorResponse{Error: fmt.Sprintf("Failed to update job state: %v", err)})
			return
		}

		c.JSON(200, MovieResponse{Movie: req.Movie})
	}
}

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

		// Start organization in background - use getter for thread-safe access
		go processOrganizeJob(job, deps.GetMatcher(), req.Destination, req.CopyOnly, deps.DB, deps.GetConfig())

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

		// Start update in background - use getter for thread-safe access
		go processUpdateJob(job, deps.GetConfig(), deps.DB)

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

		// Get the batch job (already a snapshot, don't call GetStatus() again)
		job, ok := deps.JobQueue.GetJob(jobID)
		if !ok {
			c.JSON(404, ErrorResponse{Error: "Job not found"})
			return
		}

		// Find the movie in the job results (job is already a snapshot, use it directly)
		var movie *models.Movie
		for _, result := range job.Results {
			if result.MovieID == movieID {
				if result.Data != nil {
					var ok bool
					movie, ok = result.Data.(*models.Movie)
					if !ok {
						c.JSON(500, ErrorResponse{Error: "Invalid movie data type"})
						return
					}
				}
				break
			}
		}

		// If not found by MovieID, try searching by the actual movie.ID (in case of content ID resolution)
		if movie == nil {
			for _, result := range job.Results {
				if result.Data != nil {
					if m, ok := result.Data.(*models.Movie); ok && m.ID == movieID {
						movie = m
						break
					}
				}
			}
		}

		if movie == nil {
			c.JSON(404, ErrorResponse{Error: fmt.Sprintf("Movie %s not found in job", movieID)})
			return
		}

		// Use the helper function from processors.go - read config from deps
		preview := generatePreview(movie, req.Destination, deps.GetConfig())
		c.JSON(200, preview)
	}
}
