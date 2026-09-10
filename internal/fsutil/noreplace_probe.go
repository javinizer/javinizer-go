package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// These seams replay kernel and identity failures without requiring an
// incapable mount. Cleanup uses Lstat: a symlink to the retained inode is
// still a foreign directory entry, never permission to unlink that entry.
var (
	publishNoClobberProbe  = PublishNoReplace
	noClobberProbePlatform = runtime.GOOS
	noClobberProbeAbs      = filepath.Abs
	createNoClobberProbe   = CreateExclusiveStagingFile
	noClobberProbeOps      = caseProbeOps{
		openFile: osCaseProbeOps.openFile,
		stat:     os.Lstat,
		rename:   probeRenameNoReplace,
		remove:   os.Remove,
	}
	noClobberProbeDirID = noClobberDirIdentity
	noClobberProbeStat  = os.Stat
	noClobberCacheMu    sync.Mutex
	noClobberCache      = make(map[string]error)
	noClobberInflight   = make(map[string]*noClobberFlight)
)

// ErrNoClobberProbeUnstable marks a destination whose filesystem identity
// flipped within every sample window of a probe round: no verdict computed
// during the round can be trusted to describe the currently-mounted
// filesystem, so the probe reports indeterminate instead of acting on a
// possibly-foreign capability answer (codex P2, PR #255).
var ErrNoClobberProbeUnstable = errors.New("no-clobber probe: destination identity unstable across samples")

// noClobberFlight shares one probe round with concurrent callers. id binds
// err to the directory identity that produced it: a waiter whose wake-time
// sample disagrees must NOT consume the answer — the verdict belongs to a
// filesystem that may no longer be mounted (codex P2, PR #255).
type noClobberFlight struct {
	done chan struct{}
	err  error
	id   string
}

// ProbeNoClobberPublish refuses an incapable destination before a staging
// stream starts. Only conclusive outcomes live in the process cache; transient
// failures are shared with current waiters but the next caller re-probes.
// Keys pair the absolute cleaned directory path with the directory's hosting
// filesystem identity (dev:ino): an unmount/remount or symlink retarget at
// the same absolute path produces a different key, so a stale verdict can
// never reject copies a new filesystem supports or skip its preflight. The
// identity is sampled before AND after the capability run, and again when a
// cached or peer-shared verdict is returned; a verdict whose two samples
// disagree describes the wrong filesystem and is re-probed rather than
// returned or cached. Iteration is attempt-bounded (codex P2, PR #255).
// No caller lock is acquired and no cache mutex is held across filesystem
// IO. A positive verdict is advisory: every file still uses the real final
// publish.
func ProbeNoClobberPublish(fs afero.Fs, dstDir string) error {
	if _, ok := fs.(*afero.OsFs); !ok || noClobberProbePlatform == "windows" {
		return nil
	}
	key, err := noClobberProbeAbs(dstDir)
	if err != nil {
		return fmt.Errorf("resolve no-clobber probe directory: %w", err)
	}
	// Every verdict — cached, shared through a peer flight, or freshly
	// probed — is certified against the directory identity sampled when it
	// was produced. A mount/symlink flip between sampling and consumption
	// means the answer describes the OTHER filesystem and must restart the
	// round under the re-sampled identity. Iteration shares the physical
	// probe budget: sustained flapping (real remounts or alternating failed
	// identity reads) exits indeterminate instead of recursing into the
	// goroutine stack (codex P2, PR #255).
	for round := 0; round < noClobberProbeMaxAttempts; round++ {
		sampledID := noClobberProbeDirID(key)
		cacheKey := key
		if sampledID != "" {
			// Identity failures (already-vanished directory, non-Stat_t
			// source) degrade to path-only caching rather than blocking the
			// probe entirely.
			cacheKey += "|" + sampledID
		}

		noClobberCacheMu.Lock()
		if verdict, ok := noClobberCache[cacheKey]; ok {
			noClobberCacheMu.Unlock()
			// Validate that the hit still describes the live filesystem: a
			// remount/retarget between the sample above and this lookup would
			// serve the previous mount's answer to the new one.
			if noClobberProbeDirID(key) == sampledID {
				return verdict
			}
			continue
		}
		if flight, ok := noClobberInflight[cacheKey]; ok {
			noClobberCacheMu.Unlock()
			<-flight.done
			// The shared verdict must be certified against the identity that
			// PRODUCED it, not this caller's join sample: A->B->A pinball would
			// otherwise let an A-sampled waiter consume B's answer
			// (codex P2, PR #255).
			if flight.id == noClobberProbeDirID(key) {
				return flight.err
			}
			continue
		}
		flight := &noClobberFlight{done: make(chan struct{})}
		// The flight stays registered under the key sampled BEFORE any drift
		// retry — the probe rounds below may move cacheKey to another
		// identity, and cleanup must still drop the registration key.
		flightKey := cacheKey
		noClobberInflight[flightKey] = flight
		noClobberCacheMu.Unlock()

		// A freshly computed verdict is only trusted when the identity
		// sampled before the probe still holds afterwards; each mid-probe
		// drift re-keys and retries within the shared attempt budget.
		conclusive, verdict := false, error(nil)
		for attempt := 0; attempt < noClobberProbeMaxAttempts; attempt++ {
			c, v := runNoClobberProbe(fs, key)
			afterID := noClobberProbeDirID(key)
			if afterID == sampledID {
				conclusive, verdict = c, v
				break
			}
			sampledID = afterID
			cacheKey = key
			if afterID != "" {
				cacheKey += "|" + afterID
			}
			verdict = fmt.Errorf("no-clobber preflight in %s: %w", key, ErrNoClobberProbeUnstable)
		}
		noClobberCacheMu.Lock()
		if conclusive {
			// The stabilized post-drift identity owns the verdict key;
			// waiters resolve through the original registration key.
			noClobberCache[cacheKey] = verdict
		}
		delete(noClobberInflight, flightKey)
		flight.err = verdict
		// The stabilized sample that produced the verdict; wake-time
		// revalidation compares the live identity against this, so a round
		// that drifted A->B cannot serve B's answer to a caller on A again.
		flight.id = sampledID
		close(flight.done)
		noClobberCacheMu.Unlock()
		return verdict
	}
	return fmt.Errorf("no-clobber preflight in %s: %w", key, ErrNoClobberProbeUnstable)
}

