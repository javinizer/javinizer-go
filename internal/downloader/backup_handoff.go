package downloader

import (
	"fmt"
	"os"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// POSTER-WRITE-HARDENING wave-37 (codex P2, PR#215) — the reserved-backup
// handoff must be ATOMIC where the platform offers the primitive and
// IDENTITY-BOUND everywhere else.
//
// The wave-36 verify-then-rename left two windows open around the
// dest→backup move (install_overwrite.go:772):
//
//   - OVERWRITE window: after overwriteBackupReservationStillOurs proved the
//     reservation still ours, the replacing rename ran as a SEPARATE pathname
//     op; a foreign writer replacing the reservation in between had its bytes
//     silently overwritten by the rename (POSIX) or the replacing
//     ReplaceFile (Windows).
//   - CLEANUP-UNLINK window: when the move failed, the caller's
//     `_ = d.fs.Remove(backupPath)` unlinked whatever occupied the backup
//     NAME by then — including the foreign occupant planted in that window.
//
// The closed variants, by platform:
//
//   - Linux/OsFs (backup_handoff_linux.go): renameat2(RENAME_EXCHANGE) swaps
//     the dest and reservation dentries atomically — there is NO verify→rename
//     interposition at all. The placeholder the exchange parks at dest is
//     then removed ONLY after re-proving dest still names the claimed
//     reservation object; anything else is left byte-intact with a typed
//     refusal.
//   - Everywhere else (non-Linux POSIX incl. Darwin, Windows, every virtual
//     filesystem) — the codex-accepted (i)+(ii) shape below: (i) the
//     reservation is re-derived at syscall adjacency and the rename runs ONLY
//     when the occupant still equals the claimed identity (Windows:
//     ReplaceFile replaces by design, so the pre-move verification and the
//     cleanup binding carry the whole guarantee there); (ii) a failed handoff
//     unlinks the reservation ONLY while it is still provably the claimed
//     0-byte placeholder — a foreign occupant or an indeterminate answer is
//     left byte-intact with a warn (releaseClaimedReservation).
//
// handoffViaVerifiedRename is the identity-bound handoff (i)+(ii). Windows
// legs route through moveIntoReservedBackup's PathBackslashesAreSeparators
// posture (fsutil.ReplaceFile) exactly as wave-12 established.
func handoffViaVerifiedRename(fsys afero.Fs, destPath, backupPath string, claim os.FileInfo) error {
	// (i): syscall-adjacency re-derivation — the rename is attempted ONLY
	// while the backup name still addresses THE claimed placeholder. Any
	// divergence is the typed collision class and the foreign occupant is
	// never renamed over.
	if verErr := overwriteBackupReservationStillOurs(fsys, backupPath, claim); verErr != nil {
		return verErr
	}
	if err := moveIntoReservedBackup(fsys, destPath, backupPath); err != nil {
		// (ii): bound cleanup — the reservation is released ONLY while it is
		// still provably ours; a foreign occupant survives byte-intact.
		releaseClaimedReservation(fsys, backupPath, claim)
		return fmt.Errorf("reserved backup handoff: %w", err)
	}
	return nil
}

// releaseClaimedReservation unlinks a still-claimed backup reservation after
// a failed handoff — bound to the claim's identity (wave-37 (ii)): only the
// verified 0-byte placeholder this operation reserved may be removed. A
// reservation that VANISHED on its own completed the cleanup by itself (no
// foreign bytes were ever at risk). Any other answer — foreign occupant,
// identity mismatch, indeterminate lookup — is a REFUSAL: the name is left
// byte-intact and the install surfaces the handoff failure with the occupant
// preserved.
func releaseClaimedReservation(fsys afero.Fs, backupPath string, claim os.FileInfo) {
	err := overwriteBackupReservationStillOurs(fsys, backupPath, claim)
	switch {
	case err == nil:
		// Proven ours at syscall adjacency — release the placeholder so a
		// retry never has to climb past (or worse, journal) it.
		_ = fsys.Remove(backupPath)
	case os.IsNotExist(err):
		// The reservation vanished on its own — nothing foreign to protect,
		// nothing left to remove.
	default:
		logging.Warnf("downloader: failed set-aside cleanup of %s refused — the reservation no longer provably names our claimed placeholder (%v); the occupant is left byte-intact", backupPath, err)
	}
}
