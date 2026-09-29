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
//     fails that proof, and the foreign install is removed through
//     UnlinkVerified: only the exact re-proven object is ever unlinked (never
//     a pathname Remove of an unproven entry), the refused object stays put at
//     the source name, and the typed refusal (ErrTakeAsideForeign) is what the
//     swap subject sees. A compensation that itself wedges keeps every object
//     and joins ErrPublishCompleted so the caller's observe/rollback machinery
//     reaps the stranded install.
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
	if proof == nil {
		return link(src, dst)
	}
	done, err := classifyNoreplaceDestination(fs, src, dst)
	if done || err != nil {
		return err
	}
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return fmt.Errorf("verified link: create destination directory: %w", err)
	}
	srcFile, err := openVerifiedSource(fs, src)
	if err != nil {
		return fmt.Errorf("verified link: open source %s: %w", src, err)
	}
	defer func() { _ = srcFile.Close() }()
	srcInfo, statErr := srcFile.Stat()
	if statErr != nil {
		return fmt.Errorf("verified link: inspect the open source handle %s: %w", src, statErr)
	}
	if perr := proof(src, srcInfo); perr != nil {
		return fmt.Errorf("verified link: the open source handle failed its admission proof (%w): %w", ErrTakeAsideForeign, perr)
	}
	if lerr := link(src, dst); lerr != nil {
		return lerr
	}
	dstInfo, dstErr := asideLstat(fs, dst)
	switch {
	case errors.Is(dstErr, os.ErrNotExist):
		return fmt.Errorf("verified link: the installed entry %s vanished before its identity proof: %w", dst, dstErr)
	case dstErr != nil:
		return fmt.Errorf("%w: verified link: the installed entry %s could not be re-proven (%v) — the publish may stand at the destination", ErrPublishCompleted, dst, dstErr)
	}
	if perr := proof(dst, dstInfo); perr != nil {
		if rmErr := UnlinkVerified(fs, dst, dstInfo); rmErr != nil {
			return errors.Join(
				fmt.Errorf("verified link: the installed entry %s failed its admission proof (%w): %w", dst, ErrTakeAsideForeign, perr),
				fmt.Errorf("%w: the rejected install could not be bound-unlinked and stays recoverable at %s: %v", ErrPublishCompleted, dst, rmErr),
			)
		}
		return fmt.Errorf("verified link: the installed entry %s failed its admission proof — the rejected install was unlinked (%w): %w", dst, ErrTakeAsideForeign, perr)
	}
	return nil
}
