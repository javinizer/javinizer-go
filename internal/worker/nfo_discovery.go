package worker

import (
	"os"

	"github.com/javinizer/javinizer-go/internal/logging"
)

func findExistingNFO(jobID string, fileIndex int, nfoPath string, legacyPaths []string) string {
	foundPath := ""
	if _, err := os.Stat(nfoPath); err == nil {
		foundPath = nfoPath
	} else {
		for _, legacyPath := range legacyPaths {
			if _, err := os.Stat(legacyPath); err == nil {
				foundPath = legacyPath
				logging.Debugf("[Batch %s] File %d: Found NFO at legacy path: %s", jobID, fileIndex, legacyPath)
				break
			}
		}
	}
	return foundPath
}
