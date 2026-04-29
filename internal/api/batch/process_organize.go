package batch

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
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
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/types"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
)

type organizeDeps struct {
	org             *organizer.Organizer
	historyLogger   *history.Logger
	batchFileOpRepo *database.BatchFileOperationRepository
	dl              *downloader.Downloader
	nfoGen          *nfo.Generator
	linkMode        organizer.LinkMode
	effectiveMode   types.OperationMode
}

func initOrganizeDependencies(job *worker.BatchJob, jobQueue *worker.JobQueue, cfg *config.Config, db *database.DB, registry *models.ScraperRegistry, emitter eventlog.EventEmitter, linkModeRaw string, skipDownload bool) (*organizeDeps, error) {
	outputConfig := cfg.Output
	if job.GetOperationModeOverride() != "" {
		parsed, err := types.ParseOperationMode(job.GetOperationModeOverride())
		if err != nil {
			logging.Warnf("Invalid operation mode override %q: %v, using config default", job.GetOperationModeOverride(), err)
		} else {
			outputConfig.OperationMode = parsed
		}
	}
	effectiveMode := outputConfig.GetOperationMode()
	outputConfig.OperationMode = effectiveMode

	sharedEngine := template.NewEngine()
	org := organizer.NewOrganizer(afero.NewOsFs(), &outputConfig, sharedEngine)
	fileMatcher, err := matcher.NewMatcher(&cfg.Matching)
	if err != nil {
		logging.Warnf("Failed to create matcher: %v (in-place rename disabled for this job)", err)
	} else {
		org.SetMatcher(fileMatcher)
	}

	historyLogger := history.NewLogger(db)
	batchFileOpRepo := database.NewBatchFileOperationRepository(db)

	linkMode, err := organizer.ParseLinkMode(linkModeRaw)
	if err != nil {
		broadcastProgress(&ws.ProgressMessage{
			JobID:    job.ID,
			Status:   "error",
			Progress: 0,
			Message:  fmt.Sprintf("Invalid link mode: %v", err),
		})
		if emitter != nil {
			if emitErr := emitter.EmitOrganizeEvent("batch", fmt.Sprintf("Organize job %s failed: invalid link mode", job.ID), models.SeverityError, map[string]interface{}{"job_id": job.ID, "error": err.Error()}); emitErr != nil {
				logging.Warnf("Failed to emit organize error event: %v", emitErr)
			}
		}
		job.MarkFailed()
		if jobQueue != nil {
			jobQueue.PersistJob(job)
		}
		return nil, err
	}

	var dl *downloader.Downloader
	if !skipDownload {
		httpClient, err := downloader.NewHTTPClientForDownloaderWithRegistry(cfg, registry)
		if err != nil {
			broadcastProgress(&ws.ProgressMessage{
				JobID:    job.ID,
				Status:   "error",
				Progress: 0,
				Message:  fmt.Sprintf("Failed to create HTTP client: %v", err),
			})
			if emitter != nil {
				if emitErr := emitter.EmitOrganizeEvent("batch", fmt.Sprintf("Organize job %s failed: HTTP client setup error", job.ID), models.SeverityError, map[string]interface{}{"job_id": job.ID, "error": err.Error()}); emitErr != nil {
					logging.Warnf("Failed to emit organize error event: %v", emitErr)
				}
			}
			job.MarkFailed()
			if jobQueue != nil {
				jobQueue.PersistJob(job)
			}
			return nil, err
		}
		dl = downloader.NewDownloaderWithNFOConfig(httpClient, afero.NewOsFs(), &cfg.Output, "Javinizer/1.0", cfg.Metadata.NFO.ActressLanguageJA, cfg.Metadata.NFO.FirstNameOrder, sharedEngine)
	}

	nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfo.ConfigFromAppConfig(&cfg.Metadata.NFO, &cfg.Output, &cfg.Metadata, db))

	return &organizeDeps{
		org: org, historyLogger: historyLogger, batchFileOpRepo: batchFileOpRepo,
		dl: dl, nfoGen: nfoGen, linkMode: linkMode, effectiveMode: effectiveMode,
	}, nil
}

