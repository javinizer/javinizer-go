package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/spf13/afero"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- pruneExpiredSessionsLocked ---

func TestPruneExpiredSessionsLocked_RemovesExpired(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	now := time.Now()
	manager.mu.Lock()
	manager.sessions["expired1"] = sessionRecord{Username: "admin", ExpiresAt: now.Add(-time.Hour), Persistent: false}
	manager.sessions["expired2"] = sessionRecord{Username: "admin", ExpiresAt: now.Add(-2 * time.Hour), Persistent: false}
	manager.sessions["valid"] = sessionRecord{Username: "admin", ExpiresAt: now.Add(time.Hour), Persistent: false}
	manager.pruneExpiredSessionsLocked(now)
	count := len(manager.sessions)
	manager.mu.Unlock()
	assert.Equal(t, 1, count)
}

func TestPruneExpiredSessionsLocked_NoExpired(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	now := time.Now()
	manager.mu.Lock()
	manager.sessions["valid1"] = sessionRecord{Username: "admin", ExpiresAt: now.Add(time.Hour), Persistent: false}
	manager.sessions["valid2"] = sessionRecord{Username: "admin", ExpiresAt: now.Add(2 * time.Hour), Persistent: false}
	manager.pruneExpiredSessionsLocked(now)
	count := len(manager.sessions)
	manager.mu.Unlock()
	assert.Equal(t, 2, count)
}

func TestPruneExpiredSessionsLocked_EmptyMap(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	manager.mu.Lock()
	manager.pruneExpiredSessionsLocked(time.Now())
	count := len(manager.sessions)
	manager.mu.Unlock()
	assert.Equal(t, 0, count)
}

// --- newSessionID ---

func TestNewSessionID_Success(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	id, err := manager.newSessionID()
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	// Should be valid base64url
	assert.Len(t, id, 43) // 32 bytes base64url-encoded without padding
}

func TestNewSessionID_UniqueIDs(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	id1, err := manager.newSessionID()
	require.NoError(t, err)
	id2, err := manager.newSessionID()
	require.NoError(t, err)
	assert.NotEqual(t, id1, id2)
}

func TestNewSessionID_ReaderError(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	manager.randReader = &errorReader{err: errors.New("read failed")}
	id, err := manager.newSessionID()
	assert.Error(t, err)
	assert.Empty(t, id)
}

// --- randomBytes ---

func TestRandomBytes_Success(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	buf, err := manager.randomBytes(16)
	require.NoError(t, err)
	assert.Len(t, buf, 16)
}

func TestRandomBytes_ReaderError(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	manager.randReader = &errorReader{err: errors.New("no entropy")}
	buf, err := manager.randomBytes(32)
	assert.Error(t, err)
	assert.Nil(t, buf)
}

func TestRandomBytes_ZeroSize(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	buf, err := manager.randomBytes(0)
	require.NoError(t, err)
	assert.Len(t, buf, 0)
}

// --- writeCredentialsToDisk error paths ---

func TestWriteCredentialsToDisk_NilCredentials(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.writeCredentialsToDisk(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials are required")
}

func TestWriteCredentialsToDisk_ReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file permissions work differently")
	}

	dir := t.TempDir()
	readonlyDir := filepath.Join(dir, "readonly")
	require.NoError(t, os.Mkdir(readonlyDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(readonlyDir, 0o755) })

	configFile := filepath.Join(readonlyDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	creds := &storedCredentials{
		Username: "admin", Salt: []byte("salt123456789012"),
		Hash:   []byte("hash12345678901234567890123456789"),
		Params: argon2Params{Memory: 65536, Time: 1, Threads: 4, KeyLen: 32},
	}
	err = manager.writeCredentialsToDisk(creds)
	require.Error(t, err)
}

func TestWriteCredentialsToDisk_CreatedAtPopulated(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	creds := &storedCredentials{
		Username: "admin", Salt: []byte("salt123456789012"),
		Hash:   []byte("hash12345678901234567890123456789"),
		Params: argon2Params{Memory: 65536, Time: 1, Threads: 4, KeyLen: 32},
	}
	require.NoError(t, manager.writeCredentialsToDisk(creds))

	credPath := credentialPathForConfig(configFile)
	data, err := os.ReadFile(credPath)
	require.NoError(t, err)
	var payload credentialFile
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.NotEmpty(t, payload.CreatedAt)
}

// --- writePersistentSessionsLocked ---

func TestWritePersistentSessionsLocked_EmptySessionsRemovesFile(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Create a persistent session first
	sid, err := manager.Login("admin", "password123", true)
	require.NoError(t, err)
	sessionPath := sessionPathForConfig(configFile)
	_, err = os.Stat(sessionPath)
	require.NoError(t, err)

	// Remove session from memory and write
	manager.mu.Lock()
	delete(manager.sessions, sid)
	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()
	require.NoError(t, err)
	_, err = os.Stat(sessionPath)
	assert.True(t, os.IsNotExist(err))
}

