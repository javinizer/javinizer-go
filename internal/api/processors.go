package api

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/javinizer/javinizer-go/internal/template"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// processBatchJob processes a batch scraping job (metadata only, no file organization)
func processBatchJob(job *worker.BatchJob, registry *models.ScraperRegistry, agg *aggregator.Aggregator, movieRepo *database.MovieRepository, mat *matcher.Matcher, strict, force bool, destination string, cfg *config.Config) {
	ctx, cancel := context.WithCancel(context.Background())
	job.CancelFunc = cancel
	defer cancel()

	job.MarkStarted()

	for i, filePath := range job.Files {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			job.MarkCancelled()
			return
		default:
		}

		// Get movie ID from filename
		fileInfo := scanner.FileInfo{
			Path:      filePath,
			Name:      filepath.Base(filePath),
			Extension: filepath.Ext(filePath),
			Dir:       filepath.Dir(filePath),
		}
		matchResults := mat.Match([]scanner.FileInfo{fileInfo})
		if len(matchResults) == 0 {
			result := &worker.FileResult{
				FilePath:  filePath,
				Status:    worker.JobStatusFailed,
				Error:     "Could not extract movie ID from filename",
				StartedAt: time.Now(),
			}
			now := time.Now()
			result.EndedAt = &now
			job.UpdateFileResult(filePath, result)

			// Broadcast progress
			wsHub.BroadcastProgress(&ws.ProgressMessage{
				JobID:     job.ID,
				FileIndex: i,
				FilePath:  filePath,
				Status:    string(worker.JobStatusFailed),
				Progress:  job.Progress,
				Error:     result.Error,
			})
			continue
		}

		movieID := matchResults[0].ID
		result := &worker.FileResult{
			FilePath:  filePath,
			MovieID:   movieID,
			Status:    worker.JobStatusRunning,
			StartedAt: time.Now(),
		}
		job.UpdateFileResult(filePath, result)

		// Broadcast start
		wsHub.BroadcastProgress(&ws.ProgressMessage{
			JobID:     job.ID,
			FileIndex: i,
			FilePath:  filePath,
			Status:    "running",
			Progress:  job.Progress,
			Message:   fmt.Sprintf("Scraping %s", movieID),
		})

		// Check cache first
		if !force {
			existing, err := movieRepo.FindByID(movieID)
			if err == nil && existing != nil {
				result.Status = worker.JobStatusCompleted
				result.Data = existing
				now := time.Now()
				result.EndedAt = &now
				job.UpdateFileResult(filePath, result)

				wsHub.BroadcastProgress(&ws.ProgressMessage{
					JobID:     job.ID,
					FileIndex: i,
					FilePath:  filePath,
					Status:    "completed",
					Progress:  job.Progress,
					Message:   "Found in cache",
				})
				continue
			}
		}
		// Phase 1: Content-ID Resolution using DMM
		var resolvedID string
		if dmmScraper, exists := registry.Get("dmm"); exists {
			if dmmScraperTyped, ok := dmmScraper.(*dmm.Scraper); ok {
				contentID, err := dmmScraperTyped.ResolveContentID(movieID)
				if err != nil {
					logging.Debugf("[%s] DMM content-ID resolution failed: %v, will use original ID", movieID, err)
					resolvedID = movieID // Fallback to original ID
				} else {
					resolvedID = contentID
					logging.Debugf("[%s] Resolved content-ID: %s", movieID, resolvedID)
				}
			} else {
				logging.Debugf("[%s] DMM scraper type assertion failed, using original ID", movieID)
				resolvedID = movieID
			}
		} else {
			logging.Debugf("[%s] DMM scraper not available, using original ID", movieID)
			resolvedID = movieID
		}

		// Phase 2: Scrape from sources

		results := []*models.ScraperResult{}
		errors := []string{}

		for _, scraper := range registry.GetByPriority(cfg.Scrapers.Priority) {
			logging.Debugf("[%s] Querying scraper: %s", movieID, scraper.Name())
			scraperResult, err := scraper.Search(resolvedID)
			if err != nil {
				logging.Debugf("[%s] Scraper %s failed: %v", movieID, scraper.Name(), err)
				// If scraping with resolved ID fails, try with original ID before giving up
				if resolvedID != movieID {
					logging.Debugf("[%s] Retrying scraper %s with original ID: %s", movieID, scraper.Name(), movieID)
					scraperResult, err = scraper.Search(movieID)
					if err != nil {
						logging.Debugf("[%s] Scraper %s failed with original ID: %v", movieID, scraper.Name(), err)
						errors = append(errors, fmt.Sprintf("%s: %v", scraper.Name(), err))
						continue
					}
				} else {
					errors = append(errors, fmt.Sprintf("%s: %v", scraper.Name(), err))
					continue
				}
			}
			logging.Debugf("[%s] Scraper %s returned: Title=%s, Language=%s, Actresses=%d, Genres=%d",
				movieID, scraper.Name(), scraperResult.Title, scraperResult.Language, len(scraperResult.Actresses), len(scraperResult.Genres))
			results = append(results, scraperResult)
		}

		if len(results) == 0 {
			result.Status = worker.JobStatusFailed
			result.Error = fmt.Sprintf("Movie not found: %s", strings.Join(errors, "; "))
			now := time.Now()
			result.EndedAt = &now
			job.UpdateFileResult(filePath, result)

			wsHub.BroadcastProgress(&ws.ProgressMessage{
				JobID:     job.ID,
				FileIndex: i,
				FilePath:  filePath,
				Status:    "failed",
				Progress:  job.Progress,
				Error:     result.Error,
			})
			continue
		}

		// Aggregate results
		movie, err := agg.Aggregate(results)
		if err != nil {
			result.Status = worker.JobStatusFailed
			result.Error = err.Error()
			now := time.Now()
			result.EndedAt = &now
			job.UpdateFileResult(filePath, result)

			wsHub.BroadcastProgress(&ws.ProgressMessage{
				JobID:     job.ID,
				FileIndex: i,
				FilePath:  filePath,
				Status:    "failed",
				Progress:  job.Progress,
				Error:     err.Error(),
			})
			continue
		}

		movie.OriginalFileName = filepath.Base(filePath)

		// Save to database
		if err := movieRepo.Upsert(movie); err != nil {
			logging.Errorf("Failed to save movie to database: %v", err)
		}

		// Reload movie from database to get associations (actresses, genres)
		reloadedMovie, err := movieRepo.FindByID(movie.ID)
		if err != nil {
			logging.Errorf("Failed to reload movie from database: %v", err)
			reloadedMovie = movie // Fallback to original if reload fails
		}

		result.Status = worker.JobStatusCompleted
		result.Data = reloadedMovie
		now := time.Now()
		result.EndedAt = &now
		job.UpdateFileResult(filePath, result)

		wsHub.BroadcastProgress(&ws.ProgressMessage{
			JobID:     job.ID,
			FileIndex: i,
			FilePath:  filePath,
			Status:    "completed",
			Progress:  job.Progress,
			Message:   fmt.Sprintf("Scraped %s successfully", movieID),
		})
	}

	job.MarkCompleted()

	// Broadcast final completion
	wsHub.BroadcastProgress(&ws.ProgressMessage{
		JobID:    job.ID,
		Status:   "completed",
		Progress: 100,
		Message:  fmt.Sprintf("Completed %d of %d files", job.Completed, job.TotalFiles),
	})
}

