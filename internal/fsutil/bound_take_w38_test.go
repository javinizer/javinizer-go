package fsutil

// POSTER-WRITE-HARDENING wave-38 (codex P2, PR#215 findings F2/F3/F4) — the
// generalized no-replace take-aside (bound_take.go), lifted from history's
// quarantine construction: claim-bound reservation re-proof, replace-aware
// move onto OUR OWN scratch placeholder, post-move identity re-proof,
// scratch-only bound unlink, and no-replace move-back compensations. The
// wedge wrappers replay the seam-adjacent races on a virtual filesystem; the
// identity binding is pinned on the real OsFs.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w38TakeFixture claims a take-aside scratch sibling for src with real
// contents and returns everything TakeAside needs.
func w38TakeFixture(t *testing.T, fs afero.Fs, dir string) (src, scratch string, claim os.FileInfo) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	src = filepath.Join(dir, "poster.jpg")
	require.NoError(t, afero.WriteFile(fs, src, []byte("journal bytes"), 0o644))
	scratch = filepath.Join(dir, "poster.jpg.dlq.takeaside")
	reservation, err := fs.OpenFile(scratch, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	claim, err = reservation.Stat()
	require.NoError(t, err)
	require.NoError(t, reservation.Close())
	return src, scratch, claim
}

// w38SameProve is the identity proof every leg here uses: the moved object
// must BE the src object the fixture snapped.
func w38SameProve(expect os.FileInfo) func(os.FileInfo) error {
	return func(moved os.FileInfo) error {
		if !asideSameObject(moved, expect) {
			return errors.New("w38 prove refused")
		}
		return nil
	}
}

func TestTakeAsideW38_HappyAndBoundUnlink(t *testing.T) {
	for _, mk := range []struct {
		name string
		fs   func() afero.Fs
	}{
		{"virtual", func() afero.Fs { return afero.NewMemMapFs() }},
		{"osfs", func() afero.Fs { return afero.NewOsFs() }},
	} {
		t.Run(mk.name, func(t *testing.T) {
			fs := mk.fs()
			dir := t.TempDir()
			if mk.name == "virtual" {
				dir = "/out/w38-happy"
			}
			src, scratch, claim := w38TakeFixture(t, fs, dir)
			srcInfo, serr := fs.Stat(src)
			require.NoError(t, serr)

			hold, err := TakeAside(TakeAsideSpec{
				FS: fs, Src: src, Scratch: scratch, Claim: claim,
				Prove: w38SameProve(srcInfo),
			})
			require.NoError(t, err)
			require.Equal(t, scratch, hold.Scratch())
			_, lerr := fs.Stat(src)
			require.ErrorIs(t, lerr, os.ErrNotExist, "the move freed the source name")
			require.Equal(t, "journal bytes", string(w38Read(t, fs, scratch)))

			require.NoError(t, hold.Unlink())
			_, lerr = fs.Stat(scratch)
			require.ErrorIs(t, lerr, os.ErrNotExist, "the bound unlink removed the scratch")
			require.NoError(t, hold.Unlink(), "a finalized hold's unlink is a no-op")
			require.NoError(t, hold.Restore(), "a finalized hold's restore is a no-op")
			require.Empty(t, w43VacNames(t, fs, dir), "wave-43: the vacated placeholder was dropped claim-bound — no litter")
		})
	}
}

