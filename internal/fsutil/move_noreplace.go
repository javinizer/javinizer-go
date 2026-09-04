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

	staged, handle, err := CreateExclusiveStagingFile(fs, dst, ".nrstg", noreplaceOrdinal.Add(1), config.FilePerm)
	if err != nil {
		return fmt.Errorf("no-replace copy: exclusive staging for %s: %w", dst, err)
	}
	if _, err := io.Copy(handle, srcFile); err != nil {
		DiscardFailedExclusiveStaging(fs, staged, handle)
		return fmt.Errorf("no-replace copy: stream into staging for %s: %w", dst, err)
	}

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
		discardStagedAfterFailedPublish(fs, staged, err)
		return fmt.Errorf("no-replace copy: publish %s: %w", dst, err)
	}
	return nil
}

// discardStagedAfterFailedPublish removes the staged copy after a failed
// bound publish. Per PublishStagedBound's contract, every class except the
// pre-publish verify failure and the ErrPublishCompleted-carrying classes has
// the staged name either still provably ours or already consumed — removing it
// is safe. The forbidden classes keep the name in place deliberately
// (identity-unproven staged names may address a foreign object).
func discardStagedAfterFailedPublish(fs afero.Fs, staged string, err error) {
	if errors.Is(err, ErrPublishStagedVerify) || PublishCompleted(err) {
		return
	}
	_ = fs.Remove(staged)
}
