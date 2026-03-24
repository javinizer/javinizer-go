package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthManager_SetupLoginAndSession(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	assert.False(t, manager.IsInitialized())

	err = manager.Setup("admin", "password123")
	require.NoError(t, err)
	assert.True(t, manager.IsInitialized())

	username, ok := manager.Username()
	require.True(t, ok)
	assert.Equal(t, "admin", username)

	credPath := CredentialPathForConfig(configFile)
	info, err := os.Stat(credPath)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	_, err = manager.Login("admin", "wrong-password")
	assert.ErrorIs(t, err, ErrInvalidCredentials)

	sessionID, err := manager.Login("admin", "password123")
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	sessionUser, err := manager.AuthenticateSession(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "admin", sessionUser)

	manager.Logout(sessionID)
	_, err = manager.AuthenticateSession(sessionID)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestAuthManager_SetupValidation(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.Setup("", "password123")
	assert.ErrorIs(t, err, ErrInvalidUsername)

	err = manager.Setup("admin", "short")
	assert.ErrorIs(t, err, ErrWeakPassword)

	err = manager.Setup("admin", "password123")
	require.NoError(t, err)

	err = manager.Setup("another", "password123")
	assert.ErrorIs(t, err, ErrAuthAlreadySet)
}

func TestAuthManager_LoadMalformedFile(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	credPath := CredentialPathForConfig(configFile)
	require.NoError(t, os.WriteFile(credPath, []byte("{not-json"), 0o600))

	_, err := NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse auth credential file")
}

func TestAuthManager_SessionExpiry(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, 20*time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionID, err := manager.Login("admin", "password123")
	require.NoError(t, err)

	time.Sleep(40 * time.Millisecond)
	_, err = manager.AuthenticateSession(sessionID)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestAuthManager_LoadExistingCredentialFile(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	original, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, original.Setup("admin", "password123"))

	reloaded, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	assert.True(t, reloaded.IsInitialized())

	sessionID, err := reloaded.Login("admin", "password123")
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)
}

func TestAuthManager_LoadMalformedCredentialFields(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	credPath := CredentialPathForConfig(configFile)
	payload := map[string]any{
		"version":  1,
		"username": "admin",
		// Missing hash/salt and argon2 params should fail load.
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(credPath, data, 0o600))

	_, err = NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "argon2 parameters are required")
}

func TestAuthManager_LoadRepairsCredentialPermissions(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("windows permission bits are ACL-managed")
	}

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	credPath := CredentialPathForConfig(configFile)
	require.NoError(t, os.Chmod(credPath, 0o644))

	reloaded, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	assert.True(t, reloaded.IsInitialized())

	info, err := os.Stat(credPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestAuthManager_LoadRejectsCredentialSymlink(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("windows symlink handling differs and does not use unix permission helper")
	}

	configDir := t.TempDir()
	configFile := filepath.Join(configDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	credPath := CredentialPathForConfig(configFile)
	targetPath := filepath.Join(configDir, "target.credentials.json")
	require.NoError(t, os.Rename(credPath, targetPath))
	require.NoError(t, os.Symlink(targetPath, credPath))

	_, err = NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be a symlink")
}

func TestAuthManager_SetupIgnoresPreexistingLegacyTmpSymlink(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("windows symlink handling differs and does not use unix permission helper")
	}

	configDir := t.TempDir()
	configFile := filepath.Join(configDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	credPath := CredentialPathForConfig(configFile)
	legacyTmpPath := credPath + ".tmp"
	targetPath := filepath.Join(configDir, "symlink-target.txt")
	require.NoError(t, os.WriteFile(targetPath, []byte("original"), 0o600))
	require.NoError(t, os.Symlink(targetPath, legacyTmpPath))

	require.NoError(t, manager.Setup("admin", "password123"))

	targetBytes, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, "original", string(targetBytes))
}

func TestAuthManager_SessionCountIsBounded(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	firstSession, err := manager.Login("admin", "password123")
	require.NoError(t, err)

	for i := 0; i < maxActiveSessions+32; i++ {
		_, err := manager.Login("admin", "password123")
		require.NoError(t, err)
	}

	manager.mu.RLock()
	sessionCount := len(manager.sessions)
	manager.mu.RUnlock()
	assert.LessOrEqual(t, sessionCount, maxActiveSessions)

	_, err = manager.AuthenticateSession(firstSession)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestAuthManager_LoginRateLimitAndRecovery(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	now := time.Now()
	manager.nowFn = func() time.Time { return now }

	for i := 0; i < maxFailedLoginAttempts; i++ {
		_, err := manager.Login("admin", "wrong-password")
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	}

	_, err = manager.Login("admin", "password123")
	assert.ErrorIs(t, err, ErrLoginRateLimited)

	now = now.Add(loginLockoutDuration + time.Second)

	sessionID, err := manager.Login("admin", "password123")
	require.NoError(t, err)
	assert.NotEmpty(t, sessionID)
}
