//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-16 (coverage) — the hard-link
// fallback (publish_noreplace_unix.go) is only REACHED at runtime on
// non-Linux POSIX hosts (Darwin) and on Linux kernels/filesystems that
// reject RENAME_NOREPLACE, so the coverage-uploading Linux test job never
// executed its legs via PublishNoReplace. These tests drive the fallback
// directly on any POSIX host — real syscalls for the reachable orderings,
// the publishNoReplaceLink / publishNoReplaceRemove seams for the failure
// orderings a host kernel cannot be coerced into mid-call — pinning its
// full contract: EEXIST maps to the typed collision, a non-EEXIST link error
// refuses TYPED (wave-29 fail-closed — never a classified-rename degrade),
// and every staged-cleanup failure publishes closed (destination rolled back,
// staged source intact).

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The happy link publish: the destination is created by the link alone and
// the staged name is consumed, exactly as rename would leave them.
func TestPublishNoReplaceFallbackW16_HappyLinkPublishes(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "staged.tmp")
	dst := filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))

	require.NoError(t, publishNoReplaceFallback(src, dst))
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "staged", string(got))
	_, err = os.Stat(src)
	require.ErrorIs(t, err, os.ErrNotExist, "the staged name is consumed by the publish")
}

// An occupied destination collides at link(2) with the typed class; neither
// side's bytes move.
func TestPublishNoReplaceFallbackW16_OccupiedDestinationCollides(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "staged.tmp")
	dst := filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))
	require.NoError(t, os.WriteFile(dst, []byte("racer"), 0o644))

	err := publishNoReplaceFallback(src, dst)
	require.ErrorIs(t, err, ErrPublishCollision)
	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	require.Equal(t, "racer", string(got), "the existing destination is never touched")
}

// A non-EEXIST link failure (missing staged source here) refuses TYPED
// instead of masquerading as a collision, silently succeeding, or —
// pre-wave-29 — degrading into the non-atomic classified rename leg
// (wave-29, codex P2, PR#215): the original errno stays unwrap-reachable
// behind ErrPublishNoReplaceLinkFailed and nothing is ever published.
func TestPublishNoReplaceFallbackW16_LinkErrorRefusesTyped(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "never-staged.tmp")
	dst := filepath.Join(dir, "poster.jpg")

	err := publishNoReplaceFallback(src, dst)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPublishNoReplaceLinkFailed,
		"wave-29: a missing staged source is the typed link-failure class, not a virtual-leg degrade")
	require.ErrorIs(t, err, os.ErrNotExist, "the kernel ENOENT stays unwrap-reachable")
	require.NotErrorIs(t, err, ErrPublishCollision, "an unrelated link failure is not a collision")
	_, statErr := os.Stat(dst)
	require.ErrorIs(t, statErr, os.ErrNotExist, "nothing was published")
}

// link(2) succeeded but the staged-source unlink failed (EPERM and friends):
// the publish must fail CLOSED — the destination link it created is rolled
// back and the caller keeps its staged file, never a duplicated inode pair.
func TestPublishNoReplaceFallbackW16_StagedCleanupFailureRollsDestinationBack(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "staged.tmp")
	dst := filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))

	prevRemove := publishNoReplaceRemove
	stagedUnlinkErr := errors.New("w16 staged unlink wedged")
	publishNoReplaceRemove = func(name string) error {
		if name == src {
			return stagedUnlinkErr // the dst rollback remove delegates for real
		}
		return prevRemove(name)
	}
	t.Cleanup(func() { publishNoReplaceRemove = prevRemove })

	err := publishNoReplaceFallback(src, dst)
	require.ErrorIs(t, err, stagedUnlinkErr)
	require.Contains(t, err.Error(), "staged cleanup failed")
	require.NotContains(t, err.Error(), "AND publish rollback failed", "the rollback remove delegates cleanly")

	_, statErr := os.Stat(dst)
	require.ErrorIs(t, statErr, os.ErrNotExist, "the failed publish undoes its destination link — fail closed")
	got, readErr := os.ReadFile(src)
	require.NoError(t, readErr)
	require.Equal(t, "staged", string(got), "the caller's staged file survives for retry")
}

// The belt-and-braces leg: the staged unlink failed AND the destination
// rollback remove failed too — both errors surface in one message so the
// operator sees the duplicated inode pair it must clean up.
func TestPublishNoReplaceFallbackW16_StagedCleanupAndRollbackBothFail(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "staged.tmp")
	dst := filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))

	prevRemove := publishNoReplaceRemove
	stagedUnlinkErr := errors.New("w16 staged unlink wedged")
	rollbackErr := errors.New("w16 rollback unlink wedged")
	publishNoReplaceRemove = func(name string) error {
		if name == src {
			return stagedUnlinkErr
		}
		return rollbackErr
	}
	t.Cleanup(func() { publishNoReplaceRemove = prevRemove })

	err := publishNoReplaceFallback(src, dst)
	require.ErrorIs(t, err, rollbackErr, "the rollback failure is wrapped for inspection")
	require.Contains(t, err.Error(), stagedUnlinkErr.Error())
	require.Contains(t, err.Error(), "AND publish rollback failed")

	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	require.Equal(t, "staged", string(got), "the un-rolled-back destination link is left for manual cleanup")
	got, readErr = os.ReadFile(src)
	require.NoError(t, readErr)
	require.Equal(t, "staged", string(got))
}
