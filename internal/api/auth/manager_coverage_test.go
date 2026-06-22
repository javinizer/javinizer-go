package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAuthManager_Coverage_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NotNil(t, manager)
	assert.Equal(t, time.Hour, manager.SessionTTL())
}

func TestNewAuthManager_Coverage_DefaultTTL(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	manager, err := NewAuthManager(configFile, 0)
	require.NoError(t, err)
	require.NotNil(t, manager)
	assert.Equal(t, DefaultSessionTTL, manager.SessionTTL())
}

func TestNewAuthManager_Coverage_NegativeTTL(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	manager, err := NewAuthManager(configFile, -1*time.Hour)
	require.NoError(t, err)
	require.NotNil(t, manager)
	assert.Equal(t, DefaultSessionTTL, manager.SessionTTL())
}

func TestNewAuthManager_Coverage_SetupAndLogin(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.Setup("admin", "password123")
	require.NoError(t, err)

	token, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestNewAuthManager_Coverage_LoginWrongPassword(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.Setup("admin", "password123")
	require.NoError(t, err)

	_, err = manager.Login("admin", "wrongpassword", false)
	assert.Error(t, err)
}

func TestNewAuthManager_Coverage_LoginNonexistentUser(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.Setup("admin", "password123")
	require.NoError(t, err)

	_, err = manager.Login("nobody", "password123", false)
	assert.Error(t, err)
}

func TestNewAuthManager_Coverage_ValidateTokenNoRepo(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	// ValidateToken requires apiTokenRepo which is not set by NewAuthManager
	_, err = manager.ValidateToken(nil, "some-token")
	assert.Error(t, err)
}

func TestNewAuthManager_Coverage_Logout(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.Setup("admin", "password123")
	require.NoError(t, err)

	token, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)

	manager.Logout(token)

	// After logout, ValidateToken should still fail (no repo configured)
	_, err = manager.ValidateToken(nil, token)
	assert.Error(t, err)
}

func TestNewAuthManager_Coverage_RememberMe(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.Setup("admin", "password123")
	require.NoError(t, err)

	token, err := manager.Login("admin", "password123", true)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestNewAuthManager_Coverage_IsInitialized(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	// Before setup
	assert.False(t, manager.IsInitialized())

	// After setup
	err = manager.Setup("admin", "password123")
	require.NoError(t, err)

	assert.True(t, manager.IsInitialized())
}

func TestNewAuthManager_Coverage_UpdateTokenLastUsedNoRepo(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.UpdateTokenLastUsed(nil, "some-id")
	assert.Error(t, err)
}

func TestNewAuthManager_Coverage_InvalidCredentialFile(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	// Write an invalid JSON credential file
	credPath := credentialPathForConfig(configFile)
	require.NoError(t, filepathDir(credPath))
	require.NoError(t, writeTestFile(credPath, "not-json"))

	_, err := NewAuthManager(configFile, time.Hour)
	assert.Error(t, err, "Should fail with invalid credential file")
	assert.Contains(t, err.Error(), "failed to parse")
}

func TestNewAuthManager_Coverage_CredentialFileMissingUsername(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	credPath := credentialPathForConfig(configFile)
	require.NoError(t, filepathDir(credPath))
	require.NoError(t, writeTestFile(credPath, `{"memory":64,"time":1,"threads":1,"keylen":32,"salt":"abc","hash":"def"}`))

	_, err := NewAuthManager(configFile, time.Hour)
	assert.Error(t, err, "Should fail when username is missing")
	assert.Contains(t, err.Error(), "username is required")
}

func TestNewAuthManager_Coverage_CredentialFileMissingArgon2Params(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	credPath := credentialPathForConfig(configFile)
	require.NoError(t, filepathDir(credPath))
	require.NoError(t, writeTestFile(credPath, `{"username":"admin","salt":"abc","hash":"def"}`))

	_, err := NewAuthManager(configFile, time.Hour)
	assert.Error(t, err, "Should fail when argon2 params are missing")
	assert.Contains(t, err.Error(), "argon2 parameters are required")
}

func TestNewAuthManager_Coverage_Username(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	// Before setup
	_, ok := manager.Username()
	assert.False(t, ok)

	// After setup
	err = manager.Setup("admin", "password123")
	require.NoError(t, err)

	username, ok := manager.Username()
	assert.True(t, ok)
	assert.Equal(t, "admin", username)
}

func TestNewAuthManager_Coverage_AuthenticateSession(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.Setup("admin", "password123")
	require.NoError(t, err)

	// Login to get a session
	sessionID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)
	assert.NotEmpty(t, sessionID)

	// Authenticate the session
	username, err := manager.AuthenticateSession(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "admin", username)

	// Invalid session
	_, err = manager.AuthenticateSession("invalid-session")
	assert.Error(t, err)
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0600)
}

func filepathDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0700)
}

func TestNewAuthManager_Coverage_E2EAuthAutoSetup(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	// Set E2E auth environment variable
	t.Setenv("JAVINIZER_E2E_AUTH", "true")
	t.Setenv("JAVINIZER_E2E_USERNAME", "e2euser")
	t.Setenv("JAVINIZER_E2E_PASSWORD", "e2epassword123")

	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err, "E2E auth auto-setup should succeed")
	require.NotNil(t, manager)
	assert.True(t, manager.IsInitialized(), "Manager should be initialized after E2E auto-setup")

	username, ok := manager.Username()
	assert.True(t, ok)
	assert.Equal(t, "e2euser", username)
}
