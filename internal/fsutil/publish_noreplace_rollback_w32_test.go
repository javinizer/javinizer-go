//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING wave-32 (codex local review round 2, PR#215 finding
// R3): the hard-link fallback's staged-source-cleanup rollback used to
// unlink dst by pathname sight-unseen — a foreign replacement claiming dst
// between link(2) and the rollback was deleted. The rollback now re-proves
// dst still names the just-linked inode (Lstat vs the source's own Stat
// identity) before ANY removal: a foreign occupant or an indeterminate
// reverify refuses typed with the destination untouched and WITHOUT the
// ErrPublishCompleted marker (the name is not provably ours — pending-kind
// classifiers route to rearm-refused), while a vanished destination keeps
// the plain staged-cleanup error.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// w32FallbackStage builds one real src file publishable to one absent dst.
func w32FallbackStage(t *testing.T) (src, dst string) {
	t.Helper()
	dir := t.TempDir()
	src = filepath.Join(dir, "staged.bin")
	dst = filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(src, []byte("staged bytes"), 0o644))
	return src, dst
}

// w32WedgeStagedRemoveFails wedges the staged-source unlink ONLY.
func w32WedgeStagedRemoveFails(t *testing.T, src string, sentinel error) {
	t.Helper()
	prev := publishNoReplaceRemove
	publishNoReplaceRemove = func(name string) error {
		if name == src {
			return sentinel
		}
		return os.Remove(name)
	}
	t.Cleanup(func() { publishNoReplaceRemove = prev })
}

// w32WedgeRollbackVerify substitutes the rollback re-proof.
func w32WedgeRollbackVerify(t *testing.T, fn func(src, dst string) (bool, error)) {
	t.Helper()
	prev := publishNoReplaceRollbackVerify
	publishNoReplaceRollbackVerify = fn
	t.Cleanup(func() { publishNoReplaceRollbackVerify = prev })
}

// Happy rollback: dst re-proven as the just-linked inode, the unlink undoes
// the publish, and only the staged-cleanup error returns.
func TestPublishNoReplaceW32_RollbackVerifiedRemovesLinkedDest(t *testing.T) {
	src, dst := w32FallbackStage(t)
	sentinel := errors.New("w32 staged remove wedged")
	w32WedgeStagedRemoveFails(t, src, sentinel)

	err := publishNoReplaceFallback(src, dst)
	require.ErrorIs(t, err, sentinel)
	require.NotErrorIs(t, err, ErrPublishNoReplaceRollbackUnverified)
	require.NotErrorIs(t, err, ErrPublishCompleted)
	_, serr := os.Stat(dst)
	require.ErrorIs(t, serr, os.ErrNotExist, "the verified rollback removed the linked destination")
	_, rerr := os.Stat(src)
	require.NoError(t, rerr, "the staged source survives (its own unlink was wedged)")
}

// Foreign occupant at dst: never deleted, typed refusal, no completed marker.
func TestPublishNoReplaceW32_RollbackForeignOccupantRefused(t *testing.T) {
	src, dst := w32FallbackStage(t)
	sentinel := errors.New("w32 staged remove wedged")
	w32WedgeStagedRemoveFails(t, src, sentinel)
	w32WedgeRollbackVerify(t, func(_, _ string) (bool, error) { return false, nil })

	err := publishNoReplaceFallback(src, dst)
	require.ErrorIs(t, err, sentinel)
	require.ErrorIs(t, err, ErrPublishNoReplaceRollbackUnverified)
	require.NotErrorIs(t, err, ErrPublishCompleted)
	require.Equal(t, []byte("staged bytes"), mustW32Read(t, dst),
		"the destination keeps the just-linked bytes — the permissionless unlink never ran")
}

