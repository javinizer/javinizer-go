package worker

import "sync"

// posterSourceLockEntry is a reference-counted per-key mutex. It mirrors the
// downloader's posterCropLock pattern (internal/downloader/media.go): entries
// carry a refcount so they can be evicted after the last holder releases,
// instead of accumulating one mutex per job/movie for the process lifetime.
type posterSourceLockEntry struct {
	mu   sync.Mutex
	refs int
}

var (
	posterSourceLockEntries sync.Map // jobID+"\x00"+movieID -> *posterSourceLockEntry
	posterSourceLockGuard   sync.Mutex
)

// AcquirePosterSourceLock serializes the poster-source "snapshot old assets →
// refresh/cleanup → persist movie" sequence for one (jobID, movieID) pair
// across the five paths that mutate the shared cached poster assets and the
// movie's poster state together: the whole-movie PATCH handler
// (internal/api/batch/movie_edit.go's updateBatchMovie), the field-
// Override path (jobEditorImpl.ApplyFieldOverride), the manual-crop endpoint
// (updateBatchMoviePosterCrop), the poster-from-URL refresh endpoint
// (updateBatchMoviePosterFromURL), and the rescrape phase
// (rescrapePhase.Rescrape, which replaces the shared -full.jpg via
// GeneratePoster and commits the scraped result with a CAS revision — both
// under this lock; see rescrape_phase.go). The override path takes this lock for
// EVERY field key, not just poster sources: every override persists a
// whole-movie clone, which must not interleave with a crop or source write
// mid-flight (the clone would otherwise erase the other edit). Without it, two
// concurrent source-changing edits can interleave — request A refreshes the
// cached {movieID}-full.jpg from image A, request B refreshes from image B,
// and B persists before A — leaving the job's final poster URL pointing at A
// while the cached -full.jpg is B. A subsequent manual crop is then measured
// against B while Organize downloads A.
//
// The key is the same (jobID, movieID) pair the temp poster cache files and
// the crop bounds are keyed on, so per-poster granularity is natural. The
// lock is package-level so the API handlers and every job's editor contend on
// the same instance for a given key. Callers must hold it across the whole
// refresh+persist sequence (including any multipart loop and compensation).
//
// Invariant: NO poster-state mutation happens outside this lock. That
// includes the job-envelope persist after the rescrape commit and the
// field-override fan-out: rescrapePhase.Rescrape and ApplyFieldOverride
// invoke the error-returning envelope persist (BatchJobDeps.PersistErrFn)
// INSIDE their critical sections, and compensate rollbacks there, so a
// failed persist is reverted before any other writer can interleave. The
// persist's own locks (result-store snapshots, job mutex, SQLite/repo
// locks) are leaf-level relative to this lock — nothing acquires a
// poster-source lock while holding one of them — so the SQLite write under
// the lock cannot cycle. The pipeline write-backs that touch poster fields
// (apply-phase success/failure/panic, the scrape persist pool's DB
// round-trip) also take this lock around their atomic result updates,
// keyed on the LIVE result's movie ID, and skip the movie write entirely
// when the live identity was re-keyed mid-flight.
//
// Lock ordering: ApplyFieldOverride takes its per-resultID overrideMu BEFORE
// this lock and no path reverses that order, so the two cannot deadlock.
// Two-lock rule: paths that re-resolve their key under the lock (the crop
// endpoint in internal/api/batch/movie_edit_poster.go and the non-"id"
// ApplyFieldOverride convergence) RELEASE the old key before acquiring the
// destination key, so they never hold two of these locks at once. Two paths
// are permitted to hold a (origin, destination) PAIR, always in lexical key
// order so opposing operations cannot deadlock, releasing and re-acquiring
// the origin when the destination sorts first: rescrapePhase.Rescrape on an
// A→B rekey, and ApplyFieldOverride's "id" override (which migrates the
// cached poster assets from the old key to the new one under both locks).
//
// The returned function releases the lock exactly once and evicts the map
// entry when the last holder returns, so the map never grows unboundedly.
func AcquirePosterSourceLock(jobID, movieID string) (release func()) {
	key := jobID + "\x00" + movieID

	posterSourceLockGuard.Lock()
	v, _ := posterSourceLockEntries.Load(key)
	entry, ok := v.(*posterSourceLockEntry)
	if !ok {
		entry = &posterSourceLockEntry{}
		posterSourceLockEntries.Store(key, entry)
	}
	entry.refs++
	posterSourceLockGuard.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		posterSourceLockGuard.Lock()
		entry.refs--
		if entry.refs == 0 {
			posterSourceLockEntries.Delete(key)
		}
		posterSourceLockGuard.Unlock()
	}
}
