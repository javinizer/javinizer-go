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
		nfoFilename, err := templateEngine.ExecuteWithContext(ctx, cfg.Metadata.NFO.FilenameTemplate, tmplCtx)
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
		// Only generate NFO if enabled in config
		if cfg.Metadata.NFO.Enabled {
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
