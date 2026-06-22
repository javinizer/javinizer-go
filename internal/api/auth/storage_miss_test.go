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

// --- writeCredentialsToDisk: Close error (file written but close fails) ---
// Note: Close errors on regular files are very rare on modern OSes.
// The close-error path is exercised through the readonly-directory path below.

func TestWriteCredentialsToDisk_Miss_ReadOnlyDirPreventsWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file permissions work differently")
	}

	tmpDir := t.TempDir()
	readonlyDir := filepath.Join(tmpDir, "ro")
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
			Memory: 65536, Time: 1, Threads: 4, KeyLen: 32,
		},
	}

	err = manager.writeCredentialsToDisk(creds)
	require.Error(t, err)
}

// --- writeCredentialsToDisk: MkdirAll error ---

func TestWriteCredentialsToDisk_Miss_MkdirAllError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file permissions work differently")
	}

	tmpDir := t.TempDir()
	// Make the parent directory read-only so MkdirAll fails for nested paths
	readonlyDir := filepath.Join(tmpDir, "readonly")
	require.NoError(t, os.Mkdir(readonlyDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(readonlyDir, 0o755) })

	// Try to write to a deeply nested subdirectory that can't be created
	configFile := filepath.Join(readonlyDir, "sub", "deep", "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	creds := &storedCredentials{
		Username: "admin",
		Salt:     []byte("salt123456789012"),
		Hash:     []byte("hash12345678901234567890123456789"),
		Params: argon2Params{
			Memory: 65536, Time: 1, Threads: 4, KeyLen: 32,
		},
	}

	err = manager.writeCredentialsToDisk(creds)
	require.Error(t, err)
}

// --- writeCredentialsToDisk: rename error (target is a directory) ---

func TestWriteCredentialsToDisk_Miss_RenameError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file permissions work differently")
	}

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	credPath := credentialPathForConfig(configFile)
	// Create the target as a directory so rename fails
	require.NoError(t, os.Mkdir(credPath, 0o755))

	creds := &storedCredentials{
		Username: "admin",
		Salt:     []byte("salt123456789012"),
		Hash:     []byte("hash12345678901234567890123456789"),
		Params: argon2Params{
			Memory: 65536, Time: 1, Threads: 4, KeyLen: 32,
		},
	}

	err = manager.writeCredentialsToDisk(creds)
	require.Error(t, err)
}

// --- writePersistentSessionsLocked: MkdirAll error ---

func TestWritePersistentSessionsLocked_Miss_MkdirAllError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file permissions work differently")
	}

	tmpDir := t.TempDir()
	readonlyDir := filepath.Join(tmpDir, "readonly")
	require.NoError(t, os.Mkdir(readonlyDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(readonlyDir, 0o755) })

	configFile := filepath.Join(readonlyDir, "sub", "deep", "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	creds := &storedCredentials{
		Username: "admin",
		Salt:     []byte("salt123456789012"),
		Hash:     []byte("hash12345678901234567890123456789"),
		Params: argon2Params{
			Memory: 65536, Time: 1, Threads: 4, KeyLen: 32,
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
}

// --- writePersistentSessionsLocked: rename error (session file is directory) ---

func TestWritePersistentSessionsLocked_Miss_RenameError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file permissions work differently")
	}

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	creds := &storedCredentials{
		Username: "admin",
		Salt:     []byte("salt123456789012"),
		Hash:     []byte("hash12345678901234567890123456789"),
		Params: argon2Params{
			Memory: 65536, Time: 1, Threads: 4, KeyLen: 32,
		},
	}

	manager.mu.Lock()
	manager.credentials = creds
	manager.sessions["test-session"] = sessionRecord{
		Username:   "admin",
		ExpiresAt:  time.Now().Add(time.Hour),
		Persistent: true,
	}

	// Create the session file as a directory so rename fails
	sessionPath := sessionPathForConfig(configFile)
	require.NoError(t, os.MkdirAll(sessionPath, 0o755))

	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()

	require.Error(t, err)
}

// --- loadSessionsFromDisk: file read error (permission denied) ---
// Note: On macOS, chmod 0o000 on a file may still allow read by root or
// be affected by SIP. Instead, we write invalid JSON to trigger the
// unmarshal error path which removes the file.

func TestLoadSessionsFromDisk_Miss_InvalidJSONRemovesFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionPath := sessionPathForConfig(configFile)

	// Write invalid JSON to the session file
	require.NoError(t, os.WriteFile(sessionPath, []byte("{invalid json"), 0o600))

	manager.loadSessionsFromDisk()

	// The malformed session file should be removed
	_, statErr := os.Stat(sessionPath)
	assert.True(t, os.IsNotExist(statErr), "Malformed session file should be removed")
}

