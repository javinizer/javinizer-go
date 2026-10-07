package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/config"
)

// LinkFunc creates a hard link from oldname to newname — os.Link's exact
// shape. The verified link composite takes the link verb as a seam so callers
// keep their own link route (the organizer's linker abstraction carries the
// OS/test twins) and tests replay the swap-at-link-instant window
// deterministically.
type LinkFunc func(oldname, newname string) error

// LinkFileNoReplaceVerified installs the hard link src→dst bound to a
// caller-admitted source identity (codex P1, PRRT_kwDORn9KaM6nEnUw) — the
// hard-link twin of MoveFileNoReplaceVerified / CopyFileNoReplaceVerified.
// Those composites pin the consumed object through a rename take-aside or an
// open descriptor; a hard link admits NEITHER binding: link(2) resolves the
// source BY NAME at the kernel, so a rename-swap landing between the caller's
// last validation and the link instant re-points the name at a replacement
// object, and an unverified install publishes a link to THAT — the
// destination then shares an inode with bytes the admission never proved and
// the "hard link IS the admitted object" ownership certificate (the deferred
// publication's mode-keyed pin) would attest a foreign object.
//
// The install is therefore bound at the two points the kernel allows:
//
//  1. PRE-LINK: the source is opened through openVerifiedSource (delete-shared
//     on Windows) and the handle's own Stat is re-proven against the admission
//     proof before the destination is touched — a swap that already landed
//     refuses typed (ErrTakeAsideForeign) with nothing installed.
//  2. POST-LINK: link(2) is atomically no-clobber (EEXIST on an occupied
//     destination), and dst then ALIASES whatever object src named at the link
//     instant — so the installed entry itself is re-proven against the same
//     admission proof (no-follow lookup). A swap that won the open→link window
//     fails that proof. The cleanup binds to the link operation's OWN identity
//     (codex P1, PRRT_kwDORn9KaM6npnwi): the admitted pre-link object is the
//     only node this composite provably installed, so an entry still aliasing
//     it is unlinked through UnlinkVerified bound to THAT captured identity —
//     never dst's CURRENT one. An entry diverging from it is UNPROVEN: another
//     writer may have renamed the fresh install aside and replanted dst inside
//     the link→lstat window, and authenticating the unlink against the entry's
//     own current identity would delete that foreign successor. Such an entry
//     is therefore RETAINED byte-intact (unproven outcomes retain — the
//     round-46 precedent): the typed refusal (ErrTakeAsideForeign) joins
//     ErrPublishCompleted — the doubt class, since this operation's own bytes
//     may still stand at another name — AND ErrPublishSuccessorUnproven (codex
//     P1, PRRT_kwDORn9KaM6nsX9a), the affirmative divergence marker telling the
//     caller's observe/rollback machinery that the destination's current
//     occupant is explicitly NOT provably this operation's installed output:
//     the successor is retained byte-intact, never observed/adopted as the
//     installed record (which would arm UnlinkVerified against the successor's
//     own identity) — the same adopt-not posture a wedged compensation avoids
//     by retaining the PROVEN install. The refused object stays put at the
//     source name throughout.
//
// Lookup classes after the link: NotExist proves nothing stands at the
// destination (plain refusal — the doubt-as-published class would lie); any
// other indeterminate answer keeps ErrPublishCompleted, since a bound cleanup
// has no identity to unlink and the publish may stand. Classification,
// occupancy, and adoption legs (lexical self / same-inode no-op, typed
// ErrPublishCollision) mirror the verified move/copy twins through
// classifyNoreplaceDestination. The link call's own failure passes through
// unwrapped so callers keep their EXDEV / permission classification. A nil
// proof preserves the legacy by-name behavior unchanged.
func LinkFileNoReplaceVerified(fs afero.Fs, src, dst string, link LinkFunc, proof VerifiedSourceProof) error {
	_, err := LinkFileNoReplaceVerifiedInstall(fs, src, dst, link, proof)
	return err
}

