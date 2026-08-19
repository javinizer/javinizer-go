//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING wave-38 (codex P2, PR#215 finding F1) — the POSIX
// no-replace recovery displaces a destination occupant ONLY when tied to
// PRE-PUBLISH existence evidence: a publish attempt that refused with
// ErrPublishCollision proves its obstacle pre-dates the attempt. These legs
// drive displacePrePublishCollisionPlant through the destination-lookup seam
// (record → binding re-proof → bound unlink) and the loop's retry budget.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// The headline (a) channel: a pre-publish plant collides the no-replace
// publish, gets recorded at the collision instant, and is displaced after
// the bound re-proof — the retried publish lands the GENUINE bytes.
func TestPublishStagedBoundW38POSIX_CollisionPlantDisplacedAndRepublished(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			attacked++
			// The plant claims dest BEFORE the publish attempt: the publish
			// collides, tying the occupant to pre-publish evidence.
			require.NoError(t, os.WriteFile(dst, []byte("foreign window plant"), 0o644))
		}
		return PublishNoReplace(f, src, dst)
	}
	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err, "the evidence-tied obstacle is displaced and the publish retried")
	require.Equal(t, 1, attacked)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got))
	entries, derr := os.ReadDir(dir)
	require.NoError(t, derr)
	for _, e := range entries {
		body, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NotContains(t, string(body), "foreign window plant",
			"the recorded pre-publish obstacle was displaced by the bound unlink")
	}
}

// The recorded obstacle VANISHES between the collision and the record lookup
// — nothing to displace; the retried publish lands into proven absence.
func TestPublishStagedBoundW38POSIX_CollisionPlantVanishedAtRecordRepublishes(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			attacked++
			require.NoError(t, os.WriteFile(dst, []byte("foreign window plant"), 0o644))
		}
		return PublishNoReplace(f, src, dst)
	}
	lookups := 0
	prevLookup := publishStagedBoundDestLstat
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		if name == dest {
			lookups++
			if lookups == 1 {
				require.NoError(t, os.Remove(dest), "the plant vanishes before the record lookup")
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
	require.NoError(t, err, "a vanished obstacle is the vanish leg — republish into absence")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got))
}

// An INDETERMINATE record lookup refuses typed — nothing is proven about the
// name, nothing removed, the staged name retained for the caller.
func TestPublishStagedBoundW38POSIX_CollisionRecordIndeterminateRefuses(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			attacked++
			require.NoError(t, os.WriteFile(dst, []byte("foreign window plant"), 0o644))
		}
		return PublishNoReplace(f, src, dst)
	}
	lookupDenied := errors.New("w38 collision record indeterminate")
	lookups := 0
	prevLookup := publishStagedBoundDestLstat
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		if name == dest {
			lookups++
			if lookups == 1 {
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
	require.ErrorIs(t, err, lookupDenied, "the record-lookup failure stays unwrap-reachable")
	require.NotErrorIs(t, err, ErrPublishStagedForeignOccupant)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "foreign window plant", string(got), "the plant is never removed on an indeterminate record")
	content, serr := os.ReadFile(staged)
	require.NoError(t, serr)
	require.Equal(t, "genuine staged bytes", string(content), "the staged name survives for the caller")
}

// The binding re-lookup names a DIFFERENT object than the recorded plant: a
// successor claimed dest inside the record→unlink window — typed
// foreign-occupant refusal, the successor preserved byte-intact.
func TestPublishStagedBoundW38POSIX_CollisionSuccessorAtBindingPreserved(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)
	// Pre-create the successor: rename-over keeps a provably distinct inode
	// (CI filesystems reuse freed inodes on remove+create at the same path).
	successor := filepath.Join(dir, "successor.jpg")
	const successorBytes = "a legitimate successor claimed dest inside the binding window"
	require.NoError(t, os.WriteFile(successor, []byte(successorBytes), 0o644))

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			attacked++
			require.NoError(t, os.WriteFile(dst, []byte("foreign window plant"), 0o644))
		}
		return PublishNoReplace(f, src, dst)
	}
	lookups := 0
	prevLookup := publishStagedBoundDestLstat
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		if name == dest {
			lookups++
			if lookups == 2 {
				require.NoError(t, os.Rename(successor, dest))
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
		"the binding names a successor — preserved, never the recorded-plant delete")
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, successorBytes, string(got), "the successor's bytes survive byte-intact")
	content, serr := os.ReadFile(staged)
	require.NoError(t, serr)
	require.Equal(t, "genuine staged bytes", string(content), "the staged name survives for the caller")
}

// An INDETERMINATE binding re-lookup refuses typed with the recorded plant
// itself preserved.
func TestPublishStagedBoundW38POSIX_CollisionBindingIndeterminateRefuses(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			attacked++
			require.NoError(t, os.WriteFile(dst, []byte("foreign window plant"), 0o644))
		}
		return PublishNoReplace(f, src, dst)
	}
	lookupDenied := errors.New("w38 collision binding indeterminate")
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
	require.ErrorIs(t, err, lookupDenied, "the binding-lookup failure stays unwrap-reachable")
	require.NotErrorIs(t, err, ErrPublishStagedForeignOccupant,
		"indeterminate is not proven-foreign — a distinct refusal")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "foreign window plant", string(got))
}

