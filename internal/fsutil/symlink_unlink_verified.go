package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

// symlink_unlink_verified.go — the link-object twin of UnlinkVerified
// (bound_take.go). A pinned soft-link delete intent (models.DeleteEntry
// LinkTarget) authenticates the link OBJECT by its readlink payload — a
// symlink has no bytes of its own to hash, and the bound take's shape
// identity (asideSameObject) deliberately refuses symlink objects, so the
// regular-file unlink can never express this removal.

// ReadlinkNoFollow resolves the link OBJECT at name without following it,
// through the afero LinkReader seam (OsFs and wrappers that forward it).
// ok is false where the filesystem exposes no link model at all (in-memory
// afero): the caller then cannot prove a symlink-shaped pin and must retain.
func ReadlinkNoFollow(fs afero.Fs, name string) (target string, ok bool, err error) {
	lr, supported := fs.(afero.LinkReader)
	if !supported {
		return "", false, nil
	}
	target, err = lr.ReadlinkIfPossible(name)
	if err != nil {
		return "", true, err
	}
	return target, true, nil
}

// SymlinkTargetsEqual compares a live readlink payload against a pinned
// target. Byte equality is the rule; filepath.Clean equality additionally
// absorbs the separator normalization platform symlink layers apply to the
// STORED payload (the Windows symlink syscall path rewrites forward slashes
// to backslashes, so a byte-pinned "/" spelling reads back "\"). Both forms
// name the same object — the symlink layer resolves either spelling — so
// treating them as one target cannot condemn a foreign link; a payload
// differing in anything substantive (case, components, volume) still
// mismatches.
func SymlinkTargetsEqual(a, b string) bool {
	return a == b || filepath.Clean(a) == filepath.Clean(b)
}

// vacateLinkObjectNoReplace renames the link OBJECT at name onto terminal
// without following it and without replacing an occupied terminal. The bound
// take's PublishNoReplace cannot serve this shape off Linux/Windows: its
// POSIX fallback is link(2), which is free to dereference a symlink SOURCE
// (implementation-defined per POSIX), aliasing the target inode instead of
// moving the link — so OsFs routes to the per-platform object-level
// primitive (vacate_link_noreplace_*.go: renameat2 RENAME_NOREPLACE on
// Linux, renameatx_np RENAME_EXCL on Darwin, MoveFileEx without the replace
// flag on Windows, a typed refusal elsewhere). A non-OsFs takes the shared
// virtual classify-then-rename leg: in-memory afero filesystems model no
// symlinks at all, while wrappers around an OsFs (BasePathFs & co.) resolve
// to the kernel rename through the fs surface — the residual
// classify→rename window there is the same accepted posture
// publishNoReplaceVirtual documents, against a terminal name claimed by an
// unguessable crypto-token draw.
func vacateLinkObjectNoReplace(fs afero.Fs, name, terminal string) error {
	if _, ok := fs.(*afero.OsFs); ok {
		return vacateLinkObjectOsFs(name, terminal)
	}
	return publishNoReplaceVirtual(fs, name, terminal)
}

// UnlinkSymlinkVerified removes the symlink object at name ONLY when it
// provably still carries the pinned readlink payload — the deterministic
// ownership certificate for a soft-link install (the expected target string
// is the whole payload a symlink can carry, so a readlink match
// authenticates the exact directory entry the install created; the install's
// creation time cannot exist at pin time, which is why no mtime/identity
// tuple participates — see models.DeleteEntry LinkTarget).
//
// Mechanics mirror UnlinkVerified's bound construction: the entry vacates
// onto a fresh crypto-claimed terminal sibling (object-level, never
// following the link), and ONLY the terminal is removed after the re-auth
// proves (a) it is a symlink object and (b) its readback payload equals
// expectTarget. A foreign occupant rename-swapped onto name inside the
// probe→vacate window rides over, fails the re-auth, and is rewound
// byte-intact no-replace; any vanish answers ErrTakeAsideVanished exactly
// like the regular-file twin, so callers classify consumed-vs-retained on
// the unchanged vocabulary. A platform or filesystem that cannot express
// the object-level vacate (or has no link model at all) rewinds and
// retains: an unprovable pin never authorizes a pathname remove.
func UnlinkSymlinkVerified(fs afero.Fs, name, expectTarget string) error {
	terminal, termClaim, cerr := claimTakeAsideVacName(fs, name)
	if cerr != nil {
		return fmt.Errorf("reserve the bound symlink-unlink terminal for %s: %w", name, cerr)
	}
	if relErr := releaseTakeAsideVacClaim(fs, terminal, termClaim); relErr != nil {
		return relErr
	}
	// rewind restores the pre-vacate occupancy after any doubt leg: whatever
	// the terminal holds moves BACK onto the freed name no-replace, mirroring
	// rerideBoundUnlink but through the link-object vacate (PublishNoReplace
	// must never touch a symlink source — see vacateLinkObjectNoReplace).
	rewind := func(cause error) error {
		if back := vacateLinkObjectNoReplace(fs, terminal, name); back != nil {
			return errors.Join(cause, fmt.Errorf("%w: %s re-claimed or indeterminate — the terminal object stays recoverable at %s: %v", ErrTakeAsideRestoreFailed, name, terminal, back))
		}
		return cause
	}
	if moveErr := vacateLinkObjectNoReplace(fs, name, terminal); moveErr != nil {
		if errors.Is(moveErr, os.ErrNotExist) {
			return fmt.Errorf("%w: %s vanished under the bound symlink unlink", ErrTakeAsideVanished, name)
		}
		return fmt.Errorf("bound symlink-unlink vacate of %s onto %s refused — occupant preserved byte-intact: %w", name, terminal, moveErr)
	}
	term, terr := asideLstat(fs, terminal)
	switch {
	case errors.Is(terr, os.ErrNotExist):
		return fmt.Errorf("%w: %s (terminal %s empty after the vacate)", ErrTakeAsideVanished, name, terminal)
	case terr != nil:
		return rewind(fmt.Errorf("inspect the bound symlink-unlink terminal %s: %w", terminal, terr))
	case term.Mode()&os.ModeSymlink == 0:
		return rewind(fmt.Errorf("bound symlink-unlink terminal %s is not the pinned symlink object (mode %v) — occupant preserved (never unlinked): %w", terminal, term.Mode(), ErrTakeAsideForeign))
	}
	target, readable, rerr := ReadlinkNoFollow(fs, terminal)
	switch {
	case rerr != nil && errors.Is(rerr, os.ErrNotExist):
		return fmt.Errorf("%w: %s (terminal %s vanished at the readlink re-auth)", ErrTakeAsideVanished, name, terminal)
	case rerr != nil:
		return rewind(fmt.Errorf("readlink the bound symlink-unlink terminal %s: %w", terminal, rerr))
	case !readable:
		return rewind(fmt.Errorf("bound symlink-unlink terminal %s has no provable link model — retained: %w", terminal, ErrTakeAsideForeign))
	case !SymlinkTargetsEqual(target, expectTarget):
		return rewind(fmt.Errorf("bound symlink-unlink terminal %s targets %q, not the pinned %q — foreign link preserved (never unlinked): %w", terminal, target, expectTarget, ErrTakeAsideForeign))
	}
	if rmErr := fs.Remove(terminal); rmErr != nil {
		if errors.Is(rmErr, os.ErrNotExist) {
			return fmt.Errorf("%w: %s (terminal %s vanished under the unlink)", ErrTakeAsideVanished, name, terminal)
		}
		return rewind(fmt.Errorf("remove the bound symlink-unlink terminal %s: %w", terminal, rmErr))
	}
	return nil
}
