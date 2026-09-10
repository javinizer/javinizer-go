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
	noClobberCacheMu  sync.Mutex
	noClobberCache    = make(map[string]error)
	noClobberInflight = make(map[string]*noClobberFlight)
)

type noClobberFlight struct {
	done chan struct{}
	err  error
}

// ProbeNoClobberPublish refuses an incapable destination before a staging
// stream starts. Only conclusive outcomes live in the process cache; transient
// failures are shared with current waiters but the next caller re-probes.
// Keys are absolute cleaned directory paths, deliberately not mount identities.
// No caller lock is acquired and no cache mutex is held across filesystem IO.
// A positive verdict is advisory: every file still uses the real final publish.
func ProbeNoClobberPublish(fs afero.Fs, dstDir string) error {
	if _, ok := fs.(*afero.OsFs); !ok || noClobberProbePlatform == "windows" {
		return nil
	}
	key, err := noClobberProbeAbs(dstDir)
	if err != nil {
		return fmt.Errorf("resolve no-clobber probe directory: %w", err)
	}
	noClobberCacheMu.Lock()
	if verdict, ok := noClobberCache[key]; ok {
		noClobberCacheMu.Unlock()
		return verdict
	}
	if flight, ok := noClobberInflight[key]; ok {
		noClobberCacheMu.Unlock()
		<-flight.done
		return flight.err
	}
	flight := &noClobberFlight{done: make(chan struct{})}
	noClobberInflight[key] = flight
	noClobberCacheMu.Unlock()

	conclusive, verdict := runNoClobberProbe(fs, key)
	noClobberCacheMu.Lock()
	if conclusive {
		noClobberCache[key] = verdict
	}
	delete(noClobberInflight, key)
	flight.err = verdict
	close(flight.done)
	noClobberCacheMu.Unlock()
	return verdict
}

func runNoClobberProbe(fs afero.Fs, dir string) (bool, error) {
	src, handle, err := createNoClobberProbe(fs, filepath.Join(dir, ".nrprobe"), "", nextNoReplaceOrdinal(), 0o600)
	if err != nil {
		return false, fmt.Errorf("create no-clobber probe in %s: %w", dir, err)
	}
	defer func() { _ = handle.Close() }()
	created, err := handle.Stat()
	if err != nil {
		return false, fmt.Errorf("capture no-clobber probe identity %s (retained): %w", src, err)
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
		return conclusive, fmt.Errorf("no-clobber preflight in %s: %w", dir, verdict)
	}
	return true, nil
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
