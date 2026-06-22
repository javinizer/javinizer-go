package organizer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

type organizeStrategy struct {
	fs             afero.Fs
	config         *Config
	templateEngine template.EngineInterface
	linker         linker
}

var _ OperationStrategy = (*organizeStrategy)(nil)

func newOrganizeStrategy(fs afero.Fs, cfg *Config, engine template.EngineInterface, linker linker) *organizeStrategy {
	if engine == nil {
		engine = template.NewEngine()
	}
	if linker == nil {
		linker = OSLinker{}
	}
	return &organizeStrategy{
		fs:             fs,
		config:         cfg,
		templateEngine: engine,
		linker:         linker,
	}
}

func (s *organizeStrategy) Plan(match models.FileMatchInfo, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	pc := buildPlanContext(s.config, s.templateEngine, movie, match)
	if pc.Err != nil {
		return nil, pc.Err
	}

	subfolderParts := make([]string, 0, len(s.config.SubfolderFormat))
	for _, subfolderTemplate := range s.config.SubfolderFormat {
		subfolderName, err := s.templateEngine.Execute(subfolderTemplate, pc.Ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to generate subfolder from template '%s': %w", subfolderTemplate, err)
		}
		subfolderName = template.SanitizeFolderPath(subfolderName)
		if subfolderName != "" {
			subfolderParts = append(subfolderParts, subfolderName)
		}
	}

	pathParts := []string{destDir}
	pathParts = append(pathParts, subfolderParts...)
	overheadBytes := len(filepath.Join(pathParts...)) + 2 + len(pc.FileName)
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

	pathParts = append(pathParts, folderName)
	targetDir := filepath.Join(pathParts...)
	targetPath := filepath.Join(targetDir, pc.FileName)

	if s.config.MaxPathLength > 0 {
		if err := s.templateEngine.ValidatePathLength(targetPath, s.config.MaxPathLength); err != nil {
			return nil, fmt.Errorf("path validation failed: %w", err)
		}
	}

	willMove := filepath.ToSlash(match.Path) != filepath.ToSlash(targetPath)

	conflicts := checkTargetConflict(s.fs, match.Path, targetPath, forceUpdate, willMove)

	var subfolderPath string
	if len(subfolderParts) > 0 {
		subfolderPath = filepath.Join(subfolderParts...)
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
		InPlace:            false,
		OldDir:             "",
		IsDedicated:        false,
		SkipInPlaceReason:  "organize mode - always move to destination",
		FolderName:         folderName,
		SubfolderPath:      subfolderPath,
		BaseFileName:       resolveBaseFileName(s.config, s.templateEngine, movie, match),
		PreserveSourcePath: false,
		RenameFolder:       false,
		strategy:           strategyOrganize,
		executeStrategy:    s,
		moveFiles:          true,
	}, nil
}

func (s *organizeStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: true,
	}

	// No-op: source already at target, nothing to do
	if !plan.WillMove {
		return result, nil
	}

	// Move path: moveFiles=true (default) — rename source to target
	if plan.moveFiles {
		if err := s.fs.MkdirAll(plan.TargetDir, config.DirPerm); err != nil {
			result.Error = fmt.Errorf("failed to create directory: %w", err)
			return result, result.Error
		}

		if err := fsutil.MoveFileFs(s.fs, plan.SourcePath, plan.TargetPath); err != nil {
			result.Error = fmt.Errorf("failed to move file: %w", err)
			return result, result.Error
		}

		result.Moved = true
		return result, nil
	}

	// Copy/link path (absorbed from CopyWithLinkMode)
	if len(plan.Conflicts) > 0 {
		result.Error = fmt.Errorf("conflicts detected: %s", strings.Join(plan.Conflicts, "; "))
		return result, result.Error
	}

	result.ShouldGenerateMetadata = true

	if !plan.LinkMode.IsValid() {
		result.Error = fmt.Errorf("unsupported link mode %q", plan.LinkMode)
		return result, result.Error
	}

	// Create target directory
	if err := s.fs.MkdirAll(plan.TargetDir, config.DirPerm); err != nil {
		result.Error = fmt.Errorf("failed to create directory: %w", err)
		return result, result.Error
	}

	// Remove existing target before creating link
	if plan.LinkMode != LinkModeNone {
		if err := s.fs.Remove(plan.TargetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			result.Error = fmt.Errorf("failed to prepare target path for link: %w", err)
			return result, result.Error
		}
	}

	switch plan.LinkMode {
	case LinkModeHard:
		if err := s.linker.hardlink(plan.SourcePath, plan.TargetPath); err != nil {
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
		if err := s.linker.symlink(linkTarget, plan.TargetPath); err != nil {
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
	default:
		// LinkModeNone in copy path: use linker.CopyFile (replaces the manual io.Copy block)
		if err := s.linker.copyFile(s.fs, plan.SourcePath, plan.TargetPath); err != nil {
			result.Error = fmt.Errorf("failed to copy file: %w", err)
			return result, result.Error
		}
	}

	result.Moved = true
	result.ShouldGenerateMetadata = true

	return result, nil
}
