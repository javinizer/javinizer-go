package worker

import "sync"

// jobEnvelopeLockEntry is a reference-counted per-jobID mutex. It mirrors the
// posterSourceLockEntry pattern (poster_source_lock.go): entries carry a
// refcount so they can be evicted after the last holder releases, instead of
// accumulating one mutex per job for the process lifetime.
type jobEnvelopeLockEntry struct {
	mu   sync.Mutex
	refs int
}

var (
	jobEnvelopeLockEntries sync.Map // jobID -> *jobEnvelopeLockEntry
	jobEnvelopeLockGuard   sync.Mutex
)

// AcquireJobEnvelopeLock serializes the commit→persist→rollback window of
// whole-job-envelope writers that run CONCURRENTLY on the same job — the bulk
// rescrape pool workers (internal/api/batch/batch_rescrape.go) are the
// motivating case: each worker commits its own movie's ResultMap entry and
// persists the ENTIRE job envelope while peers commit other movies under
// their own poster-source locks. Without job-scope serialization, worker A's
// persist can durably capture worker B's just-committed state, and when B's
// own persist then fails and B rolls its entry back, the durable envelope
// still contains B's rejected rescrape — a restart resurrects it against the
// restored old poster cache while the API already reported failure.
//
// Holding this mutex from the CAS commit through the envelope persist AND any
// persist-failure rollback makes every persisted envelope pair-wise complete:
// an envelope persisted BEFORE B's commit can never contain B's commit (the
// commit is inside the window), and an envelope persisted AFTER B's failure
// contains only B's rolled-back state (the rollback is inside the window
// too). A persist that runs between earlier commits legitimately carries
// those committed states — they succeeded and will not be rolled back.
//
// Lock ordering: this mutex nests INSIDE the poster-source lock(s) the
// rescrape phase already holds for its whole critical section (poster-source
// lock(s) → job envelope lock → result-store/repo leaf locks). No path may
// acquire a poster-source lock while holding this mutex; its critical section
// touches only result-store snapshots, the job mutex, and repository locks,
// all leaf-level relative to the poster-source lock.
//
// The lock is package-level and keyed on jobID alone (not on the BatchJob
// instance) so that every writer contending for one job — API handlers, the
// bulk pool, and test-created jobs alike — shares the same mutex. The
// returned function releases the lock exactly once and evicts the map entry
// when the last holder returns, so the map never grows unboundedly.
func AcquireJobEnvelopeLock(jobID string) (release func()) {
	jobEnvelopeLockGuard.Lock()
	v, _ := jobEnvelopeLockEntries.Load(jobID)
	entry, ok := v.(*jobEnvelopeLockEntry)
	if !ok {
		entry = &jobEnvelopeLockEntry{}
		jobEnvelopeLockEntries.Store(jobID, entry)
	}
	entry.refs++
	jobEnvelopeLockGuard.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		jobEnvelopeLockGuard.Lock()
		entry.refs--
		if entry.refs == 0 {
			jobEnvelopeLockEntries.Delete(jobID)
		}
		jobEnvelopeLockGuard.Unlock()
	}
}
