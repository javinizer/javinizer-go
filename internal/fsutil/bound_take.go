package fsutil

// POSTER-WRITE-HARDENING wave-38 (codex P2, PR#215 findings F2/F3/F4) — the
// generalized "no-replace take + identity-verify + bound unlink" sequence
// ("take-aside"), lifted from history's wave-26/32 quarantine construction
// (internal/history/replacement_backup_quarantine.go) so every helper that
// moves a verified object aside BEFORE a destructive pathname operation runs
// through ONE binding discipline:
//
//  1. the caller reserves a scratch sibling name O_EXCL and captures its
//     identity (each flow keeps its naming grammar + entropy seam);
//  2. TakeAside re-derives the reservation immediately before the move
//     (still THE claimed placeholder — never a foreign swap);
//  3. the moved object is re-proven AT the scratch name against the
//     caller's identity proof — a swap between the caller's pre-take
//     verification and the move moved a DIFFERENT object, and that object
//     is never acted on destructively;
//  4. the ONLY unlink ever issued (BoundAside.Unlink) targets the scratch
//     name and re-binds the object at syscall adjacency (no-follow Lstat,
//     dev/inode where exposed, then size + mtime) before fs.Remove runs.
//
// POSTER-WRITE-HARDENING wave-43 (codex P2, PR#215) — the take itself is now
// CONDITIONAL, lifting the wave-42 downloader/history quarantine handoff
// construction INTO the shared helper so every backup/quarantine flow
// inherits it: the wave-38 step-2 re-proof was followed by a REPLACE-AWARE
// rename of src onto the scratch name, so a plant landing between the verify
// and the move got its bytes overwritten (the post-move proof detected the
// substitution post-hoc, but the plant's bytes were already gone).
// ReplaceFile no longer moves src anywhere in this flow:
//
//  a. after the reservation re-proof the RESERVATION itself is taken aside:
//     a fresh unpredictable vacated name (scratch + ".vac." + crypto token)
//     is claimed O_EXCL with its own captured identity (the wave-38 claim
//     discipline) and released identity-bound, then the reservation object
//     moves scratch→vacatedName through a NO-REPLACE rename
//     (PublishNoReplace). A plant swapped in after the re-proof is what the
//     vacate moves — it arrives byte-intact (never overwritten), and the
//     post-vacate re-proof (vacatedName must still == the claim identity at
//     syscall adjacency) refuses it (ErrTakeAsideForeign) and rides it back
//     NO-REPLACE;
//  b. the scratch name is then provably FREE: src publishes onto it
//     NO-REPLACE (PublishNoReplace). A plant winning the vacate→publish gap
//     is the typed collision refusal — the plant is preserved, src never
//     moved, and the vacated reservation rides BACK onto scratch NO-REPLACE
//     (restoring the pre-call reservation state where the name is free,
//     released identity-bound when the ride-back lands, stranded
//     recoverable at the vacated name where the ride-back collides);
//  c. steps 3+4 above run unchanged at the scratch name;
//  d. the vacated reservation placeholder is removed ONLY after the vacated
//     name re-binds to the claim identity at syscall adjacency (the claimed
//     placeholder is the sole object this flow ever deletes there,
//     documented choice (a) of the wave-43 resolution) — a refused or
//     indeterminate cleanup keeps the occupant byte-intact with a warn and
//     the take stands.
//
// Every wedge compensation restores original names with NO-REPLACE moves
// (PublishNoReplace): a racer claiming the source name mid-wedge is never
// clobbered — the taken-aside object stays recoverable at the scratch name
// and the error classifies (ErrTakeAsideRestoreFailed). Whichever arm
// wedges, NO foreign bytes are ever removed and the operation's caller
// keeps its conservative failure posture.
//
// POSTER-WRITE-HARDENING wave-44 (codex P2, PR#215 finding F2) — the bound
// unlink itself loses its verify→Remove pathname window: the wave-38/43
// Unlink re-proved the scratch name and then unlinked it BY PATH (two
// syscalls a directory writer could interpose — a plant replacing the
// occupant in between had its foreign bytes deleted by OUR bound unlink).
// The unlink now runs the take's own vacate→verify→drop construction: the
// proven-held object moves a SECOND time onto a fresh crypto-claimed
// terminal name (claimed O_EXCL, released identity-bound, published
// NO-REPLACE), is re-bound to the held identity at the terminal name, and
// only THE TERMINAL NAME is unlinked. Every name an attacker can predict
// rides a re-proof; the terminal name's residual verify→Remove gap can be
// raced only by reclaiming the unpredictable crypto-token draw itself (the
// codex-accepted fresh-claimed-name terminal boundary, documented in
// Unlink). Any doubt — a substituted occupant, an indeterminate answer, a
// wedged terminal remove — retains every occupant and refuses typed,
// rewinding the terminal object onto the scratch name NO-REPLACE so a
// caller retry re-runs the whole bound construction.

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// ErrTakeAsideForeign classifies a taken-aside object (or a take-aside
// reservation) that failed its identity proof: a pathname swap between the
// caller's verification and the atomic move moved or parked a DIFFERENT
// object than the one the operation owns, and that object is preserved
// byte-intact — the destructive step never runs against it.
var ErrTakeAsideForeign = errors.New("take-aside object failed the identity proof — foreign bytes preserved")