// noClobberProbeMaxAttempts bounds probe-pair retries after a collision on
// the SYNTHETIC destination sibling: an interrupted prior probe or retained
// cleanup residue can hold ".published" — the retry swaps in a fresh ordinal
// pair instead of failing the first organize on an otherwise capable volume
// (codex P2, PR #255). The occupied sibling is never displaced or unlinked.
const noClobberProbeMaxAttempts = 3

func runNoClobberProbe(fs afero.Fs, dir string) (bool, error) {
	var lastErr error
	for attempt := 0; attempt < noClobberProbeMaxAttempts; attempt++ {
		conclusive, collision, selected, err := runNoClobberProbeAttempt(fs, dir)
		if !collision {
			return conclusive, err
		}
		// The occupied sibling pinned the ACTUALLY selected ordinal;
		// retrying from the merely-requested start would recreate and
		// recollide on the same blocked name every round (codex P2, PR #255).
		if selected != 0 {
			advanceNoReplaceOrdinalBeyond(selected)
		}
		lastErr = err
	}
	return false, fmt.Errorf("no-clobber preflight in %s: synthetic probe names still occupied after %d attempts (indeterminate): %w", dir, noClobberProbeMaxAttempts, lastErr)
}

func runNoClobberProbeAttempt(fs afero.Fs, dir string) (bool, bool, uint64, error) {
	src, handle, err := createNoClobberProbe(fs, filepath.Join(dir, ".nrprobe"), "", nextNoReplaceOrdinal(), 0o600)
	if err != nil {
		return false, false, 0, fmt.Errorf("create no-clobber probe in %s: %w", dir, err)
	}
	// The skip scan may have landed above the requested start; report that
	// ordinal so a sibling collision can fast-forward the shared nonce
	// beyond it instead of recreating and recolliding the same name
	// (codex P2, PR #255).
	var selected uint64
	if dot := strings.LastIndexByte(src, '.'); dot >= 0 {
		if parsed, perr := strconv.ParseUint(src[dot+1:], 16, 64); perr == nil {
			selected = parsed
		}
	}
	defer func() { _ = handle.Close() }()
	created, err := handle.Stat()
	if err != nil {
		return false, false, selected, fmt.Errorf("capture no-clobber probe identity %s (retained): %w", src, err)
	}
	dst := src + ".published"
	verdict := publishNoClobberProbe(fs, src, dst)
	conclusive := verdict == nil || (errors.Is(verdict, ErrPublishNoReplaceUnsupported) && linkUnsupportedClass(verdict))
	var cleanupErr error
	if verdict == nil {
		cleanupErr = boundProbeCleanup(noClobberProbeOps, dst, created)
	} else {
		// A failed publish gives no authority over the destination name. Never
		// vacate the create-path with a replacing rename, even for cleanup.
		cleanupErr = unlinkNoClobberProbe(noClobberProbeOps, src, handle, created)
	}
	if cleanupErr != nil {
		// Cleanup is diagnostic only: neither its errno nor its identity refusal
		// may mask, reclassify, or poison the capability verdict.
		logging.Warnf("no-clobber probe cleanup retained %s / %s: %v", src, dst, cleanupErr)
	}
	if verdict != nil {
		if errors.Is(verdict, ErrPublishCollision) {
			// Only the SYNTHETIC sibling was occupied — the verdict says nothing
			// about the volume. Report the pair-collision so the caller retries
			// with a fresh ordinal pair; the occupied sibling stays untouched.
			return false, true, selected, fmt.Errorf("no-clobber preflight in %s: %w", dir, verdict)
		}
		return conclusive, false, selected, fmt.Errorf("no-clobber preflight in %s: %w", dir, verdict)
	}
	return true, false, selected, nil
}

// unlinkNoClobberProbe is the primitive-less volume's NO-VACATE cleanup.
// The retained descriptor and a syscall-adjacent no-follow lookup must both
// equal the creation identity. Every failed proof or unlink is reported and
// never retried. POSIX cannot eliminate the final lookup-to-unlink window;
// zero-byte residue is preferable to gambling on an unproven pathname.
func unlinkNoClobberProbe(ops caseProbeOps, path string, handle caseProbeFile, created os.FileInfo) error {
	fdInfo, err := handle.Stat()
	if err != nil {
		return fmt.Errorf("fstat no-clobber probe %s: %w", path, err)
	}
	if created == nil || !probeSameFile(created, fdInfo) {
		return fmt.Errorf("no-clobber probe %s descriptor identity unproven: %w", path, ErrTakeAsideForeign)
	}
	pathInfo, err := ops.stat(path)
	if err != nil {
		return fmt.Errorf("lookup no-clobber probe %s: %w", path, err)
	}
	if !probeSameFile(fdInfo, pathInfo) {
		return fmt.Errorf("no-clobber probe %s pathname identity diverged: %w", path, ErrTakeAsideForeign)
	}
	if err := ops.remove(path); err != nil {
		return fmt.Errorf("unlink verified no-clobber probe %s (retained): %w", path, err)
	}
	return nil
}
