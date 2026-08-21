package fsutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/afero"
)

// wave-30 (codex P1, PR#215) — bind staged identity ACROSS the publish.
//
// The wave-29 posture proved the staged NAME addresses the staged INODE
// immediately before the publish (VerifyStagedIdentity), then published by
// path. Between that proof and the publish a directory writer can rename
// the staged name away and plant a substitute on it: the path publish then
// installs the PLANT at the destination (an absent destination, in the
// no-replace flows — proven absent by the caller's classification), after
// which the caller consumes/deletes the genuine backup. The staged bytes
// survive only under the attacker's chosen name.
//
// PublishStagedBound closes the window with a publish-with-reverify loop
// (platform legs in publish_staged_bound_unix.go /
// publish_staged_bound_windows.go):
//
//  1. keep the staged handle OPEN through the publish attempt — the genuine
//     inode stays reachable through the descriptor no matter what happens
//     to any directory name;
//  2. re-verify immediately AFTER a successful publish: the destination
//     must name the handle's inode (os.SameFile);
//  3. on a mismatch the published object is not ours — ours was renamed
//     away and we still hold its handle — so the genuine bytes are
//     re-staged FROM THE HANDLE (seek 0, copy into a fresh O_EXCL staging
//     name) and republished under a BOUNDED attempt budget. Wave-38 (codex
//     P2, PR#215 finding F1): a post-publish SUCCESS→mismatch occupant
//     necessarily arrived AFTER the kernel-proven-free rename — the
//     staged-name plant the publish moved onto dest and a legitimate file
//     created inside the publish→reverify gap are indistinguishable there,
//     so BOTH are preserved byte-intact with a typed
//     ErrPublishStagedForeignOccupant refusal; recovery continues only when
//     the occupant VANISHED (the restage republishes into proven absence).
//     Wave-49 (codex P2, PR#215) removes the wave-38 pre-publish COLLISION
//     displacement too: a no-replace publish that refused on
//     ErrPublishCollision names a racer that WON the race — 'no-replace'
//     means creating only when absent, so the winner's bytes must prevail
//     (the delete-then-retry leg destroyed a legitimate writer with no
//     backup and no ledger entry). The collision now surfaces verbatim so
//     callers reclassify through their wave-15/wave-17 legs;
//  4. exhaustion, an indeterminate destination, or (Windows leg) any
//     post-publish identity break returns typed errors and NOTHING is
//     consumed: the caller's conservative legs keep the backup armed.
//
// Bytes are never lost: the genuine content stays reachable via the open
// handle until a publish is proven to have landed OUR inode at dest, and on
// every refusal the caller retains the source backup and leaves its journal
// entry unconsumed.

// ErrPublishStagedVerify classifies the pre-publish identity-proof failure
// returned by PublishStagedBound: the staged name did not provably address
// the handle's inode and NO publish was attempted. The name is unproven —
// callers MUST NOT remove it (it may be a foreign plant). The underlying
// ErrStagedIdentityMismatch (or lookup failure) stays unwrap-reachable.
var ErrPublishStagedVerify = errors.New("staged identity proof failed — publish refused untouched")

// ErrPublishStagedClose classifies a staged-handle close failure on the
// legs that must close BEFORE the publish (virtual filesystems, whose mem
// handles re-stamp at close; the Windows publish, whose MoveFileEx cannot
// rename an open file). Nothing was published.
var ErrPublishStagedClose = errors.New("staged handle close failed before publish")

// ErrPublishStagedIdentityBreak classifies the post-publish reverify
// failure: the publish reported success but the destination does not
// provably name the staged inode (it names a window plant, it vanished
// again before the reverify could run, or the lookup was indeterminate).
// On the POSIX legs the recovery loop restages the genuine bytes and
// republishes into proven absence before ever returning this class;
// reaching the caller means the bounded budget ran out or the leg cannot
// recover (see ErrPublishStagedExhausted, and the Windows leg's documented
// refusal-only posture). Callers keep the source backup: nothing may be
// consumed after this error.
var ErrPublishStagedIdentityBreak = errors.New("destination does not provably name the staged inode after publish")

