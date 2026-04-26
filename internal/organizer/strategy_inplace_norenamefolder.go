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
	fs              afero.Fs
	config          *config.OutputConfig
	templateEngine  *template.Engine
	subtitleHandler *SubtitleHandler
	matcher         *matcher.Matcher
}

var _ OperationStrategy = (*InPlaceNoRenameFolderStrategy)(nil)

func NewInPlaceNoRenameFolderStrategy(fs afero.Fs, cfg *config.OutputConfig, m *matcher.Matcher, engine *template.Engine) *InPlaceNoRenameFolderStrategy {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &InPlaceNoRenameFolderStrategy{
		fs:              fs,
		config:          cfg,
		templateEngine:  engine,
		subtitleHandler: NewSubtitleHandler(fs, cfg),
		matcher:         m,
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
	}

	sourceDir := filepath.Dir(match.File.Path)
	targetDir := sourceDir
	targetPath := filepath.Join(targetDir, fileName)
	willMove := filepath.ToSlash(match.File.Path) != filepath.ToSlash(targetPath)

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
	}, nil
}

func (s *InPlaceNoRenameFolderStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: true, // Always generate NFO/media in source directory
	}

	if len(plan.Conflicts) > 0 {
		result.Error = fmt.Errorf("conflicts detected: %s", strings.Join(plan.Conflicts, "; "))
		return result, result.Error
	}

	if !plan.WillMove {
		return result, nil
	}

	if err := fsutil.MoveFileFs(s.fs, plan.SourcePath, plan.TargetPath); err != nil {
		result.Error = fmt.Errorf("failed to rename file: %w", err)
		return result, result.Error
	}

	result.Moved = true

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
					subtitleResult.Error = fmt.Errorf("failed to rename subtitle: %w", err)
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
