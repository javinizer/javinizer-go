//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-26 (P2, finding 1) + wave-38 (P2,
// finding F1) — do not unlink an unverified post-publish occupant. The
// wave-30 recovery displaced the mismatched post-publish destination
// UNCONDITIONALLY on the no-replace leg; wave-26 bound the unlink to the
// object recorded at mismatch detection; wave-38 CLOSED the remaining hole:
// a successful no-replace publish proves the destination was free at the
// rename instant, so the recorded occupant — even when it still sits there
// at the binding lookup — may be a legitimate file created inside the
// publish→first-Lstat gap, indistinguishable from the staged-name plant the
// publish moved over. Post-publish mismatch occupants are therefore NEVER
// unlinked now: a still-present occupant is a typed
// ErrPublishStagedForeignOccupant refusal with the bytes preserved and the
// caller's backup retained; recovery continues only when the occupant
// VANISHED (dest free — the wave-30 vanish leg). Wave-49 removed the last
// displacement channel too: a collision winner's bytes prevail and the
// refusal surfaces verbatim. These tests replay the binding's legs
// deterministically by counting destination lookups through the
// publishStagedBoundDestLstat seam.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Leg (i): the occupant is STILL the recorded plant at the binding
// re-lookup. Pre-wave-38 the bound unlink deleted it; wave-38 (finding F1)
// proves that unsafe — a successful no-replace publish means the kernel
// found dest free at the rename instant, so the recorded object arrived
// afterwards: the staged-name plant the publish itself moved over and a
// LEGITIMATE gap file are indistinguishable here. The occupant is preserved
// byte-intact with a typed ErrPublishStagedForeignOccupant refusal; nothing
// is consumed and the genuine inode stays reachable under the attacker's
// name (for its owner to arbitrate).
func TestPublishStagedBoundW26POSIX_PlantPreservedWhenUntriedToPrePublishEvidence(t *testing.T) {
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
	require.ErrorIs(t, err, ErrPublishStagedForeignOccupant,
		"the still-present post-publish occupant is a typed foreign-occupant refusal, never a removal")
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
	require.NotErrorIs(t, err, ErrPublishStagedExhausted)
	require.Equal(t, 1, attacked, "the recovery stopped AT the mismatch — no republish over unverified bytes")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "foreign window plant", string(got),
		"the plant (or a legitimate gap file, indistinguishable here) survives byte-intact")
	away, rerr := os.ReadFile(staged + ".w30-away")
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(away),
		"the genuine inode stays reachable under the attacker's name until unlinked by its owner")
}

// Leg (ii): between the mismatch detection and the binding re-lookup a
// LEGITIMATE writer displaced the plant with its own file — a different
// inode than BOTH the staged handle and the recorded plant. The occupant is
// never unlinked; the refusal is typed and nothing is consumed (wave-38:
// identical posture to leg (i) — no post-publish occupant is ever removed).
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
// identity-break class and removes NOTHING — the recorded occupant stays at
// dest for later arbitration.
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

// Leg (iii): the recorded occupant VANISHED inside the detection→recovery
// window — nothing stands at dest, so the loop recovers exactly like the
// wave-30 vanish leg (restage from the handle, republish into proven
// absence):
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
