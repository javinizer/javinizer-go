package organizer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
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
func resolveFileName(cfg *config.OutputConfig, engine *template.Engine, ctx *template.Context, match matcher.MatchResult) (string, error) {
	fileName, err := engine.Execute(cfg.FileFormat, ctx)
	if err != nil {
		return "", fmt.Errorf("failed to generate file name: %w", err)
	}

	fileName = template.SanitizeFilename(fileName)

	if fileName == "" {
		if match.ID != "" {
			fileName = template.SanitizeFilename(match.ID)
		}
		if fileName == "" {
			fileName = template.SanitizeFilename(strings.TrimSuffix(match.File.Name, match.File.Extension))
		}
		if fileName == "" && match.File.Path != "" {
			fileName = template.SanitizeFilename(strings.TrimSuffix(filepath.Base(match.File.Path), match.File.Extension))
		}
		if fileName == "" {
			fileName = "file"
		}
		logging.Warnf("[%s] Template produced empty filename after sanitization, falling back to %q", match.ID, fileName)
	}

	fileName = fileName + match.File.Extension
	return fileName, nil
}

func resolveBaseFileName(cfg *config.OutputConfig, engine *template.Engine, movie *models.Movie, match matcher.MatchResult) string {
	if cfg.RenameFile {
		baseCtx := template.NewContextFromMovie(movie)
		baseCtx.GroupActress = cfg.GroupActress
		applyTitleTruncation(engine, baseCtx, cfg.MaxTitleLength)

		rendered, err := engine.Execute(cfg.FileFormat, baseCtx)
		if err == nil {
			sanitized := template.SanitizeFilename(rendered)
			if sanitized != "" {
				return sanitized
			}
		}
		if match.ID != "" {
			if sanitized := template.SanitizeFilename(match.ID); sanitized != "" {
				return sanitized
			}
		}
		if name := template.SanitizeFilename(strings.TrimSuffix(match.File.Name, match.File.Extension)); name != "" {
			return name
		}
		if match.File.Path != "" {
			if name := template.SanitizeFilename(strings.TrimSuffix(filepath.Base(match.File.Path), match.File.Extension)); name != "" {
				return name
			}
		}
		return "file"
	}
	base := strings.TrimSuffix(match.File.Name, match.File.Extension)
	if base != "" {
		return base
	}
	if match.File.Path != "" {
		if pathBase := strings.TrimSuffix(filepath.Base(match.File.Path), match.File.Extension); pathBase != "" {
			return pathBase
		}
	}
	if match.ID != "" {
		return match.ID
	}
	return "file"
}

func applyTitleTruncation(engine *template.Engine, ctx *template.Context, maxLen int) {
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

// Organizer handles file organization (moving/renaming)
type Organizer struct {
	fs              afero.Fs
	config          *config.OutputConfig
	templateEngine  *template.Engine
	subtitleHandler *SubtitleHandler
	matcher         *matcher.Matcher
}

// LinkMode controls how files are materialized when copy mode is enabled.
type LinkMode string

const (
	LinkModeNone LinkMode = ""
	LinkModeHard LinkMode = "hard"
	LinkModeSoft LinkMode = "soft"
)

// ParseLinkMode validates and normalizes user input.
func ParseLinkMode(raw string) (LinkMode, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "", "none":
		return LinkModeNone, nil
	case string(LinkModeHard):
		return LinkModeHard, nil
	case string(LinkModeSoft):
		return LinkModeSoft, nil
	default:
		return LinkModeNone, fmt.Errorf("invalid link mode %q (expected one of: none, hard, soft)", raw)
	}
}

// IsValid returns true if the mode is supported.
func (m LinkMode) IsValid() bool {
	switch m {
	case LinkModeNone, LinkModeHard, LinkModeSoft:
		return true
	default:
		return false
	}
}

// NewOrganizer creates a new file organizer
func NewOrganizer(fs afero.Fs, cfg *config.OutputConfig, engine *template.Engine) *Organizer {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &Organizer{
		fs:              fs,
		config:          cfg,
		templateEngine:  engine,
		subtitleHandler: NewSubtitleHandler(fs, cfg),
		matcher:         nil, // Set via SetMatcher if needed
	}
}

