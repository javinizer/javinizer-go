package fsutil

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// KeyedLockRegistry provides per-key mutexes with reference-counted eviction
// (ported fresh from the worker edit-lock machinery, exported for the
// downloader/revert-ledger destination registry). Keys are case-folded so an
// ID's case variants contend on the same mutex. Locks are evicted once the
// last holder releases, keeping the registry bounded.
type KeyedLockRegistry struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	mu   sync.Mutex
	refs int
}

// NewKeyedLockRegistry returns an empty registry.
func NewKeyedLockRegistry() *KeyedLockRegistry {
	return &KeyedLockRegistry{locks: make(map[string]*keyedLock)}
}

func foldKeyedLock(s string) string {
	return strings.ToUpper(DestKey(s))
}

// DestKey canonicalizes a destination path for CROSS-FORM comparisons
// (codex P3 R12-1/R17-1): separator/case folding applies ONLY on Windows —
// there `\` is necessarily a separator and names are necessarily case-
// insensitive. POSIX (including macOS, whose APFS volume MAY be case-
// sensitive) folds NOTHING: a fabricated match between two real distinct
// files is worse than a missed alias between two spellings of one file,
// because missed matches fail CLOSED (retain backups, reject restores).
func DestKey(p string) string {
	if runtime.GOOS != "windows" {
		return filepath.Clean(strings.TrimSpace(p))
	}
	s := strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	s = filepath.Clean(s)
	return strings.ToLower(s)
}

// Acquire blocks until the mutex for key is held and returns a release
// function. The release function MUST be called exactly once.
func (r *KeyedLockRegistry) Acquire(key string) func() {
	folded := foldKeyedLock(key)
	r.mu.Lock()
	kl, ok := r.locks[folded]
	if !ok {
		kl = &keyedLock{}
		r.locks[folded] = kl
	}
	kl.refs++
	r.mu.Unlock()

	kl.mu.Lock()

	return func() {
		kl.mu.Unlock()
		r.mu.Lock()
		kl.refs--
		if kl.refs == 0 {
			delete(r.locks, folded)
		}
		r.mu.Unlock()
	}
}

// AcquirePair acquires two keys atomically in lexical (folded) order to avoid
// AB-BA deadlock for cross-key operations. Identical folded keys acquire once.
func (r *KeyedLockRegistry) AcquirePair(a, b string) func() {
	return r.AcquireMany([]string{a, b})
}

// AcquireMany acquires an arbitrary key set atomically: globally sorted
// (folded) order, deduped — THE total order for multi-key operations.
func (r *KeyedLockRegistry) AcquireMany(keys []string) func() {
	folded := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		fk := foldKeyedLock(k)
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

// sharedJournalLocks serializes read-modify-write mutation of one journal
// ROW across the workflow recorder, the reverter's consumption, and the
// sweeper (codex P3 R15-1): a sweeper updating a row snapshot from an
// index-time read could otherwise erase entries confirmed meanwhile.
var sharedJournalLocks = NewKeyedLockRegistry()

// SharedJournalLocks returns the process-wide per-operation-row journal registry.
func SharedJournalLocks() *KeyedLockRegistry { return sharedJournalLocks }

// sharedDestLocks is the process-wide destination lock registry shared by
// the downloader's overwrite discipline and the history reverter's restore
// path (POSTER-WRITE-HARDENING P3 D8).
var sharedDestLocks = NewKeyedLockRegistry()

// SharedDestLocks returns the process-wide per-destination lock registry.
func SharedDestLocks() *KeyedLockRegistry { return sharedDestLocks }
