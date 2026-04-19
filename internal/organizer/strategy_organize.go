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

type OrganizeStrategy struct {
	fs              afero.Fs
	config          *config.OutputConfig
	templateEngine  *template.Engine
	subtitleHandler *SubtitleHandler
}

var _ OperationStrategy = (*OrganizeStrategy)(nil)

func NewOrganizeStrategy(fs afero.Fs, cfg *config.OutputConfig, engine *template.Engine) *OrganizeStrategy {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &OrganizeStrategy{
		fs:              fs,
		config:          cfg,
		templateEngine:  engine,
		subtitleHandler: NewSubtitleHandler(fs, cfg),
	}
}

func (s *OrganizeStrategy) Plan(match matcher.MatchResult, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = s.config.GroupActress

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

	if s.config.MaxTitleLength > 0 {
		folderName = s.templateEngine.TruncateTitle(folderName, s.config.MaxTitleLength)
	}
	folderName = template.SanitizeFolderPath(folderName)

	var fileName string
	if s.config.RenameFile {
		fileName, err = s.templateEngine.Execute(s.config.FileFormat, ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to generate file name: %w", err)
		}

		if s.config.MaxTitleLength > 0 {
			fileName = s.templateEngine.TruncateTitle(fileName, s.config.MaxTitleLength)
		}
		fileName = template.SanitizeFilename(fileName)
		fileName = fileName + match.File.Extension
	} else {
		fileName = match.File.Name
	}

	pathParts := []string{destDir}
	pathParts = append(pathParts, subfolderParts...)
	pathParts = append(pathParts, folderName)
	targetDir := filepath.Join(pathParts...)
	targetPath := filepath.Join(targetDir, fileName)

	if s.config.MaxPathLength > 0 && len(targetPath) > s.config.MaxPathLength {
		excess := len(targetPath) - s.config.MaxPathLength
		currentFolderLen := len(folderName)
		if currentFolderLen > excess {
			newFolderByteLen := currentFolderLen - excess
			folderName = s.templateEngine.TruncateTitleBytes(folderName, newFolderByteLen)

			pathParts := []string{destDir}
			pathParts = append(pathParts, subfolderParts...)
			pathParts = append(pathParts, folderName)
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

	conflicts := make([]string, 0)
	if !forceUpdate && filepath.ToSlash(match.File.Path) != filepath.ToSlash(targetPath) {
		if _, err := s.fs.Stat(targetPath); err == nil {
			conflicts = append(conflicts, targetPath)
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
		InPlace:           false,
		OldDir:            "",
		IsDedicated:       false,
		SkipInPlaceReason: "organize mode - always move to destination",
	}, nil
}

func (s *OrganizeStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: false, // Will be set to true after successful move
	}

	if len(plan.Conflicts) > 0 {
		result.Error = fmt.Errorf("conflicts detected: %s", strings.Join(plan.Conflicts, "; "))
		return result, result.Error
	}

	if !plan.WillMove {
		return result, nil
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
	result.ShouldGenerateMetadata = true

	if s.config.MoveSubtitles {
		subtitles := s.subtitleHandler.FindSubtitles(plan.Match.File)
		if len(subtitles) > 0 {
			subtitleResults := make([]SubtitleResult, len(subtitles))
			for i, subtitle := range subtitles {
				subtitleResult := SubtitleResult{
					OriginalPath: subtitle.OriginalPath,
					Moved:        false,
				}

				videoNameWithoutExt := strings.TrimSuffix(plan.TargetFile, filepath.Ext(plan.TargetFile))
				newSubtitleName := s.subtitleHandler.generateSubtitleFileName(
					videoNameWithoutExt,
					subtitle.Language,
					subtitle.Extension,
				)
				subtitleResult.NewPath = filepath.Join(plan.TargetDir, newSubtitleName)

				if err := fsutil.MoveFileFs(s.fs, subtitle.OriginalPath, subtitleResult.NewPath); err != nil {
					subtitleResult.Error = fmt.Errorf("failed to move subtitle: %w", err)
				} else {
					subtitleResult.Moved = true
				}

				subtitleResults[i] = subtitleResult
			}
			result.Subtitles = subtitleResults
		}
	}

	return result, nil
}