func TestWritePersistentSessionsLocked_ExpiredSessionNotPersisted(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	now := time.Now()
	manager.mu.Lock()
	manager.sessions["expired-sess"] = sessionRecord{
		Username: "admin", ExpiresAt: now.Add(-time.Hour), Persistent: true,
	}
	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()
	require.NoError(t, err)

	sessionPath := sessionPathForConfig(configFile)
	_, err = os.Stat(sessionPath)
	assert.True(t, os.IsNotExist(err), "expired persistent session should not be written")
}

func TestWritePersistentSessionsLocked_NonPersistentSessionNotWritten(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	now := time.Now()
	manager.mu.Lock()
	manager.sessions["non-persist"] = sessionRecord{
		Username: "admin", ExpiresAt: now.Add(time.Hour), Persistent: false,
	}
	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()
	require.NoError(t, err)

	sessionPath := sessionPathForConfig(configFile)
	_, err = os.Stat(sessionPath)
	assert.True(t, os.IsNotExist(err), "non-persistent session should not be written to disk")
}

func TestWritePersistentSessionsLocked_ValidSessionWritten(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	now := time.Now()
	manager.mu.Lock()
	manager.sessions["valid-sess"] = sessionRecord{
		Username: "admin", ExpiresAt: now.Add(time.Hour), Persistent: true,
	}
	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()
	require.NoError(t, err)

	sessionPath := sessionPathForConfig(configFile)
	data, err := os.ReadFile(sessionPath)
	require.NoError(t, err)
	var payload sessionFile
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.Equal(t, 1, len(payload.Sessions))
	assert.Equal(t, "valid-sess", payload.Sessions[0].ID)
}

func TestWritePersistentSessionsLocked_MultipleSessionsSorted(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	now := time.Now()
	manager.mu.Lock()
	manager.sessions["zebra"] = sessionRecord{Username: "admin", ExpiresAt: now.Add(time.Hour), Persistent: true}
	manager.sessions["alpha"] = sessionRecord{Username: "admin", ExpiresAt: now.Add(2 * time.Hour), Persistent: true}
	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()
	require.NoError(t, err)

	sessionPath := sessionPathForConfig(configFile)
	data, err := os.ReadFile(sessionPath)
	require.NoError(t, err)
	var payload sessionFile
	require.NoError(t, json.Unmarshal(data, &payload))
	require.Len(t, payload.Sessions, 2)
	assert.Equal(t, "alpha", payload.Sessions[0].ID)
	assert.Equal(t, "zebra", payload.Sessions[1].ID)
}

func TestWritePersistentSessionsLocked_FilePermissions(t *testing.T) {
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
}

// --- enforceCredentialFilePermissions via full auth flow ---

func TestEnforceCredentialFilePermissions_RepairsOnLoad(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows permission bits are ACL-managed")
	}

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Make the file world-readable
	credPath := credentialPathForConfig(configFile)
	require.NoError(t, os.Chmod(credPath, 0o644))

	// New manager should repair permissions on load
	_, err = NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	info, err := os.Stat(credPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestEnforceCredentialFilePermissions_RegularFilePasses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses ACLs, not POSIX permissions")
	}

	path := filepath.Join(t.TempDir(), "test.credentials")
	require.NoError(t, os.WriteFile(path, []byte("test"), 0o600))
	err := enforceCredentialFilePermissions(afero.NewOsFs(), path)
	require.NoError(t, err)
}

func TestEnforceCredentialFilePermissions_DirectoryFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses ACLs, not POSIX permissions")
	}

	dir := t.TempDir()
	err := enforceCredentialFilePermissions(afero.NewOsFs(), dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is a directory")
}

func TestEnforceCredentialFilePermissions_NonexistentFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses ACLs, not POSIX permissions")
	}

	err := enforceCredentialFilePermissions(afero.NewOsFs(), "/nonexistent/path/file")
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

// --- errorReader helper ---

type errorReader struct {
	err error
}

func (r *errorReader) Read(_ []byte) (int, error) { return 0, r.err }

// --- randomBytes with partial read ---

func TestRandomBytes_PartialRead(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	// Reader that returns partial data then error
	manager.randReader = &partialReader{data: []byte{0x01, 0x02}, err: errors.New("short read")}
	buf, err := manager.randomBytes(8)
	assert.Error(t, err)
	assert.Nil(t, buf)
}

type partialReader struct {
	data []byte
	err  error
}

func (r *partialReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	if n < len(p) {
		return n, r.err
	}
	return n, nil
}

// --- Setup randomBytes failure ---

