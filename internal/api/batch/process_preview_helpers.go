package batch

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/worker"
)

func generateNFOPaths(movie *models.Movie, fileResults []*worker.FileResult, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine, fileName string, folderPath string) (string, []string) {
	if !cfg.Metadata.NFO.Enabled {
		return "", nil
	}

	isMultiPart := false
	for _, result := range fileResults {
		if result != nil && result.IsMultiPart {
			isMultiPart = true
			break
		}
	}
	generatePerFileNFO := cfg.Metadata.NFO.PerFile && isMultiPart

	var nfoPath string
	var nfoPaths []string

	if generatePerFileNFO {
		nfoPaths = make([]string, 0, len(fileResults))
		for _, result := range fileResults {
			if result != nil && result.FilePath != "" {
				nfoCtx := ctx.Clone()
				nfoCtx.PartNumber = result.PartNumber
				nfoCtx.PartSuffix = result.PartSuffix
				nfoCtx.IsMultiPart = result.IsMultiPart

				nfoFileName, err := templateEngine.Execute(cfg.Metadata.NFO.FilenameTemplate, nfoCtx)
				if err != nil || nfoFileName == "" {
					sanitized := template.SanitizeFilename(movie.ID)
					if sanitized == "" {
						sanitized = "metadata"
					}
					nfoFilePath := previewJoinPath(folderPath, sanitized+".nfo")
					nfoPaths = append(nfoPaths, nfoFilePath)
					continue
				}

				basename := nfoFileName
				lower := strings.ToLower(basename)
				if strings.HasSuffix(lower, ".nfo") {
					basename = basename[:len(basename)-4]
				}
				sanitized := template.SanitizeFilename(basename)

				if sanitized == "" {
					sanitized = template.SanitizeFilename(movie.ID)
					if sanitized == "" {
						sanitized = "metadata"
					}
				}

				if result.PartSuffix != "" && generatePerFileNFO {
					sanitized += result.PartSuffix
				}

				nfoFilePath := previewJoinPath(folderPath, sanitized+".nfo")
				nfoPaths = append(nfoPaths, nfoFilePath)
			}
		}
		if len(nfoPaths) > 0 {
			nfoPath = nfoPaths[0]
		}
	} else {
		nfoFileName, err := templateEngine.Execute(cfg.Metadata.NFO.FilenameTemplate, ctx)
		if err != nil || nfoFileName == "" {
			sanitized := template.SanitizeFilename(movie.ID)
			if sanitized == "" {
				sanitized = "metadata"
			}
			nfoFileName = sanitized + ".nfo"
		} else {
			basename := nfoFileName
			lower := strings.ToLower(basename)
			if strings.HasSuffix(lower, ".nfo") {
				basename = basename[:len(basename)-4]
			}
			sanitized := template.SanitizeFilename(basename)

			if sanitized == "" {
				sanitized = template.SanitizeFilename(movie.ID)
				if sanitized == "" {
					sanitized = "metadata"
				}
			}

			nfoFileName = sanitized + ".nfo"
		}
		nfoPath = previewJoinPath(folderPath, nfoFileName)
	}

	return nfoPath, nfoPaths
}

func generatePosterPath(movie *models.Movie, fileResults []*worker.FileResult, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine, folderPath string) string {
	if !cfg.Output.DownloadPoster {
		return ""
	}

	posterCtx := ctx.Clone()
	if first := firstValidFileResult(fileResults); first != nil {
		posterCtx.PartNumber = first.PartNumber
		posterCtx.PartSuffix = first.PartSuffix
		posterCtx.IsMultiPart = first.IsMultiPart
	}
	posterFileName, err := templateEngine.Execute(cfg.Output.PosterFormat, posterCtx)
	if err != nil || posterFileName == "" {
		posterFileName = fmt.Sprintf("%s-poster.jpg", movie.ID)
	}
	posterFileName = template.SanitizeFilename(posterFileName)
	if posterFileName == "" {
		sanitizedID := template.SanitizeFilename(movie.ID)
		if sanitizedID == "" {
			sanitizedID = "unknown"
		}
		posterFileName = fmt.Sprintf("%s-poster.jpg", sanitizedID)
	}
	return previewJoinPath(folderPath, posterFileName)
}

