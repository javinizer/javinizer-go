package api

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/template"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// processBatchJob processes a batch scraping job (metadata only, no file organization)
// using concurrent worker pool for improved performance.
// If updateMode is true, will also download media files and generate NFOs in place without moving files.
func processBatchJob(job *worker.BatchJob, registry *models.ScraperRegistry, agg *aggregator.Aggregator, movieRepo *database.MovieRepository, mat *matcher.Matcher, strict, force, updateMode bool, destination string, cfg *config.Config, selectedScrapers []string, db *database.DB) {
	// Setup context for cancellation
	ctx, cancel := context.WithCancel(context.Background())
	job.SetCancelFunc(cancel)
	defer cancel()

	job.MarkStarted()

	// Log which scrapers will be used
	if len(selectedScrapers) > 0 {
		logging.Infof("Batch job using custom scrapers: %v", selectedScrapers)
	} else {
		logging.Infof("Batch job using default scrapers from config: %v", cfg.Scrapers.Priority)
	}

	// Create progress adapter for WebSocket broadcasting
	adapter := NewProgressAdapter(job.ID, job, nil) // nil = use global wsHub

	// Create progress tracker that feeds the adapter
	progressTracker := worker.NewProgressTracker(adapter.GetChannel())

	// Start adapter in background
	adapter.Start()
	defer adapter.Stop()

	// Get max workers from config
	maxWorkers := cfg.Performance.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 5 // default
	}

	// Get timeout from config (worker_timeout is in seconds)
	timeout := time.Duration(cfg.Performance.WorkerTimeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute // default
	}

	// Create worker pool with job context (enables cancellation)
	pool := worker.NewPoolWithContext(ctx, maxWorkers, timeout, progressTracker)
	defer pool.Stop()

	// Submit tasks to pool
	for i, filePath := range job.Files {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			job.MarkCancelled()
			return
		default:
		}

		// Create unique task ID
		taskID := fmt.Sprintf("batch-scrape-%s-%d", job.ID, i)

		// Register with adapter for WebSocket mapping
		adapter.RegisterTask(taskID, i, filePath)

		// Create batch scrape task
		task := worker.NewBatchScrapeTask(
			taskID,
			filePath,
			i,
			job,
			registry,
			agg,
			movieRepo,
			mat,
			progressTracker,
			force,
			selectedScrapers,       // Pass as-is (empty = use defaults, non-empty = custom)
			nil,                    // httpClient - will be created in the task
			cfg.Scrapers.UserAgent, // userAgent
			cfg.Scrapers.Referer,   // referer
		)

		// Submit to pool (blocks if pool is full)
		if err := pool.Submit(task); err != nil {
			logging.Errorf("Failed to submit task for %s: %v", filePath, err)
			// Update job with failure
			result := &worker.FileResult{
				FilePath:  filePath,
				Status:    worker.JobStatusFailed,
				Error:     fmt.Sprintf("Failed to submit task: %v", err),
				StartedAt: time.Now(),
			}
			now := time.Now()
			result.EndedAt = &now
			job.UpdateFileResult(filePath, result)
		}
	}

	// Wait for all tasks to complete
	pool.Wait()

	// Mark job as completed (don't auto-process update mode - wait for user to review and click "Update")
	job.MarkCompleted()

	// Broadcast final completion
	wsHub.BroadcastProgress(&ws.ProgressMessage{
		JobID:    job.ID,
		Status:   "completed",
		Progress: 100,
		Message:  fmt.Sprintf("Completed %d of %d files", job.Completed, job.TotalFiles),
	})
}

// copyTempCroppedPoster copies the temp cropped poster to the destination directory
// Returns true if copy was successful, false otherwise
func copyTempCroppedPoster(job *worker.BatchJob, movie *models.Movie, destDir string, cfg *config.Config, mode string) bool {
	tempPosterPath := filepath.Join("data", "temp", "posters", job.ID, movie.ID+".jpg")
	if _, err := os.Stat(tempPosterPath); err != nil {
		// Temp poster doesn't exist - not an error, just skip
		return false
	}

	// Generate filename using template engine (matching downloader behavior)
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = cfg.Output.GroupActress
	engine := template.NewEngine()
	posterFilename, err := engine.Execute(cfg.Output.PosterFormat, ctx)
	if err != nil {
		// Fallback to hardcoded format if template fails
		posterFilename = fmt.Sprintf("%s-poster.jpg", movie.ID)
		logging.Warnf("%s mode: Template execution failed, using fallback filename: %v", mode, err)
	}
	destPosterPath := filepath.Join(destDir, posterFilename)

	// Copy temp poster to destination
	if err := copyFile(tempPosterPath, destPosterPath); err != nil {
		logging.Warnf("%s mode: Failed to copy temp poster: %v", mode, err)
		return false
	}

	logging.Infof("%s mode: Copied cropped poster from temp to %s", mode, destPosterPath)
	return true
}

