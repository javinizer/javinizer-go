package batch

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/types"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// generatePreview generates an organize preview response for a movie
// fileResults contains all file results for this movie (to support multi-part files)
func generatePreview(movie *models.Movie, fileResults []*worker.FileResult, destination string, cfg *config.Config, operationMode organizer.OperationMode, skipNFO bool, skipDownload bool) OrganizePreviewResponse {
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = cfg.Output.GroupActress
	templateEngine := template.NewEngine()

	fileName, err := templateEngine.Execute(cfg.Output.FileFormat, ctx)
	if err != nil {
		logging.Errorf("Failed to generate file name: %v", err)
		fileName = "error"
	}
	fileName = template.SanitizeFilename(fileName)
	if fileName == "" {
		fileName = resolvePreviewFallbackName(movie, fileResults)
	}

	sourcePath := ""
	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			sourcePath = result.FilePath
			break
		}
	}

	if operationMode == types.OperationModeInPlaceNoRenameFolder {
		return generateInPlaceNoRenameFolderPreview(movie, fileResults, cfg, ctx, templateEngine, fileName, sourcePath, operationMode, skipNFO, skipDownload)
	}

	if operationMode == types.OperationModeInPlace {
		return generateInPlacePreview(movie, fileResults, cfg, ctx, templateEngine, fileName, sourcePath, operationMode, skipNFO, skipDownload)
	}

	if operationMode == types.OperationModeMetadataOnly {
		return generateMetadataOnlyPreview(movie, fileResults, cfg, ctx, templateEngine, fileName, sourcePath, operationMode, skipNFO, skipDownload)
	}

	return generateOrganizePreview(movie, fileResults, destination, cfg, ctx, templateEngine, fileName, operationMode, skipNFO, skipDownload)
}

// generateOrganizePreview generates a preview for organize mode (move to destination with folder structure)
func generateOrganizePreview(movie *models.Movie, fileResults []*worker.FileResult, destination string, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine, fileName string, operationMode organizer.OperationMode, skipNFO bool, skipDownload bool) OrganizePreviewResponse {
	sourceExt := resolveSourceExt(fileResults)

	// Generate subfolder hierarchy (if configured)
	subfolderParts := make([]string, 0, len(cfg.Output.SubfolderFormat))
	for _, subfolderTemplate := range cfg.Output.SubfolderFormat {
		subfolderName, err := templateEngine.Execute(subfolderTemplate, ctx)
		if err != nil {
			logging.Errorf("Failed to generate subfolder from template '%s': %v", subfolderTemplate, err)
			continue
		}
		subfolderName = template.SanitizeFolderPath(subfolderName)
		if subfolderName != "" {
			subfolderParts = append(subfolderParts, subfolderName)
		}
	}

	var subfolderPath string
	if len(subfolderParts) > 0 {
		subfolderPath = previewJoinParts(subfolderParts...)
	}

	// Generate folder name
	folderName, err := templateEngine.Execute(cfg.Output.FolderFormat, ctx)
	if err != nil {
		logging.Errorf("Failed to generate folder name: %v", err)
		folderName = "error"
	}
	folderName = template.SanitizeFolderPath(folderName)

	// Build target paths with subfolder hierarchy
	pathParts := []string{destination}
	pathParts = append(pathParts, subfolderParts...)
	if folderName != "" {
		pathParts = append(pathParts, folderName)
	}
	folderPath := previewJoinParts(pathParts...)

	// Validate folder path length if configured
	if cfg.Output.MaxPathLength > 0 {
		if err := templateEngine.ValidatePathLength(folderPath, cfg.Output.MaxPathLength); err != nil {
			logging.Warnf("Preview: folder path exceeds max length: %s (length: %d, max: %d)", folderPath, len(folderPath), cfg.Output.MaxPathLength)
		}
	}

	// Generate video file paths for all parts (multi-part support)
	videoFiles := make([]string, 0, len(fileResults))
	var primaryVideoPath string

	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			ext := previewPathExt(result.FilePath)

			fileCtx := ctx.Clone()
			fileCtx.PartNumber = result.PartNumber
			fileCtx.PartSuffix = result.PartSuffix
			fileCtx.IsMultiPart = result.IsMultiPart

			videoFileName, err := templateEngine.Execute(cfg.Output.FileFormat, fileCtx)
			if err != nil {
				videoFileName = fileName
				if result.IsMultiPart && result.PartSuffix != "" {
					videoFileName = fileName + result.PartSuffix
				}
			}
			videoFileName = template.SanitizeFilename(videoFileName)
			if videoFileName == "" {
				videoFileName = fileName
				if result.IsMultiPart && result.PartSuffix != "" {
					videoFileName = fileName + result.PartSuffix
				}
			}

			videoPath := previewJoinPath(folderPath, videoFileName+ext)
			videoFiles = append(videoFiles, videoPath)

			if primaryVideoPath == "" {
				primaryVideoPath = videoPath
			}
		}
	}

	if primaryVideoPath == "" {
		primaryVideoPath = previewJoinPath(folderPath, fileName+sourceExt)
		videoFiles = append(videoFiles, primaryVideoPath)
	}

	var nfoPath string
	var nfoPaths []string
	if !skipNFO {
		nfoPath, nfoPaths = generateNFOPaths(movie, fileResults, cfg, ctx, templateEngine, fileName, folderPath)
	}
	var posterPath, fanartPath string
	var extrafanartPath string
	var screenshots []string
	if !skipDownload {
		posterPath = generatePosterPath(movie, fileResults, cfg, ctx, templateEngine, folderPath)
		fanartPath = generateFanartPath(movie, fileResults, cfg, ctx, templateEngine, folderPath)
		if cfg.Output.DownloadExtrafanart {
			extrafanartPath = previewJoinPath(folderPath, cfg.Output.ScreenshotFolder)
		}
		screenshots = generateScreenshotNames(movie, cfg, ctx, templateEngine)
	}

	validatePathLengths(cfg, templateEngine, videoFiles, nfoPath, nfoPaths, posterPath, fanartPath, extrafanartPath, screenshots)

	return OrganizePreviewResponse{
		FolderName:      folderName,
		FileName:        fileName,
		SubfolderPath:   subfolderPath,
		FullPath:        primaryVideoPath,
		VideoFiles:      videoFiles,
		NFOPath:         nfoPath,
		NFOPaths:        nfoPaths,
		PosterPath:      posterPath,
		FanartPath:      fanartPath,
		ExtrafanartPath: extrafanartPath,
		Screenshots:     screenshots,
		OperationMode:   string(operationMode),
	}
}

