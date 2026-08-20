//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING wave-38 (codex P2, PR#215 finding F1) + wave-49
// (codex P2, PR#215 — "preserve files that win the no-replace race"): the
// POSIX no-replace recovery NEVER deletes a destination occupant. Wave-38
// preserved the post-publish SUCCESS→mismatch occupant (typed
// ErrPublishStagedForeignOccupant); wave-49 extends the same preservation to
// the PRE-publish race: a publish attempt that refused with
// ErrPublishCollision names a racer that WON the no-replace race —
// 'no-replace' means creating only when absent, so the wave-38
// record-then-displace-then-retry leg contradicts the primitive's intent (a
// legitimate writer racing in had its bytes destroyed with no backup and no
// ledger entry). Collisions now surface VERBATIM for the caller's
// wave-15/wave-17 reclassification legs; the restage loop keeps only the
// post-publish vanish/mismatch recovery.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// The headline wave-49 refusal: a pre-publish plant collides the no-replace
// publish — the racer WON, so the collision class surfaces verbatim, the
// winner's bytes are preserved byte-intact, the staged name survives for the
// caller, and no displace-then-retry ever runs.
func TestPublishStagedBoundW38POSIX_CollisionSurfacesVerbatimPlantPreserved(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged, fh := w30Stage(t, fs, dest, ".rstr", 0o640)

	attacked := 0
	wedge := func(f afero.Fs, src, dst string) error {
		if attacked == 0 {
			attacked++
			// The racer claims dest BEFORE the publish attempt: the publish
			// collides, and the winner's bytes must prevail.
			require.NoError(t, os.WriteFile(dst, []byte("foreign window plant"), 0o644))
		}
		return PublishNoReplace(f, src, dst)
	}
	err := PublishStagedBound(StagedPublish{
		FS: fs, Publish: wedge, NoReplace: true,
		Staged: staged, Handle: fh, Dest: dest,
		Suffix: ".rstr", NextOrdinal: w30Ordinal(4),
	})
	require.ErrorIs(t, err, ErrPublishCollision,
		"the collision winner's race surfaces as the verbatim no-replace refusal")
	require.NotErrorIs(t, err, ErrPublishStagedIdentityBreak,
		"a plain collision is NOT an identity break — the publish never ran")
	require.NotErrorIs(t, err, ErrPublishStagedForeignOccupant)
	require.Equal(t, 1, attacked, "no displace-then-retry: exactly one publish attempt ran")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "foreign window plant", string(got),
		"the collision winner's bytes are preserved byte-intact — no-replace means create-only-when-absent")
	content, serr := os.ReadFile(staged)
	require.NoError(t, serr)
	require.Equal(t, "genuine staged bytes", string(content),
		"the staged name survives for the caller's reclassification")
	_, cerr := fh.Stat()
	require.Error(t, cerr, "the bound publish still consumed (closed) the handle")
}

// A persistent post-publish VANISH: every publish lands the genuine bytes and
// a racer immediately unlinks them again — the loop restages from the handle
// until the budget exhausts, refusing typed with nothing consumed (drives the
// restage-side budget cap).
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

// A collision-class publish error under REPLACE semantics passes through
// verbatim identically (the no-replace proven-absence posture is
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
