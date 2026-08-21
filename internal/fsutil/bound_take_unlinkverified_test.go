package fsutil

// POSTER-WRITE-HARDENING wave-r19 (codex P2, PR#215) — UnlinkVerified is the
// standalone bound-unlink twin of BoundAside.Unlink (called by history's
// removeVerified, the downloader rollback, and — after F2 —
// dropVacatedReservation). Its error legs mirror BoundAside.Unlink's terminal
// chain, so these tests reuse the wave-44 wedge doubles keyed on the source
// name (UnlinkVerified's "name" param): each leg proves a foreign/vanished/
// wedged occupant is preserved byte-intact, never a pathname Remove of an
// unproven object.

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w19UVFixture writes a verified object at dir+"/verified" and returns its
// captured identity (the claim UnlinkVerified carries from the caller).
func w19UVFixture(t *testing.T, dir, content string) (afero.Fs, string, os.FileInfo) {
	t.Helper()
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll(dir, 0o755))
	name := dir + "/verified"
	require.NoError(t, afero.WriteFile(base, name, []byte(content), 0o600))
	info, err := base.Stat(name)
	require.NoError(t, err)
	return base, name, info
}

// w19SrcVanishOnVacateFs removes the source name just before the no-replace
// vacate rename lands — the publish rename answers ENOENT (the verified
// object vanished under the bound unlink).
type w19SrcVanishOnVacateFs struct {
	afero.Fs
	src  string
	done bool
}

func (f *w19SrcVanishOnVacateFs) Rename(oldname, newname string) error {
	if !f.done && oldname == f.src && strings.Contains(newname, ".vac.") {
		f.done = true
		if err := f.Fs.Remove(oldname); err != nil {
			return err
		}
	}
	return f.Fs.Rename(oldname, newname)
}

// The terminal-name reservation failure (claimTakeAsideVacName) refuses
// before anything relocates — never a pathname Remove.
func TestUnlinkVerifiedW19_ClaimFailureRefuses(t *testing.T) {
	base, name, verified := w19UVFixture(t, "/uv-claim", "verified bytes")
	sentinel := errors.New("uv entropy wedged")
	prev := takeAsideVacRandReader
	takeAsideVacRandReader = &w43FailReader{err: sentinel}
	t.Cleanup(func() { takeAsideVacRandReader = prev })

	err := UnlinkVerified(base, name, verified)
	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "reserve the bound-unlink terminal")
	require.Equal(t, "verified bytes", string(w38Read(t, base, name)), "nothing relocated")
}

// A foreign swap at the terminal claim's release refuses typed — the verified
// object never moved, the foreign occupant is preserved.
func TestUnlinkVerifiedW19_ReleaseFailureForeignRetains(t *testing.T) {
	base, name, verified := w19UVFixture(t, "/uv-rel", "verified bytes")
	fs := &w43VacClaimCloseFs{Fs: base, plant: []byte("foreign swap at the terminal claim")}

	err := UnlinkVerified(fs, name, verified)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.ErrorContains(t, err, "no longer names our claimed placeholder")
	require.Equal(t, "verified bytes", string(w38Read(t, base, name)), "the verified object never moved")
}

// A collision at the fresh terminal name (a racer claiming the released draw)
// refuses the vacate — everything preserved byte-intact.
func TestUnlinkVerifiedW19_VacateCollisionRefuses(t *testing.T) {
	base, name, verified := w19UVFixture(t, "/uv-coll", "verified bytes")
	fs := &w43PlantAfterVacReleaseFs{Fs: base, plant: []byte("racer owning the fresh terminal draw")}

	err := UnlinkVerified(fs, name, verified)
	require.ErrorIs(t, err, ErrPublishCollision)
	require.ErrorContains(t, err, "bound-unlink vacate")
	require.ErrorContains(t, err, "occupant preserved byte-intact")
	require.Equal(t, "verified bytes", string(w38Read(t, base, name)), "the verified object never moved")
}

