package fsutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/config"
)

// move_noreplace.go — the organize-side atomic no-clobber composites (#224
// phases A+B). MoveFileFs/CopyFileFs keep REPLACE semantics for authorized
// overwrites; these two are the never-clobber variants built from the
// poster-write-hardening toolkit (CreateExclusiveStagingFile /
// PublishStagedBound / UnlinkVerified / PublishNoReplace).
//
// Vocabulary is deliberately the existing one: occupancy refuses with
// ErrPublishCollision, inexpressible volumes with ErrPublishNoReplaceUnsupported;
// both are PublishRefusal() subclasses. On every refusal/ambiguous failure,
// src AND dst are both preserved byte-intact.

// stagingFileMode restores the pre-#224 mode semantics for staging writes:
// the legacy OpenFile path let the kernel apply the process umask,
// CreateExclusiveStagingFile deliberately Chmods the exact requested mode
// instead. Without re-masking, organized files would land wider than the
// configured umask permits (codex P1). With no cached umask this degenerates
// to config.FilePerm unchanged.
func stagingFileMode() os.FileMode {
	return config.FilePerm &^ os.FileMode(config.UmaskValue())
}

// noreplaceOrdinal is the process-local staging-name nonce for the
// no-replace composites (exclusive staging retries ordinals inside).
var noreplaceOrdinal atomic.Uint64

// nextNoReplaceOrdinal hands out the next ordinal for staging-name generation.
func nextNoReplaceOrdinal() uint64 { return noreplaceOrdinal.Add(1) }

// classifyNoreplaceDestination runs the adoption/classification pre-check:
// PublishNoReplace carries NO adoption semantics (every occupied dst refuses),
// while organize keeps #225's no-op rules — lexical self, and same-inode
// (hardlink) aliases of the source. A symlink SOURCE pointing at dst's regular
// inode is NOT an alias (mirrors refuseExistingDestination's exclusion).
//
// Returns done=true with nil when the caller must return success without
// touching anything.
func classifyNoreplaceDestination(fs afero.Fs, src, dst string) (done bool, err error) {
	if filepath.Clean(src) == filepath.Clean(dst) {
		return true, nil
	}
	srcInfo, statErr := asideLstat(fs, src)
	if statErr != nil {
		return false, fmt.Errorf("no-replace: probe source %s: %w", src, statErr)
	}
	dstInfo, dstErr := asideLstat(fs, dst)
	if dstErr == nil {
		if !asideIsSymlink(srcInfo) && os.SameFile(srcInfo, dstInfo) {
			return true, nil
		}
		return false, fmt.Errorf("%w: %s", ErrPublishCollision, dst)
	}
	if !os.IsNotExist(dstErr) {
		// Indeterminate destination state — refuse without touching (the same
		// posture history holds for its restores).
		return false, fmt.Errorf("no-replace: probe destination %s: %w", dst, dstErr)
	}
	return false, nil
}

// asideIsSymlink reports whether info names a symlink.
func asideIsSymlink(info os.FileInfo) bool {
	return info != nil && info.Mode()&os.ModeSymlink != 0
}

// MoveFileNoReplace moves src to dst without ever replacing destination
// content.
//
// Symlink-source corners (spec decision note): an EXDEV move of a symlink
// publishes the TARGET's bytes as a regular file and necessarily refuses the
// source cleanup (the identity check rejects symlink identities), surfacing an
// ambiguity error with both objects preserved; and on non-Linux POSIX the
// same-volume fallback's link(2) dereferences symlink sources, so some
// same-volume fallback implementations may link the target inode rather than
// renaming the link itself (POSIX leaves symlink-source link(2) behavior
// implementation-defined).
// Organizer sources are scanner-yielded regular files, so neither arises in
// production. Same-volume is PublishNoReplace (src consumed by the rename);
// cross-device stages dest-adjacent with O_EXCL, streams through the open
// handle, publishes with bound identity (PublishStagedBound), and removes src
// ONLY via identity-checked unlink — a source swapped after the publish is
// preserved and reported (both objects then exist; the move is done at dst
// but reported as ambiguous rather than silently destroying the foreign src).
func MoveFileNoReplace(fs afero.Fs, src, dst string) error {
	done, err := classifyNoreplaceDestination(fs, src, dst)
	if done || err != nil {
		return err
	}
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return fmt.Errorf("no-replace move: create destination directory: %w", err)
	}
	if pubErr := PublishNoReplace(fs, src, dst); pubErr == nil {
		return nil
	} else if !isCrossDeviceError(pubErr) {
		return pubErr
	}

	// Cross-device leg: stage, stream, bound publish, verified source cleanup.
	srcPre, err := asideLstat(fs, src)
	if err != nil {
		return fmt.Errorf("no-replace move: re-probe source %s before staging: %w", src, err)
	}
	if err := CopyFileNoReplace(fs, src, dst); err != nil {
		return err
	}
	if rmErr := UnlinkVerified(fs, src, srcPre); rmErr != nil {
		// dst was published; src could not be proven the pre-move object —
		// keep BOTH (a swapped source is foreign) and surface the ambiguity.
		// The wrap JOINS ErrPublishCompleted (publish already landed — callers
		// must never classify this as a pre-publish refusal) with the typed
		// cleanup error so diagnostics survive without either class lying.
		return fmt.Errorf("%w: no-replace move: published to %s but source cleanup refused (%w) — source preserved", ErrPublishCompleted, dst, rmErr)
	}
	return nil
}