// ErrTakeAsideVanished classifies the take-aside scratch name being empty at
// a binding instant where a proven object was expected (post-move re-proof
// or unlink-time re-derivation), or the just-vacated reservation name being
// empty at its post-vacate re-proof. The owned object vanished through a
// path this flow never unlinked — indeterminate, never a consumed removal.
var ErrTakeAsideVanished = errors.New("take-aside object vanished before its bound operation completed")

// ErrTakeAsideRestoreFailed classifies the wedge-compensation failure: the
// taken-aside object could NOT be moved back onto the source name
// NO-REPLACE, or the vacated reservation could not be moved back onto the
// scratch name NO-REPLACE (a foreign claimant holds the name, or the publish
// failed outright), so the target name is unowned while the object stays
// recoverable at the scratch / vacated name.
var ErrTakeAsideRestoreFailed = errors.New("take-aside compensation could not restore the source name no-replace")

// TakeAsideSpec carries one TakeAside invocation.
type TakeAsideSpec struct {
	// FS is the caller's filesystem (wedges and bindings all run through
	// its interface methods).
	FS afero.Fs
	// Src is the path whose currently named object is taken aside.
	Src string
	// Scratch is the sibling name the caller reserved O_EXCL (its naming
	// grammar + entropy source stay with the caller).
	Scratch string
	// Claim is the scratch reservation's captured identity (the open
	// handle's pre-close Stat, mirroring the quarantine claims): the take
	// re-proves the reservation against it immediately before the move, so
	// a foreign writer swapping the reservation away never gets its bytes
	// silently displaced by the replace-aware rename.
	Claim os.FileInfo
	// Prove re-binds the object the move landed at the scratch name to the
	// identity the caller verified at Src BEFORE the take. A nil answer
	// accepts the object; an error is wrapped (and, when the caller wraps
	// its own typed class inside, stays unwrap-reachable).
	Prove func(moved os.FileInfo) error
}

// asideLstat looks a name up WITHOUT following a final symlink wherever the
// filesystem exposes the distinction (same discipline as the downloader's
// lstatBackupCandidate).
func asideLstat(fs afero.Fs, name string) (os.FileInfo, error) {
	if ls, ok := fs.(afero.Lstater); ok {
		info, _, err := ls.LstatIfPossible(name)
		return info, err
	}
	return fs.Stat(name)
}

// asideSameObject reports whether cur — looked up at a binding instant —
// still names THE object expect described (mirroring the repo's quarantine
// gates): always regular and non-symlink, dev/inode compared only where BOTH
// sides expose them (virtual filesystems ride the shape/metadata legs), and
// size + mtime equal on every platform.
func asideSameObject(cur, expect os.FileInfo) bool {
	if cur == nil || expect == nil || cur.Mode()&os.ModeSymlink != 0 || !cur.Mode().IsRegular() {
		return false
	}
	if expectDev, expectIno, expectOK := boundObjectIdentity(expect); expectOK {
		if curDev, curIno, curOK := boundObjectIdentity(cur); curOK && (expectDev != curDev || expectIno != curIno) {
			return false
		}
	}
	return cur.Size() == expect.Size() && cur.ModTime().Equal(expect.ModTime())
}

// takeAsideVacClaimTries bounds the vacated-name draw loop; every collision
// or racing claimant costs one draw.
const takeAsideVacClaimTries = 64

// takeAsideVacRandReader is the entropy source behind the vacated-name
// token, exposed as a seam (same discipline as history's
// backupQuarantineRandReader): production is cryptographically random and
// the token carries no path or user data, while tests wedge the failure leg
// deterministically.
var takeAsideVacRandReader io.Reader = cryptorand.Reader

