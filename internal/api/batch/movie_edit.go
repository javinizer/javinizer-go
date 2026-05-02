package batch

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/downloader"
	httpclientiface "github.com/javinizer/javinizer-go/internal/httpclient"
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
		if _, err := deps.MovieRepo.Upsert(req.Movie); err != nil {
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

		cfg := deps.GetConfig()
		tempPosterDir := filepath.Join(cfg.System.TempDir, "posters", jobID)
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

		croppedURL := fmt.Sprintf("/api/v1/temp/posters/%s/%s.jpg?v=%d", jobID, posterID, time.Now().UnixMilli())

		// Keep job state consistent so response payloads always point to the latest temp crop.
		for _, filePath := range filePaths {
			err := job.AtomicUpdateFileResult(filePath, func(current *worker.FileResult) (*worker.FileResult, error) {
				movie, ok := current.Data.(*models.Movie)
				if !ok || movie == nil {
					return current, nil
				}
				if movie.OriginalPosterURL == "" {
					movie.OriginalPosterURL = movie.PosterURL
					movie.OriginalCroppedPosterURL = movie.CroppedPosterURL
					movie.OriginalShouldCropPoster = &movie.ShouldCropPoster
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

// updateBatchMoviePosterFromURL godoc
// @Summary Download poster from URL
// @Description Download a poster image from a URL and set it as the movie's poster in the batch job
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param movieId path string true "Movie ID"
// @Param request body PosterFromURLRequest true "Poster URL"
// @Success 200 {object} PosterFromURLResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/batch/{id}/movies/{movieId}/poster-from-url [post]
func updateBatchMoviePosterFromURL(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		movieID := c.Param("movieId")

		if movieID != filepath.Base(movieID) || movieID == "" || movieID == "." {
			c.JSON(404, ErrorResponse{Error: "Movie not found in job"})
			return
		}

		var req PosterFromURLRequest
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

		var filePaths []string
		for filePath, result := range status.Results {
			if result.MovieID == movieID {
				filePaths = append(filePaths, filePath)
			}
		}
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
			c.JSON(400, ErrorResponse{Error: "Invalid movie ID for poster from URL"})
			return
		}

		cfg := deps.GetConfig()
		registry := deps.GetRegistry()

		httpClient, err := downloader.NewHTTPClientForDownloaderWithRegistry(cfg, registry)
		if err != nil {
			logging.Warnf("Failed to create HTTP client for poster download: %v", err)
			c.JSON(500, ErrorResponse{Error: "Failed to create HTTP client"})
			return
		}

		tempPosterDir := filepath.Join(cfg.System.TempDir, "posters", jobID)
		if err := os.MkdirAll(tempPosterDir, config.DirPermTemp); err != nil {
			c.JSON(500, ErrorResponse{Error: "Failed to create temp poster directory"})
			return
		}

		tempFullPath := filepath.Join(tempPosterDir, fmt.Sprintf("%s-full.jpg", posterID))
		tempCroppedPath := filepath.Join(tempPosterDir, fmt.Sprintf("%s.jpg", posterID))

		cleanTempDir := filepath.Clean(tempPosterDir) + string(os.PathSeparator)
		cleanFullPath := filepath.Clean(tempFullPath)
		cleanCroppedPath := filepath.Clean(tempCroppedPath)
		if !strings.HasPrefix(cleanFullPath, cleanTempDir) || !strings.HasPrefix(cleanCroppedPath, cleanTempDir) {
			c.JSON(400, ErrorResponse{Error: "Invalid poster path"})
			return
		}

		downloadReq, err := http.NewRequestWithContext(c.Request.Context(), "GET", req.URL, nil)
		if err != nil {
			c.JSON(400, ErrorResponse{Error: fmt.Sprintf("Invalid URL: %v", err)})
			return
		}
		if cfg.Scrapers.UserAgent != "" {
			downloadReq.Header.Set("User-Agent", cfg.Scrapers.UserAgent)
		}
		downloadReq.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
		if cfg.Scrapers.Referer != "" {
			downloadReq.Header.Set("Referer", cfg.Scrapers.Referer)
		} else if parsed, parseErr := url.Parse(req.URL); parseErr == nil && parsed.Host != "" {
			downloadReq.Header.Set("Referer", parsed.Scheme+"://"+parsed.Host+"/")
		}

		resp, err := httpClient.Do(downloadReq)
		if err != nil {
			c.JSON(502, ErrorResponse{Error: fmt.Sprintf("Failed to download image: %v", err)})
			return
		}
		defer func() { _ = httpclientiface.DrainAndClose(resp.Body) }()

		if resp.StatusCode != http.StatusOK {
			c.JSON(502, ErrorResponse{Error: fmt.Sprintf("Image download failed with status %d", resp.StatusCode)})
			return
		}

		tmpDownload, err := os.CreateTemp(tempPosterDir, posterID+"-full-*.tmp")
		if err != nil {
			c.JSON(500, ErrorResponse{Error: "Failed to create temp file"})
			return
		}
		tempDownloadPath := tmpDownload.Name()

		_, err = io.Copy(tmpDownload, resp.Body)
		_ = tmpDownload.Close()
		if err != nil {
			_ = os.Remove(tempDownloadPath)
			c.JSON(500, ErrorResponse{Error: "Failed to write image"})
			return
		}

		_ = os.Remove(tempFullPath)
		if err := os.Rename(tempDownloadPath, tempFullPath); err != nil {
			_ = os.Remove(tempDownloadPath)
			c.JSON(500, ErrorResponse{Error: "Failed to finalize image download"})
			return
		}

		if err := imageutil.CropPosterFromCover(afero.NewOsFs(), tempFullPath, tempCroppedPath); err != nil {
			logging.Warnf("Failed to auto-crop poster from URL for %s: %v (using full image as fallback)", posterID, err)
			_ = os.Remove(tempCroppedPath)
			if copyErr := copyFile(tempFullPath, tempCroppedPath); copyErr != nil {
				_ = os.Remove(tempFullPath)
				c.JSON(500, ErrorResponse{Error: "Failed to create poster image"})
				return
			}
		}

		croppedURL := fmt.Sprintf("/api/v1/temp/posters/%s/%s.jpg?v=%d", jobID, posterID, time.Now().UnixMilli())

		for _, filePath := range filePaths {
			err := job.AtomicUpdateFileResult(filePath, func(current *worker.FileResult) (*worker.FileResult, error) {
				movie, ok := current.Data.(*models.Movie)
				if !ok || movie == nil {
					return current, nil
				}
				if movie.OriginalPosterURL == "" {
					movie.OriginalPosterURL = movie.PosterURL
					movie.OriginalCroppedPosterURL = movie.CroppedPosterURL
					movie.OriginalShouldCropPoster = &movie.ShouldCropPoster
				}
				movie.PosterURL = req.URL
				movie.CroppedPosterURL = croppedURL
				movie.ShouldCropPoster = false
				current.Data = movie
				current.MovieID = movie.ID
				return current, nil
			})
			if err != nil {
				logging.Errorf("Failed to update poster from URL in job state for %s: %v", filePath, err)
				c.JSON(500, ErrorResponse{Error: fmt.Sprintf("Failed to update job state: %v", err)})
				return
			}
		}

		if _, err := deps.MovieRepo.Upsert(&models.Movie{
			ID:               posterID,
			PosterURL:        req.URL,
			CroppedPosterURL: croppedURL,
		}); err != nil {
			logging.Warnf("Failed to update movie poster in database: %v", err)
		}

		c.JSON(200, PosterFromURLResponse{
			CroppedPosterURL: croppedURL,
			PosterURL:        req.URL,
		})
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}
	return nil
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

		if job.AllFilesExcluded() {
			job.MarkCancelled()
			deps.JobQueue.PersistJob(job)
			logging.Infof("All files excluded from batch job %s, marked as cancelled", jobID)
		}

		logging.Infof("Movie %s (%d file(s)) excluded from batch job %s", movieID, len(filePaths), jobID)

		c.JSON(200, gin.H{"message": "Movie excluded from organization"})
	}
}

const bulkExcludeMaxMovies = 100

// batchExcludeMovies godoc
// @Summary Bulk exclude movies from batch organization
// @Description Exclude multiple movies from a batch job in a single request. Best-effort: excludes as many as possible and returns per-movie failures.
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param request body BatchExcludeRequest true "Movie IDs to exclude"
// @Success 200 {object} BatchExcludeResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/batch/{id}/movies/batch-exclude [post]
func batchExcludeMovies(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")

		var req BatchExcludeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: err.Error()})
			return
		}

		if len(req.MovieIDs) == 0 {
			c.JSON(400, ErrorResponse{Error: "movie_ids is required and must not be empty"})
			return
		}

		if len(req.MovieIDs) > bulkExcludeMaxMovies {
			c.JSON(400, ErrorResponse{Error: fmt.Sprintf("movie_ids must not exceed %d items", bulkExcludeMaxMovies)})
			return
		}

		job, ok := deps.JobQueue.GetJobPointer(jobID)
		if !ok {
			c.JSON(404, ErrorResponse{Error: "Job not found"})
			return
		}

		status := job.GetStatus()

		var excluded []string
		var failed []BatchExcludeFailed

		for _, movieID := range req.MovieIDs {
			var filePaths []string
			for filePath, result := range status.Results {
				if result.MovieID == movieID {
					filePaths = append(filePaths, filePath)
				}
			}

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
				failed = append(failed, BatchExcludeFailed{
					MovieID: movieID,
					Error:   fmt.Sprintf("Movie %s not found in job", movieID),
				})
				continue
			}

			for _, filePath := range filePaths {
				job.ExcludeFile(filePath)
			}
			excluded = append(excluded, movieID)
		}

		if job.AllFilesExcluded() {
			job.MarkCancelled()
			deps.JobQueue.PersistJob(job)
			logging.Infof("All files excluded from batch job %s, marked as cancelled", jobID)
		}

		logging.Infof("Batch exclude: %d movie(s) excluded, %d failed from batch job %s", len(excluded), len(failed), jobID)

		updatedStatus := job.GetStatus()
		jobResponse := buildBatchJobResponse(updatedStatus)

		if excluded == nil {
			excluded = []string{}
		}
		if failed == nil {
			failed = []BatchExcludeFailed{}
		}

		c.JSON(200, BatchExcludeResponse{
			Excluded: excluded,
			Failed:   failed,
			Job:      jobResponse,
		})
	}
}

func buildBatchJobResponse(job *worker.BatchJob) *BatchJobResponse {
	var completedAt *string
	if job.CompletedAt != nil {
		str := job.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		completedAt = &str
	}

	results := make(map[string]*BatchFileResult, len(job.Results))
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

	return &BatchJobResponse{
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
		Update:                job.GetUpdate(),
		PersistError:          job.PersistError,
	}
}