func TestTakeAsideW38_ReservationLegs(t *testing.T) {
	t.Run("indeterminate reservation lookup refuses before the move", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w38-resv-indet")
		srcInfo, _ := base.Stat(src)
		sentinel := errors.New("w38 lstat wedged")
		fs := &w38StatFailFs{Fs: base, victim: scratch, err: sentinel}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)), "nothing was moved")
		require.Zero(t, len(w38Entries(t, base, "/out/w38-resv-indet"))-2, "source + reservation untouched")
	})

	t.Run("foreign reservation swap refuses with bytes preserved", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w38-resv-swap")
		srcInfo, _ := base.Stat(src)
		// The racer replaces OUR claimed 0-byte placeholder with its own.
		require.NoError(t, base.Remove(scratch))
		require.NoError(t, afero.WriteFile(base, scratch, []byte("foreign reservation"), 0o644))

		hold, err := TakeAside(TakeAsideSpec{FS: base, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, ErrTakeAsideForeign)
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)))
		require.Equal(t, "foreign reservation", string(w38Read(t, base, scratch)),
			"the foreign occupant at the reservation name is never displaced")
	})

	t.Run("failed move relocates nothing and drops the reservation", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w38-move-fail")
		srcInfo, _ := base.Stat(src)
		moveErr := errors.New("w38 rename wedged")
		fs := &w38RenameFailFs{Fs: base, from: src, err: moveErr}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, moveErr)
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)), "the failed move relocated nothing")
		_, lerr := base.Stat(scratch)
		require.ErrorIs(t, lerr, os.ErrNotExist,
			"wave-43: the vacated reservation rode back onto the free scratch and was dropped re-proven — the same free-or-foreign failure shape the wave-38 best-effort drop kept")
	})

	t.Run("w43: failed move preserves a swapped scratch occupant", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w38-move-fail-swap")
		srcInfo, _ := base.Stat(src)
		moveErr := errors.New("w38 rename wedged")
		fs := &w38RenamePlantFs{Fs: base, from: src, err: moveErr, swapPath: scratch, foreign: []byte("foreign-placement")}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, moveErr)
		require.ErrorIs(t, err, ErrTakeAsideRestoreFailed,
			"wave-43: the publish refusal collided with the plant — the vacated placeholder strands recoverable at the vacated name")
		require.Equal(t, "foreign-placement", string(w38Read(t, base, scratch)),
			"the occupant swapped in during the failed move is preserved byte-intact")
		require.Len(t, w43VacNames(t, base, "/out/w38-move-fail-swap"), 1,
			"only our inert 0-byte placeholder lingers at the vacated name")
	})
}

// w38RenamePlantFs wedges the take-aside rename and plants foreign bytes at
// the reservation path in the verifying-cleanup window.
type w38RenamePlantFs struct {
	afero.Fs
	from     string
	err      error
	swapPath string
	foreign  []byte
}

func (f *w38RenamePlantFs) Rename(oldname, newname string) error {
	if oldname == f.from {
		_ = f.Fs.Remove(f.swapPath)
		return errors.Join(f.err, afero.WriteFile(f.Fs, f.swapPath, f.foreign, 0o600))
	}
	return f.Fs.Rename(oldname, newname)
}

