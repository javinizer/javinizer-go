package batch

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/types"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
)

func generatePreview(movie *models.Movie, fileResults []*worker.FileResult, destination string, cfg *config.Config, operationMode organizer.OperationMode, skipNFO bool, skipDownload bool) OrganizePreviewResponse {
	outputConfig := deriveOutputConfig(cfg, operationMode)

	sharedEngine := template.NewEngine()
	strategy, _ := createPreviewStrategy(&outputConfig, cfg)

	sourcePath := ""
	windowsSource := false
	uncSource := false
	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			sourcePath = result.FilePath
			windowsSource = isWindowsPathLike(sourcePath)
			uncSource = strings.HasPrefix(sourcePath, `\\`)
			break
		}
	}

	if sourcePath == "" || sourcePath == "." {
		if operationMode == types.OperationModeInPlaceNoRenameFolder ||
			operationMode == types.OperationModeInPlace ||
			operationMode == types.OperationModeMetadataOnly {
			return OrganizePreviewResponse{OperationMode: string(operationMode)}
		}
	}

	if uncSource {
		return generateUNCPreview(movie, fileResults, destination, cfg, operationMode, skipNFO, skipDownload, &outputConfig, sharedEngine)
	}

	normalize := func(path string) string {
		if windowsSource {
			return toWindowsPath(path)
		}
		return path
	}

	videoFiles := make([]string, 0, len(fileResults))
	var primaryPlan *organizer.OrganizePlan

	for _, result := range fileResults {
		if result == nil || result.FilePath == "" {
			continue
		}

		match := fileResultToMatchResult(result)

		plan, err := strategy.Plan(match, movie, destination, false)
		if err != nil {
			logging.Warnf("Preview: strategy.Plan failed for %s: %v", result.FilePath, err)
			continue
		}

		if primaryPlan == nil {
			primaryPlan = plan
		}

		videoPath := normalize(plan.TargetPath)
		videoFiles = append(videoFiles, videoPath)
	}

	if primaryPlan == nil {
		first := firstValidFileResult(fileResults)
		if first != nil {
			match := fileResultToMatchResult(first)
			plan, err := strategy.Plan(match, movie, destination, false)
			if err != nil {
				return OrganizePreviewResponse{OperationMode: string(operationMode)}
			}
			primaryPlan = plan
			videoPath := normalize(plan.TargetPath)
			videoFiles = append(videoFiles, videoPath)
		} else {
			match := matcher.MatchResult{
				File: scanner.FileInfo{
					Path:      "",
					Name:      "",
					Extension: ".mp4",
				},
				ID: movie.ID,
			}
			plan, err := strategy.Plan(match, movie, destination, false)
			if err != nil {
				return OrganizePreviewResponse{OperationMode: string(operationMode)}
			}
			primaryPlan = plan
			videoPath := normalize(plan.TargetPath)
			videoFiles = append(videoFiles, videoPath)
		}
	}

	if primaryPlan == nil {
		return OrganizePreviewResponse{OperationMode: string(operationMode)}
	}

	folderPath := normalize(primaryPlan.TargetDir)
	subfolderPath := normalize(primaryPlan.SubfolderPath)
	folderName := primaryPlan.FolderName
	fileName := primaryPlan.BaseFileName

	var nfoPath string
	var nfoPaths []string
	if !skipNFO {
		nfoPath, nfoPaths = generateNFOPaths(movie, fileResults, cfg, template.NewContextFromMovie(movie), sharedEngine, fileName, folderPath)
	}

	var posterPath, fanartPath string
	var extrafanartPath string
	var screenshots []string
	if !skipDownload {
		posterPath = generatePosterPath(movie, fileResults, cfg, template.NewContextFromMovie(movie), sharedEngine, folderPath)
		fanartPath = generateFanartPath(movie, fileResults, cfg, template.NewContextFromMovie(movie), sharedEngine, folderPath)
		if cfg.Output.DownloadExtrafanart {
			extrafanartPath = previewJoinPath(folderPath, cfg.Output.ScreenshotFolder)
		}
		screenshots = generateScreenshotNames(movie, cfg, template.NewContextFromMovie(movie), sharedEngine)
	}

	validatePathLengths(cfg, sharedEngine, videoFiles, nfoPath, nfoPaths, posterPath, fanartPath, extrafanartPath, screenshots)

	sourcePathField := ""
	if operationMode != types.OperationModeOrganize && operationMode != "" {
		if primaryPlan.SourcePath != "" {
			sourcePathField = normalize(primaryPlan.SourcePath)
		}
	}

	return OrganizePreviewResponse{
		FolderName:      folderName,
		FileName:        fileName,
		SubfolderPath:   subfolderPath,
		FullPath:        videoFiles[0],
		VideoFiles:      videoFiles,
		NFOPath:         nfoPath,
		NFOPaths:        nfoPaths,
		PosterPath:      posterPath,
		FanartPath:      fanartPath,
		ExtrafanartPath: extrafanartPath,
		Screenshots:     screenshots,
		SourcePath:      sourcePathField,
		OperationMode:   string(operationMode),
	}
}

