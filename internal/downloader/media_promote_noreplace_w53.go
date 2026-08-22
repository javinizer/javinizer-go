package downloader

import (
	"errors"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
)

// POSTER-WRITE-HARDENING wave-53 (codex P2, PR#215 finding 2 + finding 3) —
// the non-overwrite poster promote's last unprovenanced publish surface is
// closed. The pre-wave-53 legacy leg published the crop/write candidate by
// name with no provenance binding: a directory writer rotating a foreign
// substitute onto the candidate name inside the crop/write→promote window had
// its bytes published onto the (proven-absent) destination and reported as a
// successful download — the candidate name was never re-proven against the
// object downloadPoster just produced. promotePosterCandidateNoReplace binds
// the candidate's validated-handle provenance (the same
// bindCandidateProvenance discipline the overwrite install uses) and routes
// the publish through the wave-29/30 bound-publish family, so a substitute is
// refused (errStagedInputSubstituted) before any bytes-at-dest mutation. The
// both-fail refusal (finding 3) fails closed here too: when the path identity
// capture AND the no-follow re-open both fail the candidate is completely
// unprobeable — never publish unauthenticated, nothing recorded or touched,
// candidate preserved byte-intact for manual cleanup.

// promotePosterCandidateOutcome classifies the non-overwrite promote's
// outcome for downloadPoster's bookkeeping (which scratch names to reap vs
// retain, and whether the download reports success or a typed refusal).
type promotePosterCandidateOutcome int

const (
	// promotePosterCandidateSucceeded: the candidate published cleanly onto
	// the proven-absent destination — a completed download.
	promotePosterCandidateSucceeded promotePosterCandidateOutcome = iota
	// promotePosterCandidateCollision: a foreign writer claimed the
	// destination inside the download→promote window — keep the existing
	// artwork (non-overwrite mode), racer's bytes preserved. The candidate
	// temp is reaped by the caller's deferred cleanup.
	promotePosterCandidateCollision
	// promotePosterCandidateCompleted: the publish completed despite the
	// returned error (the POSIX hard-link fallback's staged-cleanup refusal
	// / wave-20 cleanup+rollback failure) — the destination provably carries
	// the candidate bytes; the possibly-foreign candidate name is retained
	// for manual cleanup (the wave-42 discipline).
	promotePosterCandidateCompleted
	// promotePosterCandidateRetained: the candidate name is possibly foreign
	// (a proven substitution refused before publish, or the both-fail
	// unprobeable refusal) — preserve it byte-intact for manual cleanup and
	// surface the typed refusal. Nothing was published onto the destination.
	promotePosterCandidateRetained
	// promotePosterCandidateFailed: a plain publish failure (not a collision,
	// not completed, not a substitution) — the candidate is provably ours, so
	// the caller's deferred cleanup reaps it; the typed error surfaces.
	promotePosterCandidateFailed
)

