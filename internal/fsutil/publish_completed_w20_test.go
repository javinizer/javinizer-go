//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-20 (P2) — ErrPublishCompleted:
// the POSIX hard-link fallback can return an error AFTER link(2) installed
// the staged bytes at the destination (staged-source unlink failed AND the
// destination rollback unlink failed too). Callers compensating a failed
// publish must not read that error as "nothing was installed": the
// destination name is OCCUPIED BY THE STAGED BYTES. The sentinel lets
// history's re-arm classifier (rearmPendingKind) route that ownership to the
// restore-pending CLEAN kind instead of treating the name as unowned. The
// wave-16 fallback pins keep asserting the message shape; these tests pin
// the sentinel contract itself.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Wave-21 (codex P2): PublishCompleted is the exported classifier sharing
// the ownership signal with both compensating callers (history's
// rearmPendingKind, the downloader's rollbackRearmPendingKind) — pin its
// exact truth table, including wrap traversal and refusal disjointness.
func TestPublishCompletedW21_ClassifierTruthTable(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"sentinel", ErrPublishCompleted, true},
		{"wrapped", fmt.Errorf("no-replace publish /a -> /b: staged cleanup failed AND publish rollback failed: %w", ErrPublishCompleted), true},
		{"double-wrapped", fmt.Errorf("swap rollback: %w", fmt.Errorf("outer: %w", ErrPublishCompleted)), true},
		{"collision", ErrPublishCollision, false},
		{"unsupported", ErrPublishNoReplaceUnsupported, false},
		{"plain failure", errors.New("staged cleanup failed"), false},
		{"nil", nil, false},
	} {
		require.Equal(t, tc.want, PublishCompleted(tc.err), tc.name)
	}
}

// link(2) succeeded, staged unlink failed, destination rollback failed too:
// the error must carry ErrPublishCompleted (both real causes still wrapped),
// with the destination provably holding the staged bytes.
func TestPublishCompletedW20_RollbackFailureCarriesSentinel(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "staged.tmp")
	dst := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")
	require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))

	prevRemove := publishNoReplaceRemove
	stagedUnlinkErr := errors.New("w20 staged unlink wedged")
	rollbackErr := errors.New("w20 rollback unlink wedged")
	publishNoReplaceRemove = func(name string) error {
		if name == src {
			return stagedUnlinkErr
		}
		return rollbackErr
	}
	t.Cleanup(func() { publishNoReplaceRemove = prevRemove })

	err := publishNoReplaceFallback(src, dst)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPublishCompleted,
		"the destination name provably carries the staged bytes despite the error")
	require.ErrorIs(t, err, rollbackErr, "the rollback failure stays wrapped")
	require.Contains(t, err.Error(), stagedUnlinkErr.Error())
	require.Contains(t, err.Error(), "AND publish rollback failed")
	require.False(t, PublishRefusal(err), "completed-despite-error is NOT a refusal class")

	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	require.Equal(t, "staged", string(got), "the destination holds the staged bytes (owned, by the caller's accounting)")
}

// link(2) succeeded, staged unlink failed, rollback SUCCEEDED: the publish
// was unwound — no ErrPublishCompleted — the destination is absent again.
func TestPublishCompletedW20_CleanRollbackDoesNotCarrySentinel(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "staged.tmp")
	dst := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")
	require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))

	prevRemove := publishNoReplaceRemove
	stagedUnlinkErr := errors.New("w20 staged unlink wedged")
	publishNoReplaceRemove = func(name string) error {
		if name == src {
			return stagedUnlinkErr
		}
		return prevRemove(name)
	}
	t.Cleanup(func() { publishNoReplaceRemove = prevRemove })

	err := publishNoReplaceFallback(src, dst)
	require.ErrorIs(t, err, stagedUnlinkErr)
	require.NotErrorIs(t, err, ErrPublishCompleted,
		"a cleanly rolled-back publish installed nothing — no completed signal")
	_, statErr := os.Stat(dst)
	require.ErrorIs(t, statErr, os.ErrNotExist, "the rolled-back destination is absent")
}

// The refusal classes never wrap ErrPublishCompleted: a refusal installed
// nothing by definition, so the two ownership signals are disjoint.
func TestPublishCompletedW20_RefusalClassesStayDisjoint(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "staged.tmp")
	dst := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")
	require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))
	require.NoError(t, os.WriteFile(dst, []byte("racer"), 0o644))

	err := publishNoReplaceFallback(src, dst)
	require.ErrorIs(t, err, ErrPublishCollision)
	require.True(t, PublishRefusal(err))
	require.NotErrorIs(t, err, ErrPublishCompleted,
		"a collision refused the publish — nothing of ours occupies the name")
	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	require.Equal(t, "racer", string(got), "the racer's bytes are intact")

	// And a happy publish reports success, not the sentinel.
	dst2 := filepath.Join(dir, "poster.jpg.dlbak.fedcba9876543210")
	require.NoError(t, publishNoReplaceFallback(src, dst2))
	got, readErr = os.ReadFile(dst2)
	require.NoError(t, readErr)
	require.Equal(t, "staged", string(got))
}
