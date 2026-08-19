//go:build linux

package downloader

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/spf13/afero"
	"golang.org/x/sys/unix"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// POSTER-WRITE-HARDENING wave-37 (codex P2, PR#215) — the POSIX-preferred
// reserved-backup handoff: ATOMIC where the kernel offers the exchange
// primitive.

// backupExchangeKernel is the syscall behind the atomic handoff, exposed as a
// test seam (same discipline as fsutil's renameNoReplaceKernel): a host
// kernel cannot be coerced into ENOSYS / EINVAL / EOPNOTSUPP or an arbitrary
// renameat2 failure on demand, so tests replay those kernel responses here.
var backupExchangeKernel = func(src, dst string) error {
	return unix.Renameat2(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_EXCHANGE)
}

// handoffToReservedBackup prefers the atomic exchange on OsFs and falls
// through to the identity-bound rename leg otherwise (virtual filesystems,
// kernels/filesystems that cannot express RENAME_EXCHANGE).
func handoffToReservedBackup(fsys afero.Fs, destPath, backupPath string, claim os.FileInfo) error {
	if exchanged, err := exchangeBackupHandoff(fsys, destPath, backupPath, claim); exchanged {
		return err
	}
	return handoffViaVerifiedRename(fsys, destPath, backupPath, claim)
}

// exchangeBackupHandoff performs the dest→backup handoff with
// renameat2(RENAME_EXCHANGE): the two dentries swap atomically, so backupPath
// receives the original destination bytes and dest receives the reservation
// placeholder with NO verify→rename window in which a foreign writer could
// plant an occupant to overwrite. The placeholder parked at dest is then
// removed ONLY after re-proving dest still names the claimed reservation
// object (releaseExchangedPlaceholder) — a foreign occupant that rode the
// swap is left byte-intact with a typed refusal.
//
// Returns (exchanged, err): exchanged=false means no exchange was attempted
// (non-OsFs) or the kernel/filesystem cannot express RENAME_EXCHANGE
// (ENOSYS / EINVAL / EOPNOTSUPP — older kernels, restricted filesystems);
// the caller then takes the identity-bound rename leg, the codex-accepted
// degrade shape. Any OTHER exchange error means the swap never happened
// (renameat2 is atomic): the reservation is released by the same bound
// discipline (it only unlinks a still-provably-ours placeholder) and the
// failure surfaces wrapped.
func exchangeBackupHandoff(fsys afero.Fs, destPath, backupPath string, claim os.FileInfo) (bool, error) {
	if _, ok := fsys.(*afero.OsFs); !ok {
		return false, nil
	}
	err := backupExchangeKernel(destPath, backupPath)
	switch {
	case err == nil:
		return true, releaseExchangedPlaceholder(fsys, destPath, claim)
	case errors.Is(err, syscall.ENOSYS), errors.Is(err, syscall.EINVAL), errors.Is(err, syscall.EOPNOTSUPP):
		return false, nil
	default:
		releaseClaimedReservation(fsys, backupPath, claim)
		return true, fmt.Errorf("atomic backup exchange %s -> %s: %w", destPath, backupPath, err)
	}
}

// releaseExchangedPlaceholder removes the reservation placeholder a
// successful exchange parked at destPath, leaving the destination absent for
// the staged publish. The unlink is bound to the claim's identity: dest must
// STILL name the claimed reservation object (dev/inode where the filesystem
// exposes them, plus size 0, mtime, and a regular non-symlink shape) at
// syscall adjacency. A placeholder that VANISHED on its own completed the
// cleanup by itself. Anything else — a foreign occupant that rode the swap,
// an indeterminate lookup, a failed unlink — is a refusal: the install fails
// closed, the destination is left byte-intact, and the set-aside backup
// (which holds the original bytes by then) is retained for the orphan sweep.
func releaseExchangedPlaceholder(fsys afero.Fs, destPath string, claim os.FileInfo) error {
	cur, err := lstatBackupCandidate(fsys, destPath)
	switch {
	case err != nil && os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("inspect exchanged placeholder %s before its removal: %w", destPath, err)
	case !destPlaceholderMatchesClaim(cur, claim):
		logging.Warnf("downloader: exchange-parked placeholder at %s no longer names the claimed reservation — a foreign occupant rode the swap; destination left byte-intact, set-aside backup retained", destPath)
		return fmt.Errorf("placeholder the exchange parked at %s no longer names the claimed reservation (foreign occupant preserved): %w", destPath, fsutil.ErrPublishCollision)
	}
	if rmErr := fsys.Remove(destPath); rmErr != nil {
		return fmt.Errorf("remove exchange-parked placeholder %s (verified ours): %w", destPath, rmErr)
	}
	return nil
}

// destPlaceholderMatchesClaim reports whether cur — the object currently
// named by dest after the exchange — is still THE claimed reservation
// placeholder: regular, non-symlink, size 0, same mtime on every platform,
// and same dev/inode where the filesystem exposes them. The shape mirrors
// overwriteBackupReservationStillOurs (wave-36) for the exchange leg's dest
// side.
func destPlaceholderMatchesClaim(cur, claim os.FileInfo) bool {
	if cur == nil || cur.Mode()&os.ModeSymlink != 0 || !cur.Mode().IsRegular() || cur.Size() != 0 || !cur.ModTime().Equal(claim.ModTime()) {
		return false
	}
	if claimDev, claimIno, claimOK := restoreSourceIdentity(claim); claimOK {
		if curDev, curIno, curOK := restoreSourceIdentity(cur); curOK && (claimDev != curDev || claimIno != curIno) {
			return false
		}
	}
	return true
}