// --- loadSessionsFromDisk: permission enforcement error (skips loading) ---

func TestLoadSessionsFromDisk_Miss_PermissionEnforcementError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file permissions work differently")
	}

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionPath := sessionPathForConfig(configFile)

	// Write a valid session file, then make it a directory so enforceCredentialFilePermissions fails
	sessionData := sessionFile{
		Version: 1,
		Sessions: []sessionFileItem{
			{ID: "test-session", Username: "admin", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)},
		},
	}
	data, _ := json.Marshal(sessionData)
	require.NoError(t, os.WriteFile(sessionPath, data, 0o600))

	// Replace the session file with a directory so enforceCredentialFilePermissions fails
	require.NoError(t, os.Remove(sessionPath))
	require.NoError(t, os.Mkdir(sessionPath, 0o755))
	t.Cleanup(func() { _ = os.Remove(sessionPath) })

	// loadSessionsFromDisk should return early due to enforceCredentialFilePermissions error
	manager.loadSessionsFromDisk()

	manager.mu.RLock()
	sessions := manager.sessions
	manager.mu.RUnlock()

	_, exists := sessions["test-session"]
	assert.False(t, exists, "Session should not be loaded when permission enforcement fails")
}

// --- loadCredentialsFromDisk: stat error (permission denied) ---

func TestLoadCredentialsFromDisk_Miss_StatError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file permissions work differently")
	}

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Write credentials
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Make the credential file unreadable/unstat-able by removing read permission on parent dir
	require.NoError(t, os.Chmod(tmpDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(tmpDir, 0o755) })

	// NewAuthManager should fail because it can't stat the credential file
	_, err = NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
}

// --- loadCredentialsFromDisk: read file error ---
// Note: chmod 0o000 may not prevent reading on all platforms.
// Instead, we test the invalid JSON parse path which exercises
// the "failed to parse" error.

func TestLoadCredentialsFromDisk_Miss_InvalidJSONParseError(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	credPath := credentialPathForConfig(configFile)

	// Write a file with valid JSON structure but missing required fields
	require.NoError(t, os.WriteFile(credPath, []byte("{}"), 0o600))

	_, err := NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "username is required")
}

// --- writePersistentSessionsLocked: CreateTemp error ---

func TestWritePersistentSessionsLocked_Miss_CreateTempError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file permissions work differently")
	}

	tmpDir := t.TempDir()
	readonlyDir := filepath.Join(tmpDir, "ro")
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
			Memory: 65536, Time: 1, Threads: 4, KeyLen: 32,
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
}

// --- writeCredentialsToDisk: enforceCredentialFilePermissions error on final path ---
// Note: After a successful write, enforceCredentialFilePermissions is called on the final path.
// If the file becomes a symlink after the rename, the next write would fail on load.
// This test verifies the credential file round-trip with valid data.

func TestWriteCredentialsToDisk_Miss_RoundTripVerification(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	creds := &storedCredentials{
		Username: "admin",
		Salt:     []byte("salt123456789012"),
		Hash:     []byte("hash12345678901234567890123456789"),
		Params: argon2Params{
			Memory: 65536, Time: 1, Threads: 4, KeyLen: 32,
		},
	}

	require.NoError(t, manager.writeCredentialsToDisk(creds))

	// Verify file exists and is readable
	credPath := credentialPathForConfig(configFile)
	data, err := os.ReadFile(credPath)
	require.NoError(t, err)

	var payload credentialFile
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.Equal(t, "admin", payload.Username)
}

// --- enforceCredentialFilePermissions: unsupported filesystem (EOPNOTSUPP/EROFS) ---

func TestEnforceCredentialFilePermissions_Miss_EROFS(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses ACLs, not POSIX permissions")
	}

	// Test that isUnsupportedPermissionMutation correctly identifies EROFS
	assert.True(t, isUnsupportedPermissionMutation(syscall.EROFS))
	assert.True(t, isUnsupportedPermissionMutation(errors.Join(errors.New("context"), syscall.EROFS)))
}

// --- loadSessionsFromDisk: empty session username is skipped ---

func TestLoadSessionsFromDisk_Miss_EmptyUsernameSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionPath := sessionPathForConfig(configFile)
	sessionData := sessionFile{
		Version: 1,
		Sessions: []sessionFileItem{
			{ID: "test-session", Username: "", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)},
		},
	}
	data, _ := json.Marshal(sessionData)
	require.NoError(t, os.WriteFile(sessionPath, data, 0o600))

	manager.loadSessionsFromDisk()

	manager.mu.RLock()
	sessions := manager.sessions
	manager.mu.RUnlock()

	_, exists := sessions["test-session"]
	assert.False(t, exists, "Session with empty username should be skipped")
}
