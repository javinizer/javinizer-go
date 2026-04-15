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
func generatePreview(movie *models.Movie, fileResults []*worker.FileResult, destination string, cfg *config.Config, operationMode organizer.OperationMode) OrganizePreviewResponse {
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = cfg.Output.GroupActress
	templateEngine := template.NewEngine()

	// Generate file name (used by all modes)
	fileName, err := templateEngine.Execute(cfg.Output.FileFormat, ctx)
	if err != nil {
		logging.Errorf("Failed to generate file name: %v", err)
		fileName = "error"
	}
	fileName = template.SanitizeFilename(fileName)

	// Determine source file path for in-place modes
	sourcePath := ""
	if len(fileResults) > 0 && fileResults[0] != nil {
		sourcePath = fileResults[0].FilePath
	}

	// For in-place-norenamefolder mode: file stays in source directory, only renamed
	if operationMode == types.OperationModeInPlaceNoRenameFolder {
		return generateInPlaceNoRenameFolderPreview(movie, fileResults, cfg, ctx, templateEngine, fileName, sourcePath, operationMode)
	}

	// For in-place mode: file may be moved to a renamed folder in the same parent directory
	if operationMode == types.OperationModeInPlace {
		return generateInPlacePreview(movie, fileResults, cfg, ctx, templateEngine, fileName, sourcePath, operationMode)
	}

	// For metadata-only mode: no file changes, metadata stays alongside source
	if operationMode == types.OperationModeMetadataOnly {
		return generateMetadataOnlyPreview(movie, fileResults, cfg, ctx, templateEngine, fileName, sourcePath, operationMode)
	}

	// Default: organize mode (and preview mode) — current behavior
	return generateOrganizePreview(movie, fileResults, destination, cfg, ctx, templateEngine, fileName, operationMode)
}

// generateOrganizePreview generates a preview for organize mode (move to destination with folder structure)
func generateOrganizePreview(movie *models.Movie, fileResults []*worker.FileResult, destination string, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine, fileName string, operationMode organizer.OperationMode) OrganizePreviewResponse {
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
	pathParts = append(pathParts, folderName)
	folderPath := filepath.Join(pathParts...)

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
			ext := filepath.Ext(result.FilePath)
			if ext == "" {
				ext = ".mp4"
			}

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

			videoPath := filepath.Join(folderPath, videoFileName+ext)
			videoFiles = append(videoFiles, videoPath)

			if primaryVideoPath == "" {
				primaryVideoPath = videoPath
			}
		}
	}

	if primaryVideoPath == "" {
		primaryVideoPath = filepath.Join(folderPath, fileName+".mp4")
		videoFiles = append(videoFiles, primaryVideoPath)
	}

	nfoPath, nfoPaths := generateNFOPaths(movie, fileResults, cfg, ctx, templateEngine, fileName, folderPath)
	posterPath := generatePosterPath(movie, fileResults, cfg, ctx, templateEngine, folderPath)
	fanartPath := generateFanartPath(movie, fileResults, cfg, ctx, templateEngine, folderPath)
	extrafanartPath := ""
	if cfg.Output.DownloadExtrafanart {
		extrafanartPath = filepath.Join(folderPath, cfg.Output.ScreenshotFolder)
	}
	screenshots := generateScreenshotNames(movie, cfg, ctx, templateEngine)

	validatePathLengths(cfg, templateEngine, videoFiles, nfoPath, nfoPaths, posterPath, fanartPath, extrafanartPath, screenshots)

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
		OperationMode:   string(operationMode),
	}
}

// generateInPlaceNoRenameFolderPreview generates a preview for in-place-norenamefolder mode
// (file stays in source directory, only file is renamed)
func generateInPlaceNoRenameFolderPreview(movie *models.Movie, fileResults []*worker.FileResult, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine, fileName string, sourcePath string, operationMode organizer.OperationMode) OrganizePreviewResponse {
	// Return empty preview if no source path available
	if sourcePath == "" || sourcePath == "." {
		return OrganizePreviewResponse{OperationMode: string(operationMode)}
	}

	// Determine source directory
	sourceDir := filepath.Dir(sourcePath)

	// No folder name or subfolders in this mode — file stays in place
	folderName := ""

	// Generate video file paths in source directory
	videoFiles := make([]string, 0, len(fileResults))
	var primaryVideoPath string

	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			ext := filepath.Ext(result.FilePath)
			if ext == "" {
				ext = ".mp4"
			}

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

			videoPath := filepath.Join(sourceDir, videoFileName+ext)
			videoFiles = append(videoFiles, videoPath)

			if primaryVideoPath == "" {
				primaryVideoPath = videoPath
			}
		}
	}

	if primaryVideoPath == "" {
		primaryVideoPath = filepath.Join(sourceDir, fileName+".mp4")
		videoFiles = append(videoFiles, primaryVideoPath)
	}

	nfoPath, nfoPaths := generateNFOPaths(movie, fileResults, cfg, ctx, templateEngine, fileName, sourceDir)
	posterPath := generatePosterPath(movie, fileResults, cfg, ctx, templateEngine, sourceDir)
	fanartPath := generateFanartPath(movie, fileResults, cfg, ctx, templateEngine, sourceDir)
	extrafanartPath := ""
	if cfg.Output.DownloadExtrafanart {
		extrafanartPath = filepath.Join(sourceDir, cfg.Output.ScreenshotFolder)
	}
	screenshots := generateScreenshotNames(movie, cfg, ctx, templateEngine)

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

