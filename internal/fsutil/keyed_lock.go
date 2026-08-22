package fsutil

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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
	"unicode"

	"github.com/spf13/afero"
	"golang.org/x/text/unicode/norm"
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

// probeFlight is the single-flight slot for one root's in-flight probe
// (wave-25, codex P3 PR#215): concurrent first-time derivations for one root
// share ONE filesystem probe — a slow probe serializes only its own root's
// first derivation, never other roots, and never the whole process. The
// runner publishes ONLY a definitive outcome to the cache; a probe ERROR is
// delivered to every sharer uncached so the next derivation retries (an
// error is undecidable, not a cached posture). done is closed under the
// cache mutex after result is set, so sharers observe a complete write.
type probeFlight struct {
	done   chan struct{}
	result bool
}

var (
	caseSensitivityCacheMu  sync.Mutex
	caseSensitivityCache    = make(map[string]bool)
	caseSensitivityInflight = make(map[string]*probeFlight)
	caseProbeOrdinal        atomic.Uint64
	// caseProbeRandReader is a seam for entropy failure tests. The production
	// source is cryptographically random, so probe names do not expose any
	// path or user data.
	caseProbeRandReader io.Reader = cryptorand.Reader
)

// ResetCaseSensitivityCache clears the process cache and any recorded
// in-flight slots. It is primarily a test seam; production callers should
// retain the one-probe-per-root lifetime. Waiters that already joined a
// flight before the reset still receive that flight's own result.
func ResetCaseSensitivityCache() {
	caseSensitivityCacheMu.Lock()
	caseSensitivityCache = make(map[string]bool)
	caseSensitivityInflight = make(map[string]*probeFlight)
	caseSensitivityCacheMu.Unlock()
}

// IsCaseSensitiveRoot reports the cached case behavior for root.
//
// One probe per root per process, zero mutex-held IO, single-flight (wave-25,
// codex P3 PR#215): concurrent first-time derivations for one root share the
// ONE in-flight probe through probeFlight instead of probing in parallel —
// and, crucially, a probe ERROR is never cached. Caching a transient probe
// failure (unwritable root, a raced cleanup, a momentary IO fault) as a
// permanent "sensitive" posture would split destination keys forever:
// spellings that address ONE file would land in different journal buckets, so
// cross-chain sequence reuse and conflict checks would miss them. The caller
// that observed the error returns the conservative case-preserving posture
// to its sharers WITHOUT publishing; the very next derivation re-probes.
// A nil probe seam remains the deliberate forced-insensitive test posture
// (a definitive outcome — cached), never an error.
//
// Case folding requires a POSITIVE insensitivity determination (codex P2):
//   - a probe ERROR leaves the case decision undecided, so THIS derivation
//     treats the root as case-PRESERVING — folding on a guess could alias
//     byte-distinct files (Poster.jpg vs poster.jpg) on what is actually a
//     case-sensitive volume, while preserved distinctions only ever cost an
//     extra (safe) bucket — and the NEXT derivation retries the probe instead
//     of inheriting the transient failure.
func IsCaseSensitiveRoot(root string) bool {
	root = cleanProbeRoot(root)
	caseSensitivityCacheMu.Lock()
	if result, ok := caseSensitivityCache[root]; ok {
		caseSensitivityCacheMu.Unlock()
		return result
	}
	if flight, ok := caseSensitivityInflight[root]; ok {
		// Single-flight: another caller is probing this root right now — share
		// its outcome instead of issuing a second filesystem probe.
		caseSensitivityCacheMu.Unlock()
		<-flight.done
		return flight.result
	}
	flight := &probeFlight{done: make(chan struct{})}
	caseSensitivityInflight[root] = flight
	caseSensitivityCacheMu.Unlock()

	probe := CaseSensitiveProbe
	result := false
	definitive := false
	if probe == nil {
		// Explicit test posture, not a probe failure (see doc comment).
		result = false
		definitive = true
	} else if probed, err := probe(root); err != nil {
		// Undecided root: preserve case distinctions WITHOUT caching; only a
		// positive insensitivity determination may fold, and only a definitive
		// outcome may publish.
		result = true
	} else {
		result = probed
		definitive = true
	}

	caseSensitivityCacheMu.Lock()
	defer caseSensitivityCacheMu.Unlock()
	if definitive {
		if _, ok := caseSensitivityCache[root]; !ok {
			caseSensitivityCache[root] = result
		}
	}
	// Publish the flight result to every sharer after the cache decision so a
	// definitive outcome is visible before they continue; drop only OUR slot
	// (a Reset-affected slot never collides with a later flight's).
	if caseSensitivityInflight[root] == flight {
		delete(caseSensitivityInflight, root)
	}
	flight.result = result
	close(flight.done)
	return result
}

