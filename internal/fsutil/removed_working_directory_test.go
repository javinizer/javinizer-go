package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalizePathAfterWorkingDirectoryRemoved(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux removed-working-directory contract; Windows cannot remove its active working directory")
	}
	// Do not parallelize: t.Chdir retains a directory handle to restore process
	// cwd safely after the removed-directory subtest, including assertion failures.
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	originalPWD, hadPWD := os.LookupEnv("PWD")
	parent := t.TempDir()
	emptyChild := filepath.Join(parent, "empty-cwd")
	require.Equal(t, parent, filepath.Dir(emptyChild))
	require.NoError(t, os.Mkdir(emptyChild, 0700))
	sentinel := filepath.Join(parent, "sentinel.txt")
	data := []byte("canonicalization must not touch this file")
	require.NoError(t, os.WriteFile(sentinel, data, 0600))
	// Construct without filepath.Join, which would clean away the dot segments.
	relative := "unused/../missing/./target.txt"
	absolute := parent + string(os.PathSeparator) + relative
	want := filepath.Join(parent, "missing", "target.txt")
	require.NoFileExists(t, want, "canonicalization must not require a target to exist")
	assertRestored := func() {
		t.Helper()
		wd, err := os.Getwd()
		require.NoError(t, err)
		require.Equal(t, originalWD, wd)
		pwd, present := os.LookupEnv("PWD")
		require.Equal(t, hadPWD, present)
		require.Equal(t, originalPWD, pwd)
	}
	t.Run("unavailable cwd rejects relative normalization", func(t *testing.T) {
		t.Chdir(emptyChild)
		require.NoError(t, os.Remove(emptyChild), "remove only the newly created empty child")
		wd, err := os.Getwd()
		require.Empty(t, wd)
		require.ErrorIs(t, err, os.ErrNotExist)
		got, err := CanonicalizePath(relative)
		require.Empty(t, got)
		require.ErrorIs(t, err, os.ErrNotExist)
		got, err = CanonicalizePath(absolute)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
	assertRestored()
	t.Run("relative normalization recovers from a surviving directory", func(t *testing.T) {
		t.Chdir(parent)
		got, err := CanonicalizePath(relative)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
	assertRestored()
	after, err := os.ReadFile(sentinel)
	require.NoError(t, err)
	require.Equal(t, data, after)
	require.NoFileExists(t, want)
	require.NoDirExists(t, emptyChild)
}