// generateInPlacePreview generates a preview for in-place mode
// (file stays in same parent directory, but the folder may be renamed if it's a dedicated folder)
func generateInPlacePreview(movie *models.Movie, fileResults []*worker.FileResult, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine, fileName string, sourcePath string, operationMode organizer.OperationMode) OrganizePreviewResponse {
	// Return empty preview if no source path available
	if sourcePath == "" || sourcePath == "." {
		return OrganizePreviewResponse{OperationMode: string(operationMode)}
	}

	// Generate folder name for potential rename
	folderName, err := templateEngine.Execute(cfg.Output.FolderFormat, ctx)
	if err != nil {
		logging.Errorf("Failed to generate folder name: %v", err)
		folderName = "error"
	}
	folderName = template.SanitizeFolderPath(folderName)

	// In in-place mode, the file stays in its current parent directory
	// The folder rename is conditional on whether the current folder is dedicated
	// For preview, we show both the source directory and potential new folder name
	sourceDir := filepath.Dir(sourcePath)

	// Use parent of source directory + new folder name as potential target
	// The actual strategy checks if the folder is "dedicated" and renames accordingly
	// For preview, show the potential renamed folder path
	parentDir := filepath.Dir(sourceDir)
	targetDir := filepath.Join(parentDir, folderName)

	// Generate video file paths in target directory
	videoFiles := make([]string, 0, len(fileResults))
	var primaryVideoPath string

	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			ext := filepath.Ext(result.FilePath)
			if ext == "" {
				ext = ".mp4"
			}

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

			videoPath := filepath.Join(targetDir, videoFileName+ext)
			videoFiles = append(videoFiles, videoPath)

			if primaryVideoPath == "" {
				primaryVideoPath = videoPath
			}
		}
	}

	if primaryVideoPath == "" {
		primaryVideoPath = filepath.Join(targetDir, fileName+".mp4")
		videoFiles = append(videoFiles, primaryVideoPath)
	}

	nfoPath, nfoPaths := generateNFOPaths(movie, fileResults, cfg, ctx, templateEngine, fileName, targetDir)
	posterPath := generatePosterPath(movie, fileResults, cfg, ctx, templateEngine, targetDir)
	fanartPath := generateFanartPath(movie, fileResults, cfg, ctx, templateEngine, targetDir)
	extrafanartPath := ""
	if cfg.Output.DownloadExtrafanart {
		extrafanartPath = filepath.Join(targetDir, cfg.Output.ScreenshotFolder)
	}
	screenshots := generateScreenshotNames(movie, cfg, ctx, templateEngine)

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

