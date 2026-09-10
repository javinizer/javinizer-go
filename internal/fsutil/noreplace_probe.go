package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

type noClobberFlight struct {
	done chan struct{}
	err  error
}

// ProbeNoClobberPublish refuses an incapable destination before a staging
// stream starts. Only conclusive outcomes live in the process cache; transient
// failures are shared with current waiters but the next caller re-probes.
// Keys pair the absolute cleaned directory path with the directory's hosting
// filesystem identity (dev:ino): an unmount/remount or symlink retarget at
// the same absolute path produces a different key, so a stale verdict can
// never reject copies a new filesystem supports or skip its preflight. The
// identity is sampled before AND after the capability run; a verdict whose
// two samples disagree describes the wrong filesystem and is re-probed
// rather than returned or cached (codex P2, PR #255). No caller lock is acquired and no cache mutex is held
// across filesystem IO. A positive verdict is advisory: every file still
// uses the real final publish.
func ProbeNoClobberPublish(fs afero.Fs, dstDir string) error {
	if _, ok := fs.(*afero.OsFs); !ok || noClobberProbePlatform == "windows" {
		return nil
	}
	key, err := noClobberProbeAbs(dstDir)
	if err != nil {
		return fmt.Errorf("resolve no-clobber probe directory: %w", err)
	}
	// The cache key additionally pins the hosting-filesystem identity, while
	// the probe itself still runs against the real directory path.
	sampledID := noClobberProbeDirID(key)
	cacheKey := key
	if sampledID != "" {
		// Identity failures (already-vanished directory, non-Stat_t source)
		// degrade to pre-keying path-only caching rather than blocking the
		// probe entirely.
		cacheKey += "|" + sampledID
	}
	noClobberCacheMu.Lock()
	if verdict, ok := noClobberCache[cacheKey]; ok {
		noClobberCacheMu.Unlock()
		// A cache hit keys the verdict to the identity sampled above, but the
		// sampling-to-return window admits a remount/retarget: serving the
		// result would hand the PREVIOUS filesystem's answer to the new one
		// (codex P2, PR #255). Drift restarts the lookup against a freshly
		// sampled identity.
		if noClobberProbeDirID(key) == sampledID {
			return verdict
		}
		return ProbeNoClobberPublish(fs, key)
	}
	if flight, ok := noClobberInflight[cacheKey]; ok {
		noClobberCacheMu.Unlock()
		<-flight.done
		// Same return-time revalidation for verdicts completed by a peer: a
		// mid-wait filesystem change invalidates the key this caller joined
		// under (codex P2, PR #255).
		if noClobberProbeDirID(key) == sampledID {
			return flight.err
		}
		return ProbeNoClobberPublish(fs, key)
	}
	flight := &noClobberFlight{done: make(chan struct{})}
	// The flight stays registered under the key sampled BEFORE any drift
	// retry — re-validation may move cacheKey to another identity mid-round,
	// and cleanup must still drop the registration waiter keys reference.
	flightKey := cacheKey
	noClobberInflight[flightKey] = flight
	noClobberCacheMu.Unlock()

	// A verdict is only trusted when the identity sampled before the probe
	// still holds afterwards: a mid-probe mount/symlink swap means the
	// result describes the OTHER filesystem, so it is neither returned nor
	// cached. The round re-samples and retries, and perpetual flapping
	// exhausts the attempt budget as indeterminate (codex P2, PR #255).
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
		// The stabilized post-drift identity owns the verdict key; waiters
		// still resolve through the original registration key below.
		noClobberCache[cacheKey] = verdict
	}
	delete(noClobberInflight, flightKey)
	flight.err = verdict
	close(flight.done)
	noClobberCacheMu.Unlock()
	return verdict
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
		conclusive, collision, err := runNoClobberProbeAttempt(fs, dir)
		if !collision {
			return conclusive, err
		}
		lastErr = err
	}
	return false, fmt.Errorf("no-clobber preflight in %s: synthetic probe names still occupied after %d attempts (indeterminate): %w", dir, noClobberProbeMaxAttempts, lastErr)
}

func runNoClobberProbeAttempt(fs afero.Fs, dir string) (bool, bool, error) {
	src, handle, err := createNoClobberProbe(fs, filepath.Join(dir, ".nrprobe"), "", nextNoReplaceOrdinal(), 0o600)
	if err != nil {
		return false, false, fmt.Errorf("create no-clobber probe in %s: %w", dir, err)
	}
	defer func() { _ = handle.Close() }()
	created, err := handle.Stat()
	if err != nil {
		return false, false, fmt.Errorf("capture no-clobber probe identity %s (retained): %w", src, err)
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
			return false, true, fmt.Errorf("no-clobber preflight in %s: %w", dir, verdict)
		}
		return conclusive, false, fmt.Errorf("no-clobber preflight in %s: %w", dir, verdict)
	}
	return true, false, nil
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