func TestTakeAsideW38_PostMoveLegs(t *testing.T) {
	t.Run("post-move scratch vanished is the vanished sentinel", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w38-postmove-vanish")
		srcInfo, _ := base.Stat(src)
		// Wave-43 ordering: scratch Stat #1 is the reservation re-proof, #2 the
		// no-replace publish's destination classification, #3 the post-move
		// re-proof — the vanish replay fires there (once).
		fs := &w38VanishOnStatFs{Fs: base, victim: scratch, afterCalls: 2}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, ErrTakeAsideVanished)
		_, lerr := base.Stat(src)
		require.ErrorIs(t, lerr, os.ErrNotExist,
			"the move relocated the object before it vanished from the scratch — no compensation runs on nothing")
		_, lerr = base.Stat(scratch)
		require.ErrorIs(t, lerr, os.ErrNotExist,
			"the vacated placeholder rode back and was dropped re-proven — no residue")
		require.Empty(t, w43VacNames(t, base, "/out/w38-postmove-vanish"), "no vacated-name litter")
	})

	t.Run("post-move indeterminate lookup restores the source name", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w38-postmove-indet")
		srcInfo, _ := base.Stat(src)
		sentinel := errors.New("w38 post-move lstat wedged")
		// Wave-43 ordering: scratch Stat #3 is the post-move re-proof lookup.
		fs := &w38FailNthStatFs{Fs: base, victim: scratch, n: 3, err: sentinel}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, sentinel)
		require.NotErrorIs(t, err, ErrTakeAsideRestoreFailed,
			"the move-back succeeded — the source name re-holds its object")
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)))
		_, lerr := base.Stat(scratch)
		require.ErrorIs(t, lerr, os.ErrNotExist)
	})

	t.Run("prove refusal moves the object back no-replace", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w38-prove-refuse")
		srcInfo, _ := base.Stat(src)
		prove := func(os.FileInfo) error { return errors.New("w38 prove refused") }

		hold, err := TakeAside(TakeAsideSpec{FS: base, Src: src, Scratch: scratch, Claim: claim, Prove: prove})
		require.Nil(t, hold)
		require.ErrorContains(t, err, "w38 prove refused")
		require.NotErrorIs(t, err, ErrTakeAsideRestoreFailed)
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)),
			"the taken-aside object rides back onto the source name")
		_ = srcInfo
	})

	t.Run("prove refusal with a reclaimed source keeps both objects", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w38-prove-reclaim")
		prove := func(os.FileInfo) error { return errors.New("w38 prove refused") }
		// Wave-43 ordering: the racer's re-claim of the source name lands at
		// scratch Stat #3 (the post-move re-proof) — inside the publish's
		// move→prove window it vacated.
		fs := &w38ClaimOnPostMoveStatFs{Fs: base, victim: scratch, plantAt: src, n: 3}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: prove})
		require.Nil(t, hold)
		require.ErrorContains(t, err, "w38 prove refused")
		require.ErrorIs(t, err, ErrTakeAsideRestoreFailed,
			"the source name re-claimed mid-wedge — the move-back refused no-replace")
		require.Equal(t, "racer at source", string(w38Read(t, base, src)),
			"the racer's claimant is never clobbered")
		require.Equal(t, "journal bytes", string(w38Read(t, base, scratch)),
			"the taken-aside object stays recoverable at the scratch name")
	})
}

