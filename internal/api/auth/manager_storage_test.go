//go:build !windows

package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteCredentialsToDisk(t *testing.T) {
	t.Parallel()

	t.Run("rejects_nil_credentials", func(t *testing.T) {
		t.Parallel()

		configFile := filepath.Join(t.TempDir(), "config.yaml")
		manager, err := NewAuthManager(configFile, time.Hour)
		require.NoError(t, err)

		err = manager.writeCredentialsToDisk(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credentials are required")
	})

	t.Run("writes_valid_credentials", func(t *testing.T) {
		t.Parallel()

		configFile := filepath.Join(t.TempDir(), "config.yaml")
		manager, err := NewAuthManager(configFile, time.Hour)
		require.NoError(t, err)

		creds := &storedCredentials{
			Username: "testuser",
			Salt:     []byte("salt123456789012"),
			Hash:     []byte("hash12345678901234567890123456789"),
			Params: argon2Params{
				Memory:  65536,
				Time:    1,
				Threads: 4,
				KeyLen:  32,
			},
		}

		err = manager.writeCredentialsToDisk(creds)
		require.NoError(t, err)

		credPath := credentialPathForConfig(configFile)
		data, err := os.ReadFile(credPath)
		require.NoError(t, err)

		var payload credentialFile
		require.NoError(t, json.Unmarshal(data, &payload))
		assert.Equal(t, "testuser", payload.Username)
		assert.Equal(t, uint32(65536), payload.Memory)
	})

	t.Run("fails_on_readonly_directory", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS == "windows" {
			t.Skip("windows file permissions work differently")
		}

		tmpDir := t.TempDir()
		readonlyDir := filepath.Join(tmpDir, "readonly")
		require.NoError(t, os.Mkdir(readonlyDir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(readonlyDir, 0o755) })

		configFile := filepath.Join(readonlyDir, "config.yaml")
		manager, err := NewAuthManager(configFile, time.Hour)
		require.NoError(t, err)

		creds := &storedCredentials{
			Username: "testuser",
			Salt:     []byte("salt123456789012"),
			Hash:     []byte("hash12345678901234567890123456789"),
			Params: argon2Params{
				Memory:  65536,
				Time:    1,
				Threads: 4,
				KeyLen:  32,
			},
		}

		err = manager.writeCredentialsToDisk(creds)
		require.Error(t, err)
	})

	t.Run("creates_parent_directory", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "subdir", "another", "config.yaml")

		manager, err := NewAuthManager(configFile, time.Hour)
		require.NoError(t, err)

		creds := &storedCredentials{
			Username: "testuser",
			Salt:     []byte("salt123456789012"),
			Hash:     []byte("hash12345678901234567890123456789"),
			Params: argon2Params{
				Memory:  65536,
				Time:    1,
				Threads: 4,
				KeyLen:  32,
			},
		}

		err = manager.writeCredentialsToDisk(creds)
		require.NoError(t, err)

		credPath := credentialPathForConfig(configFile)
		_, err = os.Stat(credPath)
		require.NoError(t, err)
	})

	t.Run("enforces_file_permissions", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS == "windows" {
			t.Skip("windows uses ACLs, not POSIX permissions")
		}

		configFile := filepath.Join(t.TempDir(), "config.yaml")
		manager, err := NewAuthManager(configFile, time.Hour)
		require.NoError(t, err)

		creds := &storedCredentials{
			Username: "testuser",
			Salt:     []byte("salt123456789012"),
			Hash:     []byte("hash12345678901234567890123456789"),
			Params: argon2Params{
				Memory:  65536,
				Time:    1,
				Threads: 4,
				KeyLen:  32,
			},
		}

		err = manager.writeCredentialsToDisk(creds)
		require.NoError(t, err)

		credPath := credentialPathForConfig(configFile)
		info, err := os.Stat(credPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})
}