// downloadMediaFiles downloads all configured media files for a movie
// destDir is where files should be downloaded to
func downloadMediaFiles(dl *downloader.Downloader, movie *models.Movie, destDir string, cfg *config.Config) {
	// Download poster (may be skipped if temp poster was already copied)
	if cfg.Output.DownloadPoster {
		if _, err := dl.DownloadPoster(movie, destDir); err != nil {
			logging.Errorf("Failed to download poster for %s: %v", movie.ID, err)
		}
	}

	// Download cover
	if cfg.Output.DownloadCover {
		if _, err := dl.DownloadCover(movie, destDir); err != nil {
			logging.Errorf("Failed to download cover for %s: %v", movie.ID, err)
		}
	}

	// Download screenshots
	if cfg.Output.DownloadExtrafanart {
		if _, err := dl.DownloadExtrafanart(movie, destDir); err != nil {
			logging.Errorf("Failed to download screenshots for %s: %v", movie.ID, err)
		}
	}

	// Download trailer
	if cfg.Output.DownloadTrailer {
		if _, err := dl.DownloadTrailer(movie, destDir); err != nil {
			logging.Errorf("Failed to download trailer for %s: %v", movie.ID, err)
		}
	}

	// Download actress images
	if cfg.Output.DownloadActress {
		if _, err := dl.DownloadActressImages(movie, destDir); err != nil {
			logging.Errorf("Failed to download actress images for %s: %v", movie.ID, err)
		}
	}
}

// processUpdateJob handles update operation triggered from review page
// Generates NFOs and downloads media files in place without moving video files
func processUpdateJob(job *worker.BatchJob, cfg *config.Config, db *database.DB) {
	// Setup context for cancellation (mirrors processBatchJob pattern)
	ctx, cancel := context.WithCancel(context.Background())
	job.SetCancelFunc(cancel)
	defer cancel()

	processUpdateMode(job, cfg, db, ctx)
}

