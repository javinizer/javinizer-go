package downloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

	if !movie.Poster.ShouldCropPoster {
		return d.download(ctx, posterURL, destPath, MediaTypePoster, overwriteExisting, dedup)
	}

	result := &DownloadResult{
		URL:  posterURL,
		Type: MediaTypePoster,
	}
	if overwriteExisting && dedup != nil {
		if _, loaded := dedup.LoadOrStore(destPath, struct{}{}); loaded {
			result.Skipped = true
			result.Duration = time.Since(startTime)
			return result, nil
		}
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
		result.LocalPath = destPath
		result.Size = info.Size()
		result.Duration = time.Since(startTime)
		return result, nil
	}

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

	if err := imageutil.CropPosterFromCover(d.fs, fullPath, cropPath, d.config.MaxPosterHeight); err != nil {
		fullResult.Error = fmt.Errorf("failed to crop poster: %w", err)
		fullResult.Downloaded = false
		fullResult.Replaced = false
		fullResult.LocalPath = ""
		fullResult.Duration = time.Since(startTime)
		return fullResult, fullResult.Error
	}

	if err := replaceFile(d.fs, cropPath, destPath); err != nil {
		fullResult.Error = fmt.Errorf("failed to replace poster: %w", err)
		fullResult.Downloaded = false
		fullResult.Replaced = false
		fullResult.LocalPath = ""
		fullResult.Duration = time.Since(startTime)
		return fullResult, fullResult.Error
	}

	fullResult.LocalPath = destPath
	fullResult.Downloaded = true
	fullResult.Replaced = existed
	if info, statErr := d.fs.Stat(destPath); statErr == nil {
		fullResult.Size = info.Size()
	}
	fullResult.Duration = time.Since(startTime)
	return fullResult, nil
}

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
