package downloader

import (
	"fmt"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

// MediaPathResolver computes media file paths (poster, fanart, trailer, screenshots)
// without downloading. Both the preview orchestrator and the downloader use this
// to ensure consistent naming: template → fallback → sanitize → join.
type MediaPathResolver struct {
	cfg    organizer.MediaFormatConfig
	engine template.EngineInterface
}

// NewMediaPathResolver creates a resolver that computes media file paths
// using the given format config and template engine.
func NewMediaPathResolver(cfg organizer.MediaFormatConfig, engine template.EngineInterface) *MediaPathResolver {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &MediaPathResolver{cfg: cfg, engine: engine}
}

// resolveMediaPath computes a media filename using the template → fallback →
// sanitize → second-fallback pattern shared by poster, fanart, and trailer
// resolution. If the template format is empty or fails, it falls back to
// "{id}-{fallbackSuffix}" with the given extension. After sanitization, if
// the result is still empty, it uses a sanitized ID as the base.
//
// If templateExt is non-empty and the template produced a result that lacks a
// file extension, the extension is appended. This handles the trailer case
// where the template may produce a basename without the correct extension.
func (r *MediaPathResolver) resolveMediaPath(format string, ctx *template.Context, fallbackID string, fallbackSuffix string, fallbackExt string, templateExt string) string {
	fileName, err := r.engine.Execute(format, ctx)
	templateProduced := err == nil && fileName != ""

	if !templateProduced {
		fileName = fmt.Sprintf("%s-%s%s", fallbackID, fallbackSuffix, fallbackExt)
	}
	fileName = template.SanitizeFilename(fileName)
	if fileName == "" {
		sanitizedID := template.SanitizeFilename(fallbackID)
		if sanitizedID == "" {
			sanitizedID = "unknown"
		}
		fileName = fmt.Sprintf("%s-%s%s", sanitizedID, fallbackSuffix, fallbackExt)
	}

	// If the template produced the filename and it lacks an extension, append one.
	if templateProduced && templateExt != "" && filepath.Ext(fileName) == "" {
		fileName += templateExt
	}

	return fileName
}

// joinPath joins the filename with the folder path, returning just the
// filename if folderPath is empty.
func joinPath(folderPath string, fileName string) string {
	if folderPath == "" {
		return fileName
	}
	return filepath.Join(folderPath, fileName)
}

// ResolvePosterPath computes the poster filename and joins it with folderPath.
// Returns empty string if downloadEnabled is false.
// If fileResults contains a valid entry, its multipart info overrides ctx.
func (r *MediaPathResolver) ResolvePosterPath(movie *models.Movie, fileResults []models.FileMatchInfo, downloadEnabled bool, ctx *template.Context, folderPath string) string {
	if !downloadEnabled {
		return ""
	}

	posterCtx := ctx.Clone()
	if first := models.FirstValidFileResult(fileResults); first != nil {
		posterCtx.PartNumber = first.PartNumber
		posterCtx.PartSuffix = first.PartSuffix
		posterCtx.IsMultiPart = first.IsMultiPart
	}

	fileName := r.resolveMediaPath(r.cfg.PosterFormat, posterCtx, movie.ID, "poster", ".jpg", "")
	return joinPath(folderPath, fileName)
}

// ResolveFanartPath computes the fanart/cover filename and joins it with folderPath.
// Returns empty string if downloadEnabled is false.
// If fileResults contains a valid entry, its multipart info overrides ctx.
func (r *MediaPathResolver) ResolveFanartPath(movie *models.Movie, fileResults []models.FileMatchInfo, downloadEnabled bool, ctx *template.Context, folderPath string) string {
	if !downloadEnabled {
		return ""
	}

	fanartCtx := ctx.Clone()
	if first := models.FirstValidFileResult(fileResults); first != nil {
		fanartCtx.PartNumber = first.PartNumber
		fanartCtx.PartSuffix = first.PartSuffix
		fanartCtx.IsMultiPart = first.IsMultiPart
	}

	fileName := r.resolveMediaPath(r.cfg.FanartFormat, fanartCtx, movie.ID, "fanart", ".jpg", "")
	return joinPath(folderPath, fileName)
}

// ResolveTrailerPath computes the trailer filename and joins it with folderPath.
// Returns empty string if downloadEnabled is false or the movie has no trailer URL.
// The file extension is derived from the trailer URL (defaulting to .mp4).
func (r *MediaPathResolver) ResolveTrailerPath(movie *models.Movie, downloadEnabled bool, ctx *template.Context, folderPath string) string {
	if !downloadEnabled {
		return ""
	}
	if movie == nil || movie.TrailerURL == "" {
		return ""
	}

	// Determine extension from URL, default to .mp4
	ext := filepath.Ext(movie.TrailerURL)
	if ext == "" {
		ext = ".mp4"
	}

	trailerCtx := ctx.Clone()
	// templateExt=ext: if the template produces a filename without extension, append it
	fileName := r.resolveMediaPath(r.cfg.TrailerFormat, trailerCtx, movie.ID, "trailer", ext, ext)
	return joinPath(folderPath, fileName)
}

// ResolveScreenshotNames computes screenshot filenames (without directory).
// Returns empty slice if downloadEnabled is false or the movie has no screenshots.
func (r *MediaPathResolver) ResolveScreenshotNames(movie *models.Movie, downloadEnabled bool, ctx *template.Context) []string {
	screenshots := []string{}
	if !downloadEnabled || len(movie.Screenshots) == 0 {
		return screenshots
	}

	for i := range movie.Screenshots {
		shotCtx := ctx.Clone()
		shotCtx.Index = i + 1
		screenshotName, err := r.engine.Execute(r.cfg.ScreenshotFormat, shotCtx)
		if err != nil || screenshotName == "" {
			if r.cfg.ScreenshotPadding > 0 {
				screenshotName = fmt.Sprintf("fanart%0*d.jpg", r.cfg.ScreenshotPadding, i+1)
			} else {
				screenshotName = fmt.Sprintf("fanart%d.jpg", i+1)
			}
		}
		screenshotName = template.SanitizeFilename(screenshotName)
		if screenshotName == "" {
			if r.cfg.ScreenshotPadding > 0 {
				screenshotName = fmt.Sprintf("fanart%0*d.jpg", r.cfg.ScreenshotPadding, i+1)
			} else {
				screenshotName = fmt.Sprintf("fanart%d.jpg", i+1)
			}
		}
		screenshots = append(screenshots, screenshotName)
	}
	return screenshots
}