// claimTakeAsideVacName atomically reserves a fresh unpredictable vacated
// sibling name for the reservation vacate (wave-43): draw a token, claim
// scratch+".vac."+token O_CREATE|O_EXCL (the observation-to-claim race
// resolves in favor of the claim — os.IsExist re-draws), capture the
// reservation's own identity through the open handle's pre-close Stat
// (mirroring the quarantine-claim discipline). A reservation whose identity
// cannot be read is RETAINED for manual cleanup (wave-r19, finding F1 — the
// name's identity is unproven, so a pathname Remove could delete foreign
// bytes; nothing here mutates on doubt). A reservation whose close fails
// keeps its captured identity, and the cleanup is bound to it: the candidate
// is released identity-bound (releaseTakeAsideVacClaim — SameFile at unlink
// adjacency, retain on doubt, never a pathname Remove of an unproven object).
func claimTakeAsideVacName(fs afero.Fs, scratch string) (string, os.FileInfo, error) {
	for attempt := 0; attempt < takeAsideVacClaimTries; attempt++ {
		var token [16]byte
		if _, err := io.ReadFull(takeAsideVacRandReader, token[:]); err != nil {
			return "", nil, fmt.Errorf("vacated-name token for %s: %w", scratch, err)
		}
		candidate := scratch + ".vac." + hex.EncodeToString(token[:])
		reservation, rerr := fs.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		switch {
		case rerr == nil:
			info, serr := reservation.Stat()
			if serr != nil {
				// Wave-r19 (codex P2, PR#215 finding F1, mirroring
				// claimBackupQuarantineName's wave-62 fix): the name's
				// identity is UNPROVEN — between our O_EXCL create and
				// now another writer may have replaced it, so unlinking
				// the path could delete foreign bytes. Retain it for
				// manual cleanup (the name stays claimed and visible;
				// nothing here mutates on doubt).
				_ = reservation.Close()
				logging.Warnf("take-aside vacated reservation %s left in place — its identity could not be proven (%v); manual cleanup advised", candidate, serr)
				return "", nil, fmt.Errorf("stat take-aside vacated reservation %s: %w", candidate, serr)
			}
			if cerr := reservation.Close(); cerr != nil {
				// The reservation's identity WAS captured (info). Wave-r19
				// (codex P2, PR#215 finding F1 — the history twin): bind
				// the cleanup to the captured identity — re-prove the
				// candidate still names our claimed placeholder (SameFile
				// at unlink adjacency) and unlink only when matching;
				// retain on doubt (never a pathname Remove of an
				// unproven object).
				if relErr := releaseTakeAsideVacClaim(fs, candidate, info); relErr != nil {
					logging.Warnf("take-aside vacated reservation %s close-failure cleanup refused — the occupant no longer provably names our claimed placeholder (%v); left byte-intact for manual cleanup", candidate, relErr)
				}
				return "", nil, fmt.Errorf("close take-aside vacated reservation %s: %w", candidate, cerr)
			}
			return candidate, info, nil
		case os.IsExist(rerr):
			continue // a racer claimed this draw first — draw again
		default:
			return "", nil, fmt.Errorf("reserve take-aside vacated name %s: %w", candidate, rerr)
		}
	}
	return "", nil, fmt.Errorf("take-aside vacated names exhausted for %s after %d attempts", scratch, takeAsideVacClaimTries)
}

// releaseTakeAsideVacClaim frees the just-claimed vacated name for the
// no-replace vacate rename — identity-bound: only our own claimed
// placeholder is ever unlinked here. A name that vanished on its own is
// already free; a foreign swap inside the claim→release window is preserved
// byte-intact and refuses the take with NOTHING relocated (the reservation
// stays claimed at the scratch name, the caller's src untouched); a wedged
// unlink refuses likewise with our own placeholder retained.
func releaseTakeAsideVacClaim(fs afero.Fs, vacName string, vacClaim os.FileInfo) error {
	// Codex P2 (wave-58): the vacated claim's own verify-then-remove window
	// is closed by re-binding the vacated name twice at syscall adjacency —
	// the fresh proof must equal BOTH the claim record and the head-of-call
	// proof — before the terminal unlink. A swap in any narrow interval
	// preserves the occupant byte-intact (typed refusal). This is the exact
	// converse of Unlink's internal chain (which calls back into this
	// helper); the release terminal CANNOT ride Unlink or the recursion
	// would never bottom out.
	cur, lerr := asideLstat(fs, vacName)
	switch {
	case os.IsNotExist(lerr):
		return nil
	case lerr != nil:
		return fmt.Errorf("inspect the take-aside vacated claim %s before its release: %w", vacName, lerr)
	case !asideSameObject(cur, vacClaim):
		return fmt.Errorf("take-aside vacated claim %s no longer names our claimed placeholder — foreign bytes preserved: %w", vacName, ErrTakeAsideForeign)
	}
	repro, rerr := asideLstat(fs, vacName)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return nil
		}
		return fmt.Errorf("re-prove the vacated claim %s before its unlink: %w", vacName, rerr)
	}
	if !asideSameObject(repro, vacClaim) || !asideSameObject(repro, cur) {
		return fmt.Errorf("take-aside vacated claim %s changed identity between the proof and the unlink — foreign bytes preserved: %w", vacName, ErrTakeAsideForeign)
	}
	if rerr := fs.Remove(vacName); rerr != nil && !os.IsNotExist(rerr) {
		return fmt.Errorf("remove the take-aside vacated claim %s (verified ours twice at syscall adjacency): %w", vacName, rerr)
	}
	return nil
}

