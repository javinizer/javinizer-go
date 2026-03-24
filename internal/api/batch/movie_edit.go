package batch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	imageutil "github.com/javinizer/javinizer-go/internal/image"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
)

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

		// Get a snapshot to search for files
		status := job.GetStatus()

		// Collect ALL file paths for this movie ID (handles multi-part files)
		var filePaths []string
		for filePath, result := range status.Results {
			if result.MovieID == movieID {
				filePaths = append(filePaths, filePath)
			}
		}

		// If not found by MovieID, try searching by the actual movie.ID (in case of content ID resolution)
		if len(filePaths) == 0 {
			for filePath, result := range status.Results {
				if result.Data != nil {
					if m, ok := result.Data.(*models.Movie); ok && m.ID == movieID {
						filePaths = append(filePaths, filePath)
					}
				}
			}
		}

		if len(filePaths) == 0 {
			c.JSON(404, ErrorResponse{Error: fmt.Sprintf("Movie %s not found in job", movieID)})
			return
		}

		// Update database first (before updating job state) to complete any mutations
		// before exposing the pointer to concurrent readers
		if err := deps.MovieRepo.Upsert(req.Movie); err != nil {
			logging.Errorf("Failed to update movie in database: %v", err)
			// Don't fail the request if DB update fails
		}

		// Update ALL file parts for this movie ID (handles multi-part files like CD1, CD2, etc.)
		for _, filePath := range filePaths {
			err := job.AtomicUpdateFileResult(filePath, func(current *worker.FileResult) (*worker.FileResult, error) {
				// Update the movie data
				current.Data = req.Movie
				// Always sync MovieID to keep job state consistent (handles both content ID resolution and user edits)
				current.MovieID = req.Movie.ID
				return current, nil
			})

			if err != nil {
				logging.Errorf("Failed to update file result for %s: %v", filePath, err)
				c.JSON(500, ErrorResponse{Error: fmt.Sprintf("Failed to update job state: %v", err)})
				return
			}
		}
		c.JSON(200, MovieResponse{Movie: req.Movie})
	}
}

// updateBatchMoviePosterCrop godoc
// @Summary Update manual poster crop in batch job
// @Description Re-crop a temp poster for the review page using fixed-size crop coordinates
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param movieId path string true "Movie ID"
// @Param request body PosterCropRequest true "Crop coordinates"
// @Success 200 {object} PosterCropResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/batch/{id}/movies/{movieId}/poster-crop [post]
func updateBatchMoviePosterCrop(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		movieID := c.Param("movieId")

		if movieID != filepath.Base(movieID) || movieID == "" || movieID == "." {
			c.JSON(404, ErrorResponse{Error: "Movie not found in job"})
			return
		}

		var req PosterCropRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}

		job, ok := deps.JobQueue.GetJobPointer(jobID)
		if !ok {
			c.JSON(404, ErrorResponse{Error: "Job not found"})
			return
		}

		status := job.GetStatus()

		// Collect all file paths for this movie ID (handles multipart files)
		var filePaths []string
		for filePath, result := range status.Results {
			if result.MovieID == movieID {
				filePaths = append(filePaths, filePath)
			}
		}

		// If not found by FileResult.MovieID, match by actual Movie.ID (content ID resolution case)
		if len(filePaths) == 0 {
			for filePath, result := range status.Results {
				if result.Data == nil {
					continue
				}
				if m, ok := result.Data.(*models.Movie); ok && m.ID == movieID {
					filePaths = append(filePaths, filePath)
				}
			}
		}

		if len(filePaths) == 0 {
			c.JSON(404, ErrorResponse{Error: fmt.Sprintf("Movie %s not found in job", movieID)})
			return
		}

		posterID := movieID
		if firstResult, exists := status.Results[filePaths[0]]; exists && firstResult != nil && firstResult.Data != nil {
			if m, ok := firstResult.Data.(*models.Movie); ok && m.ID != "" {
				posterID = m.ID
			}
		}

		if posterID != filepath.Base(posterID) || posterID == "" || posterID == "." {
			c.JSON(400, ErrorResponse{Error: "Invalid movie ID for poster crop"})
			return
		}

		tempPosterDir := filepath.Join("data", "temp", "posters", jobID)
		sourcePath := filepath.Join(tempPosterDir, fmt.Sprintf("%s-full.jpg", posterID))
		if _, err := os.Stat(sourcePath); err != nil {
			// Fallback for older jobs where full image was already cleaned up.
			sourcePath = filepath.Join(tempPosterDir, fmt.Sprintf("%s.jpg", posterID))
		}

		if _, err := os.Stat(sourcePath); err != nil {
			c.JSON(404, ErrorResponse{Error: "Source poster not found for manual crop"})
			return
		}

		croppedPath := filepath.Join(tempPosterDir, fmt.Sprintf("%s.jpg", posterID))

		// Defense in depth: ensure both paths are inside tempPosterDir.
		cleanTempDir := filepath.Clean(tempPosterDir) + string(os.PathSeparator)
		cleanSourcePath := filepath.Clean(sourcePath)
		cleanCroppedPath := filepath.Clean(croppedPath)
		if !strings.HasPrefix(cleanSourcePath, cleanTempDir) || !strings.HasPrefix(cleanCroppedPath, cleanTempDir) {
			c.JSON(400, ErrorResponse{Error: "Invalid poster crop path"})
			return
		}

		left := req.X
		top := req.Y
		right := req.X + req.Width
		bottom := req.Y + req.Height

		if err := imageutil.CropPosterWithBounds(afero.NewOsFs(), sourcePath, croppedPath, left, top, right, bottom); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}

		croppedURL := fmt.Sprintf("/api/v1/temp/posters/%s/%s.jpg", jobID, posterID)

		// Keep job state consistent so response payloads always point to the latest temp crop.
		for _, filePath := range filePaths {
			err := job.AtomicUpdateFileResult(filePath, func(current *worker.FileResult) (*worker.FileResult, error) {
				movie, ok := current.Data.(*models.Movie)
				if !ok || movie == nil {
					return current, nil
				}
				movie.CroppedPosterURL = croppedURL
				movie.ShouldCropPoster = false
				current.Data = movie
				current.MovieID = movie.ID
				return current, nil
			})
			if err != nil {
				logging.Errorf("Failed to update poster crop in job state for %s: %v", filePath, err)
				c.JSON(500, ErrorResponse{Error: fmt.Sprintf("Failed to update job state: %v", err)})
				return
			}
		}

		c.JSON(200, PosterCropResponse{CroppedPosterURL: croppedURL})
	}
}