// processUpdateMode handles update mode: generate NFOs and download media files in place (no file organization)
func processUpdateMode(job *worker.BatchJob, cfg *config.Config, db *database.DB, ctx context.Context) {
	// Initialize components
	nfoGen := nfo.NewGenerator(nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, db))
	dl := downloader.NewDownloaderWithNFOConfig(&cfg.Output, cfg.Scrapers.UserAgent, cfg.Metadata.NFO.ActressLanguageJA, cfg.Metadata.NFO.FirstNameOrder)

	// Broadcast update started
	wsHub.BroadcastProgress(&ws.ProgressMessage{
		JobID:    job.ID,
		Status:   "updating",
		Progress: 0,
		Message:  "Generating NFOs and downloading media files in place",
	})

	status := job.GetStatus()
	totalFiles := 0
	for _, fileResult := range status.Results {
		if fileResult.Status == worker.JobStatusCompleted && fileResult.Data != nil {
			totalFiles++
		}
	}

	// Guard against division by zero when no files were successfully scraped
	if totalFiles == 0 {
		wsHub.BroadcastProgress(&ws.ProgressMessage{
			JobID:    job.ID,
			Status:   "update_completed",
			Progress: 100,
			Message:  "Update completed: no files to process (all files failed during scraping)",
		})
		return
	}

	processedFiles := 0

	for filePath, fileResult := range status.Results {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			job.MarkCancelled()
			wsHub.BroadcastProgress(&ws.ProgressMessage{
				JobID:    job.ID,
				Status:   "cancelled",
				Progress: float64(processedFiles) / float64(totalFiles) * 100,
				Message:  fmt.Sprintf("Update cancelled (%d/%d files processed)", processedFiles, totalFiles),
			})
			return
		default:
		}

		// Skip files that failed during scraping
		if fileResult.Status != worker.JobStatusCompleted || fileResult.Data == nil {
			continue
		}

		movie, ok := fileResult.Data.(*models.Movie)
		if !ok {
			logging.Errorf("Invalid movie data type for file: %s", filePath)
			continue
		}

		// Get source directory (where file currently is)
		sourceDir := filepath.Dir(filePath)

		// Track whether this file had any errors
		hasErrors := false
		errorMsg := ""

		// Copy temp cropped poster BEFORE downloads (so downloader skips it)
		copyTempCroppedPoster(job, movie, sourceDir, cfg, "Update")

		// Generate NFO in source directory
		if err := nfoGen.Generate(movie, sourceDir, "", filePath); err != nil {
			logging.Warnf("Failed to generate NFO for %s: %v", movie.ID, err)
			hasErrors = true
			errorMsg = fmt.Sprintf("NFO generation failed: %v", err)
		} else {
			logging.Infof("Generated NFO in: %s", sourceDir)
		}

		// Download all media files to source directory
		results, err := dl.DownloadAll(movie, sourceDir, 0)
		if err != nil {
			logging.Warnf("Failed to download media for %s: %v", movie.ID, err)
			hasErrors = true
			if errorMsg != "" {
				errorMsg += "; Media download failed: " + err.Error()
			} else {
				errorMsg = fmt.Sprintf("Media download failed: %v", err)
			}
		} else {
			for _, result := range results {
				if result.Downloaded {
					logging.Infof("Downloaded %s: %s (%d bytes)", result.Type, result.LocalPath, result.Size)
				}
			}
		}

		processedFiles++
		progress := float64(processedFiles) / float64(totalFiles) * 100

		// Broadcast progress with error status if errors occurred
		if hasErrors {
			wsHub.BroadcastProgress(&ws.ProgressMessage{
				JobID:    job.ID,
				FilePath: filePath,
				Status:   "failed",
				Progress: progress,
				Message:  fmt.Sprintf("Partial failure for %s (%d/%d)", movie.ID, processedFiles, totalFiles),
				Error:    errorMsg,
			})
		} else {
			wsHub.BroadcastProgress(&ws.ProgressMessage{
				JobID:    job.ID,
				FilePath: filePath,
				Status:   "updated",
				Progress: progress,
				Message:  fmt.Sprintf("Updated %s (%d/%d)", movie.ID, processedFiles, totalFiles),
			})
		}
	}

	// Broadcast completion
	wsHub.BroadcastProgress(&ws.ProgressMessage{
		JobID:    job.ID,
		Status:   "update_completed",
		Progress: 100,
		Message:  fmt.Sprintf("Update completed: %d file(s) processed", processedFiles),
	})
}

// processOrganizeJob processes file organization for a completed scrape job
func processOrganizeJob(job *worker.BatchJob, mat *matcher.Matcher, destination string, copyOnly bool, db *database.DB, cfg *config.Config) {
	// Initialize organizer, downloader, and NFO generator
	org := organizer.NewOrganizer(&cfg.Output)
	dl := downloader.NewDownloaderWithNFOConfig(&cfg.Output, "Javinizer/1.0", cfg.Metadata.NFO.ActressLanguageJA, cfg.Metadata.NFO.FirstNameOrder)
	nfoGen := nfo.NewGenerator(nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, db))

	// Broadcast organization started
	wsHub.BroadcastProgress(&ws.ProgressMessage{
		JobID:    job.ID,
		Status:   "organizing",
		Progress: 0,
		Message:  "Starting file organization",
	})

	status := job.GetStatus()
	organized := 0
	failed := 0

	for filePath, fileResult := range status.Results {
		// Skip files that failed during scraping
		if fileResult.Status != worker.JobStatusCompleted || fileResult.Data == nil {
			continue
		}

		movie, ok := fileResult.Data.(*models.Movie)
		if !ok {
			logging.Errorf("Invalid movie data type for file: %s", filePath)
			failed++
			continue
		}

		// Create match result for organizer
		fileInfo := scanner.FileInfo{
			Path:      filePath,
			Name:      filepath.Base(filePath),
			Extension: filepath.Ext(filePath),
			Dir:       filepath.Dir(filePath),
		}
		matchResults := mat.Match([]scanner.FileInfo{fileInfo})
		if len(matchResults) == 0 {
			logging.Errorf("Could not match file: %s", filePath)
			failed++
			continue
		}

		match := matchResults[0]

		// Organize file
		result, err := org.Organize(match, movie, destination, false, false, copyOnly)
		if err != nil {
			logging.Errorf("Failed to organize %s: %v", filePath, err)
			failed++

			wsHub.BroadcastProgress(&ws.ProgressMessage{
				JobID:    job.ID,
				FilePath: filePath,
				Status:   "failed",
				Progress: float64(organized+failed) / float64(len(status.Results)) * 100,
				Error:    err.Error(),
			})
			continue
		}

		// Copy temp cropped poster and download all media files
		if result.Moved {
			// Copy temp cropped poster BEFORE downloads (so downloader skips it)
			copyTempCroppedPoster(job, movie, result.FolderPath, cfg, "Organize")

			// Download all media files
			downloadMediaFiles(dl, movie, result.FolderPath, cfg)
		}

		// Generate NFO file
		if result.Moved {
			// Determine part suffix for multi-part files (only if per_file is enabled)
			partSuffix := ""
			if cfg.Metadata.NFO.PerFile && match.IsMultiPart {
				partSuffix = match.PartSuffix
			}

			// Pass the video file path for stream details extraction
			// Use NewPath (destination) after move/copy, fall back to OriginalPath
			videoFilePath := result.NewPath
			if videoFilePath == "" {
				videoFilePath = result.OriginalPath
			}
			if err := nfoGen.Generate(movie, result.FolderPath, partSuffix, videoFilePath); err != nil {
				logging.Errorf("Failed to generate NFO for %s: %v", movie.ID, err)
			}
		}

		organized++

		wsHub.BroadcastProgress(&ws.ProgressMessage{
			JobID:    job.ID,
			FilePath: filePath,
			Status:   "organized",
			Progress: float64(organized+failed) / float64(len(status.Results)) * 100,
			Message:  fmt.Sprintf("Organized %s", movie.ID),
		})
	}

	// Broadcast final completion
	wsHub.BroadcastProgress(&ws.ProgressMessage{
		JobID:    job.ID,
		Status:   "organization_completed",
		Progress: 100,
		Message:  fmt.Sprintf("Organized %d files, %d failed", organized, failed),
	})
}