// dropVacatedReservation is the wave-43 success-path cleanup (the resolving
// construction's documented choice): the vacated reservation — the claimed
// placeholder, the ONLY object this flow ever removes at the vacated name —
// is unlinked ONLY after the vacated name re-binds to the claim identity at
// syscall adjacency (asideSameObject = the established dev/inode + size +
// mtime SameFile discipline). A refused or indeterminate binding (foreign
// swap, wedged lookup) keeps the occupant byte-intact with a warn — the
// take itself already stands; a name that vanished on its own completed the
// cleanup by itself.
func dropVacatedReservation(fs afero.Fs, vacName string, claim os.FileInfo) {
	cur, lerr := asideLstat(fs, vacName)
	switch {
	case os.IsNotExist(lerr):
		return
	case lerr != nil || !asideSameObject(cur, claim):
		logging.Warnf("take-aside vacated reservation cleanup of %s refused — the occupant no longer provably names the claimed placeholder; left byte-intact for manual cleanup", vacName)
		return
	}
	// F2 (codex P2, PR#215): the final Remove ran by pathname after the
	// identity check — a swap in the verify→Remove gap deleted the
	// replacement. Route the removal through the wave-r19 conditional
	// construction (UnlinkVerified — same discipline as the history/
	// downloader quarantine twins): the proven object vacates onto a fresh
	// crypto-claimed terminal sibling, the terminal re-binds to the claim
	// identity at syscall adjacency, and only the terminal — provably the
	// claimed placeholder — is unlinked. Never a pathname Remove of an
	// unproven object; a plant swapped onto the vacated name after the
	// re-proof rides the no-replace vacate onto the terminal, fails the
	// rebind, and rewinds byte-intact. A name that vanished on its own
	// completed the cleanup by itself; any other doubt retains the
	// occupant byte-intact with a warn (the take itself already stands).
	if uerr := UnlinkVerified(fs, vacName, claim); uerr != nil {
		if errors.Is(uerr, ErrTakeAsideVanished) {
			return // the placeholder vanished on its own — cleanup done by itself
		}
		logging.Warnf("take-aside vacated reservation %s could not be bound-unlinked after a successful take (%v) — occupant left byte-intact for manual cleanup", vacName, uerr)
	}
}

// dropClaimedReservation re-proves the object at name against its claim and
// unlinks ONLY it — the wave-38 failed-move leg's re-prove-then-drop
// posture: on any doubt (vanished, indeterminate, mismatched) the name is
// retained and the surfacing error stands alone.
func dropClaimedReservation(fs afero.Fs, name string, claim os.FileInfo) {
	if cur, lerr := asideLstat(fs, name); lerr == nil && asideSameObject(cur, claim) {
		_ = fs.Remove(name)
	}
}

// vacRestoreOrDrop runs the wave-43 wedge compensation for the conditional
// handoff's failure legs: the vacated object (our reservation placeholder —
// or the plant a refusal preserved) rides BACK onto the scratch name
// NO-REPLACE, restoring the pre-vacate reservation state where the name is
// free; when the ride-back lands, the restored reservation is dropped
// re-proven (dropClaimedReservation), keeping the caller-visible failure
// shape byte-identical to the wave-38 failed-move leg — the scratch name
// ends free or foreign, never this helper's own litter. A ride-back
// collision (or indeterminate classification) leaves BOTH occupants
// byte-intact and joins ErrTakeAsideRestoreFailed — our own placeholder
// stays recoverable at the vacated name.
func vacRestoreOrDrop(fs afero.Fs, scratch, vacName string, claim os.FileInfo, err error) error {
	if back := PublishNoReplace(fs, vacName, scratch); back != nil {
		return errors.Join(err, fmt.Errorf("%w: scratch %s occupied or indeterminate — the vacated object stays recoverable at %s: %v", ErrTakeAsideRestoreFailed, scratch, vacName, back))
	}
	dropClaimedReservation(fs, scratch, claim)
	return err
}

// BoundAside carries a TakeAside hold: the moved object sits at the scratch
// name until the caller either finishes with Unlink or compensates with
// Restore.
type BoundAside struct {
	fs      afero.Fs
	src     string
	scratch string
	held    os.FileInfo // the re-proven object at the scratch name
	moved   bool        // the moved object currently sits at the scratch name
}

// Scratch returns the reserved scratch name (for diagnostics/logging).
func (h *BoundAside) Scratch() string { return h.scratch }

