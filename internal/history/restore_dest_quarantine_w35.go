package history

// POSTER-WRITE-HARDENING wave-35 (codex local review round 5, PR#215):
// bind the restore-undo DESTINATION unlink to the verified restored object
// with the same quarantine discipline the backup removal got in waves 26+32
// (replacement_backup_quarantine.go).
//
// Wave-34 bound the three restore-undo legs (the sweeper's undoRestore after
// a failed journal read / failed marker persistence, the sweeper's
// consumption-failure + re-arm-succeeded compensation, and the reverter's
// marker-persistence-failure compensation) to the published restore identity
// with a restoredDestStillOurs gate — then unlinked by PATHNAME. A foreign
// writer substituting dest between that gate and the separate pathname
// Remove still had its plant deleted (foreign bytes destroyed). The legs
// below close that final check→unlink window by routing the destination
// through the backup quarantine construction, destination-flavored:
//
//  1. RE-SNAPSHOT dest no-follow and bind it to the published restore
//     identity when one exists (known=true): dev/inode where the filesystem
//     exposes them, then size + mtime. The wave-34 gate's verdict is
//     re-derived against the object answering NOW, so a substitution inside
//     the gate→here window is refused with the foreign occupant untouched.
//     On the known=false virtual/wrapper leg there is no provable identity
//     to bind (wave-31's documented residual): the pre-move snapshot itself
//     becomes the verified reference the move is proven against, which at
//     minimum refuses any snapshot→move divergence the pre-wave-35 pathname
//     unlink silently absorbed;
//  2. move the verified object onto an O_EXCL-reserved, hard-to-guess
//     sibling quarantine name (claimBackupQuarantineName, the shared
//     reservation discipline) with a NIL handle — the destination flow
//     holds no open no-follow descriptor, so there is no Windows
//     close-handle dance;
//  3. re-prove the quarantined object against the verified snapshot and
//     unlink the QUARANTINE name only (the wave-32 unlink-time re-bind
//     included — every leg of quarantineVerifiedBackup / removeVerified);
//  4. every wedge compensates the verified bytes back onto the destination
//     name NO-REPLACE (restoreQuarantinedBackup): renaming dest away even
//     briefly changes the visible tree, so a racer's occupant planted at
//     dest meanwhile is never clobbered — the typed collision keeps the
//     verified object recoverable at the quarantine name for manual
//     recovery, and the callers' postures (warn, entry left live) apply
//     unchanged since the unlink result still surfaces as their leg's
//     error.
//
// The loader/warning nouns inside the shared quarantine legs still say
// "backup"; the destination flow deliberately reuses them rather than
// forking identity-verified quarantine code per noun.
//
// Wave-36 (codex local review round 6, PR#215 finding F2): identity
// metadata alone is not enough — unlink+recreate can REUSE the inode of a
// same-size, same-mtime file, so the dev/inode + metadata binding below can
// bless a foreign substitute. When the identity carries the PUBLISHED
// BYTES' digest (known && hashed — every restore copy records the digest at
// publish time, and only identity-bearing legs use it), the gate
// additionally hashes the current destination content (no-follow open) and
// requires equality before the quarantine+unlink. An inode-reused
// substitute with different bytes is refused; a substitute holding the
// EXACT published bytes with the same mtime is content-indistinguishable
// from ours — deleting it is equivalent to deleting ours.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

// removeRestoredDestQuarantined is the one-shot destination undo unlink:
// gate the CURRENT dest occupant against the published restore identity,
// quarantine it, and unlink the quarantine name. A nil error means the
// verified object (and nothing else) is gone; every refusal/wedge leg
// removes nothing foreign and leaves the caller's proceed/compensate
// decisions exactly where the pre-wave-35 pathname unlink did.
func removeRestoredDestQuarantined(fs afero.Fs, dest, phase string, id restoredDestIdentity) error {
	hold, err := quarantineRestoredDestForUnlink(fs, dest, phase, id)
	if err != nil {
		return err
	}
	return hold.removeVerified()
}

// quarantineRestoredDestForUnlink binds dest's CURRENT occupant to the
// published restore identity and moves it aside under the shared
// quarantine construction, returning the wave-32 hold for the caller's
// unlink (or restore). The gate is the atomicity boundary the wave-34
// seam verdict could not carry across its separate pathname Remove:
// a destination that stopped naming the published object (foreign swap,
// foreign create, deletion, re-type) is refused byte-intact.
func quarantineRestoredDestForUnlink(fs afero.Fs, dest, phase string, id restoredDestIdentity) (*replacementBackupQuarantine, error) {
	absoluteDest, _ := filepath.Abs(dest)
	pre, lerr := lstatRestoreSource(fs, dest)
	switch {
	case lerr != nil:
		// Indeterminate (a missing destination included): unverifiable is
		// never ours to unlink — fail closed exactly like the wave-34 seam's
		// indeterminate verdict.
		return nil, fmt.Errorf("%s cannot bind restored destination %s for the undo unlink: %w — nothing removed", phase, absoluteDest, lerr)
	case pre == nil || pre.Mode()&os.ModeSymlink != 0 || !pre.Mode().IsRegular():
		return nil, fmt.Errorf("%s refused the undo unlink of restored destination %s: the name no longer addresses the published regular file — foreign bytes preserved, nothing removed", phase, absoluteDest)
	}
	if id.known {
		if pubDev, pubIno, pubOK := restoreSourceIdentity(id.info); pubOK {
			if preDev, preIno, preOK := restoreSourceIdentity(pre); preOK && (pubDev != preDev || pubIno != preIno) {
				return nil, fmt.Errorf("%s refused the undo unlink of restored destination %s: a substituted object answers the name since the publish (dev/inode mismatch) — foreign bytes preserved, nothing removed", phase, absoluteDest)
			}
		}
		if pre.Size() != id.info.Size() || !pre.ModTime().Equal(id.info.ModTime()) {
			return nil, fmt.Errorf("%s refused the undo unlink of restored destination %s: the occupant metadata no longer matches the published restore object — foreign bytes preserved, nothing removed", phase, absoluteDest)
		}
	}
	if id.known && id.hashed {
		// Wave-36 (finding F2): the identity gates above cannot distinguish an
		// unlink+recreate substitute that REUSED the published object's inode
		// while replaying its size and mtime — only the bytes can. Hash the
		// current occupant no-follow and require the published content: a
		// mismatch refuses byte-intact, while a hash-equal occupant with equal
		// metadata is content-indistinguishable from the published object
		// (deleting it is equivalent to deleting ours). An unreadable occupant
		// is indeterminate — fail closed, exactly like the pre-move snapshot.
		// The known=false virtual/wrapper leg keeps its documented residual:
		// no provable identity exists there, so the pre-move snapshot re-derived
		// below stays the verified reference.
		curSum, herr := hashRestoredDestContent(fs, dest)
		switch {
		case herr != nil:
			return nil, fmt.Errorf("%s cannot bind restored destination %s for the undo unlink: %w — nothing removed", phase, absoluteDest, herr)
		case curSum != id.sum:
			return nil, fmt.Errorf("%s refused the undo unlink of restored destination %s: the occupant content no longer matches the published restore bytes — foreign bytes preserved, nothing removed", phase, absoluteDest)
		}
	}
	// pre is the VERIFIED destination snapshot: quarantineVerifiedBackup's
	// post-move re-prove binds the moved object against it (dev/inode where
	// exposed, then size + mtime), its wedge legs compensate NO-REPLACE onto
	// dest, and the returned hold's removeVerified re-binds at unlink time.
	return quarantineVerifiedBackup(fs, dest, phase, nil, pre)
}
