package batch

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/api/realtime"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// processBatchJob processes a batch scraping job (metadata only, no file organization)
// using concurrent worker pool for improved performance.
// If updateMode is true, will also download media files and generate NFOs in place without moving files.
// scalarStrategy determines how to merge scalar fields (prefer-scraper, prefer-nfo)
// arrayStrategy determines how to merge array fields (merge, replace)
// moveToFolderOverride and renameFolderInPlaceOverride allow per-job folder mode overrides.
// operationModeOverride allows per-job operation mode override (organize, in-place, metadata-only, preview).
func processBatchJob(job *worker.BatchJob, jobQueue *worker.JobQueue, registry *models.ScraperRegistry, agg *aggregator.Aggregator, movieRepo *database.MovieRepository, mat *matcher.Matcher, strict, force, updateMode bool, destination string, cfg *config.Config, selectedScrapers []string, scalarStrategy string, arrayStrategy string, db *database.DB, moveToFolderOverride *bool, renameFolderInPlaceOverride *bool, operationModeOverride string, emitter eventlog.EventEmitter) {
	defer func() {
		if r := recover(); r != nil {
			logging.Errorf("Batch job %s panicked: %v", job.ID, r)
			job.MarkFailed()
			if jobQueue != nil {
				jobQueue.PersistJob(job)
			}
			broadcastProgress(&ws.ProgressMessage{
				JobID:    job.ID,
				Status:   "error",
				Progress: 0,
				Message:  fmt.Sprintf("Batch job panicked: %v", r),
			})
		}
	}()

	// Setup context for cancellation
	ctx, cancel := context.WithCancel(context.Background())
	job.SetCancelFunc(cancel)
	defer cancel()

	job.MarkStarted()
	if jobQueue != nil {
		jobQueue.PersistJob(job)
	}

	// Emit system event for batch job started
	if emitter != nil {
		if err := emitter.EmitSystemEvent("batch", fmt.Sprintf("Batch scrape job %s started", job.ID), models.SeverityInfo, map[string]interface{}{"job_id": job.ID, "file_count": len(job.GetFiles())}); err != nil {
			logging.Warnf("Failed to emit batch start event: %v", err)
		}
	}

	// Log which scrapers will be used
	if len(selectedScrapers) > 0 {
		logging.Infof("Batch job using custom scrapers: %v", selectedScrapers)
	} else {
		logging.Infof("Batch job using default scrapers from config: %v", cfg.Scrapers.Priority)
	}

	// Create progress adapter for WebSocket broadcasting
	adapter := realtime.NewProgressAdapter(job.ID, job, nil)

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
	processedMovieIDs := make(map[string]bool)

	// Track files that failed at submission time to avoid double-emitting events
	submitFailedFiles := make(map[string]bool)

	// Submit tasks to pool
	for i, filePath := range job.GetFiles() {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			job.MarkCancelled()
			if emitter != nil {
				if err := emitter.EmitSystemEvent("batch", fmt.Sprintf("Batch scrape job %s cancelled", job.ID), models.SeverityWarn, map[string]interface{}{"job_id": job.ID}); err != nil {
					logging.Warnf("Failed to emit batch cancel event: %v", err)
				}
			}
			if jobQueue != nil {
				jobQueue.PersistJob(job)
			}
			return
		default:
		}

		// Create unique task ID
		taskID := fmt.Sprintf("batch-scrape-%s-%d", job.ID, i)

		// Register with adapter for WebSocket mapping
		adapter.RegisterTask(taskID, i, filePath)

		// Determine scraper mode contract:
		// - nil = standard batch mode (DB persistence/cache enabled)
		// - non-nil = custom scraper mode (temporary aggregation, no DB persistence)
		// In standard mode, RunBatchScrapeOnce resolves actual scraper order from cfg.Scrapers.Priority.
		// Keep nil here so default mode preserves persistence semantics.
		scrapersToUse := selectedScrapers
		if len(selectedScrapers) == 0 {
			scrapersToUse = nil
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
			scrapersToUse,          // nil = standard mode (uses cfg priority internally), non-nil = custom mode
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
			submitFailedFiles[filePath] = true
			// Emit scraper event for task submission failure
			if emitter != nil {
				if err := emitter.EmitScraperEvent("batch", fmt.Sprintf("Failed to submit scrape task for %s", filePath), models.SeverityError, map[string]interface{}{"job_id": job.ID, "file": filePath, "error": fmt.Sprintf("Failed to submit task: %v", err)}); err != nil {
					logging.Warnf("Failed to emit scrape task failure event: %v", err)
				}
			}
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
			if jobQueue != nil {
				jobQueue.PersistJob(job)
			}
		}
	}

	// Wait for all tasks to complete
	if err := pool.Wait(); err != nil {
		logging.Warnf("Worker pool completed with task failures: %v", err)
	}

	// Mark job as completed (don't auto-process update mode - wait for user to review and click "Update")
	job.MarkCompleted()
	if jobQueue != nil {
		jobQueue.PersistJob(job)
	}

	// Emit system event for batch job completed
	if emitter != nil {
		sev := models.SeverityInfo
		if job.GetFailed() > 0 && job.GetCompleted() > 0 {
			sev = models.SeverityWarn
		} else if job.GetFailed() > 0 && job.GetCompleted() == 0 {
			sev = models.SeverityError
		}
		if err := emitter.EmitSystemEvent("batch", fmt.Sprintf("Batch scrape job %s completed", job.ID), sev, map[string]interface{}{"job_id": job.ID, "completed": job.GetCompleted(), "failed": job.GetFailed()}); err != nil {
			logging.Warnf("Failed to emit batch complete event: %v", err)
		}
	}

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
		if scrapeErr != nil && !submitFailedFiles[filePath] {
			// Emit scraper event for scrape failure (skip if already emitted at submit time)
			if emitter != nil {
				if err := emitter.EmitScraperEvent("batch", fmt.Sprintf("Scrape failed for %s", movieID), models.SeverityWarn, map[string]interface{}{"job_id": job.ID, "movie_id": movieID, "file": filePath, "error": fileResult.Error}); err != nil {
					logging.Warnf("Failed to emit scrape failure event: %v", err)
				}
			}
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
		Message:  fmt.Sprintf("Completed %d of %d files", job.GetCompleted(), job.GetTotalFiles()),
	})
}
