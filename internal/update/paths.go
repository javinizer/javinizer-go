package update

import (
	"os"
	"path/filepath"
)

// dataDir returns the base directory for runtime data.
// It checks JAVINIZER_DATA_DIR environment variable first, then falls back to "data" relative to the working directory.
func dataDir() string {
	if dir := os.Getenv("JAVINIZER_DATA_DIR"); dir != "" {
		return dir
	}
	// Default to data/ relative to working directory
	return "data"
}

// updateStatePath returns the path to the update cache file.
func updateStatePath() string {
	return filepath.Join(dataDir(), "update_cache.json")
}
