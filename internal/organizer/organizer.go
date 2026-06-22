package organizer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

var videoExtensions = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".wmv": true,
	".flv": true, ".mov": true, ".m4v": true, ".webm": true,
	".mpg": true, ".mpeg": true, ".m2ts": true, ".ts": true,
}

// resolveFileName generates the target filename from the template, falling back
// to the match ID (then original filename) when sanitization produces an empty string.
// This prevents creating paths like "/dest/.mkv" when template fields are all empty.
func resolveFileName(cfg *Config, engine template.EngineInterface, ctx *template.Context, match models.FileMatchInfo) (string, error) {
	fileName, err := engine.Execute(cfg.FileFormat, ctx)
	if err != nil {
		return "", fmt.Errorf("failed to generate file name: %w", err)
	}

	fileName = template.SanitizeFilename(fileName)

	if fileName == "" {
		if match.MovieID != "" {
			fileName = template.SanitizeFilename(match.MovieID)
		}
		if fileName == "" {
			fileName = template.SanitizeFilename(strings.TrimSuffix(match.Name, match.Extension))
		}
		if fileName == "" && match.Path != "" {
			fileName = template.SanitizeFilename(strings.TrimSuffix(filepath.Base(match.Path), match.Extension))
		}
		if fileName == "" {
			fileName = "file"
		}
		logging.Warnf("[%s] Template produced empty filename after sanitization, falling back to %q", match.MovieID, fileName)
	}

	fileName = fileName + match.Extension
	return fileName, nil
}

func resolveBaseFileName(cfg *Config, engine template.EngineInterface, movie *models.Movie, match models.FileMatchInfo) string {
	if cfg.RenameFile {
		baseCtx := template.NewContextFromMovie(movie)
		baseCtx.GroupActress = cfg.GroupActress
		baseCtx.GroupActressName = cfg.GroupActressName
		baseCtx.GroupUnknownActressName = cfg.GroupUnknownActressName
		baseCtx.FirstNameOrder = cfg.FirstNameOrder
		baseCtx.ActressLanguageJa = cfg.ActressLanguageJA
		baseCtx.ActressDelimiter = cfg.ActressDelimiter
		applyTitleTruncation(engine, baseCtx, cfg.MaxTitleLength)

		rendered, err := engine.Execute(cfg.FileFormat, baseCtx)
		if err == nil {
			sanitized := template.SanitizeFilename(rendered)
			if sanitized != "" {
				return sanitized
			}
		}
		if match.MovieID != "" {
			if sanitized := template.SanitizeFilename(match.MovieID); sanitized != "" {
				return sanitized
			}
		}
		if name := template.SanitizeFilename(strings.TrimSuffix(match.Name, match.Extension)); name != "" {
			return name
		}
		if match.Path != "" {
			if name := template.SanitizeFilename(strings.TrimSuffix(filepath.Base(match.Path), match.Extension)); name != "" {
				return name
			}
		}
		return "file"
	}
	base := template.SanitizeFilename(strings.TrimSuffix(match.Name, match.Extension))
	if base != "" {
		return base
	}
	if match.Path != "" {
		if pathBase := template.SanitizeFilename(strings.TrimSuffix(filepath.Base(match.Path), match.Extension)); pathBase != "" {
			return pathBase
		}
	}
	if match.MovieID != "" {
		if sanitized := template.SanitizeFilename(match.MovieID); sanitized != "" {
			return sanitized
		}
	}
	return "file"
}

func applyTitleTruncation(engine template.EngineInterface, ctx *template.Context, maxLen int) {
	if maxLen <= 0 {
		return
	}
	ctx.Title = engine.TruncateTitle(ctx.Title, maxLen)
	ctx.OriginalTitle = engine.TruncateTitle(ctx.OriginalTitle, maxLen)
}

func checkTargetConflict(fs afero.Fs, sourcePath, targetPath string, forceUpdate, willMove bool) []string {
	conflicts := make([]string, 0)
	if forceUpdate || !willMove {
		return conflicts
	}
	stat, err := fs.Stat(targetPath)
	if err != nil {
		return conflicts
	}
	sourceStat, sourceErr := fs.Stat(sourcePath)
	if sourceErr == nil && os.SameFile(sourceStat, stat) {
		return conflicts
	}
	conflicts = append(conflicts, targetPath)
	return conflicts
}

type planContext struct {
	Ctx        *template.Context
	FileName   string
	FolderName string
	Err        error
}

