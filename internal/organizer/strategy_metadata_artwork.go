package organizer

import (
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

type metadataArtworkStrategy struct {
	fs     afero.Fs
	config *Config
}

var _ OperationStrategy = (*metadataArtworkStrategy)(nil)

func newMetadataArtworkStrategy(fs afero.Fs, cfg *Config) *metadataArtworkStrategy {
	return &metadataArtworkStrategy{
		fs:     fs,
		config: cfg,
	}
}

func (s *metadataArtworkStrategy) Plan(match models.FileMatchInfo, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	fileName := match.Name
	if fileName == "" && match.Path != "" {
		fileName = filepath.Base(match.Path)
	}

	sourceDir := filepath.Dir(match.Path)

	return &OrganizePlan{
		Match:              match,
		Movie:              movie,
		SourcePath:         match.Path,
		TargetDir:          sourceDir,
		TargetFile:         fileName,
		TargetPath:         match.Path,
		WillMove:           false,
		Conflicts:          nil,
		InPlace:            false,
		OldDir:             "",
		IsDedicated:        false,
		SkipInPlaceReason:  "metadata-artwork mode",
		FolderName:         "",
		SubfolderPath:      "",
		BaseFileName:       strings.TrimSuffix(fileName, match.Extension),
		PreserveSourcePath: true,
		RenameFolder:       false,
		strategy:           strategyMetadataArtwork,
		executeStrategy:    s,
		moveFiles:          true,
	}, nil
}

func (s *metadataArtworkStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
	return &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: true,
	}, nil
}
