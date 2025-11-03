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
		go processBatchJob(job, deps.GetRegistry(), deps.GetAggregator(), deps.MovieRepo, deps.GetMatcher(), req.Strict, req.Force, req.Destination, deps.GetConfig(), req.SelectedScrapers)

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

		job, ok := deps.JobQueue.GetJob(jobID)
		if !ok {
			c.JSON(404, ErrorResponse{Error: "Job not found"})
			return
		}

		status := job.GetStatus()
		var completedAt *string
		if status.CompletedAt != nil {
			str := status.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
			completedAt = &str
		}

		c.JSON(200, BatchJobResponse{
			ID:          status.ID,
			Status:      string(status.Status),
			TotalFiles:  status.TotalFiles,
			Completed:   status.Completed,
			Failed:      status.Failed,
			Progress:    status.Progress,
			Results:     status.Results,
			StartedAt:   status.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
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

		job, ok := deps.JobQueue.GetJob(jobID)
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

		// Get the batch job
		job, ok := deps.JobQueue.GetJob(jobID)
		if !ok {
			c.JSON(404, ErrorResponse{Error: "Job not found"})
			return
		}

		// Find the file result with this movie ID
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

		// Update the movie data in the file result
		foundResult.Data = req.Movie
		job.UpdateFileResult(foundFilePath, foundResult)

		// Also update in database if it exists
		if err := deps.MovieRepo.Upsert(req.Movie); err != nil {
			logging.Errorf("Failed to update movie in database: %v", err)
			// Don't fail the request if DB update fails
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

		job, ok := deps.JobQueue.GetJob(jobID)
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

		// Get the batch job
		job, ok := deps.JobQueue.GetJob(jobID)
		if !ok {
			c.JSON(404, ErrorResponse{Error: "Job not found"})
			return
		}

		// Find the movie in the job results
		status := job.GetStatus()
		var movie *models.Movie
		for _, result := range status.Results {
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
			for _, result := range status.Results {
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
