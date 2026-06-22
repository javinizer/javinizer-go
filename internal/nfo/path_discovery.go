package nfo

import (
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

// resolveNFOPath builds the expected NFO file path and a list of legacy paths
// to check for backward compatibility.
// The engine parameter is forwarded to ResolveNFOFilename for template rendering;
// if nil, a default engine is created (no language config). Callers that have a
// shared template engine should pass it to ensure consistent filename computation
// with GenerateAtPath.
func resolveNFOPath(baseDir string, movie *models.Movie, cfg NFONameConfig, videoFilePath string, engine template.EngineInterface) (nfoPath string, legacyPaths []string) {
	nfoFilename := ResolveNFOFilename(engine, movie, cfg)
	nfoPath = filepath.Join(baseDir, nfoFilename)

	// Deprecated: Legacy NFO path fallback. Remove after v1.0 migration period.
	if nfoFilename != movie.ID+".nfo" {
		legacyPaths = append(legacyPaths, filepath.Join(baseDir, movie.ID+".nfo"))
	}

	if cfg.PerFile && cfg.IsMultiPart && videoFilePath != "" {
		videoName := strings.TrimSuffix(filepath.Base(videoFilePath), filepath.Ext(videoFilePath))
		videoNFO := filepath.Join(baseDir, videoName+".nfo")
		if videoNFO != nfoPath {
			legacyPaths = append(legacyPaths, videoNFO)
		}
	}

	return nfoPath, legacyPaths
}

// findNFOFile resolves the NFO path and searches for an existing file,
// trying the primary path first then legacy paths in order.
// Returns the found path (empty string if none found).
// The engine parameter is forwarded to resolveNFOPath/ResolveNFOFilename.
func findNFOFile(fs afero.Fs, baseDir string, movie *models.Movie, cfg NFONameConfig, videoFilePath string, engine template.EngineInterface) string {
	nfoPath, legacyPaths := resolveNFOPath(baseDir, movie, cfg, videoFilePath, engine)

	if _, err := fs.Stat(nfoPath); err == nil {
		return nfoPath
	}

	for _, legacyPath := range legacyPaths {
		if _, err := fs.Stat(legacyPath); err == nil {
			return legacyPath
		}
	}

	return ""
}
