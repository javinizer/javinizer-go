//go:build linux

package history

import (
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPrimaryUndoMissingWorkingDirectorySubprocess(t *testing.T) {
	if os.Getenv("PRIMARY_UNDO_EMPTY_CWD_CHILD") == "1" {
		original, err := os.Getwd()
		require.NoError(t, err)
		// The child creates this empty directory itself. No parent-provided path,
		// repository, shared working directory, or existing data is removed.
		parent := t.TempDir()
		empty, err := os.MkdirTemp(parent, "primary-undo-empty-cwd-")
		require.NoError(t, err)
		require.Equal(t, parent, filepath.Dir(empty))
		require.NoError(t, os.Chdir(empty))
		defer func() { require.NoError(t, os.Chdir(original)) }()
		require.NoError(t, os.Remove(empty)) // only an empty, newly created test dir
		require.Error(t, primaryUndoSyncDir("relative-staging-name"))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestPrimaryUndoMissingWorkingDirectorySubprocess$", "-test.v")
	cmd.Env = append(os.Environ(), "PRIMARY_UNDO_EMPTY_CWD_CHILD=1")
	// A subprocess has independent counters. When explicitly requested by a
	// coverage run, retain its own profile instead of pretending the parent
	// profile includes this error path.
	if path := os.Getenv("PRIMARY_UNDO_SUBPROCESS_COVERPROFILE"); path != "" && testing.CoverMode() != "" {
		cmd.Args = append(cmd.Args, "-test.coverprofile="+path)
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestPrimaryUndoUnsupportedNativeDirectorySyncRefuses(t *testing.T) {
	// Linux procfs does not implement fsync. This is a read-only directory
	// handle and a synthetic nonexistent child name; no procfs data is changed.
	info, err := os.Stat("/proc/self")
	if err != nil || !info.IsDir() {
		t.Skip("procfs unavailable")
	}
	require.Error(t, primaryUndoSyncDir("/proc/self/primary-undo-nonexistent-proof"))
}