// DestKey canonicalizes a destination path for CROSS-FORM comparisons while
// respecting the destination root's filesystem semantics. Backslash
// separators are normalized only when PathBackslashesAreSeparators is true.
// Case is folded only after a positive insensitivity determination;
// undecidable (probe-failed) and case-sensitive roots retain the spelling so
// distinct files such as Poster.jpg and poster.jpg do not share a journal
// bucket. Unicode NORMALIZATION is folded (NFC) only after a positive
// normalization-insensitivity determination for the root: on APFS/HFS+ the
// filesystem itself aliases the NFC/NFD spellings of one name, so the two
// spellings of one journaled file must share one key; on
// normalization-sensitive roots (ext4 and friends) both spellings may
// coexist as separate files and must keep separate buckets.
// Whitespace is NEVER folded under either case posture: it is part of the
// filename, and trimming it would alias byte-distinct files into one bucket.
func DestKey(p string) string {
	return DestKeyForRoot(destinationProbeRoot(p), p)
}

// destKeyInsensitive computes the insensitive destination-key form with a
// PER-RUNE simple uppercase mapping (strings.Map + unicode.ToUpper). Two
// codex PR#215 findings bound this choice from both sides:
//
//   - wave-20 (codex P2 — KEEP): plain strings.ToLower leaves GREEK SMALL
//     LETTER FINAL SIGMA (ς) un-folded against σ although both uppercase to
//     Σ and are case-equivalent on insensitive filesystems, so two spellings
//     of ONE file (`…/στ.jpg` journaled, `…/ΣΤ.jpg` queried) produced
//     different journal keys and stayed invisible to the exact matcher,
//     corrupting sequence reuse and conflict checks. unicode.ToUpper maps
//     ς→Σ and σ→Σ identically, preserving that rescue (foldKeyedLock has
//     unified the sigma forms this way all along).
//   - wave-21 (codex P2 — the change): wave-20's cases.Fold runs the FULL
//     Unicode case-fold table, which also expands one-to-many folds:
//     ß→"ss" (Straße ≡ Strasse) and the ﬃ ligature → "ffi". Those
//     multi-char folds are NOT filename equivalences on NTFS/APFS — their
//     filesystem upcase tables map per code unit and never expand — so the
//     full fold aliased byte-distinct files that can coexist in one
//     directory, merging separate replacement chains under one journal key.
//     restoreReplacementJournal would then restore EVERY entry to the
//     first-recorded spelling, overwriting one file with another's backups.
//     Simple per-rune uppercase never expands, keeping Straße/Strasse and
//     ﬃ/FFI distinct while unifying exactly the true case variants.
func destKeyInsensitive(s string) string {
	return strings.Map(unicode.ToUpper, s)
}

// DestKeyForRoot is DestKey with an explicit destination root. The explicit
// form is useful to callers that already know the media-library root. The
// case-SENSITIVE leg stays byte-identical (normalizeDestPath only) unless
// the root is also normalization-insensitive, in which case the spelling is
// NFC-canonicalized; the insensitive leg maps through destKeyInsensitive and
// THEN NFC — per-rune uppercase first, canonical composition last — so the
// finished key is always in NFC form when normalization folds apply (e.g. a
// name combining only post-uppercase, like the long-s + dot-above sequence,
// lands on the same key as its precomposed spelling).
func DestKeyForRoot(root, p string) string {
	posture := destKeyPosture{normInsensitive: IsNormalizationInsensitiveRoot(root)}
	posture.caseSensitive = IsCaseSensitiveRoot(root)
	return posture.key(p)
}

// destKeyPosture carries one probe root's fold decisions resolved ONCE
// (wave-45, codex P2, PR#215 finding F2) — see DestKeyResolver.
type destKeyPosture struct {
	caseSensitive   bool
	normInsensitive bool
}

