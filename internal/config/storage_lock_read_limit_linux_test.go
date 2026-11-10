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

func TestAcquireConfigFileLockRecoversWhenUnreadableLockDisappears(t *testing.T) {
	const childEnv = "JAVINIZER_CONFIG_LOCK_READ_LIMIT_CHILD"
	if root := os.Getenv(childEnv); root != "" {
		verifyConfigLockReadLimit(t, root)
		return
	}

	// Descriptor limits affect the whole process. Isolate the native failure
	// from other tests, including parallel tests and their background work.
	var before syscall.Rlimit
	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_NOFILE, &before))
	executable, err := os.Executable()
	require.NoError(t, err)
	args := []string{"-test.run=^TestAcquireConfigFileLockRecoversWhenUnreadableLockDisappears$", "-test.v"}
	if coverDir := flag.Lookup("test.gocoverdir"); coverDir != nil && coverDir.Value.String() != "" {
		// Include the child's actual production execution in normal go test coverage.
		args = append(args, "-test.gocoverdir="+coverDir.Value.String())
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Env = append(os.Environ(), childEnv+"="+t.TempDir())
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Contains(t, string(output), "--- PASS: TestAcquireConfigFileLockRecoversWhenUnreadableLockDisappears")
	var after syscall.Rlimit
	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_NOFILE, &after))
	require.Equal(t, before, after, "child must not change the parent descriptor limit")
}

func verifyConfigLockReadLimit(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "config.yaml")
	original := []byte("config_version: 3\n")
	require.NoError(t, os.WriteFile(path, original, 0600))
	lockPath := path + ".lock"
	require.NoError(t, os.WriteFile(lockPath, []byte("previous owner"), 0600))

	var originalLimit syscall.Rlimit
	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_NOFILE, &originalLimit))
	limited := originalLimit
	if limited.Cur > 64 {
		limited.Cur = 64
	}
	// Reserve only descriptors opened by this fixture. Keep the hard limit
	// unchanged and release all reservations before restoring the soft limit.
	reserved := make([]int, 0, 64)
	restore := func() error {
		var err error
		for _, fd := range reserved {
			err = errors.Join(err, syscall.Close(fd))
		}
		reserved = reserved[:0]
		return errors.Join(err, syscall.Setrlimit(syscall.RLIMIT_NOFILE, &originalLimit))
	}
	t.Cleanup(func() { require.NoError(t, restore()) })
	previousOpen := osOpenFileFunc
	t.Cleanup(func() { osOpenFileFunc = previousOpen })
	var readErr, statErr, reserveErr error
	calls := 0
	osOpenFileFunc = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		calls++
		if calls != 1 {
			if err := restore(); err != nil {
				return nil, err
			}
			return previousOpen(name, flag, perm)
		}
		file, collision := previousOpen(name, flag, perm)
		if !os.IsExist(collision) {
			return file, collision
		}
		// Model the owner releasing its lock while this contender has no free
		// descriptors. Linux open reports EMFILE before looking up the removed
		// pathname, whereas stat needs no descriptor and reports ENOENT.
		if err := os.Remove(name); err != nil {
			return nil, err
		}
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limited); err != nil {
			return nil, err
		}
		for {
			fd, err := syscall.Open(os.DevNull, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
			if err != nil {
				reserveErr = err
				break
			}
			reserved = append(reserved, fd)
		}
		_, readErr = os.ReadFile(name)
		_, statErr = os.Stat(name)
		return nil, collision
	}

	unlock, err := acquireConfigFileLock(path)
	// Restore even if acquisition failed, before assertions or coverage output.
	restoreErr := restore()
	osOpenFileFunc = previousOpen
	require.NoError(t, restoreErr)
	if unlock != nil {
		t.Cleanup(unlock)
	}
	require.ErrorIs(t, reserveErr, syscall.EMFILE)
	require.ErrorIs(t, readErr, syscall.EMFILE)
	require.True(t, os.IsNotExist(statErr), "released lock must be absent despite its unreadable content")
	require.NoError(t, err)
	require.NotNil(t, unlock)
	require.Equal(t, 2, calls, "must retry acquisition after the unreadable lock disappears")
	content, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	pid, created, ok := parseConfigLockMetadata(string(content))
	require.True(t, ok)
	require.Equal(t, os.Getpid(), pid)
	require.Positive(t, created)
	unlock()
	unlock()
	require.NoFileExists(t, lockPath)
	actual, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, actual, "lock recovery must preserve the config")
}
