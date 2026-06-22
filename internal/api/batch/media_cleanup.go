package batch

import (
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/spf13/afero"
)

// cleanupJobTempPosters removes temp posters for a completed or cancelled job
// Best-effort, non-blocking cleanup. Called in a goroutine.
func cleanupJobTempPosters(fs afero.Fs, jobID string, tempDir string) {
	if err := poster.ValidateJobID(jobID); err != nil {
		logging.Warnf("[Job] Refusing to clean temp poster dir with invalid job ID: %v", err)
		return
	}
	posterDir := filepath.Join(tempDir, "posters", jobID)
	if err := fsutil.AferoRemoveAll(fs, posterDir); err != nil {
		logging.Warnf("[Job %s] Failed to clean temp poster dir: %v", jobID, err)
	} else {
		logging.Debugf("[Job %s] Cleaned up temporary poster directory: %s", jobID, posterDir)
	}
}
