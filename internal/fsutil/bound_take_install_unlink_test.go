package fsutil

// Mirrors the wave-r19 corpus: the legs of UnlinkVerifiedInstall under the
// same fault classes keep the same byte-preservation guarantees (codex P1
// lineage seeded by QWen PRRT_kwDORn9KaM6qKY2k).

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func w19UVInstallFixture(t *testing.T, dir, content string) (afero.Fs, string, *BoundInstallIdentity) {
	t.Helper()
	base, name, info := w19UVFixture(t, dir, content)
	return base, name, NewWeakBoundInstallIdentity(info)
}

func TestUnlinkVerifiedInstallW_ClaimFailureRefuses(t *testing.T) {
	base, name, verified := w19UVInstallFixture(t, "/uvi-claim", "verified bytes")
	sentinel := errors.New("uvi entropy wedged")
	prev := takeAsideVacRandReader
	takeAsideVacRandReader = &w43FailReader{err: sentinel}
	t.Cleanup(func() { takeAsideVacRandReader = prev })

	err := UnlinkVerifiedInstall(base, name, verified)
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, "verified bytes", string(w38Read(t, base, name)), "nothing relocated")
}

func TestUnlinkVerifiedInstallW_ReleaseFailureForeignRetains(t *testing.T) {
	base, name, verified := w19UVInstallFixture(t, "/uvi-rel", "verified bytes")
	fs := &w43VacClaimCloseFs{Fs: base, plant: []byte("foreign swap at the terminal claim")}

	err := UnlinkVerifiedInstall(fs, name, verified)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.Equal(t, "verified bytes", string(w38Read(t, base, name)), "the verified object never moved")
}

func TestUnlinkVerifiedInstallW_VacateCollisionRefuses(t *testing.T) {
	base, name, verified := w19UVInstallFixture(t, "/uvi-coll", "verified bytes")
	fs := &w43PlantAfterVacReleaseFs{Fs: base, plant: []byte("racer owning the fresh terminal draw")}

	err := UnlinkVerifiedInstall(fs, name, verified)
	require.ErrorIs(t, err, ErrPublishCollision)
	require.ErrorContains(t, err, "occupant preserved byte-intact")
	require.Equal(t, "verified bytes", string(w38Read(t, base, name)), "the verified object never moved")
}

func TestUnlinkVerifiedInstallW_VacateVanishedRefuses(t *testing.T) {
	base, name, verified := w19UVInstallFixture(t, "/uvi-van", "verified bytes")
	fs := &w19SrcVanishOnVacateFs{Fs: base, src: name}

	err := UnlinkVerifiedInstall(fs, name, verified)
	require.ErrorIs(t, err, ErrTakeAsideVanished)
	require.ErrorContains(t, err, "vanished under the bound unlink")
}

func TestUnlinkVerifiedInstallW_TerminalVanishedRefuses(t *testing.T) {
	base, name, verified := w19UVInstallFixture(t, "/uvi-tvan", "verified bytes")
	fs := &w44VanishVacAfterUnlinkVacateFs{Fs: base, scratch: name}

	err := UnlinkVerifiedInstall(fs, name, verified)
	require.ErrorIs(t, err, ErrTakeAsideVanished)
	require.ErrorContains(t, err, "empty after the vacate")
}

func TestUnlinkVerifiedInstallW_TerminalIndeterminateRewinds(t *testing.T) {
	base, name, verified := w19UVInstallFixture(t, "/uvi-tind", "verified bytes")
	sentinel := errors.New("uvi terminal lstat wedged")
	fs := &w43FailPostVacateLookupFs{Fs: base, err: sentinel}

	err := UnlinkVerifiedInstall(fs, name, verified)
	require.ErrorIs(t, err, sentinel)
	require.NotErrorIs(t, err, ErrTakeAsideRestoreFailed, "the rewind landed on the freed name")
	require.Equal(t, "verified bytes", string(w38Read(t, base, name)), "the unproven object rewound byte-intact")
}

func TestUnlinkVerifiedInstallW_WedgedRemoveReclaimedStrands(t *testing.T) {
	base, name, verified := w19UVInstallFixture(t, "/uvi-wedge", "verified bytes")
	sentinel := errors.New("uvi terminal remove wedged")
	fs := &w44TerminalRemoveFailFs{Fs: base, err: sentinel, fail: -1, plantScratchOnFail: true}

	err := UnlinkVerifiedInstall(fs, name, verified)
	require.ErrorIs(t, err, sentinel)
	require.ErrorIs(t, err, ErrTakeAsideRestoreFailed, "the rewind collided with the racer's reclaim")
	require.Equal(t, "racer on the freed scratch", string(w38Read(t, base, name)),
		"the racer's reclaim is never clobbered")
}

func TestBoundInstallIdentity_Accessors(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/i", []byte("v"), 0o600))
	info, err := base.Stat("/i")
	require.NoError(t, err)

	require.Nil(t, newBoundInstallIdentity(nil, 1, 2, true), "the nil info is a caller-bug nil")
	require.Nil(t, NewWeakBoundInstallIdentity(nil))

	weak := NewWeakBoundInstallIdentity(info)
	require.Same(t, info, weak.FileInfo())
	dev, ino, ok := weak.strongIdentity()
	require.False(t, ok)
	require.Zero(t, dev+ino)
	require.Empty(t, weak.LinkTarget())

	link := NewBoundInstallLinkIdentity(info, "/somewhere")
	require.Equal(t, "/somewhere", link.LinkTarget())
	require.Equal(t, "", NewBoundInstallLinkIdentity(info, "").LinkTarget())

	// accessor nil-receiver tolerance (callers hold possibly-nil legs)
	var nilID *BoundInstallIdentity
	require.Empty(t, nilID.LinkTarget())
	require.Nil(t, nilID.FileInfo())
	dev, ino, ok = nilID.strongIdentity()
	require.False(t, ok)
	require.Zero(t, dev+ino)
}

type wTermRemoveNotExistFs struct{ afero.Fs }

func (f *wTermRemoveNotExistFs) Remove(name string) error {
	if strings.Contains(name, ".vac.") {
		if err := f.Fs.Remove(name); err != nil {
			return err
		}
		return os.ErrNotExist
	}
	return f.Fs.Remove(name)
}

// The terminal vanishing under the final remove (a directory writer deleting
// it between the rebind and the remove) is the vanished sentinel, not a
// completed unlink.
func TestUnlinkVerifiedInstallW_TerminalVanishedAtRemove(t *testing.T) {
	base, name, verified := w19UVInstallFixture(t, "/uvi-tvr", "verified bytes")
	fs := &wTermRemoveNotExistFs{Fs: base}

	err := UnlinkVerifiedInstall(fs, name, verified)
	require.ErrorIs(t, err, ErrTakeAsideVanished)
	require.ErrorContains(t, err, "vanished under the unlink")
}
