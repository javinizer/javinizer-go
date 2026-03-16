package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/template"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
)

func broadcastProgress(msg *ws.ProgressMessage) {
	if err := wsHub.BroadcastProgress(msg); err != nil {
		logging.Warnf("Failed to broadcast progress update for job %s: %v", msg.JobID, err)
	}
}

// processBatchJob processes a batch scraping job (metadata only, no file organization)
// using concurrent worker pool for improved performance.
// If updateMode is true, will also download media files and generate NFOs in place without moving files.
// scalarStrategy determines how to merge scalar fields (prefer-scraper, prefer-nfo)
// arrayStrategy determines how to merge array fields (merge, replace)
func processBatchJob(job *worker.BatchJob, registry *models.ScraperRegistry, agg *aggregator.Aggregator, movieRepo *database.MovieRepository, mat *matcher.Matcher, strict, force, updateMode bool, destination string, cfg *config.Config, selectedScrapers []string, scalarStrategy string, arrayStrategy string, db *database.DB) {
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

	// Create HTTP client for temp poster downloads with scraper-level download proxy support.
	httpClient, err := downloader.NewHTTPClientForDownloaderWithRegistry(cfg, registry)
	if err != nil {
		logging.Warnf("Failed to create HTTP client for poster downloads: %v (will skip poster generation)", err)
		httpClient = nil // Continue without poster generation
	}

	// Create a map to track which movie IDs have had posters generated
	// This prevents redundant poster downloads/crops for multi-part files
	// Thread safety is handled by processedMovieIDsMutex in single_scrape.go
	processedMovieIDs := make(map[string]bool)

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

		// Determine scraper priority contract:
		// - nil = use registry defaults (enables DB persistence, standard batch mode)
		// - non-nil = custom scraper mode (temporary aggregation, no DB persistence)
		// Pass nil when no custom scrapers specified to maintain proper persistence semantics
		scrapersToUse := selectedScrapers
		if len(selectedScrapers) == 0 {
			scrapersToUse = nil // Use registry defaults, not config list
		}

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
			updateMode,             // updateMode - if true, merge with existing NFO
			scrapersToUse,          // nil = registry defaults (DB persist), non-nil = custom mode
			httpClient,             // httpClient - configured with proxy support
			cfg.Scrapers.UserAgent, // userAgent
			cfg.Scrapers.Referer,   // referer
			processedMovieIDs,      // poster deduplication map (shared across all tasks)
			cfg,                    // cfg - needed for NFO path construction in update mode
			scalarStrategy,         // scalarStrategy - scalar field merge behavior (prefer-scraper, prefer-nfo)
			arrayStrategy,          // arrayStrategy - array field merge behavior (merge, replace)
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
	if err := pool.Wait(); err != nil {
		logging.Warnf("Worker pool completed with task failures: %v", err)
	}

	// Mark job as completed (don't auto-process update mode - wait for user to review and click "Update")
	job.MarkCompleted()

	// Log history for all scrape operations
	historyLogger := history.NewLogger(db)
	status := job.GetStatus()
	for filePath, fileResult := range status.Results {
		if fileResult == nil {
			continue
		}
		var scrapeErr error
		if fileResult.Status == worker.JobStatusFailed && fileResult.Error != "" {
			scrapeErr = fmt.Errorf("%s", fileResult.Error)
		}
		movieID := fileResult.MovieID
		if movieID == "" {
			movieID = filepath.Base(filePath)
		}
		if err := historyLogger.LogScrape(movieID, filePath, nil, scrapeErr); err != nil {
			logging.Warnf("Failed to log history for %s: %v", filePath, err)
		}
	}

	// NOTE: We do NOT cleanup temp posters here!
	// Users need them to view the review page after job completion.
	// Temp posters are cleaned up:
	//   1. After organize (when copied to final location)
	//   2. After job cancellation
	//   3. On server restart (for orphaned posters)

	// Broadcast final completion
	broadcastProgress(&ws.ProgressMessage{
		JobID:    job.ID,
		Status:   "completed",
		Progress: 100,
		Message:  fmt.Sprintf("Completed %d of %d files", job.Completed, job.TotalFiles),
	})
}

