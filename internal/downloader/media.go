package downloader

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/imageutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

func (d *Downloader) downloadCover(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo, options ...any) (*DownloadResult, error) {
	overwriteExisting, dedup := resolveDownloadOptions(options)
	if !d.config.DownloadCover || movie.Poster.CoverURL == "" {
		return &DownloadResult{Type: MediaTypeCover, Downloaded: false}, nil
	}

	tmplCtx := d.buildTemplateContext(movie, multipart)
	destPath := d.pathResolver.ResolveFanartPath(movie, nil, true, tmplCtx, destDir)

	return d.download(ctx, movie.Poster.CoverURL, destPath, MediaTypeCover, overwriteExisting, dedup)
}

func (d *Downloader) downloadPoster(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo, options ...any) (finalResult *DownloadResult, finalErr error) {
	startTime := time.Now()
	overwriteExisting, dedup := resolveDownloadOptions(options)
	if !d.config.DownloadPoster {
		return &DownloadResult{Type: MediaTypePoster, Downloaded: false}, nil
	}

	posterURL := movie.Poster.PosterURL
	if posterURL == "" {
		posterURL = movie.Poster.CoverURL
	}
	if posterURL == "" {
		return &DownloadResult{Type: MediaTypePoster, Downloaded: false}, nil
	}

	tmplCtx := d.buildTemplateContext(movie, multipart)
	destPath := d.pathResolver.ResolvePosterPath(movie, nil, true, tmplCtx, destDir)

	bounds := movie.Poster.PosterCropBounds
	geometryUsable := bounds != nil && movie.Poster.PosterCropSourceFull && bounds.Valid()

	if !geometryUsable && !movie.Poster.ShouldCropPoster {
		return d.download(ctx, posterURL, destPath, MediaTypePoster, overwriteExisting, dedup)
	}

	result := &DownloadResult{
		URL:  posterURL,
		Type: MediaTypePoster,
	}
	var reservation *downloadReservation
	if overwriteExisting {
		var skipped bool
		var reservationErr error
		reservation, skipped, reservationErr = acquireDownloadReservation(ctx, dedup, destPath)
		if reservationErr != nil {
			result.Error = reservationErr
			result.Duration = time.Since(startTime)
			return result, result.Error
		}
		if skipped {
			result.Skipped = true
			result.Duration = time.Since(startTime)
			return result, nil
		}
		defer func() {
			finishDownloadReservation(dedup, destPath, reservation, finalErr == nil)
		}()
	}

	existed := false
	if overwriteExisting {
		_, err := d.fs.Stat(destPath)
		switch {
		case err == nil:
			existed = true
		case os.IsNotExist(err):
		default:
			result.Error = fmt.Errorf("failed to stat destination: %w", err)
			result.Duration = time.Since(startTime)
			return result, result.Error
		}
	} else if info, err := d.fs.Stat(destPath); err == nil {
		// Existing artwork is never replaced outside overwrite mode, even with
		// pending manual crop geometry: downloaded paths feed the revert
		// delete-list, so replacing here would leave NO poster after a revert.
		result.LocalPath = destPath
		result.Size = info.Size()
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// One GET feeds every poster-producing path below — manual crop, promote,
	// and auto crop all reuse the already-downloaded bytes, so a single-use or
	// signed poster URL cannot be consumed twice.
	fullPath := uniqueTempPath(destPath, "full.tmp")
	defer func() { _ = d.fs.Remove(fullPath) }()

	fullResult, err := d.download(ctx, posterURL, fullPath, MediaTypePoster, overwriteExisting, nil)
	fullResult.LocalPath = ""
	if err != nil || !fullResult.Downloaded {
		fullResult.Downloaded = false
		fullResult.Replaced = false
		fullResult.Duration = time.Since(startTime)
		return fullResult, err
	}

	cropPath := uniqueTempPath(destPath, "crop.tmp")
	defer func() { _ = d.fs.Remove(cropPath) }()

	candidate := fullPath
	cropped := false
	if geometryUsable && d.cropDownloadedPoster(fullPath, cropPath, bounds) {
		candidate = cropPath
		cropped = true
	}
	if !cropped && movie.Poster.ShouldCropPoster {
		if err := imageutil.CropPosterFromCover(d.fs, fullPath, cropPath, d.config.MaxPosterHeight); err != nil {
			fullResult.Error = fmt.Errorf("failed to crop poster: %w", err)
			fullResult.Downloaded = false
			fullResult.Replaced = false
			fullResult.LocalPath = ""
			fullResult.Duration = time.Since(startTime)
			return fullResult, fullResult.Error
		}
		candidate = cropPath
	}

	if overwriteExisting {
		if err := fsutil.ReplaceFile(d.fs, candidate, destPath); err != nil {
			fullResult.Error = fmt.Errorf("failed to replace poster: %w", err)
			fullResult.Downloaded = false
			fullResult.Replaced = false
			fullResult.LocalPath = ""
			fullResult.Duration = time.Since(startTime)
			return fullResult, fullResult.Error
		}
	} else if rerr := d.fs.Rename(candidate, destPath); rerr != nil {
		logging.Warnf("downloadPoster: failed to promote %s: %v", candidate, rerr)
		fullResult.Downloaded = false
		fullResult.Replaced = false
		fullResult.LocalPath = ""
		fullResult.Size = 0
		fullResult.Error = fmt.Errorf("failed to finalize poster: %w", rerr)
		fullResult.Duration = time.Since(startTime)
		return fullResult, fullResult.Error
	}

	fullResult.Downloaded = true
	fullResult.Replaced = existed
	d.finalizePosterResult(fullResult, destPath)
	fullResult.Duration = time.Since(startTime)
	return fullResult, nil
}

// finalizePosterResult points result at the promoted poster, or clears the
// location fields so a caller can never see a dangling (removed) temp path.
func (d *Downloader) finalizePosterResult(result *DownloadResult, destPath string) {
	result.LocalPath = ""
	result.Size = 0
	if info, err := d.fs.Stat(destPath); err == nil {
		result.LocalPath = destPath
		result.Size = info.Size()
	}
}

// cropDownloadedPoster applies the normalized review-page geometry to an
// already-downloaded full source image and writes the cropped poster to dst.
// Returns false when the geometry does not apply to this image (undecodable,
// aspect drift, empty rect); the caller then falls back to the pre-geometry
// behavior with the temp file still in place.
func (d *Downloader) cropDownloadedPoster(tempPath, dst string, bounds *models.CropBounds) bool {
	w, h, derr := imageutil.ImageDimensions(d.fs, tempPath)
	if derr != nil || w <= 0 || h <= 0 {
		logging.Warnf("downloadPoster: cannot decode downloaded source for manual crop: %v", derr)
		return false
	}

	// Aspect guard: the geometry was normalized against the review-time
	// source; if the downloaded image no longer matches that aspect, the
	// geometry targets a different image — refuse and fall back.
	if bounds.SourceAspect > 0 {
		got := float64(w) / float64(h)
		diff := math.Abs(got - bounds.SourceAspect)
		if diff > 0.01*bounds.SourceAspect {
			logging.Warnf("downloadPoster: manual crop aspect mismatch (crop %.4f, downloaded %.4f); falling back", bounds.SourceAspect, got)
			return false
		}
	}

	// Valid() guarantees unit-square containment, so no clamping is needed:
	// rounding stays within [0,w]×[0,h] (the 1e-9 tolerance cannot edge over a
	// pixel boundary) — only degenerate rounding needs a guard.
	fw, fh := float64(w), float64(h)
	left := int(math.Round(bounds.X * fw))
	top := int(math.Round(bounds.Y * fh))
	right := int(math.Round((bounds.X + bounds.Width) * fw))
	bottom := int(math.Round((bounds.Y + bounds.Height) * fh))
	if right <= left || bottom <= top {
		logging.Warnf("downloadPoster: manual crop geometry collapses to empty rect; falling back")
		return false
	}

	if err := imageutil.CropPosterWithBounds(d.fs, tempPath, dst, left, top, right, bottom, d.config.MaxPosterHeight); err != nil {
		logging.Warnf("downloadPoster: manual crop failed: %v", err)
		return false
	}
	return true
}

// downloadExtrafanart downloads screenshots to the extrafanart subdirectory.
// Extrafanart is used by media centers like Kodi/Plex for background images.
// Note: In the original Javinizer, screenshots and extrafanart are the same thing.
func (d *Downloader) downloadExtrafanart(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo, enabled bool, options ...any) ([]DownloadResult, error) {
	overwriteExisting, dedup := resolveDownloadOptions(options)
	if !enabled || len(movie.Screenshots) == 0 {
		return []DownloadResult{}, nil
	}

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
		result, err := d.download(ctx, url, destPath, MediaTypeExtrafanart, overwriteExisting, dedup)
		if err != nil {
			result.Error = err
		}
		results = append(results, *result)
	}

	return results, nil
}