// TakeAside runs steps 2+3 of the take-aside sequence through the wave-43
// conditional handoff (see the package doc above): the reservation is
// re-proven against Claim, the reservation ITSELF vacates onto a fresh
// claimed vacated name NO-REPLACE (re-proven == Claim there), src publishes
// onto the provably-free scratch name NO-REPLACE, the moved object is
// re-proven at the scratch name through Prove, and the vacated placeholder
// is removed claim-bound. On success the returned hold names the re-proven
// object (held).
//
// Wedge legs:
//   - a reservation that no longer names Claim (foreign swap) or an
//     indeterminate reservation lookup: nothing was moved — typed
//     ErrTakeAsideForeign / plain wrapped error, foreign bytes preserved;
//   - a failed vacated-name claim, claim release, or vacate: nothing was
//     relocated — the still-claimed scratch reservation is dropped
//     best-effort re-proven (the wave-38 failed-move leg's posture), so the
//     scratch name ends free or foreign, never this helper's own litter;
//   - the reservation VANISHING before its vacate (ENOENT from the
//     no-replace vacate): indeterminate refusal — nothing was relocated,
//     the scratch name is provably free;
//   - a vacate collision/failure: nothing relocated (no-replace), src, the
//     reservation, and any plant all preserved in place;
//   - the vacated name VANISHED post-vacate: ErrTakeAsideVanished — the
//     placeholder went through a path this flow never unlinked; src never
//     moved and nothing sits at the vacated name to move back;
//   - the vacated object failing the claim re-proof (a plant moved by the
//     vacate) or an indeterminate post-vacate lookup: the object rides back
//     onto scratch NO-REPLACE byte-intact (typed ErrTakeAsideForeign / plain
//     wrapped error; a ride-back collision JOINS ErrTakeAsideRestoreFailed
//     and strands only our own placeholder at the vacated name);
//   - the src publish refusing (collision = typed, plant preserved; src
//     never moved): the reservation rides back + is released identity-bound,
//     or stays recoverable at the vacated name with the joined
//     ErrTakeAsideRestoreFailed;
//   - post-move scratch VANISHED: the owned object went through a path this
//     flow never unlinked — ErrTakeAsideVanished, no object compensation
//     (nothing sits at the scratch name to move back); the vacated
//     reservation still rides back + drops re-proven where free;
//   - post-move indeterminate lookup or Prove refusal: the object moves
//     BACK onto src NO-REPLACE first (restoring the caller's pre-call
//     names); a failed move-back JOINS ErrTakeAsideRestoreFailed.
func TakeAside(spec TakeAsideSpec) (*BoundAside, error) {
	reservation, rerr := asideLstat(spec.FS, spec.Scratch)
	switch {
	case rerr != nil:
		return nil, fmt.Errorf("inspect take-aside reservation %s before the move: %w", spec.Scratch, rerr)
	case !asideSameObject(reservation, spec.Claim):
		return nil, fmt.Errorf("take-aside reservation %s no longer names the claimed placeholder — foreign reservation swap: %w", spec.Scratch, ErrTakeAsideForeign)
	}
	// Wave-43 (a): claim + free a fresh vacated name, then take the
	// RESERVATION itself aside onto it NO-REPLACE — whatever the scratch name
	// holds at this instant rides over byte-intact and is re-proven against
	// the claim immediately (a plant swapped in after the re-proof is refused,
	// never overwritten).
	vacName, vacClaim, cerr := claimTakeAsideVacName(spec.FS, spec.Scratch)
	if cerr != nil {
		// Nothing relocated — the still-claimed scratch reservation is
		// dropped best-effort (the wave-38 failed-move leg's posture).
		dropClaimedReservation(spec.FS, spec.Scratch, spec.Claim)
		return nil, fmt.Errorf("reserve the take-aside vacated name for %s: %w", spec.Scratch, cerr)
	}
	if relErr := releaseTakeAsideVacClaim(spec.FS, vacName, vacClaim); relErr != nil {
		dropClaimedReservation(spec.FS, spec.Scratch, spec.Claim)
		return nil, relErr
	}
	if vacErr := PublishNoReplace(spec.FS, spec.Scratch, vacName); vacErr != nil {
		if os.IsNotExist(vacErr) {
			// The reservation vanished between the re-proof and the vacate —
			// indeterminate refusal; this flow relocated nothing.
			return nil, fmt.Errorf("take-aside reservation %s vanished before its no-replace vacate: %w", spec.Scratch, vacErr)
		}
		// Collision at the vacated name (a plant) or a hard failure:
		// no-replace relocated NOTHING — src, the reservation, and the plant
		// are all preserved in place, and the still-claimed reservation
		// drops best-effort re-proven before the refusal surfaces.
		dropClaimedReservation(spec.FS, spec.Scratch, spec.Claim)
		return nil, fmt.Errorf("take-aside vacate of the reservation %s onto %s refused — occupant preserved byte-intact: %w", spec.Scratch, vacName, vacErr)
	}
	// Wave-43 (2): the vacated name must address the claim identity at
	// syscall adjacency (dev/inode where exposed, size + mtime always —
	// asideSameObject is the established SameFile binding).
	vacated, verr := asideLstat(spec.FS, vacName)
	switch {
	case os.IsNotExist(verr):
		// The vacated placeholder vanished unownably right after the vacate
		// — indeterminate: src never moved and nothing sits at the vacated
		// name to move back.
		return nil, fmt.Errorf("%w: %s (vacated reservation empty at the post-vacate re-proof)", ErrTakeAsideVanished, vacName)
	case verr != nil:
		return nil, vacRestoreOrDrop(spec.FS, spec.Scratch, vacName, spec.Claim,
			fmt.Errorf("inspect the vacated reservation %s after the vacate: %w", vacName, verr))
	case !asideSameObject(vacated, spec.Claim):
		return nil, vacRestoreOrDrop(spec.FS, spec.Scratch, vacName, spec.Claim,
			fmt.Errorf("take-aside vacated a foreign plant from %s, not the claimed placeholder — plant preserved byte-intact: %w", spec.Scratch, ErrTakeAsideForeign))
	}
	// Wave-43 (b): the scratch name is provably FREE — publish src onto it
	// NO-REPLACE. A plant winning the vacate→publish gap is the typed
	// refusal: the plant is preserved, src never moved, and the reservation
	// rides back onto scratch NO-REPLACE (vacRestoreOrDrop).
	if pubErr := PublishNoReplace(spec.FS, spec.Src, spec.Scratch); pubErr != nil {
		return nil, vacRestoreOrDrop(spec.FS, spec.Scratch, vacName, spec.Claim,
			fmt.Errorf("take-aside publish of %s onto the freed scratch %s refused — occupant preserved byte-intact: %w", spec.Src, spec.Scratch, pubErr))
	}
	hold := &BoundAside{fs: spec.FS, src: spec.Src, scratch: spec.Scratch, moved: true}
	moved, merr := asideLstat(spec.FS, spec.Scratch)
	switch {
	case os.IsNotExist(merr):
		// The moved object vanished unownably right after the move —
		// indeterminate, nothing sits at the scratch name to move back.
		hold.moved = false
		return nil, vacRestoreOrDrop(spec.FS, spec.Scratch, vacName, spec.Claim,
			fmt.Errorf("%w: %s (scratch empty at the post-move re-proof)", ErrTakeAsideVanished, spec.Scratch))
	case merr != nil:
		return nil, hold.restoreOrJoinVac(vacName, spec.Claim, fmt.Errorf("inspect take-aside object %s after the move: %w", spec.Scratch, merr))
	}
	if perr := spec.Prove(moved); perr != nil {
		return nil, hold.restoreOrJoinVac(vacName, spec.Claim, fmt.Errorf("take-aside object at %s failed the identity proof: %w", spec.Scratch, perr))
	}
	// Wave-43 (d): the claimed vacated placeholder is unlinked ONLY after
	// the vacated name re-binds to the claim identity at syscall adjacency;
	// a refused/indeterminate cleanup keeps the occupant byte-intact with a
	// warn and the take stands.
	dropVacatedReservation(spec.FS, vacName, spec.Claim)
	hold.held = moved
	return hold, nil
}