func generateFanartPath(movie *models.Movie, fileResults []*worker.FileResult, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine, folderPath string) string {
	if !cfg.Output.DownloadExtrafanart {
		return ""
	}

	fanartCtx := ctx.Clone()
	if first := firstValidFileResult(fileResults); first != nil {
		fanartCtx.PartNumber = first.PartNumber
		fanartCtx.PartSuffix = first.PartSuffix
		fanartCtx.IsMultiPart = first.IsMultiPart
	}
	fanartFileName, err := templateEngine.Execute(cfg.Output.FanartFormat, fanartCtx)
	if err != nil || fanartFileName == "" {
		fanartFileName = fmt.Sprintf("%s-fanart.jpg", movie.ID)
	}
	fanartFileName = template.SanitizeFilename(fanartFileName)
	if fanartFileName == "" {
		sanitizedID := template.SanitizeFilename(movie.ID)
		if sanitizedID == "" {
			sanitizedID = "unknown"
		}
		fanartFileName = fmt.Sprintf("%s-fanart.jpg", sanitizedID)
	}
	return previewJoinPath(folderPath, fanartFileName)
}

func generateScreenshotNames(movie *models.Movie, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine) []string {
	screenshots := []string{}
	if !cfg.Output.DownloadExtrafanart || len(movie.Screenshots) == 0 {
		return screenshots
	}

	for i := range movie.Screenshots {
		ctx.Index = i + 1
		screenshotName, err := templateEngine.Execute(cfg.Output.ScreenshotFormat, ctx)
		if err != nil || screenshotName == "" {
			if cfg.Output.ScreenshotPadding > 0 {
				screenshotName = fmt.Sprintf("fanart%0*d.jpg", cfg.Output.ScreenshotPadding, i+1)
			} else {
				screenshotName = fmt.Sprintf("fanart%d.jpg", i+1)
			}
		}
		screenshotName = template.SanitizeFilename(screenshotName)
		if screenshotName == "" {
			if cfg.Output.ScreenshotPadding > 0 {
				screenshotName = fmt.Sprintf("fanart%0*d.jpg", cfg.Output.ScreenshotPadding, i+1)
			} else {
				screenshotName = fmt.Sprintf("fanart%d.jpg", i+1)
			}
		}
		screenshots = append(screenshots, screenshotName)
	}
	return screenshots
}

func validatePathLengths(cfg *config.Config, templateEngine *template.Engine, videoFiles []string, nfoPath string, nfoPaths []string, posterPath string, fanartPath string, extrafanartPath string, screenshots []string) {
	if cfg.Output.MaxPathLength <= 0 {
		return
	}

	for _, videoPath := range videoFiles {
		if err := templateEngine.ValidatePathLength(videoPath, cfg.Output.MaxPathLength); err != nil {
			logging.Warnf("Preview: video path exceeds max length: %s (length: %d, max: %d)", videoPath, len(videoPath), cfg.Output.MaxPathLength)
		}
	}
	if nfoPath != "" {
		if err := templateEngine.ValidatePathLength(nfoPath, cfg.Output.MaxPathLength); err != nil {
			logging.Warnf("Preview: NFO path exceeds max length: %s (length: %d, max: %d)", nfoPath, len(nfoPath), cfg.Output.MaxPathLength)
		}
	}
	for _, nfoFilePath := range nfoPaths {
		if err := templateEngine.ValidatePathLength(nfoFilePath, cfg.Output.MaxPathLength); err != nil {
			logging.Warnf("Preview: NFO path exceeds max length: %s (length: %d, max: %d)", nfoFilePath, len(nfoFilePath), cfg.Output.MaxPathLength)
		}
	}
	if err := templateEngine.ValidatePathLength(posterPath, cfg.Output.MaxPathLength); err != nil {
		logging.Warnf("Preview: poster path exceeds max length: %s (length: %d, max: %d)", posterPath, len(posterPath), cfg.Output.MaxPathLength)
	}
	if err := templateEngine.ValidatePathLength(fanartPath, cfg.Output.MaxPathLength); err != nil {
		logging.Warnf("Preview: fanart path exceeds max length: %s (length: %d, max: %d)", fanartPath, len(fanartPath), cfg.Output.MaxPathLength)
	}
	for _, screenshot := range screenshots {
		screenshotPath := previewJoinPath(extrafanartPath, screenshot)
		if err := templateEngine.ValidatePathLength(screenshotPath, cfg.Output.MaxPathLength); err != nil {
			logging.Warnf("Preview: screenshot path exceeds max length: %s (length: %d, max: %d)", screenshotPath, len(screenshotPath), cfg.Output.MaxPathLength)
		}
	}
}

