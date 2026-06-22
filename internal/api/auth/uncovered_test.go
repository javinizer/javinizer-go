//go:build !windows

package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- credentialPathForConfig / sessionPathForConfig uncovered ---

func TestCredentialPathForConfig_Uncovered(t *testing.T) {
	result := credentialPathForConfig("/home/user/.config/javinizer/config.yaml")
	assert.Equal(t, "/home/user/.config/javinizer/auth.credentials.json", result)
}

func TestSessionPathForConfig_Uncovered(t *testing.T) {
	result := sessionPathForConfig("/home/user/.config/javinizer/config.yaml")
	assert.Equal(t, "/home/user/.config/javinizer/auth.sessions.json", result)
}

// --- loadCredentialsFromDisk uncovered error paths ---

func TestLoadCredentialsFromDisk_Uncovered_StatError(t *testing.T) {
	t.Parallel()

	// Create a directory where the credential file should be (Lstat will fail with appropriate error)
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	credPath := credentialPathForConfig(configFile)

	// Create a directory with the credential file name (not a regular file)
	require.NoError(t, os.Mkdir(credPath, 0o755))

	_, err := NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is a directory")
}

func TestLoadCredentialsFromDisk_Uncovered_MissingArgon2Params(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	credPath := credentialPathForConfig(configFile)

	// Write valid JSON but missing argon2 params
	payload := credentialFile{
		Version:  1,
		Username: "admin",
		Salt:     base64.RawStdEncoding.EncodeToString([]byte("salt123456789012")),
		Hash:     base64.RawStdEncoding.EncodeToString([]byte("hash12345678901234567890123456789")),
		// Missing Memory, Time, Threads, KeyLen (all zero)
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(credPath, data, 0o600))

	_, err = NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "argon2 parameters are required")
}

func TestLoadCredentialsFromDisk_Uncovered_InvalidSalt(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	credPath := credentialPathForConfig(configFile)

	payload := credentialFile{
		Version:  1,
		Username: "admin",
		Salt:     "!!!invalid-base64!!!",
		Hash:     base64.RawStdEncoding.EncodeToString([]byte("hash12345678901234567890123456789")),
		Memory:   65536,
		Time:     1,
		Threads:  4,
		KeyLen:   32,
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(credPath, data, 0o600))

	_, err = NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid salt")
}

func TestLoadCredentialsFromDisk_Uncovered_InvalidHash(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	credPath := credentialPathForConfig(configFile)

	payload := credentialFile{
		Version:  1,
		Username: "admin",
		Salt:     base64.RawStdEncoding.EncodeToString([]byte("salt123456789012")),
		Hash:     "!!!invalid-base64!!!",
		Memory:   65536,
		Time:     1,
		Threads:  4,
		KeyLen:   32,
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(credPath, data, 0o600))

	_, err = NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid hash")
}

func TestLoadCredentialsFromDisk_Uncovered_EmptyUsername(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	credPath := credentialPathForConfig(configFile)

	payload := credentialFile{
		Version: 1,
		// Username is empty
		Salt:    base64.RawStdEncoding.EncodeToString([]byte("salt123456789012")),
		Hash:    base64.RawStdEncoding.EncodeToString([]byte("hash12345678901234567890123456789")),
		Memory:  65536,
		Time:    1,
		Threads: 4,
		KeyLen:  32,
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(credPath, data, 0o600))

	_, err = NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "username is required")
}

func TestLoadCredentialsFromDisk_Uncovered_EmptySaltAfterDecode(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	credPath := credentialPathForConfig(configFile)

	payload := credentialFile{
		Version:  1,
		Username: "admin",
		Salt:     "", // Empty salt
		Hash:     base64.RawStdEncoding.EncodeToString([]byte("hash12345678901234567890123456789")),
		Memory:   65536,
		Time:     1,
		Threads:  4,
		KeyLen:   32,
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(credPath, data, 0o600))

	_, err = NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid salt")
}

func TestLoadCredentialsFromDisk_Uncovered_EmptyHashAfterDecode(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	credPath := credentialPathForConfig(configFile)

	payload := credentialFile{
		Version:  1,
		Username: "admin",
		Salt:     base64.RawStdEncoding.EncodeToString([]byte("salt123456789012")),
		Hash:     "", // Empty hash
		Memory:   65536,
		Time:     1,
		Threads:  4,
		KeyLen:   32,
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(credPath, data, 0o600))

	_, err = NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid hash")
}