// key computes DestKeyForRoot's key form against pre-resolved postures, with
// zero probe calls. The fold ORDER is byte-identical to DestKeyForRoot: case
// posture picks the branch (preserved spelling vs destKeyInsensitive's
// per-rune uppercase), then the normalization posture applies NFC last.
func (p destKeyPosture) key(s string) string {
	s = normalizeDestPath(s)
	if p.caseSensitive {
		if p.normInsensitive {
			return norm.NFC.String(s)
		}
		return s
	}
	s = destKeyInsensitive(s)
	if p.normInsensitive {
		s = norm.NFC.String(s)
	}
	return s
}

// DestKeyResolver pre-resolves destination fold postures ONCE PER ROOT and
// derives keys with zero further probe calls (wave-45, codex P2, PR#215
// finding F2). DestKey re-resolves per CALL, and a transient probe error is
// deliberately never cached (wave-25) — so two successive entries of ONE
// journal, probed under different error/success mixtures, could derive
// under opposite postures: one file's case-variant cousins sorted into
// separate destination-key buckets, and the buckets' downstream map
// iteration restored the stacked chains in a nondeterministic interleave
// (leaving intermediate bytes last). A grouping pass therefore resolves each
// present root exactly once through a resolver and derives every key of the
// pass from that frozen posture set. The process-wide probe caches still
// carry definitive outcomes across invocations (their wave-25 contract is
// unchanged); only the call-local horizon is frozen.
type DestKeyResolver struct {
	postures map[string]destKeyPosture
}

// NewDestKeyResolver returns an empty resolver whose per-root postures freeze
// on first derivation.
func NewDestKeyResolver() *DestKeyResolver {
	return &DestKeyResolver{postures: make(map[string]destKeyPosture)}
}

// Key canonicalizes p exactly like DestKey, resolving p's probe root's
// postures on first use and reusing that frozen resolution afterwards.
func (r *DestKeyResolver) Key(p string) string {
	root := destinationProbeRoot(p)
	posture, ok := r.postures[root]
	if !ok {
		posture = destKeyPosture{normInsensitive: IsNormalizationInsensitiveRoot(root)}
		posture.caseSensitive = IsCaseSensitiveRoot(root)
		r.postures[root] = posture
	}
	return posture.key(p)
}

// NormalizationProbe is the process-wide Unicode-normalization probe seam,
// mirroring CaseSensitiveProbe. A probe error is undecidable, so
// IsNormalizationInsensitiveRoot keeps normalization distinctions for that
// root (conservative no-fold posture) rather than aliasing byte-distinct
// NFD/NFC files on a guess; a nil probe seam is a deliberate test posture
// for forced-insensitive folding and keeps the folded result.
var NormalizationProbe caseSensitivityProbe = defaultNormalizationInsensitiveProbe

var (
	normalizationCacheMu  sync.Mutex
	normalizationCache    = make(map[string]bool)
	normalizationInflight = make(map[string]*probeFlight)
)

// ResetNormalizationCache clears the per-root normalization-probe cache and
// any recorded in-flight slots. It is primarily a test seam; production
// callers retain the one-probe-per-root lifetime.
func ResetNormalizationCache() {
	normalizationCacheMu.Lock()
	normalizationCache = make(map[string]bool)
	normalizationInflight = make(map[string]*probeFlight)
	normalizationCacheMu.Unlock()
}

// IsNormalizationInsensitiveRoot reports the cached normalization behavior
// for root: TRUE only when the probe positively established that creating a
// name in NFD form is later addressable by its NFC spelling (APFS/HFS+
// filename comparison). The caching pattern mirrors IsCaseSensitiveRoot —
// one probe per root per process, zero mutex-held IO, single-flight sharing
// of the in-flight probe, and NO cached error outcomes (wave-25, codex P3
// PR#215): a transient probe failure returns the conservative no-fold
// posture to its sharers and leaves the cache EMPTY so the next derivation
// retries, while the nil probe seam stays the deliberate forced-insensitive
// test posture (definitive — cached).
func IsNormalizationInsensitiveRoot(root string) bool {
	root = cleanProbeRoot(root)
	normalizationCacheMu.Lock()
	if result, ok := normalizationCache[root]; ok {
		normalizationCacheMu.Unlock()
		return result
	}
	if flight, ok := normalizationInflight[root]; ok {
		// Single-flight: share the in-flight probe's outcome.
		normalizationCacheMu.Unlock()
		<-flight.done
		return flight.result
	}
	flight := &probeFlight{done: make(chan struct{})}
	normalizationInflight[root] = flight
	normalizationCacheMu.Unlock()

	probe := NormalizationProbe
	result := false
	definitive := false
	if probe == nil {
		// Explicit test posture, not a probe failure (see seam doc).
		result = true
		definitive = true
	} else if probed, err := probe(root); err != nil {
		// Undecided root: preserve normalization distinctions WITHOUT caching;
		// only a positive insensitivity determination may fold.
		result = false
	} else {
		result = probed
		definitive = true
	}

	normalizationCacheMu.Lock()
	defer normalizationCacheMu.Unlock()
	if definitive {
		if _, ok := normalizationCache[root]; !ok {
			normalizationCache[root] = result
		}
	}
	if normalizationInflight[root] == flight {
		delete(normalizationInflight, root)
	}
	flight.result = result
	close(flight.done)
	return result
}

