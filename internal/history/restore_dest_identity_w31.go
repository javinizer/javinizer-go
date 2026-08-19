package history

// POSTER-WRITE-HARDENING wave-31 (codex local round 1, PR#215 finding L1) —
// revalidate the destination against the object a restore just PUBLISHED
// before any backup deletion or journal consumption runs.
//
// The copy legs (copyRestoreBytesPublish / its no-replace twin) land the
// restored bytes through fsutil.PublishStagedBound, whose post-publish
// reverify proves WHICH object the destination names at publish time. The
// removal/consumption legs used to trust that proof forever: a foreign
// writer swapping or deleting the destination inside the publish→remove
// window had its replacement blessed while the backup — the only remaining
// copy of the pre-replacement bytes — was unlinked AND the journal entry
// consumed, losing those bytes forever (and, for a swap, erasing the only
// ledger trace of the restore).
//
// The publish therefore hands the verified destination identity back
// (fsutil.PublishStagedBoundInfo — the staged inode as published: dev/inode
// when the filesystem exposes it, size + mtime always, captured from the
// destination's own post-publish stat), and the legs below recheck the
// destination no-follow against it. On any mismatch, absence, or
// indeterminacy the deletion and the consumption are BOTH refused: the
// backup is retained, the journal entry stays exactly as it was (armed —
// deliberately NOT marked restore-pending; that marker certifies the
// destination carries the restored bytes, unproven now), and the foreign or
// absent destination is left untouched.

import (
	"os"

	"github.com/spf13/afero"
)

// restoredDestIdentity is the object a restore leg published at its
// destination, captured at publish time through fsutil.PublishStagedBoundInfo
// (the post-publish-VERIFIED destination stat, never a window-poisonable
// re-capture). known=false means the publish ran on a leg with no provable
// identity (virtual/wrapper filesystems, or the ENOSYS deferred-times
// relookup failure): those legs keep the pre-wave-31 documented residual
// posture — the recheck is skipped rather than trusted or refused on
// nothing.
type restoredDestIdentity struct {
	known bool
	info  os.FileInfo
}

// restoredDestIdentityFrom wraps fsutil.PublishStagedBoundInfo's answer: a
// nil FileInfo has no provable identity.
func restoredDestIdentityFrom(info os.FileInfo) restoredDestIdentity {
	if info == nil {
		return restoredDestIdentity{}
	}
	return restoredDestIdentity{known: true, info: info}
}

// destStillNamesRestoredObject rechecks dest the no-follow way and requires
// it to still name id's object: dev/inode when both sides expose it (the
// os.SameFile comparison through restoreSourceIdentity), then size and mtime
// on every platform. Absence, an indeterminate stat, a symlink or
// non-regular occupant, and any fact mismatch all answer false. An unknown
// identity skips the check (see the type comment).
func destStillNamesRestoredObject(fs afero.Fs, dest string, id restoredDestIdentity) bool {
	if !id.known {
		return true
	}
	now, err := lstatRestoreSource(fs, dest)
	if err != nil || now == nil || now.Mode()&os.ModeSymlink != 0 || !now.Mode().IsRegular() {
		return false
	}
	if pubDev, pubIno, pubOK := restoreSourceIdentity(id.info); pubOK {
		if nowDev, nowIno, nowOK := restoreSourceIdentity(now); nowOK && (pubDev != nowDev || pubIno != nowIno) {
			return false
		}
	}
	return now.Size() == id.info.Size() && now.ModTime().Equal(id.info.ModTime())
}

// restoredDestStillOurs is the restore-window destination recheck behind a
// package seam (same discipline as restoreStagingOwnershipFn): production
// wiring is destStillNamesRestoredObject; tests replay a foreign
// swap/deletion landing inside the publish→recheck window — an instant no
// Filesystem double can reach on the real OsFs, where the wave-30 identity
// gate requires the native descriptor. The helper's real-OsFs detection is
// pinned by direct unit tests.
var restoredDestStillOurs = destStillNamesRestoredObject
