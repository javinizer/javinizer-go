package organizer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

type InPlaceStrategy struct {
	fs              afero.Fs
	config          *config.OutputConfig
	templateEngine  *template.Engine
	subtitleHandler *SubtitleHandler
	matcher         *matcher.Matcher
}

var _ OperationStrategy = (*InPlaceStrategy)(nil)

func NewInPlaceStrategy(fs afero.Fs, cfg *config.OutputConfig, m *matcher.Matcher, engine *template.Engine) *InPlaceStrategy {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &InPlaceStrategy{
		fs:              fs,
		config:          cfg,
		templateEngine:  engine,
		subtitleHandler: NewSubtitleHandler(fs, cfg),
		matcher:         m,
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
		videoExts := map[string]bool{
			".mp4": true, ".avi": true, ".mkv": true, ".mov": true,
			".wmv": true, ".flv": true, ".webm": true, ".m4v": true,
		}

		if !videoExts[ext] {
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

	ctx.PartNumber = match.PartNumber
	ctx.PartSuffix = match.PartSuffix
	ctx.IsMultiPart = match.IsMultiPart

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

	sourceDir := filepath.Dir(match.File.Path)
	targetDir := sourceDir
	targetPath := filepath.Join(targetDir, fileName)
	willMove := filepath.ToSlash(match.File.Path) != filepath.ToSlash(targetPath)

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

	conflicts := make([]string, 0)
	if !forceUpdate && willMove {
		if _, err := s.fs.Stat(targetPath); err == nil {
			conflicts = append(conflicts, targetPath)
		}
		if inPlace {
			if _, err := s.fs.Stat(targetDir); err == nil {
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
		if err := s.fs.Rename(plan.OldDir, plan.TargetDir); err != nil {
			result.Error = fmt.Errorf("failed to rename directory: %w", err)
			return result, result.Error
		}

		result.InPlaceRenamed = true
		result.OldDirectoryPath = plan.OldDir
		result.NewDirectoryPath = plan.TargetDir

		currentFilePath := filepath.Join(plan.TargetDir, plan.Match.File.Name)
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
	}

	return result, nil
}