func deriveOutputConfig(cfg *config.Config, operationMode organizer.OperationMode) config.OutputConfig {
	outputConfig := cfg.Output
	if operationMode != "" {
		outputConfig.OperationMode = operationMode
		outputConfig.MoveToFolder = operationMode == types.OperationModeOrganize
		outputConfig.RenameFolderInPlace = operationMode == types.OperationModeInPlace
	} else {
		effectiveMode := outputConfig.GetOperationMode()
		outputConfig.OperationMode = effectiveMode
		outputConfig.MoveToFolder = effectiveMode == types.OperationModeOrganize
		outputConfig.RenameFolderInPlace = effectiveMode == types.OperationModeInPlace
	}
	return outputConfig
}

func createPreviewStrategy(outputConfig *config.OutputConfig, cfg *config.Config) (organizer.OperationStrategy, *matcher.Matcher) {
	fs := afero.NewOsFs()
	sharedEngine := template.NewEngine()

	fileMatcher, err := matcher.NewMatcher(&cfg.Matching)
	if err != nil {
		logging.Warnf("Preview: failed to create matcher: %v", err)
		fileMatcher = nil
	}

	effectiveMode := outputConfig.GetOperationMode()

	var strategy organizer.OperationStrategy
	switch effectiveMode {
	case types.OperationModeOrganize:
		strategy = organizer.NewOrganizeStrategy(fs, outputConfig, sharedEngine)
	case types.OperationModeInPlace:
		if fileMatcher != nil {
			strategy = organizer.NewInPlaceStrategy(fs, outputConfig, fileMatcher, sharedEngine)
		} else {
			strategy = organizer.NewOrganizeStrategy(fs, outputConfig, sharedEngine)
		}
	case types.OperationModeInPlaceNoRenameFolder:
		if fileMatcher != nil {
			strategy = organizer.NewInPlaceNoRenameFolderStrategy(fs, outputConfig, fileMatcher, sharedEngine)
		} else {
			strategy = organizer.NewOrganizeStrategy(fs, outputConfig, sharedEngine)
		}
	case types.OperationModeMetadataOnly:
		strategy = organizer.NewMetadataOnlyStrategy(fs, outputConfig)
	default:
		strategy = organizer.NewOrganizeStrategy(fs, outputConfig, sharedEngine)
	}

	return strategy, fileMatcher
}

func fileResultToMatchResult(result *worker.FileResult) matcher.MatchResult {
	ext := previewPathExt(result.FilePath)
	posixPath := toPosixPath(result.FilePath)
	return matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      posixPath,
			Name:      previewPathBase(result.FilePath),
			Extension: ext,
			Dir:       toPosixPath(previewPathDir(result.FilePath)),
		},
		ID:          result.MovieID,
		IsMultiPart: result.IsMultiPart,
		PartNumber:  result.PartNumber,
		PartSuffix:  result.PartSuffix,
	}
}

