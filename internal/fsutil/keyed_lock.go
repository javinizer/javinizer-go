package fsutil

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/text/cases"
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
	// Whitespace is equally a filename character here: spellings that differ
	// only in surrounding whitespace fold to DISTINCT keys, never one shared
	// lock.
	return strings.ToUpper(strings.ToLower(normalizeDestPath(s)))
}

// PathBackslashesAreSeparators is the path-separator seam used by destination
// keys and keyed locks. It defaults to the runtime platform behavior, while
// tests may select either platform's spelling rules on one host.
var PathBackslashesAreSeparators = runtime.GOOS == "windows"

// caseSensitivityProbe is intentionally injectable so filesystem semantics can
// be forced by tests without changing the journal format.
type caseSensitivityProbe func(root string) (bool, error)

// CaseSensitiveProbe is the process-wide probe seam. A probe error is
// undecidable, so IsCaseSensitiveRoot keeps case distinctions for that root
// (conservative distinct-key posture) rather than folding on a guess.
var CaseSensitiveProbe caseSensitivityProbe = defaultCaseSensitiveProbe

const caseProbeMaxAttempts = 8

var (
	caseSensitivityCacheMu sync.Mutex
	caseSensitivityCache   = make(map[string]bool)
	caseProbeOrdinal       atomic.Uint64
	// caseProbeRandReader is a seam for entropy failure tests. The production
	// source is cryptographically random, so probe names do not expose any
	// path or user data.
	caseProbeRandReader io.Reader = cryptorand.Reader
)

// ResetCaseSensitivityCache clears the process cache. It is primarily a test
// seam; production callers should retain the one-probe-per-root lifetime.
func ResetCaseSensitivityCache() {
	caseSensitivityCacheMu.Lock()
	caseSensitivityCache = make(map[string]bool)
	caseSensitivityCacheMu.Unlock()
}

// IsCaseSensitiveRoot reports the cached case behavior for root.
//
// One probe, zero mutex-held IO (codex P2): the filesystem probe runs WITHOUT
// the cache mutex — holding it across the create/stat/remove probe serialized
// every destination-key derivation process-wide behind one root's slow IO.
// Concurrent first-time queries for one root may therefore probe in parallel
// (still exactly one probe per call); the revalidation under the mutex
// publishes a single entry, and a caller whose speculative result loses the
// race adopts the published posture so all callers converge.
//
// Case folding requires a POSITIVE insensitivity determination (codex P2):
//   - a probe ERROR (unwritable root, transient IO) leaves the case decision
//     undecided, so the root is treated as case-PRESERVING — folding on a
//     guess could alias byte-distinct files (Poster.jpg vs poster.jpg) on what
//     is actually a case-sensitive volume, while preserved distinctions only
//     ever cost an extra (safe) bucket;
//   - a nil probe seam is not a failure but a deliberate test posture for
//     forced-insensitive matching, and keeps the folded result.
func IsCaseSensitiveRoot(root string) bool {
	root = cleanProbeRoot(root)
	caseSensitivityCacheMu.Lock()
	if result, ok := caseSensitivityCache[root]; ok {
		caseSensitivityCacheMu.Unlock()
		return result
	}
	caseSensitivityCacheMu.Unlock()

	probe := CaseSensitiveProbe
	result := false
	if probe == nil {
		// Explicit test posture, not a probe failure (see doc comment).
		result = false
	} else if probed, err := probe(root); err != nil {
		// Undecided root: preserve case distinctions; only a positive
		// insensitivity determination may fold.
		result = true
	} else {
		result = probed
	}

	caseSensitivityCacheMu.Lock()
	defer caseSensitivityCacheMu.Unlock()
	if cached, ok := caseSensitivityCache[root]; ok {
		// A parallel first probe published meanwhile: adopt the authoritative
		// entry rather than racing in a divergent posture.
		return cached
	}
	caseSensitivityCache[root] = result
	return result
}

// DestKey canonicalizes a destination path for CROSS-FORM comparisons while
// respecting the destination root's filesystem semantics. Backslash
// separators are normalized only when PathBackslashesAreSeparators is true.
// Case is folded only after a positive insensitivity determination;
// undecidable (probe-failed) and case-sensitive roots retain the spelling so
// distinct files such as Poster.jpg and poster.jpg do not share a journal
// bucket.
// Whitespace is NEVER folded under either case posture: it is part of the
// filename, and trimming it would alias byte-distinct files into one bucket.
func DestKey(p string) string {
	return DestKeyForRoot(destinationProbeRoot(p), p)
}