// The obstacle vanishes between the record and the binding re-lookup —
// nothing to displace; the retried publish lands into proven absence.
func TestPublishStagedBoundW38POSIX_CollisionPlantVanishedAtBindingRepublishes(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			attacked++
			require.NoError(t, os.WriteFile(dst, []byte("foreign window plant"), 0o644))
		}
		return PublishNoReplace(f, src, dst)
	}
	lookups := 0
	prevLookup := publishStagedBoundDestLstat
	publishStagedBoundDestLstat = func(name string) (os.FileInfo, error) {
		if name == dest {
			lookups++
			if lookups == 2 {
				require.NoError(t, os.Remove(dest), "the plant vanishes inside the binding window")
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
	require.NoError(t, err, "a vanished obstacle leaves the retry to publish into absence")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got))
}

// A FAILED bound unlink refuses typed — the loop never proceeds to a
// republish over the surviving (though evidence-tied) occupant.
func TestPublishStagedBoundW38POSIX_CollisionUnlinkFailureRefuses(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			attacked++
			require.NoError(t, os.WriteFile(dst, []byte("foreign window plant"), 0o644))
		}
		return PublishNoReplace(f, src, dst)
	}
	removeWedged := errors.New("w38 bound unlink wedged")
	prevFsRemove := publishStagedBoundOsRemove
	publishStagedBoundOsRemove = func(fs afero.Fs, name string) error {
		if name == dest {
			return removeWedged
		}
		return fs.Remove(name)
	}
	t.Cleanup(func() { publishStagedBoundOsRemove = prevFsRemove })

	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
	require.ErrorIs(t, err, removeWedged, "the bound unlink failure stays unwrap-reachable")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "foreign window plant", string(got),
		"the surviving obstacle is never republished over")
}

// The displacement's Remove answers ENOENT — the recorded plant vanished
// inside the re-proof→unlink window: tolerated, retry publishes into absence.
func TestPublishStagedBoundW38POSIX_CollisionUnlinkEnoentTolerated(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			attacked++
			require.NoError(t, os.WriteFile(dst, []byte("foreign window plant"), 0o644))
		}
		return PublishNoReplace(f, src, dst)
	}
	prevFsRemove := publishStagedBoundOsRemove
	publishStagedBoundOsRemove = func(fs afero.Fs, name string) error {
		if name == dest {
			// The racer beat the bound unlink: remove first, answer ENOENT.
			require.NoError(t, fs.Remove(name))
			return fs.Remove(name)
		}
		return fs.Remove(name)
	}
	t.Cleanup(func() { publishStagedBoundOsRemove = prevFsRemove })

	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.NoError(t, err)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "genuine staged bytes", string(got))
}

// A persistent post-publish VANISH: every publish lands the genuine bytes and
// a racer immediately unlinks them again — the loop restages from the handle
// until the budget exhausts, refusing typed with nothing consumed (drives the
// restage-side budget cap; the collision-side cap lives in the persistent
// pre-publish collision test).
func TestPublishStagedBoundW38POSIX_PersistentVanishExhaustsRestageBudget(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	publishes := 0
	wedge := func(f afero.Fs, src, dst string) error {
		publishes++
		if err := PublishNoReplace(f, src, dst); err != nil {
			return err
		}
		require.NoError(t, os.Remove(dst), "the racer unlinks the just-published name before every reverify")
		return nil
	}
	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishStagedExhausted)
	require.ErrorIs(t, err, ErrPublishStagedIdentityBreak)
	require.Equal(t, PublishStagedBoundAttempts, publishes, "one publish per budgeted attempt")
	_, lerr := os.Lstat(dest)
	require.ErrorIs(t, lerr, os.ErrNotExist, "the vanishing racer leaves dest free")
}

// A collision-class publish error under REPLACE semantics is never displaced
// — the error passes through verbatim (the no-replace pre-publish tie is
// meaningless where replacing was the operation's meaning).
func TestPublishStagedBoundW38POSIX_ReplaceSemanticsCollisionPassthrough(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	err := PublishStagedBound(StagedPublish{
		FS: fs, NoReplace: false,
		Publish: func(f afero.Fs, src, dst string) error { return publishCollision(dst) },
		Staged:  staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishCollision, "the publish's own class surfaces verbatim")
	_, lerr := os.Lstat(dest)
	require.ErrorIs(t, lerr, os.ErrNotExist, "nothing was planted or displaced")
	content, serr := os.ReadFile(staged)
	require.NoError(t, serr)
	require.Equal(t, "genuine staged bytes", string(content), "the staged name survives for the caller")
}
