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

// generatePreview generates an organize preview response for a movie
// fileResults contains all file results for this movie (to support multi-part files)
func generatePreview(movie *models.Movie, fileResults []*worker.FileResult, destination string, cfg *config.Config) OrganizePreviewResponse {
	// Create template context from movie
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = cfg.Output.GroupActress
	templateEngine := template.NewEngine()

	// Generate subfolder hierarchy (if configured)
	subfolderParts := make([]string, 0, len(cfg.Output.SubfolderFormat))
	for _, subfolderTemplate := range cfg.Output.SubfolderFormat {
		subfolderName, err := templateEngine.Execute(subfolderTemplate, ctx)
		if err != nil {
			logging.Errorf("Failed to generate subfolder from template '%s': %v", subfolderTemplate, err)
			continue
		}
		// Sanitize and add to parts if not empty
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

	// Generate file name
	fileName, err := templateEngine.Execute(cfg.Output.FileFormat, ctx)
	if err != nil {
		logging.Errorf("Failed to generate file name: %v", err)
		fileName = "error"
	}
	fileName = template.SanitizeFilename(fileName)

	// Build target paths with subfolder hierarchy
	// Start with destination, add subfolder parts, then final folder name
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
			// Get original extension
			ext := filepath.Ext(result.FilePath)
			if ext == "" {
				ext = ".mp4" // Fallback
			}

			// Generate filename using template with multi-part context
			fileCtx := ctx.Clone()
			fileCtx.PartNumber = result.PartNumber
			fileCtx.PartSuffix = result.PartSuffix
			fileCtx.IsMultiPart = result.IsMultiPart

			videoFileName, err := templateEngine.Execute(cfg.Output.FileFormat, fileCtx)
			if err != nil {
				// Fallback to base fileName if template fails
				videoFileName = fileName
				if result.IsMultiPart && result.PartSuffix != "" {
					videoFileName = fileName + result.PartSuffix
				}
			}
			videoFileName = template.SanitizeFilename(videoFileName)

			videoPath := filepath.Join(folderPath, videoFileName+ext)
			videoFiles = append(videoFiles, videoPath)

			// Use first video as primary path for backward compatibility
			if primaryVideoPath == "" {
				primaryVideoPath = videoPath
			}
		}
	}

	// Fallback if no file results (shouldn't happen, but be defensive)
	if primaryVideoPath == "" {
		primaryVideoPath = filepath.Join(folderPath, fileName+".mp4")
		videoFiles = append(videoFiles, primaryVideoPath)
	}

	// Check if multi-part and per_file is enabled
	isMultiPart := len(fileResults) > 1 && fileResults[0] != nil && fileResults[0].IsMultiPart
	generatePerFileNFO := cfg.Metadata.NFO.PerFile && isMultiPart

	// Generate NFO paths using template engine
	// Only generate if NFO is enabled in config
	var nfoPath string
	var nfoPaths []string

	if cfg.Metadata.NFO.Enabled {
		if generatePerFileNFO {
			// Generate one NFO per video file (matching video file naming)
			nfoPaths = make([]string, 0, len(fileResults))
			for _, result := range fileResults {
				if result != nil && result.FilePath != "" {
					// Generate NFO filename using template with multi-part context
					nfoCtx := ctx.Clone()
					nfoCtx.PartNumber = result.PartNumber
					nfoCtx.PartSuffix = result.PartSuffix
					nfoCtx.IsMultiPart = result.IsMultiPart

					nfoFileName, err := templateEngine.Execute(cfg.Metadata.NFO.FilenameTemplate, nfoCtx)
					if err != nil || nfoFileName == "" {
						// Fallback to fileName-based naming
						nfoFileName = fileName
						if result.IsMultiPart && result.PartSuffix != "" {
							nfoFileName = fileName + result.PartSuffix
						}
					}

					// Case-insensitive .nfo trimming to prevent double extensions
					basename := nfoFileName
					lower := strings.ToLower(basename)
					if strings.HasSuffix(lower, ".nfo") {
						basename = basename[:len(basename)-4]
					}
					sanitized := template.SanitizeFilename(basename)

					// Three-tier fallback for empty results
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
			// Set primary NFO path for backward compatibility (use first)
			if len(nfoPaths) > 0 {
				nfoPath = nfoPaths[0]
			}
		} else {
			// Single NFO file (default behavior) - use template engine
			nfoFileName, err := templateEngine.Execute(cfg.Metadata.NFO.FilenameTemplate, ctx)
			if err != nil || nfoFileName == "" {
				// Fallback to fileName-based naming
				nfoFileName = fileName + ".nfo"
			} else {
				// Case-insensitive .nfo trimming to prevent double extensions
				basename := nfoFileName
				lower := strings.ToLower(basename)
				if strings.HasSuffix(lower, ".nfo") {
					basename = basename[:len(basename)-4]
				}
				sanitized := template.SanitizeFilename(basename)

				// Three-tier fallback for empty results
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
	}
	// If NFO is disabled, nfoPath and nfoPaths remain empty

	// Generate poster path using template engine
	// Only generate if poster download is enabled
	logging.Debugf("DEBUG generatePreview: DownloadCover=%v, DownloadPoster=%v, DownloadExtrafanart=%v", cfg.Output.DownloadCover, cfg.Output.DownloadPoster, cfg.Output.DownloadExtrafanart)
	var posterPath string
	if cfg.Output.DownloadPoster {
		// Use first file's multipart context so templates with <IF:MULTIPART> work correctly
		posterCtx := ctx.Clone()
		if len(fileResults) > 0 && fileResults[0] != nil {
			posterCtx.PartNumber = fileResults[0].PartNumber
			posterCtx.PartSuffix = fileResults[0].PartSuffix
			posterCtx.IsMultiPart = fileResults[0].IsMultiPart
		}
		posterFileName, err := templateEngine.Execute(cfg.Output.PosterFormat, posterCtx)
		if err != nil || posterFileName == "" {
			// Fallback to hardcoded format
			posterFileName = fmt.Sprintf("%s-poster.jpg", movie.ID)
		}
		posterFileName = template.SanitizeFilename(posterFileName)
		if posterFileName == "" {
			// Double fallback if sanitization removes everything
			posterFileName = fmt.Sprintf("%s-poster.jpg", template.SanitizeFilename(movie.ID))
		}
		posterPath = filepath.Join(folderPath, posterFileName)
	}
	// If cover/poster download is disabled, posterPath remains empty

	// Generate fanart path using template engine
	// Only generate if extrafanart download is enabled
	logging.Debugf("DEBUG generatePreview: DownloadExtrafanart=%v (fanart check)", cfg.Output.DownloadExtrafanart)
	var fanartPath string
	if cfg.Output.DownloadExtrafanart {
		// Use first file's multipart context so templates with <IF:MULTIPART> work correctly
		fanartCtx := ctx.Clone()
		if len(fileResults) > 0 && fileResults[0] != nil {
			fanartCtx.PartNumber = fileResults[0].PartNumber
			fanartCtx.PartSuffix = fileResults[0].PartSuffix
			fanartCtx.IsMultiPart = fileResults[0].IsMultiPart
		}
		fanartFileName, err := templateEngine.Execute(cfg.Output.FanartFormat, fanartCtx)
		if err != nil || fanartFileName == "" {
			// Fallback to hardcoded format
			fanartFileName = fmt.Sprintf("%s-fanart.jpg", movie.ID)
		}
		fanartFileName = template.SanitizeFilename(fanartFileName)
		if fanartFileName == "" {
			// Double fallback if sanitization removes everything
			fanartFileName = fmt.Sprintf("%s-fanart.jpg", template.SanitizeFilename(movie.ID))
		}
		fanartPath = filepath.Join(folderPath, fanartFileName)
	}
	// If extrafanart download is disabled, fanartPath remains empty

	// Use configured screenshot folder name (only if extrafanart is enabled)
	var extrafanartPath string
	if cfg.Output.DownloadExtrafanart {
		extrafanartPath = filepath.Join(folderPath, cfg.Output.ScreenshotFolder)
	}

	// Generate screenshot names using template engine (same as downloader)
	// Only generate if extrafanart download is enabled
	screenshots := []string{}
	if cfg.Output.DownloadExtrafanart && len(movie.Screenshots) > 0 {
		for i := range movie.Screenshots {
			ctx.Index = i + 1 // Set index for template
			screenshotName, err := templateEngine.Execute(cfg.Output.ScreenshotFormat, ctx)
			if err != nil || screenshotName == "" {
				// Fallback to hardcoded format with configurable padding (matching downloader logic)
				if cfg.Output.ScreenshotPadding > 0 {
					screenshotName = fmt.Sprintf("fanart%0*d.jpg", cfg.Output.ScreenshotPadding, i+1)
				} else {
					screenshotName = fmt.Sprintf("fanart%d.jpg", i+1)
				}
			}
			screenshotName = template.SanitizeFilename(screenshotName)
			if screenshotName == "" {
				// Double fallback if sanitization removes everything
				if cfg.Output.ScreenshotPadding > 0 {
					screenshotName = fmt.Sprintf("fanart%0*d.jpg", cfg.Output.ScreenshotPadding, i+1)
				} else {
					screenshotName = fmt.Sprintf("fanart%d.jpg", i+1)
				}
			}
			screenshots = append(screenshots, screenshotName)
		}
	}

	// Validate path lengths if max_path_length is configured
	if cfg.Output.MaxPathLength > 0 {
		// Validate video file paths
		for _, videoPath := range videoFiles {
			if err := templateEngine.ValidatePathLength(videoPath, cfg.Output.MaxPathLength); err != nil {
				logging.Warnf("Preview: video path exceeds max length: %s (length: %d, max: %d)", videoPath, len(videoPath), cfg.Output.MaxPathLength)
			}
		}
		// Validate NFO paths
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
		// Validate media file paths
		if err := templateEngine.ValidatePathLength(posterPath, cfg.Output.MaxPathLength); err != nil {
			logging.Warnf("Preview: poster path exceeds max length: %s (length: %d, max: %d)", posterPath, len(posterPath), cfg.Output.MaxPathLength)
		}
		if err := templateEngine.ValidatePathLength(fanartPath, cfg.Output.MaxPathLength); err != nil {
			logging.Warnf("Preview: fanart path exceeds max length: %s (length: %d, max: %d)", fanartPath, len(fanartPath), cfg.Output.MaxPathLength)
		}
		// Validate screenshot paths (full paths in extrafanart folder)
		for _, screenshot := range screenshots {
			screenshotPath := filepath.Join(extrafanartPath, screenshot)
			if err := templateEngine.ValidatePathLength(screenshotPath, cfg.Output.MaxPathLength); err != nil {
				logging.Warnf("Preview: screenshot path exceeds max length: %s (length: %d, max: %d)", screenshotPath, len(screenshotPath), cfg.Output.MaxPathLength)
			}
		}
	}

	return OrganizePreviewResponse{
		FolderName:      folderName,
		FileName:        fileName,
		FullPath:        primaryVideoPath, // Backward compatibility
		VideoFiles:      videoFiles,       // All video files (multi-part support)
		NFOPath:         nfoPath,          // Single NFO or first NFO (backward compatibility)
		NFOPaths:        nfoPaths,         // All NFO paths when per_file=true (nil otherwise)
		PosterPath:      posterPath,
		FanartPath:      fanartPath,
		ExtrafanartPath: extrafanartPath,
		Screenshots:     screenshots,
	}
}