// generateUNCPreview handles preview generation for UNC source paths
// (\\server\share\...) without using filepath.Dir/Join, which collapse
// the // prefix on non-Windows platforms. Instead, it uses the
// cross-platform previewPathDir/previewJoinPath helpers that correctly
// parse and join Windows-style paths regardless of the host OS.
func generateUNCPreview(movie *models.Movie, fileResults []*worker.FileResult, destination string, cfg *config.Config, operationMode organizer.OperationMode, skipNFO bool, skipDownload bool, outputConfig *config.OutputConfig, sharedEngine *template.Engine) OrganizePreviewResponse {
	firstResult := firstValidFileResult(fileResults)

	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = outputConfig.GroupActress
	if firstResult != nil {
		ctx.PartNumber = firstResult.PartNumber
		ctx.PartSuffix = firstResult.PartSuffix
		ctx.IsMultiPart = firstResult.IsMultiPart
	}
	applyPreviewTitleTruncation(sharedEngine, ctx, outputConfig.MaxTitleLength)

	sourceDir := ""
	sourcePath := ""
	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			sourcePath = result.FilePath
			sourceDir = previewPathDir(result.FilePath)
			break
		}
	}

	folderName := ""
	subfolderPath := ""
	folderPath := sourceDir

	switch operationMode {
	case types.OperationModeMetadataOnly:
	case types.OperationModeInPlaceNoRenameFolder:
	case types.OperationModeInPlace:
		rendered, err := sharedEngine.Execute(outputConfig.FolderFormat, ctx)
		if err == nil {
			folderName = template.SanitizeFolderPath(rendered)
		}
		if folderName == "" && movie.ID != "" {
			folderName = template.SanitizeFolderPath(movie.ID)
		}
		if folderName == "" {
			folderName = "unknown"
		}
		if sourceDir != "" && folderName != "" {
			parentDir := previewPathDir(sourceDir)
			folderPath = previewJoinPath(parentDir, folderName)
		}
	default:
		subfolderParts := make([]string, 0, len(outputConfig.SubfolderFormat))
		for _, subfolderTemplate := range outputConfig.SubfolderFormat {
			subfolderName, err := sharedEngine.Execute(subfolderTemplate, ctx)
			if err == nil {
				sanitized := template.SanitizeFolderPath(subfolderName)
				if sanitized != "" {
					subfolderParts = append(subfolderParts, sanitized)
				}
			}
		}
		if len(subfolderParts) > 0 {
			subfolderPath = strings.Join(subfolderParts, `\`)
		}

		rendered, err := sharedEngine.Execute(outputConfig.FolderFormat, ctx)
		if err == nil {
			folderName = template.SanitizeFolderPath(rendered)
		}
		if folderName == "" && movie.ID != "" {
			folderName = template.SanitizeFolderPath(movie.ID)
		}
		if folderName == "" {
			folderName = "unknown"
		}

		pathBase := destination
		if pathBase == "" {
			pathBase = sourceDir
		}
		if len(subfolderParts) > 0 {
			for _, sp := range subfolderParts {
				pathBase = previewJoinPath(pathBase, sp)
			}
		}
		folderPath = previewJoinPath(pathBase, folderName)
	}

	videoFiles := make([]string, 0, len(fileResults))
	for _, result := range fileResults {
		if result == nil || result.FilePath == "" {
			continue
		}
		match := fileResultToMatchResult(result)
		perFileCtx := template.NewContextFromMovie(movie)
		perFileCtx.GroupActress = outputConfig.GroupActress
		perFileCtx.PartNumber = result.PartNumber
		perFileCtx.PartSuffix = result.PartSuffix
		perFileCtx.IsMultiPart = result.IsMultiPart
		applyPreviewTitleTruncation(sharedEngine, perFileCtx, outputConfig.MaxTitleLength)
		videoFileName := resolveUNCVideoFileName(movie, outputConfig, sharedEngine, perFileCtx, match)
		videoPath := previewJoinPath(folderPath, videoFileName)
		videoFiles = append(videoFiles, videoPath)
	}

	if len(videoFiles) == 0 {
		ext := previewPathExt(previewPathBase(sourcePath))
		if ext == "" {
			ext = ".mp4"
		}
		videoPath := previewJoinPath(folderPath, resolvePreviewFallbackName(movie, fileResults)+ext)
		videoFiles = append(videoFiles, videoPath)
	}

	fileName := computeUNCBaseFileName(movie, fileResults, outputConfig, sharedEngine, ctx, operationMode)

	var nfoPath string
	var nfoPaths []string
	if !skipNFO {
		nfoPath, nfoPaths = generateNFOPaths(movie, fileResults, cfg, ctx, sharedEngine, fileName, folderPath)
	}

	var posterPath, fanartPath string
	var extrafanartPath string
	var screenshots []string
	if !skipDownload {
		posterPath = generatePosterPath(movie, fileResults, cfg, ctx, sharedEngine, folderPath)
		fanartPath = generateFanartPath(movie, fileResults, cfg, ctx, sharedEngine, folderPath)
		if cfg.Output.DownloadExtrafanart {
			extrafanartPath = previewJoinPath(folderPath, cfg.Output.ScreenshotFolder)
		}
		screenshots = generateScreenshotNames(movie, cfg, ctx, sharedEngine)
	}

	validatePathLengths(cfg, sharedEngine, videoFiles, nfoPath, nfoPaths, posterPath, fanartPath, extrafanartPath, screenshots)

	sourcePathField := ""
	if operationMode != types.OperationModeOrganize && operationMode != "" {
		sourcePathField = sourcePath
	}

	return OrganizePreviewResponse{
		FolderName:      folderName,
		FileName:        fileName,
		SubfolderPath:   subfolderPath,
		FullPath:        videoFiles[0],
		VideoFiles:      videoFiles,
		NFOPath:         nfoPath,
		NFOPaths:        nfoPaths,
		PosterPath:      posterPath,
		FanartPath:      fanartPath,
		ExtrafanartPath: extrafanartPath,
		Screenshots:     screenshots,
		SourcePath:      sourcePathField,
		OperationMode:   string(operationMode),
	}
}

func resolveUNCVideoFileName(movie *models.Movie, outputConfig *config.OutputConfig, engine *template.Engine, ctx *template.Context, match matcher.MatchResult) string {
	if outputConfig.RenameFile {
		rendered, err := engine.Execute(outputConfig.FileFormat, ctx)
		if err == nil {
			sanitized := template.SanitizeFilename(rendered)
			if sanitized != "" {
				return sanitized + match.File.Extension
			}
		}
		fallback := ""
		if match.ID != "" {
			fallback = template.SanitizeFilename(match.ID)
		}
		if fallback == "" {
			fallback = template.SanitizeFilename(strings.TrimSuffix(match.File.Name, match.File.Extension))
		}
		if fallback == "" {
			fallback = "file"
		}
		logging.Warnf("[%s] Template produced empty filename after sanitization, falling back to %q", match.ID, fallback)
		return fallback + match.File.Extension
	}
	return match.File.Name
}

func computeUNCBaseFileName(movie *models.Movie, fileResults []*worker.FileResult, outputConfig *config.OutputConfig, engine *template.Engine, ctx *template.Context, operationMode organizer.OperationMode) string {
	if operationMode == types.OperationModeMetadataOnly {
		if len(fileResults) > 0 && fileResults[0] != nil && fileResults[0].FilePath != "" {
			base := previewPathBase(fileResults[0].FilePath)
			ext := previewPathExt(base)
			return strings.TrimSuffix(base, ext)
		}
	}

	nonMultipartCtx := template.NewContextFromMovie(movie)
	nonMultipartCtx.GroupActress = outputConfig.GroupActress
	nonMultipartCtx.IsMultiPart = false
	nonMultipartCtx.PartNumber = 0
	nonMultipartCtx.PartSuffix = ""
	applyPreviewTitleTruncation(engine, nonMultipartCtx, outputConfig.MaxTitleLength)

	if outputConfig.RenameFile {
		rendered, err := engine.Execute(outputConfig.FileFormat, nonMultipartCtx)
		if err == nil {
			sanitized := template.SanitizeFilename(rendered)
			if sanitized != "" {
				return sanitized
			}
		}
	}
	return resolvePreviewFallbackName(movie, fileResults)
}

func applyPreviewTitleTruncation(engine *template.Engine, ctx *template.Context, maxTitleLength int) {
	if maxTitleLength > 0 {
		ctx.Title = engine.TruncateTitle(ctx.Title, maxTitleLength)
		ctx.OriginalTitle = engine.TruncateTitle(ctx.OriginalTitle, maxTitleLength)
	}
}

// toPosixPath converts Windows-style backslashes to forward slashes so that
// filepath.Dir/Join work correctly on non-Windows platforms when processing
// DOS-style drive-letter paths (C:\...) that originate from Windows clients.
// UNC paths (\\server\share) are handled separately by generateUNCPreview
// which avoids filepath.Dir/Join entirely.
func toPosixPath(path string) string {
	if !isWindowsPathLike(path) && !strings.Contains(path, `\`) {
		return path
	}
	return strings.ReplaceAll(path, `\`, `/`)
}

// toWindowsPath converts forward slashes to backslashes for preview responses
// when the source file path is Windows-style. Called only when the provenance
// (windowsSource flag) is known, so it never misidentifies POSIX paths.
func toWindowsPath(path string) string {
	if path == "" {
		return ""
	}
	return strings.ReplaceAll(path, `/`, `\`)
}
