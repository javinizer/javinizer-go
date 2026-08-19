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
//     (still THE claimed placeholder — never a foreign swap), then moves the
//     object src names onto the scratch with the replace-aware rename
//     (ReplaceFile): the scratch is provably OUR claimed placeholder, so the
//     move can never displace foreign bytes;
//  3. the moved object is re-proven AT the scratch name against the
//     caller's identity proof — a swap between the caller's pre-take
//     verification and the move moved a DIFFERENT object, and that object
//     is never acted on destructively;
//  4. the ONLY unlink ever issued (BoundAside.Unlink) targets the scratch
//     name and re-binds the object at syscall adjacency (no-follow Lstat,
//     dev/inode where exposed, then size + mtime) before
//     fs.Remove runs.
//
// Every wedge compensation restores original names with NO-REPLACE moves
// (PublishNoReplace): a racer claiming the source name mid-wedge is never
// clobbered — the taken-aside object stays recoverable at the scratch name
// and the error classifies (ErrTakeAsideRestoreFailed). Whichever arm
// wedges, NO foreign bytes are ever removed and the operation's caller
// keeps its conservative failure posture.

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/afero"
)

// ErrTakeAsideForeign classifies a taken-aside object (or a take-aside
// reservation) that failed its identity proof: a pathname swap between the
// caller's verification and the atomic move moved or parked a DIFFERENT
// object than the one the operation owns, and that object is preserved
// byte-intact — the destructive step never runs against it.
var ErrTakeAsideForeign = errors.New("take-aside object failed the identity proof — foreign bytes preserved")

// ErrTakeAsideVanished classifies the take-aside scratch name being empty at
// a binding instant where a proven object was expected (post-move re-proof
// or unlink-time re-derivation). The owned object vanished through a path
// this flow never unlinked — indeterminate, never a consumed removal.
var ErrTakeAsideVanished = errors.New("take-aside object vanished before its bound operation completed")

// ErrTakeAsideRestoreFailed classifies the wedge-compensation failure: the
// taken-aside object could NOT be moved back onto the source name
// NO-REPLACE (a foreign claimant holds it, or the publish failed outright),
// so the source name is unowned while the object stays recoverable at the
// scratch name.
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

// TakeAside runs steps 2+3 of the take-aside sequence (see the package
// doc above): the reservation is re-proven against Claim, the src object is
// moved onto the scratch name with the replace-aware rename, and the moved
// object is re-proven at the scratch name through Prove. On success the
// returned hold names the re-proven object (held).
//
// Wedge legs:
//   - a reservation that no longer names Claim (foreign swap) or an
//     indeterminate reservation lookup: nothing was moved — typed
//     ErrTakeAsideForeign / plain wrapped error, foreign bytes preserved;
//   - a failed move: the rename is atomic, nothing relocated — OUR scratch
//     reservation is dropped best-effort and the move error surfaces;
//   - post-move scratch VANISHED: the owned object went through a path this
//     flow never unlinked — ErrTakeAsideVanished, no compensation (nothing
//     sits at the scratch name to move back);
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
	if merr := ReplaceFile(spec.FS, spec.Src, spec.Scratch); merr != nil {
		// The rename is atomic: a failed move relocated NOTHING, so the
		// reservation at the scratch name is still OUR claimed placeholder —
		// drop it best-effort exactly like the quarantine claim cleanups.
		_ = spec.FS.Remove(spec.Scratch)
		return nil, fmt.Errorf("take-aside move of %s onto %s: %w", spec.Src, spec.Scratch, merr)
	}
	hold := &BoundAside{fs: spec.FS, src: spec.Src, scratch: spec.Scratch, moved: true}
	moved, merr := asideLstat(spec.FS, spec.Scratch)
	switch {
	case os.IsNotExist(merr):
		// The moved object vanished unownably right after the move —
		// indeterminate, nothing to move back (the scratch name is empty).
		hold.moved = false
		return nil, fmt.Errorf("%w: %s (scratch empty at the post-move re-proof)", ErrTakeAsideVanished, spec.Scratch)
	case merr != nil:
		return nil, hold.restoreOrJoin(fmt.Errorf("inspect take-aside object %s after the move: %w", spec.Scratch, merr))
	}
	if perr := spec.Prove(moved); perr != nil {
		return nil, hold.restoreOrJoin(fmt.Errorf("take-aside object at %s failed the identity proof: %w", spec.Scratch, perr))
	}
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

// Unlink performs the one bound unlink of the take-aside flow: only THE
// SCRATCH name is ever removed, never the source pathname. Because
// fs.Remove is path-based, so the scratch name is re-derived no-follow AT
// UNLINK TIME and must still name the re-proven held object (dev/inode
// where exposed, plus size + mtime, regular and non-symlink) before the
// unlink runs; a substitution inside the window is refused with
// ErrTakeAsideForeign, never deleted. A scratch name that VANISHED on its
// own completed the hold by itself (no foreign bytes were ever at risk) —
// answering success, not the vanished sentinel, because the finalized
// object gone from the scratch name is precisely the outcome the unlink
// exists to reach.
//
// A wedge returns the failure WITHOUT compensating: the scratch name's
// state is left to the caller (F3-style flows Restore the placeholder back
// onto the source name; F4-style flows leave the inert scratch for manual
// cleanup and carry on).
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
	if rerr := h.fs.Remove(h.scratch); rerr != nil {
		if os.IsNotExist(rerr) {
			h.moved = false
			return nil
		}
		return fmt.Errorf("remove take-aside object %s (verified): %w", h.scratch, rerr)
	}
	h.moved = false
	return nil
}
