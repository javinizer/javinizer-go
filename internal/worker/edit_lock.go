package worker

import (
	"sort"
	"strings"
	"sync"

	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// keyedMutexRegistry provides per-key mutexes with reference-counted eviction
// (POSTER-WRITE-HARDENING D1). Keys are case-folded so an ID's case variants
// contend on the same mutex. Locks are evicted from the map once the last
// holder releases, keeping the registry bounded.
type keyedMutexRegistry struct {
	mu    sync.Mutex
	locks map[string]*keyedMutex
}

type keyedMutex struct {
	mu   sync.Mutex
	refs int
}

func newKeyedMutexRegistry() *keyedMutexRegistry {
	return &keyedMutexRegistry{locks: make(map[string]*keyedMutex)}
}

func foldLockKey(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// Acquire blocks until the mutex for key is held and returns a release
// function. The release function MUST be called exactly once.
func (r *keyedMutexRegistry) Acquire(key string) func() {
	folded := foldLockKey(key)
	r.mu.Lock()
	km, ok := r.locks[folded]
	if !ok {
		km = &keyedMutex{}
		r.locks[folded] = km
	}
	km.refs++
	r.mu.Unlock()

	km.mu.Lock()

	return func() {
		km.mu.Unlock()
		r.mu.Lock()
		km.refs--
		if km.refs == 0 {
			delete(r.locks, folded)
		}
		r.mu.Unlock()
	}
}

// AcquirePair acquires two keys atomically in lexical (folded) order to avoid
// AB-BA deadlock for cross-key operations such as renames (D1 dual-key rule).
// Identical folded keys acquire once.
func (r *keyedMutexRegistry) AcquirePair(a, b string) func() {
	return r.AcquireMany([]string{a, b})
}

// AcquireMany acquires an arbitrary key set atomically: globally sorted
// (folded) order, deduped. This is THE total-order for multi-entity edits —
// family rekeys fold movie-ID + content-ID surfaces onto ONE lock section
// (codex r13: cross-job rows contend on the same registry).
func (r *keyedMutexRegistry) AcquireMany(keys []string) func() {
	folded := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		fk := foldLockKey(k)
		if fk == "" {
			continue
		}
		if _, ok := seen[fk]; ok {
			continue
		}
		seen[fk] = struct{}{}
		folded = append(folded, fk)
	}
	sort.Strings(folded)
	if len(folded) == 0 {
		return func() {}
	}
	releases := make([]func(), 0, len(folded))
	for _, k := range folded {
		releases = append(releases, r.Acquire(k))
	}
	return func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
}

// familyKeyedResultMap routes the rescrape phase's CommitResult through the
// per-family edit key so it serializes with review edits (codex r20 P1):
// the network leg runs unlocked, the commit leg is serialized.
type familyKeyedResultMap struct {
	resultstore.ResultMapAccessor
	registry *keyedMutexRegistry
}

func (w *familyKeyedResultMap) CommitResult(filePath string, result *resultstore.MovieResult, expectedRevision uint64) error {
	// codex r28+r29: edits lock with the matcher alias; rescrape reads the
	// canonical Movie.ID of the CURRENT result and the INCOMING identity of
	// the rescrape candidate. Acquire ALL of them atomically — acquiring
	// only one lets a concurrent edit use the OTHER key and interleave
	// writes across the commit boundary.
	// codex r30: GetCurrentMovieID already prefers the canonical Movie.ID —
	// the MATCHER alias (FileMatchInfo.MovieID) is what edits lock on. Include
	// it too so any review edit and any rescrape of this file collide on the
	// same key set.
	key := w.GetCurrentMovieID(filePath)
	pair := []string{key}
	if fm, ok := w.GetFileMatchInfo(filePath); ok && fm.MovieID != "" && !strings.EqualFold(strings.TrimSpace(fm.MovieID), strings.TrimSpace(key)) {
		pair = append(pair, fm.MovieID)
	}
	if cur, err := w.GetMovieResult(filePath); err == nil && cur != nil && cur.Movie != nil && cur.Movie.ID != "" && !strings.EqualFold(strings.TrimSpace(cur.Movie.ID), strings.TrimSpace(key)) {
		pair = append(pair, cur.Movie.ID)
	}
	if result != nil && result.Movie != nil && result.Movie.ID != "" && !strings.EqualFold(strings.TrimSpace(result.Movie.ID), strings.TrimSpace(key)) {
		pair = append(pair, result.Movie.ID)
	}
	// codex r34: fold the stored content-id (PK) into the commit lock — the
	// PATCH path keys it via "cid:" and a rescrape commit writes the same
	// movie row.
	if cur, err := w.GetMovieResult(filePath); err == nil && cur != nil && cur.Movie != nil {
		if cid := strings.TrimSpace(cur.Movie.ContentID); cid != "" {
			pair = append(pair, "cid:"+cid)
		}
	}
	release := w.registry.AcquireMany(pair)
	defer release()
	return w.ResultMapAccessor.CommitResult(filePath, result, expectedRevision)
}
