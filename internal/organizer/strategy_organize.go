package organizer

import (
	"fmt"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

type OrganizeStrategy struct {
	fs             afero.Fs
	config         *config.OutputConfig
	templateEngine *template.Engine
}

var _ OperationStrategy = (*OrganizeStrategy)(nil)

func NewOrganizeStrategy(fs afero.Fs, cfg *config.OutputConfig, engine *template.Engine) *OrganizeStrategy {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &OrganizeStrategy{
		fs:             fs,
		config:         cfg,
		templateEngine: engine,
	}
}

func (s *OrganizeStrategy) Plan(match matcher.MatchResult, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = s.config.GroupActress

	applyTitleTruncation(s.templateEngine, ctx, s.config.MaxTitleLength)

	ctx.PartNumber = match.PartNumber
	ctx.PartSuffix = match.PartSuffix
	ctx.IsMultiPart = match.IsMultiPart

	subfolderParts := make([]string, 0, len(s.config.SubfolderFormat))
	for _, subfolderTemplate := range s.config.SubfolderFormat {
		subfolderName, err := s.templateEngine.Execute(subfolderTemplate, ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to generate subfolder from template '%s': %w", subfolderTemplate, err)
		}
		subfolderName = template.SanitizeFolderPath(subfolderName)
		if subfolderName != "" {
			subfolderParts = append(subfolderParts, subfolderName)
		}
	}

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

	pathParts := []string{destDir}
	pathParts = append(pathParts, subfolderParts...)
	if folderName != "" {
		pathParts = append(pathParts, folderName)
	}
	targetDir := filepath.Join(pathParts...)
	targetPath := filepath.Join(targetDir, fileName)

	if s.config.MaxPathLength > 0 && len(targetPath) > s.config.MaxPathLength {
		excess := len(targetPath) - s.config.MaxPathLength
		currentFolderLen := len(folderName)
		if currentFolderLen > excess {
			newFolderByteLen := currentFolderLen - excess
			if newFolderByteLen > 0 {
				truncated := s.templateEngine.TruncateTitleBytes(folderName, newFolderByteLen)
				if truncated != "" {
					folderName = truncated
				}
			}

			pathParts := []string{destDir}
			pathParts = append(pathParts, subfolderParts...)
			if folderName != "" {
				pathParts = append(pathParts, folderName)
			}
			targetDir = filepath.Join(pathParts...)
			targetPath = filepath.Join(targetDir, fileName)
		}
	}

	if s.config.MaxPathLength > 0 {
		if err := s.templateEngine.ValidatePathLength(targetPath, s.config.MaxPathLength); err != nil {
			return nil, fmt.Errorf("path validation failed: %w", err)
		}
	}

	willMove := filepath.ToSlash(match.File.Path) != filepath.ToSlash(targetPath)

	conflicts := checkTargetConflict(s.fs, match.File.Path, targetPath, forceUpdate, willMove)

	var subfolderPath string
	if len(subfolderParts) > 0 {
		subfolderPath = filepath.Join(subfolderParts...)
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
		InPlace:           false,
		OldDir:            "",
		IsDedicated:       false,
		SkipInPlaceReason: "organize mode - always move to destination",
		FolderName:        folderName,
		SubfolderPath:     subfolderPath,
		BaseFileName:      resolveBaseFileName(s.config, s.templateEngine, movie, match),
		Strategy:          StrategyTypeOrganize,
		executeStrategy:   s,
	}, nil
}

func (s *OrganizeStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: true,
	}

	if err := s.fs.MkdirAll(plan.TargetDir, 0755); err != nil {
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
