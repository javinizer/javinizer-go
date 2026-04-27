package organizer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

type InPlaceNoRenameFolderStrategy struct {
	fs             afero.Fs
	config         *config.OutputConfig
	templateEngine *template.Engine
}

var _ OperationStrategy = (*InPlaceNoRenameFolderStrategy)(nil)

func NewInPlaceNoRenameFolderStrategy(fs afero.Fs, cfg *config.OutputConfig, _ *matcher.Matcher, engine *template.Engine) *InPlaceNoRenameFolderStrategy {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &InPlaceNoRenameFolderStrategy{
		fs:             fs,
		config:         cfg,
		templateEngine: engine,
	}
}

func (s *InPlaceNoRenameFolderStrategy) Plan(match matcher.MatchResult, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = s.config.GroupActress

	applyTitleTruncation(s.templateEngine, ctx, s.config.MaxTitleLength)

	ctx.PartNumber = match.PartNumber
	ctx.PartSuffix = match.PartSuffix
	ctx.IsMultiPart = match.IsMultiPart

	var fileName string
	var err error
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
	targetDir := sourceDir
	targetPath := filepath.Join(targetDir, fileName)
	willMove := filepath.ToSlash(match.File.Path) != filepath.ToSlash(targetPath)

	if s.config.MaxPathLength > 0 && len(targetPath) > s.config.MaxPathLength {
		excess := len(targetPath) - s.config.MaxPathLength
		ext := match.File.Extension
		currentNameLen := len(fileName) - len(ext)
		if currentNameLen > excess && currentNameLen-excess > 0 {
			baseName := s.templateEngine.TruncateTitleBytes(strings.TrimSuffix(fileName, ext), currentNameLen-excess)
			if baseName != "" {
				fileName = baseName + ext
				targetPath = filepath.Join(targetDir, fileName)
				willMove = filepath.ToSlash(match.File.Path) != filepath.ToSlash(targetPath)
			}
		}
	}

	if s.config.MaxPathLength > 0 {
		if err := s.templateEngine.ValidatePathLength(targetPath, s.config.MaxPathLength); err != nil {
			return nil, fmt.Errorf("path validation failed: %w", err)
		}
	}

	conflicts := checkTargetConflict(s.fs, match.File.Path, targetPath, forceUpdate, willMove)

	return &OrganizePlan{
		Match:             match,
		Movie:             movie,
		SourcePath:        match.File.Path,
		TargetDir:         targetDir,
		TargetFile:        fileName,
		TargetPath:        targetPath,
		WillMove:          willMove,
		Conflicts:         conflicts,
		InPlace:           false,
		OldDir:            "",
		IsDedicated:       false,
		SkipInPlaceReason: "in-place-norenamefolder mode - file rename only",
		FolderName:        "",
		SubfolderPath:     "",
		BaseFileName:      resolveBaseFileName(s.config, s.templateEngine, movie, match),
		Strategy:          StrategyTypeInPlaceNoRenameFolder,
		executeStrategy:   s,
	}, nil
}

func (s *InPlaceNoRenameFolderStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
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