// Indeterminate reverify: typed refusal, destination untouched.
func TestPublishNoReplaceW32_RollbackIndeterminateReverifyRefused(t *testing.T) {
	src, dst := w32FallbackStage(t)
	sentinel := errors.New("w32 staged remove wedged")
	reverifyErr := errors.New("w32 reverify wedged")
	w32WedgeStagedRemoveFails(t, src, sentinel)
	w32WedgeRollbackVerify(t, func(_, _ string) (bool, error) { return false, reverifyErr })

	err := publishNoReplaceFallback(src, dst)
	require.ErrorIs(t, err, sentinel)
	require.ErrorIs(t, err, reverifyErr)
	require.ErrorIs(t, err, ErrPublishNoReplaceRollbackUnverified)
	require.NotErrorIs(t, err, ErrPublishCompleted)
	require.Equal(t, []byte("staged bytes"), mustW32Read(t, dst))
}

// Vanished destination: nothing foreign could be deleted; the plain
// staged-cleanup error stands (no unverified marker, no completed marker).
func TestPublishNoReplaceW32_RollbackDestVanishedKeepsPlainError(t *testing.T) {
	src, dst := w32FallbackStage(t)
	sentinel := errors.New("w32 staged remove wedged")
	w32WedgeStagedRemoveFails(t, src, sentinel)
	w32WedgeRollbackVerify(t, func(_, _ string) (bool, error) { return false, os.ErrNotExist })

	err := publishNoReplaceFallback(src, dst)
	require.ErrorIs(t, err, sentinel)
	require.NotErrorIs(t, err, ErrPublishNoReplaceRollbackUnverified)
	require.NotErrorIs(t, err, ErrPublishCompleted)
}

// Verified-but-unremovable: dst re-proven as ours, yet its unlink fails —
// the established ErrPublishCompleted contract with the verified rollback.
func TestPublishNoReplaceW32_RollbackFailureAfterVerifiedStillCompletedClass(t *testing.T) {
	src, dst := w32FallbackStage(t)
	stagedSentinel := errors.New("w32 staged remove wedged")
	rollbackSentinel := errors.New("w32 rollback remove wedged")
	prev := publishNoReplaceRemove
	publishNoReplaceRemove = func(name string) error {
		switch name {
		case src:
			return stagedSentinel
		case dst:
			return rollbackSentinel
		}
		return os.Remove(name)
	}
	t.Cleanup(func() { publishNoReplaceRemove = prev })

	err := publishNoReplaceFallback(src, dst)
	require.ErrorIs(t, err, ErrPublishCompleted, "the verified destination keeps the staged bytes when its unlink fails")
	require.ErrorIs(t, err, rollbackSentinel)
	require.Equal(t, []byte("staged bytes"), mustW32Read(t, dst))
}

// The DEFAULT rollback reverify closure's own legs (every other wave-32
// leg substitutes the seam, so the production body is exercised directly):
// a missing staged source fails the Stat arm, a missing destination fails
// the Lstat arm, a linked pair answers SameFile true, and an independent
// occupant answers false without error.
func TestPublishNoReplaceRollbackVerifyW32_RealClosureLegs(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "staged.bin")
	dst := filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(src, []byte("staged bytes"), 0o644))

	// Destination missing: the Lstat arm fails typed.
	ok, err := publishNoReplaceRollbackVerify(src, dst)
	require.False(t, ok)
	require.ErrorIs(t, err, os.ErrNotExist)

	// Staged source missing: the Stat arm fails typed.
	ok, err = publishNoReplaceRollbackVerify(filepath.Join(dir, "no-such-src"), dst)
	require.False(t, ok)
	require.ErrorIs(t, err, os.ErrNotExist)

	// An independent occupant at dst is not the just-linked inode.
	require.NoError(t, os.WriteFile(dst, []byte("foreign occupant"), 0o644))
	ok, err = publishNoReplaceRollbackVerify(src, dst)
	require.NoError(t, err)
	require.False(t, ok, "a different inode never verifies")

	// The just-linked pair verifies.
	require.NoError(t, os.Remove(dst))
	require.NoError(t, os.Link(src, dst))
	ok, err = publishNoReplaceRollbackVerify(src, dst)
	require.NoError(t, err)
	require.True(t, ok)
}

func mustW32Read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
