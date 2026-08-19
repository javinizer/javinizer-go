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
//     away and we still hold its handle — so the foreign destination is
//     displaced ONLY for publishes into a proven-absent destination (the
//     caller's classification + the no-replace primitive together prove
//     the published bytes are the window plant, never pre-existing data),
//     and the genuine bytes are re-staged FROM THE HANDLE (seek 0, copy
//     into a fresh O_EXCL staging name) and republished under a BOUNDED
//     attempt budget;
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
// On the POSIX legs the recovery loop displaces the plant and republishes
// before ever returning this class; reaching the caller means the bounded
// budget ran out or the leg cannot recover (see ErrPublishStagedExhausted,
// and the Windows leg's documented refusal-only posture). Callers keep the
// source backup: nothing may be consumed after this error.
var ErrPublishStagedIdentityBreak = errors.New("destination does not provably name the staged inode after publish")

// ErrPublishStagedExhausted means a directory writer kept substituting the
// staged name across the whole bounded re-stage/re-publish budget. Every
// displaced plant was foreign (proven-absent destinations only — never
// pre-existing bytes), the genuine inode stayed reachable via the handle
// until the final attempt's close, and the caller's conservative legs
// retain the backup. Always joined with ErrPublishStagedIdentityBreak so
// identity-break classifiers catch the whole refusal family.
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
	// proved the destination absent by classification. Only then may a
	// published-but-mismatched destination be displaced: the publish only
	// succeeds into absence, so a mismatched occupant is necessarily the
	// window plant this operation itself installed — never pre-existing
	// bytes. Replace-style publishes re-stage and republish OVER the plant
	// instead (replacing destination bytes is their operation's meaning).
	NoReplace bool
	// Staged/Handle are the O_EXCL-created staging name and its open
	// descriptor (CreateExclusiveStagingFile, wave-30 O_RDWR), Dest the
	// publish target.
	Staged string
	Handle afero.File
	Dest   string
	// Atime/Mtime/ApplyTimes mirror CloseStaged's times contract: they land
	// THROUGH THE OPEN HANDLE on the real OsFs before the publish (the
	// ENOSYS platforms defer to a post-publish name-based Chtimes on Dest,
	// documented against staging_times_unixother.go).
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
//     ErrPublishStagedExhausted: a substitution was proven after a
//     successful publish and recovery was refused or exhausted; NOTHING was
//     consumed and the caller retains its backup.
//
// In every error class except the pre-publish verify failure the caller's
// historical "remove the original staged name" cleanup stays safe: that
// name is either still ours or already consumed by the publish — and where
// a foreign plant sits on it, removing it discards attacker junk (never
// genuine bytes: those live on the handle until close).
func PublishStagedBound(p StagedPublish) error {
	if _, ok := osStagingHandle(p.FS, p.Handle); !ok {
		// Virtual/wrapper leg: the exact pre-wave-30 tail — fd-hardness does
		// not exist here, mem handles re-stamp at Close, and the path
		// publish must still run by name.
		if err := CloseStaged(p.FS, p.Staged, p.Handle, p.Atime, p.Mtime, p.ApplyTimes); err != nil {
			var timesErr *StagingTimesError
			if errors.As(err, &timesErr) {
				return err
			}
			return fmt.Errorf("%w: %s: %w", ErrPublishStagedClose, p.Staged, err)
		}
		return p.Publish(p.FS, p.Staged, p.Dest)
	}
	return publishStagedBoundOS(p)
}
