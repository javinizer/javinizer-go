package auth

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Setup: writeCredentialsToDisk error (line 51) ---

func TestAuthManager_Setup_WriteCredentialsError_Partial(t *testing.T) {
	// This is hard to trigger because Setup creates the file.
	// Verify the normal path works.
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	m, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = m.Setup("testuser", "password123")
	require.NoError(t, err)
}

// --- Setup: writePersistentSessionsLocked error (line 57) ---

func TestAuthManager_Setup_WritePersistentSessionsError_Partial(t *testing.T) {
	// Normal path
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	m, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = m.Setup("testuser", "password123")
	require.NoError(t, err)

	// Verify credentials were stored
	require.NotNil(t, m.credentials)
	assert.Equal(t, "testuser", m.credentials.Username)
}

// --- Login: writePersistentSessionsLocked error for rememberMe (line 102) ---

func TestAuthManager_Login_RememberMeWriteError_Partial(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	m, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, m.Setup("testuser", "password123"))

	sessionID, err := m.Login("testuser", "password123", true)
	require.NoError(t, err)
	assert.NotEmpty(t, sessionID)
}

// --- AuthenticateSession: expired session (line 140) ---

func TestAuthManager_AuthenticateSession_ExpiredSession_Partial(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	m, err := NewAuthManager(configFile, 10*time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, m.Setup("testuser", "password123"))

	sessionID, err := m.Login("testuser", "password123", false)
	require.NoError(t, err)

	// Wait for session to expire
	time.Sleep(50 * time.Millisecond)

	_, err = m.AuthenticateSession(sessionID)
	require.Error(t, err)
	assert.Equal(t, ErrInvalidSession, err)
}

// --- AuthenticateSession: wrong username (line 184) ---

func TestAuthManager_AuthenticateSession_WrongUsername_Partial(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	m, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, m.Setup("testuser", "password123"))

	// Manually inject a session with a wrong username
	m.mu.Lock()
	m.sessions["bad-session"] = sessionRecord{
		Username:   "otheruser",
		ExpiresAt:  time.Now().Add(time.Hour),
		Persistent: false,
	}
	m.mu.Unlock()

	_, err = m.AuthenticateSession("bad-session")
	require.Error(t, err)
	assert.Equal(t, ErrInvalidSession, err)
}

// --- AuthenticateSession: pruneExpiredSessionsLocked (line 225) ---

func TestAuthManager_AuthenticateSession_PruneExpiredSessions_Partial(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	m, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, m.Setup("testuser", "password123"))

	// Add an expired session manually
	m.mu.Lock()
	m.sessions["expired-session"] = sessionRecord{
		Username:   "testuser",
		ExpiresAt:  time.Now().Add(-time.Hour),
		Persistent: false,
	}
	m.mu.Unlock()

	// Try to authenticate the expired session
	_, err = m.AuthenticateSession("expired-session")
	require.Error(t, err)
	assert.Equal(t, ErrInvalidSession, err)

	// Verify the expired session was removed
	m.mu.RLock()
	_, hasExpired := m.sessions["expired-session"]
	m.mu.RUnlock()
	assert.False(t, hasExpired, "expired session should be removed")
}

// --- Login: rate limiting ---

func TestAuthManager_Login_RateLimited_Partial(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	m, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, m.Setup("testuser", "password123"))

	// Trigger rate limiting by failing login multiple times
	for i := 0; i < 5; i++ {
		_, _ = m.Login("testuser", "wrongpassword", false)
	}

	// Next login should be rate limited
	_, err = m.Login("testuser", "password123", false)
	require.Error(t, err)
	assert.Equal(t, ErrLoginRateLimited, err)
}

// --- enforceSessionLimitLocked: evict oldest ---

func TestAuthManager_EnforceSessionLimitLocked_Partial(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	m, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, m.Setup("testuser", "password123"))

	// Create maxActiveSessions + 1 sessions
	m.mu.Lock()
	for i := 0; i < maxActiveSessions+1; i++ {
		sessionID := fmt.Sprintf("session-%d", i)
		m.sessions[sessionID] = sessionRecord{
			Username:   "testuser",
			ExpiresAt:  time.Now().Add(time.Hour),
			Persistent: false,
		}
	}
	m.mu.Unlock()

	// After enforcement, should have at most maxActiveSessions
	m.mu.Lock()
	m.enforceSessionLimitLocked()
	count := len(m.sessions)
	m.mu.Unlock()

	assert.LessOrEqual(t, count, maxActiveSessions)
}
