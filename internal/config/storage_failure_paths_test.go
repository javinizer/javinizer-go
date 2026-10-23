package config

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateBlockedDirectoryDoesNotInvokeMutation(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	original := []byte("preserved parent file")
	require.NoError(t, os.WriteFile(blocker, original, 0600))
	called := false
	err := Update(filepath.Join(blocker, "config.yaml"), func(*Config) { called = true })
	require.False(t, called)
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	require.Equal(t, "mkdir", pathErr.Op)
	require.Equal(t, blocker, pathErr.Path)
	require.EqualError(t, err, "failed to create config directory: "+pathErr.Error())
	got, err := os.ReadFile(blocker)
	require.NoError(t, err)
	require.Equal(t, original, got)
}

func TestInitDefaultsUseExplicitEnvironmentLookup(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "ignored-environment-host")
	t.Setenv("JAVINIZER_INIT_ALLOWED_DIRECTORIES", "ignored-environment-directory")
	t.Setenv("JAVINIZER_INIT_ALLOWED_ORIGINS", "ignored-environment-origin")
	values := map[string]string{
		"JAVINIZER_INIT_SERVER_HOST":         "  fixture-host  ",
		"JAVINIZER_INIT_ALLOWED_DIRECTORIES": " /first, ,/second ",
		"JAVINIZER_INIT_ALLOWED_ORIGINS":     " https://example.test, ,https://other.test ",
	}
	cfg := DefaultConfig(nil, nil)
	require.True(t, applyInitDefaultsFromEnv(cfg, func(key string) string { return values[key] }))
	require.Equal(t, "fixture-host", cfg.Server.Host)
	require.Equal(t, []string{"/first", "/second"}, cfg.API.Security.AllowedDirectories)
	require.Equal(t, []string{"https://example.test", "https://other.test"}, cfg.API.Security.AllowedOrigins)
}

func assertLockFailureLeavesConfigAndAllowsRetry(t *testing.T, path string, original []byte) {
	t.Helper()
	require.NoFileExists(t, path+".lock")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, got)
	unlock, err := acquireConfigFileLock(path)
	require.NoError(t, err)
	t.Cleanup(unlock)
	require.FileExists(t, path+".lock")
	unlock()
	require.NoFileExists(t, path+".lock")
	got, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, got)
}

func TestLockWriteFailureCleansUpAndAllowsRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("config_version: 3\n")
	require.NoError(t, os.WriteFile(path, original, 0600))
	previousOpen := osOpenFileFunc
	t.Cleanup(func() { osOpenFileFunc = previousOpen })
	var closedFile *os.File
	// The existing open seam models a descriptor that becomes unusable before
	// the token write. It does not rely on permissions or a concurrent close.
	osOpenFileFunc = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		file, err := previousOpen(name, flag, perm)
		if err != nil {
			return nil, err
		}
		closedFile = file
		if err := file.Close(); err != nil {
			return nil, err
		}
		return file, nil
	}
	unlock, err := acquireConfigFileLock(path)
	osOpenFileFunc = previousOpen
	require.Nil(t, unlock)
	require.ErrorIs(t, err, os.ErrClosed)
	require.NotNil(t, closedFile)
	_, writeErr := closedFile.WriteString("probe")
	require.ErrorIs(t, writeErr, os.ErrClosed)
	require.EqualError(t, err, "failed to write config lock: "+writeErr.Error())
	assertLockFailureLeavesConfigAndAllowsRetry(t, path, original)
}

func TestLockSyncFailureCleansUpAndAllowsRetry(t *testing.T) {
	// This fixture relies on Linux pipe fsync rejection. Other systems may
	// differ; Windows FlushFileBuffers can wait for the pipe reader.
	if runtime.GOOS != "linux" {
		t.Skip("pipe Sync failure fixture requires Linux fsync semantics")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("config_version: 3\n")
	require.NoError(t, os.WriteFile(path, original, 0600))
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Close(); require.NoError(t, reader.Close()) })
	syncErr := writer.Sync()
	require.Error(t, syncErr)
	previousOpen := osOpenFileFunc
	t.Cleanup(func() { osOpenFileFunc = previousOpen })
	// This is an OS-backed Sync failure model using the existing open seam:
	// a pipe accepts the token but cannot fsync. It is not a claim that an
	// O_EXCL-created regular lock file normally becomes a pipe or fails Sync.
	osOpenFileFunc = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		placeholder, err := previousOpen(name, flag, perm)
		if err != nil {
			return nil, err
		}
		if err := placeholder.Close(); err != nil {
			return nil, err
		}
		return writer, nil
	}
	unlock, err := acquireConfigFileLock(path)
	osOpenFileFunc = previousOpen
	require.Nil(t, unlock)
	require.EqualError(t, err, "failed to sync config lock: "+syncErr.Error())
	// Check closure before reading, so a cleanup regression cannot block at EOF.
	_, closeErr := writer.WriteString("probe")
	require.ErrorIs(t, closeErr, os.ErrClosed)
	token, err := io.ReadAll(reader)
	require.NoError(t, err)
	pid, created, valid := parseConfigLockMetadata(string(token))
	require.True(t, valid)
	require.Equal(t, os.Getpid(), pid)
	require.Positive(t, created)
	assertLockFailureLeavesConfigAndAllowsRetry(t, path, original)
}
