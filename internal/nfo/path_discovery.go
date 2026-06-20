package nfo

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

var osStat = os.Stat

// ResolveNFOPath builds the expected NFO file path and a list of legacy paths
// to check for backward compatibility.
func ResolveNFOPath(baseDir string, movie *models.Movie, nfoFilenameTemplate string, groupActress bool, groupActressName string, groupUnknownActressName string, firstNameOrder bool, perFile bool, isMultiPart bool, partSuffix string, videoFilePath string) (nfoPath string, legacyPaths []string) {
	nfoFilename := ResolveNFOFilename(movie, nfoFilenameTemplate, groupActress, groupActressName, groupUnknownActressName, firstNameOrder, perFile, isMultiPart, partSuffix)
	nfoPath = filepath.Join(baseDir, nfoFilename)

	if nfoFilename != movie.ID+".nfo" {
		legacyPaths = append(legacyPaths, filepath.Join(baseDir, movie.ID+".nfo"))
	}

	if perFile && isMultiPart && videoFilePath != "" {
		videoName := strings.TrimSuffix(filepath.Base(videoFilePath), filepath.Ext(videoFilePath))
		videoNFO := filepath.Join(baseDir, videoName+".nfo")
		if videoNFO != nfoPath {
			legacyPaths = append(legacyPaths, videoNFO)
		}
	}

	return nfoPath, legacyPaths
}

// FindNFOFile resolves the NFO path and searches for an existing file,
// trying the primary path first then legacy paths in order.
// Returns the found path (empty string if none found).
func FindNFOFile(baseDir string, movie *models.Movie, nfoFilenameTemplate string, groupActress bool, groupActressName string, groupUnknownActressName string, firstNameOrder bool, perFile bool, isMultiPart bool, partSuffix string, videoFilePath string) string {
	nfoPath, legacyPaths := ResolveNFOPath(baseDir, movie, nfoFilenameTemplate, groupActress, groupActressName, groupUnknownActressName, firstNameOrder, perFile, isMultiPart, partSuffix, videoFilePath)

	if _, err := osStat(nfoPath); err == nil {
		return nfoPath
	}

	for _, legacyPath := range legacyPaths {
		if _, err := osStat(legacyPath); err == nil {
			return legacyPath
		}
	}

	return ""
}
