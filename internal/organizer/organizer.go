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
		if fileName == "" {
			fileName = "file"
		}
		logging.Warnf("[%s] Template produced empty filename after sanitization, falling back to %q", match.ID, fileName)
	}

	fileName = fileName + match.File.Extension
	return fileName, nil
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
	Error        error
}

// OrganizePlan represents a planned file organization operation
type OrganizePlan struct {
	Match             matcher.MatchResult
	Movie             *models.Movie
	SourcePath        string
	TargetDir         string
	TargetFile        string
	TargetPath        string
	WillMove          bool
	Conflicts         []string
	InPlace           bool   // Whether renaming folder in-place
	OldDir            string // Original directory path (for in-place renames)
	IsDedicated       bool   // Whether source folder is dedicated to this ID
	SkipInPlaceReason string // Reason why in-place was not used
}

// isDedicatedFolder checks if a folder is dedicated to a single movie ID
// It scans the directory for video files and checks if they all belong to the same ID
func (o *Organizer) isDedicatedFolder(dir string, id string, m *matcher.Matcher) bool {
	// Read directory contents
	entries, err := afero.ReadDir(o.fs, dir)
	if err != nil {
		return false
	}

	// Check all video files in the directory
	videoCount := 0
	matchingCount := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))

		if !videoExtensions[ext] {
			continue
		}

		videoCount++

		// Try to extract ID from filename
		extractedID := m.MatchString(name)
		if extractedID == id {
			matchingCount++
		}
	}

	// Dedicated if:
	// - At least one video file found
	// - All video files match the same ID
	return videoCount > 0 && videoCount == matchingCount
}

