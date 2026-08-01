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
// across the four paths that mutate the shared cached poster assets and the
// movie's poster state together: the whole-movie PATCH handler
// (internal/api/batch/movie_edit.go's updateBatchMovie), the field-
// override path (jobEditorImpl.ApplyFieldOverride), the manual-crop endpoint
// (updateBatchMoviePosterCrop), and the poster-from-URL refresh endpoint
// (updateBatchMoviePosterFromURL). Without it, two
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
// Lock ordering: ApplyFieldOverride takes its per-resultID overrideMu BEFORE
// this lock and no path reverses that order, so the two cannot deadlock.
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