// generateInPlaceNoRenameFolderPreview generates a preview for in-place-norenamefolder mode
// (file stays in source directory, only file is renamed)
func generateInPlaceNoRenameFolderPreview(movie *models.Movie, fileResults []*worker.FileResult, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine, fileName string, sourcePath string, operationMode organizer.OperationMode, skipNFO bool, skipDownload bool) OrganizePreviewResponse {
	if sourcePath == "" || sourcePath == "." {
		return OrganizePreviewResponse{OperationMode: string(operationMode)}
	}

	sourceDir := previewPathDir(sourcePath)
	sourceExt := resolveSourceExt(fileResults)

	folderName := ""

	videoFiles := make([]string, 0, len(fileResults))
	var primaryVideoPath string

	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			ext := previewPathExt(result.FilePath)

			fileCtx := ctx.Clone()
			fileCtx.PartNumber = result.PartNumber
			fileCtx.PartSuffix = result.PartSuffix
			fileCtx.IsMultiPart = result.IsMultiPart

			videoFileName, err := templateEngine.Execute(cfg.Output.FileFormat, fileCtx)
			if err != nil {
				videoFileName = fileName
				if result.IsMultiPart && result.PartSuffix != "" {
					videoFileName = fileName + result.PartSuffix
				}
			}
			videoFileName = template.SanitizeFilename(videoFileName)
			if videoFileName == "" {
				videoFileName = fileName
				if result.IsMultiPart && result.PartSuffix != "" {
					videoFileName = fileName + result.PartSuffix
				}
			}

			videoPath := previewJoinPath(sourceDir, videoFileName+ext)
			videoFiles = append(videoFiles, videoPath)

			if primaryVideoPath == "" {
				primaryVideoPath = videoPath
			}
		}
	}

	if primaryVideoPath == "" {
		primaryVideoPath = previewJoinPath(sourceDir, fileName+sourceExt)
		videoFiles = append(videoFiles, primaryVideoPath)
	}

	var nfoPath string
	var nfoPaths []string
	if !skipNFO {
		nfoPath, nfoPaths = generateNFOPaths(movie, fileResults, cfg, ctx, templateEngine, fileName, sourceDir)
	}
	var posterPath, fanartPath string
	var extrafanartPath string
	var screenshots []string
	if !skipDownload {
		posterPath = generatePosterPath(movie, fileResults, cfg, ctx, templateEngine, sourceDir)
		fanartPath = generateFanartPath(movie, fileResults, cfg, ctx, templateEngine, sourceDir)
		if cfg.Output.DownloadExtrafanart {
			extrafanartPath = previewJoinPath(sourceDir, cfg.Output.ScreenshotFolder)
		}
		screenshots = generateScreenshotNames(movie, cfg, ctx, templateEngine)
	}

	return OrganizePreviewResponse{
		FolderName:      folderName,
		FileName:        fileName,
		FullPath:        primaryVideoPath,
		VideoFiles:      videoFiles,
		NFOPath:         nfoPath,
		NFOPaths:        nfoPaths,
		PosterPath:      posterPath,
		FanartPath:      fanartPath,
		ExtrafanartPath: extrafanartPath,
		Screenshots:     screenshots,
		SourcePath:      sourcePath,
		OperationMode:   string(operationMode),
	}
}

