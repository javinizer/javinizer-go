//go:build linux

package desktop_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/javinizer/javinizer-go/internal/desktop"
	"github.com/stretchr/testify/require"
)

func TestSaveWindowStateTempPathBudgetPreservesExistingState(t *testing.T) {
	root, err := filepath.Abs(t.TempDir())
	require.NoError(t, err)
	t.Setenv("HOME", filepath.Join(root, "home"))
	short := filepath.Join(root, "short")
	t.Setenv("XDG_CONFIG_HOME", short)
	old := desktop.WindowState{Width: 1200, Height: 800}
	next := desktop.WindowState{Width: 1600, Height: 1000, Maximised: true}
	require.NoError(t, desktop.SaveWindowState(old))
	loaded, err := desktop.LoadWindowState()
	require.NoError(t, err)
	require.Equal(t, old, loaded)
	shortDir, err := desktop.UserDataDir()
	require.NoError(t, err)
	original, err := os.ReadFile(filepath.Join(shortDir, "window-state.json"))
	require.NoError(t, err)

	// Linux PATH_MAX includes the NUL terminator. The fixed filename is 17
	// bytes; Go CreateTemp's .window-state-*.json expands to at least 20
	// (14-byte prefix, at least one decimal random digit, five-byte suffix).
	// Thus every random temporary pathname is too long, not merely most draws.
	const baseBytes = 4066
	longBase := filepath.Join(root, "long")
	require.Less(t, len(longBase), baseBytes-2)
	for len(longBase)+130 < baseBytes {
		longBase = filepath.Join(longBase, strings.Repeat("d", 128))
	}
	longBase = filepath.Join(longBase, strings.Repeat("d", baseBytes-len(longBase)-1))
	require.Len(t, longBase, baseBytes)
	require.True(t, filepath.IsAbs(longBase))
	rel, err := filepath.Rel(root, longBase)
	require.NoError(t, err)
	require.False(t, filepath.IsAbs(rel))
	require.False(t, strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
	t.Setenv("XDG_CONFIG_HOME", longBase)
	dir, err := desktop.UserDataDir()
	if errors.Is(err, syscall.ENAMETOOLONG) {
		t.Skipf("filesystem cannot construct owned long-path fixture: %v", err)
	}
	require.NoError(t, err)
	require.Equal(t, filepath.Join(longBase, "Javinizer"), dir)
	require.Len(t, dir, 4076)
	target := filepath.Join(dir, "window-state.json")
	require.Len(t, target, 4094)
	require.Greater(t, len(dir)+1+20, 4095)
	err = os.WriteFile(target, original, 0600)
	if errors.Is(err, syscall.ENAMETOOLONG) {
		t.Skipf("filesystem cannot seed owned long-path target: %v", err)
	}
	require.NoError(t, err)
	before, err := os.Stat(target)
	require.NoError(t, err)
	loaded, err = desktop.LoadWindowState()
	require.NoError(t, err)
	require.Equal(t, old, loaded)

	err = desktop.SaveWindowState(next)
	require.ErrorIs(t, err, syscall.ENAMETOOLONG)
	require.ErrorContains(t, err, "desktop: cannot create temp window state file")
	loaded, err = desktop.LoadWindowState()
	require.NoError(t, err)
	require.Equal(t, old, loaded)
	actual, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, original, actual)
	after, err := os.Stat(target)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
	require.Equal(t, os.FileMode(0600), before.Mode().Perm())
	require.Equal(t, before.Mode().Perm(), after.Mode().Perm())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "window-state.json", entries[0].Name())

	t.Setenv("XDG_CONFIG_HOME", short)
	require.NoError(t, desktop.SaveWindowState(next))
	loaded, err = desktop.LoadWindowState()
	require.NoError(t, err)
	require.Equal(t, next, loaded)
	actual, err = os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, original, actual)
	// Exercise real deep-tree cleanup explicitly; TempDir's registered cleanup
	// remains a safety net if an assertion above fails.
	require.NoError(t, os.RemoveAll(filepath.Join(root, "long")))
}