// SetMatcher sets the matcher instance for in-place rename detection
func (o *Organizer) SetMatcher(m *matcher.Matcher) {
	o.matcher = m
}

// OrganizeResult represents the result of organizing a file
type OrganizeResult struct {
	OriginalPath           string
	NewPath                string
	FolderPath             string
	FileName               string
	Moved                  bool
	Error                  error
	Subtitles              []SubtitleResult
	InPlaceRenamed         bool   // Whether an in-place directory rename occurred
	OldDirectoryPath       string // Original directory path (for updating subsequent file paths)
	NewDirectoryPath       string // New directory path after in-place rename
	ShouldGenerateMetadata bool   // Whether NFO/media should be generated for this result
}

// SubtitleResult represents the result of moving a subtitle file
type SubtitleResult struct {
	OriginalPath string
	NewPath      string
	Moved        bool
	Skipped      bool
	Planned      bool
	Error        error
}

// OrganizePlan represents a planned file organization operation
type StrategyType int

const (
	StrategyTypeOrganize StrategyType = iota
	StrategyTypeInPlace
	StrategyTypeInPlaceNoRenameFolder
	StrategyTypeMetadataOnly
)

type OrganizePlan struct {
	Match             matcher.MatchResult
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
	Strategy          StrategyType
	executeStrategy   OperationStrategy
}

// Plan creates an organization plan without executing it
func (o *Organizer) resolveStrategy() OperationStrategy {
	mode := o.config.GetOperationMode()
	switch mode {
	case "organize":
		return NewOrganizeStrategy(o.fs, o.config, o.templateEngine)
	case "in-place":
		return NewInPlaceStrategy(o.fs, o.config, o.matcher, o.templateEngine)
	case "in-place-norenamefolder":
		return NewInPlaceNoRenameFolderStrategy(o.fs, o.config, o.matcher, o.templateEngine)
	case "metadata-only", "preview":
		return NewMetadataOnlyStrategy(o.fs, o.config)
	default:
		return NewOrganizeStrategy(o.fs, o.config, o.templateEngine)
	}
}

func (o *Organizer) Plan(match matcher.MatchResult, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	return o.resolveStrategy().Plan(match, movie, destDir, forceUpdate)
}

// Execute executes an organization plan
func (o *Organizer) Execute(plan *OrganizePlan, dryRun bool) (*OrganizeResult, error) {
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
		o.planSubtitles(plan, result)
		return result, nil
	}

	if dryRun {
		result.ShouldGenerateMetadata = true
		o.planSubtitles(plan, result)
		return result, nil
	}

	var strategy OperationStrategy
	if plan.executeStrategy != nil {
		strategy = plan.executeStrategy
	} else {
		switch plan.Strategy {
		case StrategyTypeInPlace:
			strategy = NewInPlaceStrategy(o.fs, o.config, o.matcher, o.templateEngine)
		case StrategyTypeInPlaceNoRenameFolder:
			strategy = NewInPlaceNoRenameFolderStrategy(o.fs, o.config, o.matcher, o.templateEngine)
		case StrategyTypeMetadataOnly:
			strategy = NewMetadataOnlyStrategy(o.fs, o.config)
		default:
			strategy = NewOrganizeStrategy(o.fs, o.config, o.templateEngine)
		}
	}

	strategyResult, err := strategy.Execute(plan)
	if err != nil {
		return strategyResult, err
	}

	if o.config.MoveSubtitles {
		o.moveSubtitles(plan, strategyResult)
	}

	return strategyResult, nil
}

func (o *Organizer) subtitleFileInfo(plan *OrganizePlan) scanner.FileInfo {
	fileInfoForSubtitles := plan.Match.File
	if plan.InPlace {
		fileInfoForSubtitles.Path = plan.TargetPath
		oldFileName := plan.Match.File.Name
		if oldFileName == "" && plan.Match.File.Path != "" {
			oldFileName = filepath.Base(plan.Match.File.Path)
		}
		if oldFileName != "" && oldFileName != plan.TargetFile {
			fileInfoForSubtitles.Path = filepath.Join(plan.TargetDir, oldFileName)
		}
	}
	return fileInfoForSubtitles
}

