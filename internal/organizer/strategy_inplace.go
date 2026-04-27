package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

type InPlaceStrategy struct {
	fs             afero.Fs
	config         *config.OutputConfig
	templateEngine *template.Engine
	matcher        *matcher.Matcher
}

var _ OperationStrategy = (*InPlaceStrategy)(nil)

func NewInPlaceStrategy(fs afero.Fs, cfg *config.OutputConfig, m *matcher.Matcher, engine *template.Engine) *InPlaceStrategy {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &InPlaceStrategy{
		fs:             fs,
		config:         cfg,
		templateEngine: engine,
		matcher:        m,
	}
}

func (s *InPlaceStrategy) isDedicatedFolder(dir string, id string, m *matcher.Matcher) bool {
	entries, err := afero.ReadDir(s.fs, dir)
	if err != nil {
		return false
	}

	videoCount := 0
	matchingCount := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))

		if !videoExtensions[ext] {
			continue
		}

		videoCount++

		matchedID := m.MatchString(entry.Name())
		if matchedID == id {
			matchingCount++
		}
	}

	return videoCount > 0 && videoCount == matchingCount
}

func (s *InPlaceStrategy) Plan(match matcher.MatchResult, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = s.config.GroupActress

	applyTitleTruncation(s.templateEngine, ctx, s.config.MaxTitleLength)

	ctx.PartNumber = match.PartNumber
	ctx.PartSuffix = match.PartSuffix
	ctx.IsMultiPart = match.IsMultiPart

	folderName, err := s.templateEngine.Execute(s.config.FolderFormat, ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate folder name: %w", err)
	}

	folderName = template.SanitizeFolderPath(folderName)
	if folderName == "" {
		folderName = template.SanitizeFolderPath(match.ID)
		if folderName == "" {
			folderName = "unknown"
		}
	}

	var fileName string
	if s.config.RenameFile {
		fileName, err = resolveFileName(s.config, s.templateEngine, ctx, match)
		if err != nil {
			return nil, err
		}
	} else {
		fileName = match.File.Name
		if fileName == "" && match.File.Path != "" {
			fileName = filepath.Base(match.File.Path)
		}
	}

	sourceDir := filepath.Dir(match.File.Path)
	var targetDir string
	targetPath := ""
	willMove := false

	inPlace := false
	oldDir := ""
	isDedicated := false
	skipInPlaceReason := ""

	if s.matcher != nil {
		isDedicated = s.isDedicatedFolder(sourceDir, match.ID, s.matcher)

		if isDedicated {
			currentFolderName := filepath.Base(sourceDir)
			if currentFolderName != folderName {
				inPlace = true
				oldDir = sourceDir
				targetDir = filepath.Join(filepath.Dir(sourceDir), folderName)
				targetPath = filepath.Join(targetDir, fileName)
				willMove = true
			} else {
				skipInPlaceReason = "folder already has correct name"
			}
		} else {
			skipInPlaceReason = "folder contains mixed IDs"
		}
	} else {
		skipInPlaceReason = "matcher not set"
	}

	if !inPlace && s.config.MoveToFolder {
		pathParts := []string{destDir}
		if folderName != "" {
			pathParts = append(pathParts, folderName)
		}
		targetDir = filepath.Join(pathParts...)
		targetPath = filepath.Join(targetDir, fileName)
		willMove = filepath.ToSlash(match.File.Path) != filepath.ToSlash(targetPath)
	} else if !inPlace {
		targetDir = sourceDir
		targetPath = filepath.Join(targetDir, fileName)
		willMove = filepath.ToSlash(match.File.Path) != filepath.ToSlash(targetPath)
	}

	if s.config.MaxPathLength > 0 && len(targetPath) > s.config.MaxPathLength {
		excess := len(targetPath) - s.config.MaxPathLength
		currentFolderLen := len(folderName)
		if currentFolderLen > excess {
			newFolderByteLen := currentFolderLen - excess
			folderName = s.templateEngine.TruncateTitleBytes(folderName, newFolderByteLen)
			if inPlace {
				targetDir = filepath.Join(filepath.Dir(sourceDir), folderName)
			} else if s.config.MoveToFolder {
				pathParts := []string{destDir}
				if folderName != "" {
					pathParts = append(pathParts, folderName)
				}
				targetDir = filepath.Join(pathParts...)
			} else {
				targetDir = sourceDir
			}
			targetPath = filepath.Join(targetDir, fileName)
			willMove = filepath.ToSlash(match.File.Path) != filepath.ToSlash(targetPath)
		}
	}

	if s.config.MaxPathLength > 0 {
		if err := s.templateEngine.ValidatePathLength(targetPath, s.config.MaxPathLength); err != nil {
			return nil, fmt.Errorf("path validation failed: %w", err)
		}
	}

	conflicts := checkTargetConflict(s.fs, match.File.Path, targetPath, forceUpdate, willMove)
	if inPlace && !forceUpdate {
		if stat, err := s.fs.Stat(targetDir); err == nil {
			if oldStat, oldErr := s.fs.Stat(oldDir); oldErr != nil || !os.SameFile(oldStat, stat) {
				conflicts = append(conflicts, targetDir)
			}
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
		FolderName:        folderName,
		SubfolderPath:     "",
		BaseFileName:      resolveBaseFileName(s.config, s.templateEngine, movie, match),
		Strategy:          StrategyTypeInPlace,
		executeStrategy:   s,
	}, nil
}

func (s *InPlaceStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: true, // Always generate NFO/media in source/target directory
	}

	if len(plan.Conflicts) > 0 {
		result.Error = fmt.Errorf("conflicts detected: %s", strings.Join(plan.Conflicts, "; "))
		return result, result.Error
	}

	if !plan.WillMove {
		return result, nil
	}

	if plan.InPlace {
		info, err := s.fs.Stat(plan.OldDir)
		if err != nil {
			result.Error = fmt.Errorf("failed to stat old directory: %w", err)
			return result, result.Error
		}
		if !info.IsDir() {
			result.Error = fmt.Errorf("old path is not a directory: %s", plan.OldDir)
			return result, result.Error
		}

		if _, err := s.fs.Stat(plan.TargetDir); err == nil {
			oldInfo, oldErr := s.fs.Stat(plan.OldDir)
			if oldErr == nil {
				newInfo, newErr := s.fs.Stat(plan.TargetDir)
				if newErr == nil && os.SameFile(oldInfo, newInfo) {
				} else {
					result.Error = fmt.Errorf("target directory already exists: %s", plan.TargetDir)
					return result, result.Error
				}
			} else {
				result.Error = fmt.Errorf("target directory already exists: %s", plan.TargetDir)
				return result, result.Error
			}
		}

		if err := s.fs.Rename(plan.OldDir, plan.TargetDir); err != nil {
			result.Error = fmt.Errorf("failed to rename directory: %w", err)
			return result, result.Error
		}

		result.InPlaceRenamed = true
		result.OldDirectoryPath = plan.OldDir
		result.NewDirectoryPath = plan.TargetDir

		oldFileName := plan.Match.File.Name
		if oldFileName == "" {
			oldFileName = filepath.Base(plan.SourcePath)
		}
		currentFilePath := filepath.Join(plan.TargetDir, oldFileName)
		if currentFilePath != plan.TargetPath {
			if err := s.fs.Rename(currentFilePath, plan.TargetPath); err != nil {
				if rollbackErr := s.fs.Rename(plan.TargetDir, plan.OldDir); rollbackErr != nil {
					logging.Errorf("[in-place] Failed to rollback directory rename %s → %s: %v", plan.TargetDir, plan.OldDir, rollbackErr)
				}
				result.Error = fmt.Errorf("failed to rename file after directory rename: %w", err)
				return result, result.Error
			}
		}

		result.Moved = true
	} else {
		if err := s.fs.MkdirAll(plan.TargetDir, 0755); err != nil {
			result.Error = fmt.Errorf("failed to create directory: %w", err)
			return result, result.Error
		}

		if err := fsutil.MoveFileFs(s.fs, plan.SourcePath, plan.TargetPath); err != nil {
			result.Error = fmt.Errorf("failed to move file: %w", err)
			return result, result.Error
		}

		result.Moved = true
	}

	return result, nil
}
