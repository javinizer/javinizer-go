package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowStateUnavailableDataDirectory(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocked, []byte("retain data"), 0600))
	original := userConfigDirFn
	userConfigDirFn = func() (string, error) { return blocked, nil }
	t.Cleanup(func() { userConfigDirFn = original })
	state, err := LoadWindowState()
	require.ErrorContains(t, err, "cannot resolve window state path")
	require.Equal(t, WindowState{}, state)
	require.ErrorContains(t, SaveWindowState(WindowState{Width: 1200, Height: 800}), "cannot resolve window state path")
	data, err := os.ReadFile(blocked)
	require.NoError(t, err)
	require.Equal(t, "retain data", string(data))
}

func TestWindowStateReadDirectoryErrorAndFailedCommitCleanup(t *testing.T) {
	root := withTempUserDataDir(t)
	target := filepath.Join(root, windowStateFile)
	require.NoError(t, os.Mkdir(target, 0700))
	sentinel := filepath.Join(target, "retain.txt")
	require.NoError(t, os.WriteFile(sentinel, []byte("existing contents"), 0600))
	state, err := LoadWindowState()
	require.ErrorContains(t, err, "cannot read window state")
	require.Equal(t, WindowState{}, state)
	require.ErrorContains(t, SaveWindowState(WindowState{Width: 1200, Height: 800}), "cannot commit window state")
	data, err := os.ReadFile(sentinel)
	require.NoError(t, err)
	require.Equal(t, "existing contents", string(data))
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1, "failed replacement must remove its temporary JSON file")
	require.Equal(t, windowStateFile, entries[0].Name())
}
