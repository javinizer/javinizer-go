package organizer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

type MetadataOnlyStrategy struct {
	fs     afero.Fs
	config *config.OutputConfig
}

var _ OperationStrategy = (*MetadataOnlyStrategy)(nil)

func NewMetadataOnlyStrategy(fs afero.Fs, cfg *config.OutputConfig) *MetadataOnlyStrategy {
	return &MetadataOnlyStrategy{
		fs:     fs,
		config: cfg,
	}
}

func (s *MetadataOnlyStrategy) Plan(match matcher.MatchResult, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	// Metadata-only mode never renames files — preserve the original filename
	fileName := match.File.Name
	if fileName == "" && match.File.Path != "" {
		fileName = filepath.Base(match.File.Path)
	}

	sourceDir := filepath.Dir(match.File.Path)

	return &OrganizePlan{
		Match:             match,
		Movie:             movie,
		SourcePath:        match.File.Path,
		TargetDir:         sourceDir,
		TargetFile:        fileName,
		TargetPath:        match.File.Path,
		WillMove:          false,
		Conflicts:         nil,
		InPlace:           false,
		OldDir:            "",
		IsDedicated:       false,
		SkipInPlaceReason: "metadata-only mode",
		FolderName:        "",
		SubfolderPath:     "",
		BaseFileName:      strings.TrimSuffix(fileName, match.File.Extension),
		Strategy:          StrategyTypeMetadataOnly,
		executeStrategy:   s,
	}, nil
}

func (s *MetadataOnlyStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: true, // Always generate NFO/media in source directory
	}

	if plan.WillMove {
		result.Error = fmt.Errorf("metadata-only mode does not support file moves; plan has WillMove=true")
		return result, result.Error
	}

	return result, nil
}
