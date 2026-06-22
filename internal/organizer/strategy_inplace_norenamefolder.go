package organizer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

type inPlaceNoRenameFolderStrategy struct {
	fs             afero.Fs
	config         *Config
	templateEngine template.EngineInterface
}

var _ OperationStrategy = (*inPlaceNoRenameFolderStrategy)(nil)

func newInPlaceNoRenameFolderStrategy(fs afero.Fs, cfg *Config, _ matcher.MatcherInterface, engine template.EngineInterface) *inPlaceNoRenameFolderStrategy {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &inPlaceNoRenameFolderStrategy{
		fs:             fs,
		config:         cfg,
		templateEngine: engine,
	}
}

func (s *inPlaceNoRenameFolderStrategy) Plan(match models.FileMatchInfo, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	pc := buildPlanContext(s.config, s.templateEngine, movie, match)
	if pc.Err != nil {
		return nil, pc.Err
	}

	sourceDir := filepath.Dir(match.Path)
	targetDir := sourceDir
	targetPath := filepath.Join(targetDir, pc.FileName)
	willMove := filepath.ToSlash(match.Path) != filepath.ToSlash(targetPath)

	if s.config.MaxPathLength > 0 && len(targetPath) > s.config.MaxPathLength {
		excess := len(targetPath) - s.config.MaxPathLength
		ext := match.Extension
		currentNameLen := len(pc.FileName) - len(ext)
		if currentNameLen > excess && currentNameLen-excess > 0 {
			baseName := s.templateEngine.TruncateTitleBytes(strings.TrimSuffix(pc.FileName, ext), currentNameLen-excess)
			if baseName != "" {
				pc.FileName = template.SanitizeFilename(baseName) + ext
				targetPath = filepath.Join(targetDir, pc.FileName)
				willMove = filepath.ToSlash(match.Path) != filepath.ToSlash(targetPath)
			}
		}
	}

	if s.config.MaxPathLength > 0 {
		if err := s.templateEngine.ValidatePathLength(targetPath, s.config.MaxPathLength); err != nil {
			return nil, fmt.Errorf("path validation failed: %w", err)
		}
	}

	conflicts := checkTargetConflict(s.fs, match.Path, targetPath, forceUpdate, willMove)

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
		SkipInPlaceReason:  "in-place-norenamefolder mode - file rename only",
		FolderName:         "",
		SubfolderPath:      "",
		BaseFileName:       resolveBaseFileName(s.config, s.templateEngine, movie, match),
		PreserveSourcePath: true,
		RenameFolder:       false,
		strategy:           strategyInPlaceNoRenameFolder,
		executeStrategy:    s,
		moveFiles:          true,
	}, nil
}

func (s *inPlaceNoRenameFolderStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: true,
	}

	if err := fsutil.MoveFileFs(s.fs, plan.SourcePath, plan.TargetPath); err != nil {
		result.Error = fmt.Errorf("failed to rename file: %w", err)
		return result, result.Error
	}

	result.Moved = true

	return result, nil
}
