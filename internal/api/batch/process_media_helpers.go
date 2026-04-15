package batch

import (
	"context"
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
func cleanupJobTempPosters(jobID string, tempDir string) {
	posterDir := filepath.Join(tempDir, "posters", jobID)
	if err := os.RemoveAll(posterDir); err != nil {
		logging.Warnf("[Job %s] Failed to clean temp poster dir: %v", jobID, err)
	} else {
		logging.Debugf("[Job %s] Cleaned up temporary poster directory: %s", jobID, posterDir)
	}
}

// copyTempCroppedPoster copies the temp cropped poster to the destination directory
// Returns true if copy was successful, false otherwise
// multipart can be nil for single-file operations
func copyTempCroppedPoster(job *worker.BatchJob, movie *models.Movie, destDir string, cfg *config.Config, mode string, multipart *downloader.MultipartInfo) string {
	if !cfg.Output.DownloadPoster {
		logging.Debugf("%s mode: Poster download disabled, skipping temp poster copy for %s", mode, movie.ID)
		return ""
	}

	tempPosterPath := filepath.Join(cfg.System.TempDir, "posters", job.ID, movie.ID+".jpg")
	if _, err := os.Stat(tempPosterPath); err != nil {
		return ""
	}

	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = cfg.Output.GroupActress
	if multipart != nil {
		ctx.IsMultiPart = multipart.IsMultiPart
		ctx.PartNumber = multipart.PartNumber
		ctx.PartSuffix = multipart.PartSuffix
	}
	engine := job.TemplateEngine()
	posterFilename, err := engine.Execute(cfg.Output.PosterFormat, ctx)
	if err != nil {
		posterFilename = fmt.Sprintf("%s-poster.jpg", movie.ID)
		logging.Warnf("%s mode: Template execution failed, using fallback filename: %v", mode, err)
	}

	posterFilename = template.SanitizeFilename(posterFilename)
	if posterFilename == "" {
		posterFilename = fmt.Sprintf("%s-poster.jpg", template.SanitizeFilename(movie.ID))
	}

	destPosterPath := filepath.Join(destDir, posterFilename)

	if err := fsutil.CopyFileAtomic(tempPosterPath, destPosterPath); err != nil {
		logging.Warnf("[post-move] mode=%s movie=%s stage=temp_poster_copy src=%s dst=%s err=%v", mode, movie.ID, tempPosterPath, destPosterPath, err)
		return ""
	}

	logging.Infof("%s mode: Copied cropped poster from temp to %s", mode, destPosterPath)
	return destPosterPath
}

// downloadMediaFilesWithHistory downloads all configured media files and logs to history
// multipart can be nil for single-file operations
func downloadMediaFilesWithHistory(ctx context.Context, dl *downloader.Downloader, movie *models.Movie, destDir string, cfg *config.Config, historyLogger *history.Logger, multipart *downloader.MultipartInfo) []string {
	results, err := dl.DownloadAll(ctx, movie, destDir, multipart)
	if err != nil {
		logging.Errorf("Failed to download media for %s: %v", movie.ID, err)
		return nil
	}

	var downloadPaths []string
	failed := 0
	downloaded := 0
	skipped := 0
	for _, result := range results {
		if result.Downloaded {
			downloaded++
			logging.Infof("Downloaded %s: %s (%d bytes)", result.Type, result.LocalPath, result.Size)
			if result.LocalPath != "" {
				downloadPaths = append(downloadPaths, result.LocalPath)
			}
		}
		if result.Error != nil {
			failed++
			logging.Warnf("[post-move] mode=Organize movie=%s stage=download media_type=%s url=%s dst=%s err=%v", movie.ID, result.Type, result.URL, result.LocalPath, result.Error)
		}
		if !result.Downloaded && result.Error == nil {
			skipped++
		}
		if result.URL != "" {
			if logErr := historyLogger.LogDownload(movie.ID, result.URL, result.LocalPath, string(result.Type), result.Error); logErr != nil {
				logging.Warnf("Failed to log download history for %s: %v", movie.ID, logErr)
			}
		}
	}

	if failed > 0 {
		logging.Warnf("[post-move] mode=Organize movie=%s stage=download_summary total=%d downloaded=%d skipped=%d failed=%d dest_dir=%s", movie.ID, len(results), downloaded, skipped, failed, destDir)
	}

	return downloadPaths
}
