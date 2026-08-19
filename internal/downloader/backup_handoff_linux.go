//go:build linux

package downloader

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

// POSTER-WRITE-HARDENING wave-37/wave-38 (codex P2, PR#215) — the
// POSIX-preferred reserved-backup handoff: ATOMIC where the kernel offers
// the exchange primitive, with the exchange-parked placeholder's removal
// rebound through the wave-38 take-aside (finding F3).

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
// removed ONLY through the take-aside binding (releaseExchangedPlaceholder):
// dest→scratch onto a fresh reserved quarantine name, identity re-proof at
// the scratch name against the claim, unlink of the scratch re-bound at
// unlink time — the pre-wave-38 verify-then-unlink-by-name gap (finding F3)
// can no longer delete a foreign file swapped in after the check.
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
