package workflow

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

// pathResolverFromConfig constructs a MediaPathResolver from PreviewPathConfig.
// Per DEEP-5: extracted so the preview orchestrator only depends on the narrow
// PreviewPathConfig, not the full *organizer.Config.
func pathResolverFromConfig(pathCfg PreviewPathConfig, engine template.EngineInterface) *downloader.MediaPathResolver {
	return downloader.NewMediaPathResolver(pathCfg.MediaFormatConfig, engine)
}

// previewOrchestrator is the internal interface for the Preview phase.
// Unexported — only the composition root (Workflow) uses it.
type previewOrchestrator interface {
	Execute(ctx context.Context, cmd PreviewCmd) (*PreviewResult, error)
}

// Path encoding has been deepened into OrganizePlan.EncodePaths().
// Per ADR-0036: the pathEncoder interface was removed because path encoding
// is a property of the organize plan, not a separate strategy. The organizer
// package owns the encoding logic; the preview orchestrator reads the results.

type previewOrchImpl struct {
	fs             afero.Fs
	matcher        matcher.MatcherInterface
	previewCfg     PreviewConfig
	nfoNameCfg     nfo.NFONameConfig
	templateEngine template.EngineInterface
	pathResolver   *downloader.MediaPathResolver
	nfoIface       nfo.NFOFieldMerger
	logger         logging.Logger
}

var _ previewOrchestrator = (*previewOrchImpl)(nil)

func newPreviewOrchestrator(
	fs afero.Fs,
	m matcher.MatcherInterface,
	previewCfg PreviewConfig,
	nfoNameCfg nfo.NFONameConfig,
	templateEngine template.EngineInterface,
	nfoIface nfo.NFOFieldMerger,
	logger logging.Logger,
) previewOrchestrator {
	if templateEngine == nil {
		templateEngine = template.NewEngine()
	}
	return &previewOrchImpl{
		fs:             fs,
		matcher:        m,
		previewCfg:     previewCfg,
		nfoNameCfg:     nfoNameCfg,
		templateEngine: templateEngine,
		pathResolver:   pathResolverFromConfig(previewCfg.PathCfg, templateEngine),
		nfoIface:       nfoIface,
		logger:         logger,
	}
}

func (o *previewOrchImpl) Execute(ctx context.Context, cmd PreviewCmd) (*PreviewResult, error) {
	// Normalize nil context — consistent with Apply, Compare, and Scrape orchestrators.
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cmd.Movie == nil {
		return nil, fmt.Errorf("movie is nil")
	}

	operationMode := cmd.OperationMode

	movie := cmd.Movie
	fileResults := cmd.FileResults
	destination := cmd.Destination
	skipNFO := cmd.SkipNFO
	skipDownload := cmd.SkipDownload

	// Resolve operation mode: command override wins, else config default.
	if operationMode == "" {
		operationMode = o.previewCfg.OpMode
	}

	sharedEngine := o.templateEngine
	if sharedEngine == nil {
		sharedEngine = template.NewEngine()
	}

	strategy, err := o.createPreviewStrategy(operationMode)
	if err != nil {
		return nil, err
	}

	// Detect source path characteristics and select the appropriate encoding.
	sourcePath := ""
	for _, result := range fileResults {
		if result.Path != "" {
			sourcePath = result.Path
			break
		}
	}

	if sourcePath == "" || sourcePath == "." {
		if operationMode == operationmode.OperationModeInPlaceNoRenameFolder ||
			operationMode == operationmode.OperationModeInPlace ||
			operationMode == operationmode.OperationModeMetadataArtwork {
			return &PreviewResult{OperationMode: operationMode}, nil
		}
	}

	encodingInfo := organizer.DetectPathEncodingInfo(sourcePath, destination)

	return o.executePreview(ctx, movie, fileResults, destination, operationMode, skipNFO, skipDownload, sharedEngine, strategy, encodingInfo), nil
}

// mediaPathsResult holds the resolved media paths for a preview operation.
// Per W-4: extracted from executePreview so that the media path resolution
// is testable independently of the plan computation.
type mediaPathsResult struct {
	FolderName      string
	FileName        string
	SubfolderPath   string
	FolderPath      string
	VideoFiles      []string
	NFOPath         string
	NFOPaths        []string
	PosterPath      string
	FanartPath      string
	ExtrafanartPath string
	Screenshots     []string
	TrailerPath     string
	SourcePath      string
}