// restoreOrJoin runs the wedge move-back for internal take legs and JOINS
// its failure into the wedge's own error (same discipline as history's
// restoreOrJoin): the caller sees ErrTakeAsideRestoreFailed through
// errors.Is when the source name could not be re-owned.
func (h *BoundAside) restoreOrJoin(err error) error {
	if rerr := h.Restore(); rerr != nil {
		return errors.Join(err, rerr)
	}
	return err
}

// restoreOrJoinVac extends restoreOrJoin with the wave-43 vacated-name
// compensation: after the moved object rode back onto src NO-REPLACE (or
// its restore failed), the vacated reservation rides back onto the freed
// scratch NO-REPLACE as well (vacRestoreOrDrop) — an occupied scratch (its
// own restore having failed) strands only our own placeholder at the
// vacated name with the joined ErrTakeAsideRestoreFailed.
func (h *BoundAside) restoreOrJoinVac(vacName string, claim os.FileInfo, err error) error {
	return vacRestoreOrDrop(h.fs, h.scratch, vacName, claim, h.restoreOrJoin(err))
}

// Restore compensates a live hold: the taken-aside object moves BACK onto
// the source name with NO-REPLACE semantics (PublishNoReplace), so a racer's
// claimant at the source name is never clobbered — on collision (or any
// publish failure) the object stays recoverable at the scratch name and the
// error classifies ErrTakeAsideRestoreFailed. Idempotent: only a live
// (moved, not yet finalized) hold performs the move-back.
func (h *BoundAside) Restore() error {
	if !h.moved {
		return nil
	}
	if back := PublishNoReplace(h.fs, h.scratch, h.src); back != nil {
		return fmt.Errorf("%w: %s stays recoverable at scratch %s: %v", ErrTakeAsideRestoreFailed, h.src, h.scratch, back)
	}
	h.moved = false
	return nil
}