// normProbeName returns the process-unique NFD probe name: the tail carries
// a canonically DECOMPOSED ä (a + COMBINING DIAERESIS) so the created name
// and its NFC spelling differ byte-wise while addressing one file on a
// normalization-insensitive volume.
func normProbeName() string {
	return ".javinizer_norm_probe_" + caseProbeToken() + "_a\u0308"
}

// defaultNormalizationInsensitiveProbe writes one process-unique NFD-form
// name and stats its NFC spelling: stat success proves the filesystem
// aliases the two normalization forms of one name (insensitive); a clean
// ENOENT proves the forms stay byte-distinct (sensitive). The O_EXCL
// collision retry, fail-closed error legs, and create-path-only cleanup all
// mirror the case probe (probeCaseSensitive).
func defaultNormalizationInsensitiveProbe(root string) (bool, error) {
	return probeNormalizationInsensitive(osCaseProbeOps, root)
}

// probeSameFile is the identity comparator for case/normalization probes
// (os.SameFile in production). Test doubles whose fake FileInfos carry no
// kernel identity override this seam (same discipline as restoreChown).
var probeSameFile = os.SameFile

func probeNormalizationInsensitive(ops caseProbeOps, root string) (bool, error) {
	for attempt := 0; ; attempt++ {
		name := normProbeName()
		path := filepath.Join(root, name)
		file, err := ops.openFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrExist) && attempt+1 < caseProbeMaxAttempts {
				continue
			}
			return false, err
		}
		// The NFC spelling may belong to the user and is never a cleanup
		// target; only the exact NFD path this probe created is removed, and
		// only after re-proving it still names THE created object (wave-39
		// bound cleanup — see boundProbeCleanup).
		alternatePath := filepath.Join(root, norm.NFC.String(name))
		// Codex P2 (wave-38, PR#215 finding F5): capture the created
		// object's identity from the OPEN handle BEFORE closing — a watcher
		// renaming the probe away after close could otherwise substitute a
		// successor for a create-path re-stat to borrow as proof. A failed
		// identity capture fails closed exactly like a failed close.
		created, sErr := file.Stat()
		closeErr := file.Close()
		cleanup := func() error { return boundProbeCleanup(ops, path, created) }
		if closeErr != nil {
			_ = cleanup()
			return false, closeErr
		}
		if sErr != nil {
			_ = cleanup()
			return false, sErr
		}

		insensitive := false
		if altInfo, statErr := ops.stat(alternatePath); statErr == nil {
			// Codex P2 (w31): the alternate spelling can belong to a racer's
			// file created between our O_EXCL create and this stat — accept
			// "insensitive" only when it addresses THE SAME object we just
			// created (bound to the handle's pre-close snapshot); a distinct
			// inode keeps spellings byte-distinct.
			if probeSameFile(created, altInfo) {
				// The NFC spelling addresses the NFD-created probe: the FS
				// normalizes names on comparison.
				insensitive = true
			}
		} else if !os.IsNotExist(statErr) {
			// Indeterminate lookup: undecidable posture, fail closed.
			_ = cleanup()
			return false, statErr
		}
		// A clean ENOENT keeps insensitive=false: byte-distinct forms.
		if err := cleanup(); err != nil {
			return false, err
		}
		return insensitive, nil
	}
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

// caseProbeFile is the O_EXCL-created probe handle. Stat is the
// IDENTITY channel (wave-38, codex P2, PR#215 finding F5): the created
// object's identity must be captured from the OPEN handle — never by
// re-statting the create-path afterwards, where a watcher could have
// renamed the probe away and parked a successor the probe would then
// borrow as evidence.
type caseProbeFile interface {
	Stat() (os.FileInfo, error)
	Close() error
}