// CopyFileNoReplace copies src to dst via dest-adjacent exclusive staging and
// a bound no-replace publish. src is never modified. On every failure leg the
// staged name is discarded via the bound discipline (a planted substitute is
// never unlinked), dst content is never replaced, and a foreign plant wins the
// name with ErrPublishCollision.
func CopyFileNoReplace(fs afero.Fs, src, dst string) error {
	done, err := classifyNoreplaceDestination(fs, src, dst)
	if done || err != nil {
		return err
	}
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return fmt.Errorf("no-replace copy: create destination directory: %w", err)
	}

	srcFile, err := fs.Open(src)
	if err != nil {
		return fmt.Errorf("no-replace copy: open source %s: %w", src, err)
	}
	defer func() { _ = srcFile.Close() }()

	staged, handle, err := CreateExclusiveStagingFile(fs, dst, ".nrstg", noreplaceOrdinal.Add(1), stagingFileMode())
	if err != nil {
		return fmt.Errorf("no-replace copy: exclusive staging for %s: %w", dst, err)
	}
	if _, err := io.Copy(handle, srcFile); err != nil {
		DiscardFailedExclusiveStaging(fs, staged, handle)
		return fmt.Errorf("no-replace copy: stream into staging for %s: %w", dst, err)
	}

	stagedIdentity := stagingIdentity(handle)

	p := StagedPublish{
		FS:          fs,
		Publish:     func(fsys afero.Fs, s, d string) error { return PublishNoReplace(fsys, s, d) },
		NoReplace:   true,
		Staged:      staged,
		Handle:      handle,
		Dest:        dst,
		Suffix:      ".nrstg",
		NextOrdinal: nextNoReplaceOrdinal,
	}
	if err := PublishStagedBound(p); err != nil {
		discardStagedAfterFailedPublish(fs, staged, stagedIdentity, err)
		return fmt.Errorf("no-replace copy: publish %s: %w", dst, err)
	}
	return nil
}

// stagingIdentity captures the staged object's identity while its handle is
// pinned (identity-bearing opens), so post-failure cleanup can re-prove the
// name before unlinking (see discardStagedAfterFailedPublish).
func stagingIdentity(fh afero.File) os.FileInfo {
	if fh == nil {
		return nil
	}
	info, err := fh.Stat()
	if err != nil {
		return nil
	}
	return info
}

// discardStagedAfterFailedPublish removes the staged copy after a failed
// bound publish. The staged name is ordinal-shaped and attacker-observable;
// a racer can substitute a foreign file within remove windows, so the removal
// is executed via UnlinkVerified bound to the identity captured at staging
// time (#224 codex finding) — a foreign substitute is never unlinked.
//
// It is skipped for classes where the contract deliberately retains the
// staged name: ErrPublishStagedVerify (name unproven from the start) and any
// ErrPublishCompleted-carrying class (the name was left in place and may
// already address a foreign object).
func discardStagedAfterFailedPublish(fs afero.Fs, staged string, identity os.FileInfo, pubErr error) {
	if errors.Is(pubErr, ErrPublishStagedVerify) || PublishCompleted(pubErr) {
		return
	}
	if identity == nil {
		return // identity unknowable — keep both, never unbound-remove
	}
	// Removal must be bound to the verified staged INODE, never the name:
	// a directory writer who renames the staged name away and plants a
	// substitute would otherwise get ITS file deleted between our proof and
	// the unlink. Vacate via a plain rename (no no-replace primitive needed —
	// this works on exFAT too, codex r4), then re-prove the vacated object
	// matches the staged identity; any mismatch or move-away restores the
	// occupancy without deleting anything.
	vac, claimInfo, claimErr := claimTakeAsideVacName(fs, staged)
	if claimErr != nil {
		return // could not claim a vacated sibling; keep both
	}
	if relErr := releaseTakeAsideVacClaim(fs, vac, claimInfo); relErr != nil {
		return
	}
	if err := fs.Rename(staged, vac); err != nil {
		return // staged already gone or foreign-won window — keep both
	}
	vacInfo, lerr := asideLstat(fs, vac)
	if lerr != nil || !asideSameObject(vacInfo, identity) {
		// The object renamed into vac is NOT the verified staged copy — a plant
		// won the rename window. Restore occupancy: put it back on the staged
		// name (best-effort) and delete nothing.
		// A failed restore keeps both names live; the caller's error still
		// propagates the publish outcome. `rb` is informational only.
		rb := fs.Rename(vac, staged)
		_ = rb
		return
	}
	_ = fs.Remove(vac)
}