func buildPlanContext(cfg *Config, engine template.EngineInterface, movie *models.Movie, match models.FileMatchInfo) planContext {
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = cfg.GroupActress
	ctx.GroupActressName = cfg.GroupActressName
	ctx.GroupUnknownActressName = cfg.GroupUnknownActressName
	ctx.FirstNameOrder = cfg.FirstNameOrder
	ctx.ActressLanguageJa = cfg.ActressLanguageJA
	ctx.ActressDelimiter = cfg.ActressDelimiter

	applyTitleTruncation(engine, ctx, cfg.MaxTitleLength)

	ctx.PartNumber = match.PartNumber
	ctx.PartSuffix = match.PartSuffix
	ctx.IsMultiPart = match.IsMultiPart

	var fileName string
	var err error
	if cfg.RenameFile {
		fileName, err = resolveFileName(cfg, engine, ctx, match)
		if err != nil {
			return planContext{Err: err}
		}
	} else {
		fileName = match.Name
		if fileName == "" && match.Path != "" {
			fileName = filepath.Base(match.Path)
		}
	}

	var folderName string
	folderName, err = engine.Execute(cfg.FolderFormat, ctx)
	if err != nil {
		return planContext{Err: fmt.Errorf("failed to generate folder name: %w", err)}
	}

	folderName = template.SanitizeFolderPath(folderName)
	if folderName == "" {
		folderName = template.SanitizeFolderPath(match.MovieID)
		if folderName == "" {
			folderName = "unknown"
		}
	}

	return planContext{
		Ctx:        ctx,
		FileName:   fileName,
		FolderName: folderName,
	}
}

// OrganizeCmd carries all parameters for the single-method Organize seam.
// Per Phase 48: replaces the fixed-sequence Plan→ValidatePlan→Execute/Copy
// protocol with one command struct.
type OrganizeCmd struct {
	Match       models.FileMatchInfo
	Movie       *models.Movie
	DestDir     string
	ForceUpdate bool
	MoveFiles   bool     // true = move files, false = copy/link
	LinkMode    LinkMode // Only relevant when MoveFiles=false
	DryRun      bool
}

// OrganizerInterface is the single-method seam for file organization.
// Per Phase 48: Plan/ValidatePlan/Execute are internal implementation details
// of the Organize method — callers invoke one method instead of a fixed sequence.
type OrganizerInterface interface {
	Organize(ctx context.Context, cmd OrganizeCmd) (*OrganizeResult, error)
}

var _ OrganizerInterface = (*Organizer)(nil)

// Organizer handles file organization (moving/renaming)
type Organizer struct {
	fs              afero.Fs
	config          *Config
	templateEngine  template.EngineInterface
	subtitleHandler *subtitleHandler
	matcher         matcher.MatcherInterface
	linker          linker
}

// NewOrganizer creates a new file organizer
func NewOrganizer(fs afero.Fs, cfg *Config, engine template.EngineInterface, m matcher.MatcherInterface) *Organizer {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &Organizer{
		fs:              fs,
		config:          cfg,
		templateEngine:  engine,
		subtitleHandler: newSubtitleHandler(fs, cfg.SubtitleExtensions),
		matcher:         m,
		linker:          OSLinker{},
	}
}

// OrganizeResult represents the result of organizing a file
type OrganizeResult struct {
	OriginalPath           string
	NewPath                string
	FolderPath             string
	FileName               string
	Moved                  bool
	Error                  error
	Subtitles              []subtitleResult
	InPlaceRenamed         bool   // Whether an in-place directory rename occurred
	OldDirectoryPath       string // Original directory path (for updating subsequent file paths)
	NewDirectoryPath       string // New directory path after in-place rename
	ShouldGenerateMetadata bool   // Whether NFO/media should be generated for this result
}

type subtitleResult struct {
	models.SubtitleMove
	Skipped bool
	Planned bool
	Error   error
}

// strategyType is an internal enum identifying the operation strategy.
// It is unexported — external consumers use behavior flags on OrganizePlan instead.
type strategyType int

const (
	strategyOrganize strategyType = iota
	strategyInPlace
	strategyInPlaceNoRenameFolder
	strategyMetadataArtwork
)