// --- loadSessionsFromDisk uncovered paths ---

func TestLoadSessionsFromDisk_Uncovered_NoCredentials(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	// No credentials set — loadSessionsFromDisk should return early
	// Write a session file anyway
	sessionPath := sessionPathForConfig(configFile)
	sessionData := sessionFile{
		Version: 1,
		Sessions: []sessionFileItem{
			{ID: "test-session", Username: "admin", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)},
		},
	}
	data, _ := json.Marshal(sessionData)
	require.NoError(t, os.WriteFile(sessionPath, data, 0o600))

	// Calling loadSessionsFromDisk directly should not panic and should not load sessions
	manager.loadSessionsFromDisk()
	// Sessions should remain empty since credentials are nil
	manager.mu.RLock()
	sessions := manager.sessions
	manager.mu.RUnlock()
	assert.Empty(t, sessions)
}

func TestLoadSessionsFromDisk_Uncovered_ExpiredSession(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Create session file with expired session
	sessionPath := sessionPathForConfig(configFile)
	sessionData := sessionFile{
		Version: 1,
		Sessions: []sessionFileItem{
			{ID: "expired-session", Username: "admin", ExpiresAt: time.Now().Add(-time.Hour).Format(time.RFC3339)},
		},
	}
	data, _ := json.Marshal(sessionData)
	require.NoError(t, os.WriteFile(sessionPath, data, 0o600))

	manager.loadSessionsFromDisk()
	manager.mu.RLock()
	sessions := manager.sessions
	manager.mu.RUnlock()

	_, exists := sessions["expired-session"]
	assert.False(t, exists, "Expired session should not be loaded")
}

func TestLoadSessionsFromDisk_Uncovered_WrongUsername(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionPath := sessionPathForConfig(configFile)
	sessionData := sessionFile{
		Version: 1,
		Sessions: []sessionFileItem{
			{ID: "wrong-user-session", Username: "otheruser", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)},
		},
	}
	data, _ := json.Marshal(sessionData)
	require.NoError(t, os.WriteFile(sessionPath, data, 0o600))

	manager.loadSessionsFromDisk()
	manager.mu.RLock()
	sessions := manager.sessions
	manager.mu.RUnlock()

	_, exists := sessions["wrong-user-session"]
	assert.False(t, exists, "Session with wrong username should not be loaded")
}

func TestLoadSessionsFromDisk_Uncovered_EmptySessionID(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionPath := sessionPathForConfig(configFile)
	sessionData := sessionFile{
		Version: 1,
		Sessions: []sessionFileItem{
			{ID: "", Username: "admin", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)},
		},
	}
	data, _ := json.Marshal(sessionData)
	require.NoError(t, os.WriteFile(sessionPath, data, 0o600))

	manager.loadSessionsFromDisk()
	manager.mu.RLock()
	sessions := manager.sessions
	manager.mu.RUnlock()
	assert.Empty(t, sessions, "Empty session ID should be skipped")
}

func TestLoadSessionsFromDisk_Uncovered_InvalidExpiryFormat(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionPath := sessionPathForConfig(configFile)
	sessionData := sessionFile{
		Version: 1,
		Sessions: []sessionFileItem{
			{ID: "bad-expiry-session", Username: "admin", ExpiresAt: "not-a-valid-time"},
		},
	}
	data, _ := json.Marshal(sessionData)
	require.NoError(t, os.WriteFile(sessionPath, data, 0o600))

	manager.loadSessionsFromDisk()
	manager.mu.RLock()
	sessions := manager.sessions
	manager.mu.RUnlock()

	_, exists := sessions["bad-expiry-session"]
	assert.False(t, exists, "Session with invalid expiry should not be loaded")
}

func TestLoadSessionsFromDisk_Uncovered_MalformedSessionFile(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionPath := sessionPathForConfig(configFile)
	require.NoError(t, os.WriteFile(sessionPath, []byte("{invalid json"), 0o600))

	manager.loadSessionsFromDisk()
	// Should not panic; malformed file should be removed
	_, statErr := os.Stat(sessionPath)
	assert.True(t, os.IsNotExist(statErr), "Malformed session file should be removed")
}

