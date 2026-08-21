package fsutil

// POSTER-WRITE-HARDENING wave-44 (codex P2, PR#215 finding F2) — the bound
// unlink loses its verify→Remove pathname window on the SCRATCH name: the
// proven-held object vacates a second time onto a fresh crypto-claimed
// terminal name (claimed O_EXCL, released identity-bound, published
// NO-REPLACE), is re-bound to the held identity there, and only the
// terminal name is unlinked. These legs replay a plant in every window the
// residual construction keeps (bind→vacate, vacate→re-bind, claim
// claim→release), the publish refusal classes (collision / hard error /
// publish-completed), and the rewind discipline every doubt leg runs
// (NO-REPLACE ride-back onto the freed scratch, ErrTakeAsideRestoreFailed
// joined when a racer holds the name).
//
// ".vac." Stat ordinals inside ONE Unlink call (claim-handle Stats bypass
// the wrappers): #1 = terminal-claim release verify, #2 = the release's
// unlink-adjacent re-proof (wave-58 dual-reproof), #3 = the no-replace
// vacate's classification, #4 = the post-vacate identity re-bind. Windows
// that must survive further ordinal shifts are scripted STRUCTURALLY (see
// the wave-43 doubles), not by counting lookups.

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestTakeAsideW44_UnlinkPlantInSecondRenameWindow(t *testing.T) {
	newHold := func(t *testing.T, dir string) (afero.Fs, string, string, *BoundAside) {
		t.Helper()
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, dir)
		srcInfo, _ := base.Stat(src)
		hold, err := TakeAside(TakeAsideSpec{FS: base, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.NoError(t, err)
		return base, src, scratch, hold
	}

	t.Run("plant in the bind→vacate window rides back preserved", func(t *testing.T) {
		base, _, scratch, hold := newHold(t, "/out/w44-plantvac")
		plant := []byte("plant swapped onto the scratch inside the second rename window")
		fs := &w44PlantOnUnlinkVacateRenameFs{Fs: base, scratch: scratch, plant: plant}
		whold := &BoundAside{fs: fs, scratch: scratch, held: hold.held, moved: true}

		err := whold.Unlink()
		require.ErrorIs(t, err, ErrTakeAsideForeign,
			"the vacate moved the PLANT — the terminal re-bind refuses it, never deletes it")
		require.NotErrorIs(t, err, ErrTakeAsideRestoreFailed,
			"the scratch name was free — the plant rode back no-replace")
		require.Equal(t, plant, w38Read(t, base, scratch),
			"the plant is preserved byte-intact at the scratch name — the wave-38 unlink would have deleted it")
		require.Empty(t, w43VacNames(t, base, "/out/w44-plantvac"), "the ride-back freed the terminal name")
		err = whold.Unlink()
		require.ErrorIs(t, err, ErrTakeAsideForeign,
			"a retry keeps refusing the substituted occupant — the foreign bytes are never unlinked")
		require.Equal(t, plant, w38Read(t, base, scratch))
	})

	t.Run("ride-back collision strands the plant recoverable at the terminal name", func(t *testing.T) {
		base, _, scratch, hold := newHold(t, "/out/w44-plantcoll")
		plant := []byte("plant the vacate moved")
		plant2 := []byte("racer reclaiming the freed scratch")
		fs := &w44PlantOnUnlinkVacateRenameFs{Fs: base, scratch: scratch, plant: plant, plant2: plant2}
		whold := &BoundAside{fs: fs, scratch: scratch, held: hold.held, moved: true}

		err := whold.Unlink()
		require.ErrorIs(t, err, ErrTakeAsideForeign)
		require.ErrorIs(t, err, ErrTakeAsideRestoreFailed,
			"the ride-back collided with the racer's reclaim — typed, nothing clobbered")
		require.Equal(t, plant2, w38Read(t, base, scratch), "the racer's reclaim is preserved byte-intact")
		vacName := vacNameOf(t, base, "/out/w44-plantcoll")
		require.Equal(t, plant, w38Read(t, base, vacName),
			"the vacate-moved plant strands recoverable at the terminal name — never unlinked")
		require.NoError(t, whold.Unlink(), "the stranded hold is inert — the scratch no longer names anything ours")
	})
}

func TestTakeAsideW44_UnlinkTerminalLegs(t *testing.T) {
	newHold := func(t *testing.T, dir string) (afero.Fs, string, string, *BoundAside) {
		t.Helper()
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, dir)
		srcInfo, _ := base.Stat(src)
		hold, err := TakeAside(TakeAsideSpec{FS: base, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.NoError(t, err)
		return base, src, scratch, hold
	}

	t.Run("vacated object vanishing before the re-bind completed the hold", func(t *testing.T) {
		base, _, scratch, hold := newHold(t, "/out/w44-vacvanish")
		fs := &w44VanishVacAfterUnlinkVacateFs{Fs: base, scratch: scratch}
		whold := &BoundAside{fs: fs, scratch: scratch, held: hold.held, moved: true}

		require.NoError(t, whold.Unlink(),
			"gone through nobody's unlink of OURS — the outcome the unlink exists to reach")
		_, lerr := base.Stat(scratch)
		require.ErrorIs(t, lerr, os.ErrNotExist)
		require.Empty(t, w43VacNames(t, base, "/out/w44-vacvanish"))
	})

	t.Run("indeterminate terminal lookup rewinds onto the scratch", func(t *testing.T) {
		base, _, scratch, hold := newHold(t, "/out/w44-vacindet")
		sentinel := errors.New("w44 terminal lstat wedged")
		fs := &w43FailPostVacateLookupFs{Fs: base, err: sentinel}
		whold := &BoundAside{fs: fs, scratch: scratch, held: hold.held, moved: true}

		err := whold.Unlink()
		require.ErrorIs(t, err, sentinel,
			"the indeterminate answer at the terminal identity re-bind refuses typed — the wrap text rides the wave-58 release's own arming, so only the class is pinned")
		require.True(t, fs.done, "the wedge fired at the post-vacate binding instant, not inside the release's own proofs")
		require.NotErrorIs(t, err, ErrTakeAsideRestoreFailed, "the rewind landed on the free scratch")
		require.Equal(t, "journal bytes", string(w38Read(t, base, scratch)),
			"the unproven object rewound onto the pre-Unlink name — nothing deleted on doubt")
		require.NoError(t, whold.Unlink(), "the retry (wedge spent) re-runs and lands")
		_, lerr := base.Stat(scratch)
		require.ErrorIs(t, lerr, os.ErrNotExist)
	})

	t.Run("wedged terminal remove with a reclaimed scratch strands recoverable", func(t *testing.T) {
		base, _, scratch, hold := newHold(t, "/out/w44-wedgecoll")
		sentinel := errors.New("w44 terminal remove wedged")
		fs := &w44TerminalRemoveFailFs{Fs: base, err: sentinel, fail: -1, plantScratchOnFail: true}
		whold := &BoundAside{fs: fs, scratch: scratch, held: hold.held, moved: true}

		err := whold.Unlink()
		require.ErrorIs(t, err, sentinel)
		require.ErrorIs(t, err, ErrTakeAsideRestoreFailed,
			"the rewind collided with the racer's reclaim — typed, both occupants kept")
		require.ErrorContains(t, err, "remove the bound unlink's verified terminal object")
		require.Equal(t, "racer on the freed scratch", string(w38Read(t, base, scratch)),
			"the racer's reclaim is never clobbered")
		vacName := vacNameOf(t, base, "/out/w44-wedgecoll")
		require.Equal(t, "journal bytes", string(w38Read(t, base, vacName)),
			"the re-verified object stays recoverable at the terminal name")
		require.NoError(t, whold.Unlink(), "the stranded hold is inert")
	})
}

func TestTakeAsideW44_UnlinkVacatePublishLegs(t *testing.T) {
	newHold := func(t *testing.T, dir string) (afero.Fs, string, string, *BoundAside) {
		t.Helper()
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, dir)
		srcInfo, _ := base.Stat(src)
		hold, err := TakeAside(TakeAsideSpec{FS: base, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.NoError(t, err)
		return base, src, scratch, hold
	}

	t.Run("collision at the fresh terminal name retains everything typed", func(t *testing.T) {
		base, _, scratch, hold := newHold(t, "/out/w44-vaccoll")
		plant := []byte("racer owning the fresh terminal draw")
		fs := &w43PlantAfterVacReleaseFs{Fs: base, plant: plant}
		whold := &BoundAside{fs: fs, scratch: scratch, held: hold.held, moved: true}

		err := whold.Unlink()
		require.ErrorIs(t, err, ErrPublishCollision, "the no-replace vacate reported the racer's draw")
		require.ErrorContains(t, err, "every occupant preserved byte-intact")
		require.Equal(t, "journal bytes", string(w38Read(t, base, scratch)), "the held object never moved")
		vacName := vacNameOf(t, base, "/out/w44-vaccoll")
		require.Equal(t, plant, w38Read(t, base, vacName), "the racer's occupant is preserved byte-intact")
		require.NoError(t, base.Remove(vacName))
		fs2 := &BoundAside{fs: base, scratch: scratch, held: hold.held, moved: true}
		require.NoError(t, fs2.Unlink(), "the racer's draw gone — the retry lands")
		_, lerr := base.Stat(scratch)
		require.ErrorIs(t, lerr, os.ErrNotExist)
	})

	t.Run("hard vacate failure relocates nothing", func(t *testing.T) {
		base, _, scratch, hold := newHold(t, "/out/w44-vachard")
		sentinel := errors.New("w44 vacate rename wedged")
		fs := &w38RenameFailFs{Fs: base, from: scratch, err: sentinel}
		whold := &BoundAside{fs: fs, scratch: scratch, held: hold.held, moved: true}

		err := whold.Unlink()
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "journal bytes", string(w38Read(t, base, scratch)),
			"a no-replace failure moved nothing — the held object stays put")
		require.Empty(t, w43VacNames(t, base, "/out/w44-vachard"), "the released claim left no litter")
	})

	t.Run("publish-completed vacate retains both names — never an unverified delete", func(t *testing.T) {
		base, _, scratch, hold := newHold(t, "/out/w44-vaccompleted")
		fs := &w44VacateCompletedFs{Fs: base, scratch: scratch}
		whold := &BoundAside{fs: fs, scratch: scratch, held: hold.held, moved: true}

		err := whold.Unlink()
		require.ErrorIs(t, err, ErrPublishCompleted,
			"the completed class is NOT honored for the vacate — it is not the install goal (contrast wave-44 finding F1)")
		require.ErrorContains(t, err, "every occupant preserved byte-intact")
		vacName := vacNameOf(t, base, "/out/w44-vaccompleted")
		require.Equal(t, "journal bytes", string(w38Read(t, base, vacName)),
			"the landed object is RETAINED recoverable at the terminal name — residue, never deleted unverified")
		require.NoError(t, base.Remove(vacName))
		require.NoError(t, whold.Unlink(), "residue cleared — the freed scratch completes the hold")
	})

	t.Run("terminal-claim entropy wedge refuses before anything relocates", func(t *testing.T) {
		base, _, scratch, hold := newHold(t, "/out/w44-entropy")
		sentinel := errors.New("w44 terminal entropy wedged")
		prev := takeAsideVacRandReader
		takeAsideVacRandReader = &w43FailReader{err: sentinel}
		t.Cleanup(func() { takeAsideVacRandReader = prev })
		whold := &BoundAside{fs: base, scratch: scratch, held: hold.held, moved: true}

		err := whold.Unlink()
		require.ErrorIs(t, err, sentinel)
		require.ErrorContains(t, err, "reserve the bound unlink's terminal name")
		require.Equal(t, "journal bytes", string(w38Read(t, base, scratch)))
		takeAsideVacRandReader = prev
		require.NoError(t, whold.Unlink(), "the seam restored — the retry lands")
	})

	t.Run("foreign swap at the terminal claim release refuses and preserves", func(t *testing.T) {
		base, _, scratch, hold := newHold(t, "/out/w44-relswap")
		occupant := []byte("foreign swap inside the terminal claim-release window")
		fs := &w43VacClaimCloseFs{Fs: base, plant: occupant}
		whold := &BoundAside{fs: fs, scratch: scratch, held: hold.held, moved: true}

		err := whold.Unlink()
		require.ErrorIs(t, err, ErrTakeAsideForeign)
		require.ErrorContains(t, err, "no longer names our claimed placeholder")
		require.Equal(t, "journal bytes", string(w38Read(t, base, scratch)), "the held object never moved")
		vacName := vacNameOf(t, base, "/out/w44-relswap")
		require.Equal(t, occupant, w38Read(t, base, vacName), "the foreign occupant of the draw is preserved")
	})
}

// --- wedge doubles ---------------------------------------------------------

// w44TerminalRemoveFailFs wedges the bound unlink's TERMINAL remove: the
// terminal name is learned as the ".vac."-carrying rename target (the
// scratch→terminal vacate arms it), and the first fail Removes of the armed
// name answer err (fail < 0 wedges every armed remove). plantScratchOnFail
// replays a racer reclaiming the freed scratch name inside the
// remove→rewind window so the rewind's no-replace ride-back collides.
type w44TerminalRemoveFailFs struct {
	afero.Fs
	err                error
	fail               int
	plantScratchOnFail bool
	armed              atomic.Value // string: the armed terminal name
	fails              int
}

func (f *w44TerminalRemoveFailFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	// Arm only a terminal name carrying a REAL object — a test's own
	// TakeAside fixture housekeeping vacates the 0-byte reservation
	// placeholder beneath the same suffix shape and is never the unlink
	// under test.
	if err == nil && strings.Contains(newname, ".vac.") {
		if info, serr := f.Fs.Stat(newname); serr == nil && info.Size() > 0 {
			f.armed.Store(newname)
		}
	}
	return err
}

func (f *w44TerminalRemoveFailFs) Remove(name string) error {
	if name == f.armed.Load() && (f.fail < 0 || f.fails < f.fail) {
		f.fails++
		if f.plantScratchOnFail {
			scratch := name[:strings.Index(name, ".vac.")]
			if err := afero.WriteFile(f.Fs, scratch, []byte("racer on the freed scratch"), 0o600); err != nil {
				return err
			}
		}
		return f.err
	}
	return f.Fs.Remove(name)
}

// w44TerminalRemoveEnoentFs unlinks the armed terminal name itself and
// answers os.ErrNotExist — a racer winning the terminal unlink after the
// identity re-bind (the documented terminal boundary).
type w44TerminalRemoveEnoentFs struct {
	afero.Fs
	armed atomic.Value // string: the armed terminal name
}

func (f *w44TerminalRemoveEnoentFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && strings.Contains(newname, ".vac.") {
		if info, serr := f.Fs.Stat(newname); serr == nil && info.Size() > 0 {
			f.armed.Store(newname)
		}
	}
	return err
}

func (f *w44TerminalRemoveEnoentFs) Remove(name string) error {
	if name == f.armed.Load() {
		if err := f.Fs.Remove(name); err != nil {
			return err
		}
		return os.ErrNotExist
	}
	return f.Fs.Remove(name)
}

// w44PlantOnUnlinkVacateRenameFs replays a foreign swap at the scratch name
// inside the bound unlink's bind→vacate window: the vacate rename moves the
// PLANT onto the fresh terminal name, and the terminal identity re-bind
// must refuse it (never delete it). plant2 non-nil additionally reclaims
// the freed scratch name so the preservational ride-back collides.
type w44PlantOnUnlinkVacateRenameFs struct {
	afero.Fs
	scratch string
	plant   []byte
	plant2  []byte
	done    bool
}

func (f *w44PlantOnUnlinkVacateRenameFs) Rename(oldname, newname string) error {
	if !f.done && oldname == f.scratch && strings.Contains(newname, ".vac.") {
		f.done = true
		if err := f.Fs.Remove(oldname); err != nil {
			return err
		}
		if err := afero.WriteFile(f.Fs, oldname, f.plant, 0o600); err != nil {
			return err
		}
		err := f.Fs.Rename(oldname, newname)
		if err == nil && f.plant2 != nil {
			if werr := afero.WriteFile(f.Fs, oldname, f.plant2, 0o600); werr != nil {
				return werr
			}
		}
		return err
	}
	return f.Fs.Rename(oldname, newname)
}

// w44VanishVacAfterUnlinkVacateFs removes the terminal name immediately
// after the vacate rename landed — the post-vacate re-bind answers ENOENT
// (the just-vacated object vanished through nobody's delete of ours).
type w44VanishVacAfterUnlinkVacateFs struct {
	afero.Fs
	scratch string
	done    bool
}

func (f *w44VanishVacAfterUnlinkVacateFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && !f.done && oldname == f.scratch && strings.Contains(newname, ".vac.") {
		f.done = true
		if rerr := f.Fs.Remove(newname); rerr != nil {
			return rerr
		}
	}
	return err
}

// w44VacateCompletedFs performs the vacate rename for real and then reports
// the POSIX hard-link fallback's completed-with-residue class (the staged
// cleanup failed AND the rollback failed): the object sits at the terminal
// name while an error rides up — the bound unlink must retain BOTH names
// and refuse, never reporting a finalized delete over live residue.
type w44VacateCompletedFs struct {
	afero.Fs
	scratch string
	done    bool
}

func (f *w44VacateCompletedFs) Rename(oldname, newname string) error {
	if !f.done && oldname == f.scratch && strings.Contains(newname, ".vac.") {
		f.done = true
		if err := f.Fs.Rename(oldname, newname); err != nil {
			return err
		}
		return fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed AND publish rollback failed: %w", oldname, newname, ErrPublishCompleted)
	}
	return f.Fs.Rename(oldname, newname)
}
