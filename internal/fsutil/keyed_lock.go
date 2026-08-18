package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	// Acquisition deliberately remains folded even on case-sensitive volumes:
	// extra contention is harmless. normalizeDestPath still applies platform
	// separator semantics, so POSIX backslashes remain distinct filename chars.
	return strings.ToUpper(strings.ToLower(normalizeDestPath(s)))
}

// PathBackslashesAreSeparators is the path-separator seam used by destination
// keys and keyed locks. It defaults to the runtime platform behavior, while
// tests may select either platform's spelling rules on one host.
var PathBackslashesAreSeparators = runtime.GOOS == "windows"

// caseSensitivityProbe is intentionally injectable so filesystem semantics can
// be forced by tests without changing the journal format.
type caseSensitivityProbe func(root string) (bool, error)

// CaseSensitiveProbe is the process-wide probe seam. A probe error is treated
// as case-insensitive by IsCaseSensitiveRoot (fail closed).
var CaseSensitiveProbe caseSensitivityProbe = defaultCaseSensitiveProbe

var (
	caseSensitivityCacheMu sync.Mutex
	caseSensitivityCache   = make(map[string]bool)
	caseProbeOrdinal       atomic.Uint64
)

// ResetCaseSensitivityCache clears the process cache. It is primarily a test
// seam; production callers should retain the one-probe-per-root lifetime.
func ResetCaseSensitivityCache() {
	caseSensitivityCacheMu.Lock()
	caseSensitivityCache = make(map[string]bool)
	caseSensitivityCacheMu.Unlock()
}

// IsCaseSensitiveRoot reports the cached case behavior for root. Probe errors,
// nil probe seams, and unwritable roots all resolve to false (insensitive).
func IsCaseSensitiveRoot(root string) bool {
	root = cleanProbeRoot(root)
	caseSensitivityCacheMu.Lock()
	defer caseSensitivityCacheMu.Unlock()
	if result, ok := caseSensitivityCache[root]; ok {
		return result
	}
	probe := CaseSensitiveProbe
	if probe == nil {
		caseSensitivityCache[root] = false
		return false
	}
	result, err := probe(root)
	if err != nil {
		result = false
	}
	caseSensitivityCache[root] = result
	return result
}

// DestKey canonicalizes a destination path for CROSS-FORM comparisons while
// respecting the destination root's filesystem semantics. Backslash
// separators are normalized only when PathBackslashesAreSeparators is true.
// Case is folded on insensitive/tolerant roots, preserving the earlier
// fail-closed behavior; case-sensitive roots retain the spelling so distinct
// files such as Poster.jpg and poster.jpg do not share a journal bucket.
func DestKey(p string) string {
	return DestKeyForRoot(destinationProbeRoot(p), p)
}

// DestKeyForRoot is DestKey with an explicit destination root. The explicit
// form is useful to callers that already know the media-library root.
func DestKeyForRoot(root, p string) string {
	s := normalizeDestPath(p)
	if IsCaseSensitiveRoot(root) {
		return s
	}
	return strings.ToLower(s)
}

func normalizeDestPath(p string) string {
	// Apply separator policy before filepath.Clean: Windows legacy journals may
	// use either slash spelling, while POSIX filepath names may contain a
	// literal backslash that must survive cleaning. Case folding is applied by
	// DestKeyForRoot only after this platform-aware path canonicalization.
	s := strings.TrimSpace(p)
	if PathBackslashesAreSeparators {
		s = strings.ReplaceAll(s, "\\", "/")
	}
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(s)))
}

var probeRootStat = os.Stat

// destinationProbeRoot selects the destination's directory, or its nearest
// existing ancestor, so the probe creates a sibling rather than touching the
// destination itself.
func destinationProbeRoot(p string) string {
	root := filepath.Dir(filepath.FromSlash(normalizeDestPath(p)))
	for {
		if info, err := probeRootStat(root); err == nil && info.IsDir() {
			return cleanProbeRoot(root)
		}
		parent := filepath.Dir(root)
		if parent == root {
			return cleanProbeRoot(root)
		}
		root = parent
	}
}

func cleanProbeRoot(root string) string {
	root = strings.TrimSpace(root)
	if PathBackslashesAreSeparators {
		root = strings.ReplaceAll(root, "\\", "/")
	}
	if root == "" {
		root = "."
	}
	root = filepath.Clean(filepath.FromSlash(root))
	absolute, _ := filepath.Abs(root)
	return filepath.Clean(absolute)
}

type caseProbeFile interface {
	Close() error
}

type caseProbeOps struct {
	openFile func(string, int, os.FileMode) (caseProbeFile, error)
	stat     func(string) (os.FileInfo, error)
	readDir  func(string) ([]os.DirEntry, error)
	remove   func(string) error
}

var osCaseProbeOps = caseProbeOps{
	openFile: func(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
		return os.OpenFile(name, flag, perm)
	},
	stat:    os.Stat,
	readDir: os.ReadDir,
	remove:  os.Remove,
}

// defaultCaseSensitiveProbe creates one uniquely named file and stats its
// differently-cased spelling. Directory enumeration is the fallback when the
// alternate stat is indeterminate. Cleanup removes only the exact probe path
// created by O_EXCL; the alternate spelling may belong to the user and is
// never a cleanup target. Any probe or cleanup failure is returned for the
// caller's fail-closed path.
func defaultCaseSensitiveProbe(root string) (bool, error) {
	return probeCaseSensitive(osCaseProbeOps, root)
}

func probeCaseSensitive(ops caseProbeOps, root string) (bool, error) {
	token := strconv.FormatUint(caseProbeOrdinal.Add(1), 10)
	name := ".javinizer_case_probe_" + token
	alternate := ".JAVINIZER_CASE_PROBE_" + token
	path := filepath.Join(root, name)
	alternatePath := filepath.Join(root, alternate)
	file, err := ops.openFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false, err
	}
	cleanup := func() error {
		if err := ops.remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := file.Close(); err != nil {
		_ = cleanup()
		return false, err
	}

	caseSensitive := false
	if _, statErr := ops.stat(alternatePath); statErr == nil {
		caseSensitive = false
	} else if os.IsNotExist(statErr) {
		caseSensitive = true
	} else {
		entries, readErr := ops.readDir(root)
		if readErr != nil {
			_ = cleanup()
			return false, readErr
		}
		caseSensitive = true
		for _, entry := range entries {
			if strings.EqualFold(entry.Name(), name) {
				caseSensitive = false
				break
			}
		}
	}
	if err := cleanup(); err != nil {
		return false, err
	}
	return caseSensitive, nil
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