// OrganizePlan represents a planned file organization operation.
type OrganizePlan struct {
	Match             models.FileMatchInfo
	Movie             *models.Movie
	SourcePath        string
	TargetDir         string
	TargetFile        string
	TargetPath        string
	WillMove          bool
	Conflicts         []string
	InPlace           bool
	OldDir            string
	IsDedicated       bool
	SkipInPlaceReason string
	FolderName        string
	SubfolderPath     string
	BaseFileName      string

	// Behavior flags for external consumers (e.g., preview orchestrator).
	// These replace direct StrategyType comparisons.
	PreserveSourcePath bool // true = metadata-artwork or in-place-norenamefolder: keep files in original directory
	RenameFolder       bool // true = in-place with dedicated folder: rename the folder, not just the file

	strategy        strategyType
	executeStrategy OperationStrategy
	LinkMode        LinkMode
	moveFiles       bool // true = move (rename); false = copy/link — determines which branch strategy.Execute takes
}

// Plan creates an organization plan without executing it
func (o *Organizer) resolveStrategy() OperationStrategy {
	return ResolveStrategy(o.fs, o.config, o.matcher, o.templateEngine)
}

func ResolveStrategy(fs afero.Fs, cfg *Config, m matcher.MatcherInterface, engine template.EngineInterface) OperationStrategy {
	switch cfg.OperationMode {
	case operationmode.OperationModeOrganize:
		return newOrganizeStrategy(fs, cfg, engine, OSLinker{})
	case operationmode.OperationModeInPlace:
		return newInPlaceStrategy(fs, cfg, m, engine)
	case operationmode.OperationModeInPlaceNoRenameFolder:
		return newInPlaceNoRenameFolderStrategy(fs, cfg, m, engine)
	case operationmode.OperationModeMetadataArtwork, operationmode.OperationModePreview:
		return newMetadataArtworkStrategy(fs, cfg)
	default:
		return newOrganizeStrategy(fs, cfg, engine, OSLinker{})
	}
}

func (o *Organizer) strategyFromType(st strategyType) OperationStrategy {
	switch st {
	case strategyInPlace:
		return newInPlaceStrategy(o.fs, o.config, o.matcher, o.templateEngine)
	case strategyInPlaceNoRenameFolder:
		return newInPlaceNoRenameFolderStrategy(o.fs, o.config, o.matcher, o.templateEngine)
	case strategyMetadataArtwork:
		return newMetadataArtworkStrategy(o.fs, o.config)
	default:
		return newOrganizeStrategy(o.fs, o.config, o.templateEngine, o.linker)
	}
}

func (o *Organizer) plan(match models.FileMatchInfo, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	return o.resolveStrategy().Plan(match, movie, destDir, forceUpdate)
}

// execute executes an organization plan
//
//nolint:unused
func (o *Organizer) execute(plan *OrganizePlan) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: false,
	}

	if len(plan.Conflicts) > 0 {
		result.Error = fmt.Errorf("conflicts detected: %s", strings.Join(plan.Conflicts, "; "))
		return result, result.Error
	}

	if !plan.WillMove {
		result.ShouldGenerateMetadata = true
		o.handleSubtitles(plan, result, nil)
		return result, nil
	}

	strategy := plan.executeStrategy
	if strategy == nil {
		strategy = o.strategyFromType(plan.strategy)
	}

	strategyResult, err := strategy.Execute(plan)
	if err != nil {
		return strategyResult, err
	}

	if o.config.MoveSubtitles {
		o.handleSubtitles(plan, strategyResult, fsutil.MoveFileFs)
	}

	return strategyResult, nil
}

func (o *Organizer) subtitleFileInfo(plan *OrganizePlan) models.FileMatchInfo {
	fileInfoForSubtitles := models.FileMatchInfo{
		Path:      plan.Match.Path,
		Name:      plan.Match.Name,
		Extension: plan.Match.Extension,
		Size:      plan.Match.Size,
		ModTime:   plan.Match.ModTime,
	}
	if plan.InPlace {
		fileInfoForSubtitles.Path = plan.TargetPath
		oldFileName := plan.Match.Name
		if oldFileName == "" && plan.Match.Path != "" {
			oldFileName = filepath.Base(plan.Match.Path)
		}
		if oldFileName != "" && oldFileName != plan.TargetFile {
			fileInfoForSubtitles.Path = filepath.Join(plan.TargetDir, oldFileName)
		}
	}
	return fileInfoForSubtitles
}