func firstValidFileResult(fileResults []*worker.FileResult) *worker.FileResult {
	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			return result
		}
	}
	return nil
}

func previewPathBase(path string) string {
	trimmed := trimPreviewPath(path)
	if trimmed == "" {
		return ""
	}

	idx := strings.LastIndexAny(trimmed, `/\`)
	if idx == -1 {
		return trimmed
	}

	return trimmed[idx+1:]
}

func previewPathDir(path string) string {
	trimmed := trimPreviewPath(path)
	if trimmed == "" {
		return "."
	}

	idx := strings.LastIndexAny(trimmed, `/\`)
	if idx == -1 {
		return "."
	}

	switch {
	case idx == 0:
		return trimmed[:1]
	case idx == 2 && len(trimmed) >= 3 && trimmed[1] == ':' && (trimmed[2] == '\\' || trimmed[2] == '/'):
		return trimmed[:3]
	default:
		dir := trimmed[:idx]
		if isUNCPath(trimmed) {
			uncRoot := uncShareRoot(trimmed)
			if uncRoot != "" && len(dir) < len(uncRoot) {
				return uncRoot
			}
		}
		return dir
	}
}

func isUNCPath(path string) bool {
	return strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, `//`)
}

func uncShareRoot(path string) string {
	if !isUNCPath(path) {
		return ""
	}
	rest := path[2:]
	idx := strings.IndexAny(rest, `/\`)
	if idx == -1 {
		return path
	}
	shareRest := rest[idx+1:]
	shareEnd := strings.IndexAny(shareRest, `/\`)
	if shareEnd == -1 {
		return path
	}
	return path[:2+idx+1+shareEnd]
}

func previewPathExt(path string) string {
	return filepath.Ext(previewPathBase(path))
}

func previewJoinPath(base string, elems ...string) string {
	if base == "" {
		return filepath.Join(elems...)
	}

	windowsStyle := isWindowsPathLike(base) || strings.Contains(base, `\`)
	if !windowsStyle {
		parts := make([]string, 0, len(elems)+1)
		parts = append(parts, base)
		parts = append(parts, elems...)
		return filepath.Join(parts...)
	}

	joined := strings.ReplaceAll(base, "/", `\`)
	joined = trimPreviewPath(joined)

	for _, elem := range elems {
		clean := strings.Trim(elem, `/\`)
		if clean == "" {
			continue
		}

		clean = strings.ReplaceAll(clean, "/", `\`)

		switch {
		case joined == "", joined == ".":
			joined = clean
		case joined == `\` || (len(joined) == 3 && joined[1] == ':' && joined[2] == '\\'):
			if strings.HasSuffix(joined, `\`) {
				joined += clean
			} else {
				joined += `\` + clean
			}
		default:
			joined += `\` + clean
		}
	}

	return joined
}

func trimPreviewPath(path string) string {
	switch {
	case path == "", path == "/", path == `\`:
		return path
	case len(path) == 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/'):
		return path
	default:
		return strings.TrimRight(path, `/\`)
	}
}

func isWindowsPathLike(path string) bool {
	return (len(path) >= 2 && path[1] == ':') || strings.HasPrefix(path, `\\`)
}