type caseProbeOps struct {
	openFile func(string, int, os.FileMode) (caseProbeFile, error)
	stat     func(string) (os.FileInfo, error)
	// rename is the cleanup take-aside's NO-REPLACE move (wave-39, codex P2,
	// PR#215 — bound probe cleanup): an occupied target REFUSES rather than
	// dislodging whatever sits there. Production wires PublishNoReplace
	// (Windows MoveFileExW without the replace flag; POSIX renameat2
	// NOREPLACE / the verified hard-link fallback).
	rename func(oldPath, newPath string) error
	remove func(string) error
}

var osCaseProbeOps = caseProbeOps{
	openFile: func(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
		return os.OpenFile(name, flag, perm)
	},
	stat:   os.Stat,
	rename: probeRenameNoReplace,
	remove: os.Remove,
}

// probeRenameNoReplace is the production cleanup take-aside move: an atomic
// no-replace rename of the verified probe onto its scratch sibling, refusing
// (collision class) rather than displacing anything that claimed the scratch
// name mid-window.
func probeRenameNoReplace(oldPath, newPath string) error {
	return PublishNoReplace(afero.NewOsFs(), oldPath, newPath)
}

// probeCleanupScratchSuffix marks the scratch sibling both probes park their
// verified created object on before the ONLY unlink runs (bound cleanup).
const probeCleanupScratchSuffix = ".cleanup"

// boundProbeCleanup is BOTH filesystem-semantics probes' bound cleanup
// (wave-39, codex P2, PR#215): pre-wave-39 the probes unlinked the mutable
// CREATE-PATH directly, so a watcher renaming the probe away and parking a
// substitute at that name got its own object deleted. The cleanup now runs
// the take-aside sequence (mirroring bound_take.go):
//
//  1. the create-path's current occupant is re-proven against the
//     handle-captured identity — a path that vanished completed the cleanup
//     by itself, and a SUBSTITUTE is left byte-intact while the cleanup
//     refuses closed (typed ErrTakeAsideForeign);
//  2. the verified object moves onto a sibling scratch name NO-REPLACE — an
//     occupied scratch refuses instead of displacing anything;
//  3. the scratch object is re-proven against the same identity (a
//     substitution under the take-aside is left entirely alone);
//  4. only THE SCRATCH name is ever unlinked; a scratch that vanished on
//     its own completed the cleanup.
//
// A nil created (the handle's identity capture itself failed) is the one
// leg with no identity channel to bind against: the just-created O_EXCL
// path is removed best-effort by pathname, exactly as before.
func boundProbeCleanup(ops caseProbeOps, path string, created os.FileInfo) error {
	if created == nil {
		// Codex P2 (wave-58): with no identity capture there is NOTHING to
		// authenticate the name against — another writer may already own it;
		// retain the pathname (the O_EXCL claim expires at process exit
		// naturally, and no unlink of a possibly-foreign object ever runs).
		return nil
	}
	cur, statErr := ops.stat(path)
	switch {
	case os.IsNotExist(statErr):
		// The probe vanished on its own (or was renamed away): nothing of
		// ours sits at the create-path — the cleanup completed itself.
		return nil
	case statErr != nil:
		return statErr
	case !probeSameFile(created, cur):
		// A substitute occupies the create-path: foreign bytes are never
		// moved or unlinked — the cleanup fails closed instead.
		return fmt.Errorf("case/normalization probe path %s no longer names the created probe (foreign substitution) — foreign bytes preserved: %w", path, ErrTakeAsideForeign)
	}
	scratch := path + probeCleanupScratchSuffix
	if err := ops.rename(path, scratch); err != nil {
		if os.IsNotExist(err) {
			// The verified probe vanished under the take-aside move —
			// indistinguishable from a completed cleanup, nothing to unlink.
			return nil
		}
		return fmt.Errorf("move probe %s onto cleanup scratch %s: %w", path, scratch, err)
	}
	moved, statErr := ops.stat(scratch)
	switch {
	case os.IsNotExist(statErr):
		// The verified object vanished from the scratch on its own.
		return nil
	case statErr != nil:
		return statErr
	case !probeSameFile(created, moved):
		// A substitution landed under the take-aside: the scratch occupant
		// is left entirely alone and the cleanup refuses closed.
		return fmt.Errorf("probe cleanup scratch %s no longer names the created probe (foreign substitution under the take-aside) — foreign bytes preserved: %w", scratch, ErrTakeAsideForeign)
	}
	// Codex P2 (w34): bind the final unlink to the verified object at
	// syscall adjacency — the scratch is re-opened O_RDONLY and BOTH its
	// descriptor identity and a fresh pathname lookup must match the created
	// probe's identity before the pathname unlink runs. A substitute at the
	// scratch name is preserved (typed refusal); a vanished scratch completes
	// silently. The residual lookup→unlink boundary is the documented POSIX
	// pathname-unlink limit at a crypto-random 0-byte name.
	proceed, ferr := bindScratchForUnlink(ops, scratch, created)
	if ferr != nil {
		return ferr
	}
	if !proceed {
		return nil // vanished during binding — nothing left to unlink
	}
	if err := ops.remove(scratch); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove probe cleanup scratch %s (verified): %w", scratch, err)
	}
	return nil
}