// Plan creates an organization plan without executing it
func (o *Organizer) Plan(match matcher.MatchResult, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = o.config.GroupActress

	applyTitleTruncation(o.templateEngine, ctx, o.config.MaxTitleLength)

	ctx.PartNumber = match.PartNumber
	ctx.PartSuffix = match.PartSuffix
	ctx.IsMultiPart = match.IsMultiPart

	// Generate subfolder hierarchy (if configured)
	subfolderParts := make([]string, 0, len(o.config.SubfolderFormat))
	for _, subfolderTemplate := range o.config.SubfolderFormat {
		subfolderName, err := o.templateEngine.Execute(subfolderTemplate, ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to generate subfolder from template '%s': %w", subfolderTemplate, err)
		}
		// Sanitize and add to parts if not empty
		subfolderName = template.SanitizeFolderPath(subfolderName)
		if subfolderName != "" {
			subfolderParts = append(subfolderParts, subfolderName)
		}
	}

	// Generate folder name
	folderName, err := o.templateEngine.Execute(o.config.FolderFormat, ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate folder name: %w", err)
	}

	folderName = template.SanitizeFolderPath(folderName)
	if folderName == "" && (o.config.MoveToFolder || o.config.RenameFolderInPlace) {
		folderName = template.SanitizeFolderPath(match.ID)
		if folderName == "" {
			folderName = "unknown"
		}
	}

	var fileName string
	if o.config.RenameFile {
		fileName, err = resolveFileName(o.config, o.templateEngine, ctx, match)
		if err != nil {
			return nil, err
		}
	} else {
		fileName = match.File.Name
	}

	// Build target paths with subfolder hierarchy
	// Start with destDir, add subfolder parts, then final folder name
	pathParts := []string{destDir}
	pathParts = append(pathParts, subfolderParts...)
	if folderName != "" {
		pathParts = append(pathParts, folderName)
	}
	targetDir := filepath.Join(pathParts...)
	targetPath := filepath.Join(targetDir, fileName)

	// In-place rename detection - check RenameFolderInPlace FIRST (priority over MoveToFolder)
	inPlace := false
	oldDir := ""
	isDedicated := false
	skipInPlaceReason := ""

	sourceDir := filepath.Dir(match.File.Path)
	sourceParent := filepath.Dir(sourceDir)

	// Check RenameFolderInPlace first - this takes priority over MoveToFolder
	if o.config.RenameFolderInPlace && o.matcher != nil {
		// Warn if both configs are enabled (rename takes priority)
		if o.config.MoveToFolder {
			logging.Warnf("[%s] Both rename_folder_in_place and move_to_folder enabled; rename takes priority", match.ID)
		}

		// Check if source folder is dedicated to this ID (unconditional - no path check)
		isDedicated = o.isDedicatedFolder(sourceDir, match.ID, o.matcher)

		if isDedicated {
			currentFolderName := filepath.Base(sourceDir)
			if currentFolderName != folderName {
				inPlace = true
				oldDir = sourceDir
				// In-place rename: rename folder in its current location (ignore destDir)
				targetDir = filepath.Join(filepath.Dir(sourceDir), folderName)
				targetPath = filepath.Join(targetDir, fileName)
				logging.Debugf("[%s] In-place folder rename enabled: %s → %s", match.ID, oldDir, targetDir)
			} else {
				skipInPlaceReason = "folder already has correct name"
				if !o.config.MoveToFolder {
					targetDir = sourceDir
					targetPath = filepath.Join(targetDir, fileName)
				}
			}
		} else {
			skipInPlaceReason = "folder contains mixed IDs"
			if !o.config.MoveToFolder {
				targetDir = sourceDir
				targetPath = filepath.Join(targetDir, fileName)
			}
		}
	} else if !o.config.RenameFolderInPlace {
		skipInPlaceReason = "feature disabled in config"
	} else if o.matcher == nil {
		skipInPlaceReason = "matcher not set"
	}

	// When both configs are false, keep file in current location (no folder changes)
	// This enables "metadata only" mode where NFO/images are generated but files aren't moved
	if !o.config.RenameFolderInPlace && !o.config.MoveToFolder {
		targetDir = sourceDir
		targetPath = filepath.Join(targetDir, fileName)
	}

	// Automatically truncate path if it exceeds MaxPathLength
	if o.config.MaxPathLength > 0 && len(targetPath) > o.config.MaxPathLength {
		// Calculate how much we need to truncate (in bytes)
		excess := len(targetPath) - o.config.MaxPathLength

		// Truncate the folder name (usually the longest variable part)
		currentFolderLen := len(folderName)
		if currentFolderLen > excess {
			// Calculate the byte budget for the new folder name
			// TruncateTitleBytes will handle adding ellipsis if there's room
			newFolderByteLen := currentFolderLen - excess
			folderName = o.templateEngine.TruncateTitleBytes(folderName, newFolderByteLen)

			if inPlace {
				targetDir = filepath.Join(sourceParent, folderName)
			} else if targetDir != sourceDir {
				pathParts := []string{destDir}
				pathParts = append(pathParts, subfolderParts...)
				if folderName != "" {
					pathParts = append(pathParts, folderName)
				}
				targetDir = filepath.Join(pathParts...)
			}
			targetPath = filepath.Join(targetDir, fileName)
		}
	}

	// Validate final path length
	if o.config.MaxPathLength > 0 {
		if err := o.templateEngine.ValidatePathLength(targetPath, o.config.MaxPathLength); err != nil {
			return nil, fmt.Errorf("path validation failed: %w", err)
		}
	}

	// Check if move is needed
	willMove := filepath.ToSlash(match.File.Path) != filepath.ToSlash(targetPath)

	conflicts := checkTargetConflict(o.fs, match.File.Path, targetPath, forceUpdate, willMove)

	return &OrganizePlan{
		Match:             match,
		Movie:             movie,
		SourcePath:        match.File.Path,
		TargetDir:         targetDir,
		TargetFile:        fileName,
		TargetPath:        targetPath,
		WillMove:          willMove,
		Conflicts:         conflicts,
		InPlace:           inPlace,
		OldDir:            oldDir,
		IsDedicated:       isDedicated,
		SkipInPlaceReason: skipInPlaceReason,
	}, nil
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

	// Check for conflicts
	if len(plan.Conflicts) > 0 {
		result.Error = fmt.Errorf("conflicts detected: %s", strings.Join(plan.Conflicts, "; "))
		return result, result.Error
	}

	// Skip if no move needed
	if !plan.WillMove {
		return result, nil
	}

	// Dry run - don't actually move
	if dryRun {
		return result, nil
	}

	// In-place rename: rename directory first, then rename file within
	if plan.InPlace {
		// Safety check: verify old directory exists and is a directory
		info, err := o.fs.Stat(plan.OldDir)
		if err != nil {
			result.Error = fmt.Errorf("failed to stat old directory: %w", err)
			return result, result.Error
		}
		if !info.IsDir() {
			result.Error = fmt.Errorf("old path is not a directory: %s", plan.OldDir)
			return result, result.Error
		}

		// Check if target directory already exists (conflict)
		if _, err := o.fs.Stat(plan.TargetDir); err == nil {
			// Check if it's actually the same directory (case-only rename on case-insensitive FS)
			oldInfo, oldErr := o.fs.Stat(plan.OldDir)
			if oldErr == nil {
				newInfo, newErr := o.fs.Stat(plan.TargetDir)
				if newErr == nil && os.SameFile(oldInfo, newInfo) {
					// Same directory with different case — skip conflict, proceed with rename
				} else {
					result.Error = fmt.Errorf("target directory already exists: %s", plan.TargetDir)
					return result, result.Error
				}
			} else {
				result.Error = fmt.Errorf("target directory already exists: %s", plan.TargetDir)
				return result, result.Error
			}
		}

		// Rename the directory
		if err := o.fs.Rename(plan.OldDir, plan.TargetDir); err != nil {
			result.Error = fmt.Errorf("failed to rename directory: %w", err)
			return result, result.Error
		}

		// Track in-place rename for multi-part path updates
		result.InPlaceRenamed = true
		result.OldDirectoryPath = plan.OldDir
		result.NewDirectoryPath = plan.TargetDir

		// After directory rename, the file is now at: plan.TargetDir/<old_filename>
		// We need to rename it to plan.TargetFile
		oldFileName := filepath.Base(plan.SourcePath)
		currentFilePath := filepath.Join(plan.TargetDir, oldFileName)

		// Only rename file if the name actually changed
		if oldFileName != plan.TargetFile {
			if err := o.fs.Rename(currentFilePath, plan.TargetPath); err != nil {
				// Try to rollback directory rename on file rename failure
				_ = o.fs.Rename(plan.TargetDir, plan.OldDir)
				result.Error = fmt.Errorf("failed to rename file after directory rename: %w", err)
				return result, result.Error
			}
		}

		result.Moved = true
		result.ShouldGenerateMetadata = true
	} else {
		// Normal move: create target directory and move file
		// Create target directory
		if err := o.fs.MkdirAll(plan.TargetDir, 0755); err != nil {
			result.Error = fmt.Errorf("failed to create directory: %w", err)
			return result, result.Error
		}

		if err := fsutil.MoveFileFs(o.fs, plan.SourcePath, plan.TargetPath); err != nil {
			result.Error = fmt.Errorf("failed to move file: %w", err)
			return result, result.Error
		}

		result.Moved = true
		result.ShouldGenerateMetadata = true
	}

	// Handle subtitle files if enabled
	if o.config.MoveSubtitles {
		// For in-place renames, we need to update the file info to point to the new location
		// so subtitle discovery works correctly
		fileInfoForSubtitles := plan.Match.File
		if plan.InPlace {
			// Update path to the new location after directory rename
			fileInfoForSubtitles.Path = plan.TargetPath
		}

		subtitles := o.subtitleHandler.FindSubtitles(fileInfoForSubtitles)
		if len(subtitles) > 0 {
			subtitleResults := make([]SubtitleResult, len(subtitles))
			for i, subtitle := range subtitles {
				subtitleResult := SubtitleResult{
					OriginalPath: subtitle.OriginalPath,
					Moved:        false,
				}

				// Generate new subtitle path
				videoNameWithoutExt := strings.TrimSuffix(plan.TargetFile, filepath.Ext(plan.TargetFile))
				newSubtitleName := o.subtitleHandler.generateSubtitleFileName(
					videoNameWithoutExt,
					subtitle.Language,
					subtitle.Extension,
				)
				subtitleResult.NewPath = filepath.Join(plan.TargetDir, newSubtitleName)

				// Move subtitle file
				if !dryRun {
					if err := fsutil.MoveFileFs(o.fs, subtitle.OriginalPath, subtitleResult.NewPath); err != nil {
						subtitleResult.Error = fmt.Errorf("failed to move subtitle: %w", err)
					} else {
						subtitleResult.Moved = true
					}
				} else {
					// Dry run - just mark as would be moved
					subtitleResult.Moved = true
				}

				subtitleResults[i] = subtitleResult
			}
			result.Subtitles = subtitleResults
		}
	}

	return result, nil
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

	// Skip if no operation needed
	if !plan.WillMove {
		return result, nil
	}

	// Dry run - don't actually copy
	if dryRun {
		return result, nil
	}

	// Create target directory
	if err := o.fs.MkdirAll(plan.TargetDir, 0755); err != nil {
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
		// Copy the file
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
		defer func() { _ = targetFile.Close() }()

		// Copy data
		if _, err := io.Copy(targetFile, sourceFile); err != nil {
			result.Error = fmt.Errorf("failed to copy file: %w", err)
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
	return result, nil
}

// Revert reverts an organization operation (moves file back)
func (o *Organizer) Revert(result *OrganizeResult) error {
	if !result.Moved {
		return nil // Nothing to revert
	}

	// Move file back to original location
	if err := fsutil.MoveFileFs(o.fs, result.NewPath, result.OriginalPath); err != nil {
		return fmt.Errorf("failed to revert move: %w", err)
	}

	// Try to remove the directory if it's empty
	dir := filepath.Dir(result.NewPath)
	_ = o.fs.Remove(dir) // Ignore error - directory might not be empty

	return nil
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

// CleanEmptyDirectories removes empty directories up to the base path
func (o *Organizer) CleanEmptyDirectories(path string, baseDir string) error {
	// Get the directory of the file
	dir := filepath.Dir(path)

	// Canonicalize and resolve symlinks for safe comparison
	dir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}
	// Resolve symlinks to prevent symlink-based escapes
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		// If we can't resolve symlinks, fail safely
		return err
	}

	// Canonicalize base directory
	baseDir, err = filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return err
	}
	// Resolve symlinks in base directory
	baseDir, err = filepath.EvalSymlinks(baseDir)
	if err != nil {
		// If we can't resolve symlinks, fail safely
		return err
	}

	for dir != baseDir {
		// Check if directory is empty
		entries, err := afero.ReadDir(o.fs, dir)
		if err != nil {
			return err
		}

		// If not empty, stop
		if len(entries) > 0 {
			break
		}

		// Safe to remove (we've verified dir != baseDir)
		if err := o.fs.Remove(dir); err != nil {
			return err
		}

		// Move up one level
		parentDir := filepath.Dir(dir)

		// Stop if we've reached the root (no more parents)
		if parentDir == dir {
			break
		}

		// Additional safety check: ensure parentDir is not above baseDir
		rel, err := filepath.Rel(baseDir, parentDir)
		if err != nil {
			// Cannot determine relationship, stop to be safe
			break
		}
		if strings.HasPrefix(rel, "..") {
			// We've gone above baseDir, stop
			break
		}

		dir = parentDir
	}

	return nil
}
