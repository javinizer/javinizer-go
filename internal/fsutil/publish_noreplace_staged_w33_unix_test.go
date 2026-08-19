//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING wave-33 (codex local review round 3, PR#215 finding
// R2): the hard-link fallback's staged-source cleanup used to unlink src by
// pathname right after link(2) succeeded — a writer swapping the staged name
// in the link→unlink window lost ITS foreign bytes to our cleanup. The staged
// unlink is now IDENTITY-BOUND to the just-linked object: the pre-link
// no-follow Lstat snapshot (dev/ino via os.SameFile + size/mtime) must
// re-prove the staged name before any removal. A swapped/re-stamped staged
// name stays BYTE-INTACT with a typed ErrPublishNoReplaceStagedUnverified
// refusal (joined with ErrPublishCompleted — the destination provably carries
// the published bytes); a staged name that vanished on its own completes the
// cleanup by itself (plain success); an indeterminate reverify keeps the
// name with the same typed refusal.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// w33Stage builds one real staged file publishable to one absent destination.
func w33Stage(t *testing.T) (dir, src, dst string) {
	t.Helper()
	dir = t.TempDir()
	src = filepath.Join(dir, "staged.bin")
	dst = filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(src, []byte("staged bytes"), 0o644))
	return dir, src, dst
}

// Happy path: the reverify proves the staged name still names the linked
// object, the staged unlink consumes it, and nil returns.
func TestPublishNoReplaceW33_StagedCleanupUnlinksOwnSource(t *testing.T) {
	_, src, dst := w33Stage(t)

	require.NoError(t, publishNoReplaceFallback(src, dst))
	require.Equal(t, []byte("staged bytes"), mustW32Read(t, dst))
	_, serr := os.Lstat(src)
	require.ErrorIs(t, serr, os.ErrNotExist, "the staged name is consumed by the publish")
}

// The finding's core case — a REAL swap inside the link→unlink window,
// replayed through the link seam (a pre-created foreign inode is renamed over
// the staged name right after link(2) lands, mirroring the CI inode-reuse
// discipline of wave-26): the fallback must REFUSE the unlink, keep the
// foreign occupant byte-intact, and surface the typed refusal with the
// completed marker (the destination stands with the published bytes).
func TestPublishNoReplaceW33_StagedSwapMidWindowKeepsForeignBytes(t *testing.T) {
	dir, src, dst := w33Stage(t)
	foreign := filepath.Join(dir, "foreign.bin")
	require.NoError(t, os.WriteFile(foreign, []byte("FOREIGN swap-victim bytes — never ours"), 0o644))

	prev := publishNoReplaceLink
	publishNoReplaceLink = func(s, d string) error {
		if err := os.Link(s, d); err != nil {
			return err
		}
		// The staged name is swapped for the pre-created foreign inode in the
		// link→unlink window (rename semantics — no freed-inode reuse).
		return os.Rename(foreign, s)
	}
	t.Cleanup(func() { publishNoReplaceLink = prev })

	err := publishNoReplaceFallback(src, dst)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPublishNoReplaceStagedUnverified)
	require.ErrorIs(t, err, ErrPublishCompleted, "the destination provably stands — pending-kind classifiers keep the owned-name routing")
	require.NotErrorIs(t, err, ErrPublishCollision)

	require.Equal(t, []byte("staged bytes"), mustW32Read(t, dst), "the published destination keeps the staged bytes")
	require.Equal(t, []byte("FOREIGN swap-victim bytes — never ours"), mustW32Read(t, src),
		"the foreign occupant at the staged name is NEVER unlinked")
}

// An INDETERMINATE reverify proves nothing about the staged name: keep it,
// keep the published destination, refuse typed.
func TestPublishNoReplaceW33_StagedIndeterminateReverifyRefused(t *testing.T) {
	_, src, dst := w33Stage(t)
	reverifyErr := errors.New("w33 staged reverify wedged")
	prevV := publishNoReplaceStagedVerify
	publishNoReplaceStagedVerify = func(string, os.FileInfo) (bool, error) { return false, reverifyErr }
	t.Cleanup(func() { publishNoReplaceStagedVerify = prevV })

	err := publishNoReplaceFallback(src, dst)
	require.ErrorIs(t, err, reverifyErr)
	require.ErrorIs(t, err, ErrPublishNoReplaceStagedUnverified)
	require.ErrorIs(t, err, ErrPublishCompleted)
	require.Equal(t, []byte("staged bytes"), mustW32Read(t, src), "an unanswerable reverify never unlinks")
	require.Equal(t, []byte("staged bytes"), mustW32Read(t, dst))
}