// destKeyFolder computes the insensitive destination-key form with FULL
// Unicode case folding (wave-20, codex P2, PR#215). Plain strings.ToLower is
// a per-rune simple case mapping: it leaves GREEK SMALL LETTER FINAL SIGMA
// (ς) un-folded against σ although both uppercase to Σ and are
// case-equivalent on insensitive filesystems, so two spellings of ONE file
// (`…/στ.jpg` journaled, `…/ΣΤ.jpg` queried) produced different journal keys
// — equivalent spellings stayed invisible to the exact matcher, corrupting
// sequence reuse and conflict checks. cases.Fold follows the Unicode default
// case-fold table (ς→σ alongside σ, ß→ss alongside ẞ, …), is byte-identical
// to ToLower for ASCII, and the returned Caser is stateless and safe for
// concurrent use.
var destKeyFolder = cases.Fold()

// DestKeyForRoot is DestKey with an explicit destination root. The explicit
// form is useful to callers that already know the media-library root. The
// case-SENSITIVE leg stays byte-identical (normalizeDestPath only); the
// insensitive leg full-folds through destKeyFolder.
func DestKeyForRoot(root, p string) string {
	s := normalizeDestPath(p)
	if IsCaseSensitiveRoot(root) {
		return s
	}
	return destKeyFolder.String(s)
}

func normalizeDestPath(p string) string {
	// Key derivation must stay byte-distinct for byte-distinct names: leading
	// and trailing whitespace are legal filename characters (POSIX plainly;
	// Win32 trims by API convention while NTFS keeps them), so a trimmed key
	// would alias different physical files ('poster.jpg' vs 'poster.jpg ')
	// into one lock and one journal bucket. Whitespace is therefore never
	// folded here — the ONLY collapses are the platform separator policy and
	// filepath.Clean.
	// Apply separator policy before filepath.Clean: Windows legacy journals may
	// use either slash spelling, while POSIX filepath names may contain a
	// literal backslash that must survive cleaning. Case folding is applied by
	// DestKeyForRoot only after this platform-aware path canonicalization.
	s := p
	if PathBackslashesAreSeparators {
		s = strings.ReplaceAll(s, "\\", "/")
	}
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(s)))
}

var probeRootStat = os.Stat

// destinationProbeRoot selects the destination's directory, or its nearest
// existing ancestor, so the probe creates a sibling rather than touching the
// destination itself. Selection is intentionally stat-only: an existing
// read-only directory remains the probe root, and the later create failure
// retains the fail-closed insensitive result.
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
	// The probe root is part of the key trail: it selects the cache entry and
	// the directory actually probed. It must not be whitespace-trimmed either
	// — distinct roots whose names differ only in surrounding whitespace stay
	// byte-distinct, and trimming would silently probe a DIFFERENT directory's
	// case posture. Only the empty string falls back to ".".
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

func caseProbeToken() string {
	ordinal := caseProbeOrdinal.Add(1)
	if caseProbeRandReader != nil {
		entropy := make([]byte, 8)
		if _, err := io.ReadFull(caseProbeRandReader, entropy); err == nil {
			return strings.Join([]string{
				strconv.Itoa(os.Getpid()),
				hex.EncodeToString(entropy),
				strconv.FormatUint(ordinal, 10),
			}, "_")
		}
	}
	return strings.Join([]string{
		strconv.Itoa(os.Getpid()),
		strconv.FormatInt(time.Now().UnixNano(), 10),
		strconv.FormatUint(ordinal, 10),
	}, "_")
}

func caseProbeName() string {
	return ".javinizer_case_probe_" + caseProbeToken()
}

// defaultCaseSensitiveProbe creates one process-unique file and stats its
// differently-cased spelling. The name carries the PID, fresh crypto entropy,
// and an ordinal (with a timestamp fallback if entropy is unavailable). An
// O_EXCL collision gets a bounded retry with a fresh name; other open failures
// retain the fail-closed path. Directory enumeration is the fallback when the
// alternate stat is indeterminate. Cleanup removes only the exact probe path
// created by O_EXCL; the alternate spelling may belong to the user and is
// never a cleanup target. Any probe or cleanup failure is returned for the
// caller's fail-closed path.
func defaultCaseSensitiveProbe(root string) (bool, error) {
	return probeCaseSensitive(osCaseProbeOps, root)
}

func probeCaseSensitive(ops caseProbeOps, root string) (bool, error) {
	for attempt := 0; ; attempt++ {
		name := caseProbeName()
		path := filepath.Join(root, name)
		file, err := ops.openFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrExist) && attempt+1 < caseProbeMaxAttempts {
				continue
			}
			return false, err
		}
		alternatePath := filepath.Join(root, strings.ToUpper(name))
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