// cleanupJobTempPosters removes temp posters for a completed or cancelled job
// Best-effort, non-blocking cleanup. Called in a goroutine.
func cleanupJobTempPosters(jobID string) {
	tempDir := filepath.Join("data", "temp", "posters", jobID)
	if err := os.RemoveAll(tempDir); err != nil {
		logging.Debugf("[Job %s] Failed to clean temp poster dir: %v", jobID, err)
	} else {
		logging.Debugf("[Job %s] Cleaned temp poster directory", jobID)
	}
}

// copyTempCroppedPoster copies the temp cropped poster to the destination directory
// Returns true if copy was successful, false otherwise
// multipart can be nil for single-file operations
func copyTempCroppedPoster(job *worker.BatchJob, movie *models.Movie, destDir string, cfg *config.Config, mode string, multipart *downloader.MultipartInfo) bool {
	tempPosterPath := filepath.Join("data", "temp", "posters", job.ID, movie.ID+".jpg")
	if _, err := os.Stat(tempPosterPath); err != nil {
		// Temp poster doesn't exist - not an error, just skip
		return false
	}

	// Generate filename using template engine (matching downloader behavior)
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = cfg.Output.GroupActress
	// Set multipart context for template conditionals like <IF:MULTIPART>
	if multipart != nil {
		ctx.IsMultiPart = multipart.IsMultiPart
		ctx.PartNumber = multipart.PartNumber
		ctx.PartSuffix = multipart.PartSuffix
	}
	engine := template.NewEngine()
	posterFilename, err := engine.Execute(cfg.Output.PosterFormat, ctx)
	if err != nil {
		// Fallback to hardcoded format if template fails
		posterFilename = fmt.Sprintf("%s-poster.jpg", movie.ID)
		logging.Warnf("%s mode: Template execution failed, using fallback filename: %v", mode, err)
	}

	// Security: Sanitize poster filename to prevent path traversal
	posterFilename = template.SanitizeFilename(posterFilename)
	if posterFilename == "" {
		posterFilename = fmt.Sprintf("%s-poster.jpg", template.SanitizeFilename(movie.ID))
	}

	destPosterPath := filepath.Join(destDir, posterFilename)

	// Copy temp poster to destination
	if err := fsutil.CopyFileAtomic(tempPosterPath, destPosterPath); err != nil {
		logging.Warnf("%s mode: Failed to copy temp poster: %v", mode, err)
		return false
	}

	logging.Infof("%s mode: Copied cropped poster from temp to %s", mode, destPosterPath)
	return true
}

// downloadMediaFilesWithHistory downloads all configured media files and logs to history
// multipart can be nil for single-file operations
func downloadMediaFilesWithHistory(dl *downloader.Downloader, movie *models.Movie, destDir string, cfg *config.Config, historyLogger *history.Logger, multipart *downloader.MultipartInfo) {
	// Use DownloadAll to get results for history logging
	results, err := dl.DownloadAll(movie, destDir, multipart)
	if err != nil {
		logging.Errorf("Failed to download media for %s: %v", movie.ID, err)
		return
	}

	for _, result := range results {
		if result.Downloaded {
			logging.Infof("Downloaded %s: %s (%d bytes)", result.Type, result.LocalPath, result.Size)
		}
		// Log download to history (both successful and failed, skip if no URL)
		if result.URL != "" {
			if logErr := historyLogger.LogDownload(movie.ID, result.URL, result.LocalPath, string(result.Type), result.Error); logErr != nil {
				logging.Warnf("Failed to log download history for %s: %v", movie.ID, logErr)
			}
		}
	}
}

// processUpdateJob handles update operation triggered from review page
// Generates NFOs and downloads media files in place without moving video files
func processUpdateJob(job *worker.BatchJob, cfg *config.Config, db *database.DB, registry *models.ScraperRegistry) {
	// Setup context for cancellation (mirrors processBatchJob pattern)
	ctx, cancel := context.WithCancel(context.Background())
	job.SetCancelFunc(cancel)
	defer cancel()

	processUpdateMode(job, cfg, db, registry, ctx)
}