func (d *Downloader) downloadTrailer(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo, options ...any) (*DownloadResult, error) {
	overwriteExisting, dedup := resolveDownloadOptions(options)
	if !d.config.DownloadTrailer || movie.TrailerURL == "" {
		return &DownloadResult{Type: MediaTypeTrailer, Downloaded: false}, nil
	}

	tmplCtx := d.buildTemplateContext(movie, multipart)
	destPath := d.pathResolver.ResolveTrailerPath(movie, true, tmplCtx, destDir)

	return d.download(ctx, movie.TrailerURL, destPath, MediaTypeTrailer, overwriteExisting, dedup)
}

func (d *Downloader) downloadActressImages(ctx context.Context, movie *models.Movie, destDir string, options ...any) ([]DownloadResult, error) {
	overwriteExisting, dedup := resolveDownloadOptions(options)
	if !d.config.DownloadActress || len(movie.Actresses) == 0 {
		return []DownloadResult{}, nil
	}

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

		formattedName := models.FormatActressName(actress, models.FormatActressNameOptions{
			JapaneseNames:      d.config.ActorJapaneseNames,
			FirstNameOrder:     d.config.ActorFirstNameOrder,
			UnknownActress:     d.config.UnknownActressText,
			UnknownActressMode: d.config.UnknownActressMode,
		})
		if formattedName == "" {
			continue
		}

		actressMovie := &models.Movie{ID: movie.ID}
		filename := d.generateActressFilename(actressMovie, formattedName, d.config.ActressFormat)
		if filename == "" {
			name := template.SanitizeFilename(formattedName)
			filename = fmt.Sprintf("%s.jpg", name)
		}
		destPath := filepath.Join(actressDir, filename)

		result, err := d.download(ctx, actress.ThumbURL, destPath, MediaTypeActress, overwriteExisting, dedup)
		if err != nil {
			result.Error = err
		}
		results = append(results, *result)
	}

	return results, nil
}

