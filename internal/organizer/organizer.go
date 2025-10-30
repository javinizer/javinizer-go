package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

// Organizer handles file organization (moving/renaming)
type Organizer struct {
	config          *config.OutputConfig
	templateEngine  *template.Engine
	subtitleHandler *SubtitleHandler
	matcher         *matcher.Matcher
}

// NewOrganizer creates a new file organizer
func NewOrganizer(cfg *config.OutputConfig) *Organizer {
	return &Organizer{
		config:          cfg,
		templateEngine:  template.NewEngine(),
		subtitleHandler: NewSubtitleHandler(cfg),
		matcher:         nil, // Set via SetMatcher if needed
	}
}

// SetMatcher sets the matcher instance for in-place rename detection
func (o *Organizer) SetMatcher(m *matcher.Matcher) {
	o.matcher = m
}

// OrganizeResult represents the result of organizing a file
type OrganizeResult struct {
	OriginalPath     string
	NewPath          string
	FolderPath       string
	FileName         string
	Moved            bool
	Error            error
	Subtitles        []SubtitleResult
	InPlaceRenamed   bool   // Whether an in-place directory rename occurred
	OldDirectoryPath string // Original directory path (for updating subsequent file paths)
	NewDirectoryPath string // New directory path after in-place rename
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
	entries, err := os.ReadDir(dir)
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

		// Check if it's a video file (common video extensions)
		isVideo := false
		videoExts := []string{".mp4", ".mkv", ".avi", ".wmv", ".flv", ".mov", ".m4v", ".mpg", ".mpeg", ".m2ts", ".ts"}
		for _, videoExt := range videoExts {
			if ext == videoExt {
				isVideo = true
				break
			}
		}

		if !isVideo {
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

	// Apply title truncation if configured
	if o.config.MaxTitleLength > 0 {
		folderName = o.templateEngine.TruncateTitle(folderName, o.config.MaxTitleLength)
	}
	folderName = template.SanitizeFolderPath(folderName)

	// Generate file name
	var fileName string
	if o.config.RenameFile {
		// Use template to generate new filename
		fileName, err = o.templateEngine.Execute(o.config.FileFormat, ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to generate file name: %w", err)
		}

		// Apply title truncation if configured (for file names too)
		if o.config.MaxTitleLength > 0 {
			fileName = o.templateEngine.TruncateTitle(fileName, o.config.MaxTitleLength)
		}
		fileName = template.SanitizeFilename(fileName)

		// Append part suffix before extension
		if match.IsMultiPart && match.PartSuffix != "" {
			fileName = fileName + match.PartSuffix
		}

		// Add extension
		fileName = fileName + match.File.Extension
	} else {
		// Keep original filename
		fileName = match.File.Name
	}

	// Build target paths with subfolder hierarchy
	// Start with destDir, add subfolder parts, then final folder name
	pathParts := []string{destDir}
	pathParts = append(pathParts, subfolderParts...)
	pathParts = append(pathParts, folderName)
	targetDir := filepath.Join(pathParts...)
	targetPath := filepath.Join(targetDir, fileName)

	// In-place rename detection
	inPlace := false
	oldDir := ""
	isDedicated := false
	skipInPlaceReason := ""

	sourceDir := filepath.Dir(match.File.Path)

	if o.config.RenameFolderInPlace && o.matcher != nil {
		// Check if source and dest base directories match (same parent location)
		sourceParent := filepath.Dir(sourceDir)

		// For in-place, we ignore SubfolderFormat - just check if destDir matches sourceParent
		if sourceParent == destDir {
			// Check if source folder is dedicated to this ID
			isDedicated = o.isDedicatedFolder(sourceDir, match.ID, o.matcher)

			if isDedicated {
				// Check if folder name already matches target
				currentFolderName := filepath.Base(sourceDir)
				if currentFolderName != folderName {
					// Enable in-place rename
					inPlace = true
					oldDir = sourceDir
					// Override targetDir to rename in-place (ignore subfolders)
					targetDir = filepath.Join(destDir, folderName)
					targetPath = filepath.Join(targetDir, fileName)
				} else {
					skipInPlaceReason = "folder already has correct name"
				}
			} else {
				skipInPlaceReason = "folder contains mixed IDs"
			}
		} else {
			skipInPlaceReason = "source and dest directories differ"
		}
	} else if !o.config.RenameFolderInPlace {
		skipInPlaceReason = "feature disabled in config"
	} else if o.matcher == nil {
		skipInPlaceReason = "matcher not set"
	}

	// Validate final path length
	if o.config.MaxPathLength > 0 {
		if err := o.templateEngine.ValidatePathLength(targetPath, o.config.MaxPathLength); err != nil {
			return nil, fmt.Errorf("path validation failed: %w", err)
		}
	}

	// Check if move is needed
	willMove := match.File.Path != targetPath

	// Check for conflicts (skip if forceUpdate is enabled)
	conflicts := make([]string, 0)
	if !forceUpdate {
		if _, err := os.Stat(targetPath); err == nil {
			conflicts = append(conflicts, fmt.Sprintf("target file exists: %s", targetPath))
		}
	}

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
		OriginalPath: plan.SourcePath,
		NewPath:      plan.TargetPath,
		FolderPath:   plan.TargetDir,
		FileName:     plan.TargetFile,
		Moved:        false,
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
		info, err := os.Stat(plan.OldDir)
		if err != nil {
			result.Error = fmt.Errorf("failed to stat old directory: %w", err)
			return result, result.Error
		}
		if !info.IsDir() {
			result.Error = fmt.Errorf("old path is not a directory: %s", plan.OldDir)
			return result, result.Error
		}

		// Check if target directory already exists (conflict)
		if _, err := os.Stat(plan.TargetDir); err == nil {
			result.Error = fmt.Errorf("target directory already exists: %s", plan.TargetDir)
			return result, result.Error
		}

		// Rename the directory
		if err := os.Rename(plan.OldDir, plan.TargetDir); err != nil {
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
			if err := os.Rename(currentFilePath, plan.TargetPath); err != nil {
				// Try to rollback directory rename on file rename failure
				os.Rename(plan.TargetDir, plan.OldDir)
				result.Error = fmt.Errorf("failed to rename file after directory rename: %w", err)
				return result, result.Error
			}
		}

		result.Moved = true
	} else {
		// Normal move: create target directory and move file
		// Create target directory
		if err := os.MkdirAll(plan.TargetDir, 0755); err != nil {
			result.Error = fmt.Errorf("failed to create directory: %w", err)
			return result, result.Error
		}

		// Move/rename the file
		if err := os.Rename(plan.SourcePath, plan.TargetPath); err != nil {
			result.Error = fmt.Errorf("failed to move file: %w", err)
			return result, result.Error
		}

		result.Moved = true
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
					if err := os.Rename(subtitle.OriginalPath, subtitleResult.NewPath); err != nil {
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
	plan, err := o.Plan(match, movie, destDir, forceUpdate)
	if err != nil {
		return nil, err
	}

	if copyOnly {
		return o.Copy(plan, dryRun)
	}
	return o.Execute(plan, dryRun)
}

// OrganizeBatch organizes multiple files
func (o *Organizer) OrganizeBatch(matches []matcher.MatchResult, movies map[string]*models.Movie, destDir string, dryRun bool, forceUpdate bool, copyOnly bool) ([]OrganizeResult, error) {
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

			result, err := o.Organize(match, movie, destDir, dryRun, forceUpdate, copyOnly)
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
	result := &OrganizeResult{
		OriginalPath: plan.SourcePath,
		NewPath:      plan.TargetPath,
		FolderPath:   plan.TargetDir,
		FileName:     plan.TargetFile,
		Moved:        false,
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
	if err := os.MkdirAll(plan.TargetDir, 0755); err != nil {
		result.Error = fmt.Errorf("failed to create directory: %w", err)
		return result, result.Error
	}

	// Copy the file
	sourceFile, err := os.Open(plan.SourcePath)
	if err != nil {
		result.Error = fmt.Errorf("failed to open source file: %w", err)
		return result, result.Error
	}
	defer sourceFile.Close()

	targetFile, err := os.Create(plan.TargetPath)
	if err != nil {
		result.Error = fmt.Errorf("failed to create target file: %w", err)
		return result, result.Error
	}
	defer targetFile.Close()

	// Copy data
	if _, err := targetFile.ReadFrom(sourceFile); err != nil {
		result.Error = fmt.Errorf("failed to copy file: %w", err)
		return result, result.Error
	}

	result.Moved = true // "Moved" means operation succeeded (even though it's a copy)
	return result, nil
}

// Revert reverts an organization operation (moves file back)
func (o *Organizer) Revert(result *OrganizeResult) error {
	if !result.Moved {
		return nil // Nothing to revert
	}

	// Move file back to original location
	if err := os.Rename(result.NewPath, result.OriginalPath); err != nil {
		return fmt.Errorf("failed to revert move: %w", err)
	}

	// Try to remove the directory if it's empty
	dir := filepath.Dir(result.NewPath)
	os.Remove(dir) // Ignore error - directory might not be empty

	return nil
}

// ValidatePlan checks if a plan is valid and safe to execute
func ValidatePlan(plan *OrganizePlan) []string {
	issues := make([]string, 0)

	// Check for conflicts
	issues = append(issues, plan.Conflicts...)

	// Check source exists
	if _, err := os.Stat(plan.SourcePath); os.IsNotExist(err) {
		issues = append(issues, fmt.Sprintf("source file does not exist: %s", plan.SourcePath))
	}

	// Check target is not the same as source
	if plan.SourcePath == plan.TargetPath {
		issues = append(issues, "source and target paths are identical")
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
func CleanEmptyDirectories(path string, baseDir string) error {
	// Get the directory of the file
	dir := filepath.Dir(path)

	// Don't go above base directory
	baseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return err
	}

	for {
		// Check if directory is empty
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}

		// If not empty, stop
		if len(entries) > 0 {
			break
		}

		// Remove empty directory
		if err := os.Remove(dir); err != nil {
			return err
		}

		// Move up one level
		parentDir := filepath.Dir(dir)

		// Stop if we've reached or gone above the base directory
		if parentDir == dir || !strings.HasPrefix(parentDir, baseDir) {
			break
		}

		dir = parentDir
	}

	return nil
}
