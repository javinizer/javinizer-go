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
		return generateUNCPreview(movie, fileResults, destination, cfg, operationMode, skipNFO, skipDownload, &outputConfig, sharedEngine, strategy)
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
			syntheticName := movie.ID + ".mp4"
			match := matcher.MatchResult{
				File: scanner.FileInfo{
					Path:      "",
					Name:      syntheticName,
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
	} else {
		effectiveMode := outputConfig.GetOperationMode()
		outputConfig.OperationMode = effectiveMode
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
		strategy = organizer.NewInPlaceNoRenameFolderStrategy(fs, outputConfig, fileMatcher, sharedEngine)
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
func generateUNCPreview(movie *models.Movie, fileResults []*worker.FileResult, destination string, cfg *config.Config, operationMode organizer.OperationMode, skipNFO bool, skipDownload bool, outputConfig *config.OutputConfig, sharedEngine *template.Engine, strategy organizer.OperationStrategy) OrganizePreviewResponse {
	sourcePath := ""
	for _, result := range fileResults {
		if result != nil && result.FilePath != "" {
			sourcePath = result.FilePath
			break
		}
	}

	posixDest := toPosixPath(destination)

	var primaryPlan *organizer.OrganizePlan
	videoFiles := make([]string, 0, len(fileResults))

	for _, result := range fileResults {
		if result == nil || result.FilePath == "" {
			continue
		}

		match := fileResultToMatchResult(result)
		match.File.Path = toPosixPath(match.File.Path)

		plan, err := strategy.Plan(match, movie, posixDest, false)
		if err != nil {
			logging.Warnf("Preview: strategy.Plan failed for %s: %v", result.FilePath, err)
			continue
		}

		if primaryPlan == nil {
			primaryPlan = plan
		}

		uncTargetPath := rebuildUNCPath(plan, sourcePath, destination)
		videoFiles = append(videoFiles, uncTargetPath)
	}

	if primaryPlan == nil {
		first := firstValidFileResult(fileResults)
		if first != nil {
			match := fileResultToMatchResult(first)
			match.File.Path = toPosixPath(match.File.Path)
			plan, err := strategy.Plan(match, movie, posixDest, false)
			if err != nil {
				return OrganizePreviewResponse{OperationMode: string(operationMode)}
			}
			primaryPlan = plan
			uncTargetPath := rebuildUNCPath(plan, sourcePath, destination)
			videoFiles = append(videoFiles, uncTargetPath)
		} else {
			syntheticName := movie.ID + ".mp4"
			match := matcher.MatchResult{
				File: scanner.FileInfo{Path: "", Name: syntheticName, Extension: ".mp4"},
				ID:   movie.ID,
			}
			plan, err := strategy.Plan(match, movie, posixDest, false)
			if err != nil {
				return OrganizePreviewResponse{OperationMode: string(operationMode)}
			}
			primaryPlan = plan
			uncTargetPath := rebuildUNCPath(plan, sourcePath, destination)
			videoFiles = append(videoFiles, uncTargetPath)
		}
	}

	if primaryPlan == nil {
		return OrganizePreviewResponse{OperationMode: string(operationMode)}
	}

	folderPath := rebuildUNCTargetDir(primaryPlan, sourcePath, destination)
	subfolderPath := primaryPlan.SubfolderPath
	if subfolderPath != "" {
		subfolderPath = strings.ReplaceAll(subfolderPath, "/", `\`)
	}
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

func rebuildUNCPath(plan *organizer.OrganizePlan, originalSource string, destination string) string {
	targetDir := rebuildUNCTargetDir(plan, originalSource, destination)
	return previewJoinPath(targetDir, plan.TargetFile)
}

func rebuildUNCTargetDir(plan *organizer.OrganizePlan, originalSource string, destination string) string {
	sourceDir := previewPathDir(originalSource)

	if plan.Strategy == organizer.StrategyTypeMetadataOnly {
		return sourceDir
	}

	if plan.Strategy == organizer.StrategyTypeInPlaceNoRenameFolder {
		return sourceDir
	}

	if plan.Strategy == organizer.StrategyTypeInPlace {
		if plan.InPlace {
			parentDir := previewPathDir(sourceDir)
			if plan.FolderName != "" {
				return previewJoinPath(parentDir, plan.FolderName)
			}
			return sourceDir
		}
	}

	pathBase := destination
	if pathBase == "" {
		pathBase = sourceDir
	}

	if plan.SubfolderPath != "" {
		parts := strings.Split(plan.SubfolderPath, `/`)
		for _, sp := range parts {
			clean := strings.Trim(sp, `/\`)
			if clean != "" {
				pathBase = previewJoinPath(pathBase, clean)
			}
		}
	}

	if plan.FolderName != "" {
		return previewJoinPath(pathBase, plan.FolderName)
	}

	return pathBase
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
