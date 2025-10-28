package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

// Organizer handles file organization (moving/renaming)
type Organizer struct {
	config         *config.OutputConfig
	templateEngine *template.Engine
}

// NewOrganizer creates a new file organizer
func NewOrganizer(cfg *config.OutputConfig) *Organizer {
	return &Organizer{
		config:         cfg,
		templateEngine: template.NewEngine(),
	}
}

// OrganizeResult represents the result of organizing a file
type OrganizeResult struct {
	OriginalPath string
	NewPath      string
	FolderPath   string
	FileName     string
	Moved        bool
	Error        error
}

// OrganizePlan represents a planned file organization operation
type OrganizePlan struct {
	Match      matcher.MatchResult
	Movie      *models.Movie
	SourcePath string
	TargetDir  string
	TargetFile string
	TargetPath string
	WillMove   bool
	Conflicts  []string
}

// Plan creates an organization plan without executing it
func (o *Organizer) Plan(match matcher.MatchResult, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	ctx := template.NewContextFromMovie(movie)

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

	// Generate file name
	fileName, err := o.templateEngine.Execute(o.config.FileFormat, ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate file name: %w", err)
	}
	fileName = template.SanitizeFilename(fileName)

	// Add extension
	fileName = fileName + match.File.Extension

	// Build target paths with subfolder hierarchy
	// Start with destDir, add subfolder parts, then final folder name
	pathParts := []string{destDir}
	pathParts = append(pathParts, subfolderParts...)
	pathParts = append(pathParts, folderName)
	targetDir := filepath.Join(pathParts...)
	targetPath := filepath.Join(targetDir, fileName)

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
		Match:      match,
		Movie:      movie,
		SourcePath: match.File.Path,
		TargetDir:  targetDir,
		TargetFile: fileName,
		TargetPath: targetPath,
		WillMove:   willMove,
		Conflicts:  conflicts,
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

	for _, match := range matches {
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

		results = append(results, *result)
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