func generateInPlacePreview(movie *models.Movie, fileResults []*worker.FileResult, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine, fileName string, sourcePath string, operationMode organizer.OperationMode, skipNFO bool, skipDownload bool) OrganizePreviewResponse {
	if sourcePath == "" || sourcePath == "." {
		return OrganizePreviewResponse{OperationMode: string(operationMode)}
	}

	folderName, err := templateEngine.Execute(cfg.Output.FolderFormat, ctx)
	if err != nil {
		logging.Errorf("Failed to generate folder name: %v", err)
		folderName = "error"
	}
	folderName = template.SanitizeFolderPath(folderName)
	if folderName == "" {
		folderName = template.SanitizeFolderPath(movie.ID)
		if folderName == "" {
			folderName = "unknown"
		}
	}

	sourceDir := previewPathDir(sourcePath)
	sourceExt := resolveSourceExt(fileResults)

	parentDir := previewPathDir(sourceDir)
	targetDir := parentDir
	if folderName != "" {
		targetDir = previewJoinPath(parentDir, folderName)
	}

	videoFiles := make([]string, 0, len(fileResults))
	var primaryVideoPath string

	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			ext := previewPathExt(result.FilePath)

			fileCtx := ctx.Clone()
			fileCtx.PartNumber = result.PartNumber
			fileCtx.PartSuffix = result.PartSuffix
			fileCtx.IsMultiPart = result.IsMultiPart

			videoFileName, err := templateEngine.Execute(cfg.Output.FileFormat, fileCtx)
			if err != nil {
				videoFileName = fileName
				if result.IsMultiPart && result.PartSuffix != "" {
					videoFileName = fileName + result.PartSuffix
				}
			}
			videoFileName = template.SanitizeFilename(videoFileName)
			if videoFileName == "" {
				videoFileName = fileName
				if result.IsMultiPart && result.PartSuffix != "" {
					videoFileName = fileName + result.PartSuffix
				}
			}

			videoPath := previewJoinPath(targetDir, videoFileName+ext)
			videoFiles = append(videoFiles, videoPath)

			if primaryVideoPath == "" {
				primaryVideoPath = videoPath
			}
		}
	}

	if primaryVideoPath == "" {
		primaryVideoPath = previewJoinPath(targetDir, fileName+sourceExt)
		videoFiles = append(videoFiles, primaryVideoPath)
	}

	var nfoPath string
	var nfoPaths []string
	if !skipNFO {
		nfoPath, nfoPaths = generateNFOPaths(movie, fileResults, cfg, ctx, templateEngine, fileName, targetDir)
	}
	var posterPath, fanartPath string
	var extrafanartPath string
	var screenshots []string
	if !skipDownload {
		posterPath = generatePosterPath(movie, fileResults, cfg, ctx, templateEngine, targetDir)
		fanartPath = generateFanartPath(movie, fileResults, cfg, ctx, templateEngine, targetDir)
		if cfg.Output.DownloadExtrafanart {
			extrafanartPath = previewJoinPath(targetDir, cfg.Output.ScreenshotFolder)
		}
		screenshots = generateScreenshotNames(movie, cfg, ctx, templateEngine)
	}

	return OrganizePreviewResponse{
		FolderName:      folderName,
		FileName:        fileName,
		FullPath:        primaryVideoPath,
		VideoFiles:      videoFiles,
		NFOPath:         nfoPath,
		NFOPaths:        nfoPaths,
		PosterPath:      posterPath,
		FanartPath:      fanartPath,
		ExtrafanartPath: extrafanartPath,
		Screenshots:     screenshots,
		SourcePath:      sourcePath,
		OperationMode:   string(operationMode),
	}
}

