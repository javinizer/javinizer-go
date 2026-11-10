//go:build linux

package config

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAcquireConfigFileLockRecoversWhenReadableLockDisappears(t *testing.T) {
	const childEnv = "JAVINIZER_CONFIG_LOCK_FIFO_CHILD"
	if root := os.Getenv(childEnv); root != "" {
		verifyConfigLockDisappearance(t, root)
		return
	}

	// A regression could strand either end of the FIFO in a blocking open or
	// read. Keep that native synchronization in a bounded child process so it
	// cannot leak a goroutine or hang the rest of the package's tests.
	executable, err := os.Executable()
	require.NoError(t, err)
	args := []string{"-test.run=^TestAcquireConfigFileLockRecoversWhenReadableLockDisappears$", "-test.v"}
	if coverDir := flag.Lookup("test.gocoverdir"); coverDir != nil && coverDir.Value.String() != "" {
		args = append(args, "-test.gocoverdir="+coverDir.Value.String())
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Env = append(os.Environ(), childEnv+"="+t.TempDir())
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Contains(t, string(output), "--- PASS: TestAcquireConfigFileLockRecoversWhenReadableLockDisappears")
}

func verifyConfigLockDisappearance(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "config.yaml")
	original := []byte("config_version: 3\n")
	require.NoError(t, os.WriteFile(path, original, 0600))
	lockPath := path + ".lock"
	require.NoError(t, syscall.Mkfifo(lockPath, 0600))

	writerDone := make(chan error, 1)
	go func() {
		// Opening the writer waits for acquireConfigFileLock's real ReadFile.
		// Unlink before closing: ReadFile cannot finish before EOF, so its
		// subsequent Stat deterministically sees the vanished pathname.
		writer, err := os.OpenFile(lockPath, os.O_WRONLY, 0)
		if err != nil {
			writerDone <- err
			return
		}
		_, writeErr := writer.WriteString("pid=1,time=1")
		removeErr := os.Remove(lockPath)
		writerDone <- errors.Join(writeErr, removeErr, writer.Close())
	}()

	started := time.Now().UnixNano()
	unlock, err := acquireConfigFileLock(path)
	if unlock != nil {
		t.Cleanup(unlock)
	}
	require.NoError(t, <-writerDone)
	require.NoError(t, err)
	require.NotNil(t, unlock)
	info, err := os.Stat(lockPath)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular(), "new owner must replace the removed FIFO with a regular lock")
	content, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	pid, created, ok := parseConfigLockMetadata(string(content))
	require.True(t, ok)
	require.Equal(t, os.Getpid(), pid)
	require.GreaterOrEqual(t, created, started)
	require.LessOrEqual(t, created, time.Now().UnixNano())
	unlock()
	unlock()
	require.NoFileExists(t, lockPath)
	actual, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, actual, "lock recovery must preserve the config")
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no FIFO or other lock artifacts should remain")
	require.Equal(t, "config.yaml", entries[0].Name())
}
