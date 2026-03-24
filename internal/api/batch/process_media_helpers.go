package batch

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/worker"
)

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
	if !cfg.Output.DownloadPoster {
		logging.Debugf("%s mode: Poster download disabled, skipping temp poster copy for %s", mode, movie.ID)
		return false
	}

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
		logging.Warnf("[post-move] mode=%s movie=%s stage=temp_poster_copy src=%s dst=%s err=%v", mode, movie.ID, tempPosterPath, destPosterPath, err)
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

	failed := 0
	downloaded := 0
	skipped := 0
	for _, result := range results {
		if result.Downloaded {
			downloaded++
			logging.Infof("Downloaded %s: %s (%d bytes)", result.Type, result.LocalPath, result.Size)
		}
		if result.Error != nil {
			failed++
			logging.Warnf("[post-move] mode=Organize movie=%s stage=download media_type=%s url=%s dst=%s err=%v", movie.ID, result.Type, result.URL, result.LocalPath, result.Error)
		}
		if !result.Downloaded && result.Error == nil {
			skipped++
		}
		// Log download to history (both successful and failed, skip if no URL)
		if result.URL != "" {
			if logErr := historyLogger.LogDownload(movie.ID, result.URL, result.LocalPath, string(result.Type), result.Error); logErr != nil {
				logging.Warnf("Failed to log download history for %s: %v", movie.ID, logErr)
			}
		}
	}

	if failed > 0 {
		logging.Warnf("[post-move] mode=Organize movie=%s stage=download_summary total=%d downloaded=%d skipped=%d failed=%d dest_dir=%s", movie.ID, len(results), downloaded, skipped, failed, destDir)
	}
}