func (o *Organizer) planSubtitles(plan *OrganizePlan, result *OrganizeResult) {
	subtitles := o.subtitleHandler.FindSubtitles(o.subtitleFileInfo(plan))
	if len(subtitles) == 0 {
		return
	}

	subtitleResults := make([]SubtitleResult, len(subtitles))
	for i, subtitle := range subtitles {
		videoNameWithoutExt := strings.TrimSuffix(plan.TargetFile, filepath.Ext(plan.TargetFile))
		newSubtitleName := o.subtitleHandler.generateSubtitleFileName(
			videoNameWithoutExt,
			subtitle.Language,
			subtitle.Extension,
		)
		subtitleResults[i] = SubtitleResult{
			OriginalPath: subtitle.OriginalPath,
			NewPath:      filepath.Join(plan.TargetDir, newSubtitleName),
			Moved:        false,
			Planned:      true,
		}
	}
	result.Subtitles = subtitleResults
}

func (o *Organizer) moveSubtitles(plan *OrganizePlan, result *OrganizeResult) {
	subtitles := o.subtitleHandler.FindSubtitles(o.subtitleFileInfo(plan))
	if len(subtitles) == 0 {
		return
	}

	subtitleResults := make([]SubtitleResult, len(subtitles))
	for i, subtitle := range subtitles {
		subtitleResult := SubtitleResult{
			OriginalPath: subtitle.OriginalPath,
			Moved:        false,
		}

		videoNameWithoutExt := strings.TrimSuffix(plan.TargetFile, filepath.Ext(plan.TargetFile))
		newSubtitleName := o.subtitleHandler.generateSubtitleFileName(
			videoNameWithoutExt,
			subtitle.Language,
			subtitle.Extension,
		)
		subtitleResult.NewPath = filepath.Join(plan.TargetDir, newSubtitleName)

		if _, err := o.fs.Stat(subtitleResult.NewPath); err == nil {
			subtitleResult.Skipped = true
		} else if err := fsutil.MoveFileFs(o.fs, subtitle.OriginalPath, subtitleResult.NewPath); err != nil {
			subtitleResult.Error = fmt.Errorf("failed to move subtitle: %w", err)
		} else {
			subtitleResult.Moved = true
		}

		subtitleResults[i] = subtitleResult
	}
	result.Subtitles = subtitleResults
}

func (o *Organizer) copySubtitles(plan *OrganizePlan, result *OrganizeResult) {
	subtitles := o.subtitleHandler.FindSubtitles(o.subtitleFileInfo(plan))
	if len(subtitles) == 0 {
		return
	}

	subtitleResults := make([]SubtitleResult, len(subtitles))
	for i, subtitle := range subtitles {
		subtitleResult := SubtitleResult{
			OriginalPath: subtitle.OriginalPath,
			Moved:        false,
		}

		videoNameWithoutExt := strings.TrimSuffix(plan.TargetFile, filepath.Ext(plan.TargetFile))
		newSubtitleName := o.subtitleHandler.generateSubtitleFileName(
			videoNameWithoutExt,
			subtitle.Language,
			subtitle.Extension,
		)
		subtitleResult.NewPath = filepath.Join(plan.TargetDir, newSubtitleName)

		if _, err := o.fs.Stat(subtitleResult.NewPath); err == nil {
			subtitleResult.Skipped = true
		} else if err := fsutil.CopyFileFs(o.fs, subtitle.OriginalPath, subtitleResult.NewPath); err != nil {
			subtitleResult.Error = fmt.Errorf("failed to copy subtitle: %w", err)
		} else {
			subtitleResult.Moved = true
		}

		subtitleResults[i] = subtitleResult
	}
	result.Subtitles = subtitleResults
}