// Unlink performs the one bound unlink of the take-aside flow: only the
// object the hold PROVED is ever removed, never the source pathname — and
// (wave-44, codex P2, PR#215 finding F2) never through a verify→Remove
// pathname pair on the caller-known scratch name either. The wave-38
// construction re-derived the scratch name no-follow and unlinked it by
// path; a plant replacing the occupant between the two syscalls had its
// foreign bytes deleted by OUR bound unlink. The unlink now runs the
// take's own vacate→verify→drop construction:
//
//  1. the scratch name is re-derived no-follow and must still name the
//     re-proven held object (dev/inode where exposed, plus size + mtime,
//     regular and non-symlink) — the unchanged wave-38 binding;
//  2. the proven object moves a SECOND time onto a FRESH crypto-claimed
//     terminal name: claimed O_EXCL (claimTakeAsideVacName), released
//     identity-bound, then published NO-REPLACE — a collision means a racer
//     owns the fresh draw: nothing relocated, the occupant kept, the
//     refusal typed. The publish-completed class is deliberately NOT
//     honored here (unlike the quarantine handoff's install publish —
//     wave-44 finding F1): a completed-with-residue vacate can leave the
//     scratch name STILL addressing the object (the hard-link fallback's
//     staged cleanup), so claiming a finalized delete would lie — every
//     publish error retains every name and refuses;
//  3. the terminal name is re-bound to the held identity at syscall
//     adjacency: a mismatch means the vacate moved a PLANT swapped onto the
//     scratch name inside step 1's re-proof→vacate window — the plant rides
//     back onto the freed scratch NO-REPLACE byte-intact (a ride-back
//     collision strands it recoverable at the terminal name with
//     ErrTakeAsideRestoreFailed joined) and the unlink refuses typed;
//  4. only the terminal name — provably the held object — is unlinked. A
//     wedged remove rewinds the object onto the freed scratch NO-REPLACE
//     and surfaces the wedge, so the caller's retry re-runs the whole bound
//     construction instead of losing the residue to silence.
//
// Terminal boundary (the codex-accepted fresh-claimed-name shape): the
// residual re-bind→Remove gap on the terminal name can be raced only by
// reclaiming the unpredictable crypto-token draw itself; every earlier
// attacker-predictable name rode a re-proof, and this flow never overwrites
// an occupant anywhere. A scratch name (or a just-vacated terminal name)
// that VANISHED on its own completed the hold by itself — the finalized
// object gone is precisely the outcome the unlink exists to reach, no
// foreign bytes were ever at risk — so those ENOENT legs answer success,
// not the vanished sentinel.
//
// The remaining wedges return the failure WITHOUT unlinking anything: a
// successful rewind leaves the hold live for Restore/retry (F3-style flows
// move the placeholder back onto the source name; F4-style flows leave the
// inert scratch for manual cleanup); a failed rewind strands the object
// recoverable at the terminal name with ErrTakeAsideRestoreFailed joined.
func (h *BoundAside) Unlink() error {
	if !h.moved || h.held == nil {
		return nil
	}
	cur, lerr := asideLstat(h.fs, h.scratch)
	switch {
	case os.IsNotExist(lerr):
		h.moved = false
		return nil
	case lerr != nil:
		return fmt.Errorf("inspect take-aside object %s at the unlink: %w", h.scratch, lerr)
	case !asideSameObject(cur, h.held):
		return fmt.Errorf("take-aside object %s no longer names the verified object at the unlink: %w", h.scratch, ErrTakeAsideForeign)
	}
	vacName, vacClaim, cerr := claimTakeAsideVacName(h.fs, h.scratch)
	if cerr != nil {
		return fmt.Errorf("reserve the bound unlink's terminal name for %s: %w", h.scratch, cerr)
	}
	if relErr := releaseTakeAsideVacClaim(h.fs, vacName, vacClaim); relErr != nil {
		return relErr
	}
	if pubErr := PublishNoReplace(h.fs, h.scratch, vacName); pubErr != nil {
		// No-replace relocated NOTHING on a refusal: the fresh draw's
		// claimant is preserved and the held object stays at the scratch
		// name. The publish-completed class keeps the same refusal posture
		// (see the doc): the object may sit at BOTH names, so nothing is
		// unlinked and the caller's retry re-derives this attempt's outcome.
		return fmt.Errorf("bound unlink's terminal vacate of %s onto %s refused — every occupant preserved byte-intact: %w", h.scratch, vacName, pubErr)
	}
	terminal, terr := asideLstat(h.fs, vacName)
	switch {
	case os.IsNotExist(terr):
		// Our own vacate moved the held object and it vanished before the
		// re-bind: the outcome the unlink exists to reach — success.
		h.moved = false
		return nil
	case terr != nil:
		return h.rerideTerminal(vacName, fmt.Errorf("inspect the bound unlink's terminal object %s: %w", vacName, terr))
	case !asideSameObject(terminal, h.held):
		return h.rerideTerminal(vacName, fmt.Errorf("bound unlink's terminal vacate moved a substituted occupant from %s (never unlinked): %w", h.scratch, ErrTakeAsideForeign))
	}
	if rerr := h.fs.Remove(vacName); rerr != nil {
		if os.IsNotExist(rerr) {
			h.moved = false
			return nil
		}
		return h.rerideTerminal(vacName, fmt.Errorf("remove the bound unlink's verified terminal object %s: %w", vacName, rerr))
	}
	h.moved = false
	return nil
}

