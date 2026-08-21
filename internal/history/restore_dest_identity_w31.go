package history

// POSTER-WRITE-HARDENING wave-31 (codex local round 1, PR#215 finding L1) —
// revalidate the destination against the object a restore just PUBLISHED
// before any backup deletion or journal consumption runs.
//
// Wave-32 (codex local review round 2, PR#215 finding R5) — caller audit:
// the ENOSYS deferred-times legs of fsutil.PublishStagedBound no longer
// degrade a failed post-publish relookup to a nil-identity success (that
// flowed HERE as known=false and the recheck SKIPPED it — "safe"). r12
// removed the deferred fallback ENTIRELY: the ENOSYS leg skips the times
// and surfaces the wave-60 completed classification (nil identity, backup
// consumed paths per the completed discipline), never a degraded success
// — so an indeterminate identity can never reach this file from a real
// filesystem. The ONLY remaining source of an unknown identity is a
// virtual/wrapper filesystem leg, whose documented residual below stands.
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
	"crypto/sha256"
	"io"
	"os"

	"github.com/spf13/afero"
)

// restoredDestIdentity is the object a restore leg published at its
// destination, captured at publish time through fsutil.PublishStagedBoundInfo
// (the post-publish-VERIFIED destination stat, never a window-poisonable
// re-capture). known=false means the publish ran on a leg with no provable
// identity — VIRTUAL/WRAPPER FILESYSTEMS ONLY since wave-32 (finding R5):
// the (pre-r12) ENOSYS deferred-times legs reported lookup failures as
// typed refusals, and r12 removed those legs entirely (times skipped,
// completed classification), so an indeterminate identity never flows
// into this type on a real filesystem. The virtual legs keep the pre-wave-31
// documented residual posture for an identity-less recheck: skipped rather
// than trusted or refused on nothing. Wave-36 (codex local review round 6,
// PR#215 finding F2): every restore copy records the published bytes'
// SHA-256, and identity-bearing legs (the real filesystems, where inode
// reuse is meaningful) gain the content qualifier from it.
type restoredDestIdentity struct {
	known  bool
	info   os.FileInfo
	sum    [32]byte
	hashed bool
}

// restoredDestIdentityFrom wraps fsutil.PublishStagedBoundInfo's answer: a
// nil FileInfo has no provable identity.
func restoredDestIdentityFrom(info os.FileInfo) restoredDestIdentity {
	if info == nil {
		return restoredDestIdentity{}
	}
	return restoredDestIdentity{known: true, info: info}
}

// restoredDestIdentityFromContent is restoredDestIdentityFrom plus the
// published-bytes content digest (wave-36, codex local review round 6,
// PR#215 finding F2): dev/inode can be REUSED by an unlink+recreate
// substitute carrying the same size and a replayed mtime, so identity
// metadata alone can bless a foreign object. The copy legs stream-hash the
// restored payload as it lands (cheap — poster-sized files) and hand the
// digest here, so the recheck and the wave-35 undo-quarantine can require
// the current destination bytes to hash equal before any removal is armed —
// on identity-bearing legs (pending legs never re-published and carry no
// digest; the virtual-leg residual of known=false stands as documented).
func restoredDestIdentityFromContent(info os.FileInfo, sum [32]byte) restoredDestIdentity {
	id := restoredDestIdentityFrom(info)
	id.hashed = true
	id.sum = sum
	return id
}

// hashRestoredDestContent streams the CURRENT occupant at dest through
// SHA-256 under the same no-follow discipline the restore source open uses
// (restoreOpenReplacementSource): a symlink swapped in mid-window is refused
// by the platform open, never hashed. Any open/read failure makes the
// content unverifiable — callers fail closed.
func hashRestoredDestContent(fs afero.Fs, dest string) ([32]byte, error) {
	src, err := restoreOpenReplacementSource(fs, dest)
	if err != nil {
		return [32]byte{}, err
	}
	defer func() { _ = src.Close() }()
	h := sha256.New()
	buf := make([]byte, 256*1024)
	if _, err := io.CopyBuffer(h, src, buf); err != nil {
		return [32]byte{}, err
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// destStillNamesRestoredObject rechecks dest the no-follow way and requires
// it to still name id's object: dev/inode when both sides expose it (the
// os.SameFile comparison through restoreSourceIdentity), then size and mtime
// on every platform. Absence, an indeterminate stat, a symlink or
// non-regular occupant, and any fact mismatch all answer false. Wave-36
// (finding F2): when the identity carries the published-bytes digest, the
// occupant's CONTENT must hash equal too — sibling identity gates (the
// restoredDestStillOurs recheck funnel) share this qualifier wherever the
// restored bytes are known. A fully unknown identity skips the check (see
// the type comment).
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
	if now.Size() != id.info.Size() || !now.ModTime().Equal(id.info.ModTime()) {
		return false
	}
	// Wave-36 (finding F2): the content qualifier runs wherever BOTH the
	// provable identity and the published bytes are known (the real-filesystem
	// legs — the virtual/wrapper residual of the skip above stands, as
	// documented at the wave-32 audit). An inode-reused substitute whose
	// bytes differ is rejected; a substitute holding the EXACT published
	// bytes with the same mtime is content-indistinguishable from the
	// published object — answering true there is equivalent to answering for
	// ours. An unreadable occupant fails closed.
	if id.hashed {
		cur, herr := hashRestoredDestContent(fs, dest)
		if herr != nil || cur != id.sum {
			return false
		}
	}
	return true
}

// restoredDestStillOurs is the restore-window destination recheck behind a
// package seam (same discipline as restoreStagingOwnershipFn): production
// wiring is destStillNamesRestoredObject; tests replay a foreign
// swap/deletion landing inside the publish→recheck window — an instant no
// Filesystem double can reach on the real OsFs, where the wave-30 identity
// gate requires the native descriptor. The helper's real-OsFs detection is
// pinned by direct unit tests. Wave-32 (finding R1): the armed sweep/
// reverter legs call the seam a SECOND time after their backup was
// quarantined (the check-to-delete window), and the pending legs re-gate
// destination presence the same way — scripted per-call answers replay both
// instants deterministically.
var restoredDestStillOurs = destStillNamesRestoredObject