// A staged name that VANISHED on its own after the link completed the cleanup
// by itself: plain success, the published destination stands.
func TestPublishNoReplaceW33_StagedVanishedMidWindowSucceeds(t *testing.T) {
	_, src, dst := w33Stage(t)
	prev := publishNoReplaceLink
	publishNoReplaceLink = func(s, d string) error {
		if err := os.Link(s, d); err != nil {
			return err
		}
		return os.Remove(s)
	}
	t.Cleanup(func() { publishNoReplaceLink = prev })

	require.NoError(t, publishNoReplaceFallback(src, dst))
	require.Equal(t, []byte("staged bytes"), mustW32Read(t, dst))
	_, serr := os.Lstat(src)
	require.ErrorIs(t, serr, os.ErrNotExist)
}

// A staged source that cannot even be identified BEFORE the link fails
// closed: nothing is linked, nothing is unlinked, the refusal classifies like
// a missing-source link failure.
func TestPublishNoReplaceW33_MissingStagedSourceRefusedBeforeLink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "never-staged.bin")
	dst := filepath.Join(dir, "poster.jpg")

	err := publishNoReplaceFallback(src, dst)
	require.ErrorIs(t, err, ErrPublishNoReplaceLinkFailed)
	require.NotErrorIs(t, err, ErrPublishCollision)
	_, derr := os.Lstat(dst)
	require.ErrorIs(t, derr, os.ErrNotExist, "nothing published")
}

// The DEFAULT staged-verify closure's own legs (every other wave-33 leg runs
// it naturally, but each answer arm is pinned directly): a missing staged
// name fails the Lstat arm; a swapped-in occupant (different inode) answers
// false without error; a same-inode restamp (mtime changed, or size changed
// with the mtime doctored back) reads as unproven; the untouched pair
// verifies.
func TestPublishNoReplaceStagedVerifyW33_RealClosureLegs(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "staged.bin")
	require.NoError(t, os.WriteFile(src, []byte("staged bytes"), 0o644))
	before, err := os.Lstat(src)
	require.NoError(t, err)

	// Untouched: the staged name still names exactly the snapshotted object.
	ok, err := publishNoReplaceStagedVerify(src, before)
	require.NoError(t, err)
	require.True(t, ok, "the untouched staged name verifies")

	// Missing staged name: the Lstat arm fails typed.
	ok, err = publishNoReplaceStagedVerify(filepath.Join(dir, "missing.bin"), before)
	require.False(t, ok)
	require.ErrorIs(t, err, os.ErrNotExist)

	// Different occupant at the staged name (rename-swap, pre-created inode):
	// SameFile answers false without error.
	occupant := filepath.Join(dir, "occupant.bin")
	require.NoError(t, os.WriteFile(occupant, []byte("occupant"), 0o644))
	ok, err = publishNoReplaceStagedVerify(occupant, before)
	require.NoError(t, err)
	require.False(t, ok, "a different inode never verifies")

	// Same inode, re-stamped mtime: mutated mid-window — unproven.
	restamped := filepath.Join(dir, "restamped.bin")
	require.NoError(t, os.WriteFile(restamped, []byte("stamped"), 0o644))
	rBefore, err := os.Lstat(restamped)
	require.NoError(t, err)
	require.NoError(t, os.Chtimes(restamped, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)))
	ok, err = publishNoReplaceStagedVerify(restamped, rBefore)
	require.NoError(t, err)
	require.False(t, ok, "a changed mtime never verifies")

	// Same inode, same mtime doctored back, different size: unproven.
	resized := filepath.Join(dir, "resized.bin")
	require.NoError(t, os.WriteFile(resized, []byte("a"), 0o644))
	zBefore, err := os.Lstat(resized)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(resized, []byte("grown-bytes"), 0o644))
	require.NoError(t, os.Chtimes(resized, zBefore.ModTime(), zBefore.ModTime()))
	ok, err = publishNoReplaceStagedVerify(resized, zBefore)
	require.NoError(t, err)
	require.False(t, ok, "a changed size never verifies")
}
