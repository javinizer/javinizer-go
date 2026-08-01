package downloader

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

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

	// Check if poster already exists. An explicit manual crop overrides this
	// skip: the user just asked for a different poster, so the existing file
	// must be replaced instead of kept.
	if movie.Poster.CropBounds == nil {
		if info, err := d.fs.Stat(destPath); err == nil {
			// Already exists
			return &DownloadResult{
				Type:       MediaTypePoster,
				LocalPath:  destPath,
				Size:       info.Size(),
				Downloaded: false,
			}, nil
		}
	}

	// A manual crop recorded in the review UI takes priority: serialize the
	// whole download+crop per destination — concurrent multipart workers share
	// both the <dest>.full.tmp staging file and the destination itself.
	if b := movie.Poster.CropBounds; b != nil {
		unlock := d.acquirePosterCropLock(destPath)
		defer unlock()
		return d.downloadAndCropPoster(ctx, posterURL, destPath, func(srcPath, outPath string) error {
			cropBounds := b
			// The apply-time download can be the same image at a different
			// resolution than the crop preview measured: scale the absolute
			// rectangle so it tracks the same region.
			if b.ImageWidth > 0 && b.ImageHeight > 0 {
				if sw, sh, dimErr := imageutil.ImageDimensionsFromFile(d.fs, srcPath); dimErr == nil && (sw != b.ImageWidth || sh != b.ImageHeight) {
					cropBounds = scaleCropBounds(b, sw, sh)
				}
			}
			err := imageutil.CropPosterWithBounds(d.fs, srcPath, outPath, cropBounds.X, cropBounds.Y, cropBounds.X+cropBounds.Width, cropBounds.Y+cropBounds.Height, b.MaxPosterHeight)
			if err == nil {
				return nil
			}
			// Only geometry failures fall back; decode/I/O errors must fail the step.
			if !errors.Is(err, imageutil.ErrInvalidCropBounds) {
				return err
			}
			// The fallback intent belongs to the image the crop was measured
			// on (recorded at crop time), not the scrape-time baseline: a
			// poster-from-URL replacement must never degrade to an auto-crop.
			if !b.SourceWasCover {
				// No probe needed: geometry errors only surface after the image
				// decoded successfully (CropPosterWithBounds decodes before
				// validating bounds), so srcPath is known-good here.
				logging.Warnf("stored crop bounds %+v invalid for poster of %s: %v - saving image uncropped", *b, movie.ID, err)
				return d.fs.Rename(srcPath, outPath)
			}
			logging.Warnf("stored crop bounds %+v invalid for poster of %s: %v - falling back to default crop", *b, movie.ID, err)
			// Keep the crop-time max height: the preview the user approved was
			// produced with it, and the configured value may have changed since.
			return imageutil.CropPosterFromCover(d.fs, srcPath, outPath, b.MaxPosterHeight)
		})
	}

	// Check if we need to crop the poster or use it directly
	if !movie.Poster.ShouldCropPoster {
		// High-quality poster - download directly without cropping
		result, err := d.download(ctx, posterURL, destPath, MediaTypePoster)
		return result, err
	}

	// Low-quality poster - download and crop from cover
	return d.downloadAndCropPoster(ctx, posterURL, destPath, func(srcPath, outPath string) error {
		return imageutil.CropPosterFromCover(d.fs, srcPath, outPath, d.config.MaxPosterHeight)
	})
}

// posterCropLock is a reference-counted per-destination mutex.
type posterCropLock struct {
	mu   sync.Mutex
	refs int
}

// acquirePosterCropLock serializes poster-crop work on destPath and releases
// (evicting the map entry) when the last holder returns.
func (d *Downloader) acquirePosterCropLock(destPath string) func() {
	d.posterCropLocksGuard.Lock()
	v, _ := d.posterCropLocks.Load(destPath)
	entry, ok := v.(*posterCropLock)
	if !ok {
		entry = &posterCropLock{}
		d.posterCropLocks.Store(destPath, entry)
	}
	entry.refs++
	d.posterCropLocksGuard.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		d.posterCropLocksGuard.Lock()
		entry.refs--
		if entry.refs == 0 {
			d.posterCropLocks.Delete(destPath)
		}
		d.posterCropLocksGuard.Unlock()
	}
}