// ErrPublishStagedForeignOccupant means a destination occupant could NOT be
// tied to this operation's own published object, so recovery is refused
// with the occupant preserved byte-intact, NOTHING consumed, and the caller
// retaining its backup (wave-38, codex P2, PR#215 finding F1): the
// post-publish SUCCESS→mismatch occupant (a successful no-replace publish
// proved the destination free at the rename instant, so anything there
// arrived afterwards — possibly the staged-name plant the publish moved
// over, possibly a legitimate file created inside the publish→reverify
// gap; indistinguishable, both preserved). Always joined with
// ErrPublishStagedIdentityBreak so the whole refusal family stays reachable
// through the identity-break classifiers.
var ErrPublishStagedForeignOccupant = errors.New("destination occupant is neither the recorded window plant nor the staged inode")

// ErrPublishStagedIdentityIndeterminate means a post-publish destination
// lookup could not PROVE which object the destination names (wave-32, codex
// local review round 2, PR#215 finding R5): the (pre-r12) ENOSYS
// deferred-times legs glimpsed the published name again before/after the
// name-based Chtimes, and a failed glimpse used to degrade to a nil-identity
// success that callers classified as "no provable identity"; it is NOT
// safe. r12 removed those legs with the name-based fallback itself (the
// publish completes with the times skipped instead), so no production
// producer remains; the class stays part of the exported refusal vocabulary
// so callers' identity-break classifiers keep their shape. Always joined
// with ErrPublishStagedIdentityBreak so identity-break classifiers catch it.
var ErrPublishStagedIdentityIndeterminate = errors.New("post-publish destination identity is indeterminate")

// ErrPublishStagedExhausted means a directory writer kept substituting the
// staged name across the whole bounded re-stage/re-publish budget. No
// destination occupant was ever displaced (proven-absence republishes
// only), the genuine inode stayed reachable via the handle until the final
// attempt's close, and the caller's conservative legs retain the backup.
// Always joined with ErrPublishStagedIdentityBreak so identity-break
// classifiers catch the whole refusal family.
var ErrPublishStagedExhausted = errors.New("staged-name substitution outlasted the bounded republish budget")

// PublishStagedBoundAttempts bounds the POSIX re-stage/re-publish loop: the
// initial publish plus re-publishes after proven substitutions. Foreign
// churn beyond the bound fails typed rather than spinning against an
// adversarial name. Exported for tests and the godoc-facing contract.
const PublishStagedBoundAttempts = 3

// publishStagedBoundRestream is the re-stage seek+copy, exposed as a test
// seam (same discipline as stagedHandleChmod / renameNoReplaceKernel): a
// healthy descriptor's lseek/read cannot be failed on demand by tests, so
// the failure leg is replayed here.
var publishStagedBoundRestream = func(src afero.File, dst io.Writer) error {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err := io.Copy(dst, src)
	return err
}

// publishStagedBoundDestLstat is the post-publish destination lookup, exposed
// as a package-level seam (same discipline as stagedHandleChtimes /
// publishStagedBoundRestream): production wiring is os.Lstat — the os gating
// callers already proved the real OsFs, and Lstat never follows a plant —
// while tests replay the INDETERMINATE-lookup leg through it. No portable
// setup denies a directory lookup deterministically for BOTH uid 0 and an
// ordinary user, and codecov/CI may run as root: chmod-based denial (the
// pre-wave-24 test shape) silently succeeds for root and left this leg
// uncovered offline and the test failing on root CI hosts.
var publishStagedBoundDestLstat = os.Lstat