// resolveMediaPaths computes the media output paths from an already-computed
// primary plan and encoded paths. Per W-4: this isolates the NFO/poster/fanart/
// trailer path resolution from the plan computation and result assembly.
func (o *previewOrchImpl) resolveMediaPaths(
	movie *models.Movie,
	fileResults []models.FileMatchInfo,
	primaryPlan *organizer.OrganizePlan,
	encoded organizer.EncodedPaths,
	videoFiles []string,
	operationMode operationmode.OperationMode,
	skipNFO bool,
	skipDownload bool,
) mediaPathsResult {
	folderPath := encoded.TargetDir
	subfolderPath := encoded.SubfolderPath
	folderName := primaryPlan.FolderName
	fileName := primaryPlan.BaseFileName

	// Build a template context with GroupActress/GroupActressName set so that
	// preview paths match the actual output (e.g., @Group folder naming).
	previewTmplCtx := template.NewContextFromMovie(movie)
	previewTmplCtx.GroupActress = o.previewCfg.PathCfg.GroupActress
	previewTmplCtx.GroupActressName = o.previewCfg.PathCfg.GroupActressName

	var nfoPath string
	var nfoPaths []string
	if !skipNFO {
		nfoPath, nfoPaths = generateNFOPaths(movie, fileResults, o.nfoNameCfg, o.previewCfg.NFOPerFile, o.previewCfg.NFOEnabled, o.nfoIface, folderPath)
	}

	var posterPath, fanartPath string
	var extrafanartPath string
	var screenshots []string
	if !skipDownload {
		posterPath = o.pathResolver.ResolvePosterPath(movie, fileResults, o.previewCfg.Downloads.Poster, previewTmplCtx, folderPath)
		fanartPath = o.pathResolver.ResolveFanartPath(movie, fileResults, o.previewCfg.Downloads.Cover, previewTmplCtx, folderPath)
		if o.previewCfg.Downloads.Extrafanart {
			extrafanartPath = organizer.JoinPath(folderPath, o.previewCfg.PathCfg.ScreenshotFolder)
		}
		screenshots = o.pathResolver.ResolveScreenshotNames(movie, o.previewCfg.Downloads.Extrafanart, previewTmplCtx)
	}

	var trailerPath string
	if !skipDownload {
		trailerPath = o.pathResolver.ResolveTrailerPath(movie, o.previewCfg.Downloads.Trailer, previewTmplCtx, folderPath)
	}

	sourcePathField := ""
	if operationMode != operationmode.OperationModeOrganize && operationMode != "" {
		sourcePathField = encoded.SourcePath
	}

	return mediaPathsResult{
		FolderName:      folderName,
		FileName:        fileName,
		SubfolderPath:   subfolderPath,
		FolderPath:      folderPath,
		VideoFiles:      videoFiles,
		NFOPath:         nfoPath,
		NFOPaths:        nfoPaths,
		PosterPath:      posterPath,
		FanartPath:      fanartPath,
		ExtrafanartPath: extrafanartPath,
		Screenshots:     screenshots,
		TrailerPath:     trailerPath,
		SourcePath:      sourcePathField,
	}
}

