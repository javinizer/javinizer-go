package worker

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/fscase"
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

// rescrapePosterBackup parks the pre-generation canonical pair aside
// (<id>.rsbak) so a failed/conflicted rescrape can RESTORE the committed
// state's bytes instead of leaving the loser's bytes at canonical names
// (audit F-R3-2a). Crash leftovers are inert litter the staleness sweep owns.
type rescrapePosterBackup struct {
	fs      afero.Fs
	full    string
	crop    string
	hadFull bool
	hadCrop bool
}

// parkCanonicalPosterPair moves pre-existing canonical legs aside. Stat
// errors are fail-closed (audit F-R3-1): an unreadable-but-existing leg marks
// had=true so later cleanup never treats the leg as op-created.
func parkCanonicalPosterPair(fs afero.Fs, dir, id string) *rescrapePosterBackup {
	b := &rescrapePosterBackup{fs: fs, full: filepath.Join(dir, id+"-full.jpg"), crop: filepath.Join(dir, id+".jpg")}
	if fs == nil || dir == "" || id == "" {
		return b
	}
	legs := []struct {
		path string
		had  *bool
	}{{b.full, &b.hadFull}, {b.crop, &b.hadCrop}}
	for _, leg := range legs {
		if _, err := fs.Stat(leg.path); err != nil {
			if !os.IsNotExist(err) {
				logging.Warnf("rescrape pair backup stat %s: %v — treated as pre-existing", leg.path, err)
				*leg.had = true
			}
			continue
		}
		if err := fs.Rename(leg.path, leg.path+".rsbak"); err != nil {
			logging.Warnf("rescrape pair backup park %s: %v", leg.path, err)
			continue
		}
		*leg.had = true
	}
	return b
}

// restore returns parked bytes to their canonical names (removing whatever
// the failed op wrote there first). Best-effort, logged.
func (b *rescrapePosterBackup) restore() {
	if b == nil || b.fs == nil {
		return
	}
	legs := []struct {
		path string
		had  bool
	}{{b.full, b.hadFull}, {b.crop, b.hadCrop}}
	for _, leg := range legs {
		if !leg.had {
			continue
		}
		bak := leg.path + ".rsbak"
		if _, err := b.fs.Stat(bak); err != nil {
			continue
		}
		_ = b.fs.Remove(leg.path)
		if err := b.fs.Rename(bak, leg.path); err != nil {
			logging.Warnf("rescrape pair restore %s: %v", leg.path, err)
		}
	}
}

// discard drops parked bytes after a successful operation.
func (b *rescrapePosterBackup) discard() {
	if b == nil || b.fs == nil {
		return
	}
	for _, p := range []string{b.full + ".rsbak", b.crop + ".rsbak"} {
		_ = b.fs.Remove(p)
	}
}

// OrphanedPosterPaths builds a list of poster file paths for orphaned movie IDs.
// When a movie ID changes during rescrape, the old ID's poster files become orphaned.
// On case-insensitive filesystems, a case-only ID change is not treated as orphaned
// (the files are the same), so those paths are skipped.
// The cache parameter provides per-job filesystem case-sensitivity probing.
func OrphanedPosterPaths(orphanedIDs []string, newMovieID string, tempDir string, jobID models.JobID, cache *fscase.FSCaseCache) []string {
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