// StagedPublish carries one PublishStagedBound invocation. It subsumes the
// pre-wave-30 staging tail (identity proof, times landing, handle close,
// publish) for every caller, so the verify/publish/re-verify discipline
// cannot drift between history restore, history re-arm, and the downloader
// rollback.
type StagedPublish struct {
	// FS is the caller's filesystem. The identity binding runs only when FS
	// is the real OsFs AND Handle is its native descriptor (osStagingHandle):
	// wrapper filesystems must observe path operations and virtual
	// filesystems have no rename-away threat model; those take the
	// pre-wave-30 tail (CloseStaged + publish) unchanged.
	FS afero.Fs
	// Publish is the caller's path-based publish (fsutil.ReplaceFile or
	// fsutil.PublishNoReplace at every production site).
	Publish func(afero.Fs, string, string) error
	// NoReplace records that Publish has no-replace semantics AND the caller
	// proved the destination absent by classification. Only then does the
	// wave-38 post-publish preservation rule apply (codex P2, PR#215 finding
	// F1): a SUCCESS→mismatch occupant is never displaced (it provably
	// arrived after the kernel-proven-free rename — plant and legitimate gap
	// file are indistinguishable there) and recovery continues only into
	// proven absence. Wave-49 (codex P2, PR#215): a publish attempt that
	// FAILS with ErrPublishCollision is never displaced either — the
	// collision winner's bytes prevail and the refusal surfaces verbatim for
	// the caller's reclassification. Replace-style publishes re-stage and
	// republish OVER the plant instead (replacing destination bytes is their
	// operation's meaning).
	NoReplace bool
	// Staged/Handle are the O_EXCL-created staging name and its open
	// descriptor (CreateExclusiveStagingFile, wave-30 O_RDWR), Dest the
	// publish target.
	Staged string
	Handle afero.File
	Dest   string
	// Atime/Mtime/ApplyTimes mirror CloseStaged's times contract: they land
	// THROUGH THE OPEN HANDLE on the real OsFs before the publish. The
	// ENOSYS platforms (staging_times_unixother.go) have no fd-scoped
	// primitive, so the times are SKIPPED there (r12: the pre-r12
	// name-based Chtimes onto the published Dest kept an identity re-proof
	// →utimens window in which a directory writer's substitute — a planted
	// symlink included — would receive the stamp); the identity-verified
	// publish instead surfaces ErrPublishCompleted with the times unapplied.
	Atime, Mtime time.Time
	ApplyTimes   bool
	// Suffix + NextOrdinal name fresh O_EXCL staging files when the loop
	// re-stages from the handle after a proven substitution; callers pass
	// their existing per-flow suffix (".rstr" / ".dlrstr" / the re-arm
	// suffix) and process-local atomic nonce.
	Suffix      string
	NextOrdinal func() uint64
}

// PublishStagedBound runs p's staging tail with the staged identity bound
// ACROSS the publish (see the package-facing comment at the top of this
// file). On return the handle is ALWAYS closed (never leaked on POSIX
// error legs, always released after a successful publish) and exactly one
// of these classes holds:
//
//   - nil: the destination provably names the caller's staged inode
//     (POSIX) or the publish succeeded with the wave-29 proof held up to
//     the close (virtual/Windows legs);
//   - an error wrapping ErrPublishStagedVerify: no publish attempted; the
//     staged name is unproven — callers MUST NOT remove it;
//   - a *StagingTimesError: the times leg failed; the caller's pre-wave-30
//     cleanup (remove the ORIGINAL staged name) applies — that name was
//     proven ours at loop entry a microsecond earlier;
//   - an error wrapping ErrPublishStagedClose: close failed pre-publish
//     (virtual/Windows legs); same cleanup posture as the times leg;
//   - the publish's own error, returned verbatim: every wave-15/17/20/29
//     publish classifier (PublishRefusal / PublishCompleted /
//     ErrPublishNoReplaceLinkFailed ...) keeps working through the caller's
//     existing wraps;
//   - an error wrapping ErrPublishStagedIdentityBreak /
//     ErrPublishStagedExhausted / ErrPublishStagedForeignOccupant /
//     ErrPublishStagedIdentityIndeterminate: a
//     substitution was proven after a successful publish and recovery was
//     refused or exhausted — or (wave-38) a destination occupant could not
//     be tied to pre-publish existence evidence, so the foreign bytes were
//     preserved instead; NOTHING was consumed and the caller retains its
//     backup.
//
// In every error class except the pre-publish verify failure AND the
// ErrPublishCompleted-carrying classes (wave-34, codex local review round 4,
// PR#215 finding F3) the caller's historical "remove the original staged
// name" cleanup stays safe: that name is either still ours or already
// consumed by the publish — and where a foreign plant sits on it, removing
// it discards attacker junk (never genuine bytes: those live on the handle
// until close). The publish's OWN ErrPublishCompleted-carrying error (the
// POSIX hard-link fallback's staged-cleanup refusal, wave-33's
// ErrPublishNoReplaceStagedUnverified, the wave-20 cleanup+rollback
// failure leg, or r12's ENOSYS leg on an identity-
// VERIFIED destination — the times are refused onto the published name
// with the publish already landed, wave-60's completed classification) is
// the exception: the
// staged name was DELIBERATELY left in place and may address a foreign
// object, so callers must check
// errors.Is(err, ErrPublishCompleted) BEFORE any staged removal — the
// destination provably carries the published bytes regardless.
func PublishStagedBound(p StagedPublish) error {
	_, err := PublishStagedBoundInfo(p)
	return err
}

