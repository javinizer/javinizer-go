//go:build linux

package desktop_test

import (
	"bytes"
	"context"
	"flag"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/desktop"
	"github.com/stretchr/testify/require"
)

func TestSaveWindowStateWriteLimitPreservesExistingState(t *testing.T) {
	const childEnv = "JAVINIZER_WINDOW_STATE_WRITE_LIMIT_CHILD"
	if root := os.Getenv(childEnv); root != "" {
		t.Setenv("XDG_CONFIG_HOME", root)
		t.Setenv("HOME", root)
		verifyWindowStateWriteLimit(t)
		return
	}

	// RLIMIT_FSIZE and signal disposition are process-wide. Exercise the real
	// kernel write failure in a separate copy of this test binary so unrelated
	// tests and background writers cannot inherit the temporary restriction.
	var before syscall.Rlimit
	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_FSIZE, &before))
	executable, err := os.Executable()
	require.NoError(t, err)
	args := []string{"-test.run=^TestSaveWindowStateWriteLimitPreservesExistingState$", "-test.v"}
	if coverDir := flag.Lookup("test.gocoverdir"); coverDir != nil && coverDir.Value.String() != "" {
		// Let go test include the child's actual production execution in the
		// same coverage report. No profile rewriting or source substitution.
		args = append(args, "-test.gocoverdir="+coverDir.Value.String())
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Env = append(os.Environ(), childEnv+"="+t.TempDir())
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Contains(t, string(output), "--- PASS: TestSaveWindowStateWriteLimitPreservesExistingState")
	var after syscall.Rlimit
	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_FSIZE, &after))
	require.Equal(t, before, after, "child must not change the parent file-size limit")
}

func verifyWindowStateWriteLimit(t *testing.T) {
	t.Helper()
	old := desktop.WindowState{Width: 1200, Height: 800}
	next := desktop.WindowState{Width: 1600, Height: 1000, Maximised: true}
	require.NoError(t, desktop.SaveWindowState(old))
	dir, err := desktop.UserDataDir()
	require.NoError(t, err)
	path := filepath.Join(dir, "window-state.json")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	before, err := os.Stat(path)
	require.NoError(t, err)

	var originalLimit syscall.Rlimit
	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_FSIZE, &originalLimit))
	limited := originalLimit
	limited.Cur = 1 // Permit a partial temporary file, then fail its remaining write.
	signal.Ignore(syscall.SIGXFSZ)
	t.Cleanup(func() { signal.Reset(syscall.SIGXFSZ) })
	t.Cleanup(func() { require.NoError(t, syscall.Setrlimit(syscall.RLIMIT_FSIZE, &originalLimit)) })
	require.NoError(t, syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited))
	err = desktop.SaveWindowState(next)
	// Restore before assertions and before go test emits its coverage files.
	require.NoError(t, syscall.Setrlimit(syscall.RLIMIT_FSIZE, &originalLimit))
	require.ErrorIs(t, err, syscall.EFBIG)
	require.ErrorContains(t, err, "desktop: cannot write window state")

	actual, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, bytes.Equal(original, actual), "failed save changed published state")
	after, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after), "failed save replaced the published file")
	require.Equal(t, before.Mode().Perm(), after.Mode().Perm())
	loaded, err := desktop.LoadWindowState()
	require.NoError(t, err)
	require.Equal(t, old, loaded)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "failed save left a partial temporary file")
	require.Equal(t, "window-state.json", entries[0].Name())

	require.NoError(t, desktop.SaveWindowState(next))
	loaded, err = desktop.LoadWindowState()
	require.NoError(t, err)
	require.Equal(t, next, loaded)
	entries, err = os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "successful retry left a temporary file")
	require.Equal(t, "window-state.json", entries[0].Name())
}
