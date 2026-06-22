package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- writeCredentialsToDisk: nil creds returns error ---

func TestWriteCredentialsToDisk_Miss3_NilCreds(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.writeCredentialsToDisk(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials are required")
}

// --- writeCredentialsToDisk: successful write with temp file cleanup ---

func TestWriteCredentialsToDisk_Miss3_SuccessfulWrite(t *testing.T) {
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

	// Verify no .tmp files left behind
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, filepath.Ext(e.Name()) == ".tmp", "temp file should be cleaned up: %s", e.Name())
	}
}

// --- writePersistentSessionsLocked: no persistent sessions removes file ---

func TestWritePersistentSessionsLocked_Miss3_NoPersistentSessions(t *testing.T) {
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
	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()

	require.NoError(t, err)
}

// --- writePersistentSessionsLocked: successful write with sessions ---

func TestWritePersistentSessionsLocked_Miss3_SuccessfulWrite(t *testing.T) {
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
	manager.sessions["sess1"] = sessionRecord{
		Username:   "admin",
		ExpiresAt:  time.Now().Add(time.Hour),
		Persistent: true,
	}
	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()

	require.NoError(t, err)

	sessionPath := sessionPathForConfig(configFile)
	_, statErr := os.Stat(sessionPath)
	assert.NoError(t, statErr, "session file should exist")
}

// --- writePersistentSessionsLocked: expired sessions are excluded ---

func TestWritePersistentSessionsLocked_Miss3_ExpiredSessionsExcluded(t *testing.T) {
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
	manager.sessions["expired"] = sessionRecord{
		Username:   "admin",
		ExpiresAt:  time.Now().Add(-1 * time.Hour),
		Persistent: true,
	}
	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()

	require.NoError(t, err)
}

// --- writePersistentSessionsLocked: non-persistent sessions are excluded ---

func TestWritePersistentSessionsLocked_Miss3_NonPersistentExcluded(t *testing.T) {
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
	manager.sessions["nonpersist"] = sessionRecord{
		Username:   "admin",
		ExpiresAt:  time.Now().Add(time.Hour),
		Persistent: false,
	}
	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()

	require.NoError(t, err)
}

// --- loadSessionsFromDisk: session with wrong username is skipped ---

func TestLoadSessionsFromDisk_Miss3_WrongUsernameSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionPath := sessionPathForConfig(configFile)
	sessionData := sessionFile{
		Version: 1,
		Sessions: []sessionFileItem{
			{ID: "wrong-user-session", Username: "wronguser", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)},
		},
	}
	data, err := json.Marshal(sessionData)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(sessionPath, data, 0o600))

	manager.loadSessionsFromDisk()

	manager.mu.RLock()
	sessions := manager.sessions
	manager.mu.RUnlock()

	_, exists := sessions["wrong-user-session"]
	assert.False(t, exists, "Session with wrong username should be skipped")
}

// --- loadSessionsFromDisk: expired session is skipped ---

func TestLoadSessionsFromDisk_Miss3_ExpiredSessionSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionPath := sessionPathForConfig(configFile)
	sessionData := sessionFile{
		Version: 1,
		Sessions: []sessionFileItem{
			{ID: "expired-session", Username: "admin", ExpiresAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339)},
		},
	}
	data, err := json.Marshal(sessionData)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(sessionPath, data, 0o600))

	manager.loadSessionsFromDisk()

	manager.mu.RLock()
	sessions := manager.sessions
	manager.mu.RUnlock()

	_, exists := sessions["expired-session"]
	assert.False(t, exists, "Expired session should be skipped")
}