func capturePreOrganizeSnapshot(movie *models.Movie, filePath string, sourceDir string, match matcher.MatchResult, cfg *config.Config, batchFileOpRepo *database.BatchFileOperationRepository, job *worker.BatchJob, copyOnly bool, linkMode organizer.LinkMode) (*models.BatchFileOperation, history.NFOSnapshotResult) {
	sourceNFOFilename := nfo.ResolveNFOFilename(movie, cfg.Metadata.NFO.FilenameTemplate, cfg.Output.GroupActress, cfg.Metadata.NFO.PerFile, match.IsMultiPart, match.PartSuffix)
	snapshotCandidates := []string{
		filepath.Join(sourceDir, sourceNFOFilename),
		filepath.Join(sourceDir, movie.ID+".nfo"),
	}
	if cfg.Metadata.NFO.PerFile && filePath != "" {
		videoName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
		videoNFO := filepath.Join(sourceDir, videoName+".nfo")
		if videoNFO != snapshotCandidates[0] && videoNFO != snapshotCandidates[1] {
			snapshotCandidates = append(snapshotCandidates, videoNFO)
		}
	}
	snapshotResult := history.ReadNFOSnapshot(afero.NewOsFs(), snapshotCandidates...)
	opType := history.DetermineOperationType(!copyOnly, linkMode, false)
	preRecord := history.NewPreOrganizeRecord(
		job.ID, movie.ID, filePath, snapshotResult.Content, "", sourceDir, opType, false,
	)
	if err := batchFileOpRepo.Create(preRecord); err != nil {
		logging.Warnf("Failed to persist pre-organize record for %s: %v", movie.ID, err)
	}
	return preRecord, snapshotResult
}

func organizeFile(match matcher.MatchResult, movie *models.Movie, destination string, copyOnly bool, org *organizer.Organizer, linkMode organizer.LinkMode, job *worker.BatchJob, filePath string, historyLogger *history.Logger, emitter eventlog.EventEmitter) (*organizer.OrganizeResult, error) {
	var result *organizer.OrganizeResult
	var organizeErr error

	if copyOnly {
		plan, planErr := org.Plan(match, movie, destination, false)
		if planErr != nil {
			organizeErr = planErr
		} else {
			result, organizeErr = org.CopyWithLinkMode(plan, false, linkMode)
		}
	} else {
		result, organizeErr = org.OrganizeWithLinkMode(match, movie, destination, false, false, copyOnly, linkMode)
	}

	if organizeErr != nil {
		logging.Errorf("Failed to organize %s: %v", filePath, organizeErr)
		if emitter != nil {
			if err := emitter.EmitOrganizeEvent("file_move", fmt.Sprintf("Failed to organize %s", movie.ID), models.SeverityError, map[string]interface{}{"job_id": job.ID, "movie_id": movie.ID, "file": filePath, "error": organizeErr.Error()}); err != nil {
				logging.Warnf("Failed to emit organize failure event: %v", err)
			}
		}
		if logErr := historyLogger.LogOrganize(movie.ID, filePath, "", false, organizeErr); logErr != nil {
			logging.Warnf("Failed to log history for %s: %v", filePath, logErr)
		}
		return nil, organizeErr
	}

	if result.Moved || result.ShouldGenerateMetadata {
		newPath := result.NewPath
		if newPath == "" {
			newPath = result.OriginalPath
		}
		if logErr := historyLogger.LogOrganize(movie.ID, filePath, newPath, false, nil); logErr != nil {
			logging.Warnf("Failed to log history for %s: %v", filePath, logErr)
		}
	}

	return result, nil
}

type postOrganizeResult struct {
	postMoveIssueCount int
	downloadPaths      []string
	partSuffix         string
}

