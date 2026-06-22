package workflow

import (
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

func generateNFOPaths(movie *models.Movie, fileResults []models.FileMatchInfo, nfoNameCfg nfo.NFONameConfig, perFile bool, nfoEnabled bool, nfoIface nfo.NFOFieldMerger, folderPath string) (string, []string) {
	if !nfoEnabled || nfoIface == nil {
		return "", nil
	}

	isMultiPart := false
	for _, result := range fileResults {
		if result.IsMultiPart {
			isMultiPart = true
			break
		}
	}
	generatePerFileNFO := perFile && isMultiPart

	var nfoPath string
	var nfoPaths []string

	if generatePerFileNFO {
		nfoPaths = make([]string, 0, len(fileResults))
		for _, result := range fileResults {
			if result.Path != "" {
				nfCfg := nfoNameCfg
				nfCfg.IsMultiPart = result.IsMultiPart
				nfCfg.PartSuffix = result.PartSuffix
				nfoFileName := nfoIface.ResolveNFOFilename(movie, nfCfg)
				nfoFilePath := organizer.JoinPath(folderPath, nfoFileName)
				nfoPaths = append(nfoPaths, nfoFilePath)
			}
		}
		if len(nfoPaths) > 0 {
			nfoPath = nfoPaths[0]
		}
	} else {
		nfCfg := nfoNameCfg
		nfCfg.IsMultiPart = isMultiPart
		nfoFileName := nfoIface.ResolveNFOFilename(movie, nfCfg)
		nfoPath = organizer.JoinPath(folderPath, nfoFileName)
	}

	return nfoPath, nfoPaths
}

func validatePathLengths(logger logging.Logger, maxPathLength int, templateEngine template.EngineInterface, videoFiles []string, nfoPath string, nfoPaths []string, posterPath string, fanartPath string, extrafanartPath string, screenshots []string) {
	if maxPathLength <= 0 {
		return
	}

	for _, videoPath := range videoFiles {
		if err := templateEngine.ValidatePathLength(videoPath, maxPathLength); err != nil {
			logger.Warnf("Preview: video path exceeds max length: %s (length: %d, max: %d)", videoPath, len(videoPath), maxPathLength)
		}
	}
	if nfoPath != "" {
		if err := templateEngine.ValidatePathLength(nfoPath, maxPathLength); err != nil {
			logger.Warnf("Preview: NFO path exceeds max length: %s (length: %d, max: %d)", nfoPath, len(nfoPath), maxPathLength)
		}
	}
	for _, nfoFilePath := range nfoPaths {
		if err := templateEngine.ValidatePathLength(nfoFilePath, maxPathLength); err != nil {
			logger.Warnf("Preview: NFO path exceeds max length: %s (length: %d, max: %d)", nfoFilePath, len(nfoFilePath), maxPathLength)
		}
	}
	if err := templateEngine.ValidatePathLength(posterPath, maxPathLength); err != nil {
		logger.Warnf("Preview: poster path exceeds max length: %s (length: %d, max: %d)", posterPath, len(posterPath), maxPathLength)
	}
	if err := templateEngine.ValidatePathLength(fanartPath, maxPathLength); err != nil {
		logger.Warnf("Preview: fanart path exceeds max length: %s (length: %d, max: %d)", fanartPath, len(fanartPath), maxPathLength)
	}
	for _, screenshot := range screenshots {
		screenshotPath := organizer.JoinPath(extrafanartPath, screenshot)
		if err := templateEngine.ValidatePathLength(screenshotPath, maxPathLength); err != nil {
			logger.Warnf("Preview: screenshot path exceeds max length: %s (length: %d, max: %d)", screenshotPath, len(screenshotPath), maxPathLength)
		}
	}
}
