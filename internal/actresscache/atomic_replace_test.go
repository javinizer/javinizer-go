package actresscache

import (
	"errors"
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
	originalR, originalM := atomicRename, atomicRemove
	t.Cleanup(func() { atomicRename, atomicRemove = originalR, originalM })
	first := true
	atomicRename = func(_, _ string) error {
		if first {
			first = false
			return errors.New("windows-style busy")
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
	originalR, originalM := atomicRename, atomicRemove
	t.Cleanup(func() { atomicRename, atomicRemove = originalR, originalM })
	calls := 0
	atomicRename = func(_, _ string) error {
		calls++
		if calls == 1 {
			return errors.New("windows-style busy")
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