// scaleCropBounds rescales an absolute preview rectangle to a re-downloaded
// copy of the same image at a different resolution, preserving the region.
func scaleCropBounds(b *models.CropBounds, newW, newH int) *models.CropBounds {
	sx := float64(newW) / float64(b.ImageWidth)
	sy := float64(newH) / float64(b.ImageHeight)
	scaled := *b
	// Scale rectangle edges (not origin+size) so right/bottom-touching crops
	// cannot overflow the resized image through independent rounding.
	scaled.X = max(int(math.Round(float64(b.X)*sx)), 0)
	scaled.Y = max(int(math.Round(float64(b.Y)*sy)), 0)
	right := int(math.Round(float64(b.X+b.Width) * sx))
	bottom := int(math.Round(float64(b.Y+b.Height) * sy))
	scaled.Width = max(right-scaled.X, 1)
	scaled.Height = max(bottom-scaled.Y, 1)
	return &scaled
}

// downloadAndCropPoster downloads posterURL to a staging file, applies cropFn
// to produce a separate crop stage, and only then replaces destPath. Stale
// staging files from an interrupted run are cleared up front so they cannot be
// mistaken for a completed download; a failed crop never destroys a
// pre-existing poster at destPath.
func (d *Downloader) downloadAndCropPoster(ctx context.Context, posterURL, destPath string, cropFn func(srcPath, outPath string) error) (*DownloadResult, error) {
	tempPath := destPath + ".full.tmp"
	cropTmpPath := destPath + ".crop.tmp"
	backupPath := destPath + ".bak"
	// Recover a crashed previous replace FIRST: dest vanished after dest→.bak
	// but the install never landed, leaving the only copy of the old poster in
	// the backup. Any early failure below (download, crop) must find that old
	// poster back in place, not discover the slot permanently empty.
	result := &DownloadResult{URL: posterURL, LocalPath: destPath, Type: MediaTypePoster}
	if _, statErr := d.fs.Stat(destPath); errors.Is(statErr, os.ErrNotExist) {
		if _, bakErr := d.fs.Stat(backupPath); bakErr == nil {
			if recErr := d.fs.Rename(backupPath, destPath); recErr != nil {
				result.Error = fmt.Errorf("failed to recover interrupted poster backup %s: %w", backupPath, recErr)
				result.Downloaded = false
				return result, result.Error
			}
		}
	}
	// Stale staging files from an interrupted run must be cleared — if removal
	// itself fails (permissions, Windows file locks), download() would mistake
	// the stale stage for a completed download and silently skip the crop, so
	// surface that as an error instead.
	for _, stale := range []string{tempPath, cropTmpPath} {
		if rmErr := d.fs.Remove(stale); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			result.Error = fmt.Errorf("failed to clear stale poster staging %s: %w", stale, rmErr)
			result.Downloaded = false
			return result, result.Error
		}
	}

	dlResult, err := d.download(ctx, posterURL, tempPath, MediaTypePoster)
	if err != nil || !dlResult.Downloaded {
		_ = d.fs.Remove(tempPath) // Clean up if exists
		return dlResult, err
	}
	result = dlResult

	if err := cropFn(tempPath, cropTmpPath); err != nil {
		_ = d.fs.Remove(tempPath)
		_ = d.fs.Remove(cropTmpPath)
		result.Error = fmt.Errorf("failed to crop poster: %w", err)
		result.Downloaded = false
		return result, result.Error
	}
	_ = d.fs.Remove(tempPath)

	// Install only a complete image. Backup-and-rollback the existing
	// destination is staged aside first so a failed install rename restores
	// it instead of leaving the old poster destroyed. (Backup recovery for a
	// crashed previous replace runs before the download above.)
	_ = d.fs.Remove(backupPath)
	hadExisting := false
	if _, statErr := d.fs.Stat(destPath); statErr == nil {
		hadExisting = true
		if mvErr := d.fs.Rename(destPath, backupPath); mvErr != nil {
			_ = d.fs.Remove(cropTmpPath)
			result.Error = fmt.Errorf("failed to stage existing poster aside: %w", mvErr)
			result.Downloaded = false
			return result, result.Error
		}
	}
	if err := d.fs.Rename(cropTmpPath, destPath); err != nil {
		if hadExisting {
			_ = d.fs.Rename(backupPath, destPath)
		}
		_ = d.fs.Remove(cropTmpPath)
		result.Error = fmt.Errorf("failed to install cropped poster: %w", err)
		result.Downloaded = false
		return result, result.Error
	}
	if hadExisting {
		_ = d.fs.Remove(backupPath)
	}

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
