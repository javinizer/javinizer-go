package worker

import (
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

// CleanupPosterPaths removes each existing file in the given paths list.
// Logs a warning if removal fails; silently skips non-existent files.
// If fs is nil, the real OS filesystem is used.
func CleanupPosterPaths(fs afero.Fs, paths []string) {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	for _, posterPath := range paths {
		if _, err := fs.Stat(posterPath); err == nil {
			if err := fs.Remove(posterPath); err != nil {
				logging.Warnf("[Rescrape] Failed to remove old temp poster %s: %v", posterPath, err)
			} else {
				logging.Infof("[Rescrape] Removed old temp poster %s", posterPath)
			}
		}
	}
}

// CleanupMoviePosters removes poster files for a movie in the job's temp directory.
// Builds poster paths from tempDir, jobID, and movie ID, then delegates to CleanupPosterPaths.
func CleanupMoviePosters(fs afero.Fs, tempDir string, jobID models.JobID, movie *models.Movie) {
	if movie != nil && movie.ID != "" {
		CleanupPosterPaths(fs, []string{
			filepath.Join(tempDir, "posters", jobID.String(), movie.ID+".jpg"),
			filepath.Join(tempDir, "posters", jobID.String(), movie.ID+"-full.jpg"),
		})
	}
}

// OrphanedPosterPaths builds a list of poster file paths for orphaned movie IDs.
// When a movie ID changes during rescrape, the old ID's poster files become orphaned.
// On case-insensitive filesystems, a case-only ID change is not treated as orphaned
// (the files are the same), so those paths are skipped.
// The cache parameter provides per-job filesystem case-sensitivity probing.
func OrphanedPosterPaths(orphanedIDs []string, newMovieID string, tempDir string, jobID models.JobID, cache *FSCaseCache) []string {
	var paths []string
	for _, id := range orphanedIDs {
		if strings.EqualFold(id, newMovieID) {
			posterDir := filepath.Join(tempDir, "posters", jobID.String())
			if cache == nil || cache.IsCaseInsensitive(posterDir) {
				logging.Infof("[Rescrape] Case change detected (%s → %s), skipping poster cleanup (case-insensitive filesystem)", id, newMovieID)
				continue
			}
			logging.Infof("[Rescrape] Case change detected (%s → %s) on case-sensitive filesystem, cleaning up poster", id, newMovieID)
		}
		paths = append(paths,
			filepath.Join(tempDir, "posters", jobID.String(), id+".jpg"),
			filepath.Join(tempDir, "posters", jobID.String(), id+"-full.jpg"),
		)
	}
	return paths
}
