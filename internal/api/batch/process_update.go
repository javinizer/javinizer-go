package batch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

type UpdateOptions struct {
	ForceOverwrite bool
	PreserveNFO    bool
	Preset         string
	ScalarStrategy string
	ArrayStrategy  string
	SkipNFO        bool
	SkipDownload   bool
}

func processUpdateJob(job *worker.BatchJob, cfg *config.Config, db *database.DB, registry *models.ScraperRegistry, emitter eventlog.EventEmitter, opts *UpdateOptions) {
	if opts == nil {
		opts = &UpdateOptions{}
	}

	timeout := time.Duration(cfg.Performance.WorkerTimeout) * time.Second
	if timeout <= 0 {
		timeout = 600 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	job.SetCancelFunc(cancel)
	defer cancel()

	processUpdateMode(job, cfg, db, registry, ctx, emitter, opts)
}

func processUpdateMode(job *worker.BatchJob, cfg *config.Config, db *database.DB, registry *models.ScraperRegistry, ctx context.Context, emitter eventlog.EventEmitter, opts *UpdateOptions) {
	nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, db))
	historyLogger := history.NewLogger(db)
	batchFileOpRepo := database.NewBatchFileOperationRepository(db)

	var dl *downloader.Downloader
	if !opts.SkipDownload {
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
		dl = downloader.NewDownloaderWithNFOConfig(httpClient, afero.NewOsFs(), &cfg.Output, cfg.Scrapers.UserAgent, cfg.Metadata.NFO.ActressLanguageJA, cfg.Metadata.NFO.FirstNameOrder, nil)
	}

	broadcastProgress(&ws.ProgressMessage{
		JobID:    job.ID,
		Status:   "updating",
		Progress: 0,
		Message:  "Generating NFOs and downloading media files in place",
	})

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
		select {
		case <-ctx.Done():
			job.MarkCancelled()
			cancelMsg := "Update cancelled"
			if ctx.Err() == context.DeadlineExceeded {
				cancelMsg = fmt.Sprintf("Update timed out after %ds", cfg.Performance.WorkerTimeout)
			}
			broadcastProgress(&ws.ProgressMessage{
				JobID:    job.ID,
				Status:   "cancelled",
				Progress: float64(processedFiles) / float64(totalFiles) * 100,
				Message:  fmt.Sprintf("%s (%d/%d files processed)", cancelMsg, processedFiles, totalFiles),
			})
			if emitter != nil {
				if err := emitter.EmitOrganizeEvent("batch", fmt.Sprintf("Update job %s cancelled", job.ID), models.SeverityWarn, map[string]interface{}{"job_id": job.ID, "processed_files": processedFiles}); err != nil {
					logging.Warnf("Failed to emit update cancel event: %v", err)
				}
			}
			return
		default:
		}

		fileTimeout := 120 * time.Second
		if cfg.Performance.WorkerTimeout > 0 && len(status.Results) > 0 {
			fileTimeout = time.Duration(cfg.Performance.WorkerTimeout/len(status.Results)+1) * time.Second
			if fileTimeout < 30*time.Second {
				fileTimeout = 30 * time.Second
			}
			if fileTimeout > 600*time.Second {
				fileTimeout = 600 * time.Second
			}
		}
		fileCtx, fileCancel := context.WithTimeout(ctx, fileTimeout)
		defer fileCancel()

		if fileResult.Status != worker.JobStatusCompleted || fileResult.Data == nil {
			continue
		}

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

		sourceDir := filepath.Dir(filePath)

		hasErrors := false
		errorMsg := ""

		movieToWrite := movie
		var mergeStats *nfo.MergeStats

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

		nfoPath, legacyPaths := nfo.ResolveNFOPath(sourceDir, movie, cfg.Metadata.NFO.FilenameTemplate, cfg.Output.GroupActress, cfg.Metadata.NFO.PerFile, isMultiPart, partSuffix, filePath)

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

		if !opts.ForceOverwrite {
			scalarStrategy := nfo.PreferScraper
			mergeArrays := true

			if opts.ScalarStrategy != "" {
				scalarStrategy = nfo.ParseScalarStrategy(opts.ScalarStrategy)
			}

			if opts.ArrayStrategy != "" {
				mergeArrays = nfo.ParseArrayStrategy(opts.ArrayStrategy)
			}

			if opts.Preset != "" {
				resolvedScalar, resolvedArray, presetErr := nfo.ApplyPreset(opts.Preset, opts.ScalarStrategy, opts.ArrayStrategy)
				if presetErr != nil {
					logging.Warnf("Invalid preset %q: %v, using defaults", opts.Preset, presetErr)
				} else {
					scalarStrategy = nfo.ParseScalarStrategy(resolvedScalar)
					mergeArrays = nfo.ParseArrayStrategy(resolvedArray)
				}
			}

			if opts.PreserveNFO {
				scalarStrategy = nfo.PreserveExisting
			}

			if foundPath != "" {
				logging.Infof("Found existing NFO, merging data: %s", foundPath)

				parseResult, err := nfo.ParseNFO(afero.NewOsFs(), foundPath)
				if err != nil {
					logging.Warnf("Failed to parse existing NFO %s: %v (will overwrite)", foundPath, err)
				} else {
					mergeResult, err := nfo.MergeMovieMetadataWithOptions(movie, parseResult.Movie, scalarStrategy, mergeArrays)
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
		}

		if cfg.Metadata.NFO.DisplayTitle != "" {
			displayTmplCtx := template.NewContextFromMovie(movieToWrite)
			displayTmplCtx.Title = movie.Title
			displayEngine := template.NewEngine()
			if displayName, err := displayEngine.ExecuteWithContext(fileCtx, cfg.Metadata.NFO.DisplayTitle, displayTmplCtx); err == nil {
				movieToWrite.DisplayTitle = displayName
			} else if movieToWrite.DisplayTitle == "" {
				movieToWrite.DisplayTitle = movieToWrite.Title
			}
		} else if movieToWrite.DisplayTitle == "" {
			movieToWrite.DisplayTitle = movieToWrite.Title
		}

		var multipart *downloader.MultipartInfo
		if isMultiPart {
			multipart = &downloader.MultipartInfo{
				IsMultiPart: isMultiPart,
				PartNumber:  partNum,
				PartSuffix:  partSuffix,
			}
		}

		var posterPath string
		if !opts.SkipDownload {
			posterPath = copyTempCroppedPoster(job, movieToWrite, sourceDir, cfg, "Update", multipart)
			if posterPath != "" {
				movieToWrite.ShouldCropPoster = false
			}
		}

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
		updateRecord.NewPath = filePath
		if err := batchFileOpRepo.Create(updateRecord); err != nil {
			logging.Warnf("Failed to persist update-mode record for %s: %v", movie.ID, err)
		}

		var nfoErr error
		if cfg.Metadata.NFO.Enabled && !opts.SkipNFO {
			nfoErr = nfoGen.Generate(movieToWrite, sourceDir, partSuffix, filePath)
			if nfoErr != nil {
				logging.Warnf("Failed to generate NFO for %s: %v", movieToWrite.ID, nfoErr)
				hasErrors = true
				errorMsg = fmt.Sprintf("NFO generation failed: %v", nfoErr)

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

			if logErr := historyLogger.LogNFO(movie.ID, nfoPath, nfoErr); logErr != nil {
				logging.Warnf("Failed to log NFO history for %s: %v", movie.ID, logErr)
			}
		} else if opts.SkipNFO {
			logging.Debugf("NFO generation skipped for %s (skip_nfo requested)", movie.ID)
		} else {
			logging.Infof("NFO generation disabled in config, skipping for %s", movie.ID)
		}

		var results []downloader.DownloadResult
		if !opts.SkipDownload {
			dlResults, dlErr := dl.DownloadAll(fileCtx, movieToWrite, sourceDir, multipart)
			if dlErr != nil {
				logging.Warnf("Failed to download media for %s: %v", movie.ID, dlErr)
				hasErrors = true

				if emitter != nil {
					if err := emitter.EmitOrganizeEvent("media_download", fmt.Sprintf("Media download failed for %s", movie.ID), models.SeverityError, map[string]interface{}{"job_id": job.ID, "movie_id": movie.ID, "error": dlErr.Error()}); err != nil {
						logging.Warnf("Failed to emit download failure event: %v", err)
					}
				}

				if errorMsg != "" {
					errorMsg += "; Media download failed: " + dlErr.Error()
				} else {
					errorMsg = fmt.Sprintf("Media download failed: %v", dlErr)
				}
			} else {
				results = dlResults
				for _, result := range dlResults {
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
					if result.URL != "" {
						if logErr := historyLogger.LogDownload(movie.ID, result.URL, result.LocalPath, string(result.Type), result.Error); logErr != nil {
							logging.Warnf("Failed to log download history for %s: %v", movie.ID, logErr)
						}
					}
				}
			}
		} else {
			logging.Debugf("Media download skipped for %s (skip_download requested)", movie.ID)
		}

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
			if cfg.Metadata.NFO.Enabled && !opts.SkipNFO {
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

		if hasErrors {
			failedFiles++
			_ = job.AtomicUpdateFileResult(filePath, func(fr *worker.FileResult) (*worker.FileResult, error) {
				fr.Status = worker.JobStatusCompleted
				if fr.Error == "" {
					fr.Error = errorMsg
				} else {
					fr.Error = fr.Error + "; " + errorMsg
				}
				return fr, nil
			})
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

	broadcastProgress(&ws.ProgressMessage{
		JobID:    job.ID,
		Status:   "update_completed",
		Progress: 100,
		Message:  fmt.Sprintf("Update completed: %d file(s) processed", processedFiles),
	})

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