// --- AuthManager type methods uncovered ---

func TestAuthManager_SessionTTL_Uncovered(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, 2*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 2*time.Hour, manager.SessionTTL())
}

func TestAuthManager_SessionTTL_Uncovered_ZeroDefault(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, 0)
	require.NoError(t, err)
	assert.Equal(t, DefaultSessionTTL, manager.SessionTTL(), "Zero TTL should default to DefaultSessionTTL")
}

func TestAuthManager_Username_Uncovered_NotInitialized(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	username, ok := manager.Username()
	assert.False(t, ok)
	assert.Empty(t, username)
}

func TestAuthManager_IsInitialized_Uncovered_NotInitialized(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	assert.False(t, manager.IsInitialized())
}

func TestAuthManager_SetDisableRateLimit_Uncovered(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	manager.SetDisableRateLimit(true)
	manager.mu.RLock()
	disabled := manager.disableRateLimit
	manager.mu.RUnlock()
	assert.True(t, disabled)

	manager.SetDisableRateLimit(false)
	manager.mu.RLock()
	disabled = manager.disableRateLimit
	manager.mu.RUnlock()
	assert.False(t, disabled)
}

// --- AuthManager.SetApiTokenRepo uncovered ---

func TestAuthManager_SetApiTokenRepo_Uncovered(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	// Setting nil repo should not panic
	manager.SetApiTokenRepo(nil)
}

// --- AuthManager.Logout uncovered ---

func TestAuthManager_Logout_Uncovered_EmptySessionID(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	// Should not panic with empty session ID
	assert.NotPanics(t, func() {
		manager.Logout("")
	})
}

func TestAuthManager_Logout_Uncovered_NonexistentSession(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	// Should not panic with nonexistent session ID
	assert.NotPanics(t, func() {
		manager.Logout("nonexistent-session-id")
	})
}

// --- AuthManager.AuthenticateSession uncovered ---

