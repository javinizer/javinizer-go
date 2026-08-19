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

// publishStagedBoundOsRemove is the bound unlink behind
// displacePrePublishCollisionPlant (wave-38, finding F1), exposed as a test
// seam (same discipline as publishStagedBoundDestLstat): this leg only runs
// under the osStagingHandle gate — i.e. the real OsFs — and the wedge legs
// cannot be replayed through wrapper filesystems without demoting the whole
// publish to the virtual tail, so tests replay them here.
var publishStagedBoundOsRemove = func(fs afero.Fs, name string) error { return fs.Remove(name) }

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
//     done. A mismatch means the published bytes are not ours — ours was
//     renamed away and stays reachable via the open handle. For a
//     no-replace publish into a caller-proven-absent dest the recovery
//     restages FROM THE HANDLE into a fresh O_EXCL name and republishes —
//     but ONLY ever displaces a destination object tied to PRE-PUBLISH
//     existence evidence, and never the object the post-publish Lstat
//     recorded at the mismatch instant (wave-38, codex P2, PR#215 finding
//     F1 — see the big comment in the mismatch leg below): the kernel
//     proved dest FREE at the instant a successful no-replace rename
//     landed, so every mismatch occupant — hostile plant or a legitimate
//     file created inside the publish→first-Lstat gap — arrived AFTER the
//     publish and is preserved byte-intact with a typed refusal. A dest
//     that VANISHED between publish and reverify (or vanished at the one
//     binding re-lookup) recovers via restage+republish with nothing to
//     displace.
//  5. a publish attempt that FAILED with ErrPublishCollision carries the
//     other tie: the occupant provably existed BEFORE the publish (it is
//     the obstacle the no-replace primitive refused on), so it is
//     RECORDED at the collision instant and displaced after a bound
//     re-proof (fresh Lstat + os.SameFile immediately before the unlink),
//     and the loop retries the publish with the staged name still intact.
//     A different occupant at the binding lookup, an indeterminate
//     lookup, or a failed displacement refuses typed with nothing
//     consumed.
//  6. an indeterminate dest lookup, a restaging failure, or a persistent
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
			// Wave-38 (codex P2, PR#215 finding F1) — evidence channel (a):
			// a no-replace publish that REFUSED on an occupied destination
			// proves the occupant existed BEFORE this attempt (it is the
			// obstacle the no-replace primitive reported). That pre-publish
			// existence tie is THE ONLY circumstance under which a dest
			// occupant may be displaced: RECORD the plant at the collision
			// instant, re-prove the name still addresses THE recorded object
			// (bound unlink — a successor swapped in afterwards is a typed
			// refusal, never deleted), drop the obstacle, and retry the
			// publish within the budget. The staged name was untouched by the
			// failed publish, so no re-stage is needed on this leg.
			if p.NoReplace && errors.Is(pubErr, ErrPublishCollision) {
				// Displace the recorded obstacle FIRST: the (a) evidence ties
				// it regardless of the remaining retry budget, exactly like the
				// pre-wave-38 exhaustion leg displaced its last proven plant.
				if derr := displacePrePublishCollisionPlant(p.FS, p.Dest); derr != nil {
					_ = fh.Close()
					return nil, derr
				}
				if attempt+1 >= PublishStagedBoundAttempts {
					_ = fh.Close()
					return nil, fmt.Errorf("%w after %d attempts for %s: %w", ErrPublishStagedExhausted, PublishStagedBoundAttempts, p.Dest, ErrPublishStagedIdentityBreak)
				}
				continue
			}
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
			// Wave-38 (codex P2, PR#215 finding F1): the post-publish mismatch
			// occupant (destInfo, recorded at the detection instant) is NEVER
			// unlinked. A successful no-replace publish PROVED the destination
			// free at the rename instant, so the occupant necessarily arrived
			// afterwards — the hostile staged-name plant the publish itself
			// moved onto dest and a LEGITIMATE file created inside the
			// publish→first-Lstat gap are indistinguishable at this layer, and
			// the wave-26/32 record-then-displace binding deleted both. The
			// occupancy-tie rule: ONLY an occupant tied to PRE-PUBLISH
			// existence evidence (a collision-refused publish attempt, handled
			// at the publish site above) may ever be displaced. Everything
			// recorded here is preserved byte-intact with a typed
			// ErrPublishStagedForeignOccupant refusal (backup retained, nothing
			// consumed, no restage over unverified bytes — the no-replace
			// republish would collision-fail anyway).
			//
			// ONE binding re-lookup still runs: an occupant that VANISHED
			// again inside the detection→recovery window leaves the
			// destination free, which is exactly the wave-30 vanish leg —
			// restage from the handle and republish into absence below.
			_, oerr := publishStagedBoundDestLstat(p.Dest)
			switch {
			case os.IsNotExist(oerr):
				// The occupant vanished inside the window — nothing to
				// displace; the restage below republishes into absence.
			case oerr != nil:
				// Indeterminate re-lookup: nothing is proven about the name —
				// refuse typed, same posture as the detection-side failure.
				_ = fh.Close()
				return nil, fmt.Errorf("post-publish occupant binding lookup of %s: %w: %w", p.Dest, oerr, ErrPublishStagedIdentityBreak)
			default:
				// Still occupied — post-publish occupant (recorded: destInfo)
				// preserved byte-intact; recovery refused typed.
				_ = fh.Close()
				return nil, fmt.Errorf("%w at %s — post-publish occupant preserved, recovery refused (no evidence tied it to pre-publish obstruction; re-stage from handle never attempted over unverified foreign bytes): %w", ErrPublishStagedForeignOccupant, p.Dest, ErrPublishStagedIdentityBreak)
			}
		}
		if attempt+1 >= PublishStagedBoundAttempts {
			// Budget spent under a persistent substitution: refuse TYPED.
			// Evidence-tied obstacles were displaced above where the rule
			// licensed it, and the callers' conservative legs retain the
			// genuine backup — the finding's consume-after-plant harm is
			// closed even here.
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

// displacePrePublishCollisionPlant removes the destination obstacle a
// collision-refused no-replace publish just PROVED to pre-date the publish
// attempt (wave-38, codex P2, PR#215 finding F1 — evidence channel (a)):
// the occupant is recorded at the collision instant and unlinked ONLY while
// the destination still addresses THE recorded object (fresh no-follow
// Lstat + os.SameFile immediately before the Remove). Every divergence is a
// typed refusal with the occupant preserved byte-intact:
//
//   - record lookup VANISHED (or the binding re-lookup did): nothing to
//     displace — the retried publish republishes into absence (nil);
//   - either lookup INDETERMINATE: nothing is proven about the name —
//     ErrPublishStagedIdentityBreak, nothing removed;
//   - the binding re-lookup names a DIFFERENT object: the recorded plant
//     was displaced by some racer inside the binding window —
//     ErrPublishStagedForeignOccupant, that successor preserved (never a
//     delete-by-record);
//   - the bound unlink FAILED: ErrPublishStagedIdentityBreak — the loop
//     never proceeds to a republish over the surviving occupant.
func displacePrePublishCollisionPlant(fs afero.Fs, dest string) error {
	plant, perr := publishStagedBoundDestLstat(dest)
	switch {
	case os.IsNotExist(perr):
		return nil
	case perr != nil:
		return fmt.Errorf("pre-publish plant record of %s: %w: %w", dest, perr, ErrPublishStagedIdentityBreak)
	}
	occupant, oerr := publishStagedBoundDestLstat(dest)
	switch {
	case oerr == nil && os.SameFile(occupant, plant):
		// The obstacle is STILL the recorded pre-publish plant — bound
		// displacement of the evidence-tied object.
		if rmErr := publishStagedBoundOsRemove(fs, dest); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("pre-publish plant displacement at %s failed: %w: %w", dest, rmErr, ErrPublishStagedIdentityBreak)
		}
		return nil
	case oerr == nil:
		return fmt.Errorf("%w at %s — a successor replaced the recorded pre-publish plant inside the binding window, preserved byte-intact: %w", ErrPublishStagedForeignOccupant, dest, ErrPublishStagedIdentityBreak)
	case !os.IsNotExist(oerr):
		return fmt.Errorf("pre-publish plant binding lookup of %s: %w: %w", dest, oerr, ErrPublishStagedIdentityBreak)
	default: // os.IsNotExist(oerr)
		return nil
	}
}