func TestTakeAsideW38_UnlinkLegs(t *testing.T) {
	newHold := func(t *testing.T, dir string) (afero.Fs, string, string, *BoundAside) {
		t.Helper()
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, dir)
		srcInfo, _ := base.Stat(src)
		hold, err := TakeAside(TakeAsideSpec{FS: base, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.NoError(t, err)
		return base, src, scratch, hold
	}

	t.Run("scratch vanished at the unlink completes the hold", func(t *testing.T) {
		base, _, scratch, hold := newHold(t, "/out/w38-unlink-vanish")
		require.NoError(t, base.Remove(scratch))
		require.NoError(t, hold.Unlink())
		require.NoError(t, hold.Restore(), "a vanished hold is dead for the compensation too")
	})

	t.Run("indeterminate unlink lookup keeps the hold live for compensation", func(t *testing.T) {
		base, src, scratch, hold := newHold(t, "/out/w38-unlink-indet")
		sentinel := errors.New("w38 unlink lstat wedged")
		func() { // scope the wrapper: wedge, fail once, then release
			// The hold keeps its fs — swap is impossible without re-binding;
			// drive the wedge via a fresh hold on the wrapper instead.
			wfs := &w38StatFailFs{Fs: base, victim: scratch, err: sentinel}
			whold := &BoundAside{fs: wfs, src: src, scratch: scratch, held: hold.held, moved: true}
			require.ErrorIs(t, whold.Unlink(), sentinel)
		}()
		// The surviving object stays at the scratch name and the plain hold
		// can finish it.
		require.NoError(t, hold.Unlink())
		_, lerr := base.Stat(scratch)
		require.ErrorIs(t, lerr, os.ErrNotExist)
		_ = src
	})

	t.Run("swap under the unlink refuses and preserves", func(t *testing.T) {
		base, _, scratch, hold := newHold(t, "/out/w38-unlink-swap")
		require.NoError(t, base.Remove(scratch))
		require.NoError(t, afero.WriteFile(base, scratch, []byte("foreign scratch object"), 0o644))
		err := hold.Unlink()
		require.ErrorIs(t, err, ErrTakeAsideForeign)
		require.Equal(t, "foreign scratch object", string(w38Read(t, base, scratch)),
			"the swapped-in occupant is never unlinked")
	})

	t.Run("wedged terminal unlink rewinds and the retry lands", func(t *testing.T) {
		// Wave-44: the bound unlink no longer path-removes the scratch —
		// the wedge targets the fresh terminal name's remove; the rewind
		// rides the re-verified object back onto the freed scratch NO-REPLACE.
		base, _, scratch, hold := newHold(t, "/out/w38-unlink-fail")
		sentinel := errors.New("w38 terminal remove wedged")
		wfs := &w44TerminalRemoveFailFs{Fs: base, err: sentinel, fail: 1}
		whold := &BoundAside{fs: wfs, scratch: scratch, held: hold.held, moved: true}
		err := whold.Unlink()
		require.ErrorIs(t, err, sentinel)
		require.Contains(t, err.Error(), "remove the bound unlink's verified terminal object")
		require.NotErrorIs(t, err, ErrTakeAsideRestoreFailed,
			"the scratch name was free — the rewind rode the verified object back no-replace")
		require.Equal(t, "journal bytes", string(w38Read(t, base, scratch)),
			"a wedged terminal unlink rewinds the object onto the scratch name — never a silent loss")
		require.Empty(t, w43VacNames(t, base, "/out/w38-unlink-fail"), "the rewind freed the terminal name")
		require.NoError(t, whold.Unlink(), "the retry re-runs the whole bound construction")
		_, lerr := base.Stat(scratch)
		require.ErrorIs(t, lerr, os.ErrNotExist)
	})

	t.Run("terminal unlink answering ENOENT after a racer's unlink completes the hold", func(t *testing.T) {
		base, _, scratch, hold := newHold(t, "/out/w38-unlink-enoent")
		whold := &BoundAside{fs: &w44TerminalRemoveEnoentFs{Fs: base}, scratch: scratch, held: hold.held, moved: true}
		require.NoError(t, whold.Unlink())
		_, lerr := base.Stat(scratch)
		require.ErrorIs(t, lerr, os.ErrNotExist)
		require.Empty(t, w43VacNames(t, base, "/out/w38-unlink-enoent"))
	})

	t.Run("dead holds take no further action", func(t *testing.T) {
		base := afero.NewMemMapFs()
		_, scratch, claim := w38TakeFixture(t, base, "/out/w38-dead-hold")
		hold := &BoundAside{fs: base, scratch: scratch}
		require.NoError(t, hold.Unlink(), "no moved object — nothing to unlink")
		require.NoError(t, hold.Restore())
		require.Equal(t, scratch, hold.Scratch())
		_ = claim
	})
}

func TestAsideSameObjectW38_ShapeTable(t *testing.T) {
	tmp := t.TempDir()
	mk := func(name string, data []byte) os.FileInfo {
		p := filepath.Join(tmp, name)
		require.NoError(t, os.WriteFile(p, data, 0o600))
		info, err := os.Lstat(p)
		require.NoError(t, err)
		return info
	}
	expect := mk("expect", []byte("x"))
	require.True(t, asideSameObject(expect, expect), "the object matches itself")
	require.False(t, asideSameObject(nil, expect), "a nil lookup never matches")

	mk("other", []byte("x"))
	require.NoError(t, os.Chtimes(filepath.Join(tmp, "other"), expect.ModTime(), expect.ModTime()))
	otherInfo, err := os.Lstat(filepath.Join(tmp, "other"))
	require.NoError(t, err)
	// boundObjectIdentity exposes dev/inode only on the POSIX Stat_t targets
	// (bound_identity_other.go answers not-OK elsewhere): on Windows the
	// SAME-SHAPE different-inode leg degrades to the shape/metadata legs, so
	// the refusal is asserted only where a kernel identity exists.
	if runtime.GOOS == "windows" {
		require.True(t, asideSameObject(otherInfo, expect),
			"windows exposes no dev/ino — the shape binding alone answers")
	} else {
		require.False(t, asideSameObject(otherInfo, expect),
			"same shape, different inode refuses via dev/ino")
	}

	bigger := mk("bigger", []byte("xyz"))
	require.False(t, asideSameObject(bigger, expect), "size divergence refuses")

	link := filepath.Join(tmp, "link")
	require.NoError(t, os.Symlink(filepath.Join(tmp, "expect"), link))
	linkInfo, lerr := os.Lstat(link)
	require.NoError(t, lerr)
	require.False(t, asideSameObject(linkInfo, expect), "a symlink refuses")

	require.False(t, asideSameObject(expect, nil), "a nil expectation never matches")

	// Virtual filesystems expose no kernel identity: the shape/metadata legs
	// carry the binding (same posture as the quarantine helpers).
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/m", []byte("mm"), 0o600))
	memInfo, merr := base.Stat("/m")
	require.NoError(t, merr)
	require.True(t, asideSameObject(memInfo, memInfo), "no dev/ino on either side — shape match answers")
}

// --- wedge doubles ---------------------------------------------------------

type w38StatFailFs struct {
	afero.Fs
	victim string
	err    error
}

func (f *w38StatFailFs) Stat(name string) (os.FileInfo, error) {
	if name == f.victim {
		return nil, f.err
	}
	return f.Fs.Stat(name)
}

type w38FailNthStatFs struct {
	afero.Fs
	victim string
	n      int
	err    error
	calls  int
}

func (f *w38FailNthStatFs) Stat(name string) (os.FileInfo, error) {
	if name == f.victim {
		f.calls++
		if f.calls == f.n {
			return nil, f.err
		}
	}
	return f.Fs.Stat(name)
}

// w38VanishOnStatFs answers os.ErrNotExist for the victim ONCE, at the
// lookup after afterCalls lookups for it have passed (used to vanish the
// scratch between the move and the post-move re-proof); later lookups pass
// through so the wave-43 ride-back + re-proven drop can complete.
type w38VanishOnStatFs struct {
	afero.Fs
	victim     string
	afterCalls int
	calls      int
}

func (f *w38VanishOnStatFs) Stat(name string) (os.FileInfo, error) {
	if name == f.victim {
		f.calls++
		if f.calls == f.afterCalls+1 {
			_ = f.Fs.Remove(name)
			return nil, os.ErrNotExist
		}
	}
	return f.Fs.Stat(name)
}

// w38ClaimOnPostMoveStatFs plants a racer claimant at plantAt when the
// victim scratch is inspected at its nth lookup (wave-43: n=3 is the
// post-move re-proof).
type w38ClaimOnPostMoveStatFs struct {
	afero.Fs
	victim  string
	plantAt string
	n       int
	calls   int
}

func (f *w38ClaimOnPostMoveStatFs) Stat(name string) (os.FileInfo, error) {
	if name == f.victim {
		f.calls++
		if f.calls == f.n {
			if err := afero.WriteFile(f.Fs, f.plantAt, []byte("racer at source"), 0o600); err != nil {
				return nil, err
			}
		}
	}
	return f.Fs.Stat(name)
}

type w38RenameFailFs struct {
	afero.Fs
	from string
	err  error
}

func (f *w38RenameFailFs) Rename(oldname, newname string) error {
	if oldname == f.from {
		return f.err
	}
	return f.Fs.Rename(oldname, newname)
}

func w38Read(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	body, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return body
}

func w38Entries(t *testing.T, fs afero.Fs, dir string) []os.FileInfo {
	t.Helper()
	entries, err := afero.ReadDir(fs, dir)
	require.NoError(t, err)
	return entries
}
