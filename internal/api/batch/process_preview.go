package batch

import (
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