// Organize plans and executes file organization in one step
func (o *Organizer) Organize(match matcher.MatchResult, movie *models.Movie, destDir string, dryRun bool, forceUpdate bool, copyOnly bool) (*OrganizeResult, error) {
	return o.OrganizeWithLinkMode(match, movie, destDir, dryRun, forceUpdate, copyOnly, LinkModeNone)
}

// OrganizeWithLinkMode plans and executes file organization with optional link behavior for copy operations.
func (o *Organizer) OrganizeWithLinkMode(
	match matcher.MatchResult,
	movie *models.Movie,
	destDir string,
	dryRun bool,
	forceUpdate bool,
	copyOnly bool,
	linkMode LinkMode,
) (*OrganizeResult, error) {
	plan, err := o.Plan(match, movie, destDir, forceUpdate)
	if err != nil {
		return nil, err
	}

	if copyOnly {
		return o.CopyWithLinkMode(plan, dryRun, linkMode)
	}
	return o.Execute(plan, dryRun)
}

// OrganizeBatch organizes multiple files
func (o *Organizer) OrganizeBatch(matches []matcher.MatchResult, movies map[string]*models.Movie, destDir string, dryRun bool, forceUpdate bool, copyOnly bool) ([]OrganizeResult, error) {
	return o.OrganizeBatchWithLinkMode(matches, movies, destDir, dryRun, forceUpdate, copyOnly, LinkModeNone)
}

// OrganizeBatchWithLinkMode organizes multiple files with optional link behavior for copy operations.
func (o *Organizer) OrganizeBatchWithLinkMode(
	matches []matcher.MatchResult,
	movies map[string]*models.Movie,
	destDir string,
	dryRun bool,
	forceUpdate bool,
	copyOnly bool,
	linkMode LinkMode,
) ([]OrganizeResult, error) {
	results := make([]OrganizeResult, 0, len(matches))

	// Group by ID to process multi-part sets together
	grouped := matcher.GroupByID(matches)

	// Stable process: deterministic ID order
	ids := make([]string, 0, len(grouped))
	for id := range grouped {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		group := grouped[id]

		// Sort parts: 0 (single/no suffix) first, then 1..N
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].PartNumber < group[j].PartNumber
		})

		// Track directory renames for multi-part path updates
		var lastInPlaceRename *OrganizeResult

		for idx := range group {
			match := group[idx] // Use index to get mutable reference

			// If a previous part in this group triggered an in-place directory rename,
			// update this match's path to reflect the new directory
			if lastInPlaceRename != nil && lastInPlaceRename.InPlaceRenamed {
				oldDir := lastInPlaceRename.OldDirectoryPath
				newDir := lastInPlaceRename.NewDirectoryPath

				// Check if this match's path is in the old directory
				if filepath.Dir(match.File.Path) == oldDir {
					// Update path to new directory
					oldFileName := filepath.Base(match.File.Path)
					match.File.Path = filepath.Join(newDir, oldFileName)
					group[idx] = match // Update the slice
				}
			}

			movie, exists := movies[match.ID]
			if !exists {
				results = append(results, OrganizeResult{
					OriginalPath: match.File.Path,
					Error:        fmt.Errorf("no movie data found for ID: %s", match.ID),
				})
				continue
			}

			result, err := o.OrganizeWithLinkMode(match, movie, destDir, dryRun, forceUpdate, copyOnly, linkMode)
			if err != nil {
				result = &OrganizeResult{
					OriginalPath: match.File.Path,
					Error:        err,
				}
			}

			// Track in-place renames for subsequent parts
			if result.InPlaceRenamed {
				lastInPlaceRename = result
			}

			results = append(results, *result)
		}
	}

	return results, nil
}

// Copy copies a file instead of moving it
func (o *Organizer) Copy(plan *OrganizePlan, dryRun bool) (*OrganizeResult, error) {
	return o.CopyWithLinkMode(plan, dryRun, LinkModeNone)
}

