package batch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/template"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
)

// processUpdateJob handles update operation triggered from review page
// Generates NFOs and downloads media files in place without moving video files
func processUpdateJob(job *worker.BatchJob, cfg *config.Config, db *database.DB, registry *models.ScraperRegistry, emitter eventlog.EventEmitter) {
	// Setup context for cancellation (mirrors processBatchJob pattern)
	ctx, cancel := context.WithCancel(context.Background())
	job.SetCancelFunc(cancel)
	defer cancel()

	processUpdateMode(job, cfg, db, registry, ctx, emitter)
}

// processUpdateMode handles update mode: generate NFOs and download media files in place (no file organization)
func processUpdateMode(job *worker.BatchJob, cfg *config.Config, db *database.DB, registry *models.ScraperRegistry, ctx context.Context, emitter eventlog.EventEmitter) {
	// Initialize components
	nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, db))
	historyLogger := history.NewLogger(db)
	batchFileOpRepo := database.NewBatchFileOperationRepository(db)

	// Initialize HTTP client for downloader
	httpClient, err := downloader.NewHTTPClientForDownloaderWithRegistry(cfg, registry)
	if err != nil {
		broadcastProgress(&ws.ProgressMessage{
			JobID:    job.ID,
			Status:   "error",
			Progress: 0,
			Message:  fmt.Sprintf("Failed to create HTTP client: %v", err),
		})
		if emitter != nil {
			if emitErr := emitter.EmitOrganizeEvent("batch", fmt.Sprintf("Update job %s failed: HTTP client setup error", job.ID), models.SeverityError, map[string]interface{}{"job_id": job.ID, "error": err.Error()}); emitErr != nil {
				logging.Warnf("Failed to emit update error event: %v", emitErr)
			}
		}
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

	// Emit organize event for update start
	if emitter != nil {
		if err := emitter.EmitOrganizeEvent("batch", fmt.Sprintf("Update started for job %s", job.ID), models.SeverityInfo, map[string]interface{}{"job_id": job.ID}); err != nil {
			logging.Warnf("Failed to emit update start event: %v", err)
		}
	}

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
		if emitter != nil {
			if emitErr := emitter.EmitOrganizeEvent("batch", fmt.Sprintf("Update job %s failed: no processable files", job.ID), models.SeverityError, map[string]interface{}{"job_id": job.ID, "processed_files": 0, "total_scraped": totalFiles}); emitErr != nil {
				logging.Warnf("Failed to emit update complete event: %v", emitErr)
			}
		}
		job.MarkCompleted()
		return
	}

	processedFiles := 0
	failedFiles := 0

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
			if emitter != nil {
				if err := emitter.EmitOrganizeEvent("batch", fmt.Sprintf("Update job %s cancelled", job.ID), models.SeverityWarn, map[string]interface{}{"job_id": job.ID, "processed_files": processedFiles}); err != nil {
					logging.Warnf("Failed to emit update cancel event: %v", err)
				}
			}
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
			failedFiles++
			if emitter != nil {
				if err := emitter.EmitOrganizeEvent("batch", fmt.Sprintf("Invalid movie data for file in job %s", job.ID), models.SeverityError, map[string]interface{}{"job_id": job.ID, "file": filePath}); err != nil {
					logging.Warnf("Failed to emit update error event: %v", err)
				}
			}
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

		// Detect part suffix for multi-part files
		partSuffix := ""
		isMultiPart := false
		var partNum int
		if cfg.Metadata.NFO.PerFile && filePath != "" {
			videoName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
			pn, ps, pt := matcher.DetectPartSuffix(videoName, movie.ID)
			partNum = pn
			partSuffix = ps
			isMultiPart = pt == matcher.PatternExplicit
		}

		// Construct expected NFO path using centralized resolution (matches generator logic)
		nfoFilename := nfo.ResolveNFOFilename(movie, cfg.Metadata.NFO.FilenameTemplate, cfg.Output.GroupActress, cfg.Metadata.NFO.PerFile, isMultiPart, partSuffix)
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

					// Determine DisplayTitle: use template or fallback to Title
					// If Title already looks template-generated (starts with [ID]),
					// use it directly to avoid double-templating.
					titleLooksTemplated := worker.LooksLikeTemplatedTitle(movieToWrite.Title, movieToWrite.ID)
					if titleLooksTemplated {
						movieToWrite.DisplayTitle = movieToWrite.Title
					} else if cfg.Metadata.NFO.DisplayTitle != "" {
						displayTmplCtx := template.NewContextFromMovie(movieToWrite)
						displayEngine := template.NewEngine()
						if displayName, err := displayEngine.ExecuteWithContext(ctx, cfg.Metadata.NFO.DisplayTitle, displayTmplCtx); err == nil {
							movieToWrite.DisplayTitle = displayName
						} else {
							movieToWrite.DisplayTitle = movieToWrite.Title
						}
					} else {
						movieToWrite.DisplayTitle = movieToWrite.Title
					}

					logging.Infof("NFO merge complete for %s: %d from scraper, %d from NFO, %d conflicts resolved",
						movie.ID, mergeStats.FromScraper, mergeStats.FromNFO, mergeStats.ConflictsResolved)
				}
			}
		} else {
			logging.Debugf("No existing NFO found, creating new one at %s", nfoPath)
		}

		// Create multipart info for template conditionals
		var multipart *downloader.MultipartInfo
		if isMultiPart {
			multipart = &downloader.MultipartInfo{
				IsMultiPart: isMultiPart,
				PartNumber:  partNum,
				PartSuffix:  partSuffix,
			}
		}

		posterPath := copyTempCroppedPoster(job, movieToWrite, sourceDir, cfg, "Update", multipart)

		// Capture NFO snapshot BEFORE overwrite (HIST-05: crash-safe)
		snapshotResult := history.ReadNFOSnapshot(afero.NewOsFs(),
			nfoPath,
			filepath.Join(sourceDir, movie.ID+".nfo"),
		)
		if snapshotResult.FoundPath == "" && foundPath != "" {
			snapshotResult = history.ReadNFOSnapshot(afero.NewOsFs(), foundPath, "")
		}
		effectiveNFOPath := nfoPath
		if !cfg.Metadata.NFO.Enabled && snapshotResult.FoundPath != "" {
			effectiveNFOPath = snapshotResult.FoundPath
		}
		if cfg.Metadata.NFO.Enabled && snapshotResult.FoundPath != "" && snapshotResult.FoundPath != nfoPath {
			effectiveNFOPath = snapshotResult.FoundPath
		}
		updateRecord := history.NewPreOrganizeRecord(
			job.ID, movie.ID, filePath, snapshotResult.Content, effectiveNFOPath, sourceDir, models.OperationTypeUpdate, false,
		)
		updateRecord.NewPath = filePath // In update mode, file doesn't move
		if err := batchFileOpRepo.Create(updateRecord); err != nil {
			logging.Warnf("Failed to persist update-mode record for %s: %v", movie.ID, err)
		}

		// Note: partSuffix already computed above for NFO template lookup

		// Generate NFO in source directory (with merged data if applicable)
		// Only generate NFO if enabled in config
		if cfg.Metadata.NFO.Enabled {
			nfoErr := nfoGen.Generate(movieToWrite, sourceDir, partSuffix, filePath)
			if nfoErr != nil {
				logging.Warnf("Failed to generate NFO for %s: %v", movieToWrite.ID, nfoErr)
				hasErrors = true
				errorMsg = fmt.Sprintf("NFO generation failed: %v", nfoErr)

				// Emit organize event for NFO failure
				if emitter != nil {
					if err := emitter.EmitOrganizeEvent("nfo_gen", fmt.Sprintf("NFO generation failed for %s", movieToWrite.ID), models.SeverityError, map[string]interface{}{"job_id": job.ID, "movie_id": movieToWrite.ID, "error": nfoErr.Error()}); err != nil {
						logging.Warnf("Failed to emit NFO failure event: %v", err)
					}
				}
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
		} else {
			logging.Infof("NFO generation disabled in config, skipping for %s", movie.ID)
		}

		// Download all media files to source directory
		// Use movieToWrite (merged) to include NFO data in downloads
		// Reuse multipart info created earlier for template rendering
		results, err := dl.DownloadAll(movieToWrite, sourceDir, multipart)
		if err != nil {
			logging.Warnf("Failed to download media for %s: %v", movie.ID, err)
			hasErrors = true

			// Emit organize event for media download failure
			if emitter != nil {
				if err := emitter.EmitOrganizeEvent("media_download", fmt.Sprintf("Media download failed for %s", movie.ID), models.SeverityError, map[string]interface{}{"job_id": job.ID, "movie_id": movie.ID, "error": err.Error()}); err != nil {
					logging.Warnf("Failed to emit download failure event: %v", err)
				}
			}

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
				if result.Error != nil {
					hasErrors = true
					logging.Warnf("[post-move] mode=Update movie=%s file=%s stage=download media_type=%s url=%s dst=%s err=%v", movie.ID, filePath, result.Type, result.URL, result.LocalPath, result.Error)
					if errorMsg != "" {
						errorMsg += "; "
					}
					errorMsg += fmt.Sprintf("%s download failed: %v", result.Type, result.Error)
				}
				// Log download to history (both successful and failed)
				if result.URL != "" {
					if logErr := historyLogger.LogDownload(movie.ID, result.URL, result.LocalPath, string(result.Type), result.Error); logErr != nil {
						logging.Warnf("Failed to log download history for %s: %v", movie.ID, logErr)
					}
				}
			}
		}

		// Update BatchFileOperation record with generated file paths after post-ops
		if updateRecord.ID > 0 {
			var downloadPaths []string
			if posterPath != "" {
				downloadPaths = append(downloadPaths, posterPath)
			}
			for _, dlResult := range results {
				if dlResult.Downloaded && dlResult.LocalPath != "" {
					downloadPaths = append(downloadPaths, dlResult.LocalPath)
				}
			}
			generatedNFOPath := ""
			if cfg.Metadata.NFO.Enabled {
				generatedNFOPath = nfoPath
			}
			generatedFilesJSON := history.BuildGeneratedFilesJSON(generatedNFOPath, nil, downloadPaths)
			history.UpdatePostOrganize(updateRecord, filePath, false, sourceDir, generatedFilesJSON)
			if err := batchFileOpRepo.Update(updateRecord); err != nil {
				logging.Warnf("Failed to update update-mode record for %s: %v", movie.ID, err)
			}
		}

		processedFiles++
		progress := float64(processedFiles) / float64(totalFiles) * 100

		// Broadcast progress with error status if errors occurred
		if hasErrors {
			failedFiles++
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

	// Emit organize event for update completion
	if emitter != nil {
		sev := models.SeverityInfo
		if failedFiles > 0 && processedFiles > failedFiles {
			sev = models.SeverityWarn
		} else if failedFiles > 0 {
			sev = models.SeverityError
		}
		if err := emitter.EmitOrganizeEvent("batch", fmt.Sprintf("Update completed for job %s", job.ID), sev, map[string]interface{}{"job_id": job.ID, "processed_files": processedFiles, "failed_files": failedFiles}); err != nil {
			logging.Warnf("Failed to emit update complete event: %v", err)
		}
	}

	job.MarkCompleted()
}
