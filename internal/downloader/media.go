package downloader

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

func (d *Downloader) downloadCover(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo) (*DownloadResult, error) {
	if !d.config.DownloadCover || movie.Poster.CoverURL == "" {
		return &DownloadResult{Type: MediaTypeCover, Downloaded: false}, nil
	}

	tmplCtx := d.buildTemplateContext(movie, multipart)
	destPath := d.pathResolver.ResolveFanartPath(movie, nil, true, tmplCtx, destDir)

	return d.download(ctx, movie.Poster.CoverURL, destPath, MediaTypeCover)
}

// downloadPoster downloads the movie poster
// If ShouldCropPoster is true, the poster is created by cropping the right 47.2% of the cover image
// If ShouldCropPoster is false, the poster is downloaded directly without cropping (high-quality poster)
func (d *Downloader) downloadPoster(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo) (*DownloadResult, error) {
	if !d.config.DownloadPoster {
		return &DownloadResult{Type: MediaTypePoster, Downloaded: false}, nil
	}

	// Use PosterURL if available, otherwise fall back to CoverURL
	posterURL := movie.Poster.PosterURL
	if posterURL == "" {
		posterURL = movie.Poster.CoverURL
	}
	if posterURL == "" {
		return &DownloadResult{Type: MediaTypePoster, Downloaded: false}, nil
	}

	tmplCtx := d.buildTemplateContext(movie, multipart)
	destPath := d.pathResolver.ResolvePosterPath(movie, nil, true, tmplCtx, destDir)

	// Check if poster already exists
	if info, err := d.fs.Stat(destPath); err == nil {
		// Already exists
		return &DownloadResult{
			Type:       MediaTypePoster,
			LocalPath:  destPath,
			Size:       info.Size(),
			Downloaded: false,
		}, nil
	}

	// Check if we need to crop the poster or use it directly
	if !movie.Poster.ShouldCropPoster {
		// High-quality poster - download directly without cropping
		result, err := d.download(ctx, posterURL, destPath, MediaTypePoster)
		return result, err
	}

	// Low-quality poster - download and crop from cover
	tempPath := destPath + ".full.tmp"
	result, err := d.download(ctx, posterURL, tempPath, MediaTypePoster)
	if err != nil || !result.Downloaded {
		_ = d.fs.Remove(tempPath) // Clean up if exists
		return result, err
	}

	// Crop the poster from the downloaded image
	if err := imageutil.CropPosterFromCover(d.fs, tempPath, destPath, d.config.MaxPosterHeight); err != nil {
		_ = d.fs.Remove(tempPath) // Clean up temp file
		result.Error = fmt.Errorf("failed to crop poster: %w", err)
		result.Downloaded = false
		return result, result.Error
	}

	// Clean up the temporary full image
	_ = d.fs.Remove(tempPath)

	// Update result with final path and size
	if info, err := d.fs.Stat(destPath); err == nil {
		result.LocalPath = destPath
		result.Size = info.Size()
	}

	return result, nil
}

// downloadExtrafanart downloads screenshots to the extrafanart subdirectory.
// Extrafanart is used by media centers like Kodi/Plex for background images.
// Note: In the original Javinizer, screenshots and extrafanart are the same thing.
func (d *Downloader) downloadExtrafanart(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo, enabled bool) ([]DownloadResult, error) {
	if !enabled || len(movie.Screenshots) == 0 {
		return []DownloadResult{}, nil
	}

	// Create extrafanart subdirectory using configurable folder name
	extrafanartDir := filepath.Join(destDir, d.config.ScreenshotFolder)

	tmplCtx := d.buildTemplateContext(movie, multipart)
	screenshotNames := d.pathResolver.ResolveScreenshotNames(movie, true, tmplCtx)

	results := make([]DownloadResult, 0, len(movie.Screenshots))

	for i, url := range movie.Screenshots {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		if i >= len(screenshotNames) {
			break
		}
		destPath := filepath.Join(extrafanartDir, screenshotNames[i])

		result, err := d.download(ctx, url, destPath, MediaTypeExtrafanart)
		if err != nil {
			result = &DownloadResult{
				URL:   url,
				Type:  MediaTypeExtrafanart,
				Error: err,
			}
		}
		results = append(results, *result)
	}

	return results, nil
}

// downloadTrailer downloads the movie trailer
func (d *Downloader) downloadTrailer(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo) (*DownloadResult, error) {
	if !d.config.DownloadTrailer || movie.TrailerURL == "" {
		return &DownloadResult{Type: MediaTypeTrailer, Downloaded: false}, nil
	}

	tmplCtx := d.buildTemplateContext(movie, multipart)
	destPath := d.pathResolver.ResolveTrailerPath(movie, true, tmplCtx, destDir)

	return d.download(ctx, movie.TrailerURL, destPath, MediaTypeTrailer)
}

