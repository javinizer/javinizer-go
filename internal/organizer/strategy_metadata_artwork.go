package organizer

import (
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

type MetadataArtworkStrategy struct {
	fs     afero.Fs
	config *config.OutputConfig
}

var _ OperationStrategy = (*MetadataArtworkStrategy)(nil)

func NewMetadataArtworkStrategy(fs afero.Fs, cfg *config.OutputConfig) *MetadataArtworkStrategy {
	return &MetadataArtworkStrategy{
		fs:     fs,
		config: cfg,
	}
}

func (s *MetadataArtworkStrategy) Plan(match matcher.MatchResult, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	// Metadata-artwork mode never renames files — preserve the original filename
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
		SkipInPlaceReason: "metadata-artwork mode",
		FolderName:        "",
		SubfolderPath:     "",
		BaseFileName:      strings.TrimSuffix(fileName, match.File.Extension),
		Strategy:          StrategyTypeMetadataArtwork,
		executeStrategy:   s,
	}, nil
}

func (s *MetadataArtworkStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
	return &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: true,
	}, nil
}