func handlePostOrganize(ctx context.Context, result *organizer.OrganizeResult, movie *models.Movie, filePath string, match matcher.MatchResult, cfg *config.Config, dl *downloader.Downloader, nfoGen *nfo.Generator, historyLogger *history.Logger, batchFileOpRepo *database.BatchFileOperationRepository, preRecord *models.BatchFileOperation, snapshotResult history.NFOSnapshotResult, skipNFO bool, skipDownload bool, job *worker.BatchJob, organized int, failed int) *postOrganizeResult {
	por := &postOrganizeResult{}
	sourceDir := filepath.Dir(filePath)

	for _, subtitle := range result.Subtitles {
		if subtitle.Error != nil {
			por.postMoveIssueCount++
			logging.Warnf("[post-move] mode=Organize movie=%s file=%s stage=subtitle_move src=%s dst=%s err=%v", movie.ID, filePath, subtitle.OriginalPath, subtitle.NewPath, subtitle.Error)
		}
	}

	if result.ShouldGenerateMetadata && !skipDownload {
		var multipart *downloader.MultipartInfo
		if match.IsMultiPart {
			multipart = &downloader.MultipartInfo{
				IsMultiPart: match.IsMultiPart,
				PartNumber:  match.PartNumber,
				PartSuffix:  match.PartSuffix,
			}
		}

		posterPath := copyTempCroppedPoster(job, movie, result.FolderPath, cfg, "Organize", multipart)
		if posterPath != "" {
			por.downloadPaths = append(por.downloadPaths, posterPath)
			movie.ShouldCropPoster = false
		}

		dlPaths := downloadMediaFilesWithHistory(ctx, dl, movie, result.FolderPath, cfg, historyLogger, multipart)
		por.downloadPaths = append(por.downloadPaths, dlPaths...)
	} else if result.ShouldGenerateMetadata && skipDownload {
		logging.Debugf("Media download skipped for %s (skip_download requested)", movie.ID)
	}

	if result.ShouldGenerateMetadata && cfg.Metadata.NFO.Enabled && !skipNFO {
		if cfg.Metadata.NFO.PerFile && match.IsMultiPart {
			por.partSuffix = match.PartSuffix
		}

		videoFilePath := result.NewPath
		if videoFilePath == "" {
			videoFilePath = result.OriginalPath
		}

		if (organized+failed)%10 == 0 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			logging.Debugf("[memory] movie=%s before_nfo alloc=%.1fMB sys=%.1fMB numGC=%d",
				movie.ID, float64(m.Alloc)/1024/1024, float64(m.Sys)/1024/1024, m.NumGC)
		}

		nfoErr := nfoGen.Generate(movie, result.FolderPath, por.partSuffix, videoFilePath)
		if nfoErr != nil {
			por.postMoveIssueCount++
			logging.Warnf("[post-move] mode=Organize movie=%s file=%s stage=nfo_generate folder=%s video=%s part_suffix=%q err=%v", movie.ID, filePath, result.FolderPath, videoFilePath, por.partSuffix, nfoErr)
		}

		nfoPath := filepath.Join(result.FolderPath, nfo.ResolveNFOFilename(movie, cfg.Metadata.NFO.FilenameTemplate, cfg.Output.GroupActress, cfg.Metadata.NFO.PerFile, match.IsMultiPart, por.partSuffix))
		if logErr := historyLogger.LogNFO(movie.ID, nfoPath, nfoErr); logErr != nil {
			logging.Warnf("Failed to log NFO history for %s: %v", movie.ID, logErr)
		}
	} else if result.ShouldGenerateMetadata && (skipNFO || !cfg.Metadata.NFO.Enabled) {
		if skipNFO {
			logging.Debugf("NFO generation skipped for %s (skip_nfo requested)", movie.ID)
		} else {
			logging.Debugf("NFO generation disabled in config, skipping for %s", movie.ID)
		}
	}

	if por.postMoveIssueCount > 0 {
		logging.Warnf("[post-move] mode=Organize movie=%s file=%s stage=summary issues=%d moved_path=%s folder=%s", movie.ID, filePath, por.postMoveIssueCount, result.NewPath, result.FolderPath)
	}

	updatePreOrganizeRecord(preRecord, result, movie, filePath, match, cfg, snapshotResult, sourceDir, por, skipNFO, batchFileOpRepo)

	return por
}