// CopyWithLinkMode materializes a file using direct copy, hard link, or soft link.
func (o *Organizer) CopyWithLinkMode(plan *OrganizePlan, dryRun bool, linkMode LinkMode) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: false,
	}

	// Check for conflicts
	if len(plan.Conflicts) > 0 {
		result.Error = fmt.Errorf("conflicts detected: %s", strings.Join(plan.Conflicts, "; "))
		return result, result.Error
	}

	if !plan.WillMove {
		result.ShouldGenerateMetadata = true
		o.planSubtitles(plan, result)
		return result, nil
	}

	if dryRun {
		result.ShouldGenerateMetadata = true
		o.planSubtitles(plan, result)
		return result, nil
	}

	// Create target directory
	if err := o.fs.MkdirAll(plan.TargetDir, config.DirPerm); err != nil {
		result.Error = fmt.Errorf("failed to create directory: %w", err)
		return result, result.Error
	}

	if !linkMode.IsValid() {
		result.Error = fmt.Errorf("unsupported link mode %q", linkMode)
		return result, result.Error
	}

	// Allow force-update workflows to replace an existing target when linking.
	if linkMode != LinkModeNone {
		if err := o.fs.Remove(plan.TargetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			result.Error = fmt.Errorf("failed to prepare target path for link: %w", err)
			return result, result.Error
		}
	}

	switch linkMode {
	case LinkModeNone:
		sourceFile, err := o.fs.Open(plan.SourcePath)
		if err != nil {
			result.Error = fmt.Errorf("failed to open source file: %w", err)
			return result, result.Error
		}
		defer func() { _ = sourceFile.Close() }()

		targetFile, err := o.fs.Create(plan.TargetPath)
		if err != nil {
			result.Error = fmt.Errorf("failed to create target file: %w", err)
			return result, result.Error
		}
		defer func() {
			if closeErr := targetFile.Close(); closeErr != nil && result.Error == nil {
				result.Error = fmt.Errorf("failed to close copied file: %w", closeErr)
			}
		}()

		if _, err := io.Copy(targetFile, sourceFile); err != nil {
			result.Error = fmt.Errorf("failed to copy file: %w", err)
			return result, result.Error
		}

		if err := targetFile.Sync(); err != nil {
			result.Error = fmt.Errorf("failed to sync copied file: %w", err)
			return result, result.Error
		}
	case LinkModeHard:
		if err := os.Link(plan.SourcePath, plan.TargetPath); err != nil {
			if errors.Is(err, syscall.EXDEV) {
				result.Error = fmt.Errorf("failed to create hard link (source and destination must be on the same filesystem): %w", err)
				return result, result.Error
			}
			if errors.Is(err, os.ErrPermission) {
				result.Error = fmt.Errorf("failed to create hard link (permission denied): %w", err)
				return result, result.Error
			}
			result.Error = fmt.Errorf("failed to create hard link: %w", err)
			return result, result.Error
		}
	case LinkModeSoft:
		linkTarget := plan.SourcePath
		if !filepath.IsAbs(linkTarget) {
			abs, err := filepath.Abs(linkTarget)
			if err != nil {
				result.Error = fmt.Errorf("failed to resolve source path for symlink: %w", err)
				return result, result.Error
			}
			linkTarget = abs
		}
		if err := os.Symlink(linkTarget, plan.TargetPath); err != nil {
			if errors.Is(err, os.ErrPermission) && runtime.GOOS == "windows" {
				result.Error = fmt.Errorf("failed to create soft link (Windows requires Developer Mode or elevated privileges for symlinks): %w", err)
				return result, result.Error
			}
			if errors.Is(err, os.ErrPermission) {
				result.Error = fmt.Errorf("failed to create soft link (permission denied): %w", err)
				return result, result.Error
			}
			result.Error = fmt.Errorf("failed to create soft link: %w", err)
			return result, result.Error
		}
	}

	result.Moved = true // "Moved" means operation succeeded (even though it's a copy)
	result.ShouldGenerateMetadata = true

	if o.config.MoveSubtitles {
		o.copySubtitles(plan, result)
	}

	return result, nil
}

// ValidatePlan checks if a plan is valid and safe to execute
func (o *Organizer) ValidatePlan(plan *OrganizePlan) []string {
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