// excludeBatchMovie godoc
// @Summary Exclude movie from batch organization
// @Description Mark a movie in a batch job as excluded from file organization
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Param movieId path string true "Movie ID"
// @Success 200 {object} map[string]string
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/batch/{id}/movies/{movieId}/exclude [post]
func excludeBatchMovie(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		movieID := c.Param("movieId")

		// Use GetJobPointer to get the real job (not a snapshot) for mutations
		job, ok := deps.JobQueue.GetJobPointer(jobID)
		if !ok {
			c.JSON(404, ErrorResponse{Error: "Job not found"})
			return
		}

		// Get a snapshot to search for the file(s)
		status := job.GetStatus()

		// Collect ALL file paths for this movie ID (handles multi-part files)
		var filePaths []string
		for filePath, result := range status.Results {
			if result.MovieID == movieID {
				filePaths = append(filePaths, filePath)
			}
		}

		// If not found by MovieID, try searching by the actual movie.ID
		if len(filePaths) == 0 {
			logging.Debugf("[ExcludeBatchMovie] No matches by FileResult.MovieID, trying Movie.ID")
			for filePath, result := range status.Results {
				if result.Data != nil {
					if m, ok := result.Data.(*models.Movie); ok {
						logging.Debugf("[ExcludeBatchMovie] File: %s, Movie.ID: %s", filePath, m.ID)
						if m.ID == movieID {
							filePaths = append(filePaths, filePath)
							logging.Debugf("[ExcludeBatchMovie] Matched by Movie.ID: %s", filePath)
						}
					}
				}
			}
		}

		if len(filePaths) == 0 {
			c.JSON(404, ErrorResponse{Error: fmt.Sprintf("Movie %s not found in job", movieID)})
			return
		}

		// Mark ALL parts as excluded (handles multi-part files like CD1, CD2, etc.)
		logging.Debugf("[ExcludeBatchMovie] Excluding %d file(s) for movieID=%s", len(filePaths), movieID)
		for _, filePath := range filePaths {
			job.ExcludeFile(filePath)
			logging.Debugf("[ExcludeBatchMovie] Excluded: %s", filePath)
		}

		logging.Infof("Movie %s (%d file(s)) excluded from batch job %s", movieID, len(filePaths), jobID)

		c.JSON(200, gin.H{"message": "Movie excluded from organization"})
	}
}
