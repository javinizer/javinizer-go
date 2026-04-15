package batch

import (
	"os"

	"github.com/javinizer/javinizer-go/internal/logging"
)

func cleanupPosterPaths(paths []string) {
	for _, posterPath := range paths {
		if _, err := os.Stat(posterPath); err == nil {
			if err := os.Remove(posterPath); err != nil {
				logging.Warnf("[Rescrape] Failed to remove old temp poster %s: %v", posterPath, err)
			} else {
				logging.Infof("[Rescrape] Removed old temp poster %s", posterPath)
			}
		}
	}
}
