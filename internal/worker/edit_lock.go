package worker

import (
	"sort"
	"strings"
	"sync"

	"github.com/spf13/afero"

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
	// updater publishes provenance inside the same keyed section as the
	// result commit (codex r36 P1); nil in bare test fixtures.
	updater  resultstore.ResultUpdater
	registry *keyedMutexRegistry
	// fs/tempDir/jobID feed the witness fence (audit F1): the rescrape commit
	// leg must not advance a family's revision while a promote/crop/rekey
	// witness is unresolved. Nil fs ⇒ no probe (bare test fixtures).
	fs      afero.Fs
	tempDir string
	jobID   string
}

// commitKeys computes the full identity key set for a commit: the matcher
// alias, the stored canonical and content-id, and the incoming candidate ID
// — any edit/rescrape of this file collides on the same set (codex r28–r34).
func (w *familyKeyedResultMap) commitKeys(filePath string, result *resultstore.MovieResult) []string {
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
	return pair
}

func (w *familyKeyedResultMap) CommitResult(filePath string, result *resultstore.MovieResult, expectedRevision uint64) error {
	return w.CommitResultWithProvenance(filePath, result, expectedRevision, nil)
}

// CommitResultWithProvenance commits the result AND publishes its provenance
// within ONE continuous family-locked section (codex r36 P1): a concurrent
// field override can never slip into a post-commit/pre-provenance gap, read
// the new movie with stale attribution, and then have its commit overwritten
// by the rescrape's provenance write.
func (w *familyKeyedResultMap) CommitResultWithProvenance(filePath string, result *resultstore.MovieResult, expectedRevision uint64, prov *resultstore.ProvenanceData) error {
	release := w.registry.AcquireMany(w.commitKeys(filePath, result))
	defer release()
	// audit F1: re-probe witnesses UNDER the family key — a fence-exempt
	// rescrape commit would otherwise advance the revision while canonical
	// bytes are mid-recovery, misflipping startup arbitration to committed.
	if w.fs != nil {
		seen := map[string]struct{}{}
		var storedID string
		if cur, err := w.GetMovieResult(filePath); err == nil && cur != nil && cur.Movie != nil {
			storedID = strings.TrimSpace(cur.Movie.ID)
		}
		var newID string
		if result != nil && result.Movie != nil {
			newID = strings.TrimSpace(result.Movie.ID)
		}
		for _, pid := range []string{storedID, newID} {
			if pid == "" {
				continue
			}
			if _, dup := seen[strings.ToLower(pid)]; dup {
				continue
			}
			seen[strings.ToLower(pid)] = struct{}{}
			if err := posterWitnessConflictCore(w.fs, w.tempDir, w.jobID, pid); err != nil {
				return err
			}
		}
	}
	if err := w.ResultMapAccessor.CommitResult(filePath, result, expectedRevision); err != nil {
		return err
	}
	if prov != nil && (prov.FieldSources != nil || prov.ActressSources != nil || prov.ScraperResults != nil) && w.updater != nil {
		w.updater.SetProvenance(filePath, prov)
	}
	return nil
}