// promotePosterCandidateNoReplace binds the downloadPoster crop/write
// candidate's validated-handle provenance and publishes it onto destPath
// through the NoReplace bound publish (wave-53, finding 2). The bound publish
// re-proves the candidate name against the handle's fd identity at publish
// adjacency, so a substitute rotated onto it inside the crop/write→promote
// window is refused (errStagedInputSubstituted) instead of being published
// unprovenanced; NoReplace=true keeps a racer's destination bytes byte-intact
// on collision (the pre-wave-53 posture). The degraded (recorded-only)
// posture — a Lstat-known candidate whose handle could not be opened/fstat'd —
// keeps the wave-47 plain no-replace publish residual (the snapshot gate
// guards the overwrite install's bound legs; this leg has no snapshot gate of
// its own, matching the legacy posture). The both-fail refusal (finding 3)
// fails closed. The outcome tells the caller which scratch names to reap vs
// retain and whether the download reports success or a typed refusal; the
// error is non-nil exactly for the retained + plain-failure legs.
// Wave-66 (codex P2, PR#215 — bind the candidate to the PRODUCER'S identity):
// the caller hands the crop/write producer's write-time identity record down
// (wave-67: downloadPoster's full-download record filed on the result, or
// the crop producers' returned post-write FileInfo) and
// the bind authenticates its install-time Lstat AND the re-opened fd's fstat
// against THAT record — a substitute rotated onto the candidate name between
// the producer write and this promote's bind no longer authenticates against
// itself; the refusal rides the same typed Retained leg.
func promotePosterCandidateNoReplace(fs afero.Fs, candidate, destPath string, producer installedDestIdentity) (promotePosterCandidateOutcome, error) {
	prov, provErr := bindCandidateProvenanceFn(fs, candidate, producer)
	if provErr != nil {
		if errors.Is(provErr, errStagedInputSubstituted) {
			// Wave-66: the candidate name provably stopped naming the
			// producer-written object between the crop/write and this bind —
			// same retained-substitute posture as the publish-time
			// substitution refusal below, reached before any publish ran.
			logging.Warnf("downloadPoster: promote of %s onto %s refused — candidate name %s no longer names the crop/write-produced object (foreign substitution between the crop/write and the promote-time bind); substitute preserved, destination untouched, manual cleanup advised", candidate, destPath, candidate)
		} else {
			// Finding 3: both the path identity capture and the no-follow
			// re-open failed — the candidate is completely unprobeable. Fail closed.
			logging.Warnf("downloadPoster: promote of %s onto %s refused — candidate %s could not be proven (path identity capture and no-follow re-open both failed); refusing to publish unauthenticated, destination untouched, candidate preserved for manual cleanup", candidate, destPath, candidate)
		}
		return promotePosterCandidateRetained, provErr
	}
	var rerr error
	if prov.handle == nil {
		// Wave-54 (finding 3): a merely-recorded pathname is never published by
		// mutation — the no-follow re-open failed, so the candidate is unproven;
		// refuse typed (never publish unauthenticated by name), candidate preserved.
		rerr = fmt.Errorf("candidate %s could not be bound to a publish handle (no-follow re-open failed) — never publish by mutation of a merely-recorded pathname: %w", candidate, errStagedInputSubstituted)
	}
	if rerr == nil {
		// Bound publish: the candidate name re-proves itself against the
		// handle's fd identity at publish adjacency; NoReplace=true keeps the
		// racer's bytes byte-intact on collision. publishStagedBoundFn (the
		// wave-48 production seam) always closes the handle; stagedPublishVerdict
		// maps the unproven/identity-break refusal family onto the typed
		// substitution refusal.
		_, pubErr := publishStagedBoundFn(fsutil.StagedPublish{
			FS:          fs,
			Publish:     fsutil.PublishNoReplace,
			NoReplace:   true,
			Staged:      candidate,
			Handle:      prov.handle,
			Dest:        destPath,
			Suffix:      ".dlpub",
			NextOrdinal: nextStagedRepublishOrdinal,
		})
		rerr = stagedPublishVerdict(pubErr)
		if rerr == nil {
			rerr = pubErr // collision / completed / plain failure ride through verbatim
		}
	}
	if rerr == nil {
		return promotePosterCandidateSucceeded, nil
	}
	if errors.Is(rerr, errStagedInputSubstituted) {
		logging.Warnf("downloadPoster: promote of %s onto %s refused — candidate name %s no longer names the crop/write-produced object (foreign substitution between crop and promote); substitute preserved, destination untouched, manual cleanup advised", candidate, destPath, candidate)
		return promotePosterCandidateRetained, rerr
	}
	if errors.Is(rerr, fsutil.ErrPublishCollision) {
		logging.Warnf("downloadPoster: promote of %s onto %s refused — a foreign writer claimed the destination inside the download window; keeping the existing artwork (non-overwrite mode), racer's bytes preserved", candidate, destPath)
		return promotePosterCandidateCollision, nil
	}
	if fsutil.PublishCompleted(rerr) {
		logging.Warnf("downloadPoster: promote of %s onto %s completed despite the returned error (%v) — the staged name could not be re-proven (possibly foreign) and is left in place; manual cleanup advised", candidate, destPath, rerr)
		return promotePosterCandidateCompleted, nil
	}
	logging.Warnf("downloadPoster: failed to promote %s: %v", candidate, rerr)
	return promotePosterCandidateFailed, fmt.Errorf("failed to finalize poster: %w", rerr)
}