func TestWritePersistentSessionsLocked(t *testing.T) {
	t.Parallel()

	t.Run("removes_session_file_when_no_persistent_sessions", func(t *testing.T) {
		t.Parallel()

		configFile := filepath.Join(t.TempDir(), "config.yaml")
		manager, err := NewAuthManager(configFile, time.Hour)
		require.NoError(t, err)
		require.NoError(t, manager.Setup("admin", "password123"))

		sessionPath := sessionPathForConfig(configFile)

		sessionID, err := manager.Login("admin", "password123", true)
		require.NoError(t, err)

		_, err = os.Stat(sessionPath)
		require.NoError(t, err)

		manager.mu.Lock()
		delete(manager.sessions, sessionID)
		err = manager.writePersistentSessionsLocked()
		manager.mu.Unlock()

		require.NoError(t, err)
		_, err = os.Stat(sessionPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("creates_session_file_with_persistent_sessions", func(t *testing.T) {
		t.Parallel()

		configFile := filepath.Join(t.TempDir(), "config.yaml")
		manager, err := NewAuthManager(configFile, time.Hour)
		require.NoError(t, err)
		require.NoError(t, manager.Setup("admin", "password123"))

		_, err = manager.Login("admin", "password123", true)
		require.NoError(t, err)

		sessionPath := sessionPathForConfig(configFile)
		data, err := os.ReadFile(sessionPath)
		require.NoError(t, err)

		var payload sessionFile
		require.NoError(t, json.Unmarshal(data, &payload))
		assert.Equal(t, 1, len(payload.Sessions))
	})

	t.Run("handles_missing_session_file_gracefully", func(t *testing.T) {
		t.Parallel()

		configFile := filepath.Join(t.TempDir(), "config.yaml")
		manager, err := NewAuthManager(configFile, time.Hour)
		require.NoError(t, err)
		require.NoError(t, manager.Setup("admin", "password123"))

		manager.mu.Lock()
		err = manager.writePersistentSessionsLocked()
		manager.mu.Unlock()

		require.NoError(t, err)
	})

	t.Run("fails_on_readonly_directory", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS == "windows" {
			t.Skip("windows file permissions work differently")
		}

		tmpDir := t.TempDir()
		readonlyDir := filepath.Join(tmpDir, "readonly")
		require.NoError(t, os.Mkdir(readonlyDir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(readonlyDir, 0o755) })

		configFile := filepath.Join(readonlyDir, "config.yaml")
		manager, err := NewAuthManager(configFile, time.Hour)
		require.NoError(t, err)

		creds := &storedCredentials{
			Username: "admin",
			Salt:     []byte("salt123456789012"),
			Hash:     []byte("hash12345678901234567890123456789"),
			Params: argon2Params{
				Memory:  65536,
				Time:    1,
				Threads: 4,
				KeyLen:  32,
			},
		}

		manager.mu.Lock()
		manager.credentials = creds
		manager.sessions["test-session"] = sessionRecord{
			Username:   "admin",
			ExpiresAt:  time.Now().Add(time.Hour),
			Persistent: true,
		}
		err = manager.writePersistentSessionsLocked()
		manager.mu.Unlock()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to")
	})

	t.Run("enforces_session_file_permissions", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS == "windows" {
			t.Skip("windows uses ACLs, not POSIX permissions")
		}

		configFile := filepath.Join(t.TempDir(), "config.yaml")
		manager, err := NewAuthManager(configFile, time.Hour)
		require.NoError(t, err)
		require.NoError(t, manager.Setup("admin", "password123"))

		_, err = manager.Login("admin", "password123", true)
		require.NoError(t, err)

		sessionPath := sessionPathForConfig(configFile)
		info, err := os.Stat(sessionPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("filters_non_persistent_sessions", func(t *testing.T) {
		t.Parallel()

		configFile := filepath.Join(t.TempDir(), "config.yaml")
		manager, err := NewAuthManager(configFile, time.Hour)
		require.NoError(t, err)
		require.NoError(t, manager.Setup("admin", "password123"))

		_, err = manager.Login("admin", "password123", false)
		require.NoError(t, err)

		_, err = manager.Login("admin", "password123", true)
		require.NoError(t, err)

		sessionPath := sessionPathForConfig(configFile)
		data, err := os.ReadFile(sessionPath)
		require.NoError(t, err)

		var payload sessionFile
		require.NoError(t, json.Unmarshal(data, &payload))
		assert.Equal(t, 1, len(payload.Sessions))
	})
}

func TestWritePersistentSessionsLocked_RemoveError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("windows file permissions work differently")
	}

	t.Run("handles_remove_permission_denied", func(t *testing.T) {
		tmpDir := t.TempDir()
		subDir := filepath.Join(tmpDir, "sub")
		require.NoError(t, os.Mkdir(subDir, 0o755))

		configFile := filepath.Join(subDir, "config.yaml")

		manager, err := NewAuthManager(configFile, time.Hour)
		require.NoError(t, err)

		creds := &storedCredentials{
			Username: "admin",
			Salt:     []byte("salt123456789012"),
			Hash:     []byte("hash12345678901234567890123456789"),
			Params: argon2Params{
				Memory:  65536,
				Time:    1,
				Threads: 4,
				KeyLen:  32,
			},
		}

		manager.mu.Lock()
		manager.credentials = creds
		manager.sessions["test-session"] = sessionRecord{
			Username:   "admin",
			ExpiresAt:  time.Now().Add(time.Hour),
			Persistent: true,
		}
		require.NoError(t, manager.writePersistentSessionsLocked())
		manager.mu.Unlock()

		sessionPath := sessionPathForConfig(configFile)
		_, err = os.Stat(sessionPath)
		require.NoError(t, err)

		require.NoError(t, os.Chmod(subDir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(subDir, 0o755) })

		manager.mu.Lock()
		for id := range manager.sessions {
			delete(manager.sessions, id)
		}
		err = manager.writePersistentSessionsLocked()
		manager.mu.Unlock()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to remove auth session file")
	})
}

func TestWriteCredentialsToDisk_EnforcePermissionsError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("windows uses ACLs, not POSIX permissions")
	}

	t.Run("fails_when_credential_path_is_directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")

		manager, err := NewAuthManager(configFile, time.Hour)
		require.NoError(t, err)

		credPath := credentialPathForConfig(configFile)
		require.NoError(t, os.Mkdir(credPath, 0o755))

		creds := &storedCredentials{
			Username: "testuser",
			Salt:     []byte("salt123456789012"),
			Hash:     []byte("hash12345678901234567890123456789"),
			Params: argon2Params{
				Memory:  65536,
				Time:    1,
				Threads: 4,
				KeyLen:  32,
			},
		}

		err = manager.writeCredentialsToDisk(creds)
		require.Error(t, err)
	})
}