func generateMetadataOnlyPreview(movie *models.Movie, fileResults []*worker.FileResult, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine, fileName string, sourcePath string, operationMode organizer.OperationMode, skipNFO bool, skipDownload bool) OrganizePreviewResponse {
	// Return empty preview if no source path available
	if sourcePath == "" || sourcePath == "." {
		return OrganizePreviewResponse{OperationMode: string(operationMode)}
	}

	// In metadata-only mode, nothing is renamed — use original file name
	sourceDir := previewPathDir(sourcePath)

	originalFileName := previewPathBase(sourcePath)

	// No folder name in metadata-only mode
	folderName := ""

	// Video file stays where it is
	videoFiles := make([]string, 0)
	var primaryVideoPath string
	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			videoFiles = append(videoFiles, result.FilePath)
			if primaryVideoPath == "" {
				primaryVideoPath = result.FilePath
			}
		}
	}
	if primaryVideoPath == "" {
		primaryVideoPath = sourcePath
		videoFiles = append(videoFiles, primaryVideoPath)
	}

	var nfoPath string
	var nfoPaths []string
	if !skipNFO {
		nfoPath, nfoPaths = generateNFOPaths(movie, fileResults, cfg, ctx, templateEngine, fileName, sourceDir)
	}
	var posterPath, fanartPath string
	var extrafanartPath string
	var screenshots []string
	if !skipDownload {
		posterPath = generatePosterPath(movie, fileResults, cfg, ctx, templateEngine, sourceDir)
		fanartPath = generateFanartPath(movie, fileResults, cfg, ctx, templateEngine, sourceDir)
		if cfg.Output.DownloadExtrafanart {
			extrafanartPath = previewJoinPath(sourceDir, cfg.Output.ScreenshotFolder)
		}
		screenshots = generateScreenshotNames(movie, cfg, ctx, templateEngine)
	}

	return OrganizePreviewResponse{
		FolderName:      folderName,
		FileName:        originalFileName,
		FullPath:        primaryVideoPath,
		VideoFiles:      videoFiles,
		NFOPath:         nfoPath,
		NFOPaths:        nfoPaths,
		PosterPath:      posterPath,
		FanartPath:      fanartPath,
		ExtrafanartPath: extrafanartPath,
		Screenshots:     screenshots,
		SourcePath:      sourcePath,
		OperationMode:   string(operationMode),
	}
}

// Helper functions for generating media paths

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

func resolveSourceExt(fileResults []*worker.FileResult) string {
	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			return previewPathExt(result.FilePath)
		}
	}
	return ".mp4"
}

func resolvePreviewFallbackName(movie *models.Movie, fileResults []*worker.FileResult) string {
	if movie.ID != "" {
		if sanitized := template.SanitizeFilename(movie.ID); sanitized != "" {
			return sanitized
		}
	}
	for _, result := range fileResults {
		if result != nil && result.MovieID != "" {
			if sanitized := template.SanitizeFilename(result.MovieID); sanitized != "" {
				return sanitized
			}
		}
	}
	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			base := previewPathBase(result.FilePath)
			ext := previewPathExt(result.FilePath)
			nameWithoutExt := strings.TrimSuffix(base, ext)
			if sanitized := template.SanitizeFilename(nameWithoutExt); sanitized != "" {
				return sanitized
			}
		}
	}
	return "file"
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
		return trimmed[:idx]
	}
}

func previewPathExt(path string) string {
	return filepath.Ext(previewPathBase(path))
}

func previewJoinParts(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}

	joined := parts[0]
	for _, part := range parts[1:] {
		joined = previewJoinPath(joined, part)
	}

	return joined
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
	return len(path) >= 2 && path[1] == ':' || strings.HasPrefix(path, `\\`)
}