// rerideTerminal runs the wave-44 terminal-leg rewind: whatever the terminal
// name holds after a doubt leg (the re-verified held object after a wedged
// remove, the preserved plant after a substitution, the unproven occupant
// after an indeterminate lookup) moves BACK onto the freed scratch name
// NO-REPLACE, restoring the pre-Unlink occupancy so the caller's retry or
// compensation re-derives this attempt's outcome against the pre-Unlink
// names. A ride-back refusal keeps BOTH occupants byte-intact and strands
// the terminal object recoverable at the terminal name with
// ErrTakeAsideRestoreFailed joined; the hold then reports the scratch
// vacated — nothing provably ours sits at the scratch name for Restore to
// move.
func (h *BoundAside) rerideTerminal(vacName string, err error) error {
	if back := PublishNoReplace(h.fs, vacName, h.scratch); back != nil {
		h.moved = false
		return errors.Join(err, fmt.Errorf("%w: scratch %s re-claimed or indeterminate — the terminal object stays recoverable at %s: %v", ErrTakeAsideRestoreFailed, h.scratch, vacName, back))
	}
	return err
}

// UnlinkVerified performs the bound terminal unlink of a named object given
// its verified identity — wave-r19 (codex P2, PR#215 findings F1/F2, the
// removeVerified unlink window). The verified object vacates onto a fresh
// crypto-claimed terminal sibling (claimed O_EXCL, released identity-bound
// through the wave-58 double reproof), the terminal re-binds to the verified
// identity at syscall adjacency, and ONLY the terminal — provably the
// verified object — is unlinked: never a pathname Remove of an unproven
// object. A plant swapped onto the name between the caller's verify and the
// vacate rides the no-replace vacate onto the terminal, fails the rebind,
// and rewinds byte-intact (the terminal object rides BACK onto the freed
// name NO-REPLACE so the caller's compensation restores it). Any vanish —
// the name before/during the vacate, the terminal after the vacate or at
// the Remove — answers ErrTakeAsideVanished (the owned bytes vanished
// unownably, never a consumed removal — the R4 posture the quarantine
// holds carry); a foreign terminal answers ErrTakeAsideForeign (preserved
// byte-intact); a wedged rewind joins ErrTakeAsideRestoreFailed (the
// terminal object stays recoverable at the terminal name).
func UnlinkVerified(fs afero.Fs, name string, verified os.FileInfo) error {
	terminal, termClaim, cerr := claimTakeAsideVacName(fs, name)
	if cerr != nil {
		return fmt.Errorf("reserve the bound-unlink terminal for %s: %w", name, cerr)
	}
	if relErr := releaseTakeAsideVacClaim(fs, terminal, termClaim); relErr != nil {
		return relErr
	}
	if moveErr := PublishNoReplace(fs, name, terminal); moveErr != nil {
		if os.IsNotExist(moveErr) {
			return fmt.Errorf("%w: %s vanished under the bound unlink", ErrTakeAsideVanished, name)
		}
		return fmt.Errorf("bound-unlink vacate of %s onto %s refused — occupant preserved byte-intact: %w", name, terminal, moveErr)
	}
	term, terr := asideLstat(fs, terminal)
	switch {
	case os.IsNotExist(terr):
		return fmt.Errorf("%w: %s (terminal %s empty after the vacate)", ErrTakeAsideVanished, name, terminal)
	case terr != nil:
		return rerideBoundUnlink(fs, terminal, name, fmt.Errorf("inspect the bound-unlink terminal %s: %w", terminal, terr))
	case !asideSameObject(term, verified):
		return rerideBoundUnlink(fs, terminal, name, fmt.Errorf("bound-unlink terminal %s names a foreign object, not the verified one — foreign bytes preserved (never unlinked): %w", terminal, ErrTakeAsideForeign))
	}
	if rerr := fs.Remove(terminal); rerr != nil {
		if os.IsNotExist(rerr) {
			return fmt.Errorf("%w: %s (terminal %s vanished under the unlink)", ErrTakeAsideVanished, name, terminal)
		}
		return rerideBoundUnlink(fs, terminal, name, fmt.Errorf("remove the bound-unlink terminal %s: %w", terminal, rerr))
	}
	return nil
}

// rerideBoundUnlink rewinds the bound-unlink terminal object BACK onto the
// freed name NO-REPLACE after a doubt leg (a foreign terminal, an
// indeterminate lookup, or a wedged remove), restoring the pre-vacate
// occupancy so the caller's compensation re-derives this attempt's outcome
// against the pre-unlink names. A rewind refusal (the name re-claimed or
// indeterminate) keeps BOTH occupants byte-intact and strands the terminal
// object recoverable at the terminal name with ErrTakeAsideRestoreFailed
// joined.
func rerideBoundUnlink(fs afero.Fs, terminal, name string, err error) error {
	if back := PublishNoReplace(fs, terminal, name); back != nil {
		return errors.Join(err, fmt.Errorf("%w: %s re-claimed or indeterminate — the terminal object stays recoverable at %s: %v", ErrTakeAsideRestoreFailed, name, terminal, back))
	}
	return err
}