func (o *Organizer) handleSubtitles(plan *OrganizePlan, result *OrganizeResult, fileOp func(afero.Fs, string, string) error) {
	subtitles := o.subtitleHandler.FindSubtitles(o.subtitleFileInfo(plan))
	if len(subtitles) == 0 {
		return
	}

	subtitleResults := make([]subtitleResult, len(subtitles))
	for i, subtitle := range subtitles {
		videoNameWithoutExt := strings.TrimSuffix(plan.TargetFile, filepath.Ext(plan.TargetFile))
		newSubtitleName := o.subtitleHandler.generateSubtitleFileName(
			videoNameWithoutExt,
			subtitle.Language,
			subtitle.Extension,
		)
		newPath := filepath.Join(plan.TargetDir, newSubtitleName)

		if fileOp == nil {
			subtitleResults[i] = subtitleResult{
				SubtitleMove: models.SubtitleMove{
					OriginalPath: subtitle.OriginalPath,
					NewPath:      newPath,
				},
				Planned: true,
			}
			continue
		}

		sr := subtitleResult{
			SubtitleMove: models.SubtitleMove{
				OriginalPath: subtitle.OriginalPath,
				NewPath:      newPath,
			},
		}

		if _, err := o.fs.Stat(newPath); err == nil {
			sr.Skipped = true
		} else if err := fileOp(o.fs, subtitle.OriginalPath, newPath); err != nil {
			sr.Error = fmt.Errorf("failed to handle subtitle: %w", err)
		} else {
			sr.Moved = true
		}

		subtitleResults[i] = sr
	}
	result.Subtitles = subtitleResults
}

// Organize is the single-method seam that plans, validates, and executes
// file organization in one call. Per Phase 48: Plan/ValidatePlan/Execute
// are internal implementation details — callers use Organize instead of a
// fixed sequence.
func (o *Organizer) Organize(ctx context.Context, cmd OrganizeCmd) (*OrganizeResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	plan, err := o.plan(cmd.Match, cmd.Movie, cmd.DestDir, cmd.ForceUpdate)
	if err != nil {
		return nil, err
	}

	if !cmd.ForceUpdate {
		if issues := o.validatePlan(plan); len(issues) > 0 {
			return nil, fmt.Errorf("organization validation failed: %v", issues)
		}
	}

	// Propagate link mode to plan for strategy consumption.
	// When MoveFiles=true, LinkMode stays LinkModeNone (zero value) — strategy does a move.
	// When MoveFiles=false, LinkMode is set from the command — strategy does copy/link.
	plan.moveFiles = cmd.MoveFiles
	if !cmd.MoveFiles {
		plan.LinkMode = cmd.LinkMode
	}

	// Check for conflicts before executing
	if len(plan.Conflicts) > 0 {
		return nil, fmt.Errorf("conflicts detected: %s", strings.Join(plan.Conflicts, "; "))
	}

	// Dry-run: return early with planned result (no filesystem changes).
	if cmd.DryRun {
		return &OrganizeResult{
			OriginalPath:           plan.SourcePath,
			NewPath:                plan.TargetPath,
			FolderPath:             plan.TargetDir,
			FileName:               plan.TargetFile,
			Moved:                  false,
			ShouldGenerateMetadata: true,
		}, nil
	}

	strategy := plan.executeStrategy
	if strategy == nil {
		strategy = o.strategyFromType(plan.strategy)
	}

	strategyResult, err := strategy.Execute(plan)
	if err != nil {
		return strategyResult, err
	}

	// Subtitle handling is centralized here — applies to both move and copy/link paths.
	if cmd.MoveFiles && o.config.MoveSubtitles {
		o.handleSubtitles(plan, strategyResult, fsutil.MoveFileFs)
	} else if !cmd.MoveFiles && o.config.MoveSubtitles {
		o.handleSubtitles(plan, strategyResult, fsutil.CopyFileFs)
	}

	return strategyResult, nil
}

// validatePlan checks if a plan is valid and safe to execute
func (o *Organizer) validatePlan(plan *OrganizePlan) []string {
	issues := make([]string, 0)

	// Check for conflicts
	issues = append(issues, plan.Conflicts...)

	// Check source exists
	if _, err := o.fs.Stat(plan.SourcePath); os.IsNotExist(err) {
		issues = append(issues, fmt.Sprintf("source file does not exist: %s", plan.SourcePath))
	}

	// Check folder name is not empty
	if plan.TargetDir == "" || plan.TargetFile == "" {
		issues = append(issues, "target directory or filename is empty")
	}

	// Check for invalid characters in paths
	if strings.Contains(plan.TargetPath, "//") {
		issues = append(issues, "target path contains double slashes")
	}

	return issues
}
