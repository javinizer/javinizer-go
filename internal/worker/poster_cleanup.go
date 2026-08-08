package worker

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

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

// rescrapePosterBackup parks the pre-generation canonical pair aside so a
// failed/conflicted rescrape can RESTORE the committed state's bytes instead
// of leaving the loser's bytes at canonical names (audit F-R3-2a). Park paths
// carry a per-op nonce (audit F-R4-4): two concurrent ops on the same ID must
// never clobber or restore each other's backups. Crash leftovers are re-homed
// by TempDirCleaner.reconcileParkedPosterBackups (audit F-R4-5).
type rescrapePosterBackup struct {
	fs      afero.Fs
	full    string
	crop    string
	fullBak string
	cropBak string
	hadFull bool
	hadCrop bool
	// parkErr carries the "abandon parking" signal to the caller (audit
	// codex-P2: a leg that refuses to move leaves generation WITHOUT a
	// recoverable copy — the rescrape must abort generation instead).
	parkErr error
	// markerPath is the per-op in-flight sentinel (audit F-R19-1): written
	// even when the op parked NOTHING, so orphan sweeps + edit fences can
	// never see an in-flight generation's bytes as deleteable litter.
	markerPath string
}

var rescrapeBackupSeq atomic.Int64

// parkCanonicalPosterPair moves pre-existing canonical legs aside. Stat
// errors are fail-closed (audit F-R3-1): an unreadable-but-existing leg marks
// had=true so later cleanup never treats the leg as op-created.
func parkCanonicalPosterPair(fs afero.Fs, dir, id string) *rescrapePosterBackup {
	b := &rescrapePosterBackup{fs: fs}
	if fs == nil || dir == "" || id == "" {
		return b
	}
	// codex P1 (@poster_cleanup): park paths derive directly from the ID —
	// isSafePosterFileID gates filepath construction, NOT the scraper-shaped

	if !isSafePosterFileID(id) {
		logging.Warnf("rescrape pair backup skipped: unsafe poster ID %q", id)
		return b
	}
	b.full = filepath.Join(dir, id+"-full.jpg")
	b.crop = filepath.Join(dir, id+".jpg")
	nonce := fmt.Sprintf("%x.%x", time.Now().UnixNano(), rescrapeBackupSeq.Add(1))
	b.fullBak = b.full + ".rsbak." + nonce
	b.cropBak = b.crop + ".rsbak." + nonce
	// audit F-R19-1: ALWAYS write the in-flight sentinel — "nothing to park"
	// no longer reads as "nothing in flight". Startup reconciliation removes
	// stranded sentinels (a live process can't hold one across a restart).
	// audit F-R20-1: the dir CAN be absent on a job where no download ran yet —
	// creating it is matching DownloadFromURL's own first-step invariant.
	if dErr := b.fs.MkdirAll(dir, 0o755); dErr != nil {
		logging.Warnf("in-flight marker dir %s: %v", dir, dErr)
		return b
	}
	b.markerPath = filepath.Join(dir, ".inflight-"+url.PathEscape(id)+"."+nonce)
	if mErr := afero.WriteFile(b.fs, b.markerPath, nil, 0o644); mErr != nil {
		logging.Warnf("in-flight marker write %s: %v", b.markerPath, mErr)
		b.markerPath = ""
	}
	legs := []struct {
		path string
		bak  string
		had  *bool
	}{{b.full, b.fullBak, &b.hadFull}, {b.crop, b.cropBak, &b.hadCrop}}
	for _, leg := range legs {
		if _, err := fs.Stat(leg.path); err != nil {
			if !os.IsNotExist(err) {
				logging.Warnf("rescrape pair backup stat %s: %v — treated as pre-existing", leg.path, err)
				*leg.had = true // fail-closed (audit F-R3-1)
			}
			continue
		}
		if err := fs.Rename(leg.path, leg.bak); err != nil {
			logging.Warnf("rescrape pair backup park %s: %v — refusing generation", leg.path, err)
			// codex P2 (@poster_cleanup): a failed park on an EXISTING leg must
			// abort generation — no recoverable copy would survive the loss.
			b.parkErr = fmt.Errorf("poster backup park %s: %w", leg.path, err)
			continue
		}
		*leg.had = true
	}
	return b
}

// restore returns parked bytes to their canonical names (removing whatever
// the failed op wrote there first). Best-effort, logged. When verify is
// non-nil it is consulted PER-LEG against the CURRENT canonical content
// (audit F-R5-1): allowed=false+undecidable=false ⇒ a concurrent winner's
// committed bytes sit there (dispose the obsolete copy); undecidable=true ⇒
// canonical content couldn't be read — keep the parked copy for arbitration
// (codex P2, never rewind blind).
func (b *rescrapePosterBackup) restore(verify func(legPath string) (allowed bool, undecidable bool)) {
	if b == nil || b.fs == nil {
		return
	}
	legs := []struct {
		path string
		bak  string
		had  bool
	}{{b.full, b.fullBak, b.hadFull}, {b.crop, b.cropBak, b.hadCrop}}
	for _, leg := range legs {
		if !leg.had {
			continue
		}
		if _, err := b.fs.Stat(leg.bak); err != nil {
			continue
		}
		if verify != nil {
			allowed, undecidable := verify(leg.path)
			if !allowed && undecidable {
				// codex P2: canonical unreadable — NEVER rewind blind and never
				// dispose the only recovery copy; the reconciler arbitrates later.
				logging.Warnf("rescrape pair restore %s skipped: canonical undecidable — parked copy kept for arbitration", leg.path)
				continue
			}
			if !allowed {
				// audit F-R10-2: canonical holds NEWER committed bytes — the parked
				// pre-op copy is obsolete: dispose it so the parked-marker fences
				// don't brick poster admissions until restart.
				if rmErr := b.fs.Remove(leg.bak); rmErr != nil {
					logging.Warnf("rescrape parked dispose %s: %v", leg.bak, rmErr)
				}
				logging.Warnf("rescrape pair restore %s skipped: canonical holds newer committed bytes — parked copy disposed", leg.path)
				continue
			}
		}
		_ = b.fs.Remove(leg.path)
		if err := b.fs.Rename(leg.bak, leg.path); err != nil {
			logging.Warnf("rescrape pair restore %s: %v", leg.path, err)
		}
	}
	// audit F-R19-1 lifecycle: the in-flight sentinel belongs to this op —
	// once its bytes are settled (restored/disposed), the marker's purpose is
	// spent. Crash-tolerant restart also sweeps stranded markers.
	if b.markerPath != "" {
		_ = b.fs.Remove(b.markerPath)
	}
}

// discard drops parked bytes after a successful operation.
func (b *rescrapePosterBackup) discard() {
	if b == nil || b.fs == nil {
		return
	}
	for _, p := range []string{b.fullBak, b.cropBak, b.markerPath} {
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
		// codex P1: orphaned IDs flow from committed scraper state that the
		// poster manager's validation never rejected the COMMIT — a legacy
		// "../victim" ID would join outside the job dir. Build nothing.
		if !isSafePosterFileID(id) {
			logging.Warnf("[Rescrape] orphan sweep skipped unsafe poster ID %q", id)
			continue
		}
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
