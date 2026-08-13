package actresscache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAtomicReplaceFallbacksNative(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "new"), filepath.Join(dir, "old")
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o600))
	require.NoError(t, atomicReplace(src, dst))
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))

	// NotExist fallback: remove of a missing dst must not abort; the second
	// rename creates dst fresh.
	src2, dst2 := filepath.Join(dir, "n2"), filepath.Join(dir, "o2")
	require.NoError(t, os.WriteFile(src2, []byte("x"), 0o600))
	require.NoError(t, atomicReplace(src2, dst2))

	// Remove-of-nonempty-dir is a real failure on every platform: surface it.
	destdir := filepath.Join(dir, "destdir")
	require.NoError(t, os.Mkdir(destdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(destdir, "inner.txt"), []byte("x"), 0o600))
	src3 := filepath.Join(dir, "n3")
	require.NoError(t, os.WriteFile(src3, []byte("x"), 0o600))
	assert.Error(t, atomicReplace(src3, destdir))
}

// Gate: remove(dst) reports a hard error — propagate it, don't retry.
func TestAtomicReplaceRemoveFailureLeg(t *testing.T) {
	originalR, originalM, originalOS := atomicRename, atomicRemove, replaceIsWindows
	t.Cleanup(func() { atomicRename, atomicRemove, replaceIsWindows = originalR, originalM, originalOS })
	// Exercise the Windows-only fallback deterministically on any host OS.
	replaceIsWindows = true
	first := true
	atomicRename = func(_, _ string) error {
		if first {
			first = false
			return fmt.Errorf("windows replace collision: %w", os.ErrExist)
		}
		return nil
	}
	atomicRemove = func(string) error { return errors.New("in use") }
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "s"), filepath.Join(dir, "d")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0o600))
	err := atomicReplace(src, dst)
	require.ErrorContains(t, err, "in use")
}

// Gate: remove(dst) races a concurrent delete — the remove-gets-ENOENT path
// must skip the failure entirely and re-run rename.
func TestAtomicReplaceRemoveNotExistLeg(t *testing.T) {
	originalR, originalM, originalOS := atomicRename, atomicRemove, replaceIsWindows
	t.Cleanup(func() { atomicRename, atomicRemove, replaceIsWindows = originalR, originalM, originalOS })
	// Exercise the Windows-only fallback deterministically on any host OS.
	replaceIsWindows = true
	calls := 0
	atomicRename = func(_, _ string) error {
		calls++
		if calls == 1 {
			return fmt.Errorf("windows replace collision: %w", os.ErrExist)
		}
		return nil
	}
	atomicRemove = func(string) error { return os.ErrNotExist }
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "s"), filepath.Join(dir, "d")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0o600))
	require.NoError(t, atomicReplace(src, dst))
	assert.GreaterOrEqual(t, calls, 2)
}

// Non-Windows rename failures are final: dst (the committed artifact) must
// never be removed for unrelated errors like cross-device or permission
// failures -- the retry could not succeed anyway.
func TestAtomicReplacePreservesDstOnNonWindowsRenameFailure(t *testing.T) {
	originalR, originalM, originalOS := atomicRename, atomicRemove, replaceIsWindows
	t.Cleanup(func() { atomicRename, atomicRemove, replaceIsWindows = originalR, originalM, originalOS })
	replaceIsWindows = false
	removeCalls := 0
	atomicRename = func(_, _ string) error { return errors.New("cross-device link") }
	atomicRemove = func(string) error { removeCalls++; return nil }
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "tmp"), filepath.Join(dir, "committed.json")
	require.NoError(t, os.WriteFile(src, []byte("pending"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("committed"), 0o600))
	err := atomicReplace(src, dst)
	require.ErrorContains(t, err, "cross-device link")
	assert.Equal(t, 0, removeCalls, "POSIX must never destroy dst on rename failure")
	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "committed", string(got))
}

// On Windows, rename failures OUTSIDE the replace-collision class must not
// touch dst: deleting the committed artifact cannot heal a missing source or
// a cross-volume move.
func TestAtomicReplacePreservesDstOnWindowsNonCollisionFailure(t *testing.T) {
	originalR, originalM, originalOS := atomicRename, atomicRemove, replaceIsWindows
	t.Cleanup(func() { atomicRename, atomicRemove, replaceIsWindows = originalR, originalM, originalOS })
	replaceIsWindows = true
	removeCalls := 0
	atomicRename = func(_, _ string) error { return errors.New("not the same device") }
	atomicRemove = func(string) error { removeCalls++; return nil }
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "tmp"), filepath.Join(dir, "committed.json")
	require.NoError(t, os.WriteFile(src, []byte("pending"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("committed"), 0o600))
	err := atomicReplace(src, dst)
	require.ErrorContains(t, err, "not the same device")
	assert.Equal(t, 0, removeCalls)
	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "committed", string(got))
}