// LinkFileNoReplaceVerifiedInstall is LinkFileNoReplaceVerified with the
// INSTALLED entry's proven identity handed back (codex P1,
// PRRT_kwDORn9KaM6p3Dq1): the composite re-proves the entry it installed
// against the admitted pre-link object, so that proven stat IS the identity a
// caller must bind its record to — a later name lookup could authenticate a
// successor another writer planted after the install. A nil proof keeps the
// legacy by-name behavior and yields no identity.
func LinkFileNoReplaceVerifiedInstall(fs afero.Fs, src, dst string, link LinkFunc, proof VerifiedSourceProof) (os.FileInfo, error) {
	if proof == nil {
		return nil, link(src, dst)
	}
	done, err := classifyNoreplaceDestination(fs, src, dst)
	if done || err != nil {
		return nil, err
	}
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return nil, fmt.Errorf("verified link: create destination directory: %w", err)
	}
	srcFile, err := openVerifiedSource(fs, src)
	if err != nil {
		return nil, fmt.Errorf("verified link: open source %s: %w", src, err)
	}
	defer func() { _ = srcFile.Close() }()
	srcInfo, statErr := srcFile.Stat()
	if statErr != nil {
		return nil, fmt.Errorf("verified link: inspect the open source handle %s: %w", src, statErr)
	}
	if perr := proof(src, srcInfo); perr != nil {
		return nil, fmt.Errorf("verified link: the open source handle failed its admission proof (%w): %w", ErrTakeAsideForeign, perr)
	}
	if lerr := link(src, dst); lerr != nil {
		return nil, lerr
	}
	dstInfo, dstErr := asideLstat(fs, dst)
	switch {
	case errors.Is(dstErr, os.ErrNotExist):
		return nil, fmt.Errorf("verified link: the installed entry %s vanished before its identity proof: %w", dst, dstErr)
	case dstErr != nil:
		return nil, fmt.Errorf("%w: verified link: the installed entry %s could not be re-proven (%v) — the publish may stand at the destination", ErrPublishCompleted, dst, dstErr)
	}
	if perr := proof(dst, dstInfo); perr != nil {
		// Bind the cleanup decision to the link operation's OWN identity (the
		// admitted pre-link object — the only node this composite provably
		// installed), never dst's CURRENT identity (codex P1,
		// PRRT_kwDORn9KaM6npnwi): a divergent entry is unproven — our install
		// may have been renamed aside and the name replanted by another writer
		// inside the link→lstat window — so it is RETAINED byte-intact with
		// the doubt-as-published class joined, exactly like a wedged
		// compensation. Only an entry still provably aliasing the admitted
		// object is bound-unlinked.
		if !asideSameObject(dstInfo, srcInfo) {
			// The current occupant affirmatively DIVERGES from the link
			// operation's own identity: an explicitly unproven successor, not a
			// merely indeterminate re-proof. It is retained byte-intact here,
			// and ErrPublishSuccessorUnproven tells the caller's
			// record-reflection that it is likewise NOT this batch's installed
			// output there — observing it for rollback would arm UnlinkVerified
			// against the successor's own identity and delete the foreign bytes
			// (codex P1, PRRT_kwDORn9KaM6nsX9a).
			return nil, errors.Join(
				fmt.Errorf("verified link: the installed entry %s failed its admission proof (%w): %w", dst, ErrTakeAsideForeign, perr),
				fmt.Errorf("%w: %s no longer provably names the object the link operation installed — the unproven entry is retained byte-intact (never bound-unlinked against its own current identity)", ErrPublishCompleted, dst),
				fmt.Errorf("%w: the occupant at %s must be retained, never observed as this operation's installed output", ErrPublishSuccessorUnproven, dst),
			)
		}
		if rmErr := UnlinkVerified(fs, dst, srcInfo); rmErr != nil {
			return nil, errors.Join(
				fmt.Errorf("verified link: the installed entry %s failed its admission proof (%w): %w", dst, ErrTakeAsideForeign, perr),
				fmt.Errorf("%w: the rejected install could not be bound-unlinked and stays recoverable at %s: %v", ErrPublishCompleted, dst, rmErr),
			)
		}
		return nil, fmt.Errorf("verified link: the installed entry %s failed its admission proof — the rejected install was unlinked (%w): %w", dst, ErrTakeAsideForeign, perr)
	}
	return dstInfo, nil
}