func updatePreOrganizeRecord(preRecord *models.BatchFileOperation, result *organizer.OrganizeResult, movie *models.Movie, filePath string, match matcher.MatchResult, cfg *config.Config, snapshotResult history.NFOSnapshotResult, sourceDir string, por *postOrganizeResult, skipNFO bool, batchFileOpRepo *database.BatchFileOperationRepository) {
	if preRecord.ID <= 0 {
		return
	}
	nfoPath := ""
	generatedNFOPath := ""
	if result.ShouldGenerateMetadata && cfg.Metadata.NFO.Enabled && !skipNFO {
		generatedNFOPath = filepath.Join(result.FolderPath, nfo.ResolveNFOFilename(movie, cfg.Metadata.NFO.FilenameTemplate, cfg.Output.GroupActress, cfg.Metadata.NFO.PerFile, match.IsMultiPart, por.partSuffix))
		if snapshotResult.FoundPath != "" {
			nfoPath = snapshotResult.FoundPath
		} else {
			nfoPath = generatedNFOPath
		}
	} else if snapshotResult.FoundPath != "" {
		nfoPath = snapshotResult.FoundPath
	}
	preRecord.NFOPath = nfoPath
	generatedFilesJSON := history.BuildGeneratedFilesJSON(generatedNFOPath, result.Subtitles, por.downloadPaths)
	inPlaceRenamed := result.InPlaceRenamed
	originalDir := sourceDir
	if result.InPlaceRenamed && result.OldDirectoryPath != "" {
		originalDir = result.OldDirectoryPath
	}
	history.UpdatePostOrganize(preRecord, result.NewPath, inPlaceRenamed, originalDir, generatedFilesJSON)
	if err := batchFileOpRepo.Update(preRecord); err != nil {
		logging.Warnf("Failed to update post-organize record for %s: %v", movie.ID, err)
	}
}

type organizeFileOutcome struct {
	organized bool
	failed    bool
	newPath   string
}

func processOrganizeFile(ctx context.Context, filePath string, fileResult *worker.FileResult, job *worker.BatchJob, destination string, copyOnly bool, cfg *config.Config, deps *organizeDeps, emitter eventlog.EventEmitter, organized int, failed int, skipNFO bool, skipDownload bool) *organizeFileOutcome {
	movie, ok := fileResult.Data.(*models.Movie)
	if !ok {
		logging.Errorf("Invalid movie data type for file: %s", filePath)
		return &organizeFileOutcome{failed: true}
	}

	ext := filepath.Ext(filePath)
	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      filePath,
			Name:      filepath.Base(filePath),
			Extension: ext,
			Dir:       filepath.Dir(filePath),
		},
		ID:          movie.ID,
		IsMultiPart: fileResult.IsMultiPart,
		PartNumber:  fileResult.PartNumber,
		PartSuffix:  fileResult.PartSuffix,
	}

	sourceDir := filepath.Dir(filePath)
	preRecord, snapshotResult := capturePreOrganizeSnapshot(movie, filePath, sourceDir, match, cfg, deps.batchFileOpRepo, job, copyOnly, deps.linkMode)

	result, organizeErr := organizeFile(match, movie, destination, copyOnly, deps.org, deps.linkMode, job, filePath, deps.historyLogger, emitter)
	if organizeErr != nil {
		broadcastProgress(&ws.ProgressMessage{
			JobID:    job.ID,
			FilePath: filePath,
			Status:   "failed",
			Progress: float64(organized+failed) / float64(len(job.GetStatus().Results)) * 100,
			Error:    organizeErr.Error(),
		})
		return &organizeFileOutcome{failed: true}
	}

	handlePostOrganize(ctx, result, movie, filePath, match, cfg, deps.dl, deps.nfoGen, deps.historyLogger, deps.batchFileOpRepo, preRecord, snapshotResult, skipNFO, skipDownload, job, organized, failed)

	return &organizeFileOutcome{organized: true, newPath: result.NewPath}
}

func emitOrganizeCompletion(emitter eventlog.EventEmitter, job *worker.BatchJob, organized int, failed int) {
	broadcastProgress(&ws.ProgressMessage{
		JobID:    job.ID,
		Status:   "organization_completed",
		Progress: 100,
		Message:  fmt.Sprintf("Organized %d files, %d failed", organized, failed),
	})

	if emitter != nil {
		sev := models.SeverityInfo
		if failed > 0 && organized > 0 {
			sev = models.SeverityWarn
		} else if failed > 0 && organized == 0 {
			sev = models.SeverityError
		}
		if err := emitter.EmitOrganizeEvent("batch", fmt.Sprintf("File organization completed for job %s", job.ID), sev, map[string]interface{}{"job_id": job.ID, "organized": organized, "failed": failed}); err != nil {
			logging.Warnf("Failed to emit organize complete event: %v", err)
		}
	}
}

