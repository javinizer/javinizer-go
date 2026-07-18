package nfo

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

// FindExistingNFO locates and parses the existing NFO for movie in baseDir, reusing
// findNFOFile + ParseNFO. Returns (nil, "", nil) when no NFO is found.
func FindExistingNFO(fs afero.Fs, baseDir string, movie *models.Movie, cfg NFONameConfig, videoFilePath string, engine template.EngineInterface) (*ParseResult, string, error) {
	foundPath := findNFOFile(fs, baseDir, movie, cfg, videoFilePath, engine)
	if foundPath == "" {
		return nil, "", nil
	}
	parseResult, err := ParseNFO(fs, foundPath)
	if err != nil {
		return nil, foundPath, err
	}
	return parseResult, foundPath, nil
}
