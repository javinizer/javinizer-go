package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/afero"
)

const exclusiveStagingAttempts = 64

// stagedHandleChmod is the OsFs leg's THROUGH-THE-HANDLE chmod behind
// restoreStagingMode (wave-29), exposed as a test seam (same discipline as
// probeRootStat / restoreFchown): tests record the fd+mode the handle-based
// re-assert requested without depending on host umask behavior.
var stagedHandleChmod = func(f *os.File, mode os.FileMode) error { return f.Chmod(mode) }

// osStagingHandle returns the *os.File behind fh ONLY when fs is the real
// OsFs. wave-29 (codex P1, PR#215) moved every staging metadata operation
// onto the open handle so a name planted mid-flow can never redirect it, but
// a WRAPPER filesystem (the test doubles interposing Chmod/Chtimes/OpenFile)
// must still see its path operations: it cannot intercept handle syscalls,
// so handle-based hardening keys on the filesystem's identity, never the
// handle's dynamic type.
func osStagingHandle(fs afero.Fs, fh afero.File) (*os.File, bool) {
	if _, ok := fs.(*afero.OsFs); !ok {
		return nil, false
	}
	of, ok := fh.(*os.File)
	return of, ok
}

// stagedVirtualModePath normalizes a staged name for the virtual-filesystem
// fallback legs. Afero's MemMapFs stores entries under filepath.Clean'd keys
// (backslash-spelled on Windows) while its Chmod performs a RAW map lookup,
// so a slash-spelled staged name misses the just-created entry
// ("chmod ...: file does not exist" on the Windows runner). filepath.Clean
// is exactly the store-time key derivation, so it hits on every host and
// spelling; on POSIX both are byte-identical for the clean spelling grammar
// the staging names follow.
func stagedVirtualModePath(staged string) string { return filepath.Clean(staged) }

// CreateExclusiveStagingFile creates a destination-adjacent staging file and
// returns its chosen path and open handle. Callers provide the next process
// ordinal so the existing hexadecimal `.rstr.<n>`/`.dlrstr.<n>` naming grammar
// remains unchanged; these transient names are not `.dlbak` ownership markers
// consumed by replacement_sweep_p3.go.
//
// wave-29 (codex P1, PR#215): the mode re-assert runs THROUGH THE OPEN
// HANDLE on the real OsFs (see restoreStagingMode). An O_EXCL creation does
// NOT stop a directory writer from renaming the staging name away and
// planting a symlink before a path-based Chmod — that call would have hit an
// arbitrary target; the handle-based call always lands on the inode this
// process created. Virtual filesystems keep a name-based fallback against
// the stored spelling (stagedVirtualModePath). Callers pair this with
// RestoreStagingOwnership / VerifyStagedIdentity / CloseStaged so no
// unverified path-based metadata operation ever touches the staged name
// again.
// wave-30 (codex P1, PR#215): the handle opens O_RDWR, not O_WRONLY —
// PublishStagedBound's publish-with-reverify loop re-stages bytes FROM THE
// OPEN HANDLE (seek 0 + copy into a fresh O_EXCL name) when a directory
// writer swapped the staged name mid-publish, which a write-only descriptor
// cannot serve. The wider mode grants nothing to the staged NAME: O_EXCL
// still pins the inode, and the fd never escapes the staging flow.
func CreateExclusiveStagingFile(fs afero.Fs, dest, suffix string, start uint64, mode os.FileMode) (string, afero.File, error) {
	for attempt := uint64(0); attempt < exclusiveStagingAttempts; attempt++ {
		ordinal := start + attempt
		staged := dest + suffix + "." + strconv.FormatUint(ordinal, 16)
		file, err := fs.OpenFile(staged, os.O_CREATE|os.O_EXCL|os.O_RDWR, mode)
		if err == nil {
			// The kernel narrows `mode` by the process umask at O_CREATE time
			// (e.g. 0666 becomes 0644 under umask 0077) and nothing else
			// re-applies it, so a restore staging for permissive media would
			// silently narrow its permission bits. Re-assert the exact
			// requested perms THROUGH THE OPEN HANDLE while the name is
			// exclusively ours; see replacements_posix.go /
			// replacements_windows.go for the strict-vs-best-effort split.
			if cerr := restoreStagingMode(fs, staged, file, mode.Perm()); cerr != nil {
				_ = file.Close()
				_ = fs.Remove(staged)
				return "", nil, fmt.Errorf("apply exclusive staging mode for %s: %w", staged, cerr)
			}
			return staged, file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, fmt.Errorf("exclusive staging names exhausted for %s%s after %d attempts", dest, suffix, exclusiveStagingAttempts)
}

// StagingTimesError distinguishes CloseStaged's times-application legs from
// its handle-close failure so callers keep their distinct pre-wave-29 error
// texts ("stage restore times" vs "stage restore close" and friends).
type StagingTimesError struct {
	Staged string
	Err    error
}

func (e *StagingTimesError) Error() string {
	return fmt.Sprintf("apply staged times for %s: %v", e.Staged, e.Err)
}

func (e *StagingTimesError) Unwrap() error { return e.Err }

// CloseStaged takes an exclusive staging handle through its exact close-time
// tail on every filesystem flavor (wave-29, codex P1, PR#215):
//
//  1. With applyTimes, the staged atime/mtime land THROUGH THE OPEN HANDLE
//     on the real OsFs — utimensat(fd, AT_EMPTY_PATH) with a /proc/self/fd
//     fallback on Linux, futimes on *BSD/Darwin, SetFileTime on Windows — so
//     a staged name swapped out mid-flow can never redirect the metadata onto
//     a foreign target. Platforms without an fd-scoped timestamp wrapper in
//     x/sys answer ENOSYS (staging_times_unixother.go) and defer to leg 3.
//  2. The handle is closed; the path-based publish that follows requires it
//     (Windows refuses a MoveFileEx rename of an open file).
//  3. Virtual filesystems — afero mem-style handles re-stamp ModTime at every
//     Write AND at Close, overwriting any pre-close times — apply the times
//     by name AFTER the close, against the stored (filepath.Clean'd)
//     spelling. They have no symlink model, so the name-based leg carries no
//     retargeting risk there.
//
// A times-leg failure closes the handle before returning (the inode stays
// staged for the caller's remove) and wraps as *StagingTimesError; a close
// failure returns the raw close error.
func CloseStaged(fs afero.Fs, staged string, fh afero.File, atime, mtime time.Time, applyTimes bool) error {
	timesPending := applyTimes
	if applyTimes {
		if of, ok := osStagingHandle(fs, fh); ok {
			err := stagedHandleChtimes(of.Fd(), atime, mtime)
			switch {
			case err == nil:
				timesPending = false
			case errors.Is(err, syscall.ENOSYS):
				// defer to the post-close name-based leg below
			default:
				_ = fh.Close()
				return &StagingTimesError{Staged: staged, Err: err}
			}
		}
	}
	if err := fh.Close(); err != nil {
		return err
	}
	if timesPending {
		if err := fs.Chtimes(stagedVirtualModePath(staged), atime, mtime); err != nil {
			return &StagingTimesError{Staged: staged, Err: err}
		}
	}
	return nil
}