// The verified object vanishing under the no-replace vacate is the typed
// vanished sentinel — never a consumed removal.
func TestUnlinkVerifiedW19_VacateVanishedRefuses(t *testing.T) {
	base, name, verified := w19UVFixture(t, "/uv-van", "verified bytes")
	fs := &w19SrcVanishOnVacateFs{Fs: base, src: name}

	err := UnlinkVerified(fs, name, verified)
	require.ErrorIs(t, err, ErrTakeAsideVanished)
	require.ErrorContains(t, err, "vanished under the bound unlink")
}

// The terminal vanishing after the vacate (through nobody's delete of ours)
// is the typed vanished sentinel.
func TestUnlinkVerifiedW19_TerminalVanishedRefuses(t *testing.T) {
	base, name, verified := w19UVFixture(t, "/uv-tvan", "verified bytes")
	fs := &w44VanishVacAfterUnlinkVacateFs{Fs: base, scratch: name}

	err := UnlinkVerified(fs, name, verified)
	require.ErrorIs(t, err, ErrTakeAsideVanished)
	require.ErrorContains(t, err, "empty after the vacate")
}

// An indeterminate terminal re-bind rewinds the object onto the freed name
// byte-intact (rerideBoundUnlink) — nothing deleted on doubt.
func TestUnlinkVerifiedW19_TerminalIndeterminateRewinds(t *testing.T) {
	base, name, verified := w19UVFixture(t, "/uv-tind", "verified bytes")
	sentinel := errors.New("uv terminal lstat wedged")
	fs := &w43FailPostVacateLookupFs{Fs: base, err: sentinel}

	err := UnlinkVerified(fs, name, verified)
	require.ErrorIs(t, err, sentinel)
	require.NotErrorIs(t, err, ErrTakeAsideRestoreFailed, "the rewind landed on the freed name")
	require.Equal(t, "verified bytes", string(w38Read(t, base, name)), "the unproven object rewound byte-intact")
}

// A foreign terminal (a plant the vacate moved) is refused, rewound onto the
// freed name byte-intact — never deleted unverified.
func TestUnlinkVerifiedW19_ForeignTerminalRewinds(t *testing.T) {
	base, name, verified := w19UVFixture(t, "/uv-foreign", "verified bytes")
	plant := []byte("foreign plant swapped onto the terminal")
	fs := &w44PlantOnUnlinkVacateRenameFs{Fs: base, scratch: name, plant: plant}

	err := UnlinkVerified(fs, name, verified)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.ErrorContains(t, err, "foreign bytes preserved")
	require.Equal(t, plant, w38Read(t, base, name), "the foreign plant rewound onto the freed name byte-intact")
}

// A wedged terminal remove whose rewind collides with a reclaimed name
// strands the verified object recoverable at the terminal name — both
// occupants kept, typed ErrTakeAsideRestoreFailed.
func TestUnlinkVerifiedW19_WedgedRemoveReclaimedStrands(t *testing.T) {
	base, name, verified := w19UVFixture(t, "/uv-wedge", "verified bytes")
	sentinel := errors.New("uv terminal remove wedged")
	fs := &w44TerminalRemoveFailFs{Fs: base, err: sentinel, fail: -1, plantScratchOnFail: true}

	err := UnlinkVerified(fs, name, verified)
	require.ErrorIs(t, err, sentinel)
	require.ErrorIs(t, err, ErrTakeAsideRestoreFailed, "the rewind collided with the racer's reclaim")
	require.Equal(t, "racer on the freed scratch", string(w38Read(t, base, name)),
		"the racer's reclaim is never clobbered")
	vacName := vacNameOf(t, base, "/uv-wedge")
	require.Equal(t, "verified bytes", string(w38Read(t, base, vacName)),
		"the re-verified object stays recoverable at the terminal name")
}
