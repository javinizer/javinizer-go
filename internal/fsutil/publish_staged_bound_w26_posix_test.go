//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-26 (P2, finding 1) — do not unlink
// an unverified post-publish occupant. The wave-30 recovery displaced the
// mismatched post-publish destination UNCONDITIONALLY on the no-replace leg,
// reasoning the occupant must be the window plant the publish itself
// installed. But the plant may itself be displaced inside the reverify→unlink
// window by a legitimate writer's file created AFTER the publish, and the
// unconditional Remove then destroyed unjournaled bytes. The removal is now
// bound to the plant RECORDED at mismatch detection: the occupant is
// re-verified through the Lstat seam and displaced ONLY when it is still the
// recorded inode; a different occupant (or an indeterminate re-lookup) is a
// typed refusal with the foreign bytes preserved and the caller's backup
// retained. These tests replay the binding's four legs deterministically by
// counting destination lookups through the publishStagedBoundDestLstat seam.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Leg (i): the occupant is STILL the recorded plant at removal time — the
// bound displacement unlinks it and the loop restages/republishes the genuine
// bytes, exactly the wave-30 recovery posture.
func TestPublishStagedBoundW26POSIX_PlantRemovedWhenIdentityMatchesRecord(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			w30SwapPlant(t, src)
			attacked++
		}
		return PublishNoReplace(f, src, dst)
	}
	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err, "the recorded plant's verified displacement keeps the wave-30 recovery")
	require.Equal(t, 1, attacked)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got),
		"the loop restaged from the handle and republished the genuine bytes")
	entries, derr := os.ReadDir(dir)
	require.NoError(t, derr)
	for _, e := range entries {
		body, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NotContains(t, string(body), "foreign window plant",
			"the recorded plant was displaced by the bound unlink")
	}
	away, rerr := os.ReadFile(staged + ".w30-away")
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(away),
		"the genuine inode stays reachable under the attacker's name until unlinked by its owner")
}

// Leg (ii): between the mismatch detection and the recovery unlink a
// LEGITIMATE writer displaced the plant with its own file — a different
// inode than BOTH the staged handle and the recorded plant. The occupant is
// never unlinked; the refusal is typed and nothing is consumed.
func TestPublishStagedBoundW26POSIX_LegitimateOccupantAfterPublishIsNeverUnlinked(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)
	// Pre-create the legitimate occupant: rename-over swaps the inode for a
	// provably distinct one (CI filesystems reuse freed inodes on
	// remove+create at the same path).
	legit := filepath.Join(dir, "legit-writer.jpg")
	const legitBytes = "a legitimate writer created me after the publish"
	require.NoError(t, os.WriteFile(legit, []byte(legitBytes), 0o644))

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			w30SwapPlant(t, src)
			attacked++
		}
		return PublishNoReplace(f, src, dst)
	}
	lookups := 0
	prevLookup := publishStagedBoundDestLstat
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		if name == dest {
			lookups++
			if lookups == 2 {
				// The reverify→unlink window, replayed at the binding lookup:
				// the plant is displaced by the legitimate writer's file.
				require.NoError(t, os.Rename(legit, dest))
			}
		}
		return prevLookup(name)
	}
	t.Cleanup(func() { publishStagedBoundDestLstat = prevLookup })

	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishStagedForeignOccupant,
		"the occupant provably differs from the recorded plant — typed foreign-occupant refusal")
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak,
		"the refusal family rides the identity-break classifier")
	require.NotErrorIs(t, err, ErrPublishStagedExhausted)
	require.Equal(t, 2, lookups, "detection + binding lookup, then the refusal — no retry over foreign bytes")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, legitBytes, string(got),
		"the legitimate post-publish occupant is preserved byte-intact — the finding's lost-write is closed")
	away, rerr := os.ReadFile(staged + ".w30-away")
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(away),
		"the genuine inode survives under the attacker's name — nothing was consumed")
}

// Leg (iv): an INDETERMINATE binding re-lookup refuses typed through the
// identity-break class and removes NOTHING — the recorded plant itself stays
// at dest for later arbitration.
func TestPublishStagedBoundW26POSIX_BindingLookupIndeterminateRefuses(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			w30SwapPlant(t, src)
			attacked++
		}
		return PublishNoReplace(f, src, dst)
	}
	lookupDenied := errors.New("w26 binding lookup indeterminate")
	lookups := 0
	prevLookup := publishStagedBoundDestLstat
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		if name == dest {
			lookups++
			if lookups == 2 {
				return nil, lookupDenied
			}
		}
		return prevLookup(name)
	}
	t.Cleanup(func() { publishStagedBoundDestLstat = prevLookup })

	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
	require.ErrorIs(t, err, lookupDenied, "the binding lookup failure stays unwrap-reachable")
	require.NotErrorIs(t, err, ErrPublishStagedForeignOccupant,
		"indeterminate is not proven-foreign — a distinct refusal")
	require.NotErrorIs(t, err, ErrPublishStagedExhausted)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "foreign window plant", string(got),
		"the recorded plant is never removed on an indeterminate binding answer")
}

// Leg (iii): the recorded plant VANISHED inside the reverify→unlink window —
// nothing to displace, so the loop skips the removal and recovers exactly
// like the wave-30 vanish leg.
func TestPublishStagedBoundW26POSIX_PlantVanishedBeforeBindingRestages(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			w30SwapPlant(t, src)
			attacked++
		}
		return PublishNoReplace(f, src, dst)
	}
	lookups := 0
	prevLookup := publishStagedBoundDestLstat
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		if name == dest {
			lookups++
			if lookups == 2 {
				require.NoError(t, os.Remove(dest),
					"the racer unlinks the recorded plant inside the binding window")
			}
		}
		return prevLookup(name)
	}
	t.Cleanup(func() { publishStagedBoundDestLstat = prevLookup })

	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err, "a vanished plant is the vanish leg: restage from the handle and republish")
	require.Equal(t, 3, lookups, "detection + binding-vanish + the successful republish's reverify")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got))
	away, rerr := os.ReadFile(staged + ".w30-away")
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(away))
}