// executePreview is the unified pipeline for both UNC and non-UNC paths.
// Path encoding is delegated to OrganizePlan.EncodePaths() per ADR-0036.
//
// Per W-4: the pipeline is now a 3-step orchestrator:
//  1. Compute plans (strategy.Plan for each file result)
//  2. Resolve media paths (NFO, poster, fanart, trailer)
//  3. Build result (validate path lengths, assemble PreviewResult)
func (o *previewOrchImpl) executePreview(
	ctx context.Context,
	movie *models.Movie,
	fileResults []models.FileMatchInfo,
	destination string,
	operationMode operationmode.OperationMode,
	skipNFO bool,
	skipDownload bool,
	sharedEngine template.EngineInterface,
	strategy organizer.OperationStrategy,
	encodingInfo organizer.PathEncodingInfo,
) *PreviewResult {
	preparedDest := encodingInfo.PrepareDestination(destination)

	// Step 1: Compute plans.
	var primaryPlan *organizer.OrganizePlan
	videoFiles := make([]string, 0, len(fileResults))

	for _, result := range fileResults {
		if ctx.Err() != nil {
			return &PreviewResult{OperationMode: operationMode}
		}
		if result.Path == "" {
			continue
		}

		match := result
		match.Path = encodingInfo.PrepareMatchPath(match.Path)

		plan, err := strategy.Plan(match, movie, preparedDest, false)
		if err != nil {
			resolveLogger(o.logger).Warnf("Preview: strategy.Plan failed for %s: %v", result.Path, err)
			continue
		}

		if primaryPlan == nil {
			primaryPlan = plan
		}

		encoded := plan.EncodePaths(encodingInfo)
		videoFiles = append(videoFiles, encoded.TargetPath)
	}

	// Check for cancellation before proceeding to NFO/poster generation.
	if ctx.Err() != nil {
		return &PreviewResult{OperationMode: operationMode}
	}

	// Fallback: if no plan was produced, try the first valid file result
	// or synthesize a models.FileMatchInfo from the movie ID.
	if primaryPlan == nil {
		first := models.FirstValidFileResult(fileResults)
		if first != nil {
			match := *first
			match.Path = encodingInfo.PrepareMatchPath(match.Path)
			plan, err := strategy.Plan(match, movie, preparedDest, false)
			if err != nil {
				resolveLogger(o.logger).Warnf("Preview: strategy.Plan failed for first valid file: %v", err)
				return &PreviewResult{OperationMode: operationMode}
			}
			primaryPlan = plan
			encoded := plan.EncodePaths(encodingInfo)
			videoFiles = append(videoFiles, encoded.TargetPath)
		} else {
			syntheticName := movie.ID + ".mp4"
			match := models.FileMatchInfo{
				Path:      "",
				Name:      syntheticName,
				Extension: ".mp4",
				MovieID:   movie.ID,
			}
			plan, err := strategy.Plan(match, movie, preparedDest, false)
			if err != nil {
				resolveLogger(o.logger).Warnf("Preview: strategy.Plan failed for synthetic file: %v", err)
				return &PreviewResult{OperationMode: operationMode}
			}
			primaryPlan = plan
			encoded := plan.EncodePaths(encodingInfo)
			videoFiles = append(videoFiles, encoded.TargetPath)
		}
	}

	if primaryPlan == nil {
		return &PreviewResult{OperationMode: operationMode}
	}

	// Step 2: Resolve media paths.
	encoded := primaryPlan.EncodePaths(encodingInfo)
	mediaPaths := o.resolveMediaPaths(movie, fileResults, primaryPlan, encoded, videoFiles, operationMode, skipNFO, skipDownload)

	// Step 3: Validate path lengths and build result.
	validatePathLengths(o.logger, o.previewCfg.MaxPathLength, sharedEngine, mediaPaths.VideoFiles, mediaPaths.NFOPath, mediaPaths.NFOPaths, mediaPaths.PosterPath, mediaPaths.FanartPath, mediaPaths.ExtrafanartPath, mediaPaths.Screenshots)

	var firstVideo string
	if len(mediaPaths.VideoFiles) > 0 {
		firstVideo = mediaPaths.VideoFiles[0]
	}

	return &PreviewResult{
		FolderName:      mediaPaths.FolderName,
		FileName:        mediaPaths.FileName,
		SubfolderPath:   mediaPaths.SubfolderPath,
		FullPath:        firstVideo,
		VideoFiles:      mediaPaths.VideoFiles,
		NFOPath:         mediaPaths.NFOPath,
		NFOPaths:        mediaPaths.NFOPaths,
		PosterPath:      mediaPaths.PosterPath,
		FanartPath:      mediaPaths.FanartPath,
		ExtrafanartPath: mediaPaths.ExtrafanartPath,
		Screenshots:     mediaPaths.Screenshots,
		TrailerPath:     mediaPaths.TrailerPath,
		SourcePath:      mediaPaths.SourcePath,
		OperationMode:   operationMode,
	}
}

func (o *previewOrchImpl) createPreviewStrategy(operationMode operationmode.OperationMode) (organizer.OperationStrategy, error) {
	// Per DEEP-5: delegate to the strategy resolver function provided by the
	// factory, instead of carrying the full *organizer.Config and calling
	// ResolveStrategy directly. This decouples the preview orchestrator from
	// organizer config changes that don't affect path computation.
	if o.previewCfg.ResolveStrategy != nil {
		return o.previewCfg.ResolveStrategy(operationMode), nil
	}
	// Fallback: construct a minimal strategy if no resolver was provided.
	// This preserves backward compatibility for tests that construct
	// PreviewConfig without a ResolveStrategy function.
	if o.fs == nil {
		return nil, fmt.Errorf("filesystem not configured")
	}
	strategyCfg := &organizer.Config{OperationMode: operationMode}
	return organizer.ResolveStrategy(o.fs, strategyCfg, o.matcher, o.templateEngine), nil
}

// noOpPreviewOrchestrator returns an error when Preview is called on a Workflow
// that was not configured for preview (e.g., scan-only mode via WorkflowFactory).
type noOpPreviewOrchestrator struct{}

var _ previewOrchestrator = (*noOpPreviewOrchestrator)(nil)

func (noOpPreviewOrchestrator) Execute(_ context.Context, _ PreviewCmd) (*PreviewResult, error) {
	return nil, fmt.Errorf("preview not configured")
}