func finalizeOrganizeJob(job *worker.BatchJob, jobQueue *worker.JobQueue, organized int, failed int) {
	if failed == 0 && organized > 0 {
		job.MarkOrganized()
	} else {
		job.MarkCompleted()
	}
	if jobQueue != nil {
		jobQueue.PersistJob(job)
	}
}

func processOrganizeJob(ctx context.Context, job *worker.BatchJob, jobQueue *worker.JobQueue, destination string, copyOnly bool, linkModeRaw string, skipNFO bool, skipDownload bool, db *database.DB, cfg *config.Config, registry *models.ScraperRegistry, emitter eventlog.EventEmitter) {
	deps, err := initOrganizeDependencies(job, jobQueue, cfg, db, registry, emitter, linkModeRaw, skipDownload)
	if err != nil {
		return
	}

	broadcastProgress(&ws.ProgressMessage{
		JobID:    job.ID,
		Status:   "organizing",
		Progress: 0,
		Message:  "Starting file organization",
	})

	if emitter != nil {
		if err := emitter.EmitOrganizeEvent("batch", fmt.Sprintf("File organization started for job %s", job.ID), models.SeverityInfo, map[string]interface{}{"job_id": job.ID, "mode": string(deps.effectiveMode)}); err != nil {
			logging.Warnf("Failed to emit organize start event: %v", err)
		}
	}

	status := job.GetStatus()
	organized := 0
	failed := 0
	totalFiles := len(status.Results)

	for filePath, fileResult := range status.Results {
		select {
		case <-ctx.Done():
			broadcastProgress(&ws.ProgressMessage{
				JobID:    job.ID,
				Status:   "cancelled",
				Progress: float64(organized+failed) / float64(totalFiles) * 100,
				Message:  fmt.Sprintf("Organize cancelled (%d/%d files processed)", organized, totalFiles),
			})
			if emitter != nil {
				if err := emitter.EmitOrganizeEvent("batch", fmt.Sprintf("Organize job %s cancelled", job.ID), models.SeverityWarn, map[string]interface{}{"job_id": job.ID, "organized": organized, "failed": failed}); err != nil {
					logging.Warnf("Failed to emit organize cancel event: %v", err)
				}
			}
			job.MarkCancelled()
			if jobQueue != nil {
				jobQueue.PersistJob(job)
			}
			return
		default:
		}

		if fileResult.Status != worker.JobStatusCompleted || fileResult.Data == nil {
			continue
		}
		if job.IsExcluded(filePath) {
			logging.Infof("Skipping excluded file: %s", filePath)
			continue
		}

		outcome := processOrganizeFile(ctx, filePath, fileResult, job, destination, copyOnly, cfg, deps, emitter, organized, failed, skipNFO, skipDownload)
		if outcome.organized {
			organized++
		}
		if outcome.failed {
			failed++
		}

		if outcome.organized {
			movie, _ := fileResult.Data.(*models.Movie)
			if emitter != nil {
				if err := emitter.EmitOrganizeEvent("file_move", fmt.Sprintf("Organized %s", movie.ID), models.SeverityInfo, map[string]interface{}{"job_id": job.ID, "movie_id": movie.ID, "file": filePath, "new_path": outcome.newPath}); err != nil {
					logging.Warnf("Failed to emit organize success event: %v", err)
				}
			}
			broadcastProgress(&ws.ProgressMessage{
				JobID:    job.ID,
				FilePath: filePath,
				Status:   "organized",
				Progress: float64(organized+failed) / float64(totalFiles) * 100,
				Message:  fmt.Sprintf("Organized %s", movie.ID),
			})
			delete(status.Results, filePath)
		}
	}

	emitOrganizeCompletion(emitter, job, organized, failed)
	finalizeOrganizeJob(job, jobQueue, organized, failed)
}