// bindScratchForUnlink proves the pathname's current object equals the
// created probe's identity twice over — by descriptor fstat and by lookup —
// immediately before the caller unlinks the name.
func bindScratchForUnlink(ops caseProbeOps, scratch string, created os.FileInfo) (bool, error) {
	fh, err := ops.openFile(scratch, os.O_RDONLY, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // vanished on its own — nothing left to unlink
		}
		return false, fmt.Errorf("open probe cleanup scratch %s for binding: %w", scratch, err)
	}
	defer func() { _ = fh.Close() }()
	fdStat, serr := fh.Stat()
	if serr != nil {
		return false, fmt.Errorf("fstat probe cleanup scratch %s: %w", scratch, serr)
	}
	if !probeSameFile(created, fdStat) {
		return false, fmt.Errorf("probe cleanup scratch %s fails descriptor identity — foreign bytes preserved: %w", scratch, ErrTakeAsideForeign)
	}
	linkStat, lerr := ops.stat(scratch)
	if lerr != nil {
		if os.IsNotExist(lerr) {
			return false, nil
		}
		return false, fmt.Errorf("stat probe cleanup scratch %s during binding: %w", scratch, lerr)
	}
	if !probeSameFile(fdStat, linkStat) {
		return false, fmt.Errorf("probe cleanup scratch %s pathname identity diverged while bound — foreign bytes preserved: %w", scratch, ErrTakeAsideForeign)
	}
	return true, nil
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
// retain the fail-closed path. Cleanup removes only the exact probe path
// created by O_EXCL — bound to the handle-captured identity through the
// wave-39 take-aside (boundProbeCleanup); the alternate spelling may belong
// to the user and is never a cleanup target. Any probe or cleanup failure is
// returned for the caller's fail-closed path.
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
		// Only the exact probe path created by O_EXCL is ever removed, bound
		// to the handle-captured identity (wave-39 — boundProbeCleanup); the
		// alternate spelling may belong to the user and is never a target.
		alternatePath := filepath.Join(root, strings.ToUpper(name))
		// Codex P2 (wave-38, PR#215 finding F5): the created object's
		// identity is captured from the OPEN handle BEFORE closing — the
		// case probe repeats the normalization probe's defect when it
		// re-stats the mutable create-path afterwards (a watcher's successor
		// object would be borrowed as case evidence). A failed identity
		// capture fails closed exactly like a failed close.
		created, sErr := file.Stat()
		closeErr := file.Close()
		cleanup := func() error { return boundProbeCleanup(ops, path, created) }
		if closeErr != nil {
			_ = cleanup()
			return false, closeErr
		}
		if sErr != nil {
			_ = cleanup()
			return false, sErr
		}

		caseSensitive := false
		if altInfo, statErr := ops.stat(alternatePath); statErr == nil {
			// Codex P2 (w31): a racer's uppercased file must not masquerade
			// as our probe — only an identity match proves case-insensitivity.
			if !probeSameFile(created, altInfo) {
				caseSensitive = true
			}
		} else if os.IsNotExist(statErr) {
			caseSensitive = true
		} else {
			// Codex P2 (wave-39, PR#215): an indeterminate alternate-stat is
			// UNDECIDABLE — the pre-wave-39 readDir fallback necessarily
			// matched the probe's OWN name via EqualFold, permanently caching
			// the root as case-INSENSITIVE even on a sensitive volume and
			// aliasing DestKeys for byte-distinct files. Fail closed WITHOUT
			// enumerating; the wave-25 contract keeps error outcomes uncached
			// so the next derivation re-probes.
			_ = cleanup()
			return false, statErr
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