func TestAuthManager_AuthenticateSession_Uncovered_EmptySessionID(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	_, err = manager.AuthenticateSession("")
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestAuthManager_AuthenticateSession_Uncovered_NotInitialized(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	_, err = manager.AuthenticateSession("some-session")
	assert.ErrorIs(t, err, ErrAuthNotInitialized)
}

func TestAuthManager_AuthenticateSession_Uncovered_WrongUsername(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Manually inject a session with wrong username
	manager.mu.Lock()
	manager.sessions["bad-session"] = sessionRecord{
		Username:   "otheruser",
		ExpiresAt:  time.Now().Add(time.Hour),
		Persistent: false,
	}
	manager.mu.Unlock()

	_, err = manager.AuthenticateSession("bad-session")
	assert.ErrorIs(t, err, ErrInvalidSession)
}

// --- AuthManager.Login uncovered ---

func TestAuthManager_Login_Uncovered_NotInitialized(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	_, err = manager.Login("admin", "password123", false)
	assert.ErrorIs(t, err, ErrAuthNotInitialized)
}

// --- loadCredentialsFromDisk: file not exist returns nil ---

func TestLoadCredentialsFromDisk_Uncovered_FileNotExist(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	// No credential file created — should succeed with nil credentials
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	assert.False(t, manager.IsInitialized())
}

// --- writeCredentialsToDisk: round-trip ---

func TestWriteCredentialsToDisk_Uncovered_RoundTrip(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	require.NoError(t, manager.Setup("testuser", "testpassword123"))

	// Reload from disk
	reloaded, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	assert.True(t, reloaded.IsInitialized())

	username, ok := reloaded.Username()
	assert.True(t, ok)
	assert.Equal(t, "testuser", username)
}

// --- credentialFile.CreatedAt ---

func TestCredentialFile_CreatedAt_Uncovered(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	credPath := credentialPathForConfig(configFile)
	data, err := os.ReadFile(credPath)
	require.NoError(t, err)

	var payload credentialFile
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.NotEmpty(t, payload.CreatedAt, "CreatedAt should be populated")
}

// --- loadCredentialsFromDisk: permission repair on load ---

func TestLoadCredentialsFromDisk_Uncovered_PermissionRepair(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("windows permission bits are ACL-managed")
	}

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	credPath := credentialPathForConfig(configFile)
	require.NoError(t, os.Chmod(credPath, 0o644))

	_, err = NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	info, err := os.Stat(credPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// --- ValidateToken uncovered ---

func TestAuthManager_ValidateToken_Uncovered_NoRepo(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	_, err = manager.ValidateToken(nil, "somehash")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api token repository not configured")
}

// --- UpdateTokenLastUsed uncovered ---

func TestAuthManager_UpdateTokenLastUsed_Uncovered_NoRepo(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.UpdateTokenLastUsed(nil, "some-token-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api token repository not configured")
}

// --- writePersistentSessionsLocked: sort order ---

func TestWritePersistentSessionsLocked_Uncovered_SortedOutput(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Create multiple sessions
	_, err = manager.Login("admin", "password123", true)
	require.NoError(t, err)
	_, err = manager.Login("admin", "password123", true)
	require.NoError(t, err)

	sessionPath := sessionPathForConfig(configFile)
	data, err := os.ReadFile(sessionPath)
	require.NoError(t, err)

	var payload sessionFile
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.Equal(t, 2, len(payload.Sessions))

	// Verify sessions are sorted by ID
	if len(payload.Sessions) == 2 {
		assert.True(t, payload.Sessions[0].ID <= payload.Sessions[1].ID, "Sessions should be sorted by ID")
	}
}

// --- isUnsupportedPermissionMutation uncovered ---

func TestIsUnsupportedPermissionMutation_Uncovered_NilError(t *testing.T) {
	assert.False(t, isUnsupportedPermissionMutation(nil))
}

func TestIsUnsupportedPermissionMutation_Uncovered_GenericError(t *testing.T) {
	assert.False(t, isUnsupportedPermissionMutation(errors.New("generic error")))
}

// --- loadCredentialsFromDisk uncovered: valid file round-trip with all fields ---

func TestLoadCredentialsFromDisk_Uncovered_ValidRoundTrip(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("testadmin", "testpassword123"))

	// Verify credentials are loaded correctly
	assert.True(t, manager.IsInitialized())

	username, ok := manager.Username()
	assert.True(t, ok)
	assert.Equal(t, "testadmin", username)

	// Create a new manager and verify it loads from disk
	manager2, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	assert.True(t, manager2.IsInitialized())

	username2, ok2 := manager2.Username()
	assert.True(t, ok2)
	assert.Equal(t, "testadmin", username2)
}

// --- loadCredentialsFromDisk uncovered: JSON parse error ---

func TestLoadCredentialsFromDisk_Uncovered_InvalidJSON(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	credPath := credentialPathForConfig(configFile)

	require.NoError(t, os.WriteFile(credPath, []byte("not valid json at all"), 0o600))

	_, err := NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse")
}

// --- writeCredentialsToDisk uncovered: nil credentials ---

func TestWriteCredentialsToDisk_Uncovered_NilCredentials(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.writeCredentialsToDisk(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials are required")
}

// --- Login uncovered: rate limiting ---

func TestAuthManager_Login_Uncovered_RateLimiting(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Make multiple failed login attempts
	for i := 0; i < maxFailedLoginAttempts; i++ {
		_, err = manager.Login("admin", "wrongpassword", false)
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	}

	// Next attempt should be rate limited
	_, err = manager.Login("admin", "password123", false)
	assert.ErrorIs(t, err, ErrLoginRateLimited)
}

// --- Login uncovered: weak password ---

func TestAuthManager_Login_Uncovered_WeakPassword(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.Setup("admin", "short")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWeakPassword)
}

// --- Login uncovered: empty username ---

func TestAuthManager_Login_Uncovered_EmptyUsername(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.Setup("", "password123")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidUsername)
}

// --- Login uncovered: already initialized ---

func TestAuthManager_Login_Uncovered_AlreadyInitialized(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Second setup should fail
	err = manager.Setup("admin2", "password456")
	require.ErrorIs(t, err, ErrAuthAlreadySet)
}

// --- AuthenticateSession uncovered: expired session ---