// processOrganizeJob processes file organization for a completed scrape job
func processOrganizeJob(job *worker.BatchJob, mat *matcher.Matcher, destination string, copyOnly bool, cfg *config.Config) {
	// Initialize organizer, downloader, and NFO generator
	org := organizer.NewOrganizer(&cfg.Output)
	dl := downloader.NewDownloader(&cfg.Output, "Javinizer/1.0")
	nfoGen := nfo.NewGenerator(nfo.ConfigFromAppConfig(&cfg.Metadata.NFO))

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

		// Download artwork if file was moved
		if result.Moved && cfg.Output.DownloadPoster {
			if _, err := dl.DownloadPoster(movie, result.FolderPath); err != nil {
				logging.Errorf("Failed to download poster for %s: %v", movie.ID, err)
			}
		}

		if result.Moved && cfg.Output.DownloadCover {
			if _, err := dl.DownloadCover(movie, result.FolderPath); err != nil {
				logging.Errorf("Failed to download cover for %s: %v", movie.ID, err)
			}
		}

		// Download screenshots if enabled
		if result.Moved && cfg.Output.DownloadExtrafanart {
			if _, err := dl.DownloadExtrafanart(movie, result.FolderPath); err != nil {
				logging.Errorf("Failed to download screenshots for %s: %v", movie.ID, err)
			}
		}

		// Generate NFO file
		if result.Moved {
			// Determine part suffix for multi-part files (only if per_file is enabled)
			partSuffix := ""
			if cfg.Metadata.NFO.PerFile && match.IsMultiPart {
				partSuffix = match.PartSuffix
			}

			if err := nfoGen.Generate(movie, result.FolderPath, partSuffix); err != nil {
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
	templateEngine := template.NewEngine()

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

	// Build paths
	folderPath := filepath.Join(destination, folderName)
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
