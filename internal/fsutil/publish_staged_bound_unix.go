//go:build !windows

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// publishStagedBoundOS is PublishStagedBound's POSIX leg: the staged handle
// stays OPEN through the path publish (rename/hard-link publishes never
// needed it closed), and a successful publish is re-verified against the
// handle's identity — fh identity was captured by the pre-publish fstat and
// cannot change while the descriptor stays open.
//
// The loop, bounded by PublishStagedBoundAttempts:
//
//  1. verify the staged name STILL addresses the handle's inode (a swapped
//     name is refused BEFORE touching it, exactly like wave-29);
//  2. land the times through the handle (ENOSYS platforms defer them onto
//     the published name below);
//  3. publish by path;
//  4. re-verify: Lstat(dest) must name the handle's inode. A match is
//     done. A mismatch means the publish installed a window PLANT — ours
//     was renamed away and stays reachable via the open handle. For a
//     no-replace publish into a caller-proven-absent dest, the plant is
//     necessarily this operation's own install (never pre-existing bytes)
//     and is displaced; genuine bytes are re-staged FROM THE HANDLE into a
//     fresh O_EXCL name and republished. A dest that VANISHED between
//     publish and reverify recovers the same way with nothing to displace.
//  5. an indeterminate dest lookup, a restaging failure, or a persistent
//     substitution past the budget returns typed errors — the genuine
//     source backup is retained by every caller, so nothing is consumed.
func publishStagedBoundOS(p StagedPublish) error {
	staged, fh := p.Staged, p.Handle
	// Recomputed after every re-stage; the caller's gate (osStagingHandle)
	// guarantees the first unwrapping succeeds, and re-stages always produce
	// native handles on the OsFs.
	of, _ := osStagingHandle(p.FS, fh)
	for attempt := 0; ; attempt++ {
		handleInfo, verr := stagedIdentityProof(p.FS, staged, fh)
		if verr != nil {
			// The staged name is unproven (foreign or vanished): refuse
			// BEFORE any publish, leave the name untouched, drop the handle.
			_ = fh.Close()
			return fmt.Errorf("%w: %w", ErrPublishStagedVerify, verr)
		}
		pendingTimes := false
		if p.ApplyTimes {
			if terr := stagedHandleChtimes(of.Fd(), p.Atime, p.Mtime); terr != nil {
				if !errors.Is(terr, syscall.ENOSYS) {
					_ = fh.Close()
					return &StagingTimesError{Staged: staged, Err: terr}
				}
				// No fd-scoped timestamp wrapper on this platform (see
				// staging_times_unixother.go): defer to the name-based leg
				// against the PUBLISHED name — the staged name no longer
				// exists by then.
				pendingTimes = true
			}
		}
		if pubErr := p.Publish(p.FS, staged, p.Dest); pubErr != nil {
			_ = fh.Close()
			return pubErr
		}
		// Post-publish reverify. The handle still names our inode regardless
		// of any directory-level renaming, so only the destination side is
		// looked up by name — os.Lstat directly: this leg only runs under the
		// osStagingHandle gate, i.e. the real OsFs (Lstat, never follows).
		destInfo, lerr := os.Lstat(p.Dest)
		switch {
		case lerr == nil && os.SameFile(handleInfo, destInfo):
			// The publish provably landed OUR inode at dest. Times deferred
			// on ENOSYS platforms land on the published name now; the handle
			// then closes — a post-publish close error cannot undo the
			// proven install and is deliberately not surfaced.
			if pendingTimes {
				if cerr := p.FS.Chtimes(p.Dest, p.Atime, p.Mtime); cerr != nil {
					_ = fh.Close()
					return &StagingTimesError{Staged: p.Dest, Err: cerr}
				}
			}
			_ = fh.Close()
			return nil
		case lerr != nil && !os.IsNotExist(lerr):
			// Indeterminate destination: nothing proven about the name —
			// refuse typed, keep the caller's backup, touch nothing.
			_ = fh.Close()
			return fmt.Errorf("post-publish publish reverify of %s: %w: %w", p.Dest, lerr, ErrPublishStagedIdentityBreak)
		}
		// The published bytes are NOT ours (mismatch) or are gone again
		// (ENOENT): the staged inode survives on the handle. Displace the
		// proven plant — only for no-replace publishes into a
		// caller-proven-absent dest, where a mismatched occupant is
		// necessarily this publish's own install — then re-stage the
		// genuine bytes FROM THE HANDLE and retry within the budget.
		if !os.IsNotExist(lerr) && p.NoReplace {
			_ = p.FS.Remove(p.Dest)
		}
		if attempt+1 >= PublishStagedBoundAttempts {
			// Budget spent under a persistent substitution: refuse TYPED.
			// The plant was displaced above (no-replace legs) and the
			// callers' conservative legs retain the genuine backup — the
			// finding's consume-after-plant harm is closed even here.
			_ = fh.Close()
			return fmt.Errorf("%w after %d attempts for %s: %w", ErrPublishStagedExhausted, PublishStagedBoundAttempts, p.Dest, ErrPublishStagedIdentityBreak)
		}
		newStaged, newFh, serr := CreateExclusiveStagingFile(p.FS, p.Dest, p.Suffix, p.NextOrdinal(), handleInfo.Mode().Perm())
		if serr != nil {
			_ = fh.Close()
			return fmt.Errorf("re-stage substituted staged file for %s: %w: %w", p.Dest, serr, ErrPublishStagedIdentityBreak)
		}
		if rerr := publishStagedBoundRestream(fh, newFh); rerr != nil {
			_ = newFh.Close()
			_ = fh.Close()
			_ = p.FS.Remove(newStaged)
			return fmt.Errorf("re-stage substituted bytes for %s: %w: %w", p.Dest, rerr, ErrPublishStagedIdentityBreak)
		}
		// The fresh inode mirrors the original's metadata: ownership rides
		// the handle again (best-effort, same as the callers' pre-publish
		// leg); mode lands at create and times at loop head on the next
		// attempt.
		RestoreStagingOwnership(p.FS, newFh, handleInfo)
		_ = fh.Close()
		staged, fh = newStaged, newFh
		of, _ = osStagingHandle(p.FS, fh)
	}
}
