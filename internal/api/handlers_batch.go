package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	imageutil "github.com/javinizer/javinizer-go/internal/image"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
)

const (
	// minScanTimeout is the minimum timeout duration for directory scans.
	// This prevents immediate context cancellation when ScanTimeoutSeconds is 0 or negative.
	minScanTimeout = 5 * time.Second
)

// isDirAllowed checks if a directory is allowed based on API security settings.
// It enforces both denied (blocklist) and allowed (allowlist) directory rules.
// Also enforces the built-in denylist to match behavior of validateScanPath.
func isDirAllowed(dir string, allow, deny []string) bool {
	// Expand home directory first
	expandedDir := expandHomeDir(dir)
	d := filepath.Clean(expandedDir)

	// Resolve symlinks for the checked directory (security: prevent symlink traversal)
	resolved := d
	absPath, err := filepath.Abs(d)
	if err != nil {
		// Fail closed: deny access if absolute path resolution fails
		return false
	}

	// Try to resolve symlinks, but allow validation even if path doesn't exist yet
	// (e.g., during batch job setup before files are downloaded)
	if realPath, err := filepath.EvalSymlinks(absPath); err == nil {
		resolved = realPath
	} else if !os.IsNotExist(err) {
		// Fail closed: deny access if symlink resolution fails for reasons other than non-existence
		return false
	} else {
		// Path doesn't exist yet - use absolute path for validation
		resolved = absPath
	}

	// Get built-in denied directories (system directories that should never be accessed)
	builtInDenied := getDeniedDirectories()

	// Check built-in denylist first (with symlink resolution)
	for _, blocked := range builtInDenied {
		cleanBlocked := filepath.Clean(blocked)
		if absBlocked, err := filepath.Abs(cleanBlocked); err == nil {
			// Resolve symlink for blocked path if it exists
			realBlocked := absBlocked
			if resolvedBlocked, err := filepath.EvalSymlinks(absBlocked); err == nil {
				realBlocked = resolvedBlocked
			}
			if strings.HasPrefix(resolved, realBlocked+string(os.PathSeparator)) || resolved == realBlocked {
				return false
			}
		}
	}

	// Check config-provided denied directories (with home expansion and symlink resolution)
	for _, blocked := range deny {
		expandedBlocked := expandHomeDir(blocked)
		cleanBlocked := filepath.Clean(expandedBlocked)
		if absBlocked, err := filepath.Abs(cleanBlocked); err == nil {
			// Resolve symlink for blocked path if it exists
			realBlocked := absBlocked
			if resolvedBlocked, err := filepath.EvalSymlinks(absBlocked); err == nil {
				realBlocked = resolvedBlocked
			}
			if strings.HasPrefix(resolved, realBlocked+string(os.PathSeparator)) || resolved == realBlocked {
				return false
			}
		}
	}

	// If no allow list specified, deny by default (secure by default)
	// This matches the behavior of validateNFOPath and prevents unrestricted filesystem access
	if len(allow) == 0 {
		return false
	}

	// Check if directory is in allow list (with home expansion and symlink resolution)
	for _, allowed := range allow {
		expandedAllowed := expandHomeDir(allowed)
		cleanAllowed := filepath.Clean(expandedAllowed)
		if absAllowed, err := filepath.Abs(cleanAllowed); err == nil {
			// Resolve symlink for allowed path if it exists
			realAllowed := absAllowed
			if resolvedAllowed, err := filepath.EvalSymlinks(absAllowed); err == nil {
				realAllowed = resolvedAllowed
			}
			if strings.HasPrefix(resolved, realAllowed+string(os.PathSeparator)) || resolved == realAllowed {
				return true
			}
		}
	}

	return false
}