// generatePreview generates an organize preview response for a movie
func generatePreview(movie *models.Movie, destination string, cfg *config.Config) OrganizePreviewResponse {
	// Create template context from movie
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = cfg.Output.GroupActress
	templateEngine := template.NewEngine()

	// Generate subfolder hierarchy (if configured)
	subfolderParts := make([]string, 0, len(cfg.Output.SubfolderFormat))
	for _, subfolderTemplate := range cfg.Output.SubfolderFormat {
		subfolderName, err := templateEngine.Execute(subfolderTemplate, ctx)
		if err != nil {
			logging.Errorf("Failed to generate subfolder from template '%s': %v", subfolderTemplate, err)
			continue
		}
		// Sanitize and add to parts if not empty
		subfolderName = template.SanitizeFolderPath(subfolderName)
		if subfolderName != "" {
			subfolderParts = append(subfolderParts, subfolderName)
		}
	}

	// Generate folder name
	folderName, err := templateEngine.Execute(cfg.Output.FolderFormat, ctx)
	if err != nil {
		logging.Errorf("Failed to generate folder name: %v", err)
		folderName = "error"
	}
	folderName = template.SanitizeFolderPath(folderName)

	// Generate file name
	fileName, err := templateEngine.Execute(cfg.Output.FileFormat, ctx)
	if err != nil {
		logging.Errorf("Failed to generate file name: %v", err)
		fileName = "error"
	}
	fileName = template.SanitizeFilename(fileName)

	// Build target paths with subfolder hierarchy
	// Start with destination, add subfolder parts, then final folder name
	pathParts := []string{destination}
	pathParts = append(pathParts, subfolderParts...)
	pathParts = append(pathParts, folderName)
	folderPath := filepath.Join(pathParts...)
	videoPath := filepath.Join(folderPath, fileName+".mp4") // Using .mp4 as placeholder
	nfoPath := filepath.Join(folderPath, fileName+".nfo")
	posterPath := filepath.Join(folderPath, fileName+"-poster.jpg")
	fanartPath := filepath.Join(folderPath, fileName+"-fanart.jpg")
	extrafanartPath := filepath.Join(folderPath, "extrafanart")

	// Generate screenshot names
	screenshots := []string{}
	if movie.Screenshots != nil && len(movie.Screenshots) > 0 {
		for i := range movie.Screenshots {
			screenshots = append(screenshots, fmt.Sprintf("fanart%d.jpg", i+1))
		}
	}

	return OrganizePreviewResponse{
		FolderName:      folderName,
		FileName:        fileName,
		FullPath:        videoPath,
		NFOPath:         nfoPath,
		PosterPath:      posterPath,
		FanartPath:      fanartPath,
		ExtrafanartPath: extrafanartPath,
		Screenshots:     screenshots,
	}
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}
