package runtime

import (
	"os"
	"path/filepath"
)

// DataDir returns the base directory for runtime data.
// It checks JAVINIZER_DATA_DIR environment variable first, then falls back to "data" relative to the working directory.
func DataDir() string {
	if dir := os.Getenv("JAVINIZER_DATA_DIR"); dir != "" {
		return dir
	}
	// Default to data/ relative to working directory
	return "data"
}

// UpdateStatePath returns the path to the update cache file.
func UpdateStatePath() string {
	return filepath.Join(DataDir(), "update_cache.json")
}