// discoverSiblingParts finds all multi-part files with the same base movie ID in the parent directories.
// It handles two scenarios:
// 1. User submits all parts → Groups them by movie ID and ensures completeness
// 2. User submits some parts → Auto-discovers missing siblings from disk
func discoverSiblingParts(files []string, fileMatcher *matcher.Matcher, cfg *config.Config) []string {
	if len(files) == 0 {
		return files
	}

	// First, match all submitted files to understand what we have
	scan := scanner.NewScanner(afero.NewOsFs(), &cfg.Matching)
	seenPaths := make(map[string]bool)
	fileInfos := make([]scanner.FileInfo, 0, len(files))

	for _, filePath := range files {
		seenPaths[filePath] = true
		fileInfos = append(fileInfos, scanner.FileInfo{
			Path:      filePath,
			Name:      filepath.Base(filePath),
			Extension: filepath.Ext(filePath),
			Dir:       filepath.Dir(filePath),
		})
	}

	// Match submitted files to detect multi-part status
	submittedMatches := fileMatcher.Match(fileInfos)

	// Validate letter-based multipart patterns using directory context
	// This enables correct multipart detection when user submits multiple files like ABW-121-A.mp4 + ABW-121-B.mp4
	submittedMatches = matcher.ValidateMultipartInDirectory(submittedMatches)

	// Group submitted files by movie ID and check if any are multi-part
	movieIDsToProcess := make(map[string]bool) // movie IDs that need sibling discovery
	directoriesScanned := make(map[string]bool)

	for _, match := range submittedMatches {
		// If ANY file for this movie ID is multi-part, we need to discover all siblings
		if match.IsMultiPart {
			movieIDsToProcess[match.ID] = true
			logging.Debugf("Detected multi-part file: %s (movie ID: %s, part: %d)",
				match.File.Name, match.ID, match.PartNumber)
		}
	}

	// If no multi-part files detected, return original list
	if len(movieIDsToProcess) == 0 {
		logging.Debugf("No multi-part files detected in submission, skipping auto-discovery")
		return files
	}

	// Scan parent directories to find all siblings for multi-part movies
	allFiles := make([]string, 0, len(files))
	allFiles = append(allFiles, files...) // Start with original files

	for _, match := range submittedMatches {
		if !movieIDsToProcess[match.ID] {
			continue // Not a multi-part movie
		}

		dir := match.File.Dir
		if directoriesScanned[dir] {
			continue // Already scanned this directory
		}
		directoriesScanned[dir] = true

		// Security: Check if directory is allowed before scanning
		if !isDirAllowed(dir, cfg.API.Security.AllowedDirectories, cfg.API.Security.DeniedDirectories) {
			logging.Debugf("Skipping auto-discovery in disallowed directory: %s", dir)
			continue
		}

		// Check if directory exists
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			logging.Debugf("Directory does not exist: %s", dir)
			continue
		}

		// Create context with timeout to prevent resource exhaustion on large directories
		// Use minimum timeout if config is 0 or negative
		timeout := time.Duration(cfg.API.Security.ScanTimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = minScanTimeout
		}
		scanCtx, cancelScan := context.WithTimeout(context.Background(), timeout)

		// Scan the directory with resource limits (non-recursive)
		result, err := scan.ScanWithLimits(scanCtx, dir, cfg.API.Security.MaxFilesPerScan)
		cancelScan() // Immediate cleanup, not deferred
		if err != nil {
			logging.Debugf("Failed to scan directory %s: %v", dir, err)
			continue
		}
		if result.TimedOut {
			effectiveSeconds := int(timeout / time.Second)
			logging.Warnf("Directory scan timed out for %s (limit: %d seconds)",
				dir, effectiveSeconds)
			// Continue with partial results
		}
		if result.LimitReached {
			logging.Warnf("Directory scan reached file limit for %s (limit: %d files)",
				dir, cfg.API.Security.MaxFilesPerScan)
			// Continue with partial results
		}

		// Match all files in the directory
		matchResults := fileMatcher.Match(result.Files)

		// Validate letter-based multipart patterns using directory context
		// This enables detection of letter-suffix siblings (e.g., ABW-121-A.mp4, ABW-121-B.mp4)
		matchResults = matcher.ValidateMultipartInDirectory(matchResults)

		// Find siblings for the multi-part movies we're processing
		// Note: All files in result.Files come from 'dir', which was already validated above.
		// No need to re-validate each file's parent directory (avoids symlink race condition).
		for _, dirMatch := range matchResults {
			if movieIDsToProcess[dirMatch.ID] && dirMatch.IsMultiPart {
				if !seenPaths[dirMatch.File.Path] {
					// Sanity check: Ensure file is actually in the scanned directory
					parent := filepath.Dir(dirMatch.File.Path)
					if filepath.Clean(parent) != filepath.Clean(dir) {
						logging.Warnf("Scanner returned file outside scanned directory: %s (expected: %s)", parent, dir)
						continue
					}

					seenPaths[dirMatch.File.Path] = true
					allFiles = append(allFiles, dirMatch.File.Path)
					logging.Infof("Auto-discovered multi-part sibling: %s (movie ID: %s, part: %d)",
						dirMatch.File.Name, dirMatch.ID, dirMatch.PartNumber)
				}
			}
		}
	}

	return allFiles
}

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
		allFiles := discoverSiblingParts(req.Files, deps.GetMatcher(), cfg)

		if len(allFiles) > len(req.Files) {
			logging.Infof("Auto-discovered %d sibling files for batch job (original: %d, total: %d)",
				len(allFiles)-len(req.Files), len(req.Files), len(allFiles))
		}

		// Create job with all files (original + discovered siblings)
		job := deps.JobQueue.CreateJob(allFiles)

		// Start processing in background - use getters for thread-safe access
		go processBatchJob(job, deps.GetRegistry(), deps.GetAggregator(), deps.MovieRepo, deps.GetMatcher(), req.Strict, req.Force, req.Update, req.Destination, deps.GetConfig(), req.SelectedScrapers, req.ScalarStrategy, req.ArrayStrategy, deps.DB)

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
				FilePath:    fileResult.FilePath,
				MovieID:     fileResult.MovieID,
				Status:      string(fileResult.Status),
				Error:       fileResult.Error,
				Data:        fileResult.Data,
				StartedAt:   fileResult.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
				EndedAt:     endedAt,
				IsMultiPart: fileResult.IsMultiPart,
				PartNumber:  fileResult.PartNumber,
				PartSuffix:  fileResult.PartSuffix,
			}
		}

		c.JSON(200, BatchJobResponse{
			ID:          job.ID,
			Status:      string(job.Status),
			TotalFiles:  job.TotalFiles,
			Completed:   job.Completed,
			Failed:      job.Failed,
			Excluded:    job.Excluded,
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

		// Cleanup temp posters for cancelled job (batch job is gone, temp posters no longer needed)
		go cleanupJobTempPosters(jobID)

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

		// Security: Validate destination directory against allow/deny lists
		cfg := deps.GetConfig()
		if !isDirAllowed(req.Destination, cfg.API.Security.AllowedDirectories, cfg.API.Security.DeniedDirectories) {
			c.JSON(403, ErrorResponse{Error: "Access denied to requested directory"})
			return
		}

		// Reuse the batch job lifecycle for organize progress polling.
		job.MarkStarted()

		// Start organization in background - use getter for thread-safe access
		go processOrganizeJob(job, deps.GetMatcher(), req.Destination, req.CopyOnly, deps.DB, cfg)

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

		// Security: Validate destination directory against allow/deny lists
		cfg := deps.GetConfig()
		if !isDirAllowed(req.Destination, cfg.API.Security.AllowedDirectories, cfg.API.Security.DeniedDirectories) {
			c.JSON(403, ErrorResponse{Error: "Access denied to requested directory"})
			return
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
		preview := generatePreview(movie, fileResults, req.Destination, deps.GetConfig())
		c.JSON(200, preview)
	}
}
