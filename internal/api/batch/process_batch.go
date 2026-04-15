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
type BatchProcessOptions struct {
	Job                         *worker.BatchJob
	JobQueue                    *worker.JobQueue
	Registry                    *models.ScraperRegistry
	Aggregator                  *aggregator.Aggregator
	MovieRepo                   *database.MovieRepository
	Matcher                     *matcher.Matcher
	Strict                      bool
	Force                       bool
	UpdateMode                  bool
	Destination                 string
	Cfg                         *config.Config
	SelectedScrapers            []string
	ScalarStrategy              string
	ArrayStrategy               string
	DB                          *database.DB
	MoveToFolderOverride        *bool
	RenameFolderInPlaceOverride *bool
	OperationModeOverride       string
	Emitter                     eventlog.EventEmitter
}

func processBatchJob(opts *BatchProcessOptions) {
	defer func() {
		if r := recover(); r != nil {
			logging.Errorf("Batch job %s panicked: %v", opts.Job.ID, r)
			opts.Job.MarkFailed()
			if opts.JobQueue != nil {
				opts.JobQueue.PersistJob(opts.Job)
			}
			broadcastProgress(&ws.ProgressMessage{
				JobID:    opts.Job.ID,
				Status:   "error",
				Progress: 0,
				Message:  fmt.Sprintf("Batch job panicked: %v", r),
			})
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	opts.Job.SetCancelFunc(cancel)
	defer cancel()

	opts.Job.MarkStarted()
	if opts.JobQueue != nil {
		opts.JobQueue.PersistJob(opts.Job)
	}

	if opts.Emitter != nil {
		if err := opts.Emitter.EmitSystemEvent("batch", fmt.Sprintf("Batch scrape job %s started", opts.Job.ID), models.SeverityInfo, map[string]interface{}{"job_id": opts.Job.ID, "file_count": len(opts.Job.GetFiles())}); err != nil {
			logging.Warnf("Failed to emit batch start event: %v", err)
		}
	}

	if len(opts.SelectedScrapers) > 0 {
		logging.Infof("Batch job using custom scrapers: %v", opts.SelectedScrapers)
	} else {
		logging.Infof("Batch job using default scrapers from config: %v", opts.Cfg.Scrapers.Priority)
	}

	adapter := realtime.NewProgressAdapter(opts.Job.ID, opts.Job, nil)
	progressTracker := worker.NewProgressTracker(adapter.GetChannel())
	adapter.Start()
	defer adapter.Stop()

	maxWorkers := opts.Cfg.Performance.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 5
	}

	timeout := time.Duration(opts.Cfg.Performance.WorkerTimeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	pool := worker.NewPoolWithContext(ctx, maxWorkers, timeout, progressTracker)
	defer pool.Stop()

	httpClient, err := downloader.NewHTTPClientForDownloaderWithRegistry(opts.Cfg, opts.Registry)
	if err != nil {
		logging.Warnf("Failed to create HTTP client for poster downloads: %v (will skip poster generation)", err)
		httpClient = nil
	}

	processedMovieIDs := make(map[string]bool)
	submitFailedFiles := make(map[string]bool)

	for i, filePath := range opts.Job.GetFiles() {
		select {
		case <-ctx.Done():
			opts.Job.MarkCancelled()
			if opts.Emitter != nil {
				if err := opts.Emitter.EmitSystemEvent("batch", fmt.Sprintf("Batch scrape job %s cancelled", opts.Job.ID), models.SeverityWarn, map[string]interface{}{"job_id": opts.Job.ID}); err != nil {
					logging.Warnf("Failed to emit batch cancel event: %v", err)
				}
			}
			if opts.JobQueue != nil {
				opts.JobQueue.PersistJob(opts.Job)
			}
			return
		default:
		}

		taskID := fmt.Sprintf("batch-scrape-%s-%d", opts.Job.ID, i)
		adapter.RegisterTask(taskID, i, filePath)

		scrapersToUse := opts.SelectedScrapers
		if len(opts.SelectedScrapers) == 0 {
			scrapersToUse = nil
		}

		task := worker.NewBatchScrapeTask(&worker.BatchScrapeOptions{
			TaskID:            taskID,
			FilePath:          filePath,
			FileIndex:         i,
			Job:               opts.Job,
			Registry:          opts.Registry,
			Aggregator:        opts.Aggregator,
			MovieRepo:         opts.MovieRepo,
			Matcher:           opts.Matcher,
			ProgressTracker:   progressTracker,
			Force:             opts.Force,
			UpdateMode:        opts.UpdateMode,
			SelectedScrapers:  scrapersToUse,
			HTTPClient:        httpClient,
			UserAgent:         opts.Cfg.Scrapers.UserAgent,
			Referer:           opts.Cfg.Scrapers.Referer,
			ProcessedMovieIDs: processedMovieIDs,
			Cfg:               opts.Cfg,
			ScalarStrategy:    opts.ScalarStrategy,
			ArrayStrategy:     opts.ArrayStrategy,
		})

		if err := pool.Submit(task); err != nil {
			logging.Errorf("Failed to submit task for %s: %v", filePath, err)
			submitFailedFiles[filePath] = true
			if opts.Emitter != nil {
				if err := opts.Emitter.EmitScraperEvent("batch", fmt.Sprintf("Failed to submit scrape task for %s", filePath), models.SeverityError, map[string]interface{}{"job_id": opts.Job.ID, "file": filePath, "error": fmt.Sprintf("Failed to submit task: %v", err)}); err != nil {
					logging.Warnf("Failed to emit scrape task failure event: %v", err)
				}
			}
			result := &worker.FileResult{
				FilePath:  filePath,
				Status:    worker.JobStatusFailed,
				Error:     fmt.Sprintf("Failed to submit task: %v", err),
				StartedAt: time.Now(),
			}
			now := time.Now()
			result.EndedAt = &now
			opts.Job.UpdateFileResult(filePath, result)
			if opts.JobQueue != nil {
				opts.JobQueue.PersistJob(opts.Job)
			}
		}
	}

	if err := pool.Wait(); err != nil {
		logging.Warnf("Worker pool completed with task failures: %v", err)
	}

	opts.Job.MarkCompleted()
	if opts.JobQueue != nil {
		opts.JobQueue.PersistJob(opts.Job)
	}

	if opts.Emitter != nil {
		sev := models.SeverityInfo
		if opts.Job.GetFailed() > 0 && opts.Job.GetCompleted() > 0 {
			sev = models.SeverityWarn
		} else if opts.Job.GetFailed() > 0 && opts.Job.GetCompleted() == 0 {
			sev = models.SeverityError
		}
		if err := opts.Emitter.EmitSystemEvent("batch", fmt.Sprintf("Batch scrape job %s completed", opts.Job.ID), sev, map[string]interface{}{"job_id": opts.Job.ID, "completed": opts.Job.GetCompleted(), "failed": opts.Job.GetFailed()}); err != nil {
			logging.Warnf("Failed to emit batch complete event: %v", err)
		}
	}

	historyLogger := history.NewLogger(opts.DB)
	status := opts.Job.GetStatus()
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
			if opts.Emitter != nil {
				if err := opts.Emitter.EmitScraperEvent("batch", fmt.Sprintf("Scrape failed for %s", movieID), models.SeverityWarn, map[string]interface{}{"job_id": opts.Job.ID, "movie_id": movieID, "file": filePath, "error": fileResult.Error}); err != nil {
					logging.Warnf("Failed to emit scrape failure event: %v", err)
				}
			}
		}
	}

	broadcastProgress(&ws.ProgressMessage{
		JobID:    opts.Job.ID,
		Status:   "completed",
		Progress: 100,
		Message:  fmt.Sprintf("Completed %d of %d files", opts.Job.GetCompleted(), opts.Job.GetTotalFiles()),
	})
}