func TestLoadCredentialsFromDisk_Errors(t *testing.T) {
	t.Parallel()

	t.Run("fails_on_directory_as_credential_path", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")

		credPath := credentialPathForConfig(configFile)
		require.NoError(t, os.Mkdir(credPath, 0o755))

		_, err := NewAuthManager(configFile, time.Hour)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is a directory")
	})

	t.Run("fails_on_nonregular_file_as_credential_path", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("named pipes not supported on windows")
		}

		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")

		credPath := credentialPathForConfig(configFile)
		require.NoError(t, syscall.Mkfifo(credPath, 0o600))

		_, err := NewAuthManager(configFile, time.Hour)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not a regular file")
	})

	t.Run("fails_on_symlink_as_credential_path", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("windows symlink handling differs")
		}

		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")

		targetPath := filepath.Join(tmpDir, "target")
		require.NoError(t, os.WriteFile(targetPath, []byte("{}"), 0o600))

		credPath := credentialPathForConfig(configFile)
		require.NoError(t, os.Symlink(targetPath, credPath))

		_, err := NewAuthManager(configFile, time.Hour)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not be a symlink")
	})
}

func TestEnforceCredentialFilePermissions_ChmodError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("windows uses ACLs, not POSIX permissions")
	}

	t.Run("handles_unsupported_permission_mutation_eopnotsupp", func(t *testing.T) {
		err := isUnsupportedPermissionMutation(syscall.EOPNOTSUPP)
		assert.True(t, err)
	})

	t.Run("handles_unsupported_permission_mutation_enotsup", func(t *testing.T) {
		err := isUnsupportedPermissionMutation(syscall.ENOTSUP)
		assert.True(t, err)
	})

	t.Run("handles_unsupported_permission_mutation_erofs", func(t *testing.T) {
		err := isUnsupportedPermissionMutation(syscall.EROFS)
		assert.True(t, err)
	})

	t.Run("other_errors_not_unsupported_mutation", func(t *testing.T) {
		assert.False(t, isUnsupportedPermissionMutation(syscall.EPERM))
		assert.False(t, isUnsupportedPermissionMutation(syscall.EACCES))
		assert.False(t, isUnsupportedPermissionMutation(errors.New("generic")))
		assert.False(t, isUnsupportedPermissionMutation(nil))
	})

	t.Run("wrapped_errors_detected", func(t *testing.T) {
		wrapped := errors.Join(errors.New("context"), syscall.EOPNOTSUPP)
		assert.True(t, isUnsupportedPermissionMutation(wrapped))
	})
}