func (d *Downloader) downloadAllWithExtrafanart(ctx context.Context, movie *models.Movie, destDir string, multipart *MultipartInfo, extrafanartEnabled bool, options ...any) ([]DownloadResult, error) {
	overwriteExisting, dedup := resolveDownloadOptions(options)
	results := make([]DownloadResult, 0)
	criticalAttempted := 0
	criticalSucceeded := 0

	coverResult, _ := d.downloadCover(ctx, movie, destDir, multipart, overwriteExisting, dedup)
	if coverResult != nil {
		if coverResult.Error != nil {
			logging.Warnf("downloadAll: cover download failed for %s: %v", movie.ID, coverResult.Error)
		}
		if coverResult.Type == MediaTypeCover && d.config.DownloadCover && movie.Poster.CoverURL != "" && !coverResult.Skipped {
			criticalAttempted++
			if coverResult.Error == nil && coverResult.LocalPath != "" {
				criticalSucceeded++
			}
		}
		results = append(results, *coverResult)
	}

	posterResult, _ := d.downloadPoster(ctx, movie, destDir, multipart, overwriteExisting, dedup)
	if posterResult != nil {
		if posterResult.Error != nil {
			logging.Warnf("downloadAll: poster download failed for %s: %v", movie.ID, posterResult.Error)
		}
		if posterResult.Type == MediaTypePoster && d.config.DownloadPoster && !posterResult.Skipped {
			posterURL := movie.Poster.PosterURL
			if posterURL == "" {
				posterURL = movie.Poster.CoverURL
			}
			if posterURL != "" {
				criticalAttempted++
				if posterResult.Error == nil && posterResult.LocalPath != "" {
					criticalSucceeded++
				}
			}
		}
		results = append(results, *posterResult)
	}

	extrafanart, _ := d.downloadExtrafanart(ctx, movie, destDir, multipart, extrafanartEnabled, overwriteExisting, dedup)
	for i := range extrafanart {
		if extrafanart[i].Error != nil {
			logging.Warnf("downloadAll: extrafanart[%d] download failed for %s: %v", i, movie.ID, extrafanart[i].Error)
		}
	}
	results = append(results, extrafanart...)

	if trailerResult, _ := d.downloadTrailer(ctx, movie, destDir, multipart, overwriteExisting, dedup); trailerResult != nil {
		if trailerResult.Error != nil {
			logging.Warnf("downloadAll: trailer download failed for %s: %v", movie.ID, trailerResult.Error)
		}
		results = append(results, *trailerResult)
	}

	partNumber := 0
	if multipart != nil {
		partNumber = multipart.PartNumber
	}
	if partNumber == 0 || partNumber == 1 || overwriteExisting {
		actresses, err := d.downloadActressImages(ctx, movie, destDir, overwriteExisting, dedup)
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

	if criticalAttempted > 0 && criticalSucceeded == 0 {
		return results, &DownloadPartialError{Attempted: criticalAttempted, Succeeded: criticalSucceeded}
	}

	return results, nil
}