// processUpdateMode handles update mode: generate NFOs and download media files in place (no file organization)
func processUpdateMode(job *worker.BatchJob, cfg *config.Config, db *database.DB, registry *models.ScraperRegistry, ctx context.Context) {
	// Initialize components
	nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, db))
	historyLogger := history.NewLogger(db)

	// Initialize HTTP client for downloader
	httpClient, err := downloader.NewHTTPClientForDownloaderWithRegistry(cfg, registry)
	if err != nil {
		broadcastProgress(&ws.ProgressMessage{
			JobID:    job.ID,
			Status:   "error",
			Progress: 0,
			Message:  fmt.Sprintf("Failed to create HTTP client: %v", err),
		})
		job.MarkFailed()
		return
	}
	dl := downloader.NewDownloaderWithNFOConfig(httpClient, afero.NewOsFs(), &cfg.Output, cfg.Scrapers.UserAgent, cfg.Metadata.NFO.ActressLanguageJA, cfg.Metadata.NFO.FirstNameOrder)

	// Broadcast update started
	broadcastProgress(&ws.ProgressMessage{
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
		broadcastProgress(&ws.ProgressMessage{
			JobID:    job.ID,
			Status:   "update_completed",
			Progress: 100,
			Message:  "Update completed: no files to process (all files failed during scraping)",
		})
		job.MarkCompleted()
		return
	}

	processedFiles := 0

	for filePath, fileResult := range status.Results {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			job.MarkCancelled()
			broadcastProgress(&ws.ProgressMessage{
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

		// Skip files excluded by user
		if job.IsExcluded(filePath) {
			logging.Infof("Skipping excluded file: %s", filePath)
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

		// NFO MERGE LOGIC: Check if NFO already exists and merge if present
		// Default strategy: prefer scraper data (non-destructive update)
		movieToWrite := movie
		var mergeStats *nfo.MergeStats

		// Construct expected NFO path using template (same logic as NFO generation)
		// This ensures we find custom-named NFOs correctly
		tmplCtx := template.NewContextFromMovie(movie)
		tmplCtx.GroupActress = cfg.Output.GroupActress

		// Detect part suffix for multi-part files
		partSuffix := ""
		if cfg.Metadata.NFO.PerFile && filePath != "" {
			videoName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
			partNum, detectedSuffix, patternType := matcher.DetectPartSuffix(videoName, movie.ID)
			partSuffix = detectedSuffix

			// Populate template context with multi-part fields for accurate NFO lookup
			// Note: For single-file context, only explicit patterns (pt1, part2, -1, -2) are
			// considered multipart. Letter patterns need directory context validation which
			// isn't available here.
			tmplCtx.PartNumber = partNum
			tmplCtx.PartSuffix = partSuffix
			tmplCtx.IsMultiPart = patternType == matcher.PatternExplicit
		}

		// Generate expected filename using template
		templateEngine := template.NewEngine()
		nfoFilename, err := templateEngine.Execute(cfg.Metadata.NFO.FilenameTemplate, tmplCtx)
		if err != nil {
			// Fall back to default naming on template error (with sanitization)
			logging.Warnf("Failed to execute NFO filename template: %v, using default", err)
			sanitized := template.SanitizeFilename(movie.ID)
			if sanitized == "" {
				sanitized = "metadata"
			}
			nfoFilename = sanitized + ".nfo"
		} else {
			// Template fully controls suffix/part formatting - do not re-append
			// Case-insensitive .nfo trimming to prevent double extensions
			basename := nfoFilename
			lower := strings.ToLower(basename)
			if strings.HasSuffix(lower, ".nfo") {
				basename = basename[:len(basename)-4]
			}
			sanitized := template.SanitizeFilename(basename)

			// Fallback to safe default if sanitization results in empty string
			if sanitized == "" {
				sanitized = template.SanitizeFilename(movie.ID)
				if sanitized == "" {
					sanitized = "metadata" // Ultimate fallback
				}
			}

			nfoFilename = sanitized + ".nfo"
		}

		nfoPath := filepath.Join(sourceDir, nfoFilename)

		// Also try legacy paths for backward compatibility
		legacyPaths := []string{}
		if nfoFilename != movie.ID+".nfo" {
			legacyPaths = append(legacyPaths, filepath.Join(sourceDir, movie.ID+".nfo"))
		}
		if cfg.Metadata.NFO.PerFile && filePath != "" {
			videoName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
			videoNFO := filepath.Join(sourceDir, videoName+".nfo")
			if videoNFO != nfoPath {
				legacyPaths = append(legacyPaths, videoNFO)
			}
		}

		// Check if NFO exists (try template path first, then legacy)
		foundPath := ""
		if _, err := os.Stat(nfoPath); err == nil {
			foundPath = nfoPath
		} else {
			for _, legacyPath := range legacyPaths {
				if _, err := os.Stat(legacyPath); err == nil {
					foundPath = legacyPath
					logging.Debugf("Found NFO at legacy path: %s", legacyPath)
					break
				}
			}
		}

		if foundPath != "" {
			// NFO exists - parse and merge
			logging.Infof("Found existing NFO, merging data: %s", foundPath)

			parseResult, err := nfo.ParseNFO(afero.NewOsFs(), foundPath)
			if err != nil {
				logging.Warnf("Failed to parse existing NFO %s: %v (will overwrite)", foundPath, err)
			} else {
				// Merge with prefer-scraper strategy (default)
				mergeResult, err := nfo.MergeMovieMetadata(movie, parseResult.Movie, nfo.PreferScraper)
				if err != nil {
					logging.Warnf("Failed to merge NFO data for %s: %v (using scraper data only)", movie.ID, err)
				} else {
					movieToWrite = mergeResult.Merged
					mergeStats = &mergeResult.Stats
					logging.Infof("NFO merge complete for %s: %d from scraper, %d from NFO, %d conflicts resolved",
						movie.ID, mergeStats.FromScraper, mergeStats.FromNFO, mergeStats.ConflictsResolved)
				}
			}
		} else {
			logging.Debugf("No existing NFO found, creating new one at %s", nfoPath)
		}

		// Create multipart info for template conditionals
		var multipart *downloader.MultipartInfo
		if tmplCtx.IsMultiPart {
			multipart = &downloader.MultipartInfo{
				IsMultiPart: tmplCtx.IsMultiPart,
				PartNumber:  tmplCtx.PartNumber,
				PartSuffix:  tmplCtx.PartSuffix,
			}
		}

		// Copy temp cropped poster BEFORE downloads (so downloader skips it)
		copyTempCroppedPoster(job, movieToWrite, sourceDir, cfg, "Update", multipart)

		// Note: partSuffix already computed above for NFO template lookup

		// Generate NFO in source directory (with merged data if applicable)
		nfoErr := nfoGen.Generate(movieToWrite, sourceDir, partSuffix, filePath)
		if nfoErr != nil {
			logging.Warnf("Failed to generate NFO for %s: %v", movieToWrite.ID, nfoErr)
			hasErrors = true
			errorMsg = fmt.Sprintf("NFO generation failed: %v", nfoErr)
		} else {
			if mergeStats != nil {
				logging.Infof("Generated merged NFO in: %s (%d fields from scraper, %d from existing NFO)",
					sourceDir, mergeStats.FromScraper, mergeStats.FromNFO)
			} else {
				logging.Infof("Generated NFO in: %s", sourceDir)
			}
		}

		// Log NFO generation to history
		if logErr := historyLogger.LogNFO(movie.ID, nfoPath, nfoErr); logErr != nil {
			logging.Warnf("Failed to log NFO history for %s: %v", movie.ID, logErr)
		}

		// Download all media files to source directory
		// Use movieToWrite (merged) to include NFO data in downloads
		// Reuse multipart info created earlier for template rendering
		results, err := dl.DownloadAll(movieToWrite, sourceDir, multipart)
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
				// Log download to history (both successful and failed)
				if result.URL != "" {
					if logErr := historyLogger.LogDownload(movie.ID, result.URL, result.LocalPath, string(result.Type), result.Error); logErr != nil {
						logging.Warnf("Failed to log download history for %s: %v", movie.ID, logErr)
					}
				}
			}
		}

		processedFiles++
		progress := float64(processedFiles) / float64(totalFiles) * 100

		// Broadcast progress with error status if errors occurred
		if hasErrors {
			broadcastProgress(&ws.ProgressMessage{
				JobID:    job.ID,
				FilePath: filePath,
				Status:   "failed",
				Progress: progress,
				Message:  fmt.Sprintf("Partial failure for %s (%d/%d)", movie.ID, processedFiles, totalFiles),
				Error:    errorMsg,
			})
		} else {
			broadcastProgress(&ws.ProgressMessage{
				JobID:    job.ID,
				FilePath: filePath,
				Status:   "updated",
				Progress: progress,
				Message:  fmt.Sprintf("Updated %s (%d/%d)", movie.ID, processedFiles, totalFiles),
			})
		}
	}

	// Broadcast completion
	broadcastProgress(&ws.ProgressMessage{
		JobID:    job.ID,
		Status:   "update_completed",
		Progress: 100,
		Message:  fmt.Sprintf("Update completed: %d file(s) processed", processedFiles),
	})
	job.MarkCompleted()
}

// processOrganizeJob processes file organization for a completed scrape job
func processOrganizeJob(job *worker.BatchJob, mat *matcher.Matcher, destination string, copyOnly bool, linkModeRaw string, db *database.DB, cfg *config.Config, registry *models.ScraperRegistry) {
	// Initialize organizer, downloader, NFO generator, and history logger
	org := organizer.NewOrganizer(afero.NewOsFs(), &cfg.Output)
	historyLogger := history.NewLogger(db)
	linkMode, err := organizer.ParseLinkMode(linkModeRaw)
	if err != nil {
		broadcastProgress(&ws.ProgressMessage{
			JobID:    job.ID,
			Status:   "error",
			Progress: 0,
			Message:  fmt.Sprintf("Invalid link mode: %v", err),
		})
		job.MarkFailed()
		return
	}

	// Initialize HTTP client for downloader
	httpClient, err := downloader.NewHTTPClientForDownloaderWithRegistry(cfg, registry)
	if err != nil {
		broadcastProgress(&ws.ProgressMessage{
			JobID:    job.ID,
			Status:   "error",
			Progress: 0,
			Message:  fmt.Sprintf("Failed to create HTTP client: %v", err),
		})
		job.MarkFailed()
		return
	}
	dl := downloader.NewDownloaderWithNFOConfig(httpClient, afero.NewOsFs(), &cfg.Output, "Javinizer/1.0", cfg.Metadata.NFO.ActressLanguageJA, cfg.Metadata.NFO.FirstNameOrder)
	nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, db))

	// Broadcast organization started
	broadcastProgress(&ws.ProgressMessage{
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

		// Skip files excluded by user
		if job.IsExcluded(filePath) {
			logging.Infof("Skipping excluded file: %s", filePath)
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
		result, err := org.OrganizeWithLinkMode(match, movie, destination, false, false, copyOnly, linkMode)
		if err != nil {
			logging.Errorf("Failed to organize %s: %v", filePath, err)
			failed++

			// Log failed organize operation
			if logErr := historyLogger.LogOrganize(movie.ID, filePath, "", false, err); logErr != nil {
				logging.Warnf("Failed to log history for %s: %v", filePath, logErr)
			}

			broadcastProgress(&ws.ProgressMessage{
				JobID:    job.ID,
				FilePath: filePath,
				Status:   "failed",
				Progress: float64(organized+failed) / float64(len(status.Results)) * 100,
				Error:    err.Error(),
			})
			continue
		}

		// Log successful organize operation
		if result.Moved {
			if logErr := historyLogger.LogOrganize(movie.ID, filePath, result.NewPath, false, nil); logErr != nil {
				logging.Warnf("Failed to log history for %s: %v", filePath, logErr)
			}
		}

		// Copy temp cropped poster and download all media files
		if result.Moved {
			// Create multipart info from match for template conditionals
			var multipart *downloader.MultipartInfo
			if match.IsMultiPart {
				multipart = &downloader.MultipartInfo{
					IsMultiPart: match.IsMultiPart,
					PartNumber:  match.PartNumber,
					PartSuffix:  match.PartSuffix,
				}
			}

			// Copy temp cropped poster BEFORE downloads (so downloader skips it)
			copyTempCroppedPoster(job, movie, result.FolderPath, cfg, "Organize", multipart)

			// Download all media files and log to history
			downloadMediaFilesWithHistory(dl, movie, result.FolderPath, cfg, historyLogger, multipart)
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
			nfoErr := nfoGen.Generate(movie, result.FolderPath, partSuffix, videoFilePath)
			if nfoErr != nil {
				logging.Errorf("Failed to generate NFO for %s: %v", movie.ID, nfoErr)
			}

			// Log NFO generation to history
			nfoPath := filepath.Join(result.FolderPath, movie.ID+".nfo")
			if logErr := historyLogger.LogNFO(movie.ID, nfoPath, nfoErr); logErr != nil {
				logging.Warnf("Failed to log NFO history for %s: %v", movie.ID, logErr)
			}
		}

		organized++

		broadcastProgress(&ws.ProgressMessage{
			JobID:    job.ID,
			FilePath: filePath,
			Status:   "organized",
			Progress: float64(organized+failed) / float64(len(status.Results)) * 100,
			Message:  fmt.Sprintf("Organized %s", movie.ID),
		})
	}

	// Broadcast final completion
	broadcastProgress(&ws.ProgressMessage{
		JobID:    job.ID,
		Status:   "organization_completed",
		Progress: 100,
		Message:  fmt.Sprintf("Organized %d files, %d failed", organized, failed),
	})
	job.MarkCompleted()

	// Cleanup temp posters only if ALL files succeeded
	// If any failed, keep temp posters so user can retry without re-scraping
	if failed == 0 {
		go cleanupJobTempPosters(job.ID)
	} else {
		logging.Debugf("[Job %s] Keeping temp posters for %d failed files (retry possible)", job.ID, failed)
	}
}

// generatePreview generates an organize preview response for a movie
// fileResults contains all file results for this movie (to support multi-part files)
func generatePreview(movie *models.Movie, fileResults []*worker.FileResult, destination string, cfg *config.Config) OrganizePreviewResponse {
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

	// Validate folder path length if configured
	if cfg.Output.MaxPathLength > 0 {
		if err := templateEngine.ValidatePathLength(folderPath, cfg.Output.MaxPathLength); err != nil {
			logging.Warnf("Preview: folder path exceeds max length: %s (length: %d, max: %d)", folderPath, len(folderPath), cfg.Output.MaxPathLength)
		}
	}

	// Generate video file paths for all parts (multi-part support)
	videoFiles := make([]string, 0, len(fileResults))
	var primaryVideoPath string

	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			// Get original extension
			ext := filepath.Ext(result.FilePath)
			if ext == "" {
				ext = ".mp4" // Fallback
			}

			// Generate filename using template with multi-part context
			fileCtx := ctx.Clone()
			fileCtx.PartNumber = result.PartNumber
			fileCtx.PartSuffix = result.PartSuffix
			fileCtx.IsMultiPart = result.IsMultiPart

			videoFileName, err := templateEngine.Execute(cfg.Output.FileFormat, fileCtx)
			if err != nil {
				// Fallback to base fileName if template fails
				videoFileName = fileName
				if result.IsMultiPart && result.PartSuffix != "" {
					videoFileName = fileName + result.PartSuffix
				}
			}
			videoFileName = template.SanitizeFilename(videoFileName)

			videoPath := filepath.Join(folderPath, videoFileName+ext)
			videoFiles = append(videoFiles, videoPath)

			// Use first video as primary path for backward compatibility
			if primaryVideoPath == "" {
				primaryVideoPath = videoPath
			}
		}
	}

	// Fallback if no file results (shouldn't happen, but be defensive)
	if primaryVideoPath == "" {
		primaryVideoPath = filepath.Join(folderPath, fileName+".mp4")
		videoFiles = append(videoFiles, primaryVideoPath)
	}

	// Check if multi-part and per_file is enabled
	isMultiPart := len(fileResults) > 1 && fileResults[0] != nil && fileResults[0].IsMultiPart
	generatePerFileNFO := cfg.Metadata.NFO.PerFile && isMultiPart

	// Generate NFO paths using template engine
	var nfoPath string
	var nfoPaths []string

	if generatePerFileNFO {
		// Generate one NFO per video file (matching video file naming)
		nfoPaths = make([]string, 0, len(fileResults))
		for _, result := range fileResults {
			if result != nil && result.FilePath != "" {
				// Generate NFO filename using template with multi-part context
				nfoCtx := ctx.Clone()
				nfoCtx.PartNumber = result.PartNumber
				nfoCtx.PartSuffix = result.PartSuffix
				nfoCtx.IsMultiPart = result.IsMultiPart

				nfoFileName, err := templateEngine.Execute(cfg.Metadata.NFO.FilenameTemplate, nfoCtx)
				if err != nil || nfoFileName == "" {
					// Fallback to fileName-based naming
					nfoFileName = fileName
					if result.IsMultiPart && result.PartSuffix != "" {
						nfoFileName = fileName + result.PartSuffix
					}
				}

				// Case-insensitive .nfo trimming to prevent double extensions
				basename := nfoFileName
				lower := strings.ToLower(basename)
				if strings.HasSuffix(lower, ".nfo") {
					basename = basename[:len(basename)-4]
				}
				sanitized := template.SanitizeFilename(basename)

				// Three-tier fallback for empty results
				if sanitized == "" {
					sanitized = template.SanitizeFilename(fileName)
					if sanitized == "" {
						sanitized = "metadata"
					}
				}

				nfoFilePath := filepath.Join(folderPath, sanitized+".nfo")
				nfoPaths = append(nfoPaths, nfoFilePath)
			}
		}
		// Set primary NFO path for backward compatibility (use first)
		if len(nfoPaths) > 0 {
			nfoPath = nfoPaths[0]
		}
	} else {
		// Single NFO file (default behavior) - use template engine
		nfoFileName, err := templateEngine.Execute(cfg.Metadata.NFO.FilenameTemplate, ctx)
		if err != nil || nfoFileName == "" {
			// Fallback to fileName-based naming
			nfoFileName = fileName + ".nfo"
		} else {
			// Case-insensitive .nfo trimming to prevent double extensions
			basename := nfoFileName
			lower := strings.ToLower(basename)
			if strings.HasSuffix(lower, ".nfo") {
				basename = basename[:len(basename)-4]
			}
			sanitized := template.SanitizeFilename(basename)

			// Three-tier fallback for empty results
			if sanitized == "" {
				sanitized = template.SanitizeFilename(fileName)
				if sanitized == "" {
					sanitized = "metadata"
				}
			}

			nfoFileName = sanitized + ".nfo"
		}
		nfoPath = filepath.Join(folderPath, nfoFileName)
	}

	// Generate poster path using template engine
	// Use first file's multipart context so templates with <IF:MULTIPART> work correctly
	posterCtx := ctx.Clone()
	if len(fileResults) > 0 && fileResults[0] != nil {
		posterCtx.PartNumber = fileResults[0].PartNumber
		posterCtx.PartSuffix = fileResults[0].PartSuffix
		posterCtx.IsMultiPart = fileResults[0].IsMultiPart
	}
	posterFileName, err := templateEngine.Execute(cfg.Output.PosterFormat, posterCtx)
	if err != nil || posterFileName == "" {
		// Fallback to hardcoded format
		posterFileName = fmt.Sprintf("%s-poster.jpg", movie.ID)
	}
	posterFileName = template.SanitizeFilename(posterFileName)
	if posterFileName == "" {
		// Double fallback if sanitization removes everything
		posterFileName = fmt.Sprintf("%s-poster.jpg", template.SanitizeFilename(movie.ID))
	}
	posterPath := filepath.Join(folderPath, posterFileName)

	// Generate fanart path using template engine
	// Use first file's multipart context so templates with <IF:MULTIPART> work correctly
	fanartCtx := ctx.Clone()
	if len(fileResults) > 0 && fileResults[0] != nil {
		fanartCtx.PartNumber = fileResults[0].PartNumber
		fanartCtx.PartSuffix = fileResults[0].PartSuffix
		fanartCtx.IsMultiPart = fileResults[0].IsMultiPart
	}
	fanartFileName, err := templateEngine.Execute(cfg.Output.FanartFormat, fanartCtx)
	if err != nil || fanartFileName == "" {
		// Fallback to hardcoded format
		fanartFileName = fmt.Sprintf("%s-fanart.jpg", movie.ID)
	}
	fanartFileName = template.SanitizeFilename(fanartFileName)
	if fanartFileName == "" {
		// Double fallback if sanitization removes everything
		fanartFileName = fmt.Sprintf("%s-fanart.jpg", template.SanitizeFilename(movie.ID))
	}
	fanartPath := filepath.Join(folderPath, fanartFileName)

	// Use configured screenshot folder name
	extrafanartPath := filepath.Join(folderPath, cfg.Output.ScreenshotFolder)

	// Generate screenshot names using template engine (same as downloader)
	screenshots := []string{}
	if len(movie.Screenshots) > 0 {
		for i := range movie.Screenshots {
			ctx.Index = i + 1 // Set index for template
			screenshotName, err := templateEngine.Execute(cfg.Output.ScreenshotFormat, ctx)
			if err != nil || screenshotName == "" {
				// Fallback to hardcoded format with configurable padding (matching downloader logic)
				if cfg.Output.ScreenshotPadding > 0 {
					screenshotName = fmt.Sprintf("fanart%0*d.jpg", cfg.Output.ScreenshotPadding, i+1)
				} else {
					screenshotName = fmt.Sprintf("fanart%d.jpg", i+1)
				}
			}
			screenshotName = template.SanitizeFilename(screenshotName)
			if screenshotName == "" {
				// Double fallback if sanitization removes everything
				if cfg.Output.ScreenshotPadding > 0 {
					screenshotName = fmt.Sprintf("fanart%0*d.jpg", cfg.Output.ScreenshotPadding, i+1)
				} else {
					screenshotName = fmt.Sprintf("fanart%d.jpg", i+1)
				}
			}
			screenshots = append(screenshots, screenshotName)
		}
	}

	// Validate path lengths if max_path_length is configured
	if cfg.Output.MaxPathLength > 0 {
		// Validate video file paths
		for _, videoPath := range videoFiles {
			if err := templateEngine.ValidatePathLength(videoPath, cfg.Output.MaxPathLength); err != nil {
				logging.Warnf("Preview: video path exceeds max length: %s (length: %d, max: %d)", videoPath, len(videoPath), cfg.Output.MaxPathLength)
			}
		}
		// Validate NFO paths
		if nfoPath != "" {
			if err := templateEngine.ValidatePathLength(nfoPath, cfg.Output.MaxPathLength); err != nil {
				logging.Warnf("Preview: NFO path exceeds max length: %s (length: %d, max: %d)", nfoPath, len(nfoPath), cfg.Output.MaxPathLength)
			}
		}
		for _, nfoFilePath := range nfoPaths {
			if err := templateEngine.ValidatePathLength(nfoFilePath, cfg.Output.MaxPathLength); err != nil {
				logging.Warnf("Preview: NFO path exceeds max length: %s (length: %d, max: %d)", nfoFilePath, len(nfoFilePath), cfg.Output.MaxPathLength)
			}
		}
		// Validate media file paths
		if err := templateEngine.ValidatePathLength(posterPath, cfg.Output.MaxPathLength); err != nil {
			logging.Warnf("Preview: poster path exceeds max length: %s (length: %d, max: %d)", posterPath, len(posterPath), cfg.Output.MaxPathLength)
		}
		if err := templateEngine.ValidatePathLength(fanartPath, cfg.Output.MaxPathLength); err != nil {
			logging.Warnf("Preview: fanart path exceeds max length: %s (length: %d, max: %d)", fanartPath, len(fanartPath), cfg.Output.MaxPathLength)
		}
		// Validate screenshot paths (full paths in extrafanart folder)
		for _, screenshot := range screenshots {
			screenshotPath := filepath.Join(extrafanartPath, screenshot)
			if err := templateEngine.ValidatePathLength(screenshotPath, cfg.Output.MaxPathLength); err != nil {
				logging.Warnf("Preview: screenshot path exceeds max length: %s (length: %d, max: %d)", screenshotPath, len(screenshotPath), cfg.Output.MaxPathLength)
			}
		}
	}

	return OrganizePreviewResponse{
		FolderName:      folderName,
		FileName:        fileName,
		FullPath:        primaryVideoPath, // Backward compatibility
		VideoFiles:      videoFiles,       // All video files (multi-part support)
		NFOPath:         nfoPath,          // Single NFO or first NFO (backward compatibility)
		NFOPaths:        nfoPaths,         // All NFO paths when per_file=true (nil otherwise)
		PosterPath:      posterPath,
		FanartPath:      fanartPath,
		ExtrafanartPath: extrafanartPath,
		Screenshots:     screenshots,
	}
}

// copyFile copies a file from src to dst atomically using streaming I/O
// Returns an error if the source file doesn't exist or if the copy fails
// Uses streaming to avoid loading entire file into memory (safe for large files)