// PublishStagedBoundInfo is PublishStagedBound with the PUBLISHED object's
// identity handed back (wave-31, codex local round 1, PR#215 findings
// L1/L2): a restore/rollback caller must revalidate that its destination
// still names the object the publish landed BEFORE it deletes its source
// backup or consumes the journal — a foreign writer using the
// publish→remove window otherwise gets the last recoverable copy destroyed.
//
// On a proven publish the returned FileInfo is the destination's own
// post-publish stat, os.SameFile-bound to the staged inode:
//
//   - POSIX legs hand back the reverify lookup. r12 (codex P2 — "keep
//     deferred timestamps bound to the published inode"): the ENOSYS
//     deferred-times leg NO LONGER EXISTS — a platform without an
//     fd-scoped times primitive (staging_times_unixother.go) skips the
//     times entirely instead of ever touching the published name (its
//     identity re-proof→Chtimes window could have stamped a substitute,
//     symlink chase included). The verified publish still surfaces
//     wave-60's completed classification (ErrPublishCompleted, NIL
//     identity): destination bytes proven, foreign bytes AND foreign
//     metadata untouched. The whole wave-32 foreign-occupant/indeterminate
//     refusal family of the name-based legs is gone with them — no
//     post-reverify destination lookup runs at all;
//   - the Windows leg hands back its post-publish reverify stat;
//   - the VIRTUAL leg (wrapper/MemMap filesystems — no rename-away threat
//     model, no handle identity) reports nil with the publish's own error
//     result: a nil info with a nil error means the publish succeeded but
//     there is no provable identity to revalidate against, and the caller
//     keeps its documented pre-wave-31 residual posture for that leg
//     instead of either trusting or refusing on nothing.
//
// Every error class is exactly PublishStagedBound's — the identity is nil
// on any failure.
func PublishStagedBoundInfo(p StagedPublish) (os.FileInfo, error) {
	if _, ok := osStagingHandle(p.FS, p.Handle); !ok {
		// Virtual/wrapper leg: the exact pre-wave-30 tail — fd-hardness does
		// not exist here, mem handles re-stamp at Close, and the path
		// publish must still run by name.
		if err := CloseStaged(p.FS, p.Staged, p.Handle, p.Atime, p.Mtime, p.ApplyTimes); err != nil {
			var timesErr *StagingTimesError
			if errors.As(err, &timesErr) {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %s: %w", ErrPublishStagedClose, p.Staged, err)
		}
		return nil, p.Publish(p.FS, p.Staged, p.Dest)
	}
	return publishStagedBoundOS(p)
}
