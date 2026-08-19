//go:build !windows

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/spf13/afero"
)

// publishStagedBoundDeferredChtimes is the ENOSYS-platform deferred times leg
// (fd-scoped wrappers missing, staging_times_unixother.go): a Chtimes on the
// PUBLISHED destination name. Seam discipline like stagedHandleChtimes: no
// portable setup fails utimens on a just-published name for BOTH uid 0 and
// an ordinary user, so the failure leg is replayed here.
var publishStagedBoundDeferredChtimes = func(fs afero.Fs, name string, atime, mtime time.Time) error {
	return fs.Chtimes(name, atime, mtime)
}

// publishStagedBoundOS is PublishStagedBound's POSIX leg: the staged handle
// stays OPEN through the path publish (rename/hard-link publishes never
// needed it closed), and a successful publish is re-verified against the
// handle's identity — fh identity was captured by the pre-publish fstat and
// cannot change while the descriptor stays open. The post-publish VERIFIED
// destination stat rides the first return (wave-31): callers revalidate the
// destination against it before deleting their restore source.
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
//     no-replace publish into a caller-proven-absent dest, the occupant
//     RECORDED at the mismatch instant is displaced — but ONLY that
//     recorded inode, re-proven by a fresh Lstat immediately before the
//     unlink (wave-26 + wave-32, PR#215 findings: the recorded occupant
//     may itself be a foreign file created in the publish→reverify
//     window, and the window between the binding re-lookup and the
//     unlink may swap in a successor; neither is ever deleted). Genuine
//     bytes are then re-staged FROM THE HANDLE into a fresh O_EXCL name
//     and republished; a differing occupant, a failed displacement, or
//     an indeterminate lookup refuses typed with nothing consumed. A
//     dest that VANISHED between publish and reverify recovers the same
//     way with nothing to displace.
//  5. an indeterminate dest lookup, a restaging failure, or a persistent
//     substitution past the budget returns typed errors — the genuine
//     source backup is retained by every caller, so nothing is consumed.
func publishStagedBoundOS(p StagedPublish) (os.FileInfo, error) {
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
			return nil, fmt.Errorf("%w: %w", ErrPublishStagedVerify, verr)
		}
		pendingTimes := false
		if p.ApplyTimes {
			if terr := stagedHandleChtimes(of.Fd(), p.Atime, p.Mtime); terr != nil {
				if !errors.Is(terr, syscall.ENOSYS) {
					_ = fh.Close()
					return nil, &StagingTimesError{Staged: staged, Err: terr}
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
			return nil, pubErr
		}
		// Post-publish reverify. The handle still names our inode regardless
		// of any directory-level renaming, so only the destination side is
		// looked up by name — through the Lstat seam (production: os.Lstat;
		// this leg only runs under the osStagingHandle gate, i.e. the real
		// OsFs, and Lstat never follows).
		destInfo, lerr := publishStagedBoundDestLstat(p.Dest)
		switch {
		case lerr == nil && os.SameFile(handleInfo, destInfo):
			// The publish provably landed OUR inode at dest. Times deferred
			// on ENOSYS platforms land on the published name now; the handle
			// then closes — a post-publish close error cannot undo the
			// proven install and is deliberately not surfaced.
			if pendingTimes {
				// wave-32 (codex local round 2, PR#215 finding R5): the name-
				// based times leg re-proves the published name STILL addresses
				// the staged inode BEFORE the Chtimes — a foreign occupant in
				// the match→Chtimes window never gets its times clobbered, it
				// gets a typed refusal with the times skipped; an indeterminate
				// lookup is refused the same way.
				ownerNow, oerr := publishStagedBoundDestLstat(p.Dest)
				switch {
				case oerr == nil && os.SameFile(handleInfo, ownerNow):
					// still ours — the deferred times may land on the name.
				case oerr == nil:
					_ = fh.Close()
					return nil, fmt.Errorf("%w at %s — foreign occupant before the deferred times leg, name-based times skipped: %w: %w", ErrPublishStagedForeignOccupant, p.Dest, ErrPublishStagedIdentityIndeterminate, ErrPublishStagedIdentityBreak)
				case os.IsNotExist(oerr):
					_ = fh.Close()
					return nil, fmt.Errorf("published destination %s vanished before the deferred times leg: %w: %w", p.Dest, ErrPublishStagedIdentityIndeterminate, ErrPublishStagedIdentityBreak)
				default:
					_ = fh.Close()
					return nil, fmt.Errorf("published destination %s indeterminate before the deferred times leg: %w: %w: %w", p.Dest, oerr, ErrPublishStagedIdentityIndeterminate, ErrPublishStagedIdentityBreak)
				}
				if cerr := publishStagedBoundDeferredChtimes(p.FS, p.Dest, p.Atime, p.Mtime); cerr != nil {
					_ = fh.Close()
					return nil, &StagingTimesError{Staged: p.Dest, Err: cerr}
				}
				// wave-31: hand back a FRESH destination identity carrying the
				// just-applied times — destInfo predates the deferred Chtimes,
				// and the callers' destination revalidations compare mtime.
				// wave-32 (finding R5): a FAILED relookup used to degrade to a
				// nil identity the callers read as "nothing to check — safe";
				// it proves NOTHING about the published name, so it now refuses
				// typed. Equally, a relookup naming a DIFFERENT inode is never
				// handed back as the published identity.
				fresh, ferr := publishStagedBoundDestLstat(p.Dest)
				_ = fh.Close()
				if ferr != nil {
					return nil, fmt.Errorf("post-publish identity relookup of %s after the deferred times leg: %w: %w: %w", p.Dest, ferr, ErrPublishStagedIdentityIndeterminate, ErrPublishStagedIdentityBreak)
				}
				if !os.SameFile(handleInfo, fresh) {
					return nil, fmt.Errorf("%w at %s — destination no longer names the staged inode after the deferred times leg: %w", ErrPublishStagedForeignOccupant, p.Dest, ErrPublishStagedIdentityBreak)
				}
				return fresh, nil
			}
			_ = fh.Close()
			return destInfo, nil
		case lerr != nil && !os.IsNotExist(lerr):
			// Indeterminate destination: nothing proven about the name —
			// refuse typed, keep the caller's backup, touch nothing.
			_ = fh.Close()
			return nil, fmt.Errorf("post-publish publish reverify of %s: %w: %w", p.Dest, lerr, ErrPublishStagedIdentityBreak)
		}
		// The published bytes are NOT ours (mismatch) or are gone again
		// (ENOENT): the staged inode survives on the handle. Displace the
		// RECORDED plant — only for no-replace publishes into a
		// caller-proven-absent dest, and only the exact object the mismatch
		// detection recorded (wave-26/wave-32: a post-publish foreign
		// replacement is never unverified-deleted) — then re-stage the
		// genuine bytes FROM THE HANDLE and retry within the budget.
		if !os.IsNotExist(lerr) && p.NoReplace {
			// Wave-26 (codex P2, PR#215 finding 1) + wave-32 (codex local
			// review round 2, PR#215 finding R2): bind the unlink to the plant
			// RECORDED AT THE MISMATCH INSTANT (destInfo). The recorded object
			// may itself be a foreign replacement that claimed dest in the
			// publish→reverify window — no leg can distinguish it from a
			// pre-publish plant there — so the binding guarantees the narrower
			// property: ONLY the exact object recorded at detection (re-proven
			// here by SameFile against a fresh Lstat) is ever unlinked, never
			// a successor swapped in afterwards. A different occupant and any
			// indeterminate re-lookup are typed refusals (backup retained,
			// nothing consumed), and a FAILED displacement refuses typed
			// instead of proceeding to a republish over the surviving
			// occupant. The remaining Lstat→Remove window is the documented
			// portability residual: no portable unlink-by-handle exists, so
			// the binding narrows it to the verified-inode check.
			occupant, oerr := publishStagedBoundDestLstat(p.Dest)
			switch {
			case oerr == nil && os.SameFile(occupant, destInfo):
				// The occupant is STILL the recorded window plant — bound
				// displacement of the verified foreign install.
				if rmErr := p.FS.Remove(p.Dest); rmErr != nil && !os.IsNotExist(rmErr) {
					// The verified object could not be unlinked: refuse typed
					// rather than republish over it (a no-replace republish
					// would collision-fail anyway; a replace-style one would now
					// destroy the very object this leg proved foreign).
					_ = fh.Close()
					return nil, fmt.Errorf("post-publish displacement of the recorded plant at %s failed: %v: %w", p.Dest, rmErr, ErrPublishStagedIdentityBreak)
				}
			case oerr == nil:
				// Neither the recorded plant nor our inode: a post-publish
				// legitimate occupant. Preserve it byte-intact and refuse
				// typed rather than restaging over unverified bytes.
				_ = fh.Close()
				return nil, fmt.Errorf("%w at %s — post-publish occupant preserved, recovery refused (re-stage from handle not attempted over unverified foreign bytes): %w", ErrPublishStagedForeignOccupant, p.Dest, ErrPublishStagedIdentityBreak)
			case !os.IsNotExist(oerr):
				// Indeterminate re-lookup: nothing is proven about the name —
				// refuse typed, same posture as the detection-side failure.
				_ = fh.Close()
				return nil, fmt.Errorf("post-publish occupant binding lookup of %s: %w: %w", p.Dest, oerr, ErrPublishStagedIdentityBreak)
			}
			// os.IsNotExist(oerr): the plant vanished inside the window —
			// nothing to displace; the restage below republishes into absence.
		}
		if attempt+1 >= PublishStagedBoundAttempts {
			// Budget spent under a persistent substitution: refuse TYPED.
			// The plant was displaced above (no-replace legs) and the
			// callers' conservative legs retain the genuine backup — the
			// finding's consume-after-plant harm is closed even here.
			_ = fh.Close()
			return nil, fmt.Errorf("%w after %d attempts for %s: %w", ErrPublishStagedExhausted, PublishStagedBoundAttempts, p.Dest, ErrPublishStagedIdentityBreak)
		}
		newStaged, newFh, serr := CreateExclusiveStagingFile(p.FS, p.Dest, p.Suffix, p.NextOrdinal(), handleInfo.Mode().Perm())
		if serr != nil {
			_ = fh.Close()
			return nil, fmt.Errorf("re-stage substituted staged file for %s: %w: %w", p.Dest, serr, ErrPublishStagedIdentityBreak)
		}
		if rerr := publishStagedBoundRestream(fh, newFh); rerr != nil {
			_ = newFh.Close()
			_ = fh.Close()
			_ = p.FS.Remove(newStaged)
			return nil, fmt.Errorf("re-stage substituted bytes for %s: %w: %w", p.Dest, rerr, ErrPublishStagedIdentityBreak)
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