func TestAuthManager_AuthenticateSession_Uncovered_ExpiredSession(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Login with remember me
	sessionID, err := manager.Login("admin", "password123", true)
	require.NoError(t, err)

	// Manually expire the session
	manager.mu.Lock()
	sess := manager.sessions[sessionID]
	sess.ExpiresAt = time.Now().Add(-time.Hour) // Expired
	manager.sessions[sessionID] = sess
	manager.mu.Unlock()

	_, err = manager.AuthenticateSession(sessionID)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

// --- Login uncovered: successful login with remember me ---

func TestAuthManager_Login_Uncovered_SuccessWithRememberMe(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionID, err := manager.Login("admin", "password123", true)
	require.NoError(t, err)
	assert.NotEmpty(t, sessionID)

	// Session file should exist
	sessionPath := sessionPathForConfig(configFile)
	_, err = os.Stat(sessionPath)
	require.NoError(t, err, "Session file should exist after remember-me login")
}

// --- Login uncovered: wrong credentials ---

func TestAuthManager_Login_Uncovered_WrongCredentials(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	_, err = manager.Login("wronguser", "password123", false)
	assert.ErrorIs(t, err, ErrInvalidCredentials)

	_, err = manager.Login("admin", "wrongpassword", false)
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

// --- loadSessionsFromDisk uncovered: valid session loaded ---

func TestLoadSessionsFromDisk_Uncovered_ValidSessionLoaded(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Login with remember me to persist session
	sessionID, err := manager.Login("admin", "password123", true)
	require.NoError(t, err)

	// Create a new manager that should load the session
	manager2, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	// Session should be valid in the new manager
	username, err := manager2.AuthenticateSession(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "admin", username)
}

// --- loadSessionsFromDisk uncovered: permission repair ---

func TestLoadSessionsFromDisk_Uncovered_PermissionRepair(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("windows permission bits are ACL-managed")
	}

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Login with remember-me to create the session file
	_, err = manager.Login("admin", "password123", true)
	require.NoError(t, err)

	sessionPath := sessionPathForConfig(configFile)
	// Make the session file world-readable
	require.NoError(t, os.Chmod(sessionPath, 0o644))

	// Create a new manager — it should fix permissions
	_, err = NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	info, err := os.Stat(sessionPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// --- writePersistentSessionsLocked uncovered: no persistent sessions ---

func TestWritePersistentSessionsLocked_Uncovered_NoPersistentSessions(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Login without remember me
	sessionID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)

	// Session file should not exist (or be empty) since session is not persistent
	sessionPath := sessionPathForConfig(configFile)
	manager.mu.Lock()
	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()
	require.NoError(t, err)

	// Session file should be removed since no persistent sessions exist
	_, statErr := os.Stat(sessionPath)
	assert.True(t, os.IsNotExist(statErr), "Session file should be removed when no persistent sessions")
	_ = sessionID
}

// --- Logout uncovered: persistent session removes from disk ---

func TestAuthManager_Logout_Uncovered_PersistentSessionRemoved(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionID, err := manager.Login("admin", "password123", true)
	require.NoError(t, err)

	// Session file should exist
	sessionPath := sessionPathForConfig(configFile)
	_, err = os.Stat(sessionPath)
	require.NoError(t, err)

	// Logout should remove the session
	manager.Logout(sessionID)

	// Verify session is gone
	_, err = manager.AuthenticateSession(sessionID)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

// --- AuthManager enforceSessionLimitLocked uncovered ---

func TestAuthManager_EnforceSessionLimit_Uncovered(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))
	manager.SetDisableRateLimit(true)

	// Create maxActiveSessions + 1 sessions
	sessions := make([]string, 0, maxActiveSessions+1)
	for i := 0; i < maxActiveSessions+1; i++ {
		sessionID, err := manager.Login("admin", "password123", false)
		require.NoError(t, err)
		sessions = append(sessions, sessionID)
	}

	// The first session should have been evicted
	_, err = manager.AuthenticateSession(sessions[0])
	assert.ErrorIs(t, err, ErrInvalidSession, "First session should be evicted when limit exceeded")

	// The last session should still be valid
	_, err = manager.AuthenticateSession(sessions[maxActiveSessions])
	assert.NoError(t, err, "Latest session should still be valid")
}
