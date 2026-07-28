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
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

type inPlaceStrategy struct {
	fs             afero.Fs
	config         *Config
	templateEngine template.EngineInterface
	matcher        matcher.MatcherInterface
}

var _ OperationStrategy = (*inPlaceStrategy)(nil)

func newInPlaceStrategy(fs afero.Fs, cfg *Config, m matcher.MatcherInterface, engine template.EngineInterface) *inPlaceStrategy {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &inPlaceStrategy{
		fs:             fs,
		config:         cfg,
		templateEngine: engine,
		matcher:        m,
	}
}

func (s *inPlaceStrategy) isDedicatedFolder(dir string, id string, m matcher.MatcherInterface) (bool, error) {
	entries, err := afero.ReadDir(s.fs, dir)
	if err != nil {
		// Propagate the directory-read error instead of treating an unreadable
		// source folder as "not dedicated" (which could misclassify it and allow
		// an invalid move plan).
		return false, fmt.Errorf("failed to read source directory %q: %w", dir, err)
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
		if matchedID == id || strings.Contains(strings.ToUpper(entry.Name()), strings.ToUpper(id)) {
			matchingCount++
		}
	}

	return videoCount > 0 && videoCount == matchingCount, nil
}

func (s *inPlaceStrategy) Plan(match models.FileMatchInfo, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	pc := buildPlanContext(s.config, s.templateEngine, movie, match)
	if pc.Err != nil {
		return nil, pc.Err
	}

	sourceDir := filepath.Dir(match.Path)
	parentDir := filepath.Dir(sourceDir)
	baseDirLen := len(parentDir)
	if len(destDir) > baseDirLen {
		baseDirLen = len(destDir)
	}
	overheadBytes := baseDirLen + 2 + len(pc.FileName)
	folderMaxBytes := 0
	if s.config.MaxPathLength > 0 && overheadBytes < s.config.MaxPathLength {
		folderMaxBytes = s.config.MaxPathLength - overheadBytes
	}

	folderName := pc.FolderName
	if folderMaxBytes > 0 {
		var err error
		folderName, err = s.templateEngine.ExecuteWithMaxBytes(s.config.FolderFormat, pc.Ctx, folderMaxBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to generate folder name: %w", err)
		}
		folderName = template.SanitizeFolderPath(folderName)
		if folderName == "" {
			folderName = template.SanitizeFolderPath(match.MovieID)
			if folderName == "" {
				folderName = "unknown"
			}
		}
	}

	var targetDir string
	targetPath := ""
	willMove := false

	inPlace := false
	oldDir := ""
	isDedicated := false
	skipInPlaceReason := ""

	if s.matcher != nil {
		var err error
		isDedicated, err = s.isDedicatedFolder(sourceDir, match.MovieID, s.matcher)
		if err != nil {
			return nil, err
		}

		if isDedicated {
			currentFolderName := filepath.Base(sourceDir)
			if currentFolderName != folderName {
				inPlace = true
				oldDir = sourceDir
				targetDir = filepath.Join(filepath.Dir(sourceDir), folderName)
				targetPath = filepath.Join(targetDir, pc.FileName)
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

	if !inPlace && s.config.OperationMode == operationmode.OperationModeOrganize {
		pathParts := []string{destDir}
		if folderName != "" {
			pathParts = append(pathParts, folderName)
		}
		targetDir = filepath.Join(pathParts...)
		targetPath = filepath.Join(targetDir, pc.FileName)
		willMove = filepath.ToSlash(match.Path) != filepath.ToSlash(targetPath)
	} else if !inPlace {
		targetDir = sourceDir
		targetPath = filepath.Join(targetDir, pc.FileName)
		willMove = filepath.ToSlash(match.Path) != filepath.ToSlash(targetPath)
	}

	if s.config.MaxPathLength > 0 {
		if err := s.templateEngine.ValidatePathLength(targetPath, s.config.MaxPathLength); err != nil {
			return nil, fmt.Errorf("path validation failed: %w", err)
		}
	}

	conflicts := checkTargetConflict(s.fs, match.Path, targetPath, forceUpdate, willMove)
	if inPlace && !forceUpdate {
		if stat, err := s.fs.Stat(targetDir); err == nil {
			oldStat, oldErr := s.fs.Stat(oldDir)
			if oldErr != nil {
				conflicts = append(conflicts, targetDir)
			} else if !os.SameFile(oldStat, stat) {
				conflicts = append(conflicts, targetDir)
			}
		}
	}

	return &OrganizePlan{
		Match:              match,
		Movie:              movie,
		SourcePath:         match.Path,
		TargetDir:          targetDir,
		TargetFile:         pc.FileName,
		TargetPath:         targetPath,
		WillMove:           willMove,
		Conflicts:          conflicts,
		InPlace:            inPlace,
		OldDir:             oldDir,
		IsDedicated:        isDedicated,
		SkipInPlaceReason:  skipInPlaceReason,
		FolderName:         folderName,
		SubfolderPath:      "",
		BaseFileName:       resolveBaseFileName(s.config, s.templateEngine, movie, match),
		PreserveSourcePath: false,
		RenameFolder:       inPlace,
		strategy:           strategyInPlace,
		executeStrategy:    s,
		moveFiles:          true,
	}, nil
}

func (s *inPlaceStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: true,
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
			// NOTE: TOCTOU race — targetDir could be created between this Stat check and the
			// Rename below. This is a known limitation mitigated by the Plan-phase conflict check,
			// which validates exclusivity before Execute runs. A filesystem-level atomic rename
			// would be required to fully eliminate this window.
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

		oldFileName := plan.Match.Name
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
		if err := s.fs.MkdirAll(plan.TargetDir, config.DirPerm); err != nil {
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