func TestSetup_RandomBytesFailure(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	manager.randReader = &errorReader{err: errors.New("no entropy")}
	err = manager.Setup("admin", "password123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "salt")
}

// --- Login newSessionID failure ---

func TestLogin_SessionIDGenerationFailure(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	manager.randReader = bytes.NewReader(nil) // will exhaust immediately
	_, err = manager.Login("admin", "password123", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create session")
}

// --- AuthenticateSession expired persistent session triggers disk write ---

func TestAuthenticateSession_ExpiredPersistentSessionCleansUp(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sid, err := manager.Login("admin", "password123", true)
	require.NoError(t, err)

	// Manually expire the session
	manager.mu.Lock()
	sess := manager.sessions[sid]
	sess.ExpiresAt = time.Now().Add(-time.Hour)
	manager.sessions[sid] = sess
	manager.mu.Unlock()

	_, err = manager.AuthenticateSession(sid)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

// --- Logout persistent session triggers writePersistentSessionsLocked ---

func TestLogout_PersistentSessionWritesDisk(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sid, err := manager.Login("admin", "password123", true)
	require.NoError(t, err)

	sessionPath := sessionPathForConfig(configFile)
	_, err = os.Stat(sessionPath)
	require.NoError(t, err)

	manager.Logout(sid)
	_, err = os.Stat(sessionPath)
	assert.True(t, os.IsNotExist(err), "session file should be removed after logout of only persistent session")
}

// --- writeCredentialsToDisk: credential path is a directory (rename fails) ---

func TestWriteCredentialsToDisk_FinalPathIsDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file permissions work differently")
	}

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	// Create the credential file path as a directory so that rename fails
	credPath := credentialPathForConfig(configFile)
	require.NoError(t, os.Mkdir(credPath, 0o755))

	creds := &storedCredentials{
		Username: "admin", Salt: []byte("salt123456789012"),
		Hash:   []byte("hash12345678901234567890123456789"),
		Params: argon2Params{Memory: 65536, Time: 1, Threads: 4, KeyLen: 32},
	}
	err = manager.writeCredentialsToDisk(creds)
	require.Error(t, err)
	// Rename will fail because the target is a directory
	assert.Contains(t, err.Error(), "failed to persist auth credential file")
}

// --- writePersistentSessionsLocked: credential path is directory (enforce fails on temp file) ---

func TestWritePersistentSessionsLocked_SessionPathIsDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file permissions work differently")
	}

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	// Set up credentials manually
	creds := &storedCredentials{
		Username: "admin", Salt: []byte("salt123456789012"),
		Hash:   []byte("hash12345678901234567890123456789"),
		Params: argon2Params{Memory: 65536, Time: 1, Threads: 4, KeyLen: 32},
	}
	manager.mu.Lock()
	manager.credentials = creds
	manager.sessions["test-sess"] = sessionRecord{
		Username: "admin", ExpiresAt: time.Now().Add(time.Hour), Persistent: true,
	}
	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()
	require.NoError(t, err)

	// Now make the session file path a directory so next write fails on enforce
	sessionPath := sessionPathForConfig(configFile)
	require.NoError(t, os.Remove(sessionPath))
	require.NoError(t, os.Mkdir(sessionPath, 0o755))

	manager.mu.Lock()
	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()
	require.Error(t, err)
}

// --- enforceCredentialFilePermissions: chmod repair succeeds ---

func TestEnforceCredentialFilePermissions_RepairsWrongPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses ACLs, not POSIX permissions")
	}

	path := filepath.Join(t.TempDir(), "test.credentials")
	require.NoError(t, os.WriteFile(path, []byte("test"), 0o644))

	err := enforceCredentialFilePermissions(afero.NewOsFs(), path)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// --- enforceCredentialFilePermissions: symlink is rejected ---

func TestEnforceCredentialFilePermissions_SymlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses ACLs, not POSIX permissions")
	}

	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "target")
	require.NoError(t, os.WriteFile(targetPath, []byte("test"), 0o600))

	symlinkPath := filepath.Join(tmpDir, "symlink")
	require.NoError(t, os.Symlink(targetPath, symlinkPath))

	err := enforceCredentialFilePermissions(afero.NewOsFs(), symlinkPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be a symlink")
}

// --- writePersistentSessionsLocked: creates parent directories ---

func TestWritePersistentSessionsLocked_CreatesParentDir(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "sub", "deep", "config.yaml")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	creds := &storedCredentials{
		Username: "admin", Salt: []byte("salt123456789012"),
		Hash:   []byte("hash12345678901234567890123456789"),
		Params: argon2Params{Memory: 65536, Time: 1, Threads: 4, KeyLen: 32},
	}
	manager.mu.Lock()
	manager.credentials = creds
	manager.sessions["sess"] = sessionRecord{
		Username: "admin", ExpiresAt: time.Now().Add(time.Hour), Persistent: true,
	}
	err = manager.writePersistentSessionsLocked()
	manager.mu.Unlock()
	require.NoError(t, err)

	sessionPath := sessionPathForConfig(configFile)
	_, err = os.Stat(sessionPath)
	require.NoError(t, err)
}

// --- writeCredentialsToDisk: creates parent directories ---

func TestWriteCredentialsToDisk_CreatesParentDir(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "sub", "deep", "config.yaml")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	creds := &storedCredentials{
		Username: "admin", Salt: []byte("salt123456789012"),
		Hash:   []byte("hash12345678901234567890123456789"),
		Params: argon2Params{Memory: 65536, Time: 1, Threads: 4, KeyLen: 32},
	}
	err = manager.writeCredentialsToDisk(creds)
	require.NoError(t, err)

	credPath := credentialPathForConfig(configFile)
	_, err = os.Stat(credPath)
	require.NoError(t, err)
}