// generateMetadataOnlyPreview generates a preview for metadata-only mode
// (no file moves or renames — metadata files alongside the original)
func generateMetadataOnlyPreview(movie *models.Movie, fileResults []*worker.FileResult, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine, fileName string, sourcePath string, operationMode organizer.OperationMode) OrganizePreviewResponse {
	// Return empty preview if no source path available
	if sourcePath == "" || sourcePath == "." {
		return OrganizePreviewResponse{OperationMode: string(operationMode)}
	}

	// In metadata-only mode, nothing is renamed — use original file name
	sourceDir := filepath.Dir(sourcePath)

	// Use original filename from source path
	originalFileName := filepath.Base(sourcePath)

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

	nfoPath, nfoPaths := generateNFOPaths(movie, fileResults, cfg, ctx, templateEngine, fileName, sourceDir)
	posterPath := generatePosterPath(movie, fileResults, cfg, ctx, templateEngine, sourceDir)
	fanartPath := generateFanartPath(movie, fileResults, cfg, ctx, templateEngine, sourceDir)
	extrafanartPath := ""
	if cfg.Output.DownloadExtrafanart {
		extrafanartPath = filepath.Join(sourceDir, cfg.Output.ScreenshotFolder)
	}
	screenshots := generateScreenshotNames(movie, cfg, ctx, templateEngine)

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

	isMultiPart := len(fileResults) > 1 && fileResults[0] != nil && fileResults[0].IsMultiPart
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
					nfoFileName = fileName
					if result.IsMultiPart && result.PartSuffix != "" {
						nfoFileName = fileName + result.PartSuffix
					}
				}

				basename := nfoFileName
				lower := strings.ToLower(basename)
				if strings.HasSuffix(lower, ".nfo") {
					basename = basename[:len(basename)-4]
				}
				sanitized := template.SanitizeFilename(basename)

				if sanitized == "" {
					sanitized = template.SanitizeFilename(fileName)
					if sanitized == "" {
						sanitized = "metadata"
					}
				}

				nfoFilePath := filepath.Join(folderPath, sanitized+".nfo")
				nfoPaths = append(nfoPaths, nfoFilePath)
			}
		}
		if len(nfoPaths) > 0 {
			nfoPath = nfoPaths[0]
		}
	} else {
		nfoFileName, err := templateEngine.Execute(cfg.Metadata.NFO.FilenameTemplate, ctx)
		if err != nil || nfoFileName == "" {
			nfoFileName = fileName + ".nfo"
		} else {
			basename := nfoFileName
			lower := strings.ToLower(basename)
			if strings.HasSuffix(lower, ".nfo") {
				basename = basename[:len(basename)-4]
			}
			sanitized := template.SanitizeFilename(basename)

			if sanitized == "" {
				sanitized = template.SanitizeFilename(fileName)
				if sanitized == "" {
					sanitized = "metadata"
				}
			}

			nfoFileName = sanitized + ".nfo"
		}
		nfoPath = filepath.Join(folderPath, nfoFileName)
	}

	return nfoPath, nfoPaths
}

func generatePosterPath(movie *models.Movie, fileResults []*worker.FileResult, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine, folderPath string) string {
	if !cfg.Output.DownloadPoster {
		return ""
	}

	posterCtx := ctx.Clone()
	if len(fileResults) > 0 && fileResults[0] != nil {
		posterCtx.PartNumber = fileResults[0].PartNumber
		posterCtx.PartSuffix = fileResults[0].PartSuffix
		posterCtx.IsMultiPart = fileResults[0].IsMultiPart
	}
	posterFileName, err := templateEngine.Execute(cfg.Output.PosterFormat, posterCtx)
	if err != nil || posterFileName == "" {
		posterFileName = fmt.Sprintf("%s-poster.jpg", movie.ID)
	}
	posterFileName = template.SanitizeFilename(posterFileName)
	if posterFileName == "" {
		posterFileName = fmt.Sprintf("%s-poster.jpg", template.SanitizeFilename(movie.ID))
	}
	return filepath.Join(folderPath, posterFileName)
}

func generateFanartPath(movie *models.Movie, fileResults []*worker.FileResult, cfg *config.Config, ctx *template.Context, templateEngine *template.Engine, folderPath string) string {
	if !cfg.Output.DownloadExtrafanart {
		return ""
	}

	fanartCtx := ctx.Clone()
	if len(fileResults) > 0 && fileResults[0] != nil {
		fanartCtx.PartNumber = fileResults[0].PartNumber
		fanartCtx.PartSuffix = fileResults[0].PartSuffix
		fanartCtx.IsMultiPart = fileResults[0].IsMultiPart
	}
	fanartFileName, err := templateEngine.Execute(cfg.Output.FanartFormat, fanartCtx)
	if err != nil || fanartFileName == "" {
		fanartFileName = fmt.Sprintf("%s-fanart.jpg", movie.ID)
	}
	fanartFileName = template.SanitizeFilename(fanartFileName)
	if fanartFileName == "" {
		fanartFileName = fmt.Sprintf("%s-fanart.jpg", template.SanitizeFilename(movie.ID))
	}
	return filepath.Join(folderPath, fanartFileName)
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
		screenshotPath := filepath.Join(extrafanartPath, screenshot)
		if err := templateEngine.ValidatePathLength(screenshotPath, cfg.Output.MaxPathLength); err != nil {
			logging.Warnf("Preview: screenshot path exceeds max length: %s (length: %d, max: %d)", screenshotPath, len(screenshotPath), cfg.Output.MaxPathLength)
		}
	}
}