// downloadActressImages downloads actress thumbnail images.
// Per-item download errors are captured in DownloadResult.Error fields rather than
// returned as a top-level error. The caller should inspect individual results
// for failures. A top-level error is only returned for context cancellation.
func (d *Downloader) downloadActressImages(ctx context.Context, movie *models.Movie, destDir string) ([]DownloadResult, error) {
	if !d.config.DownloadActress || len(movie.Actresses) == 0 {
		return []DownloadResult{}, nil
	}

	// Create actress subdirectory using configurable folder name
	actressDir := filepath.Join(destDir, d.config.ActressFolder)

	results := make([]DownloadResult, 0)

	for _, actress := range movie.Actresses {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		if actress.ThumbURL == "" {
			continue
		}

		// Format actress name according to NFO settings (Japanese vs English)
		formattedName := models.FormatActressName(actress, models.FormatActressNameOptions{
			JapaneseNames:      d.config.ActorJapaneseNames,
			FirstNameOrder:     d.config.ActorFirstNameOrder,
			UnknownActress:     d.config.UnknownActressText,
			UnknownActressMode: d.config.UnknownActressMode,
		})
		if formattedName == "" {
			continue
		}

		// Use configurable template for actress filenames
		// Create a temporary movie with actress data for template processing
		actressMovie := &models.Movie{
			ID: movie.ID,
		}

		filename := d.generateActressFilename(actressMovie, formattedName, d.config.ActressFormat)
		if filename == "" {
			// Fallback to default format
			name := template.SanitizeFilename(formattedName)
			filename = fmt.Sprintf("%s.jpg", name)
		}
		destPath := filepath.Join(actressDir, filename)

		result, err := d.download(ctx, actress.ThumbURL, destPath, MediaTypeActress)
		if err != nil {
			result = &DownloadResult{
				URL:   actress.ThumbURL,
				Type:  MediaTypeActress,
				Error: err,
			}
		}
		results = append(results, *result)
	}

	return results, nil
}

// downloadAllWithExtrafanart is like downloadAll but accepts an explicit extrafanart flag.
// This avoids mutating the shared Config struct when the TUI needs to toggle extrafanart at runtime.
func (d *Downloader) downloadAllWithExtrafanart(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo, extrafanartEnabled bool) ([]DownloadResult, error) {
	results := make([]DownloadResult, 0)

	// Track critical media (cover + poster) to detect partial-download-failure.
	// If both cover and poster are attempted but neither succeeds, return a
	// DownloadPartialError sentinel; the apply orchestrator treats it as
	// non-fatal (logs the failure, preserves non-critical artifacts for revert
	// cleanup, and proceeds to NFO generation per the project's NFO guarantee).
	criticalAttempted := 0
	criticalSucceeded := 0

	// Download cover (fanart)
	// Note: Each download method has a file-exists check, so if templates produce
	// the same filename for different parts, the file won't be re-downloaded.
	// If templates use <IF:MULTIPART> or <PART>, each part gets its own file.
	coverResult, _ := d.downloadCover(ctx, movie, destDir, multipart)
	if coverResult != nil {
		if coverResult.Error != nil {
			logging.Warnf("downloadAll: cover download failed for %s: %v", movie.ID, coverResult.Error)
		}
		if coverResult.Type == MediaTypeCover {
			// Only count as attempted if cover downloading is enabled and URL was present
			if d.config.DownloadCover && movie.Poster.CoverURL != "" {
				criticalAttempted++
				// File exists on disk = success (whether newly downloaded or already present)
				if coverResult.Error == nil && coverResult.LocalPath != "" {
					criticalSucceeded++
				}
			}
		}
		results = append(results, *coverResult)
	}

	// Download poster
	posterResult, _ := d.downloadPoster(ctx, movie, destDir, multipart)
	if posterResult != nil {
		if posterResult.Error != nil {
			logging.Warnf("downloadAll: poster download failed for %s: %v", movie.ID, posterResult.Error)
		}
		if posterResult.Type == MediaTypePoster {
			if d.config.DownloadPoster {
				posterURL := movie.Poster.PosterURL
				if posterURL == "" {
					posterURL = movie.Poster.CoverURL
				}
				if posterURL != "" {
					criticalAttempted++
					// File exists on disk = success (whether newly downloaded or already present)
					if posterResult.Error == nil && posterResult.LocalPath != "" {
						criticalSucceeded++
					}
				}
			}
		}
		results = append(results, *posterResult)
	}

	// Download extrafanart (screenshots)
	extrafanart, _ := d.downloadExtrafanart(ctx, movie, destDir, multipart, extrafanartEnabled)
	for i := range extrafanart {
		if extrafanart[i].Error != nil {
			logging.Warnf("downloadAll: extrafanart[%d] download failed for %s: %v", i, movie.ID, extrafanart[i].Error)
		}
	}
	results = append(results, extrafanart...)

	// Download trailer
	if trailerResult, _ := d.downloadTrailer(ctx, movie, destDir, multipart); trailerResult != nil {
		if trailerResult.Error != nil {
			logging.Warnf("downloadAll: trailer download failed for %s: %v", movie.ID, trailerResult.Error)
		}
		results = append(results, *trailerResult)
	}

	// Download actress images (doesn't use multipart - shared across all parts)
	// Only download for single files or first part to avoid duplicate downloads
	partNumber := 0
	if multipart != nil {
		partNumber = multipart.PartNumber
	}
	if partNumber == 0 || partNumber == 1 {
		actresses, err := d.downloadActressImages(ctx, movie, destDir)
		if err != nil {
			logging.Warnf("downloadAll: actress image download aborted for %s: %v", movie.ID, err)
		}
		for i := range actresses {
			if actresses[i].Error != nil {
				logging.Warnf("downloadAll: actress image download failed for %s: %v", movie.ID, actresses[i].Error)
			}
		}
		results = append(results, actresses...)
	}

	// Return partial-error sentinel when all critical media (cover+poster) failed.
	// The apply orchestrator treats this as non-fatal: it logs the failure,
	// preserves any non-critical artifacts that did download (for revert
	// cleanup), and proceeds to NFO generation — the project guarantee is that
	// a correct NFO is produced regardless of artwork availability.
	if criticalAttempted > 0 && criticalSucceeded == 0 {
		return results, &DownloadPartialError{
			Attempted: criticalAttempted,
			Succeeded: criticalSucceeded,
		}
	}

	return results, nil
}
